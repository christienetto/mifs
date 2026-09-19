package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// ProviderTrack is a discovery provider's copy of a song.
type ProviderTrack struct {
	Provider    string // "spotify", "deezer", …
	ID          string // the provider's track ID
	ISRC        string
	Title       string
	Artist      string
	Album       string
	DurationMs  int64
	Explicit    bool
	TrackNumber int
	ReleaseYear int
	URL         string // the track's page on the provider
	ArtworkURL  string
}

// Ref is the track's provider-qualified ID, e.g. "deezer:908604612".
func (t ProviderTrack) Ref() string { return t.Provider + ":" + t.ID }

// MatchToleranceMs is how far apart two durations may be for tracks without matching
// ISRCs to count as the same recording.
const MatchToleranceMs = 3000

// EnsureSong returns the canonical song that track is a copy of, linking track to it.
// Songs are matched by provider ID, then ISRC, then normalised title and artist with a
// duration within MatchToleranceMs. When nothing matches it creates a pending song,
// ready for ingestion, and reports created.
func (s *Store) EnsureSong(ctx context.Context, track ProviderTrack) (song Song, created bool, err error) {
	track.ISRC = NormalizeISRC(track.ISRC)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Song{}, false, err
	}
	defer tx.Rollback()

	var id string
	err = tx.QueryRowContext(ctx, "SELECT song_id FROM provider_tracks WHERE provider = ? AND provider_id = ?",
		track.Provider, track.ID).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Song{}, false, err
	}
	if id == "" && track.ISRC != "" {
		err = tx.QueryRowContext(ctx, "SELECT id FROM songs WHERE isrc = ?", track.ISRC).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Song{}, false, err
		}
	}
	if id == "" {
		if id, err = matchByKey(ctx, tx, track); err != nil {
			return Song{}, false, err
		}
	}

	now := timestamp(time.Now())
	if id == "" {
		id, created = newID(), true
		_, err = tx.ExecContext(ctx, `
			INSERT INTO songs (id, title, artist, album, track_number, release_year, explicit, duration_ms,
				audio_key, audio_content_type, isrc, match_key, status, next_attempt_at, artwork_url, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, 'pending', ?, ?, ?, ?)`,
			id, track.Title, track.Artist, track.Album, track.TrackNumber, track.ReleaseYear, track.Explicit,
			track.DurationMs, track.ISRC, MatchKey(track.Title, track.Artist), now, track.ArtworkURL, now, now)
		if err != nil {
			return Song{}, false, err
		}
		pending := Song{ID: id, Title: track.Title, Artist: track.Artist, Album: track.Album}
		if err := indexSong(ctx, tx, pending, ""); err != nil {
			return Song{}, false, err
		}
	} else if track.ISRC != "" {
		// Learn the ISRC of a song matched by name (nothing else has it: we looked it up).
		if _, err := tx.ExecContext(ctx, "UPDATE songs SET isrc = ? WHERE id = ? AND isrc = ''", track.ISRC, id); err != nil {
			return Song{}, false, err
		}
	}
	if err := linkTrack(ctx, tx, id, track, now); err != nil {
		return Song{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Song{}, false, err
	}
	song, err = s.Song(ctx, id)
	return song, created, err
}

// matchByKey finds a song with the same normalised title and artist and a similar
// duration. Tracks whose ISRCs are both known and differ are different recordings.
func matchByKey(ctx context.Context, tx *sql.Tx, track ProviderTrack) (string, error) {
	key := MatchKey(track.Title, track.Artist)
	if key == "|" {
		return "", nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT id, duration_ms, isrc FROM songs WHERE match_key = ? ORDER BY created_at, id", key)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, isrc string
		var duration int64
		if err := rows.Scan(&id, &duration, &isrc); err != nil {
			return "", err
		}
		if track.ISRC != "" && isrc != "" && track.ISRC != isrc {
			continue
		}
		if track.DurationMs > 0 && duration > 0 && abs(track.DurationMs-duration) > MatchToleranceMs {
			continue
		}
		return id, nil
	}
	return "", rows.Err()
}

// LinkTrack records that track is a copy of song id. A track already linked elsewhere
// keeps its first link.
func (s *Store) LinkTrack(ctx context.Context, id string, track ProviderTrack) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := linkTrack(ctx, tx, id, track, timestamp(time.Now())); err != nil {
		return err
	}
	return tx.Commit()
}

func linkTrack(ctx context.Context, tx *sql.Tx, id string, track ProviderTrack, now string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO provider_tracks (provider, provider_id, song_id, isrc, title, artist, duration_ms, url, linked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (provider, provider_id) DO NOTHING`,
		track.Provider, track.ID, id, NormalizeISRC(track.ISRC), track.Title, track.Artist, track.DurationMs, track.URL, now)
	return err
}

// Links returns the provider tracks linked to a song, by provider name.
func (s *Store) Links(ctx context.Context, id string) ([]ProviderTrack, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, provider_id, isrc, title, artist, duration_ms, url
		FROM provider_tracks WHERE song_id = ? ORDER BY provider, linked_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var links []ProviderTrack
	for rows.Next() {
		var t ProviderTrack
		if err := rows.Scan(&t.Provider, &t.ID, &t.ISRC, &t.Title, &t.Artist, &t.DurationMs, &t.URL); err != nil {
			return nil, err
		}
		links = append(links, t)
	}
	return links, rows.Err()
}

// SongsForTracks finds the songs MIFS already has for provider tracks, by link or ISRC,
// keyed by the track's Ref.
func (s *Store) SongsForTracks(ctx context.Context, tracks []ProviderTrack) (map[string]Song, error) {
	idByRef := map[string]string{}
	idByISRC := map[string]string{}
	var pairs, isrcs []string
	var pairArgs, isrcArgs []any
	for _, t := range tracks {
		pairs = append(pairs, "(?, ?)")
		pairArgs = append(pairArgs, t.Provider, t.ID)
		if isrc := NormalizeISRC(t.ISRC); isrc != "" {
			isrcs = append(isrcs, "?")
			isrcArgs = append(isrcArgs, isrc)
		}
	}
	lookups := []struct {
		query string
		args  []any
		into  map[string]string
	}{
		{`SELECT provider || ':' || provider_id, song_id FROM provider_tracks
			WHERE (provider, provider_id) IN (VALUES ` + strings.Join(pairs, ", ") + `)`, pairArgs, idByRef},
		{`SELECT isrc, id FROM songs WHERE isrc IN (` + strings.Join(isrcs, ", ") + `)`, isrcArgs, idByISRC},
	}
	for _, lookup := range lookups {
		if len(lookup.args) == 0 {
			continue
		}
		if err := s.scanPairs(ctx, lookup.into, lookup.query, lookup.args...); err != nil {
			return nil, err
		}
	}

	idFor := func(t ProviderTrack) string {
		if id, ok := idByRef[t.Ref()]; ok {
			return id
		}
		return idByISRC[NormalizeISRC(t.ISRC)]
	}
	ids := map[string]bool{}
	for _, t := range tracks {
		if id := idFor(t); id != "" {
			ids[id] = true
		}
	}
	songs, err := s.songsByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	found := map[string]Song{}
	for _, t := range tracks {
		if song, ok := songs[idFor(t)]; ok {
			found[t.Ref()] = song
		}
	}
	return found, nil
}

func (s *Store) scanPairs(ctx context.Context, into map[string]string, query string, args ...any) error {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		into[key] = value
	}
	return rows.Err()
}

func (s *Store) songsByID(ctx context.Context, ids map[string]bool) (map[string]Song, error) {
	out := map[string]Song{}
	if len(ids) == 0 {
		return out, nil
	}
	var marks []string
	var args []any
	for id := range ids {
		marks = append(marks, "?")
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+songColumns+` FROM songs s WHERE s.id IN (`+strings.Join(marks, ", ")+`)`, args...)
	if err != nil {
		return nil, err
	}
	songs, err := collectSongs(rows)
	for _, song := range songs {
		out[song.ID] = song
	}
	return out, err
}

// Ingestion queue. A song's row is its job: status 'pending' with next_attempt_at in the
// past means "process me". Workers claim songs one at a time.

// Claim marks the next due pending song as processing and returns it.
func (s *Store) Claim(ctx context.Context, now time.Time) (Song, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Song{}, false, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM songs WHERE status = 'pending' AND next_attempt_at <= ?
		ORDER BY next_attempt_at, id LIMIT 1`, timestamp(now)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Song{}, false, nil
	}
	if err != nil {
		return Song{}, false, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE songs SET status = 'processing', downloaded_bytes = 0, download_total_bytes = 0, attempts = attempts + 1, updated_at = ? WHERE id = ?",
		timestamp(now), id)
	if err != nil {
		return Song{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Song{}, false, err
	}
	song, err := s.Song(ctx, id)
	return song, err == nil, err
}

// SetStatus records the outcome of an attempt that didn't make the song ready. A pending
// song is retried at retryAt; unavailable and failed songs may be requeued after it.
func (s *Store) SetStatus(ctx context.Context, id string, status Status, detail string, retryAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE songs SET status = ?, status_detail = ?, next_attempt_at = ?, updated_at = ? WHERE id = ?`,
		status, detail, timestamp(retryAt), timestamp(time.Now()), id)
	return err
}

// Requeue queues an unavailable or failed song again, if its retry time has come, and
// reports whether it did.
func (s *Store) Requeue(ctx context.Context, id string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE songs SET status = 'pending', attempts = 0, next_attempt_at = ?, updated_at = ?
		WHERE id = ? AND status IN ('unavailable', 'failed') AND next_attempt_at <= ?`,
		timestamp(now), timestamp(now), id, timestamp(now))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// ResetProcessing returns songs a stopped worker was processing to the queue.
func (s *Store) ResetProcessing(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE songs SET status = 'pending' WHERE status = 'processing'")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Identity helpers.

// NormalizeISRC returns isrc as 12 upper-case characters, or "" when it isn't one.
func NormalizeISRC(isrc string) string {
	isrc = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(isrc)))
	if len(isrc) != 12 {
		return ""
	}
	for i, r := range isrc {
		letter, digit := r >= 'A' && r <= 'Z', r >= '0' && r <= '9'
		if !(letter || digit) || (i < 2 && !letter) || (i >= 7 && !digit) {
			return ""
		}
	}
	return isrc
}

// MatchKey is a song's normalised "title|artist": lower case, letters and digits only,
// without featured artists, and only the first credited artist.
func MatchKey(title, artist string) string {
	return normalizeName(stripFeaturing(title)) + "|" + normalizeName(primaryArtist(artist))
}

// featuring matches a credit for a featured artist: "(feat. X)", "[ft. X]", "(with X)",
// or a trailing " feat. X".
var featuring = regexp.MustCompile(`(?i)\s*[(\[](?:feat\.?|ft\.|featuring|with)\s[^)\]]*[)\]]|\s+(?:feat\.|ft\.|featuring)\s.*$`)

func stripFeaturing(title string) string { return featuring.ReplaceAllString(title, "") }

func primaryArtist(artist string) string {
	lower := strings.ToLower(artist)
	cut := len(artist)
	for _, sep := range []string{",", " & ", " feat", " ft.", " featuring ", ";", " / "} {
		if i := strings.Index(lower, sep); i > 0 && i < cut {
			cut = i
		}
	}
	return artist[:cut]
}

func normalizeName(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		case r == '\'' || r == '’':
			// "don't" and "dont" match
		default:
			space = true
		}
	}
	return b.String()
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns a random song ID: 12 characters, 60 bits.
func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return idEncoding.EncodeToString(b[:])[:12]
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
