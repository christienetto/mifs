// Package lyrics fetches line-synced lyrics from providers and normalises them into
// catalog.LyricLine: every line with an explicit start and end in milliseconds. Songs
// store only that form, so the provider can change without touching clients.
package lyrics

import (
	"context"
	"errors"
	"unicode"

	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/lrc"
)

// ErrNotFound means the provider has no synced lyrics for the song (or it's instrumental).
var ErrNotFound = errors.New("no synced lyrics")

// Query describes the recording to find lyrics for. DurationMs matters: synced lyrics
// are only right for a recording of the same length.
type Query struct {
	Title      string
	Artist     string
	Album      string
	ISRC       string
	DurationMs int64
}

// Provider is a source of synced lyrics.
type Provider interface {
	Name() string
	// Synced returns lyrics timed to a recording of q.DurationMs, or ErrNotFound.
	Synced(ctx context.Context, q Query) ([]catalog.LyricLine, error)
}

// MaxLastLineMs is how long a final line without an end marker is shown for at most.
const MaxLastLineMs = 8000

// Lines converts LRC entries into lines with explicit ends: the next entry's time, or for
// the final line a short hold. Entries without words (empty, or just "♪") only mark where
// the previous line ends. Lines starting at or after durationMs are dropped.
func Lines(entries []lrc.Line, durationMs int64) []catalog.LyricLine {
	var lines []catalog.LyricLine
	for i, entry := range entries {
		start := entry.Time.Milliseconds()
		if !hasWords(entry.Text) || start < 0 || start >= durationMs {
			continue
		}
		end := min(durationMs, start+MaxLastLineMs)
		if i+1 < len(entries) {
			end = min(durationMs, entries[i+1].Time.Milliseconds())
		}
		if end > start {
			lines = append(lines, catalog.LyricLine{StartMs: start, EndMs: end, Text: entry.Text})
		}
	}
	return lines
}

// Fit trims lines timed against one duration to audio of durationMs.
func Fit(lines []catalog.LyricLine, durationMs int64) []catalog.LyricLine {
	var fitted []catalog.LyricLine
	for _, line := range lines {
		if line.StartMs >= durationMs {
			continue
		}
		line.EndMs = min(line.EndMs, durationMs)
		fitted = append(fitted, line)
	}
	return fitted
}

func hasWords(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}
