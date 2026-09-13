package ingest

import (
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/christienetto/mifs/server/internal/blob"
	"github.com/christienetto/mifs/server/internal/catalog"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
}

// writeTone writes a 16-bit stereo WAV: quiet for 3 s, loud for 3 s.
func writeTone(t *testing.T, path string) {
	t.Helper()
	const rate, seconds = 44100, 6
	samples := make([]int16, 0, rate*seconds*2)
	for i := range rate * seconds {
		amplitude := 0.05
		if i >= rate*3 {
			amplitude = 0.8
		}
		v := int16(amplitude * 32767 * math.Sin(2*math.Pi*440*float64(i)/rate))
		samples = append(samples, v, v)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data := uint32(len(samples) * 2)
	header := []any{
		[4]byte{'R', 'I', 'F', 'F'}, 36 + data, [4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '}, uint32(16), uint16(1), uint16(2), uint32(rate), uint32(rate * 4), uint16(4), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, data,
	}
	for _, field := range append(header, samples) {
		if err := binary.Write(file, binary.LittleEndian, field); err != nil {
			t.Fatal(err)
		}
	}
}

func writeSongFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTone(t, filepath.Join(dir, "audio.wav"))

	art := image.NewRGBA(image.Rect(0, 0, 80, 50)) // not square: ingest crops
	for x := range 80 {
		for y := range 50 {
			art.Set(x, y, color.RGBA{uint8(x * 3), 40, uint8(y * 5), 255})
		}
	}
	file, err := os.Create(filepath.Join(dir, "artwork.png"))
	if err != nil {
		t.Fatal(err)
	}
	png.Encode(file, art)
	file.Close()

	lyrics := "[ti:Tone]\n[00:01.00]first line\n[00:02.50]second line\n[00:03.20]\n[00:04.00]last line\n"
	manifest := `{"id": "tone", "title": "Tone", "artist": "Tests", "genre": "Test", "highlightStart": 3.0,
		"license": {"name": "CC0-1.0", "url": "https://creativecommons.org/publicdomain/zero/1.0/"}}`
	for name, content := range map[string]string{"lyrics.lrc": lyrics, "song.json": manifest} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestIngestSongFolder(t *testing.T) {
	requireFFmpeg(t)
	ctx := context.Background()
	data := t.TempDir()
	store, err := catalog.Open(filepath.Join(data, "mifs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	blobs, err := blob.Open(filepath.Join(data, "media"))
	if err != nil {
		t.Fatal(err)
	}
	ingester := &Ingester{Store: store, Blobs: blobs, FFmpeg: "ffmpeg", FFprobe: "ffprobe"}

	dir := writeSongFolder(t)
	song, err := ingester.Ingest(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	if song.DurationMs < 5900 || song.DurationMs > 6100 {
		t.Errorf("duration = %d ms", song.DurationMs)
	}
	if song.HighlightStartMs != 3000 || !song.HasLyrics || song.AudioContentType != "audio/mp4" {
		t.Errorf("song = %+v", song)
	}
	for _, key := range []string{song.AudioKey, song.ArtworkKey, song.ThumbnailKey} {
		if !blob.ValidKey(key) {
			t.Fatalf("invalid key %q", key)
		}
		if _, err := os.Stat(filepath.Join(blobs.Dir(), key)); err != nil {
			t.Error(err)
		}
	}

	thumb, err := os.Open(filepath.Join(blobs.Dir(), song.ThumbnailKey))
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(thumb)
	thumb.Close()
	if err != nil || config.Width != thumbnailSize || config.Height != thumbnailSize {
		t.Errorf("thumbnail = %+v, %v", config, err)
	}

	waveform, err := store.Waveform(ctx, "tone")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(waveform.RMS); n < 58 || n > 62 {
		t.Errorf("waveform has %d points", n)
	}
	quiet, loud := waveform.RMS[15], waveform.RMS[45]
	if !(loud > quiet*8) {
		t.Errorf("waveform doesn't follow loudness: quiet %v loud %v", quiet, loud)
	}

	lines, err := store.Lyrics(ctx, "tone")
	if err != nil {
		t.Fatal(err)
	}
	want := []catalog.LyricLine{
		{StartMs: 1000, EndMs: 2500, Text: "first line"},
		{StartMs: 2500, EndMs: 3200, Text: "second line"}, // ended by the empty marker
		{StartMs: 4000, EndMs: song.DurationMs, Text: "last line"},
	}
	if len(lines) != len(want) {
		t.Fatalf("lyrics = %+v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, lines[i], want[i])
		}
	}

	// Re-ingesting unchanged sources is idempotent: same blobs, one row.
	again, err := ingester.Ingest(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.AudioKey != song.AudioKey || again.ArtworkKey != song.ArtworkKey {
		t.Errorf("re-ingest changed keys: %s → %s", song.AudioKey, again.AudioKey)
	}
	if n, _ := store.Count(ctx); n != 1 {
		t.Errorf("count = %d", n)
	}
}

func TestManifestValidation(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "song.json"), []byte(`{"id": "Bad ID", "title": "", "artist": "A", "audio": "../x.wav"}`), 0o644)
	if _, err := readManifest(dir); err == nil {
		t.Error("expected validation errors")
	}
	os.WriteFile(filepath.Join(dir, "song.json"), []byte(`{"id": "ok", "title": "T", "artist": "A", "tempo": 1}`), 0o644)
	if _, err := readManifest(dir); err == nil {
		t.Error("expected unknown field error")
	}
}

func TestSongDirs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"b", "a"} {
		os.MkdirAll(filepath.Join(root, name), 0o755)
		os.WriteFile(filepath.Join(root, name, "song.json"), []byte("{}"), 0o644)
	}
	os.MkdirAll(filepath.Join(root, "not-a-song"), 0o755)
	dirs, err := SongDirs(root)
	if err != nil || len(dirs) != 2 || filepath.Base(dirs[0]) != "a" {
		t.Errorf("dirs = %v, %v", dirs, err)
	}
	single, err := SongDirs(filepath.Join(root, "a"))
	if err != nil || len(single) != 1 {
		t.Errorf("single = %v, %v", single, err)
	}
}
