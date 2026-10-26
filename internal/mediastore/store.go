// Package mediastore provides private immutable object storage independent of profile business rules.
// focodebase/fobackend/internal/mediastore/store.go
package mediastore

import (
	"context"
	"errors"
	"io"
	"regexp"
)

var (
	ErrNotFound    = errors.New("media object not found")
	ErrUnavailable = errors.New("media storage unavailable")
	ErrInvalidKey  = errors.New("invalid media object key")
	keyPattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// Store never authorizes users or accepts a user-supplied path. The service
// generates a fresh UUID for each object and stores its ownership in PostgreSQL.
// Put is create-only. It may leave bytes after an interrupted write; callers must
// durably register the key BEFORE Put and reconcile uncertain outcomes by key.
// Future media workflows can stream through this boundary with their own limits.
// Callers must supply a bounded context and close every returned reader.
type Store interface {
	Location() (driver, location string)
	Put(context.Context, string, string, io.Reader) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error // missing objects count as successfully deleted
	Close() error
}

func validKey(key string) bool { return keyPattern.MatchString(key) }

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}
