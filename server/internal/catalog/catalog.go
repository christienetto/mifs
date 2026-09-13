// Package catalog is the song database: metadata, synced lyrics and waveforms in SQLite,
// with FTS5 full-text search over titles, artists, albums and lyrics.
//
// Media bytes live in the blob store; rows only reference blob keys.
package catalog

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a song (or its lyrics/waveform) doesn't exist.
var ErrNotFound = errors.New("not found")

// Song is a catalog entry. Times are in milliseconds.
type Song struct {
	ID               string
	Title            string
	Artist           string
	Album            string // empty for singles
	TrackNumber      int    // 0 when unknown
	ReleaseYear      int    // 0 when unknown
	Genre            string
	Explicit         bool
	DurationMs       int64
	HighlightStartMs int64 // suggested snippet start; -1 when unknown
	AudioKey         string
	AudioContentType string
	AudioBitrate     int
	AudioBytes       int64
	ArtworkKey       string // empty when the song has no artwork
	ThumbnailKey     string
	LicenseName      string
	LicenseURL       string
	Attribution      string
	HasLyrics        bool
	UpdatedAt        time.Time
}

// LyricLine is one synced lyric line.
type LyricLine struct {
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Text    string `json:"text"`
}

// Waveform is the loudness envelope used to draw and scrub the timeline.
type Waveform struct {
	PointsPerSecond int
	RMS             []float32
}

// Record is everything ingest writes for one song.
type Record struct {
	Song     Song
	Lyrics   []LyricLine // nil when the song has no lyrics
	Waveform Waveform
}

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating and migrating if needed) the database at path.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrations[i] upgrades the schema from user_version i to i+1. Append only.
var migrations = []string{`
CREATE TABLE songs (
	id                 TEXT PRIMARY KEY,
	title              TEXT NOT NULL,
	artist             TEXT NOT NULL,
	album              TEXT NOT NULL DEFAULT '',
	track_number       INTEGER NOT NULL DEFAULT 0,
	release_year       INTEGER NOT NULL DEFAULT 0,
	genre              TEXT NOT NULL DEFAULT '',
	explicit           INTEGER NOT NULL DEFAULT 0,
	duration_ms        INTEGER NOT NULL,
	highlight_start_ms INTEGER NOT NULL DEFAULT -1,
	audio_key          TEXT NOT NULL,
	audio_content_type TEXT NOT NULL,
	audio_bitrate      INTEGER NOT NULL DEFAULT 0,
	audio_bytes        INTEGER NOT NULL DEFAULT 0,
	artwork_key        TEXT NOT NULL DEFAULT '',
	thumbnail_key      TEXT NOT NULL DEFAULT '',
	license_name       TEXT NOT NULL DEFAULT '',
	license_url        TEXT NOT NULL DEFAULT '',
	attribution        TEXT NOT NULL DEFAULT '',
	created_at         TEXT NOT NULL,
	updated_at         TEXT NOT NULL
);
CREATE INDEX songs_by_title ON songs (title COLLATE NOCASE, id);

CREATE TABLE lyrics (
	song_id TEXT PRIMARY KEY REFERENCES songs (id) ON DELETE CASCADE,
	lines   TEXT NOT NULL -- JSON array of LyricLine
);

CREATE TABLE waveforms (
	song_id           TEXT PRIMARY KEY REFERENCES songs (id) ON DELETE CASCADE,
	points_per_second INTEGER NOT NULL,
	rms               BLOB NOT NULL -- little-endian float32
);

CREATE VIRTUAL TABLE songs_fts USING fts5 (
	song_id UNINDEXED, title, artist, album, lyrics,
	tokenize = 'unicode61 remove_diacritics 2'
);
`}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for ; version < len(migrations); version++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[version]); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const songColumns = `s.id, s.title, s.artist, s.album, s.track_number, s.release_year, s.genre, s.explicit,
	s.duration_ms, s.highlight_start_ms, s.audio_key, s.audio_content_type, s.audio_bitrate, s.audio_bytes,
	s.artwork_key, s.thumbnail_key, s.license_name, s.license_url, s.attribution, s.updated_at,
	EXISTS (SELECT 1 FROM lyrics l WHERE l.song_id = s.id)`

func scanSong(row interface{ Scan(...any) error }) (Song, error) {
	var song Song
	var updated string
	err := row.Scan(&song.ID, &song.Title, &song.Artist, &song.Album, &song.TrackNumber, &song.ReleaseYear,
		&song.Genre, &song.Explicit, &song.DurationMs, &song.HighlightStartMs, &song.AudioKey,
		&song.AudioContentType, &song.AudioBitrate, &song.AudioBytes, &song.ArtworkKey, &song.ThumbnailKey,
		&song.LicenseName, &song.LicenseURL, &song.Attribution, &updated, &song.HasLyrics)
	if err != nil {
		return Song{}, err
	}
	song.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return song, nil
}

// Songs lists songs ordered by title, or — when query is non-empty — songs matching every
// word of query (prefix match on title, artist, album and lyrics), best matches first.
func (s *Store) Songs(ctx context.Context, query string, limit, offset int) ([]Song, error) {
	var rows *sql.Rows
	var err error
	if match := ftsQuery(query); match != "" {
		// bm25 weights follow the column order: song_id, title, artist, album, lyrics.
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+songColumns+`
			FROM songs_fts f JOIN songs s ON s.id = f.song_id
			WHERE songs_fts MATCH ?
			ORDER BY bm25(songs_fts, 0, 10, 6, 3, 1), s.title COLLATE NOCASE, s.id
			LIMIT ? OFFSET ?`, match, limit, offset)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+songColumns+` FROM songs s
			ORDER BY s.title COLLATE NOCASE, s.id
			LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	songs := []Song{}
	for rows.Next() {
		song, err := scanSong(rows)
		if err != nil {
			return nil, err
		}
		songs = append(songs, song)
	}
	return songs, rows.Err()
}

// ftsQuery turns free text into an FTS5 query where every word is a quoted prefix term,
// so user input can never be interpreted as FTS syntax.
func ftsQuery(query string) string {
	words := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	if len(words) > 8 {
		words = words[:8]
	}
	terms := make([]string, len(words))
	for i, word := range words {
		terms[i] = `"` + word + `"*`
	}
	return strings.Join(terms, " ")
}

// Song returns one song by ID.
func (s *Store) Song(ctx context.Context, id string) (Song, error) {
	song, err := scanSong(s.db.QueryRowContext(ctx, `SELECT `+songColumns+` FROM songs s WHERE s.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Song{}, ErrNotFound
	}
	return song, err
}

// Count returns the number of songs.
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM songs").Scan(&n)
	return n, err
}

// Lyrics returns a song's synced lyrics.
func (s *Store) Lyrics(ctx context.Context, id string) ([]LyricLine, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT lines FROM lyrics WHERE song_id = ?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var lines []LyricLine
	return lines, json.Unmarshal([]byte(raw), &lines)
}

// Waveform returns a song's loudness envelope.
func (s *Store) Waveform(ctx context.Context, id string) (Waveform, error) {
	var waveform Waveform
	var raw []byte
	err := s.db.QueryRowContext(ctx, "SELECT points_per_second, rms FROM waveforms WHERE song_id = ?", id).
		Scan(&waveform.PointsPerSecond, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Waveform{}, ErrNotFound
	}
	if err != nil {
		return Waveform{}, err
	}
	waveform.RMS = make([]float32, len(raw)/4)
	for i := range waveform.RMS {
		waveform.RMS[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return waveform, nil
}

// Put inserts or replaces a song with its lyrics, waveform and search entry, atomically.
func (s *Store) Put(ctx context.Context, record Record) error {
	song := record.Song
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO songs (id, title, artist, album, track_number, release_year, genre, explicit, duration_ms,
			highlight_start_ms, audio_key, audio_content_type, audio_bitrate, audio_bytes, artwork_key,
			thumbnail_key, license_name, license_url, attribution, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			title = excluded.title, artist = excluded.artist, album = excluded.album,
			track_number = excluded.track_number, release_year = excluded.release_year, genre = excluded.genre,
			explicit = excluded.explicit, duration_ms = excluded.duration_ms,
			highlight_start_ms = excluded.highlight_start_ms, audio_key = excluded.audio_key,
			audio_content_type = excluded.audio_content_type, audio_bitrate = excluded.audio_bitrate,
			audio_bytes = excluded.audio_bytes, artwork_key = excluded.artwork_key,
			thumbnail_key = excluded.thumbnail_key, license_name = excluded.license_name,
			license_url = excluded.license_url, attribution = excluded.attribution,
			updated_at = excluded.updated_at`,
		song.ID, song.Title, song.Artist, song.Album, song.TrackNumber, song.ReleaseYear, song.Genre, song.Explicit,
		song.DurationMs, song.HighlightStartMs, song.AudioKey, song.AudioContentType, song.AudioBitrate,
		song.AudioBytes, song.ArtworkKey, song.ThumbnailKey, song.LicenseName, song.LicenseURL, song.Attribution,
		now, now)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM lyrics WHERE song_id = ?", song.ID); err != nil {
		return err
	}
	var lyricText []string
	if len(record.Lyrics) > 0 {
		lines, err := json.Marshal(record.Lyrics)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO lyrics (song_id, lines) VALUES (?, ?)", song.ID, string(lines)); err != nil {
			return err
		}
		for _, line := range record.Lyrics {
			lyricText = append(lyricText, line.Text)
		}
	}

	rms := make([]byte, 4*len(record.Waveform.RMS))
	for i, value := range record.Waveform.RMS {
		binary.LittleEndian.PutUint32(rms[i*4:], math.Float32bits(value))
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO waveforms (song_id, points_per_second, rms) VALUES (?, ?, ?)
		ON CONFLICT (song_id) DO UPDATE SET points_per_second = excluded.points_per_second, rms = excluded.rms`,
		song.ID, record.Waveform.PointsPerSecond, rms)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM songs_fts WHERE song_id = ?", song.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO songs_fts (song_id, title, artist, album, lyrics) VALUES (?, ?, ?, ?, ?)",
		song.ID, song.Title, song.Artist, song.Album, strings.Join(lyricText, "\n"))
	if err != nil {
		return err
	}
	return tx.Commit()
}
