// Package lrc parses LRC synchronised lyrics.
//
// Supported: ID tags ([ti:…], [offset:±ms], …), one or more leading timestamps per
// line ([mm:ss], [mm:ss.x], [mm:ss.xx], [mm:ss.xxx]) and enhanced-LRC word stamps
// (<mm:ss.xx>), which are stripped. A timestamp with no text marks where the
// previous line ends.
package lrc

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Line is one timestamped entry. Empty Text marks the end of the preceding line.
type Line struct {
	Time time.Duration
	Text string
}

// File is a parsed LRC document. Lines are sorted by Time with the offset applied.
type File struct {
	Tags  map[string]string
	Lines []Line
}

var (
	stamp     = regexp.MustCompile(`^\[(\d{1,3}):(\d{2})(?:[.:](\d{1,3}))?\]`)
	tag       = regexp.MustCompile(`^\[([A-Za-z]+):(.*)\]\s*$`)
	wordStamp = regexp.MustCompile(`<\d{1,3}:\d{2}(?:[.:]\d{1,3})?>`)
)

// Parse reads an LRC document. Lines that carry neither a tag nor a timestamp are ignored.
func Parse(r io.Reader) (*File, error) {
	file := &File{Tags: map[string]string{}}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for number := 1; scanner.Scan(); number++ {
		raw := strings.TrimSpace(scanner.Text())
		if number == 1 {
			raw = strings.TrimPrefix(raw, "\xef\xbb\xbf")
		}
		if raw == "" {
			continue
		}

		var times []time.Duration
		rest := raw
		for {
			match := stamp.FindStringSubmatch(rest)
			if match == nil {
				break
			}
			t, err := parseStamp(match[1], match[2], match[3])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", number, err)
			}
			times = append(times, t)
			rest = rest[len(match[0]):]
		}

		if len(times) == 0 {
			if match := tag.FindStringSubmatch(raw); match != nil {
				file.Tags[strings.ToLower(match[1])] = strings.TrimSpace(match[2])
			}
			continue
		}

		text := strings.Join(strings.Fields(wordStamp.ReplaceAllString(rest, "")), " ")
		for _, t := range times {
			file.Lines = append(file.Lines, Line{Time: t, Text: text})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if offset, ok := file.Tags["offset"]; ok {
		ms, err := strconv.Atoi(strings.TrimPrefix(offset, "+"))
		if err != nil {
			return nil, fmt.Errorf("invalid offset tag %q", offset)
		}
		// A positive offset makes lyrics appear sooner.
		for i := range file.Lines {
			file.Lines[i].Time = max(0, file.Lines[i].Time-time.Duration(ms)*time.Millisecond)
		}
	}

	sort.SliceStable(file.Lines, func(i, j int) bool { return file.Lines[i].Time < file.Lines[j].Time })
	return file, nil
}

func parseStamp(minutes, seconds, fraction string) (time.Duration, error) {
	m, _ := strconv.Atoi(minutes)
	s, _ := strconv.Atoi(seconds)
	if s >= 60 {
		return 0, fmt.Errorf("invalid timestamp %s:%s", minutes, seconds)
	}
	t := time.Duration(m)*time.Minute + time.Duration(s)*time.Second
	if fraction != "" {
		// ".5" is half a second, ".05" fifty milliseconds, ".005" five.
		f, _ := strconv.Atoi(fraction)
		for range 3 - len(fraction) {
			f *= 10
		}
		t += time.Duration(f) * time.Millisecond
	}
	return t, nil
}
