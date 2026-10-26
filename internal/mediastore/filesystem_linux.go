// Package mediastore implements private persistent local media storage for Linux development containers.
// focodebase/fobackend/internal/mediastore/filesystem_linux.go
package mediastore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Filesystem pins the provisioned directory by descriptor. All operations are
// single-component openat/unlinkat calls; symlinks and path traversal cannot
// redirect reads or writes. Linux is the established backend deployment target.
type Filesystem struct {
	directory *os.File
	location  string
}

func NewFilesystem(root string) (*Filesystem, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, ErrUnavailable
	}
	fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Filesystem{directory: os.NewFile(uintptr(fd), root), location: root}, nil
}

func (s *Filesystem) Location() (string, string) { return "filesystem", s.location }
func (s *Filesystem) Close() error             { return s.directory.Close() }

func (s *Filesystem) Put(ctx context.Context, key, contentType string, source io.Reader) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := syscall.Openat(int(s.directory.Fd()), key, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), key)
	// Partial objects stay registered in the durable staging ledger for cleanup.
	_, copyErr := io.Copy(f, contextReader{ctx: ctx, source: source})
	if copyErr == nil {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return ErrUnavailable
	}
	if err = s.directory.Sync(); err != nil {
		return ErrUnavailable
	}
	return ctx.Err()
}

func (s *Filesystem) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validKey(key) {
		return nil, ErrInvalidKey
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := syscall.Openat(int(s.directory.Fd()), key, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), key)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, ErrUnavailable
	}
	return f, nil
}

func (s *Filesystem) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := syscall.Unlinkat(int(s.directory.Fd()), key)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return ErrUnavailable
	}
	if err = s.directory.Sync(); err != nil {
		return ErrUnavailable
	}
	return nil
}
