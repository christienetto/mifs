package ingest

import (
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/christienetto/mifs/server/internal/audio"
	"github.com/christienetto/mifs/server/internal/blob"
	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/discovery"
	"github.com/christienetto/mifs/server/internal/lyrics"
)

// toneSource supplies the 6-second test tone for every song.
type toneSource struct {
	t     *testing.T
	calls atomic.Int32
}

func (s *toneSource) Name() string { return "tone" }

func (s *toneSource) Fetch(_ context.Context, req audio.Request, dir string) (audio.Result, error) {
	s.calls.Add(1)
	path := filepath.Join(dir, "tone.wav")
	writeTone(s.t, path)
	return audio.Result{Path: path, LicenseName: "CC0-1.0"}, nil
}

type failingSource struct{ err error }

func (s failingSource) Name() string { return "failing" }
func (s failingSource) Fetch(context.Context, audio.Request, string) (audio.Result, error) {
	return audio.Result{}, s.err
}

type fakeLyrics struct{ query lyrics.Query }

func (f *fakeLyrics) Name() string { return "fake" }
func (f *fakeLyrics) Synced(_ context.Context, q lyrics.Query) ([]catalog.LyricLine, error) {
	f.query = q
	return []catalog.LyricLine{{StartMs: 1000, EndMs: 2000, Text: "first"}, {StartMs: 5500, EndMs: 9000, Text: "last"}}, nil
}

type fakeDiscovery struct{}

func (fakeDiscovery) Name() string { return "spotify" }
func (fakeDiscovery) Search(context.Context, string, int) ([]discovery.Track, error) {
	return nil, nil
}
func (fakeDiscovery) Track(context.Context, string) (discovery.Track, error) {
	return discovery.Track{}, discovery.ErrNotFound
}
func (fakeDiscovery) ByISRC(_ context.Context, isrc string) (discovery.Track, error) {
	return discovery.Track{ProviderTrack: catalog.ProviderTrack{Provider: "spotify", ID: "sp1", ISRC: isrc,
		Title: "Tone", Artist: "Tests", URL: "https://open.spotify.com/track/sp1"}}, nil
}

func newWorker(t *testing.T, sources ...audio.Source) (*Worker, *catalog.Store) {
	t.Helper()
	requireFFmpeg(t)
	data := t.TempDir()
	store, err := catalog.Open(filepath.Join(data, "mifs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	blobs, err := blob.Open(filepath.Join(data, "media"))
	if err != nil {
		t.Fatal(err)
	}
	return &Worker{
		Ingester: &Ingester{Store: store, Blobs: blobs, FFmpeg: "ffmpeg", FFprobe: "ffprobe"},
		Sources:  sources,
	}, store
}

// pendingSong adds a song as if a user picked it from search, and claims it.
func pendingSong(t *testing.T, store *catalog.Store, durationMs int64, artworkURL string) catalog.Song {
	t.Helper()
	ctx := context.Background()
	_, _, err := store.EnsureSong(ctx, catalog.ProviderTrack{Provider: "deezer", ID: "1", ISRC: "QZES72600001",
		Title: "Tone", Artist: "Tests", Album: "Signals", DurationMs: durationMs, ArtworkURL: artworkURL})
	if err != nil {
		t.Fatal(err)
	}
	song, ok, err := store.Claim(ctx, time.Now().Add(time.Second))
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	return song
}

func TestWorkerMakesSongReady(t *testing.T) {
	art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		png.Encode(w, image.NewRGBA(image.Rect(0, 0, 64, 64)))
	}))
	defer art.Close()
	source := &toneSource{t: t}
	worker, store := newWorker(t, failingSource{audio.ErrNotFound}, source)
	lyricsProvider := &fakeLyrics{}
	worker.Lyrics = []lyrics.Provider{lyricsProvider}
	worker.Discovery = []discovery.Provider{fakeDiscovery{}}
	ctx := context.Background()

	song := pendingSong(t, store, 6_000, art.URL+"/cover.png")
	worker.Process(ctx, song)

	ready, err := store.Song(ctx, song.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ready.Ready() || ready.AudioSource != "tone" || ready.LicenseName != "CC0-1.0" || ready.StatusDetail != "" ||
		ready.ArtworkKey == "" || ready.ThumbnailKey == "" || !ready.HasLyrics || ready.DurationMs < 5900 {
		t.Fatalf("song = %+v", ready)
	}
	if source.calls.Load() != 1 || lyricsProvider.query.Album != "Signals" || lyricsProvider.query.DurationMs != 6_000 {
		t.Errorf("calls %d, lyrics query %+v", source.calls.Load(), lyricsProvider.query)
	}
	lines, _ := store.Lyrics(ctx, song.ID)
	if len(lines) != 2 || lines[1].EndMs != ready.DurationMs {
		t.Errorf("lyrics not fitted to the audio: %+v", lines)
	}
	if waveform, err := store.Waveform(ctx, song.ID); err != nil || len(waveform.RMS) < 58 {
		t.Errorf("waveform = %d points, %v", len(waveform.RMS), err)
	}
	links, _ := store.Links(ctx, song.ID)
	if len(links) != 2 || links[1].Ref() != "spotify:sp1" {
		t.Errorf("links = %+v", links)
	}
}

func TestWorkerOutcomes(t *testing.T) {
	ctx := context.Background()
	check := func(t *testing.T, store *catalog.Store, id string, status catalog.Status, detail string, retryAfter time.Duration) {
		t.Helper()
		song, _ := store.Song(ctx, id)
		if song.Status != status || !strings.Contains(song.StatusDetail, detail) {
			t.Errorf("song = %s %q, want %s %q", song.Status, song.StatusDetail, status, detail)
		}
		// Not retried before its time.
		if _, ok, _ := store.Claim(ctx, time.Now().Add(retryAfter/2)); ok {
			t.Error("claimed before retry time")
		}
	}

	t.Run("no sources", func(t *testing.T) {
		worker, store := newWorker(t)
		song := pendingSong(t, store, 6_000, "")
		worker.Process(ctx, song)
		check(t, store, song.ID, catalog.StatusUnavailable, "no audio sources", time.Minute)
	})

	t.Run("authentication does not retry automatically", func(t *testing.T) {
		worker, store := newWorker(t, failingSource{audio.ErrAuthenticationRequired})
		song := pendingSong(t, store, 6_000, "")
		worker.Process(ctx, song)
		check(t, store, song.ID, catalog.StatusFailed, audio.ErrAuthenticationRequired.Error(), time.Minute)
		if _, ok, err := store.Claim(ctx, time.Now().Add(time.Hour)); ok || err != nil {
			t.Fatalf("blocked download was retried: %v %v", ok, err)
		}
	})

	t.Run("another source can satisfy an authentication failure", func(t *testing.T) {
		worker, store := newWorker(t, failingSource{audio.ErrAuthenticationRequired}, &toneSource{t: t})
		song := pendingSong(t, store, 6_000, "")
		worker.Process(ctx, song)
		ready, err := store.Song(ctx, song.ID)
		if err != nil || !ready.Ready() {
			t.Fatalf("fallback did not succeed: %+v %v", ready, err)
		}
	})
	t.Run("wrong length is another version", func(t *testing.T) {
		worker, store := newWorker(t, &toneSource{t: t})
		song := pendingSong(t, store, 200_000, "")
		worker.Process(ctx, song)
		check(t, store, song.ID, catalog.StatusUnavailable, "tone's audio is 0:06 long, not 3:20", time.Minute)
	})
	t.Run("transient failure retries, then fails", func(t *testing.T) {
		worker, store := newWorker(t, failingSource{errors.New("network down")})
		worker.MaxAttempts = 2
		song := pendingSong(t, store, 6_000, "")
		worker.Process(ctx, song)
		check(t, store, song.ID, catalog.StatusPending, "network down", 30*time.Second)

		again, ok, _ := store.Claim(ctx, time.Now().Add(time.Minute))
		if !ok || again.Attempts != 2 {
			t.Fatalf("retry claim = %+v", again)
		}
		worker.Process(ctx, again)
		check(t, store, song.ID, catalog.StatusFailed, "network down", time.Minute)
	})
}

func TestWorkerRunDrainsQueue(t *testing.T) {
	worker, store := newWorker(t, &toneSource{t: t})
	worker.Concurrency = 2
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, id := range []string{"1", "2", "3"} {
		store.EnsureSong(ctx, catalog.ProviderTrack{Provider: "deezer", ID: id, Title: "Tone " + id, Artist: "Tests", DurationMs: 6_000})
	}
	done := make(chan error)
	go func() { done <- worker.Run(ctx) }()
	worker.Notify()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if n, _ := store.Count(ctx); n == 3 {
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Errorf("Run = %v", err)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("queue not drained")
}
