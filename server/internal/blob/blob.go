// Package blob stores media files by content hash.
//
// Keys look like "audio/3f/3fa9…e1.m4a". Because a key changes whenever its bytes do,
// blobs are immutable: they can be cached forever and synced as-is to object storage
// or a CDN (see MIFS_MEDIA_URL).
package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Store is a directory of content-addressed files.
type Store struct {
	dir string
}

var validKey = regexp.MustCompile(`^[a-z]+/[0-9a-f]{2}/[0-9a-f]{64}\.[a-z0-9]+$`)

// Open returns a store rooted at dir, creating it if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir is the store's root directory.
func (s *Store) Dir() string { return s.dir }

// Put copies the file at src into the store under kind (e.g. "audio") and returns its key.
func (s *Store) Put(src, kind, ext string) (key string, size int64, err error) {
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()

	hash := sha256.New()
	size, err = io.Copy(hash, in)
	if err != nil {
		return "", 0, err
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	key = fmt.Sprintf("%s/%s/%s.%s", kind, sum[:2], sum, ext)
	if !validKey.MatchString(key) {
		return "", 0, fmt.Errorf("invalid blob key %q", key)
	}

	dest := filepath.Join(s.dir, filepath.FromSlash(key))
	if _, err := os.Stat(dest); err == nil {
		return key, size, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", 0, err
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".incoming-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return "", 0, err
	}
	return key, size, nil
}

// ValidKey reports whether key has the shape Put produces (no traversal possible).
func ValidKey(key string) bool { return validKey.MatchString(key) }

// ContentHash is the SHA-256 part of a valid key.
func ContentHash(key string) string {
	base := filepath.Base(key)
	return base[:len(base)-len(filepath.Ext(base))]
}
