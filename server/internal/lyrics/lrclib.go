package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/lrc"
)

// LRCLIB is https://lrclib.net, an open database of LRC lyrics. It needs no key.
type LRCLIB struct {
	BaseURL   string // default https://lrclib.net
	Client    *http.Client
	UserAgent string
}

func (l *LRCLIB) Name() string { return "lrclib" }

type lrclibRecord struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"` // seconds
	Instrumental bool    `json:"instrumental"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// Synced asks for the exact recording (title, artist, album, duration) first, then falls
// back to searching by title and artist for one within catalog.MatchToleranceMs.
func (l *LRCLIB) Synced(ctx context.Context, q Query) ([]catalog.LyricLine, error) {
	if q.Title == "" || q.Artist == "" {
		return nil, ErrNotFound
	}
	seconds := strconv.FormatInt(int64(math.Round(float64(q.DurationMs)/1000)), 10)
	if q.Album != "" && q.DurationMs > 0 {
		var record lrclibRecord
		found, err := l.get(ctx, "/api/get", url.Values{
			"track_name": {q.Title}, "artist_name": {q.Artist}, "album_name": {q.Album}, "duration": {seconds},
		}, &record)
		if err != nil {
			return nil, err
		}
		if found && record.Instrumental {
			return nil, ErrNotFound
		}
		if found && record.SyncedLyrics != "" {
			return l.lines(record, q.DurationMs)
		}
	}

	var records []lrclibRecord
	if _, err := l.get(ctx, "/api/search", url.Values{"track_name": {q.Title}, "artist_name": {q.Artist}}, &records); err != nil {
		return nil, err
	}
	for _, record := range records {
		diff := math.Abs(record.Duration*1000 - float64(q.DurationMs))
		if record.SyncedLyrics != "" && !record.Instrumental && (q.DurationMs == 0 || diff <= catalog.MatchToleranceMs) {
			return l.lines(record, q.DurationMs)
		}
	}
	return nil, ErrNotFound
}

func (l *LRCLIB) lines(record lrclibRecord, durationMs int64) ([]catalog.LyricLine, error) {
	if durationMs == 0 {
		durationMs = int64(record.Duration * 1000)
	}
	parsed, err := lrc.Parse(strings.NewReader(record.SyncedLyrics))
	if err != nil {
		return nil, fmt.Errorf("lrclib: %w", err)
	}
	lines := Lines(parsed.Lines, durationMs)
	if len(lines) == 0 {
		return nil, ErrNotFound
	}
	return lines, nil
}

// get fetches JSON into out, reporting found=false for a 404.
func (l *LRCLIB) get(ctx context.Context, path string, query url.Values, out any) (found bool, err error) {
	base := l.BaseURL
	if base == "" {
		base = "https://lrclib.net"
	}
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+path+"?"+query.Encode(), nil)
	if err != nil {
		return false, err
	}
	agent := l.UserAgent
	if agent == "" {
		agent = "MIFS (https://github.com/christienetto/mifs)"
	}
	req.Header.Set("User-Agent", agent)
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("lrclib: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("lrclib: %s returned %s", path, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return false, fmt.Errorf("lrclib: %w", err)
	}
	return true, nil
}
