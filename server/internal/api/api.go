// Package api serves the MIFS catalog over HTTP.
//
//	GET  /healthz
//	GET  /v1/search?q=&limit=            MIFS songs and provider tracks, with availability
//	GET  /v1/songs?q=&limit=&cursor=     list or search ready songs
//	POST /v1/songs                       {"ref"}: add a provider track to MIFS (idempotent)
//	GET  /v1/songs/{id}                  one song, with its ingestion status
//	GET  /v1/songs/{id}/lyrics           synced lyrics
//	GET  /v1/songs/{id}/waveform         loudness envelope for the timeline
//	POST /v1/songs/{id}/mifs             {"startMs","durationMs"}: make a mif (idempotent)
//	GET  /v1/mifs/{id}                   a mif
//	GET  /m/{id}                         a mif's share page: plays in any browser
//	GET  /media/{key}                    audio, clips and artwork (range requests, immutable)
//
// Media is referenced by absolute URLs in responses, so it can move to a CDN (MediaURL)
// without client changes.
package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/christienetto/mifs/server/internal/blob"
	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/clip"
	"github.com/christienetto/mifs/server/internal/discovery"
)

// Config configures the handler.
type Config struct {
	Store *catalog.Store
	// MediaDir is the blob store root served under /media.
	MediaDir string
	// PublicURL is the externally visible origin (e.g. https://api.example.com), used to build
	// media URLs. When empty it's derived from each request, which suits local development
	// from a simulator (localhost) and a phone (the Mac's LAN address) alike.
	IOSAppID  string // Apple team ID + bundle ID, for Universal Links
	PublicURL string
	// MediaURL is where media keys are published (e.g. https://cdn.example.com/media).
	// Defaults to PublicURL + "/media".
	MediaURL string
	Logger   *slog.Logger

	// Discovery providers searched by /v1/search and resolved by POST /v1/songs, in
	// Search uses Spotify only; the other providers remain usable for legacy identities.
	Discovery []discovery.Provider
	// SearchTimeout bounds how long search waits for providers. Default 4 s.
	SearchTimeout time.Duration
	// Clips renders mif audio. Nil disables making mifs.
	Clips *clip.Renderer
	// Notify is called when a song is queued for ingestion, e.g. Worker.Notify.
	Notify func()
}

const (
	defaultLimit = 50
	maxLimit     = 100
	maxQuery     = 200
)

type server struct {
	Config
	media *os.Root
}

// New returns the API handler.
func New(config Config) (http.Handler, error) {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	config.PublicURL = strings.TrimSuffix(config.PublicURL, "/")
	config.MediaURL = strings.TrimSuffix(config.MediaURL, "/")
	media, err := os.OpenRoot(config.MediaDir)
	if err != nil {
		return nil, err
	}
	s := &server{Config: config, media: media}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /.well-known/apple-app-site-association", s.appleAssociation)
	mux.HandleFunc("GET /v1/search", s.search)
	mux.HandleFunc("GET /v1/songs", s.listSongs)
	mux.HandleFunc("POST /v1/songs", s.addSong)
	mux.HandleFunc("GET /v1/songs/{id}", s.getSong)
	mux.HandleFunc("GET /v1/songs/{id}/lyrics", s.getLyrics)
	mux.HandleFunc("GET /v1/songs/{id}/waveform", s.getWaveform)
	mux.HandleFunc("POST /v1/songs/{id}/mifs", s.createMif)
	mux.HandleFunc("GET /v1/mifs/{id}", s.getMif)
	mux.HandleFunc("GET /v1/mifs/{id}/audio", s.playMif)
	mux.HandleFunc("GET /m/{id}", s.mifPage)
	mux.HandleFunc("GET /media/{key...}", s.getMedia)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	return s.logRequests(mux), nil
}

// JSON shapes.

type songJSON struct {
	DownloadedBytes    int64  `json:"downloadedBytes"`
	DownloadTotalBytes int64  `json:"downloadTotalBytes"`
	ID                 string `json:"id"`
	// Status is the song's ingestion status (catalog.Status). Only "ready" songs have audio.
	Status           string       `json:"status"`
	ISRC             string       `json:"isrc,omitempty"`
	Title            string       `json:"title"`
	Artist           string       `json:"artist"`
	Album            string       `json:"album,omitempty"`
	TrackNumber      int          `json:"trackNumber,omitempty"`
	ReleaseYear      int          `json:"releaseYear,omitempty"`
	Genre            string       `json:"genre,omitempty"`
	Explicit         bool         `json:"explicit"`
	DurationMs       int64        `json:"durationMs"`
	HighlightStartMs *int64       `json:"highlightStartMs,omitempty"`
	Audio            *audioJSON   `json:"audio,omitempty"`
	Artwork          *artworkJSON `json:"artwork,omitempty"`
	HasLyrics        bool         `json:"hasLyrics"`
	License          *licenseJSON `json:"license,omitempty"`
	// Links are the song's pages on providers, for "listen on …". Single-song responses only.
	Links []linkJSON `json:"links,omitempty"`
}

type linkJSON struct {
	Provider string `json:"provider"`
	URL      string `json:"url"`
}

type audioJSON struct {
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	Bitrate     int    `json:"bitrate,omitempty"`
	Size        int64  `json:"size"`
}

type artworkJSON struct {
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnailUrl"`
}

type licenseJSON struct {
	Name        string `json:"name"`
	URL         string `json:"url,omitempty"`
	Attribution string `json:"attribution,omitempty"`
}

type songListJSON struct {
	Songs      []songJSON `json:"songs"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

type lyricsJSON struct {
	SongID string              `json:"songId"`
	Synced bool                `json:"synced"`
	Lines  []catalog.LyricLine `json:"lines"`
}

type waveformJSON struct {
	SongID          string    `json:"songId"`
	DurationMs      int64     `json:"durationMs"`
	PointsPerSecond int       `json:"pointsPerSecond"`
	RMS             []float32 `json:"rms"`
}

func (s *server) songJSON(r *http.Request, song catalog.Song) songJSON {
	out := songJSON{
		DownloadedBytes: song.DownloadedBytes, DownloadTotalBytes: song.DownloadTotalBytes,
		ID:          song.ID,
		Status:      string(song.Status),
		ISRC:        song.ISRC,
		Title:       song.Title,
		Artist:      song.Artist,
		Album:       song.Album,
		TrackNumber: song.TrackNumber,
		ReleaseYear: song.ReleaseYear,
		Genre:       song.Genre,
		Explicit:    song.Explicit,
		DurationMs:  song.DurationMs,
		HasLyrics:   song.HasLyrics,
	}
	if song.Ready() {
		out.Audio = &audioJSON{
			URL:         s.mediaURL(r, song.AudioKey),
			ContentType: song.AudioContentType,
			Bitrate:     song.AudioBitrate,
			Size:        song.AudioBytes,
		}
	}
	if song.HighlightStartMs >= 0 {
		out.HighlightStartMs = &song.HighlightStartMs
	}
	if song.ArtworkKey != "" {
		out.Artwork = &artworkJSON{URL: s.mediaURL(r, song.ArtworkKey), ThumbnailURL: s.mediaURL(r, song.ThumbnailKey)}
	} else if song.ArtworkURL != "" {
		// Not ingested yet: show the provider's artwork meanwhile.
		out.Artwork = &artworkJSON{URL: song.ArtworkURL, ThumbnailURL: song.ArtworkURL}
	}
	if song.LicenseName != "" {
		out.License = &licenseJSON{Name: song.LicenseName, URL: song.LicenseURL, Attribution: song.Attribution}
	}
	return out
}

// songDetailJSON is songJSON plus the song's provider links.
func (s *server) songDetailJSON(r *http.Request, song catalog.Song) (songJSON, error) {
	out := s.songJSON(r, song)
	links, err := s.Store.Links(r.Context(), song.ID)
	for _, link := range links {
		if link.URL != "" {
			out.Links = append(out.Links, linkJSON{Provider: link.Provider, URL: link.URL})
		}
	}
	return out, err
}

func (s *server) origin(r *http.Request) string {
	if s.PublicURL != "" {
		return s.PublicURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *server) mediaURL(r *http.Request, key string) string {
	base := s.MediaURL
	if base == "" {
		base = s.origin(r) + "/media"
	}
	return base + "/" + key
}

// Handlers.

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	count, err := s.Store.Count(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, map[string]any{"status": "ok", "songs": count})
}

func (s *server) listSongs(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	query := strings.TrimSpace(params.Get("q"))
	if len(query) > maxQuery {
		writeError(w, http.StatusBadRequest, "invalid_query", "q is too long")
		return
	}
	limit := defaultLimit
	if raw := params.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxLimit {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	offset, err := decodeCursor(params.Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor is not valid")
		return
	}

	// Fetch one extra row to learn whether there's a next page.
	songs, err := s.Store.Songs(r.Context(), query, limit+1, offset)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := songListJSON{Songs: []songJSON{}}
	if len(songs) > limit {
		songs = songs[:limit]
		out.NextCursor = encodeCursor(offset + limit)
	}
	for _, song := range songs {
		out.Songs = append(out.Songs, s.songJSON(r, song))
	}
	writeJSON(w, r, out)
}

func (s *server) getSong(w http.ResponseWriter, r *http.Request) {
	song, ok := s.lookup(w, r)
	if !ok {
		return
	}
	out, err := s.songDetailJSON(r, song)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !song.Ready() {
		// Clients poll while a song is ingested; don't let a cache answer for the server.
		w.Header().Set("Cache-Control", "no-store")
	}
	writeJSON(w, r, out)
}

func (s *server) getLyrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	lines, err := s.Store.Lyrics(r.Context(), id)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no lyrics for this song")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, r, lyricsJSON{SongID: id, Synced: true, Lines: lines})
}

func (s *server) getWaveform(w http.ResponseWriter, r *http.Request) {
	song, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if !song.Ready() {
		writeError(w, http.StatusConflict, "not_ready", "this song is still being added to MIFS")
		return
	}
	waveform, err := s.Store.Waveform(r.Context(), song.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, r, waveformJSON{
		SongID:          song.ID,
		DurationMs:      song.DurationMs,
		PointsPerSecond: waveform.PointsPerSecond,
		RMS:             waveform.RMS,
	})
}

var contentTypes = map[string]string{
	".m4a":  "audio/mp4",
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".json": "application/json",
}

func (s *server) getMedia(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !blob.ValidKey(key) {
		writeError(w, http.StatusNotFound, "not_found", "no such media")
		return
	}
	file, err := s.media.Open(key)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such media")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	// Keys are content hashes, so a response never changes.
	w.Header().Set("Content-Type", contentTypes[path.Ext(key)])
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+blob.ContentHash(key)+`"`)
	http.ServeContent(w, r, "", info.ModTime(), file)
}

func (s *server) lookup(w http.ResponseWriter, r *http.Request) (catalog.Song, bool) {
	song, err := s.Store.Song(r.Context(), r.PathValue("id"))
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such song")
		return song, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return song, false
	}
	return song, true
}

// Helpers.

// writeJSON sends a 200 with a weak ETag; clients revalidate cheaply (304) on every use.
func writeJSON(w http.ResponseWriter, r *http.Request, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "couldn't encode response")
		return
	}
	sum := sha256.Sum256(body)
	etag := `W/"` + hex.EncodeToString(sum[:12]) + `"`
	header := w.Header()
	header.Set("ETag", etag)
	if header.Get("Cache-Control") == "" {
		header.Set("Cache-Control", "public, no-cache")
	}
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, strings.TrimPrefix(etag, "W/")) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	header.Set("Content-Type", "application/json; charset=utf-8")
	w.Write(body)
}

// writeJSONStatus sends a response that mustn't be cached, e.g. to a POST.
func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "couldn't encode response")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(body)
}

// decodeBody reads a small JSON request body into v, rejecting unknown fields so typos
// fail loudly. It writes the error response itself.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	// Requiring JSON makes browsers send a CORS preflight, which this server doesn't grant,
	// so other websites can't make visitors' browsers trigger ingestion or clip rendering.
	if mediaType, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(mediaType) != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "send the body as application/json")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be JSON: "+err.Error())
		return false
	}
	return true
}

func (s *server) notify() {
	if s.Notify != nil {
		s.Notify()
	}
}

// detach returns a context for work that should finish even if the client goes away.
func detach(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), timeout)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (s *server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o" + strconv.Itoa(offset)))
}

func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) < 2 || raw[0] != 'o' {
		return 0, errors.New("invalid cursor")
	}
	offset, err := strconv.Atoi(string(raw[1:]))
	if err != nil || offset < 0 {
		return 0, errors.New("invalid cursor")
	}
	return offset, nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// The catalog is public, so any web client may read it. Writes need a JSON body,
		// which a cross-origin page can't send without a preflight (see decodeBody).
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		attrs := []any{"method", r.Method, "path", r.URL.Path, "status", recorder.status,
			"duration", time.Since(start).Round(time.Microsecond)}
		if rng := r.Header.Get("Range"); rng != "" {
			attrs = append(attrs, "range", rng)
		}
		s.Logger.Info("request", attrs...)
	})
}

func (s *server) appleAssociation(w http.ResponseWriter, r *http.Request) {
	if s.IOSAppID == "" {
		writeError(w, 404, "not_configured", "Universal Links are not configured")
		return
	}
	writeJSON(w, r, map[string]any{"applinks": map[string]any{
		"apps": []string{}, "details": []any{map[string]any{"appID": s.IOSAppID, "paths": []string{"/m/*"}}},
	}})
}
