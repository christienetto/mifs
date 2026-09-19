// Package audio is where a song's playable audio comes from.
//
// Discovery tells MIFS a song exists; a Source supplies its audio. The ingestion worker
// asks each configured source in turn and hands the first file it gets to the same
// pipeline as a hand-imported song (transcode, waveform, blob store), so nothing
// downstream — the API, mifs, lyrics, the apps — knows or cares which source it was.
//
// Only use sources whose audio you have the rights to store and redistribute: MIFS
// streams songs and sends clips of them to other people.
package audio

import (
	"context"
	"errors"
)

// ErrNotFound means the source can't supply this song. Any other error is treated as
// transient and retried.
var ErrNotFound = errors.New("not available from this source")

// Request describes the song whose audio is wanted.
type Request struct {
	Progress   func(downloaded, total int64)
	SongID     string
	ISRC       string
	Title      string
	Artist     string
	Album      string
	DurationMs int64    // expected length; the worker rejects audio that's far off
	Refs       []string // provider references, e.g. "spotify:0VjIjW4GlUZAMYd2vXMi3b"
}

// Result is audio a source produced.
type Result struct {
	// Path is the audio file, in any format ffmpeg reads.
	Path string
	// Artwork is an optional image file that came with the audio (e.g. embedded art).
	Artwork string
	// Where the audio comes from and on what terms, shown with the song.
	LicenseName string
	LicenseURL  string
	Attribution string
}

// Source supplies audio for songs.
type Source interface {
	Name() string
	// Fetch obtains the song's audio, writing any files it creates inside dir, which the
	// caller deletes afterwards. It returns ErrNotFound when the source doesn't have it.
	Fetch(ctx context.Context, req Request, dir string) (Result, error)
}
