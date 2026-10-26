// Package main composes environment-specific private media storage before serving requests.
// focodebase/fobackend/internal/server/cmd/api/media_storage.go
package main

import (
	"context"
	"os"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
)

func newMediaStorage(ctx context.Context) (mediastore.Store, error) {
	driver := os.Getenv("MEDIA_STORAGE_DRIVER")
	cloudRun := os.Getenv("K_SERVICE") != ""
	if driver == "" && !cloudRun {
		driver = "filesystem"
	}
	// A Cloud Run revision may never silently fall back to ephemeral disk.
	if cloudRun && driver != "gcs" {
		return nil, mediastore.ErrUnavailable
	}
	switch driver {
	case "filesystem":
		root := os.Getenv("MEDIA_ROOT")
		if root == "" {
			root = "/app/media"
		}
		return mediastore.NewFilesystem(root)
	case "gcs":
		store, err := mediastore.NewGCS(os.Getenv("MEDIA_GCS_BUCKET"))
		if err != nil {
			return nil, err
		}
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err = store.Validate(check); err != nil {
			_ = store.Close()
			return nil, err
		}
		return store, nil
	default:
		return nil, mediastore.ErrUnavailable
	}
}
