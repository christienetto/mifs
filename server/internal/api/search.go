package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/discovery"
)

// statusNew marks a search result MIFS doesn't have yet: POST /v1/songs adds it.
const statusNew = "new"

type searchResultJSON struct {
	// Ref identifies the result for POST /v1/songs: "mifs:<songId>" for songs MIFS has,
	// otherwise the provider's "<provider>:<id>".
	Ref        string       `json:"ref"`
	SongID     string       `json:"songId,omitempty"`
	Status     string       `json:"status"` // a song status, or "new"
	Source     string       `json:"source"` // "mifs" or the provider it was found on
	Title      string       `json:"title"`
	Artist     string       `json:"artist"`
	Album      string       `json:"album,omitempty"`
	DurationMs int64        `json:"durationMs,omitempty"`
	Explicit   bool         `json:"explicit"`
	ISRC       string       `json:"isrc,omitempty"`
	Artwork    *artworkJSON `json:"artwork,omitempty"`
	// Song is the playable song, when Status is "ready".
	PreviewURL string    `json:"previewUrl,omitempty"`
	Song       *songJSON `json:"song,omitempty"`
}

type searchJSON struct {
	Results []searchResultJSON `json:"results"`
	// Incomplete is set when a provider failed or timed out, so results may be missing.
	Incomplete bool `json:"incomplete,omitempty"`
}

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 50
)

// search merges MIFS's songs with provider tracks. MIFS's own matches come first (they
// may be ready to clip now), then provider results in the providers' order, each marked
// with whether MIFS already has it. A song found several ways appears once.
func (s *server) search(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	query := strings.TrimSpace(params.Get("q"))
	if query == "" || len(query) > maxQuery {
		writeError(w, http.StatusBadRequest, "invalid_query", "q must be 1 to 200 characters")
		return
	}
	limit := defaultSearchLimit
	if raw := params.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxSearchLimit {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 50")
			return
		}
		limit = n
	}

	timeout := s.SearchTimeout
	if timeout == 0 {
		timeout = 4 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	found := make([][]discovery.Track, len(s.Discovery))
	failed := make([]error, len(s.Discovery))
	var wg sync.WaitGroup
	for i, provider := range s.Discovery {
		if provider.Name() == "spotify" {
			wg.Go(func() { found[i], failed[i] = provider.Search(ctx, query, limit) })
		}
	}
	wg.Wait()
	out := searchJSON{Results: []searchResultJSON{}}
	configured := false
	for _, p := range s.Discovery {
		if p.Name() == "spotify" {
			configured = true
		}
	}
	if !configured {
		writeError(w, 503, "spotify_unavailable", "Spotify search is not configured")
		return
	}
	var tracks []discovery.Track
	for i, provider := range s.Discovery {
		if failed[i] != nil {
			out.Incomplete = true
			s.Logger.Warn("provider search failed", "provider", provider.Name(), "q", query, "err", failed[i])
			continue
		}
		tracks = append(tracks, found[i]...)
	}
	if out.Incomplete && len(tracks) == 0 {
		writeError(w, http.StatusBadGateway, "spotify_unavailable", "Spotify search is temporarily unavailable")
		return
	}
	refs := make([]catalog.ProviderTrack, len(tracks))
	for i, t := range tracks {
		refs[i] = t.ProviderTrack
	}
	known, err := s.Store.SongsForTracks(r.Context(), refs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	seen := map[string]bool{}
	// claim marks a result's identities as shown, and reports whether any already were.
	claim := func(songID, isrc, title, artist string, durationMs int64) bool {
		keys := []string{"key:" + catalog.MatchKey(title, artist) + "|" + strconv.FormatInt(durationMs/5000, 10)}
		if songID != "" {
			keys = append(keys, "song:"+songID)
		}
		if isrc = catalog.NormalizeISRC(isrc); isrc != "" {
			keys = append(keys, "isrc:"+isrc)
		}
		dup := false
		for _, key := range keys {
			dup = dup || seen[key]
			seen[key] = true
		}
		return dup
	}
	for _, t := range tracks {
		if len(out.Results) >= limit {
			break
		}
		if song, ok := known[t.Ref()]; ok {
			if !claim(song.ID, song.ISRC, song.Title, song.Artist, song.DurationMs) {
				result := s.songResult(r, song, t.Provider)
				result.Ref = t.Ref()
				result.PreviewURL = t.PreviewURL
				out.Results = append(out.Results, result)
			}
			continue
		}
		if claim("", t.ISRC, t.Title, t.Artist, t.DurationMs) {
			continue
		}
		result := searchResultJSON{
			Ref: t.Ref(), Status: statusNew, Source: t.Provider, PreviewURL: t.PreviewURL,
			Title: t.Title, Artist: t.Artist, Album: t.Album, DurationMs: t.DurationMs,
			Explicit: t.Explicit, ISRC: catalog.NormalizeISRC(t.ISRC),
		}
		if t.ArtworkURL != "" {
			result.Artwork = &artworkJSON{URL: t.ArtworkURL, ThumbnailURL: cmp.Or(t.ThumbnailURL, t.ArtworkURL)}
		}
		out.Results = append(out.Results, result)
	}
	if out.Incomplete {
		w.Header().Set("Cache-Control", "no-store")
	}
	writeJSON(w, r, out)
}

func (s *server) songResult(r *http.Request, song catalog.Song, source string) searchResultJSON {
	full := s.songJSON(r, song)
	result := searchResultJSON{
		Ref: "mifs:" + song.ID, SongID: song.ID, Status: full.Status, Source: source,
		Title: song.Title, Artist: song.Artist, Album: song.Album, DurationMs: song.DurationMs,
		Explicit: song.Explicit, ISRC: song.ISRC, Artwork: full.Artwork,
	}
	if song.Ready() {
		result.Song = &full
	}
	return result
}

// addSong makes MIFS have a song: it resolves a search result's ref to the canonical song
// (creating it, and queueing ingestion, when MIFS has none) and returns it. Calling it
// again is harmless, and requeues a song that couldn't be ingested once its retry time
// has passed. Clients poll GET /v1/songs/{id} until status is "ready".
func (s *server) addSong(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ref string `json:"ref"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	providerName, id, ok := discovery.ParseRef(body.Ref)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_ref", `ref must look like "deezer:908604612" or "mifs:<songId>"`)
		return
	}
	// The song is created even if the client hangs up, so a retry finds it.
	ctx, cancel := detach(r, 15*time.Second)
	defer cancel()

	var song catalog.Song
	var created bool
	var err error
	if providerName == "mifs" {
		song, err = s.Store.Song(ctx, id)
	} else {
		song, created, err = s.resolve(ctx, providerName, id)
	}
	var unknown errUnknownProvider
	switch {
	case errors.As(err, &unknown):
		writeError(w, http.StatusBadRequest, "unknown_provider", "no discovery provider named "+string(unknown))
		return
	case errors.Is(err, catalog.ErrNotFound), errors.Is(err, discovery.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such song")
		return
	case err != nil:
		var status *discovery.StatusError
		if errors.As(err, &status) {
			s.Logger.Warn("provider lookup failed", "ref", body.Ref, "err", err)
			writeError(w, http.StatusBadGateway, "provider_error", "couldn't look the song up on "+providerName)
			return
		}
		s.internalError(w, r, err)
		return
	}

	queued := created
	if !created && (song.Status == catalog.StatusUnavailable || song.Status == catalog.StatusFailed) {
		if queued, err = s.Store.Requeue(ctx, song.ID, time.Now()); err == nil && queued {
			song, err = s.Store.Song(ctx, song.ID)
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	if queued {
		s.notify()
	}
	out, err := s.songDetailJSON(r, song)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	w.Header().Set("Location", "/v1/songs/"+song.ID)
	writeJSONStatus(w, status, out)
}

type errUnknownProvider string

func (e errUnknownProvider) Error() string { return "unknown provider " + string(e) }

// resolve returns the song a provider track is a copy of, fetching the track's full
// metadata (the client's copy of a search result is never trusted) when MIFS hasn't
// seen it before.
func (s *server) resolve(ctx context.Context, providerName, id string) (catalog.Song, bool, error) {
	var provider discovery.Provider
	for _, p := range s.Discovery {
		if p.Name() == providerName {
			provider = p
		}
	}
	if provider == nil {
		return catalog.Song{}, false, errUnknownProvider(providerName)
	}
	ref := catalog.ProviderTrack{Provider: providerName, ID: id}
	known, err := s.Store.SongsForTracks(ctx, []catalog.ProviderTrack{ref})
	if err != nil {
		return catalog.Song{}, false, err
	}
	if song, ok := known[ref.Ref()]; ok {
		return song, false, nil
	}
	track, err := provider.Track(ctx, id)
	if err != nil {
		return catalog.Song{}, false, err
	}
	return s.Store.EnsureSong(ctx, track.ProviderTrack)
}
