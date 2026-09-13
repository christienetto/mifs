package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	hexenc "encoding/hex"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Voice renders spoken lines with Piper and caches them on disk.
type Voice struct {
	Piper, Model, Cache string
}

// Line returns the trimmed, level-matched mono clip for text at 44.1 kHz.
func (v Voice) Line(text string, lengthScale float64) ([]float32, error) {
	key := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%.3f", filepath.Base(v.Model), text, lengthScale)))
	wav := filepath.Join(v.Cache, "vocals", hexenc.EncodeToString(key[:8])+".wav")
	if _, err := os.Stat(wav); err != nil {
		if err := os.MkdirAll(filepath.Dir(wav), 0o755); err != nil {
			return nil, err
		}
		cmd := exec.Command(v.Piper, "-m", v.Model, "--length-scale", fmt.Sprint(lengthScale),
			"--sentence-silence", "0", "-f", wav)
		cmd.Stdin = strings.NewReader(text)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("piper %q: %v\n%s", text, err, out)
		}
	}
	raw, err := decode(wav)
	if err != nil {
		return nil, err
	}
	return levelMatch(trim(raw)), nil
}

// decode converts any audio file to mono float32 PCM at the project sample rate via ffmpeg.
func decode(path string) ([]float32, error) {
	cmd := exec.Command("ffmpeg", "-v", "error", "-i", path, "-ac", "1", "-ar", fmt.Sprint(sampleRate), "-f", "f32le", "-")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg decode %s: %v: %s", path, err, stderr.String())
	}
	samples := make([]float32, out.Len()/4)
	if err := binary.Read(&out, binary.LittleEndian, samples); err != nil {
		return nil, err
	}
	return samples, nil
}

// trim removes leading and trailing silence so a clip's first sample is its vocal onset.
func trim(x []float32) []float32 {
	win := sample(0.005)
	rms := func(start int) float64 {
		sum := 0.0
		for i := start; i < min(start+win, len(x)); i++ {
			sum += float64(x[i]) * float64(x[i])
		}
		return math.Sqrt(sum / float64(win))
	}
	peak := 0.0
	for i := 0; i+win <= len(x); i += win {
		peak = math.Max(peak, rms(i))
	}
	threshold := peak * math.Pow(10, -38.0/20)
	first, last := 0, len(x)
	for i := 0; i+win <= len(x); i += win {
		if rms(i) > threshold {
			first = max(0, i-win) // keep 5 ms of the attack
			break
		}
	}
	for i := len(x) - win; i >= 0; i -= win {
		if rms(i) > threshold {
			last = min(len(x), i+4*win)
			break
		}
	}
	clip := append([]float32(nil), x[first:last]...)
	fade := sample(0.003)
	for i := 0; i < fade && i < len(clip); i++ {
		clip[i] *= float32(i) / float32(fade)
		clip[len(clip)-1-i] *= float32(i) / float32(fade)
	}
	return clip
}

// levelMatch scales a clip to a consistent RMS so every line sits at the same level.
func levelMatch(x []float32) []float32 {
	sum := 0.0
	for _, s := range x {
		sum += float64(s) * float64(s)
	}
	rms := math.Sqrt(sum / float64(len(x)))
	gain := float32(math.Pow(10, -20.0/20) / rms)
	for i := range x {
		x[i] *= gain
	}
	return x
}
