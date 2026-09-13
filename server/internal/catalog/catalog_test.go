package catalog

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func record(id, title, artist, album string, lyrics ...string) Record {
	lines := make([]LyricLine, len(lyrics))
	for i, text := range lyrics {
		lines[i] = LyricLine{StartMs: int64(i) * 2000, EndMs: int64(i)*2000 + 1500, Text: text}
	}
	return Record{
		Song: Song{
			ID: id, Title: title, Artist: artist, Album: album, DurationMs: 60_000, HighlightStartMs: -1,
			AudioKey: "audio/aa/" + id + ".m4a", AudioContentType: "audio/mp4",
		},
		Lyrics:   lines,
		Waveform: Waveform{PointsPerSecond: 10, RMS: []float32{0.5, 0.25, 0.125}},
	}
}

func seed(t *testing.T, store *Store, records ...Record) {
	t.Helper()
	for _, r := range records {
		if err := store.Put(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
}

func ids(songs []Song) []string {
	out := make([]string, len(songs))
	for i, song := range songs {
		out[i] = song.ID
	}
	return out
}

func TestListAndSearch(t *testing.T) {
	store := openTestStore(t)
	seed(t, store,
		record("neon", "Neon Harbor", "Orchid Relay", "Night Signals", "lights on the water", "harbor calling"),
		record("chorus", "Send Me the Chorus", "Orchid Relay", "Night Signals", "send me the best part"),
		record("paper", "Paper Satellites", "Juniper Fold", "", "folded paper satellites", "neon glow"),
	)
	ctx := context.Background()

	all, err := store.Songs(ctx, "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(all); !slices.Equal(got, []string{"neon", "paper", "chorus"}) {
		t.Errorf("list order = %v", got)
	}

	cases := map[string][]string{
		"orch":           {"neon", "chorus"}, // artist prefix
		"neon":           {"neon", "paper"},  // title match outranks a lyric match
		"harbor orchid":  {"neon"},           // every word must match
		"best part":      {"chorus"},         // lyrics are searchable
		`"); DROP TABLE`: {},                 // FTS syntax is neutralised
		"satellités":     {"paper"},          // diacritics are ignored
	}
	for query, want := range cases {
		songs, err := store.Songs(ctx, query, 50, 0)
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		if got := ids(songs); !slices.Equal(got, want) {
			t.Errorf("%q = %v, want %v", query, got, want)
		}
	}

	page, err := store.Songs(ctx, "", 1, 1)
	if err != nil || len(page) != 1 || page[0].ID != "paper" {
		t.Errorf("paging = %v, %v", ids(page), err)
	}
}

func TestPutReplacesSong(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	seed(t, store, record("neon", "Neon Harbor", "Orchid Relay", "", "old words"))
	updated := record("neon", "Neon Harbor (Remaster)", "Orchid Relay", "", "new words")
	updated.Waveform.RMS = []float32{1}
	seed(t, store, updated)

	if n, _ := store.Count(ctx); n != 1 {
		t.Errorf("count = %d", n)
	}
	song, err := store.Song(ctx, "neon")
	if err != nil || song.Title != "Neon Harbor (Remaster)" || !song.HasLyrics {
		t.Errorf("song = %+v, %v", song, err)
	}
	if old, _ := store.Songs(ctx, "old", 10, 0); len(old) != 0 {
		t.Errorf("stale search entry: %v", ids(old))
	}
	if found, _ := store.Songs(ctx, "new", 10, 0); len(found) != 1 {
		t.Errorf("new lyrics not searchable: %v", ids(found))
	}
	waveform, err := store.Waveform(ctx, "neon")
	if err != nil || len(waveform.RMS) != 1 || waveform.RMS[0] != 1 {
		t.Errorf("waveform = %+v, %v", waveform, err)
	}
}

func TestLyricsAndWaveform(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	seed(t, store, record("neon", "Neon Harbor", "Orchid Relay", "", "one", "two"), record("inst", "Instrumental", "X", ""))

	lines, err := store.Lyrics(ctx, "neon")
	if err != nil || len(lines) != 2 || lines[1] != (LyricLine{StartMs: 2000, EndMs: 3500, Text: "two"}) {
		t.Errorf("lyrics = %+v, %v", lines, err)
	}
	if _, err := store.Lyrics(ctx, "inst"); !errors.Is(err, ErrNotFound) {
		t.Errorf("instrumental lyrics err = %v", err)
	}
	if song, _ := store.Song(ctx, "inst"); song.HasLyrics {
		t.Error("instrumental reports lyrics")
	}
	waveform, err := store.Waveform(ctx, "neon")
	if err != nil || waveform.PointsPerSecond != 10 || len(waveform.RMS) != 3 || waveform.RMS[2] != 0.125 {
		t.Errorf("waveform = %+v, %v", waveform, err)
	}
	if _, err := store.Song(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing song err = %v", err)
	}
}
