package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Mif is a shared moment of a song: the platform-independent object every share
// (iMessage, Telegram, WhatsApp, a web link) points at. It pins the exact audio it was
// cut from and the lyrics heard, so it plays the same even if the song is re-ingested.
type Mif struct {
	ID         string
	SongID     string
	AudioKey   string
	StartMs    int64
	DurationMs int64
	Lyrics     []LyricLine // lines heard, in song time
	CreatedAt  time.Time
}

// MifID derives a mif's ID from what it is, so asking twice for the same moment of the
// same audio yields the same mif (and the same cached clip).
func MifID(songID, audioKey string, startMs, durationMs int64) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d\x00%d", songID, audioKey, startMs, durationMs))
	return idEncoding.EncodeToString(sum[:])[:12]
}

// NewMif makes the mif for a moment of a ready song, with the lyric lines heard in it.
func NewMif(song Song, lines []LyricLine, startMs, durationMs int64) Mif {
	return Mif{
		ID:         MifID(song.ID, song.AudioKey, startMs, durationMs),
		SongID:     song.ID,
		AudioKey:   song.AudioKey,
		StartMs:    startMs,
		DurationMs: durationMs,
		Lyrics:     Heard(lines, startMs, startMs+durationMs),
	}
}

// Heard returns the lines heard during [startMs, endMs): at least half a second of them,
// or half of a shorter line. It matches the app's LyricLine.heard.
func Heard(lines []LyricLine, startMs, endMs int64) []LyricLine {
	heard := []LyricLine{}
	for _, line := range lines {
		overlap := min(endMs, line.EndMs) - max(startMs, line.StartMs)
		if overlap > 0 && overlap >= min(500, (line.EndMs-line.StartMs)/2) {
			heard = append(heard, line)
		}
	}
	return heard
}

// PutMif stores mif unless it exists, and returns the stored mif and whether it is new.
func (s *Store) PutMif(ctx context.Context, mif Mif) (Mif, bool, error) {
	lyrics, err := json.Marshal(mif.Lyrics)
	if err != nil {
		return Mif{}, false, err
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO mifs (id, song_id, audio_key, start_ms, duration_ms, lyrics, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING`,
		mif.ID, mif.SongID, mif.AudioKey, mif.StartMs, mif.DurationMs, string(lyrics), timestamp(time.Now()))
	if err != nil {
		return Mif{}, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Mif{}, false, err
	}
	stored, err := s.Mif(ctx, mif.ID)
	return stored, n > 0, err
}

// Mif returns a mif by ID.
func (s *Store) Mif(ctx context.Context, id string) (Mif, error) {
	var mif Mif
	var lyrics, created string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, song_id, audio_key, start_ms, duration_ms, lyrics, created_at FROM mifs WHERE id = ?`, id).
		Scan(&mif.ID, &mif.SongID, &mif.AudioKey, &mif.StartMs, &mif.DurationMs, &lyrics, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Mif{}, ErrNotFound
	}
	if err != nil {
		return Mif{}, err
	}
	mif.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return mif, json.Unmarshal([]byte(lyrics), &mif.Lyrics)
}
