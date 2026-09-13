package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// writeWAV writes a 16-bit stereo WAV, with TPDF dither when dither is set.
func writeWAV(path string, b *Bus, dither bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	n := b.Len()
	data := uint32(n * 4)
	header := []any{
		[]byte("RIFF"), 36 + data, []byte("WAVE"),
		[]byte("fmt "), uint32(16), uint16(1), uint16(2), uint32(sampleRate), uint32(sampleRate * 4), uint16(4), uint16(16),
		[]byte("data"), data,
	}
	for _, v := range header {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	rng := rand.New(rand.NewPCG(1, 2))
	quantise := func(x float32) int16 {
		v := float64(x) * 32767
		if dither {
			v += rng.Float64() - rng.Float64()
		}
		return int16(math.Max(-32768, math.Min(32767, math.Round(v))))
	}
	for i := 0; i < n; i++ {
		if err := binary.Write(w, binary.LittleEndian, [2]int16{quantise(b.L[i]), quantise(b.R[i])}); err != nil {
			return err
		}
	}
	return w.Flush()
}

func writeSong(s *Song, a *Arrangement, master *Bus, cfg config) error {
	dir := filepath.Join(cfg.out, s.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	wav := filepath.Join(cfg.stems, s.ID+"-master.wav")
	if err := writeWAV(wav, master, true); err != nil {
		return err
	}
	audio, err := encode(wav, dir, cfg.format)
	if err != nil {
		return err
	}

	art := s.Artwork(1400)
	f, err := os.Create(filepath.Join(dir, "artwork.jpg"))
	if err != nil {
		return err
	}
	if err := jpeg.Encode(f, art.Image(), &jpeg.Options{Quality: 90}); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	duration := float64(master.Len()) / sampleRate
	if err := os.WriteFile(filepath.Join(dir, "lyrics.lrc"), []byte(lrc(s, a, duration)), 0o644); err != nil {
		return err
	}

	highlight := 0.0
	for _, p := range a.Parts {
		if p.Name == "chorus" && len(p.Lines) > 0 {
			for _, l := range a.Lines {
				if l.Start >= float64(p.StartBar)*a.Bar-0.01 {
					highlight = math.Round((l.Start-0.3)*100) / 100
					break
				}
			}
			break
		}
	}
	manifest := map[string]any{
		"id":             s.ID,
		"title":          s.Title,
		"artist":         s.Artist,
		"releaseYear":    s.ReleaseYear,
		"genre":          s.Genre,
		"explicit":       false,
		"highlightStart": highlight,
		"license":        map[string]string{"name": "CC0-1.0", "url": "https://creativecommons.org/publicdomain/zero/1.0/"},
		"attribution":    "Original demo recording made for the MIFS prototype. Vocals synthesised with Piper TTS (en_US-ljspeech-high voice).",
		"audio":          audio,
		"artwork":        "artwork.jpg",
		"lyrics":         "lyrics.lrc",
	}
	if s.Album != "" {
		manifest["album"] = s.Album
		manifest["trackNumber"] = s.TrackNumber
	}
	data, err := json.MarshalIndent(orderedManifest(manifest), "", "  ")
	if err != nil {
		return err
	}
	log.Printf("%s: wrote %s (%.1f s, highlight %.2f s)", s.ID, dir, duration, highlight)
	return os.WriteFile(filepath.Join(dir, "song.json"), append(data, '\n'), 0o644)
}

// orderedManifest keeps song.json keys in a readable, stable order.
func orderedManifest(m map[string]any) json.Marshaler {
	return manifestJSON{m}
}

type manifestJSON struct{ m map[string]any }

func (o manifestJSON) MarshalJSON() ([]byte, error) {
	order := []string{"id", "title", "artist", "album", "trackNumber", "releaseYear", "genre", "explicit",
		"highlightStart", "license", "attribution", "audio", "artwork", "lyrics"}
	var sb strings.Builder
	sb.WriteString("{")
	first := true
	for _, k := range order {
		v, ok := o.m[k]
		if !ok {
			continue
		}
		enc, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if !first {
			sb.WriteString(",")
		}
		first = false
		fmt.Fprintf(&sb, "%q:%s", k, enc)
	}
	sb.WriteString("}")
	return []byte(sb.String()), nil
}

// encode turns the master WAV into audio.flac (or audio.m4a) inside dir.
func encode(wav, dir, format string) (string, error) {
	flac := filepath.Join(dir, "audio.flac")
	m4a := filepath.Join(dir, "audio.m4a")
	os.Remove(flac)
	os.Remove(m4a)
	run := func(args ...string) error {
		out, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-y", "-i", wav, "-map_metadata", "-1", "-fflags", "+bitexact"}, args...)...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("ffmpeg: %v: %s", err, out)
		}
		return nil
	}
	if format != "m4a" {
		if err := run("-c:a", "flac", "-compression_level", "8", flac); err != nil {
			return "", err
		}
		info, err := os.Stat(flac)
		if err != nil {
			return "", err
		}
		if format == "flac" || info.Size() <= 12<<20 {
			return "audio.flac", nil
		}
		os.Remove(flac)
	}
	codec := "aac"
	if out, _ := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output(); strings.Contains(string(out), "aac_at") {
		codec = "aac_at" // Apple's AudioToolbox encoder sounds better where it exists
	}
	if err := run("-c:a", codec, "-b:a", "256k", "-movflags", "+faststart", m4a); err != nil {
		return "", err
	}
	return "audio.m4a", nil
}

// lrc renders line-synced lyrics. A blank timestamped line marks where a line ends before
// an instrumental gap of more than two seconds.
func lrc(s *Song, a *Arrangement, duration float64) string {
	stamp := func(t float64) string {
		cs := int(math.Round(t * 100))
		return fmt.Sprintf("[%02d:%02d.%02d]", cs/6000, cs/100%60, cs%100)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[ti:%s]\n[ar:%s]\n", s.Title, s.Artist)
	if s.Album != "" {
		fmt.Fprintf(&sb, "[al:%s]\n", s.Album)
	}
	total := int(math.Round(duration))
	fmt.Fprintf(&sb, "[length:%02d:%02d]\n[by:MIFS demogen]\n\n", total/60, total%60)
	for i, l := range a.Lines {
		fmt.Fprintf(&sb, "%s%s\n", stamp(l.Start), l.Text)
		if i == len(a.Lines)-1 || a.Lines[i+1].Start-l.End > 2 {
			fmt.Fprintf(&sb, "%s\n", stamp(l.End))
		}
	}
	return sb.String()
}
