// Package services verifies image integrity and bounds buffered delivery.
// focodebase/fobackend/internal/services/future_offering_image_integrity.go
package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"
	"time"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/mediastore"
)

// Retain a bounded image-read lease until HTTP delivery closes its reader.
// Releasing immediately after verification would let slow clients retain an
// unbounded number of fully buffered images. Waiting requests allocate no image.
var offeringImageReads=make(chan struct{},8)
type offeringImageReader struct {io.ReadCloser;release func();once sync.Once}
func(r *offeringImageReader)Close()error{
	err:=r.ReadCloser.Close();r.once.Do(r.release);return err
}
func acquireOfferingImageRead(ctx context.Context)(func(),error){
	if err:=ctx.Err();err!=nil{return nil,err}
	wait,cancel:=context.WithTimeout(ctx,2*time.Second);defer cancel()
	select{
	case offeringImageReads<-struct{}{}:return func(){<-offeringImageReads},nil
	case <-wait.Done():if ctx.Err()!=nil{return nil,ctx.Err()};return nil,mediastore.ErrUnavailable
	}
}

// Image bytes are verified before HTTP headers or body bytes are emitted. This
// includes private originals. Video retains its existing bounded range stream.
func verifiedOfferingImage(source io.ReadCloser,meta data.FutureOfferingMedia)(io.ReadCloser,error){
	if source==nil{return nil,mediastore.ErrUnavailable}
	if meta.ByteSize<1||meta.ByteSize>32<<20||len(meta.Digest)!=64{_ = source.Close();return nil,mediastore.ErrUnavailable}
	raw,err:=io.ReadAll(io.LimitReader(source,meta.ByteSize+1))
	closeErr:=source.Close()
	if err!=nil||closeErr!=nil||int64(len(raw))!=meta.ByteSize{return nil,mediastore.ErrUnavailable}
	sum:=sha256.Sum256(raw)
	if hex.EncodeToString(sum[:])!=meta.Digest{return nil,mediastore.ErrUnavailable}
	return io.NopCloser(bytes.NewReader(raw)),nil
}
