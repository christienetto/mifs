package audio

import (
	"context"
	"errors"
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

func TestSpotDLFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, script, detail string
		auth                 bool
	}{
		{"stdout despite successful exit", "printf 'AudioProviderError: upstream rejected request\\n'", "upstream rejected request", false},
		{"stderr on failure", "echo 'connection refused' >&2; exit 1", "connection refused", false},
		{"authentication marker", `printf 'MIFS_SOURCE_ERROR {"message":"Sign in to confirm you’re not a bot"}\n'`, "", true},
		{"wrapped authentication message", "printf 'Sign in to confirm you are not\\na bot\\n'", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "spotdl-test")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			_, err := (&SpotDL{Binary: binary}).Fetch(context.Background(), Request{Refs: []string{"spotify:0123456789abcdefghijkl"}}, dir)
			if err == nil || (tc.auth && !errors.Is(err, ErrAuthenticationRequired)) || (!tc.auth && !strings.Contains(err.Error(), tc.detail)) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDiagnosticTailIsBounded(t *testing.T) {
	var tail diagnosticTail
	for range 100 {
		tail.Write([]byte(strings.Repeat("x", 1000)))
	}
	tail.Write([]byte("last error"))
	if len(tail.text) != 8192 || !strings.HasSuffix(tail.text, "last error") {
		t.Fatal("diagnostics must retain only the bounded tail")
	}
}
