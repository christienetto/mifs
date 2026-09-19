package audio

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Command is a Source that runs an external program, so any acquisition method (a
// licensed catalog's API, a script that copies from a NAS, …) plugs in without code
// changes.
//
// The program runs via `sh -c Script`. It receives the song only through environment
// variables — never interpolated into the script — and writes one audio file into
// $MIFS_OUT_DIR (plus, optionally, an image named artwork.*):
//
//	MIFS_OUT_DIR       where to write the file
//	MIFS_SONG_ID       the MIFS song ID
//	MIFS_ISRC          ISRC, when known
//	MIFS_TITLE, MIFS_ARTIST, MIFS_ALBUM
//	MIFS_DURATION_MS   expected duration
//	MIFS_REFS          space-separated provider refs ("spotify:… deezer:…")
//	MIFS_REF_<NAME>    each provider's ID, e.g. MIFS_REF_SPOTIFY
//
// Exit 0 with a file means found; exit 0 without one means not available; any other
// exit status is an error (retried later).
type Command struct {
	Script  string
	Timeout time.Duration // default 5 minutes
}

func (c *Command) Name() string { return "command" }

var audioExts = []string{".aac", ".aif", ".aiff", ".alac", ".flac", ".m4a", ".mp3", ".mp4", ".oga", ".ogg", ".opus", ".wav", ".webm"}

func (c *Command) Fetch(ctx context.Context, req Request, dir string) (Result, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out := filepath.Join(dir, "command")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return Result{}, err
	}
	// Runs in the server's directory, so a relative script path works.
	cmd := exec.CommandContext(ctx, "sh", "-c", c.Script)
	cmd.Env = append(os.Environ(),
		"MIFS_OUT_DIR="+out,
		"MIFS_SONG_ID="+req.SongID,
		"MIFS_ISRC="+req.ISRC,
		"MIFS_TITLE="+req.Title,
		"MIFS_ARTIST="+req.Artist,
		"MIFS_ALBUM="+req.Album,
		"MIFS_DURATION_MS="+strconv.FormatInt(req.DurationMs, 10),
		"MIFS_REFS="+strings.Join(req.Refs, " "),
	)
	for _, ref := range req.Refs {
		if provider, id, ok := strings.Cut(ref, ":"); ok {
			cmd.Env = append(cmd.Env, "MIFS_REF_"+strings.ToUpper(provider)+"="+id)
		}
	}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 500 {
			tail = "…" + tail[len(tail)-500:]
		}
		return Result{}, fmt.Errorf("audio command: %w: %s", err, tail)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		return Result{}, err
	}
	var result Result
	for _, entry := range entries {
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case entry.IsDir():
		case strings.HasPrefix(strings.ToLower(name), "artwork.") && slices.Contains([]string{".jpg", ".jpeg", ".png", ".webp"}, ext):
			result.Artwork = filepath.Join(out, name)
		case slices.Contains(audioExts, ext) && result.Path == "":
			result.Path = filepath.Join(out, name)
		}
	}
	if result.Path == "" {
		return Result{}, ErrNotFound
	}
	return result, nil
}
