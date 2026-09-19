package audio

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/christienetto/mifs/server/internal/catalog"
)

// Library is a Source backed by a folder of audio files you own, e.g. ripped CDs or
// purchased downloads. Files are matched by their ISRC tag, then by title and artist tags
// (or an "Artist - Title" file name) with a duration within catalog.MatchToleranceMs.
//
// Tags are read with ffprobe and cached in IndexPath, so only new or changed files are
// probed again. The folder is rescanned at most every MaxAge, when a song is requested,
// so a file dropped in is found on the next attempt.
type Library struct {
	Dir       string
	FFprobe   string
	IndexPath string        // optional tag cache, e.g. data/library-index.json
	MaxAge    time.Duration // default 30 s
	Logger    *slog.Logger

	mu      sync.Mutex
	entries map[string]libraryEntry // by path
	scanned time.Time
}

type libraryEntry struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	ModTime    int64  `json:"modTime"` // Unix nanoseconds
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album,omitempty"`
	ISRC       string `json:"isrc,omitempty"`
	DurationMs int64  `json:"durationMs"`
	HasArtwork bool   `json:"hasArtwork,omitempty"`
}

func (l *Library) Name() string { return "library" }

func (l *Library) Fetch(ctx context.Context, req Request, dir string) (Result, error) {
	l.mu.Lock()
	maxAge := l.MaxAge
	if maxAge == 0 {
		maxAge = 30 * time.Second
	}
	if time.Since(l.scanned) > maxAge {
		if err := l.refreshLocked(ctx); err != nil {
			l.mu.Unlock()
			return Result{}, err
		}
	}
	entry, ok := l.match(req)
	l.mu.Unlock()
	if !ok {
		return Result{}, ErrNotFound
	}
	result := Result{Path: entry.Path}
	if entry.HasArtwork {
		result.Artwork = entry.Path // ffmpeg reads the embedded picture as an image
	}
	return result, nil
}

// match picks the entry for req: same ISRC, else same title and artist with a close duration.
func (l *Library) match(req Request) (libraryEntry, bool) {
	isrc := catalog.NormalizeISRC(req.ISRC)
	key := catalog.MatchKey(req.Title, req.Artist)
	var best libraryEntry
	bestDiff := int64(math.MaxInt64)
	for _, entry := range l.entries {
		if isrc != "" && entry.ISRC == isrc {
			return entry, true
		}
		if isrc != "" && entry.ISRC != "" {
			continue // both known and different: another recording
		}
		if catalog.MatchKey(entry.Title, entry.Artist) != key {
			continue
		}
		diff := entry.DurationMs - req.DurationMs
		if diff < 0 {
			diff = -diff
		}
		if (req.DurationMs == 0 || diff <= catalog.MatchToleranceMs) && diff < bestDiff {
			best, bestDiff = entry, diff
		}
	}
	return best, bestDiff != math.MaxInt64
}

// Refresh rescans the folder now.
func (l *Library) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.refreshLocked(ctx)
}

func (l *Library) refreshLocked(ctx context.Context) error {
	if l.entries == nil {
		l.entries = l.loadIndex()
	}
	start := time.Now()
	seen := map[string]bool{}
	var changed []libraryEntry
	err := filepath.WalkDir(l.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable folders are skipped
		}
		if d.IsDir() {
			if path != l.Dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !slices.Contains(audioExts, strings.ToLower(filepath.Ext(path))) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		seen[path] = true
		if old, ok := l.entries[path]; !ok || old.Size != info.Size() || old.ModTime != info.ModTime().UnixNano() {
			changed = append(changed, libraryEntry{Path: path, Size: info.Size(), ModTime: info.ModTime().UnixNano()})
		}
		return nil
	})
	if err != nil {
		return err
	}
	for path := range l.entries {
		if !seen[path] {
			delete(l.entries, path)
		}
	}

	// Probe new and changed files, a few at a time.
	var wg sync.WaitGroup
	jobs := make(chan int)
	for range 4 {
		wg.Go(func() {
			for i := range jobs {
				changed[i] = l.probe(ctx, changed[i])
			}
		})
	}
	for i := range changed {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, entry := range changed {
		if entry.DurationMs > 0 {
			l.entries[entry.Path] = entry
		}
	}
	l.scanned = time.Now()
	if len(changed) > 0 {
		l.saveIndex()
		if l.Logger != nil {
			l.Logger.Info("library scanned", "dir", l.Dir, "files", len(l.entries), "probed", len(changed),
				"took", time.Since(start).Round(time.Millisecond))
		}
	}
	return nil
}

// probe reads a file's tags and duration. Files ffprobe can't read get no duration and
// are left out.
func (l *Library) probe(ctx context.Context, entry libraryEntry) libraryEntry {
	ffprobe := l.FFprobe
	if ffprobe == "" {
		ffprobe = "ffprobe"
	}
	out, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries",
		"format=duration:format_tags:stream=codec_type:stream_tags", "-of", "json", entry.Path).Output()
	if err != nil {
		return entry
	}
	var parsed struct {
		Streams []struct {
			CodecType string            `json:"codec_type"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	if json.Unmarshal(out, &parsed) != nil {
		return entry
	}
	tags := map[string]string{}
	for _, stream := range parsed.Streams {
		if stream.CodecType == "video" {
			entry.HasArtwork = true // an attached picture
		}
		for k, v := range stream.Tags {
			tags[strings.ToLower(k)] = v
		}
	}
	for k, v := range parsed.Format.Tags {
		tags[strings.ToLower(k)] = v
	}
	seconds, _ := strconv.ParseFloat(parsed.Format.Duration, 64)
	entry.DurationMs = int64(math.Round(seconds * 1000))
	entry.Title, entry.Artist, entry.Album = tags["title"], tags["artist"], tags["album"]
	if entry.Artist == "" {
		entry.Artist = tags["album_artist"]
	}
	entry.ISRC = catalog.NormalizeISRC(tags["isrc"])
	if entry.ISRC == "" {
		entry.ISRC = catalog.NormalizeISRC(tags["tsrc"])
	}
	if entry.Title == "" || entry.Artist == "" {
		// Fall back to "Artist - Title.ext".
		stem := strings.TrimSuffix(filepath.Base(entry.Path), filepath.Ext(entry.Path))
		if artist, title, ok := strings.Cut(stem, " - "); ok {
			entry.Artist, entry.Title = strings.TrimSpace(artist), strings.TrimSpace(title)
		}
	}
	return entry
}

func (l *Library) loadIndex() map[string]libraryEntry {
	entries := map[string]libraryEntry{}
	if l.IndexPath == "" {
		return entries
	}
	data, err := os.ReadFile(l.IndexPath)
	if err != nil {
		return entries
	}
	var list []libraryEntry
	if json.Unmarshal(data, &list) == nil {
		for _, entry := range list {
			entries[entry.Path] = entry
		}
	}
	return entries
}

func (l *Library) saveIndex() {
	if l.IndexPath == "" {
		return
	}
	list := make([]libraryEntry, 0, len(l.entries))
	for _, entry := range l.entries {
		list = append(list, entry)
	}
	slices.SortFunc(list, func(a, b libraryEntry) int { return strings.Compare(a.Path, b.Path) })
	data, err := json.Marshal(list)
	if err != nil {
		return
	}
	tmp := l.IndexPath + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, l.IndexPath)
	}
}
