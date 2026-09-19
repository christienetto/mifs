// Package clip serves timestamped intervals from one stored recording.
// Rendering happens only on playback, with a bounded memory cache and no clip files.
package clip

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/christienetto/mifs/server/internal/blob"
)

// Bump recipeVersion when the encoding below changes, so old clips aren't reused.
const recipeVersion = "v1"

// ContentType is the MIME type of rendered clips (AAC in MP4, i.e. .m4a).
const ContentType = "audio/mp4"

const bitrate = "192k"

type Renderer struct {
	Blobs      *blob.Store
	FFmpeg     string
	once       sync.Once
	slots      chan struct{}
	memoryMu   sync.Mutex
	memory     map[string][]byte
	memorySize int
	transient  map[string]*memoryFlight
}

// Key identifies a recipe in memory (and identifies legacy clip files for migration checks).
func Key(audioKey string, startMs, durationMs int64) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%d|%d|m4a", recipeVersion, audioKey, startMs, durationMs))
	return blob.Key("clips", hex.EncodeToString(sum[:]), "m4a")
}

// Memory-only, bounded cache avoids repeated encoding for popular moments and range requests.
// Sharing never invokes this: encoding happens only on playback.
type memoryFlight struct {
	done chan struct{}
	data []byte
	err  error
}

func (r *Renderer) Bytes(ctx context.Context, key string, start, duration int64) ([]byte, error) {
	recipe := Key(key, start, duration)
	r.memoryMu.Lock()
	if data := r.memory[recipe]; data != nil {
		r.memoryMu.Unlock()
		return data, nil
	}
	if r.transient == nil {
		r.transient = map[string]*memoryFlight{}
	}
	f := r.transient[recipe]
	if f == nil {
		f = &memoryFlight{done: make(chan struct{})}
		r.transient[recipe] = f
		go func() {
			renderCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			r.once.Do(func() { r.slots = make(chan struct{}, max(runtime.NumCPU()/2, 1)) })
			select {
			case r.slots <- struct{}{}:
				f.data, f.err = r.encodeMemory(renderCtx, key, start, duration)
				<-r.slots
			case <-renderCtx.Done():
				f.err = renderCtx.Err()
			}
			r.memoryMu.Lock()
			if f.err == nil {
				const budget = 32 * 1024 * 1024
				if r.memorySize+len(f.data) > budget {
					r.memory = nil
					r.memorySize = 0
				}
				if r.memory == nil {
					r.memory = map[string][]byte{}
				}
				if len(f.data) <= budget {
					r.memory[recipe] = f.data
					r.memorySize += len(f.data)
				}
			}
			delete(r.transient, recipe)
			close(f.done)
			r.memoryMu.Unlock()
		}()
	}
	r.memoryMu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.done:
		return f.data, f.err
	}
}
func (r *Renderer) encodeMemory(ctx context.Context, key string, start, duration int64) ([]byte, error) {
	if _, ok := r.Blobs.Exists(key); !ok {
		return nil, fmt.Errorf("source audio missing")
	}
	ffmpeg := r.FFmpeg
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	seconds := func(ms int64) string { return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64) }
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-ss", seconds(start), "-i", r.Blobs.Locate(key),
		"-t", seconds(duration), "-map", "0:a:0", "-map_metadata", "-1", "-c:a", "aac", "-b:a", bitrate,
		"-ar", "44100", "-ac", "2", "-fflags", "+bitexact", "-flags:a", "+bitexact",
		"-movflags", "+frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
	var data, stderr bytes.Buffer
	cmd.Stdout = &data
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("moment audio: %w: %s", err, stderr.String())
	}
	return data.Bytes(), nil
}
