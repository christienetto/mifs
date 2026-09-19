package audio

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SpotDL acquires a selected Spotify recording once; the normal ingestion pipeline
// verifies its duration, converts it and stores the full song under a content hash.
type SpotDL struct{ Binary string }

//go:embed spotdl_progress.py
var progressScript string

func (s *SpotDL) Name() string { return "spotdl" }

var spotifyRef = regexp.MustCompile(`^spotify:([A-Za-z0-9]{22})$`)

func (s *SpotDL) Fetch(ctx context.Context, req Request, dir string) (Result, error) {
	var id string
	for _, ref := range req.Refs {
		if m := spotifyRef.FindStringSubmatch(ref); m != nil {
			id = m[1]
			break
		}
	}
	if id == "" {
		return Result{}, ErrNotFound
	}
	binary := s.Binary
	if binary == "" {
		binary = "spotdl"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out := filepath.Join(dir, "spotdl.m4a")
	args := []string{"download", "https://open.spotify.com/track/" + id,
		"--format", "m4a", "--output", filepath.Join(dir, "spotdl.{output-ext}"), "--threads", "1"}
	command := binary
	if resolved, err := exec.LookPath(binary); err == nil {
		if target, err := filepath.EvalSymlinks(resolved); err == nil {
			python := filepath.Join(filepath.Dir(target), "python")
			if info, err := os.Stat(python); err == nil && !info.IsDir() {
				command = python
				args = append([]string{"-u", "-c", progressScript}, args...)
			}
		}
	}
	cmd := exec.CommandContext(ctx, command, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	lastPercent := -1
	for scanner.Scan() {
		line := scanner.Text()
		if at := strings.Index(line, "MIFS_PROGRESS "); at >= 0 {
			var p struct {
				Downloaded int64 `json:"downloaded"`
				Total      int64 `json:"total"`
			}
			if json.Unmarshal([]byte(line[at+14:]), &p) == nil && p.Total > 0 {
				percent := int(min(100, p.Downloaded*100/p.Total))
				if req.Progress != nil && percent != lastPercent {
					req.Progress(p.Downloaded, p.Total)
					lastPercent = percent
				}
			}
		}
	}
	if scanner.Err() != nil {
		_ = cmd.Process.Kill()
	}
	if err := cmd.Wait(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[len(detail)-500:]
		}
		return Result{}, fmt.Errorf("spotdl: %w: %s", err, detail)
	}
	if err := scanner.Err(); err != nil {
		return Result{}, err
	}
	if info, err := os.Stat(out); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return Result{}, fmt.Errorf("spotdl produced no audio")
	}
	return Result{Path: out}, nil
}
