package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/christienetto/mifs/server/internal/catalog"
)

// Spotify is the Spotify Web API with an app's client credentials (no user login).
//
// Since February 2026 a development-mode app returns at most 10 search results per
// request, and the app owner must have Premium. MIFS therefore treats Spotify as one
// optional discovery source, never as a dependency.
type Spotify struct {
	ClientID     string
	ClientSecret string
	Market       string // optional ISO country code, e.g. "US"
	APIBase      string // default https://api.spotify.com
	AuthBase     string // default https://accounts.spotify.com
	Client       *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// SpotifyMaxSearch is the most results one search request may ask for.
const SpotifyMaxSearch = 10

func (s *Spotify) Name() string { return "spotify" }

type spotifyTrack struct {
	ID          string `json:"id"`
	PreviewURL  string `json:"preview_url"`
	Name        string `json:"name"`
	DurationMs  int64  `json:"duration_ms"`
	Explicit    bool   `json:"explicit"`
	TrackNumber int    `json:"track_number"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name        string `json:"name"`
		ReleaseDate string `json:"release_date"`
		Images      []struct {
			URL   string `json:"url"`
			Width int    `json:"width"`
		} `json:"images"`
	} `json:"album"`
}

func (t spotifyTrack) track() Track {
	var artists []string
	for _, a := range t.Artists {
		artists = append(artists, a.Name)
	}
	// Images come largest first.
	var artwork, thumbnail string
	for _, image := range t.Album.Images {
		if artwork == "" {
			artwork = image.URL
		}
		if image.Width >= 200 || thumbnail == "" {
			thumbnail = image.URL
		}
	}
	return Track{
		ProviderTrack: catalog.ProviderTrack{
			Provider:    "spotify",
			ID:          t.ID,
			ISRC:        t.ExternalIDs.ISRC,
			Title:       t.Name,
			Artist:      strings.Join(artists, ", "),
			Album:       t.Album.Name,
			DurationMs:  t.DurationMs,
			Explicit:    t.Explicit,
			TrackNumber: t.TrackNumber,
			ReleaseYear: year(t.Album.ReleaseDate),
			URL:         t.ExternalURLs.Spotify,
			ArtworkURL:  artwork,
		},
		ThumbnailURL: thumbnail,
		PreviewURL:   t.PreviewURL,
	}
}

func (s *Spotify) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	var response struct {
		Tracks struct {
			Items []spotifyTrack `json:"items"`
		} `json:"tracks"`
	}
	params := url.Values{"q": {query}, "type": {"track"}, "limit": {strconv.Itoa(min(max(limit, 1), SpotifyMaxSearch))}}
	if err := s.get(ctx, "/v1/search", params, &response); err != nil {
		return nil, err
	}
	tracks := make([]Track, 0, len(response.Tracks.Items))
	for _, t := range response.Tracks.Items {
		if t.ID != "" {
			tracks = append(tracks, t.track())
		}
	}
	return tracks, nil
}

var spotifyID = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

func (s *Spotify) Track(ctx context.Context, id string) (Track, error) {
	if !spotifyID.MatchString(id) {
		return Track{}, ErrNotFound
	}
	var t spotifyTrack
	if err := s.get(ctx, "/v1/tracks/"+id, url.Values{}, &t); err != nil {
		return Track{}, err
	}
	return t.track(), nil
}

func (s *Spotify) ByISRC(ctx context.Context, isrc string) (Track, error) {
	if isrc = catalog.NormalizeISRC(isrc); isrc == "" {
		return Track{}, ErrNotFound
	}
	tracks, err := s.Search(ctx, "isrc:"+isrc, 1)
	if err != nil {
		return Track{}, err
	}
	if len(tracks) == 0 {
		return Track{}, ErrNotFound
	}
	return tracks[0], nil
}

func (s *Spotify) get(ctx context.Context, path string, params url.Values, out any) error {
	if s.Market != "" {
		params.Set("market", s.Market)
	}
	base := s.APIBase
	if base == "" {
		base = "https://api.spotify.com"
	}
	for attempt := 0; ; attempt++ {
		token, err := s.accessToken(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+path+"?"+params.Encode(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		err = getJSON(s.Client, req, "spotify", out)
		var status *StatusError
		if attempt == 0 && errors.As(err, &status) && status.Status == http.StatusUnauthorized {
			s.mu.Lock()
			s.token = "" // expired or revoked early: fetch a new one once
			s.mu.Unlock()
			continue
		}
		return err
	}
}

// accessToken returns a cached client-credentials token, refreshing it a minute early.
func (s *Spotify) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.expires) {
		return s.token, nil
	}
	base := s.AuthBase
	if base == "" {
		base = "https://accounts.spotify.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/api/token",
		strings.NewReader(url.Values{"grant_type": {"client_credentials"}}.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(s.ClientID, s.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var response struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := getJSON(s.Client, req, "spotify auth", &response); err != nil {
		return "", err
	}
	if response.AccessToken == "" {
		return "", fmt.Errorf("spotify auth: no access token")
	}
	s.token = response.AccessToken
	s.expires = time.Now().Add(time.Duration(max(response.ExpiresIn-60, 30)) * time.Second)
	return s.token, nil
}
