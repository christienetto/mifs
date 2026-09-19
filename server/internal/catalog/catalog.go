// Package catalog is the song database: canonical songs, the provider tracks linked to
// them, synced lyrics, waveforms and mifs in SQLite, with FTS5 full-text search over
// titles, artists, albums and lyrics.
//
// A song's ID is MIFS's own and never derives from a provider: provider tracks (Spotify,
// Deezer, …) are links to a song, matched by provider ID, then ISRC, then title, artist and
// duration (see EnsureSong). A song exists before its audio does; Status says how far
// ingestion has got.
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

// Status is how far a song's ingestion has got.
type Status string

const (
	// StatusPending: queued for ingestion, or waiting to retry after a transient failure.
	StatusPending Status = "pending"
	// StatusProcessing: a worker is fetching and processing the audio.
	StatusProcessing Status = "processing"
	// StatusReady: audio and waveform are in place (lyrics too, when a provider had them).
	StatusReady Status = "ready"
	// StatusUnavailable: no audio source could supply the song. Retried when requested again.
	StatusUnavailable Status = "unavailable"
	// StatusFailed: ingestion kept failing. Retried when requested again.
	StatusFailed Status = "failed"
)

// Song is a catalog entry. Times are in milliseconds.
type Song struct {
	DownloadedBytes    int64
	DownloadTotalBytes int64
	ID                 string
	ISRC               string // empty when unknown
	Status             Status // Put treats empty as StatusReady
	StatusDetail       string // why the song isn't ready; empty when it is
	Attempts           int    // ingestion attempts so far
	Title              string
	Artist             string
	Album              string // empty for singles
	TrackNumber        int    // 0 when unknown
	ReleaseYear        int    // 0 when unknown
	Genre              string
	Explicit           bool
	DurationMs         int64
	HighlightStartMs   int64  // suggested snippet start; -1 when unknown
	AudioKey           string // empty until the song is ready
	AudioSource        string // what supplied the audio: "manual", "library", "command", …
	AudioContentType   string
	AudioBitrate       int
	AudioBytes         int64
	ArtworkKey         string // empty when the song has no artwork
	ThumbnailKey       string
	ArtworkURL         string // a provider's artwork, shown until ArtworkKey is set
	LicenseName        string
	LicenseURL         string
	Attribution        string
	HasLyrics          bool
	UpdatedAt          time.Time
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
	// _txlock=immediate takes the write lock when a transaction begins, so concurrent
	// read-then-write transactions queue on busy_timeout instead of failing to upgrade.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	if err := store.backfillMatchKeys(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Ready reports whether the song can be played and clipped.
func (song Song) Ready() bool { return song.Status == StatusReady && song.AudioKey != "" }

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
`, `
-- Songs can exist before their audio: audio_key stays '' until status is 'ready'.
-- Songs imported before this migration are ready and were imported by hand.
ALTER TABLE songs ADD COLUMN isrc            TEXT NOT NULL DEFAULT '';
ALTER TABLE songs ADD COLUMN match_key       TEXT NOT NULL DEFAULT ''; -- normalised title|artist
ALTER TABLE songs ADD COLUMN status          TEXT NOT NULL DEFAULT 'ready';
ALTER TABLE songs ADD COLUMN status_detail   TEXT NOT NULL DEFAULT '';
ALTER TABLE songs ADD COLUMN attempts        INTEGER NOT NULL DEFAULT 0;
ALTER TABLE songs ADD COLUMN next_attempt_at TEXT NOT NULL DEFAULT '';
ALTER TABLE songs ADD COLUMN audio_source    TEXT NOT NULL DEFAULT '';
ALTER TABLE songs ADD COLUMN artwork_url     TEXT NOT NULL DEFAULT '';
UPDATE songs SET audio_source = 'manual';
CREATE UNIQUE INDEX songs_by_isrc ON songs (isrc) WHERE isrc <> '';
CREATE INDEX songs_by_match_key ON songs (match_key) WHERE match_key <> '';
CREATE INDEX songs_queue ON songs (next_attempt_at) WHERE status = 'pending';

-- A provider's track, linked to the canonical song it is a copy of.
CREATE TABLE provider_tracks (
	provider    TEXT NOT NULL, -- 'spotify', 'deezer', …
	provider_id TEXT NOT NULL,
	song_id     TEXT NOT NULL REFERENCES songs (id) ON DELETE CASCADE,
	isrc        TEXT NOT NULL DEFAULT '',
	title       TEXT NOT NULL,
	artist      TEXT NOT NULL,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	url         TEXT NOT NULL DEFAULT '', -- the track's page on the provider
	linked_at   TEXT NOT NULL,
	PRIMARY KEY (provider, provider_id)
);
CREATE INDEX provider_tracks_by_song ON provider_tracks (song_id);

-- A shared moment of a song. Immutable: it pins the audio it was cut from and the lyrics
-- heard, so it plays the same forever even if the song is re-ingested. No cascade: a
-- song with mifs can't be deleted out from under them.
CREATE TABLE mifs (
	id          TEXT PRIMARY KEY,
	song_id     TEXT NOT NULL REFERENCES songs (id),
	audio_key   TEXT NOT NULL,
	start_ms    INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL,
	lyrics      TEXT NOT NULL DEFAULT '[]', -- JSON array of LyricLine, in song time
	created_at  TEXT NOT NULL
);
CREATE INDEX mifs_by_song ON mifs (song_id);
`, `
ALTER TABLE songs ADD COLUMN downloaded_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE songs ADD COLUMN download_total_bytes INTEGER NOT NULL DEFAULT 0;
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
	s.isrc, s.status, s.status_detail, s.attempts, s.audio_source, s.artwork_url, s.downloaded_bytes, s.download_total_bytes,
	EXISTS (SELECT 1 FROM lyrics l WHERE l.song_id = s.id)`

func scanSong(row interface{ Scan(...any) error }) (Song, error) {
	var song Song
	var updated string
	err := row.Scan(&song.ID, &song.Title, &song.Artist, &song.Album, &song.TrackNumber, &song.ReleaseYear,
		&song.Genre, &song.Explicit, &song.DurationMs, &song.HighlightStartMs, &song.AudioKey,
		&song.AudioContentType, &song.AudioBitrate, &song.AudioBytes, &song.ArtworkKey, &song.ThumbnailKey,
		&song.LicenseName, &song.LicenseURL, &song.Attribution, &updated,
		&song.ISRC, &song.Status, &song.StatusDetail, &song.Attempts, &song.AudioSource, &song.ArtworkURL, &song.DownloadedBytes, &song.DownloadTotalBytes,
		&song.HasLyrics)
	if err != nil {
		return Song{}, err
	}
	song.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return song, nil
}

// Songs lists ready songs ordered by title, or — when query is non-empty — ready songs
// matching every word of query (prefix match on title, artist, album and lyrics), best
// matches first.
func (s *Store) Songs(ctx context.Context, query string, limit, offset int) ([]Song, error) {
	return s.songs(ctx, query, true, limit, offset)
}

// Search is Songs over every song, whatever its status: songs being ingested (or that
// couldn't be) show up too. query must not be empty.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Song, error) {
	if ftsQuery(query) == "" {
		return []Song{}, nil
	}
	return s.songs(ctx, query, false, limit, 0)
}

func (s *Store) songs(ctx context.Context, query string, readyOnly bool, limit, offset int) ([]Song, error) {
	filter := ""
	if readyOnly {
		filter = "AND s.status = 'ready'"
	}
	var rows *sql.Rows
	var err error
	if match := ftsQuery(query); match != "" {
		// bm25 weights follow the column order: song_id, title, artist, album, lyrics.
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+songColumns+`
			FROM songs_fts f JOIN songs s ON s.id = f.song_id
			WHERE songs_fts MATCH ? `+filter+`
			ORDER BY bm25(songs_fts, 0, 10, 6, 3, 1), s.title COLLATE NOCASE, s.id
			LIMIT ? OFFSET ?`, match, limit, offset)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+songColumns+` FROM songs s
			WHERE 1 `+filter+`
			ORDER BY s.title COLLATE NOCASE, s.id
			LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	return collectSongs(rows)
}

func collectSongs(rows *sql.Rows) ([]Song, error) {
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

// Count returns the number of ready songs.
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM songs WHERE status = 'ready'").Scan(&n)
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
// It is how a song becomes ready: an empty Status means StatusReady.
func (s *Store) Put(ctx context.Context, record Record) error {
	song := record.Song
	now := timestamp(time.Now())
	if song.Status == "" {
		song.Status = StatusReady
	}
	song.ISRC = NormalizeISRC(song.ISRC)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO songs (id, title, artist, album, track_number, release_year, genre, explicit, duration_ms,
			highlight_start_ms, audio_key, audio_content_type, audio_bitrate, audio_bytes, artwork_key,
			thumbnail_key, license_name, license_url, attribution, created_at, updated_at,
			isrc, match_key, status, status_detail, audio_source, artwork_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			title = excluded.title, artist = excluded.artist, album = excluded.album,
			track_number = excluded.track_number, release_year = excluded.release_year, genre = excluded.genre,
			explicit = excluded.explicit, duration_ms = excluded.duration_ms,
			highlight_start_ms = excluded.highlight_start_ms, audio_key = excluded.audio_key,
			audio_content_type = excluded.audio_content_type, audio_bitrate = excluded.audio_bitrate,
			audio_bytes = excluded.audio_bytes, artwork_key = excluded.artwork_key,
			thumbnail_key = excluded.thumbnail_key, license_name = excluded.license_name,
			license_url = excluded.license_url, attribution = excluded.attribution,
			updated_at = excluded.updated_at, isrc = excluded.isrc, match_key = excluded.match_key,
			status = excluded.status, status_detail = excluded.status_detail,
			audio_source = excluded.audio_source, artwork_url = excluded.artwork_url`,
		song.ID, song.Title, song.Artist, song.Album, song.TrackNumber, song.ReleaseYear, song.Genre, song.Explicit,
		song.DurationMs, song.HighlightStartMs, song.AudioKey, song.AudioContentType, song.AudioBitrate,
		song.AudioBytes, song.ArtworkKey, song.ThumbnailKey, song.LicenseName, song.LicenseURL, song.Attribution,
		now, now, song.ISRC, MatchKey(song.Title, song.Artist), song.Status, song.StatusDetail, song.AudioSource,
		song.ArtworkURL)
	if isUniqueViolation(err) {
		return fmt.Errorf("song %s: ISRC %s already belongs to another song", song.ID, song.ISRC)
	}
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

	if err := indexSong(ctx, tx, song, strings.Join(lyricText, "\n")); err != nil {
		return err
	}
	return tx.Commit()
}

// indexSong replaces the song's search entry.
func indexSong(ctx context.Context, tx *sql.Tx, song Song, lyrics string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM songs_fts WHERE song_id = ?", song.ID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO songs_fts (song_id, title, artist, album, lyrics) VALUES (?, ?, ?, ?, ?)",
		song.ID, song.Title, song.Artist, song.Album, lyrics)
	return err
}

// backfillMatchKeys computes match keys for songs imported before they existed.
func (s *Store) backfillMatchKeys(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id, title, artist FROM songs WHERE match_key = ''")
	if err != nil {
		return err
	}
	keys := map[string]string{}
	for rows.Next() {
		var id, title, artist string
		if err := rows.Scan(&id, &title, &artist); err != nil {
			rows.Close()
			return err
		}
		keys[id] = MatchKey(title, artist)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for id, key := range keys {
		if _, err := s.db.ExecContext(ctx, "UPDATE songs SET match_key = ? WHERE id = ?", key, id); err != nil {
			return err
		}
	}
	return nil
}

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// PutPendingLyrics exposes metadata lyrics while the full audio is being prepared.
func (s *Store) PutPendingLyrics(ctx context.Context, id string, lines []LyricLine) error {
	if len(lines) == 0 {
		return nil
	}
	data, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO lyrics (song_id, lines) VALUES (?, ?) ON CONFLICT(song_id) DO UPDATE SET lines=excluded.lines", id, string(data))
	return err
}

func (s *Store) SetDownloadProgress(ctx context.Context, id string, downloaded, total int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE songs SET downloaded_bytes=?, download_total_bytes=? WHERE id=? AND status='processing'", max(0, downloaded), max(0, total), id)
	return err
}
