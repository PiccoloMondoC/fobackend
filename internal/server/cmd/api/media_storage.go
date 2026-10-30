// Package main composes environment-specific private media storage before serving requests.
// focodebase/fobackend/internal/server/cmd/api/media_storage.go
package main

import (
	"context"
	"os"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
 "github.com/PiccoloMondoC/focodebase/fobackend/internal/mediaimage"
)

func newMediaStorage(ctx context.Context) (mediastore.Store, error) {
	return newMediaStorageWithCodec(ctx,mediaimage.Check)
}

// Internal injection keeps storage policy tests independent of an installed
// executable. Production always passes the real encoder health check above.
func newMediaStorageWithCodec(ctx context.Context,check func(context.Context)error) (mediastore.Store, error) {
	if ctx==nil||check==nil{return nil,mediaimage.ErrUnavailable}
 checkCodec,cancelCodec:=context.WithTimeout(ctx,20*time.Second)
 err:=check(checkCodec)
 cancelCodec()
 if err!=nil{return nil,mediaimage.ErrUnavailable}

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
