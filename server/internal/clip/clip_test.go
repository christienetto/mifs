package clip

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/christienetto/mifs/server/internal/blob"
)

func TestMemoryOnlyPlayback(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	dir := t.TempDir()
	blobs, err := blob.Open(filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "song.m4a")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=20", "-c:a", "aac", src).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	audioKey, _, err := blobs.Put(src, "audio", "m4a")
	if err != nil {
		t.Fatal(err)
	}
	renderer := &Renderer{Blobs: blobs, FFmpeg: "ffmpeg"}
	ctx := context.Background()

	// Concurrent requests for one clip share a render and agree.
	var wg sync.WaitGroup
	clips := make([][]byte, 4)
	for i := range clips {
		wg.Go(func() {
			var err error
			clips[i], err = renderer.Bytes(ctx, audioKey, 5250, 10000)
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for _, data := range clips {
		if len(data) < 1000 || !bytes.Equal(data, clips[0]) {
			t.Fatal("concurrent playback differed")
		}
	}
	key := Key(audioKey, 5250, 10000)
	if _, exists := blobs.Exists(key); exists {
		t.Fatal("clip was stored on disk")
	}
	// Save response only in the test directory so ffprobe can inspect the delivered duration.
	responseFile := filepath.Join(dir, "response.m4a")
	if err := os.WriteFile(responseFile, clips[0], 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", responseFile).Output()
	if err != nil {
		t.Fatal(err)
	}
	if seconds, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); seconds < 9.95 || seconds > 10.1 {
		t.Errorf("moment is %v s long", seconds)
	}
	if Key(audioKey, 5_250, 5_000) == key {
		t.Error("different clips share a key")
	}
	if _, err := renderer.Bytes(ctx, "audio/00/"+strings.Repeat("0", 64)+".m4a", 0, 5_000); err == nil {
		t.Error("rendered from missing audio")
	}
}
