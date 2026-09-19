package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/clip"
	"github.com/christienetto/mifs/server/internal/discovery"
)

type fakeProvider struct {
	name   string
	tracks []discovery.Track
	err    error
}

func (p *fakeProvider) Name() string { return p.name }

func (p *fakeProvider) Search(context.Context, string, int) ([]discovery.Track, error) {
	return p.tracks, p.err
}

func (p *fakeProvider) Track(_ context.Context, id string) (discovery.Track, error) {
	for _, t := range p.tracks {
		if t.ID == id {
			return t, nil
		}
	}
	if p.err != nil {
		return discovery.Track{}, p.err
	}
	return discovery.Track{}, discovery.ErrNotFound
}

func (p *fakeProvider) ByISRC(context.Context, string) (discovery.Track, error) {
	return discovery.Track{}, discovery.ErrNotFound
}

func track(provider, id, isrc, title, artist string, durationMs int64) discovery.Track {
	return discovery.Track{ProviderTrack: catalog.ProviderTrack{
		Provider: provider, ID: id, ISRC: isrc, Title: title, Artist: artist, DurationMs: durationMs,
		URL: "https://" + provider + ".example/" + id, ArtworkURL: "https://img.example/" + id + ".jpg",
	}}
}

func post(t *testing.T, url, body string) *http.Response {
	t.Helper()
	response, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func TestSearchOnlySpotifyWithServerAvailability(t *testing.T) {
	spotify := &fakeProvider{name: "spotify", tracks: []discovery.Track{
		track("spotify", "sp-new", "USUG11904206", "Blinding Lights", "The Weeknd", 200_040),
		track("spotify", "sp-neon", "QZES72600001", "Neon Harbor", "Orchid Relay", 120_000),
	}}
	deezer := &fakeProvider{name: "deezer", tracks: []discovery.Track{
		track("deezer", "dz-dup", "USUG11904206", "Blinding Lights", "The Weeknd", 200_000), // same recording as sp-new
		track("deezer", "dz-noisrc", "", "Save Your Tears", "The Weeknd", 215_000),
	}}
	broken := &fakeProvider{name: "apple", err: errors.New("down")}
	f := newFixture(t, Config{Discovery: []discovery.Provider{spotify, deezer, broken}})
	// MIFS learns that its "Neon Harbor" is Spotify's sp-neon.
	if _, _, err := f.store.EnsureSong(context.Background(), spotify.tracks[1].ProviderTrack); err != nil {
		t.Fatal(err)
	}

	got := decode[searchJSON](t, get(t, f.server.URL+"/v1/search?q=neon", nil))
	if got.Incomplete {
		t.Error("non-Spotify providers must not participate in search")
	}
	type row struct{ ref, status, source string }
	var rows []row
	for _, r := range got.Results {
		rows = append(rows, row{r.Ref, r.Status, r.Source})
	}
	want := []row{
		{"spotify:sp-new", "new", "spotify"},
		{"spotify:sp-neon", "ready", "spotify"},
	}
	if len(rows) != len(want) {
		t.Fatalf("results = %+v", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
	if first := got.Results[1]; first.Song == nil || first.Song.Audio == nil || first.ISRC != "QZES72600001" {
		t.Errorf("ready result should carry the playable song: %+v", first)
	}
	if second := got.Results[0]; second.Song != nil || second.Artwork == nil || second.ISRC != "USUG11904206" {
		t.Errorf("new result = %+v", second)
	}

	for _, bad := range []string{"", "?q=", "?q=a&limit=51"} {
		if response := get(t, f.server.URL+"/v1/search"+bad, nil); response.StatusCode != http.StatusBadRequest {
			t.Errorf("%q → %d", bad, response.StatusCode)
		}
	}
}

func TestAddSongQueuesIngestion(t *testing.T) {
	deezer := &fakeProvider{name: "deezer", tracks: []discovery.Track{
		track("deezer", "908604612", "USUG11904206", "Blinding Lights", "The Weeknd", 200_000),
	}}
	var notified atomic.Int32
	f := newFixture(t, Config{Discovery: []discovery.Provider{deezer}, Notify: func() { notified.Add(1) }})
	base := f.server.URL

	response := post(t, base+"/v1/songs", `{"ref": "deezer:908604612"}`)
	song := decode[songJSON](t, response)
	if response.StatusCode != http.StatusCreated || song.Status != "pending" || song.Audio != nil ||
		song.ISRC != "USUG11904206" || song.Artwork == nil || len(song.Links) != 1 || notified.Load() != 1 {
		t.Fatalf("created %d %+v, notified %d", response.StatusCode, song, notified.Load())
	}
	if response.Header.Get("Location") != "/v1/songs/"+song.ID {
		t.Errorf("Location = %q", response.Header.Get("Location"))
	}

	again := post(t, base+"/v1/songs", `{"ref": "deezer:908604612"}`)
	if same := decode[songJSON](t, again); again.StatusCode != http.StatusOK || same.ID != song.ID || notified.Load() != 1 {
		t.Errorf("repeat = %d %s, notified %d", again.StatusCode, same.ID, notified.Load())
	}
	if byID := post(t, base+"/v1/songs", `{"ref": "mifs:`+song.ID+`"}`); byID.StatusCode != http.StatusOK {
		t.Errorf("mifs ref → %d", byID.StatusCode)
	}

	polled := get(t, base+"/v1/songs/"+song.ID, nil)
	if p := decode[songJSON](t, polled); p.Status != "pending" || polled.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("poll = %+v %v", p, polled.Header)
	}
	if response := get(t, base+"/v1/songs/"+song.ID+"/waveform", nil); response.StatusCode != http.StatusConflict {
		t.Errorf("waveform of a pending song → %d", response.StatusCode)
	}
	if list := decode[songListJSON](t, get(t, base+"/v1/songs", nil)); len(list.Songs) != 3 {
		t.Errorf("the app's list must only have ready songs, got %d", len(list.Songs))
	}

	// Songs no source had are requeued when asked for again, once their retry time passes.
	f.store.SetStatus(context.Background(), song.ID, catalog.StatusUnavailable, "no source", time.Now().Add(-time.Second))
	if requeued := decode[songJSON](t, post(t, base+"/v1/songs", `{"ref": "deezer:908604612"}`)); requeued.Status != "pending" || notified.Load() != 2 {
		t.Errorf("requeue = %+v, notified %d", requeued, notified.Load())
	}

	cases := map[string]int{
		`{"ref": "nope"}`:             http.StatusBadRequest,
		`{"ref": "tidal:1"}`:          http.StatusBadRequest,
		`{"ref": "deezer:1"}`:         http.StatusNotFound,
		`{"ref": "mifs:missing"}`:     http.StatusNotFound,
		`{"ref": "deezer:1", "x": 1}`: http.StatusBadRequest,
		`not json`:                    http.StatusBadRequest,
	}
	for body, status := range cases {
		if response := post(t, base+"/v1/songs", body); response.StatusCode != status {
			t.Errorf("%s → %d, want %d", body, response.StatusCode, status)
		}
	}
	// A form or text/plain post (what another website could send without a preflight) is refused.
	plain, err := http.Post(base+"/v1/songs", "text/plain", strings.NewReader(`{"ref": "deezer:908604612"}`))
	if err != nil {
		t.Fatal(err)
	}
	plain.Body.Close()
	if plain.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain → %d", plain.StatusCode)
	}
}

func TestMifs(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	f := newFixture(t, Config{})
	ctx := context.Background()
	// A real, clippable song (the fixture's audio is placeholder bytes).
	dir := t.TempDir()
	src := filepath.Join(dir, "tone.m4a")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=30", "-c:a", "aac", src).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	audioKey, size, err := f.blobs.Put(src, "audio", "m4a")
	if err != nil {
		t.Fatal(err)
	}
	err = f.store.Put(ctx, catalog.Record{
		Song: catalog.Song{ID: "tone", Title: "Tone <3", Artist: "Tests", DurationMs: 30_000, HighlightStartMs: -1,
			AudioKey: audioKey, AudioContentType: "audio/mp4", AudioBytes: size},
		Lyrics:   []catalog.LyricLine{{StartMs: 4_000, EndMs: 6_000, Text: "heard"}, {StartMs: 20_000, EndMs: 22_000, Text: "not heard"}},
		Waveform: catalog.Waveform{PointsPerSecond: 10, RMS: []float32{0.5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.store.LinkTrack(ctx, "tone", catalog.ProviderTrack{Provider: "spotify", ID: "sp1", Title: "Tone", Artist: "Tests", URL: "https://open.spotify.com/track/sp1"})
	base := f.server.URL

	response := post(t, base+"/v1/songs/tone/mifs", `{"startMs": 3000, "durationMs": 10000}`)
	mif := decode[mifJSON](t, response)
	if response.StatusCode != http.StatusCreated || mif.ID == "" || mif.URL != base+"/m/"+mif.ID ||
		len(mif.Lyrics) != 1 || mif.Lyrics[0].Text != "heard" || mif.Audio.ContentType != "audio/mp4" ||
		mif.Audio.URL != base+"/v1/mifs/"+mif.ID+"/audio" || mif.Song.ID != "tone" || len(mif.Song.Links) != 1 {
		t.Fatalf("mif = %d %+v", response.StatusCode, mif)
	}
	if again := post(t, base+"/v1/songs/tone/mifs", `{"startMs": 3000, "durationMs": 10000}`); again.StatusCode != http.StatusOK {
		t.Errorf("same mif again → %d", again.StatusCode)
	}

	clipResponse := get(t, mif.Audio.URL, nil)
	clipBytes, _ := io.ReadAll(clipResponse.Body)
	if clipResponse.StatusCode != http.StatusOK || len(clipBytes) < 1000 || clipResponse.Header.Get("Content-Type") != "audio/mp4" {
		t.Errorf("clip → %d, %d bytes (want %d), %s", clipResponse.StatusCode, len(clipBytes), mif.Audio.Size, clipResponse.Header.Get("Content-Type"))
	}
	if _, exists := f.blobs.Exists(clip.Key(audioKey, 3000, 10000)); exists {
		t.Fatal("playback persisted a clip")
	}
	partial := get(t, mif.Audio.URL, map[string]string{"Range": "bytes=0-99"})
	partialBytes, _ := io.ReadAll(partial.Body)
	if partial.StatusCode != 206 || len(partialBytes) != 100 || !bytes.Equal(partialBytes, clipBytes[:100]) {
		t.Fatal("moment range request failed")
	}
	cached := get(t, mif.Audio.URL, map[string]string{"If-None-Match": clipResponse.Header.Get("ETag")})
	if cached.StatusCode != 304 {
		t.Fatalf("cached audio status %d", cached.StatusCode)
	}
	if got := decode[mifJSON](t, get(t, base+"/v1/mifs/"+mif.ID, nil)); got.ID != mif.ID || got.StartMs != 3000 {
		t.Errorf("GET mif = %+v", got)
	}

	page := get(t, mif.URL, nil)
	html, _ := io.ReadAll(page.Body)
	for _, want := range []string{`og:audio" content="` + mif.Audio.URL, "Tone &lt;3", `data-start="4000"`, "https://open.spotify.com/track/sp1", "music.apple.com/search"} {
		if !bytes.Contains(html, []byte(want)) {
			t.Errorf("page lacks %q", want)
		}
	}
	if page.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("page type %q", page.Header.Get("Content-Type"))
	}

	cases := map[string]int{
		`{"startMs": -1, "durationMs": 10000}`:      http.StatusBadRequest,
		`{"startMs": 25000, "durationMs": 10000}`:   http.StatusBadRequest, // past the end
		`{"startMs": 20000, "durationMs": 10040}`:   http.StatusCreated,    // overshoot within rounding: trimmed
		`{"startMs": 0, "durationMs": 500}`:         http.StatusBadRequest,
		`{"startMs": 0, "durationMs": 20001}`:       http.StatusBadRequest,
		`{"startMs": 0}`:                            http.StatusBadRequest,
		`{"startMs": 0, "durationMs": 5000, "x":1}`: http.StatusBadRequest,
	}
	for body, status := range cases {
		if response := post(t, base+"/v1/songs/tone/mifs", body); response.StatusCode != status {
			t.Errorf("%s → %d, want %d", body, response.StatusCode, status)
		}
	}
	for path, status := range map[string]int{"/v1/mifs/missing": 404, "/m/missing": 404} {
		if response := get(t, base+path, nil); response.StatusCode != status {
			t.Errorf("%s → %d", path, response.StatusCode)
		}
	}
	if response := post(t, base+"/v1/songs/missing/mifs", `{"startMs": 0, "durationMs": 5000}`); response.StatusCode != http.StatusNotFound {
		t.Errorf("mif of missing song → %d", response.StatusCode)
	}
	pending, _, _ := f.store.EnsureSong(ctx, catalog.ProviderTrack{Provider: "deezer", ID: "1", Title: "P", Artist: "Q", DurationMs: 60_000})
	if response := post(t, base+"/v1/songs/"+pending.ID+"/mifs", `{"startMs": 0, "durationMs": 5000}`); response.StatusCode != http.StatusConflict {
		t.Errorf("mif of pending song → %d", response.StatusCode)
	}
}

func TestSpotifyFailureDoesNotFallback(t *testing.T) {
	for _, providers := range [][]discovery.Provider{nil, {&fakeProvider{name: "spotify", err: errors.New("offline")}}} {
		f := newFixture(t, Config{Discovery: providers})
		response := get(t, f.server.URL+"/v1/search?q=Neon", nil)
		if response.StatusCode != 503 && response.StatusCode != 502 {
			t.Fatalf("Spotify failure returned %d", response.StatusCode)
		}
	}
}

func TestUniversalLinkAssociation(t *testing.T) {
	f := newFixture(t, Config{IOSAppID: "TEAM.com.example.mifs"})
	response := get(t, f.server.URL+"/.well-known/apple-app-site-association", nil)
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || !bytes.Contains(body, []byte("TEAM.com.example.mifs")) || !bytes.Contains(body, []byte("/m/*")) {
		t.Fatalf("association: %d %s", response.StatusCode, body)
	}
}

func TestSharingNeverNeedsAnEncoder(t *testing.T) {
	f := newFixture(t, Config{Clips: &clip.Renderer{FFmpeg: "deliberately-missing-encoder"}})
	response := post(t, f.server.URL+"/v1/songs/neon-harbor/mifs", `{"startMs":1000,"durationMs":7500}`)
	mif := decode[mifJSON](t, response)
	if response.StatusCode != 201 {
		t.Fatalf("metadata-only sharing: %d", response.StatusCode)
	}
	if get(t, mif.URL, nil).StatusCode != 200 {
		t.Fatal("share page attempted encoding")
	}
	if get(t, f.server.URL+"/v1/mifs/"+mif.ID, nil).StatusCode != 200 {
		t.Fatal("metadata attempted encoding")
	}
}

func TestDownloadProgressIsVisibleWhileProcessing(t *testing.T) {
	f := newFixture(t, Config{})
	ctx := context.Background()
	song, _, err := f.store.EnsureSong(ctx, catalog.ProviderTrack{Provider: "spotify", ID: "progress", Title: "Progress", Artist: "Test", DurationMs: 100000})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.store.Claim(ctx, time.Now()); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := f.store.SetDownloadProgress(ctx, song.ID, 256, 1024); err != nil {
		t.Fatal(err)
	}
	result := decode[songJSON](t, get(t, f.server.URL+"/v1/songs/"+song.ID, nil))
	if result.DownloadedBytes != 256 || result.DownloadTotalBytes != 1024 || result.Status != "processing" {
		t.Fatalf("progress: %+v", result)
	}
	f.store.ResetProcessing(ctx)
	f.store.Claim(ctx, time.Now())
	reset, _ := f.store.Song(ctx, song.ID)
	if reset.DownloadedBytes != 0 || reset.DownloadTotalBytes != 0 {
		t.Fatal("retry retained stale progress")
	}
}
