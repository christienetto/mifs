// Package blob stores immutable media files.
//
// Keys look like "audio/3f/3fa9…e1.m4a". Most are content hashes (Put); derived files
// such as mif clips use a hash of their recipe instead (PutAt), which is just as immutable
// because the inputs never change. Either way a key never changes its bytes, so blobs can be
// cached forever and synced as-is to object storage or a CDN (see MIFS_MEDIA_URL).
//
// Put, PutAt, Exists and Locate are the whole storage seam: an S3-compatible store needs
// only these four, with Locate returning a (presigned) URL that ffmpeg reads with range
// requests instead of a local path.
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
	key = Key(kind, hex.EncodeToString(hash.Sum(nil)), ext)
	if !validKey.MatchString(key) {
		return "", 0, fmt.Errorf("invalid blob key %q", key)
	}
	if _, ok := s.Exists(key); ok {
		return key, size, nil
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	return key, size, s.write(in, key)
}

// Key builds a key from a kind, a hex SHA-256 and an extension.
func Key(kind, sum, ext string) string {
	return fmt.Sprintf("%s/%s/%s.%s", kind, sum[:2], sum, ext)
}

// PutAt stores the file at src under a key the caller derived (see Key). The caller
// guarantees that key always names the same bytes.
func (s *Store) PutAt(src, key string) (size int64, err error) {
	if !validKey.MatchString(key) {
		return 0, fmt.Errorf("invalid blob key %q", key)
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), s.write(in, key)
}

// Exists reports whether key is stored, and its size.
func (s *Store) Exists(key string) (size int64, ok bool) {
	if !validKey.MatchString(key) {
		return 0, false
	}
	info, err := os.Stat(s.Locate(key))
	if err != nil {
		return 0, false
	}
	return info.Size(), true
}

// Locate returns where key can be read from: a path here, a URL for remote storage.
func (s *Store) Locate(key string) string {
	return filepath.Join(s.dir, filepath.FromSlash(key))
}

// write copies r to key atomically, so readers never see a partial file.
func (s *Store) write(r io.Reader, key string) error {
	dest := s.Locate(key)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".incoming-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// ValidKey reports whether key has the shape Put produces (no traversal possible).
func ValidKey(key string) bool { return validKey.MatchString(key) }

// ContentHash is the SHA-256 part of a valid key.
func ContentHash(key string) string {
	base := filepath.Base(key)
	return base[:len(base)-len(filepath.Ext(base))]
}
