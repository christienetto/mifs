// Package discovery searches music providers' catalogs (Spotify, Deezer, …) so MIFS can
// offer far more songs than it holds. Provider tracks are metadata only: they identify a
// recording (ideally by ISRC) and are linked to a canonical MIFS song, whose audio comes
// from elsewhere (package audio).
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/christienetto/mifs/server/internal/catalog"
)

// ErrNotFound means the provider has no such track.
var ErrNotFound = errors.New("track not found")

// Track is a provider's track, with a small artwork URL for search results.
type Track struct {
	catalog.ProviderTrack
	PreviewURL   string
	ThumbnailURL string
}

// Provider is a searchable music catalog.
type Provider interface {
	Name() string
	Search(ctx context.Context, query string, limit int) ([]Track, error)
	// Track returns one track with full metadata (including ISRC when known), or ErrNotFound.
	Track(ctx context.Context, id string) (Track, error)
	// ByISRC returns the provider's track for a recording, or ErrNotFound.
	ByISRC(ctx context.Context, isrc string) (Track, error)
}

// ParseRef splits a "provider:id" reference.
func ParseRef(ref string) (provider, id string, ok bool) {
	provider, id, ok = strings.Cut(ref, ":")
	return provider, id, ok && provider != "" && id != "" && len(ref) <= 200
}

// getJSON performs req and decodes a 200 response into out.
func getJSON(client *http.Client, req *http.Request, name string, out any) error {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return &StatusError{Provider: name, Status: resp.StatusCode, Body: strings.TrimSpace(string(body)),
			RetryAfter: resp.Header.Get("Retry-After")}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// StatusError is an unexpected HTTP status from a provider.
type StatusError struct {
	Provider   string
	Status     int
	Body       string
	RetryAfter string
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("%s: HTTP %d", e.Provider, e.Status)
	if e.RetryAfter != "" {
		msg += " (retry after " + e.RetryAfter + "s)"
	}
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// year returns the year of a "2020-03-20" or "2020" date, or 0.
func year(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(date[:4])
	return y
}
