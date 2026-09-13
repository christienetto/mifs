package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christienetto/mifs/server/internal/blob"
	"github.com/christienetto/mifs/server/internal/catalog"
)

type fixture struct {
	server   *httptest.Server
	audioKey string
}

func newFixture(t *testing.T, config Config) fixture {
	t.Helper()
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "mifs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	blobs, err := blob.Open(filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "audio.m4a")
	os.WriteFile(src, []byte("0123456789abcdefghij"), 0o644)
	audioKey, size, err := blobs.Put(src, "audio", "m4a")
	if err != nil {
		t.Fatal(err)
	}

	for i, title := range []string{"Neon Harbor", "Paper Satellites", "Send Me the Chorus"} {
		id := strings.ToLower(strings.ReplaceAll(title, " ", "-"))
		record := catalog.Record{
			Song: catalog.Song{
				ID: id, Title: title, Artist: "Orchid Relay", DurationMs: 120_000, HighlightStartMs: -1,
				AudioKey: audioKey, AudioContentType: "audio/mp4", AudioBytes: size,
				LicenseName: "CC0-1.0",
			},
			Waveform: catalog.Waveform{PointsPerSecond: 10, RMS: []float32{0.1, 0.2}},
		}
		if i == 0 {
			record.Song.HighlightStartMs = 42_500
			record.Song.ArtworkKey, record.Song.ThumbnailKey = audioKey, audioKey
			record.Lyrics = []catalog.LyricLine{{StartMs: 1000, EndMs: 2000, Text: "lights on the water"}}
		}
		if err := store.Put(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}

	config.Store = store
	config.MediaDir = blobs.Dir()
	config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return fixture{server: server, audioKey: audioKey}
}

func get(t *testing.T, url string, header map[string]string) *http.Response {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, url, nil)
	for k, v := range header {
		request.Header.Set(k, v)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func decode[T any](t *testing.T, response *http.Response) T {
	t.Helper()
	var value T
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestListSearchAndPaging(t *testing.T) {
	f := newFixture(t, Config{})
	base := f.server.URL

	list := decode[songListJSON](t, get(t, base+"/v1/songs", nil))
	if len(list.Songs) != 3 || list.NextCursor != "" {
		t.Fatalf("list = %+v", list)
	}
	first := list.Songs[0]
	if first.ID != "neon-harbor" || first.Audio.URL != base+"/media/"+f.audioKey || first.Audio.ContentType != "audio/mp4" {
		t.Errorf("first = %+v", first)
	}
	if !first.HasLyrics {
		t.Error("first song should report lyrics")
	}
	if first.Artwork == nil || first.HighlightStartMs == nil || *first.HighlightStartMs != 42_500 {
		t.Errorf("artwork/highlight = %+v %v", first.Artwork, first.HighlightStartMs)
	}
	if second := list.Songs[1]; second.HasLyrics || second.Artwork != nil || second.HighlightStartMs != nil {
		t.Errorf("optional fields should be omitted: %+v", second)
	}

	search := decode[songListJSON](t, get(t, base+"/v1/songs?q=water", nil))
	if len(search.Songs) != 1 || search.Songs[0].ID != "neon-harbor" {
		t.Errorf("lyric search = %+v", search.Songs)
	}

	page1 := decode[songListJSON](t, get(t, base+"/v1/songs?limit=2", nil))
	if len(page1.Songs) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v", page1)
	}
	page2 := decode[songListJSON](t, get(t, base+"/v1/songs?limit=2&cursor="+page1.NextCursor, nil))
	if len(page2.Songs) != 1 || page2.Songs[0].ID != "send-me-the-chorus" || page2.NextCursor != "" {
		t.Errorf("page2 = %+v", page2)
	}

	for _, bad := range []string{"?limit=0", "?limit=abc", "?cursor=!!", "?q=" + strings.Repeat("a", 201)} {
		if response := get(t, base+"/v1/songs"+bad, nil); response.StatusCode != http.StatusBadRequest {
			t.Errorf("%s → %d", bad, response.StatusCode)
		}
	}
}

func TestSongLyricsWaveform(t *testing.T) {
	f := newFixture(t, Config{})
	base := f.server.URL

	song := decode[songJSON](t, get(t, base+"/v1/songs/neon-harbor", nil))
	if song.Title != "Neon Harbor" || song.License == nil || song.License.Name != "CC0-1.0" {
		t.Errorf("song = %+v", song)
	}

	lyrics := decode[lyricsJSON](t, get(t, base+"/v1/songs/neon-harbor/lyrics", nil))
	if !lyrics.Synced || len(lyrics.Lines) != 1 || lyrics.Lines[0].Text != "lights on the water" || lyrics.Lines[0].EndMs != 2000 {
		t.Errorf("lyrics = %+v", lyrics)
	}

	waveform := decode[waveformJSON](t, get(t, base+"/v1/songs/neon-harbor/waveform", nil))
	if waveform.PointsPerSecond != 10 || len(waveform.RMS) != 2 || waveform.DurationMs != 120_000 {
		t.Errorf("waveform = %+v", waveform)
	}

	for _, path := range []string{"/v1/songs/missing", "/v1/songs/paper-satellites/lyrics", "/v1/songs/missing/waveform", "/nope"} {
		response := get(t, base+path, nil)
		body := decode[map[string]map[string]string](t, response)
		if response.StatusCode != http.StatusNotFound || body["error"]["code"] != "not_found" {
			t.Errorf("%s → %d %v", path, response.StatusCode, body)
		}
	}
}

func TestJSONRevalidation(t *testing.T) {
	f := newFixture(t, Config{})
	url := f.server.URL + "/v1/songs/neon-harbor"
	response := get(t, url, nil)
	etag := response.Header.Get("ETag")
	if etag == "" || response.Header.Get("Cache-Control") != "public, no-cache" {
		t.Fatalf("headers = %v", response.Header)
	}
	if again := get(t, url, map[string]string{"If-None-Match": etag}); again.StatusCode != http.StatusNotModified {
		t.Errorf("revalidation → %d", again.StatusCode)
	}
}

func TestMediaRangesAndCaching(t *testing.T) {
	f := newFixture(t, Config{})
	url := f.server.URL + "/media/" + f.audioKey

	response := get(t, url, map[string]string{"Range": "bytes=10-14"})
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusPartialContent || string(body) != "abcde" {
		t.Errorf("range → %d %q", response.StatusCode, body)
	}
	if got := response.Header.Get("Content-Range"); got != "bytes 10-14/20" {
		t.Errorf("Content-Range = %q", got)
	}
	if response.Header.Get("Content-Type") != "audio/mp4" || !strings.Contains(response.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("headers = %v", response.Header)
	}
	etag := response.Header.Get("ETag")
	if again := get(t, url, map[string]string{"If-None-Match": etag}); again.StatusCode != http.StatusNotModified {
		t.Errorf("media revalidation → %d", again.StatusCode)
	}

	for _, path := range []string{"/media/../mifs.db", "/media/audio/00/" + strings.Repeat("0", 64) + ".m4a", "/media/%2e%2e/mifs.db"} {
		if response := get(t, f.server.URL+path, nil); response.StatusCode != http.StatusNotFound {
			t.Errorf("%s → %d", path, response.StatusCode)
		}
	}
}

func TestConfiguredURLs(t *testing.T) {
	f := newFixture(t, Config{PublicURL: "https://api.example.com/"})
	song := decode[songJSON](t, get(t, f.server.URL+"/v1/songs/neon-harbor", nil))
	if song.Audio.URL != "https://api.example.com/media/"+f.audioKey {
		t.Errorf("audio url = %q", song.Audio.URL)
	}

	cdn := newFixture(t, Config{PublicURL: "https://api.example.com", MediaURL: "https://cdn.example.com/media/"})
	song = decode[songJSON](t, get(t, cdn.server.URL+"/v1/songs/neon-harbor", nil))
	if song.Audio.URL != "https://cdn.example.com/media/"+cdn.audioKey || song.Artwork.ThumbnailURL != song.Audio.URL {
		t.Errorf("cdn urls = %q %q", song.Audio.URL, song.Artwork.ThumbnailURL)
	}
}
