package audio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpotDLArgumentsAndMissingOutput(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-spotdl")
	script := `#!/bin/sh
[ "$1" = download ] || exit 2
[ "$2" = https://open.spotify.com/track/0123456789abcdefghijkl ] || exit 3
[ "$3" = --format ] && [ "$4" = m4a ] && [ "$5" = --output ] || exit 4
case "$6" in */spotdl.\{output-ext\}) ;; *) exit 5 ;; esac
printf 'MIFS_PROGRESS {"downloaded":512,"total":1024}\n'
printf 'MIFS_PROGRESS {"downloaded":1024,"total":1024}\n'
printf audio > "${6%/*}/spotdl.m4a"
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	source := &SpotDL{Binary: binary}
	var downloaded []int64
	result, err := source.Fetch(context.Background(), Request{Progress: func(n, total int64) {
		if total != 1024 {
			t.Errorf("total: %d", total)
		}
		downloaded = append(downloaded, n)
	}, Refs: []string{"spotify:0123456789abcdefghijkl"}, Title: "$(touch bad)"}, dir)
	if err != nil || result.Path != filepath.Join(dir, "spotdl.m4a") {
		t.Fatalf("%+v %v", result, err)
	}
	if len(downloaded) != 2 || downloaded[0] != 512 || downloaded[1] != 1024 {
		t.Fatalf("progress: %v", downloaded)
	}
	if _, err := source.Fetch(context.Background(), Request{Refs: []string{"spotify:$(touch bad)"}}, dir); err != ErrNotFound {
		t.Fatalf("unvalidated ref: %v", err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Fetch(context.Background(), Request{Refs: []string{"spotify:0123456789abcdefghijkl"}}, t.TempDir()); err == nil || !strings.Contains(err.Error(), "no audio") {
		t.Fatalf("missing output: %v", err)
	}
}
