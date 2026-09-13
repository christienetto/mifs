// Package ingest imports songs into the catalog from source folders:
//
//	<song>/song.json    metadata (see Manifest)
//	<song>/audio.*      any format ffmpeg reads
//	<song>/artwork.*    optional, any image ffmpeg reads
//	<song>/lyrics.lrc   optional, synced lyrics
//
// Audio is normalised to AAC in MP4 with the index up front (so clients can stream and
// seek with range requests), a loudness envelope is precomputed for the timeline, and
// artwork is rendered at full and thumbnail size. Only ingest needs ffmpeg; serving does not.
package ingest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/christienetto/mifs/server/internal/blob"
	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/lrc"
)

const (
	// PointsPerSecond matches the iOS app's Waveform.resolution.
	PointsPerSecond = 10
	waveformRate    = 8000
	artworkSize     = 1200
	thumbnailSize   = 300
	audioBitrate    = "256k"
	// A final lyric line without an explicit end marker is shown for at most this long.
	maxLastLineMs = 8000
)

// Manifest is song.json.
type Manifest struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	TrackNumber int    `json:"trackNumber"`
	ReleaseYear int    `json:"releaseYear"`
	Genre       string `json:"genre"`
	Explicit    bool   `json:"explicit"`
	// HighlightStart is the suggested snippet start in seconds, e.g. the chorus.
	HighlightStart *float64 `json:"highlightStart"`
	License        struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"license"`
	Attribution string `json:"attribution"`
	// File names inside the song folder. Defaults: audio.*, artwork.*, lyrics.lrc.
	Audio   string `json:"audio"`
	Artwork string `json:"artwork"`
	Lyrics  string `json:"lyrics"`
}

var validID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Ingester imports song folders.
type Ingester struct {
	Store   *catalog.Store
	Blobs   *blob.Store
	FFmpeg  string
	FFprobe string
}

// SongDirs returns root if it is a song folder, otherwise its immediate song subfolders.
func SongDirs(root string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, "song.json")); err == nil {
		return []string{root}, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, "song.json")); entry.IsDir() && err == nil {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("%s contains no song.json", root)
	}
	return dirs, nil
}

// Ingest imports (or re-imports) the song in dir.
func (in *Ingester) Ingest(ctx context.Context, dir string) (catalog.Song, error) {
	manifest, err := readManifest(dir)
	if err != nil {
		return catalog.Song{}, err
	}
	work, err := os.MkdirTemp("", "mifs-ingest-")
	if err != nil {
		return catalog.Song{}, err
	}
	defer os.RemoveAll(work)

	song := catalog.Song{
		ID:               manifest.ID,
		Title:            manifest.Title,
		Artist:           manifest.Artist,
		Album:            manifest.Album,
		TrackNumber:      manifest.TrackNumber,
		ReleaseYear:      manifest.ReleaseYear,
		Genre:            manifest.Genre,
		Explicit:         manifest.Explicit,
		HighlightStartMs: -1,
		AudioContentType: "audio/mp4",
		LicenseName:      manifest.License.Name,
		LicenseURL:       manifest.License.URL,
		Attribution:      manifest.Attribution,
	}

	// Audio.
	audio := filepath.Join(work, "audio.m4a")
	if err := in.transcode(ctx, filepath.Join(dir, manifest.Audio), audio); err != nil {
		return catalog.Song{}, err
	}
	info, err := in.probe(ctx, audio)
	if err != nil {
		return catalog.Song{}, err
	}
	song.DurationMs = int64(math.Round(info.duration * 1000))
	song.AudioBitrate = info.bitrate
	if song.AudioKey, song.AudioBytes, err = in.Blobs.Put(audio, "audio", "m4a"); err != nil {
		return catalog.Song{}, err
	}
	if manifest.HighlightStart != nil {
		song.HighlightStartMs = min(max(0, int64(*manifest.HighlightStart*1000)), song.DurationMs)
	}

	rms, err := in.waveform(ctx, audio)
	if err != nil {
		return catalog.Song{}, err
	}

	// Artwork.
	if manifest.Artwork != "" {
		src := filepath.Join(dir, manifest.Artwork)
		if song.ArtworkKey, err = in.artwork(ctx, src, work, artworkSize); err != nil {
			return catalog.Song{}, err
		}
		if song.ThumbnailKey, err = in.artwork(ctx, src, work, thumbnailSize); err != nil {
			return catalog.Song{}, err
		}
	}

	// Lyrics.
	var lines []catalog.LyricLine
	if manifest.Lyrics != "" {
		if lines, err = readLyrics(filepath.Join(dir, manifest.Lyrics), song.DurationMs); err != nil {
			return catalog.Song{}, err
		}
	}

	record := catalog.Record{Song: song, Lyrics: lines, Waveform: catalog.Waveform{PointsPerSecond: PointsPerSecond, RMS: rms}}
	if err := in.Store.Put(ctx, record); err != nil {
		return catalog.Song{}, err
	}
	song.HasLyrics = len(lines) > 0
	return song, nil
}

func readManifest(dir string) (Manifest, error) {
	var manifest Manifest
	data, err := os.ReadFile(filepath.Join(dir, "song.json"))
	if err != nil {
		return manifest, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("%s/song.json: %w", dir, err)
	}

	var problems []string
	if !validID.MatchString(manifest.ID) {
		problems = append(problems, "id must be lowercase letters, digits and dashes")
	}
	if strings.TrimSpace(manifest.Title) == "" {
		problems = append(problems, "title is required")
	}
	if strings.TrimSpace(manifest.Artist) == "" {
		problems = append(problems, "artist is required")
	}
	defaults := []struct {
		field    *string
		base     string
		required bool
	}{
		{&manifest.Audio, "audio", true},
		{&manifest.Artwork, "artwork", false},
		{&manifest.Lyrics, "lyrics", false},
	}
	for _, d := range defaults {
		if *d.field == "" {
			matches, _ := filepath.Glob(filepath.Join(dir, d.base+".*"))
			if len(matches) > 0 {
				*d.field = filepath.Base(matches[0])
			}
		}
		if *d.field == "" {
			if d.required {
				problems = append(problems, d.base+" file is missing")
			}
			continue
		}
		if filepath.Base(*d.field) != *d.field {
			problems = append(problems, fmt.Sprintf("%s must be a file name inside the song folder", d.base))
		} else if _, err := os.Stat(filepath.Join(dir, *d.field)); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return manifest, fmt.Errorf("%s/song.json: %s", dir, strings.Join(problems, "; "))
	}
	return manifest, nil
}

func (in *Ingester) transcode(ctx context.Context, src, dest string) error {
	info, err := in.probe(ctx, src)
	if err != nil {
		return err
	}
	args := []string{"-v", "error", "-y", "-i", src, "-map", "0:a:0", "-map_metadata", "-1",
		"-fflags", "+bitexact", "-flags:a", "+bitexact"}
	if ext := strings.ToLower(filepath.Ext(src)); info.codec == "aac" && (ext == ".m4a" || ext == ".mp4") {
		args = append(args, "-c:a", "copy") // already delivery format; don't re-encode
	} else {
		args = append(args, "-c:a", "aac", "-b:a", audioBitrate, "-ar", "44100", "-ac", "2")
	}
	args = append(args, "-movflags", "+faststart", dest)
	_, err = run(ctx, in.FFmpeg, args...)
	return err
}

type probeInfo struct {
	duration float64
	bitrate  int
	codec    string
}

func (in *Ingester) probe(ctx context.Context, path string) (probeInfo, error) {
	out, err := run(ctx, in.FFprobe, "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name,bit_rate:format=duration", "-of", "json", path)
	if err != nil {
		return probeInfo{}, err
	}
	var parsed struct {
		Streams []struct {
			Codec   string `json:"codec_name"`
			Bitrate string `json:"bit_rate"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return probeInfo{}, err
	}
	if len(parsed.Streams) == 0 {
		return probeInfo{}, fmt.Errorf("%s has no audio stream", path)
	}
	duration, err := strconv.ParseFloat(parsed.Format.Duration, 64)
	if err != nil || duration <= 0 {
		return probeInfo{}, fmt.Errorf("%s: unknown duration", path)
	}
	bitrate, _ := strconv.Atoi(parsed.Streams[0].Bitrate)
	return probeInfo{duration: duration, bitrate: bitrate, codec: parsed.Streams[0].Codec}, nil
}

// waveform decodes to 8 kHz mono and returns the RMS of every 1/PointsPerSecond second,
// the same analysis the iOS app runs on audio it reads locally.
func (in *Ingester) waveform(ctx context.Context, path string) ([]float32, error) {
	cmd := exec.CommandContext(ctx, in.FFmpeg, "-v", "error", "-i", path,
		"-ac", "1", "-ar", strconv.Itoa(waveformRate), "-f", "f32le", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	const window = waveformRate / PointsPerSecond
	reader := bufio.NewReaderSize(stdout, 64*1024)
	var levels []float32
	var sum float64
	var count int
	var sample [4]byte
	for {
		if _, err := io.ReadFull(reader, sample[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			cmd.Wait()
			return nil, err
		}
		value := float64(math.Float32frombits(binary.LittleEndian.Uint32(sample[:])))
		sum += value * value
		if count++; count == window {
			levels = append(levels, roundSignificant(math.Sqrt(sum/window)))
			sum, count = 0, 0
		}
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg waveform: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if count > 0 {
		levels = append(levels, roundSignificant(math.Sqrt(sum/float64(count))))
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("%s decoded to no audio", path)
	}
	return levels, nil
}

// roundSignificant keeps 4 significant digits: plenty for drawing, and a third of the JSON.
func roundSignificant(value float64) float32 {
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(value, 'g', 4, 32), 32)
	return float32(rounded)
}

func (in *Ingester) artwork(ctx context.Context, src, work string, size int) (string, error) {
	dest := filepath.Join(work, fmt.Sprintf("artwork-%d.jpg", size))
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase:flags=lanczos,crop=%d:%d", size, size, size, size)
	if _, err := run(ctx, in.FFmpeg, "-v", "error", "-y", "-i", src, "-vf", filter, "-frames:v", "1", "-update", "1",
		"-q:v", "3", "-fflags", "+bitexact", "-flags:v", "+bitexact", dest); err != nil {
		return "", err
	}
	key, _, err := in.Blobs.Put(dest, "artwork", "jpg")
	return key, err
}

// readLyrics converts LRC entries to lines with explicit ends: the next entry's time
// (an empty entry marks the end of a line), or for the final line a short hold.
func readLyrics(path string, durationMs int64) ([]catalog.LyricLine, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	parsed, err := lrc.Parse(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var lines []catalog.LyricLine
	for i, entry := range parsed.Lines {
		if entry.Text == "" {
			continue
		}
		start := entry.Time.Milliseconds()
		if start >= durationMs {
			return nil, fmt.Errorf("%s: line %q starts at %s, after the audio ends", path, entry.Text, entry.Time)
		}
		end := min(durationMs, start+maxLastLineMs)
		if i+1 < len(parsed.Lines) {
			end = min(durationMs, parsed.Lines[i+1].Time.Milliseconds())
		}
		lines = append(lines, catalog.LyricLine{StartMs: start, EndMs: end, Text: entry.Text})
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%s has no timestamped lyric lines", path)
	}
	return lines, nil
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
