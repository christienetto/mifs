package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/clip"
)

// Mif lengths accepted. The apps offer 5–15 s; the server allows a little either way.
const (
	minMifMs = 1_000
	maxMifMs = 20_000
	// endSlackMs forgives a selection that overshoots the song's end by rounding.
	endSlackMs = 50
)

type mifJSON struct {
	ID string `json:"id"`
	// URL is what gets shared: it plays the mif in any browser, and the apps can claim it
	// as a universal link (iOS) or app link (Android).
	URL        string              `json:"url"`
	SongID     string              `json:"songId"`
	StartMs    int64               `json:"startMs"`
	DurationMs int64               `json:"durationMs"`
	Lyrics     []catalog.LyricLine `json:"lyrics"` // lines heard, in song time
	// Audio is the mif on its own: a short AAC clip any platform plays.
	Audio     audioJSON `json:"audio"`
	Song      songJSON  `json:"song"`
	CreatedAt string    `json:"createdAt"`
}

func (s *server) createMif(w http.ResponseWriter, r *http.Request) {
	var body struct {
		StartMs    *int64 `json:"startMs"`
		DurationMs *int64 `json:"durationMs"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	song, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if !song.Ready() {
		writeError(w, http.StatusConflict, "not_ready", "this song is still being added to MIFS")
		return
	}
	if s.Clips == nil {
		writeError(w, http.StatusServiceUnavailable, "mifs_unavailable", "this server can't make mifs")
		return
	}
	if body.StartMs == nil || body.DurationMs == nil {
		writeError(w, http.StatusBadRequest, "invalid_range", "startMs and durationMs are required")
		return
	}
	start, length := *body.StartMs, *body.DurationMs
	if start >= 0 && start <= song.DurationMs && length > song.DurationMs-start && length <= song.DurationMs-start+endSlackMs {
		length = song.DurationMs - start
	}
	if start < 0 || length < minMifMs || length > maxMifMs || start > song.DurationMs || length > song.DurationMs-start {
		writeError(w, http.StatusBadRequest, "invalid_range",
			"the mif must be 1 to 20 seconds long and inside the song")
		return
	}
	lines, err := s.Store.Lyrics(r.Context(), song.ID)
	if err != nil && !errors.Is(err, catalog.ErrNotFound) {
		s.internalError(w, r, err)
		return
	}

	ctx, cancel := detach(r, time.Minute)
	defer cancel()
	mif := catalog.NewMif(song, lines, start, length)
	mif, created, err := s.Store.PutMif(ctx, mif)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out, err := s.mifJSON(r, mif, song)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	w.Header().Set("Location", "/v1/mifs/"+mif.ID)
	writeJSONStatus(w, status, out)
}

func (s *server) getMif(w http.ResponseWriter, r *http.Request) {
	out, ok := s.loadMif(w, r)
	if ok {
		writeJSON(w, r, out)
	}
}

// loadMif reads only metadata; audio work is deferred until playback.
func (s *server) loadMif(w http.ResponseWriter, r *http.Request) (mifJSON, bool) {
	mif, err := s.Store.Mif(r.Context(), r.PathValue("id"))
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such mif")
		return mifJSON{}, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return mifJSON{}, false
	}
	song, err := s.Store.Song(r.Context(), mif.SongID)
	if err != nil {
		s.internalError(w, r, err)
		return mifJSON{}, false
	}
	out, err := s.mifJSON(r, mif, song)
	if err != nil {
		s.internalError(w, r, err)
		return mifJSON{}, false
	}
	return out, true
}

func (s *server) mifJSON(r *http.Request, mif catalog.Mif, song catalog.Song) (mifJSON, error) {
	songOut, err := s.songDetailJSON(r, song)
	return mifJSON{
		ID:         mif.ID,
		URL:        s.origin(r) + "/m/" + url.PathEscape(mif.ID),
		SongID:     mif.SongID,
		StartMs:    mif.StartMs,
		DurationMs: mif.DurationMs,
		Lyrics:     mif.Lyrics,
		Audio:      audioJSON{URL: s.origin(r) + "/v1/mifs/" + url.PathEscape(mif.ID) + "/audio", ContentType: clip.ContentType},
		Song:       songOut,
		CreatedAt:  mif.CreatedAt.UTC().Format(time.RFC3339),
	}, err
}

// Only the selected interval crosses the network. No clip is persisted to the blob store.
func (s *server) playMif(w http.ResponseWriter, r *http.Request) {
	mif, err := s.Store.Mif(r.Context(), r.PathValue("id"))
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, 404, "not_found", "no such mif")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if s.Clips == nil {
		writeError(w, 503, "unavailable", "audio unavailable")
		return
	}
	etag := `"moment-` + mif.ID + `-v2"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, err := s.Clips.Bytes(r.Context(), mif.AudioKey, mif.StartMs, mif.DurationMs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", clip.ContentType)
	http.ServeContent(w, r, "moment.m4a", mif.CreatedAt, bytes.NewReader(data))
}
