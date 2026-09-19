package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/christienetto/mifs/server/internal/audio"
	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/discovery"
	"github.com/christienetto/mifs/server/internal/lyrics"
)

// Worker makes pending songs ready: it gets their audio from the first Source that has
// it, runs it through the same pipeline as a hand-imported song, fetches synced lyrics
// and artwork, and links the song to other providers' copies by ISRC.
//
// The queue is the songs table itself (catalog.Claim), so it survives restarts and needs
// no broker. Several Workers — in this process or others sharing the database — can run
// at once.
type Worker struct {
	Ingester  *Ingester
	Sources   []audio.Source
	Lyrics    []lyrics.Provider
	Discovery []discovery.Provider // for linking a new song to every provider's copy
	HTTP      *http.Client         // downloads provider artwork
	Logger    *slog.Logger
	// Concurrency is how many songs are processed at once. Default 2.
	Concurrency int
	// MaxAttempts is how often a transient failure is retried before a song is marked
	// failed. Default 4.
	MaxAttempts int
	// UnavailableRetry is how soon a song no source had is looked for again when someone
	// asks for it. Default 1 minute: long enough to absorb repeated taps, short enough that a
	// file just added to the library is found. Raise it for sources with per-lookup costs.
	UnavailableRetry time.Duration

	wakeOnce sync.Once
	wake     chan struct{}
}

// errUnavailable means no source could supply the song's audio.
type errUnavailable struct{ detail string }

func (e errUnavailable) Error() string { return e.detail }

// Notify tells the worker there's a new pending song. It never blocks.
func (w *Worker) Notify() {
	select {
	case w.wakeChan() <- struct{}{}:
	default:
	}
}

func (w *Worker) wakeChan() chan struct{} {
	w.wakeOnce.Do(func() { w.wake = make(chan struct{}, 1) })
	return w.wake
}

// Run processes songs until ctx is done.
func (w *Worker) Run(ctx context.Context) error {
	if n, err := w.Ingester.Store.ResetProcessing(ctx); err != nil {
		return err
	} else if n > 0 {
		w.logger().Info("requeued songs interrupted by a restart", "songs", n)
	}
	var wg sync.WaitGroup
	for range max(w.Concurrency, 1) {
		wg.Go(func() { w.loop(ctx) })
	}
	wg.Wait()
	return ctx.Err()
}

func (w *Worker) loop(ctx context.Context) {
	// The ticker picks up scheduled retries; Notify picks up new songs straight away.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		song, ok, err := w.Ingester.Store.Claim(ctx, time.Now())
		if err != nil && ctx.Err() == nil {
			w.logger().Error("claim failed", "err", err)
		}
		if ok {
			w.Process(ctx, song)
			w.Notify() // there may be more; let an idle loop look too
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wakeChan():
		case <-ticker.C:
		}
	}
}

// Process ingests one claimed song and records the outcome.
func (w *Worker) Process(ctx context.Context, song catalog.Song) {
	start := time.Now()
	log := w.logger().With("song", song.ID, "title", song.Title, "artist", song.Artist)
	err := w.ingest(ctx, song)
	store := w.Ingester.Store
	// Record the outcome even while shutting down.
	record := context.WithoutCancel(ctx)
	var unavailable errUnavailable
	switch {
	case err == nil:
		log.Info("song ready", "took", time.Since(start).Round(time.Millisecond))
		return
	case ctx.Err() != nil:
		err = store.SetStatus(record, song.ID, catalog.StatusPending, "interrupted", time.Now())
	case errors.As(err, &unavailable):
		log.Info("song unavailable", "reason", err)
		err = store.SetStatus(record, song.ID, catalog.StatusUnavailable, err.Error(), time.Now().Add(w.unavailableRetry()))
	case song.Attempts >= w.maxAttempts():
		log.Error("song failed", "attempts", song.Attempts, "err", err)
		err = store.SetStatus(record, song.ID, catalog.StatusFailed, err.Error(), time.Now().Add(w.unavailableRetry()))
	default:
		retry := time.Duration(30*math.Pow(4, float64(song.Attempts-1))) * time.Second // 30 s, 2 min, 8 min, …
		log.Warn("song failed; will retry", "attempt", song.Attempts, "retry_in", retry, "err", err)
		err = store.SetStatus(record, song.ID, catalog.StatusPending, err.Error(), time.Now().Add(retry))
	}
	if err != nil {
		log.Error("couldn't record ingestion outcome", "err", err)
	}
}

func (w *Worker) ingest(ctx context.Context, song catalog.Song) error {
	work, err := os.MkdirTemp("", "mifs-worker-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	links, err := w.Ingester.Store.Links(ctx, song.ID)
	if err != nil {
		return err
	}
	req := audio.Request{
		Progress: func(downloaded, total int64) {
			_ = w.Ingester.Store.SetDownloadProgress(ctx, song.ID, downloaded, total)
		},
		SongID: song.ID, ISRC: song.ISRC, Title: song.Title, Artist: song.Artist, Album: song.Album,
		DurationMs: song.DurationMs,
	}
	for _, link := range links {
		req.Refs = append(req.Refs, link.Ref())
	}

	// Lyrics only need metadata, so fetch them while the audio is being acquired.
	lyricsCtx, cancelLyrics := context.WithCancel(ctx)
	defer cancelLyrics()
	found := make(chan []catalog.LyricLine, 1)
	go func() {
		lines := w.fetchLyrics(lyricsCtx, song)
		if err := w.Ingester.Store.PutPendingLyrics(lyricsCtx, song.ID, lines); err != nil {
			w.logger().Warn("pending lyrics", "err", err)
		}
		found <- lines
	}()

	result, source, err := w.acquire(ctx, req, work)
	if err != nil {
		return err
	}
	ready := song
	rms, err := w.Ingester.processAudio(ctx, result.Path, work, &ready)
	if err != nil {
		return err
	}
	ready.Status, ready.StatusDetail, ready.AudioSource = catalog.StatusReady, "", source
	ready.LicenseName, ready.LicenseURL, ready.Attribution = result.LicenseName, result.LicenseURL, result.Attribution
	w.addArtwork(ctx, result.Artwork, work, &ready)

	lines := lyrics.Fit(<-found, ready.DurationMs)
	record := catalog.Record{Song: ready, Lyrics: lines, Waveform: catalog.Waveform{PointsPerSecond: PointsPerSecond, RMS: rms}}
	if err := w.Ingester.Store.Put(ctx, record); err != nil {
		return err
	}
	w.linkProviders(ctx, ready, links)
	return nil
}

// acquire asks each source in turn for the audio. Audio whose length is far from the
// song's is rejected: it's probably another version, and synced lyrics wouldn't fit.
func (w *Worker) acquire(ctx context.Context, req audio.Request, work string) (audio.Result, string, error) {
	if len(w.Sources) == 0 {
		return audio.Result{}, "", errUnavailable{"no audio sources are configured"}
	}
	var failures, rejections []string
	for i, source := range w.Sources {
		dir := filepath.Join(work, fmt.Sprintf("source-%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return audio.Result{}, "", err
		}
		result, err := source.Fetch(ctx, req, dir)
		if errors.Is(err, audio.ErrNotFound) {
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", source.Name(), err))
			continue
		}
		info, err := w.Ingester.probe(ctx, result.Path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", source.Name(), err))
			continue
		}
		got := int64(math.Round(info.duration * 1000))
		if tolerance := max(catalog.MatchToleranceMs, req.DurationMs/50); req.DurationMs > 0 && abs(got-req.DurationMs) > tolerance {
			rejections = append(rejections, fmt.Sprintf("%s's audio is %s long, not %s", source.Name(), clock(got), clock(req.DurationMs)))
			continue
		}
		return result, source.Name(), nil
	}
	if len(failures) > 0 {
		return audio.Result{}, "", errors.New(strings.Join(failures, "; "))
	}
	detail := "no audio source has this song"
	if len(rejections) > 0 {
		detail += " (" + strings.Join(rejections, "; ") + ")"
	}
	return audio.Result{}, "", errUnavailable{detail}
}

func (w *Worker) fetchLyrics(ctx context.Context, song catalog.Song) []catalog.LyricLine {
	q := lyrics.Query{Title: song.Title, Artist: song.Artist, Album: song.Album, ISRC: song.ISRC, DurationMs: song.DurationMs}
	for _, provider := range w.Lyrics {
		lines, err := provider.Synced(ctx, q)
		if err == nil && len(lines) > 0 {
			return lines
		}
		if err != nil && !errors.Is(err, lyrics.ErrNotFound) && ctx.Err() == nil {
			w.logger().Warn("lyrics lookup failed", "provider", provider.Name(), "song", song.ID, "err", err)
		}
	}
	return nil
}

// addArtwork uses the artwork that came with the audio, else the provider's. A song
// without artwork is still ready, so failures are only logged.
func (w *Worker) addArtwork(ctx context.Context, src, work string, song *catalog.Song) {
	if src != "" {
		if err := w.Ingester.processArtwork(ctx, src, work, song); err == nil {
			return
		}
	}
	if song.ArtworkURL == "" {
		return
	}
	downloaded, err := w.download(ctx, song.ArtworkURL, filepath.Join(work, "provider-artwork"))
	if err == nil {
		err = w.Ingester.processArtwork(ctx, downloaded, work, song)
	}
	if err != nil {
		w.logger().Warn("artwork unavailable", "song", song.ID, "err", err)
	}
}

func (w *Worker) download(ctx context.Context, url, dest string) (string, error) {
	client := w.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	file, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(file, io.LimitReader(resp.Body, 20<<20))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return dest, err
}

// linkProviders looks the song up by ISRC on providers it isn't linked to yet, so its
// page can offer "listen on …" for each. Best effort.
func (w *Worker) linkProviders(ctx context.Context, song catalog.Song, links []catalog.ProviderTrack) {
	if song.ISRC == "" {
		return
	}
	linked := map[string]bool{}
	for _, link := range links {
		linked[link.Provider] = true
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for _, provider := range w.Discovery {
		if linked[provider.Name()] {
			continue
		}
		track, err := provider.ByISRC(ctx, song.ISRC)
		if err == nil {
			err = w.Ingester.Store.LinkTrack(ctx, song.ID, track.ProviderTrack)
		}
		if err != nil && !errors.Is(err, discovery.ErrNotFound) {
			w.logger().Warn("provider link failed", "provider", provider.Name(), "song", song.ID, "err", err)
		}
	}
}

func (w *Worker) logger() *slog.Logger {
	if w.Logger == nil {
		return slog.Default()
	}
	return w.Logger
}

func (w *Worker) maxAttempts() int {
	if w.MaxAttempts == 0 {
		return 4
	}
	return w.MaxAttempts
}

func (w *Worker) unavailableRetry() time.Duration {
	if w.UnavailableRetry == 0 {
		return time.Minute
	}
	return w.UnavailableRetry
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func clock(ms int64) string {
	seconds := (ms + 500) / 1000
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
