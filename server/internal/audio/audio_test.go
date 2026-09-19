package audio

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	ctx := context.Background()
	req := Request{SongID: "s1", ISRC: "USUG11904206", Title: "It's \"quoted\"; rm -rf /", Artist: "A", DurationMs: 1234,
		Refs: []string{"deezer:908604612", "spotify:abc"}}

	// Metadata arrives in the environment, never in the script, so it can't inject commands.
	found := &Command{Script: `printf '%s|%s|%s|%s' "$MIFS_TITLE" "$MIFS_ISRC" "$MIFS_REF_DEEZER" "$MIFS_DURATION_MS" > "$MIFS_OUT_DIR/song.mp3"; touch "$MIFS_OUT_DIR/artwork.jpg"`}
	result, err := found.Fetch(ctx, req, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(result.Path)
	if string(written) != req.Title+"|USUG11904206|908604612|1234" || filepath.Base(result.Artwork) != "artwork.jpg" {
		t.Errorf("result = %+v, file %q", result, written)
	}

	if _, err := (&Command{Script: "true"}).Fetch(ctx, req, t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Errorf("no file: err = %v", err)
	}
	_, err = (&Command{Script: "echo nope >&2; exit 3"}).Fetch(ctx, req, t.TempDir())
	if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "nope") {
		t.Errorf("failure: err = %v", err)
	}
}

func TestLibrary(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	dir := t.TempDir()
	tone := func(name string, seconds string, metadata ...string) {
		args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=duration=" + seconds}
		for _, m := range metadata {
			args = append(args, "-metadata", m)
		}
		if out, err := exec.Command("ffmpeg", append(args, filepath.Join(dir, name))...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg: %v: %s", err, out)
		}
	}
	tone("tagged.flac", "3", "title=Tagged Song", "artist=Some Artist", "ISRC=USUG11904206")
	tone("Other Artist - Named Song.wav", "4")
	tone("short.flac", "1", "title=Named Song", "artist=Other Artist")

	index := filepath.Join(t.TempDir(), "index.json")
	library := &Library{Dir: dir, FFprobe: "ffprobe", IndexPath: index}
	ctx := context.Background()
	cases := []struct {
		req  Request
		want string
	}{
		{Request{ISRC: "USUG11904206", Title: "Different Title", Artist: "X", DurationMs: 3000}, "tagged.flac"},
		{Request{Title: "Tagged Song", Artist: "Some Artist, Guest", DurationMs: 3500}, "tagged.flac"},
		{Request{Title: "Named Song", Artist: "Other Artist", DurationMs: 4200}, "Other Artist - Named Song.wav"},
		{Request{Title: "Named Song", Artist: "Other Artist", DurationMs: 1000}, "short.flac"},
		{Request{Title: "Tagged Song", Artist: "Some Artist", ISRC: "GBAYE0601498", DurationMs: 3000}, ""},
		{Request{Title: "Named Song", Artist: "Other Artist", DurationMs: 60_000}, ""},
	}
	for _, c := range cases {
		result, err := library.Fetch(ctx, c.req, t.TempDir())
		if c.want == "" {
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("%+v: got %v, %v", c.req, result.Path, err)
			}
			continue
		}
		if err != nil || filepath.Base(result.Path) != c.want {
			t.Errorf("%+v: got %q, %v; want %s", c.req, result.Path, err, c.want)
		}
	}

	// A restart reuses the index instead of probing again.
	if _, err := os.Stat(index); err != nil {
		t.Fatal(err)
	}
	restarted := &Library{Dir: dir, FFprobe: "/nonexistent/ffprobe", IndexPath: index}
	if result, err := restarted.Fetch(ctx, cases[0].req, t.TempDir()); err != nil || filepath.Base(result.Path) != "tagged.flac" {
		t.Errorf("from index: %+v, %v", result, err)
	}
}
