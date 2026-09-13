package lrc

import (
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	doc := "\xef\xbb\xbf[ti:Neon Harbor]\r\n" +
		"[ar:Orchid Relay]\n" +
		"[length:02:21]\n" +
		"\n" +
		"[00:12.50]First line\n" +
		"[00:15.2]Second  <00:15.80>line\n" +
		"[00:20.00][01:02.005]Chorus\n" +
		"[00:23.10]\n" +
		"not a lyric\n"

	file, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if file.Tags["ti"] != "Neon Harbor" || file.Tags["ar"] != "Orchid Relay" || file.Tags["length"] != "02:21" {
		t.Errorf("tags = %v", file.Tags)
	}
	want := []Line{
		{12500 * time.Millisecond, "First line"},
		{15200 * time.Millisecond, "Second line"},
		{20 * time.Second, "Chorus"},
		{23100 * time.Millisecond, ""},
		{62005 * time.Millisecond, "Chorus"},
	}
	if len(file.Lines) != len(want) {
		t.Fatalf("got %d lines: %+v", len(file.Lines), file.Lines)
	}
	for i, line := range file.Lines {
		if line != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, line, want[i])
		}
	}
}

func TestOffsetShiftsLinesEarlier(t *testing.T) {
	file, err := Parse(strings.NewReader("[offset:+500]\n[00:00.20]a\n[00:10.00]b\n"))
	if err != nil {
		t.Fatal(err)
	}
	if file.Lines[0].Time != 0 || file.Lines[1].Time != 9500*time.Millisecond {
		t.Errorf("lines = %+v", file.Lines)
	}
}

func TestRejectsInvalidTimestamp(t *testing.T) {
	if _, err := Parse(strings.NewReader("[00:75.00]bad\n")); err == nil {
		t.Error("expected an error for 75 seconds")
	}
}
