// Package storage stores blobs (CI logs now; artifacts, caches and release
// assets later) behind an interface so a deployment can use local disk or,
// later, S3-compatible object storage.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotFound is returned for missing keys.
var ErrNotFound = errors.New("storage: not found")

// Store is a flat key/value blob store. Keys are slash-separated paths.
type Store interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	// DeletePrefix removes every key starting with prefix + "/".
	DeletePrefix(ctx context.Context, prefix string) error
}

// Local stores blobs as files under a directory.
type Local struct{ Root string }

// NewLocal creates the root directory if needed.
func NewLocal(root string) (*Local, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &Local{Root: root}, nil
}

func (l *Local) path(key string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean("/" + key))
	if clean == "/" || strings.Contains(key, "..") || strings.Contains(key, "\\") {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	return filepath.Join(l.Root, filepath.FromSlash(clean[1:])), nil
}

// Put writes the blob atomically (temp file + rename).
func (l *Local) Put(_ context.Context, key string, r io.Reader) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}

func (l *Local) Get(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (l *Local) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (l *Local) DeletePrefix(_ context.Context, prefix string) error {
	p, err := l.path(prefix)
	if err != nil {
		return err
	}
	return os.RemoveAll(p)
}
