package lyrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/lrc"
)

func TestLines(t *testing.T) {
	parsed, err := lrc.Parse(strings.NewReader("[00:01.00]one\n[00:02.00]♪\n[00:05.00]two\n[00:09.00]past the end\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := Lines(parsed.Lines, 8_000)
	want := []catalog.LyricLine{{StartMs: 1000, EndMs: 2000, Text: "one"}, {StartMs: 5000, EndMs: 8000, Text: "two"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Lines = %+v", got)
	}
	fitted := Fit(got, 6_000)
	if len(fitted) != 2 || fitted[1].EndMs != 6_000 || len(Fit(got, 3_000)) != 1 {
		t.Errorf("Fit = %+v", fitted)
	}
}

func TestLRCLIB(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "MIFS") {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/api/get" && q.Get("track_name") == "Blinding Lights" && q.Get("duration") == "200":
			w.Write([]byte(`{"trackName": "Blinding Lights", "duration": 200, "instrumental": false,
				"syncedLyrics": "[00:27.16] I've been tryna call\n[00:29.96] I've been on my own for long enough"}`))
		case r.URL.Path == "/api/get":
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/api/search" && q.Get("track_name") == "No Album":
			w.Write([]byte(`[{"duration": 100, "syncedLyrics": "[00:01.00]wrong length"},
				{"duration": 181, "syncedLyrics": "[00:01.00]right one"}]`))
		case r.URL.Path == "/api/search" && q.Get("track_name") == "Interlude":
			w.Write([]byte(`[{"duration": 60, "instrumental": true}]`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()
	lrclib := &LRCLIB{BaseURL: server.URL}
	ctx := context.Background()

	lines, err := lrclib.Synced(ctx, Query{Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours", DurationMs: 200_000})
	if err != nil || len(lines) != 2 || lines[0] != (catalog.LyricLine{StartMs: 27_160, EndMs: 29_960, Text: "I've been tryna call"}) {
		t.Errorf("get = %+v, %v", lines, err)
	}
	lines, err = lrclib.Synced(ctx, Query{Title: "No Album", Artist: "A", DurationMs: 180_000})
	if err != nil || len(lines) != 1 || lines[0].Text != "right one" {
		t.Errorf("search fallback = %+v, %v", lines, err)
	}
	for _, q := range []Query{{Title: "Interlude", Artist: "A", DurationMs: 60_000}, {Title: "Unknown", Artist: "A"}, {Title: "No artist"}} {
		if _, err := lrclib.Synced(ctx, q); !errors.Is(err, ErrNotFound) {
			t.Errorf("%+v: err = %v", q, err)
		}
	}
}
