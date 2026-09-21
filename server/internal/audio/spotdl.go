package audio

import (
	"bufio"
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
type SpotDL struct {
	Binary     string
	CookieFile string
}

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
	// MIFS fetches synced lyrics separately. Avoid redundant provider requests.
	args = append(args, "--log-level", "ERROR", "--lyrics")
	if s.CookieFile != "" {
		args = append(args, "--cookie-file", s.CookieFile)
	}
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
	var stderr, output diagnosticTail
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	lastPercent := -1
	authenticationRequired := false
	for scanner.Scan() {
		line := scanner.Text()
		output.Write([]byte(line + "\n"))
		if at := strings.Index(line, "MIFS_SOURCE_ERROR "); at >= 0 {
			var failure struct {
				Message string `json:"message"`
			}
			if json.Unmarshal([]byte(line[at+18:]), &failure) == nil {
				authenticationRequired = authenticationRequired || needsAuthentication(failure.Message)
			}
		}
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
	waitErr := cmd.Wait()
	detail := strings.TrimSpace(output.text + "\n" + stderr.text)
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("spotdl: %w", ctx.Err())
	}
	if err := scanner.Err(); err != nil {
		return Result{}, err
	}
	info, statErr := os.Stat(out)
	if waitErr != nil || statErr != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		if authenticationRequired || needsAuthentication(detail) {
			return Result{}, ErrAuthenticationRequired
		}
		if len(detail) > 2000 {
			detail = detail[len(detail)-2000:]
		}
		if waitErr != nil {
			return Result{}, fmt.Errorf("spotdl: %w: %s", waitErr, detail)
		}
		return Result{}, fmt.Errorf("spotdl produced no audio: %s", detail)
	}
	return Result{Path: out}, nil
}

// Bound subprocess diagnostics even if a provider logs continuously.
type diagnosticTail struct{ text string }

func (b *diagnosticTail) Write(p []byte) (int, error) {
	const limit = 8192
	b.text += string(p)
	if len(b.text) > limit {
		b.text = b.text[len(b.text)-limit:]
	}
	return len(p), nil
}

func needsAuthentication(message string) bool {
	message = strings.ToLower(strings.Join(strings.Fields(message), " "))
	return strings.Contains(message, "sign in to confirm") ||
		strings.Contains(message, "login required") || strings.Contains(message, "authentication required")
}
