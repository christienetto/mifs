package discovery

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/christienetto/mifs/server/internal/catalog"
)

// Deezer is Deezer's public API: no key or account needed, ISRCs included, and tracks can
// be looked up by ISRC. That makes it the default discovery provider.
type Deezer struct {
	BaseURL string // default https://api.deezer.com
	Client  *http.Client
}

func (d *Deezer) Name() string { return "deezer" }

type deezerTrack struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	ISRC          string `json:"isrc"`
	Link          string `json:"link"`
	Duration      int64  `json:"duration"` // seconds
	TrackPosition int    `json:"track_position"`
	ReleaseDate   string `json:"release_date"`
	Explicit      bool   `json:"explicit_lyrics"`
	Artist        struct {
		Name string `json:"name"`
	} `json:"artist"`
	Album struct {
		Title       string `json:"title"`
		CoverXL     string `json:"cover_xl"`
		CoverMedium string `json:"cover_medium"`
		ReleaseDate string `json:"release_date"`
	} `json:"album"`
	// Deezer reports errors (including "not found") in a 200 response.
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func (t deezerTrack) track() Track {
	released := t.ReleaseDate
	if released == "" {
		released = t.Album.ReleaseDate
	}
	return Track{
		ProviderTrack: catalog.ProviderTrack{
			Provider:    "deezer",
			ID:          strconv.FormatInt(t.ID, 10),
			ISRC:        t.ISRC,
			Title:       t.Title,
			Artist:      t.Artist.Name,
			Album:       t.Album.Title,
			DurationMs:  t.Duration * 1000,
			Explicit:    t.Explicit,
			TrackNumber: t.TrackPosition,
			ReleaseYear: year(released),
			URL:         t.Link,
			ArtworkURL:  t.Album.CoverXL,
		},
		ThumbnailURL: t.Album.CoverMedium,
	}
}

func (d *Deezer) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	var response struct {
		Data  []deezerTrack `json:"data"`
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := d.get(ctx, "/search?"+url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}.Encode(), &response); err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("deezer: %s (code %d)", response.Error.Message, response.Error.Code)
	}
	tracks := make([]Track, 0, len(response.Data))
	for _, t := range response.Data {
		tracks = append(tracks, t.track())
	}
	return tracks, nil
}

var deezerID = regexp.MustCompile(`^[0-9]{1,20}$`)

func (d *Deezer) Track(ctx context.Context, id string) (Track, error) {
	if !deezerID.MatchString(id) {
		return Track{}, ErrNotFound
	}
	return d.lookup(ctx, "/track/"+id)
}

func (d *Deezer) ByISRC(ctx context.Context, isrc string) (Track, error) {
	if isrc = catalog.NormalizeISRC(isrc); isrc == "" {
		return Track{}, ErrNotFound
	}
	return d.lookup(ctx, "/track/isrc:"+isrc)
}

func (d *Deezer) lookup(ctx context.Context, path string) (Track, error) {
	var t deezerTrack
	if err := d.get(ctx, path, &t); err != nil {
		return Track{}, err
	}
	if t.Error != nil {
		if t.Error.Code == 800 { // DataException: no data
			return Track{}, ErrNotFound
		}
		return Track{}, fmt.Errorf("deezer: %s (code %d)", t.Error.Message, t.Error.Code)
	}
	if t.ID == 0 {
		return Track{}, ErrNotFound
	}
	return t.track(), nil
}

func (d *Deezer) get(ctx context.Context, path string, out any) error {
	base := d.BaseURL
	if base == "" {
		base = "https://api.deezer.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+path, nil)
	if err != nil {
		return err
	}
	return getJSON(d.Client, req, "deezer", out)
}
