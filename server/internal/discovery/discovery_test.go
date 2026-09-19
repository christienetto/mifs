package discovery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const deezerBlindingLights = `{"id": 908604612, "title": "Blinding Lights", "isrc": "USUG11904206",
	"link": "https://www.deezer.com/track/908604612", "duration": 200, "track_position": 9,
	"release_date": "2020-03-20", "explicit_lyrics": false, "artist": {"name": "The Weeknd"},
	"album": {"title": "After Hours", "cover_xl": "https://cdn/xl.jpg", "cover_medium": "https://cdn/m.jpg"}}`

func TestDeezer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			if r.URL.Query().Get("q") != "blinding lights" || r.URL.Query().Get("limit") != "5" {
				t.Errorf("search query = %v", r.URL.Query())
			}
			w.Write([]byte(`{"data": [` + deezerBlindingLights + `], "total": 1}`))
		case "/track/908604612", "/track/isrc:USUG11904206":
			w.Write([]byte(deezerBlindingLights))
		default:
			w.Write([]byte(`{"error": {"type": "DataException", "message": "no data", "code": 800}}`))
		}
	}))
	defer server.Close()
	deezer := &Deezer{BaseURL: server.URL}
	ctx := context.Background()

	tracks, err := deezer.Search(ctx, "blinding lights", 5)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("search = %v, %v", tracks, err)
	}
	got := tracks[0]
	if got.Ref() != "deezer:908604612" || got.ISRC != "USUG11904206" || got.DurationMs != 200_000 ||
		got.Album != "After Hours" || got.ReleaseYear != 2020 || got.TrackNumber != 9 ||
		got.ArtworkURL != "https://cdn/xl.jpg" || got.ThumbnailURL != "https://cdn/m.jpg" {
		t.Errorf("track = %+v", got)
	}
	if track, err := deezer.Track(ctx, "908604612"); err != nil || track.Title != "Blinding Lights" {
		t.Errorf("track = %+v, %v", track, err)
	}
	if track, err := deezer.ByISRC(ctx, "usug11904206"); err != nil || track.ID != "908604612" {
		t.Errorf("by isrc = %+v, %v", track, err)
	}
	for _, id := range []string{"1", "../etc", ""} {
		if _, err := deezer.Track(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Track(%q) err = %v", id, err)
		}
	}
}

func TestSpotify(t *testing.T) {
	var tokens, unauthorized atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/token" {
			if id, secret, _ := r.BasicAuth(); id != "id" || secret != "secret" || r.FormValue("grant_type") != "client_credentials" {
				t.Errorf("token request: %s %s %v", id, secret, r.Form)
			}
			n := tokens.Add(1)
			w.Write([]byte(`{"access_token": "t` + string(rune('0'+n)) + `", "expires_in": 3600}`))
			return
		}
		// The first token is "revoked": the client must fetch another and retry once.
		if r.Header.Get("Authorization") == "Bearer t1" {
			unauthorized.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		track := `{"id": "0VjIjW4GlUZAMYd2vXMi3b", "name": "Blinding Lights", "duration_ms": 200040,
			"external_ids": {"isrc": "USUG11904206"}, "external_urls": {"spotify": "https://open.spotify.com/track/0VjIjW4GlUZAMYd2vXMi3b"},
			"artists": [{"name": "The Weeknd"}], "album": {"name": "After Hours", "release_date": "2020-03-20",
			"images": [{"url": "https://i/640", "width": 640}, {"url": "https://i/300", "width": 300}, {"url": "https://i/64", "width": 64}]}}`
		switch r.URL.Path {
		case "/v1/search":
			if r.URL.Query().Get("limit") != "10" || r.URL.Query().Get("type") != "track" || r.URL.Query().Get("market") != "US" {
				t.Errorf("search query = %v", r.URL.Query())
			}
			w.Write([]byte(`{"tracks": {"items": [` + track + `]}}`))
		case "/v1/tracks/0VjIjW4GlUZAMYd2vXMi3b":
			w.Write([]byte(track))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	spotify := &Spotify{ClientID: "id", ClientSecret: "secret", Market: "US", APIBase: server.URL, AuthBase: server.URL}
	ctx := context.Background()

	tracks, err := spotify.Search(ctx, "blinding lights", 50) // capped at 10
	if err != nil || len(tracks) != 1 {
		t.Fatalf("search = %v, %v", tracks, err)
	}
	got := tracks[0]
	if got.Ref() != "spotify:0VjIjW4GlUZAMYd2vXMi3b" || got.ISRC != "USUG11904206" || got.ArtworkURL != "https://i/640" ||
		got.ThumbnailURL != "https://i/300" || got.ReleaseYear != 2020 {
		t.Errorf("track = %+v", got)
	}
	if _, err := spotify.Track(ctx, "0VjIjW4GlUZAMYd2vXMi3b"); err != nil {
		t.Error(err)
	}
	if tokens.Load() != 2 || unauthorized.Load() != 1 {
		t.Errorf("tokens fetched %d, unauthorized %d; want 2, 1", tokens.Load(), unauthorized.Load())
	}
	if _, err := spotify.Track(ctx, "not-an-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad id err = %v", err)
	}
}

func TestParseRef(t *testing.T) {
	if p, id, ok := ParseRef("spotify:0VjIjW4GlUZAMYd2vXMi3b"); !ok || p != "spotify" || id != "0VjIjW4GlUZAMYd2vXMi3b" {
		t.Errorf("ParseRef = %q %q %v", p, id, ok)
	}
	for _, bad := range []string{"", "spotify", ":1", "deezer:"} {
		if _, _, ok := ParseRef(bad); ok {
			t.Errorf("ParseRef(%q) ok", bad)
		}
	}
}
