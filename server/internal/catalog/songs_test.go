package catalog

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func deezerTrack(id, isrc, title, artist string, durationMs int64) ProviderTrack {
	return ProviderTrack{Provider: "deezer", ID: id, ISRC: isrc, Title: title, Artist: artist, DurationMs: durationMs,
		URL: "https://www.deezer.com/track/" + id, ArtworkURL: "https://cdn.example/" + id + ".jpg"}
}

func TestEnsureSongMatchesAndCreates(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	song, created, err := store.EnsureSong(ctx, deezerTrack("1", "usug1-19-04206", "Blinding Lights", "The Weeknd", 200_000))
	if err != nil || !created {
		t.Fatalf("create: %v, created %v", err, created)
	}
	if song.Status != StatusPending || song.ISRC != "USUG11904206" || song.Ready() || song.ArtworkURL == "" {
		t.Errorf("new song = %+v", song)
	}
	if len(song.ID) != 12 {
		t.Errorf("id %q", song.ID)
	}

	cases := []struct {
		name  string
		track ProviderTrack
		same  bool
	}{
		{"same provider track", deezerTrack("1", "", "whatever", "x", 0), true},
		{"another provider, same ISRC", ProviderTrack{Provider: "spotify", ID: "abc", ISRC: "USUG11904206", Title: "Blinding Lights", Artist: "The Weeknd"}, true},
		{"no ISRC, same name, close duration", ProviderTrack{Provider: "apple", ID: "9", Title: "Blinding Lights (feat. Nobody)", Artist: "The Weeknd, Someone", DurationMs: 201_500}, true},
		{"no ISRC, same name, other duration", ProviderTrack{Provider: "apple", ID: "10", Title: "Blinding Lights", Artist: "The Weeknd", DurationMs: 260_000}, false},
		{"different ISRC, same name", deezerTrack("2", "USUG12000001", "Blinding Lights", "The Weeknd", 200_000), false},
	}
	for _, c := range cases {
		got, created, err := store.EnsureSong(ctx, c.track)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if same := got.ID == song.ID; same != c.same || created == c.same {
			t.Errorf("%s: got %s (created %v), first %s", c.name, got.ID, created, song.ID)
		}
	}

	links, err := store.Links(ctx, song.ID)
	if err != nil {
		t.Fatal(err)
	}
	var providers []string
	for _, link := range links {
		providers = append(providers, link.Ref())
	}
	if !slices.Equal(providers, []string{"apple:9", "deezer:1", "spotify:abc"}) {
		t.Errorf("links = %v", providers)
	}
}

func TestEnsureSongLearnsISRCAndLinksSeedSongs(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	seed(t, store, record("neon", "Neon Harbor", "Orchid Relay", ""))
	song, created, err := store.EnsureSong(ctx, deezerTrack("5", "QZES72600001", "Neon Harbor", "Orchid Relay", 60_500))
	if err != nil || created || song.ID != "neon" || song.ISRC != "QZES72600001" || !song.Ready() {
		t.Fatalf("song = %+v, created %v, %v", song, created, err)
	}
}

func TestSongsForTracks(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	first, _, _ := store.EnsureSong(ctx, deezerTrack("1", "USUG11904206", "Blinding Lights", "The Weeknd", 200_000))
	found, err := store.SongsForTracks(ctx, []ProviderTrack{
		{Provider: "deezer", ID: "1"},
		{Provider: "spotify", ID: "zzz", ISRC: "USUG11904206"},
		{Provider: "spotify", ID: "other", ISRC: "GBAYE0601498"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found["deezer:1"].ID != first.ID || found["spotify:zzz"].ID != first.ID {
		t.Errorf("found = %v", found)
	}
	if empty, err := store.SongsForTracks(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("empty = %v, %v", empty, err)
	}
}

func TestPendingSongsAreSearchableButNotListed(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	seed(t, store, record("neon", "Neon Harbor", "Orchid Relay", ""))
	pending, _, _ := store.EnsureSong(ctx, deezerTrack("1", "", "Neon Nights", "Someone", 100_000))

	listed, _ := store.Songs(ctx, "neon", 10, 0)
	if got := ids(listed); !slices.Equal(got, []string{"neon"}) {
		t.Errorf("Songs = %v", got)
	}
	searched, _ := store.Search(ctx, "neon", 10)
	if got := ids(searched); len(got) != 2 || !slices.Contains(got, pending.ID) {
		t.Errorf("Search = %v", got)
	}
	if n, _ := store.Count(ctx); n != 1 {
		t.Errorf("Count = %d", n)
	}
}

func TestQueue(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now()
	a, _, _ := store.EnsureSong(ctx, deezerTrack("1", "", "A", "X", 100_000))
	b, _, _ := store.EnsureSong(ctx, deezerTrack("2", "", "B", "X", 100_000))

	claimed, ok, err := store.Claim(ctx, now.Add(time.Second))
	if err != nil || !ok || claimed.Status != StatusProcessing || claimed.Attempts != 1 {
		t.Fatalf("claim = %+v, %v, %v", claimed, ok, err)
	}
	other, ok, _ := store.Claim(ctx, now.Add(time.Second))
	if !ok || other.ID == claimed.ID {
		t.Fatalf("second claim = %+v", other)
	}
	if _, ok, _ := store.Claim(ctx, now.Add(time.Second)); ok {
		t.Error("claimed a song twice")
	}

	// A transient failure waits for its retry time.
	store.SetStatus(ctx, a.ID, StatusPending, "boom", now.Add(time.Hour))
	if _, ok, _ := store.Claim(ctx, now.Add(time.Minute)); ok {
		t.Error("claimed before retry time")
	}
	if song, ok, _ := store.Claim(ctx, now.Add(2*time.Hour)); !ok || song.ID != a.ID || song.StatusDetail != "boom" {
		t.Errorf("retry claim = %+v", song)
	}

	// Unavailable songs are requeued on request, once their retry time has passed.
	store.SetStatus(ctx, b.ID, StatusUnavailable, "no source", now.Add(time.Hour))
	if queued, _ := store.Requeue(ctx, b.ID, now); queued {
		t.Error("requeued too early")
	}
	if queued, _ := store.Requeue(ctx, b.ID, now.Add(2*time.Hour)); !queued {
		t.Error("not requeued")
	}
	if song, _ := store.Song(ctx, b.ID); song.Status != StatusPending || song.Attempts != 0 {
		t.Errorf("requeued song = %+v", song)
	}

	if n, err := store.ResetProcessing(ctx); err != nil || n != 1 {
		t.Errorf("reset = %d, %v", n, err)
	}
}

func TestMifs(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	seed(t, store, record("neon", "Neon Harbor", "Orchid Relay", "", "one", "two", "three"))
	song, _ := store.Song(ctx, "neon")
	lines, _ := store.Lyrics(ctx, "neon")

	mif := NewMif(song, lines, 1_800, 2_000) // hears the end of "one" (0.2 s: no), all of "two"
	if len(mif.Lyrics) != 1 || mif.Lyrics[0].Text != "two" || mif.AudioKey != song.AudioKey {
		t.Errorf("mif = %+v", mif)
	}
	stored, created, err := store.PutMif(ctx, mif)
	if err != nil || !created || stored.ID != mif.ID || stored.CreatedAt.IsZero() {
		t.Fatalf("put = %+v, %v, %v", stored, created, err)
	}
	if again, created, _ := store.PutMif(ctx, NewMif(song, lines, 1_800, 2_000)); created || again.ID != mif.ID {
		t.Errorf("same moment made a new mif: %v", again.ID)
	}
	if other := MifID("neon", song.AudioKey, 1_801, 2_000); other == mif.ID {
		t.Error("different moments share an ID")
	}
	if _, err := store.Mif(ctx, "missing"); err != ErrNotFound {
		t.Errorf("missing mif err = %v", err)
	}
}

func TestIdentityHelpers(t *testing.T) {
	isrcs := map[string]string{"usug11904206": "USUG11904206", "US-UG1-19-04206": "USUG11904206", "": "", "123456789012": "", "USUG1190420": ""}
	for in, want := range isrcs {
		if got := NormalizeISRC(in); got != want {
			t.Errorf("NormalizeISRC(%q) = %q", in, got)
		}
	}
	keys := [][2]string{
		{MatchKey("Don't Start Now", "Dua Lipa"), MatchKey("Dont Start Now", "Dua Lipa")},
		{MatchKey("Stay (with Justin Bieber)", "The Kid LAROI, Justin Bieber"), MatchKey("STAY", "The Kid LAROI")},
		{MatchKey("Old Town Road - feat. Billy Ray Cyrus", "Lil Nas X & Billy Ray Cyrus"), MatchKey("Old Town Road", "Lil Nas X")},
	}
	for _, pair := range keys {
		if pair[0] != pair[1] {
			t.Errorf("%q != %q", pair[0], pair[1])
		}
	}
	if MatchKey("Blinding Lights (Remix)", "The Weeknd") == MatchKey("Blinding Lights", "The Weeknd") {
		t.Error("a remix matched the original")
	}
}

// A database created before provider tracks existed migrates in place and keeps its songs.
func TestMigratesVersion1Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(migrations[0] + "PRAGMA user_version = 1;"); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO songs (id, title, artist, duration_ms, audio_key, audio_content_type, created_at, updated_at)
		VALUES ('neon', 'Neon Harbor', 'Orchid Relay', 1000, 'audio/aa/x.m4a', 'audio/mp4', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	song, err := store.Song(context.Background(), "neon")
	if err != nil || !song.Ready() || song.AudioSource != "manual" {
		t.Fatalf("migrated song = %+v, %v", song, err)
	}
	matched, created, err := store.EnsureSong(context.Background(), deezerTrack("7", "", "Neon Harbor", "Orchid Relay", 1000))
	if err != nil || created || matched.ID != "neon" {
		t.Errorf("match key not backfilled: %+v, %v, %v", matched, created, err)
	}
}
