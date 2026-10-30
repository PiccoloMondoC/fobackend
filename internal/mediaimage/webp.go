// Package mediaimage implements bounded, metadata-free presentation encodings.
// The Go service remains CGO-free. The fixed, installed codec is invoked directly,
// never through a shell, and receives only a canonical decoded PNG.
// focodebase/fobackend/internal/mediaimage/webp.go
package mediaimage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"os/exec"
	"strconv"
	"time"
)

const Executable = "/usr/bin/cwebp"
const MaxOutputBytes = 8 << 20
var ErrUnavailable = errors.New("image encoder unavailable")

// limitedOutput prevents an encoder fault from growing the service heap without
// a bound. An io.Writer failure makes os/exec terminate the pipe copy.
type limitedOutput struct { bytes.Buffer }
func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > MaxOutputBytes-w.Len() { return 0, ErrUnavailable }
	return w.Buffer.Write(p)
}

// EncodePNG preserves alpha and aspect ratio supplied by the caller. Dimensions
// are exact; callers must already have applied the no-upscaling size policy.
func EncodePNG(ctx context.Context, canonical []byte, width, height int) ([]byte, error) {
	if ctx == nil || width < 1 || height < 1 || width > 2048 || height > 2048 || len(canonical)==0 || len(canonical)>65<<20 {
		return nil, ErrUnavailable
	}
	if err:=ctx.Err();err!=nil{return nil,err}
	// No supplied names, paths or original metadata reach the command line.
	// Pipe canonical pixels over stdin; no temporary image files can survive a
	// process crash, cancellation or failed encode.
	run,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
	cmd:=exec.CommandContext(run,Executable,"-quiet","-q","82","-m","4","-low_memory","-metadata","none","-resize",strconv.Itoa(width),strconv.Itoa(height),"-o","-","--","-")
	cmd.Stdin=bytes.NewReader(canonical)
	cmd.WaitDelay=time.Second
	var output limitedOutput
	cmd.Stdout=&output
	// Codec diagnostics can include paths or input details. They never enter
	// public errors, logs or retained metadata.
	cmd.Stderr=io.Discard
	if err:=cmd.Run();err!=nil{if run.Err()!=nil{return nil,run.Err()};return nil,ErrUnavailable}
	raw:=output.Bytes()
	if !validWebP(raw,width,height){return nil,ErrUnavailable}
	return raw,nil
}

// validWebP checks the codec's RIFF envelope, still-image dimensions and absence
// of metadata/animation chunks. The trusted codec, not this framing check, owns
// compression. Nothing supplied by a merchant can bypass PNG decoding first.
func validWebP(raw []byte,width,height int)bool{
	if len(raw)<20 || string(raw[:4])!="RIFF" || string(raw[8:12])!="WEBP" || uint64(binary.LittleEndian.Uint32(raw[4:8]))+8!=uint64(len(raw)){return false}
	w,h,frames:=0,0,0
	for pos:=12;pos<len(raw);{
		if len(raw)-pos<8{return false};tag:=string(raw[pos:pos+4]);size:=uint64(binary.LittleEndian.Uint32(raw[pos+4:pos+8]));start:=pos+8
		end:=uint64(start)+size;if end>uint64(len(raw)){return false};chunk:=raw[start:int(end)]
		switch tag{
		case "VP8X":
			if len(chunk)!=10 || chunk[0]&^byte(0x10)!=0 || chunk[1]!=0 || chunk[2]!=0 || chunk[3]!=0 {return false}
			w=1+int(chunk[4])+(int(chunk[5])<<8)+(int(chunk[6])<<16)
			h=1+int(chunk[7])+(int(chunk[8])<<8)+(int(chunk[9])<<16)
		case "VP8 ":
			if len(chunk)<10||!bytes.Equal(chunk[3:6],[]byte{0x9d,0x01,0x2a}){return false};frames++
			cw:=int(binary.LittleEndian.Uint16(chunk[6:8])&0x3fff);ch:=int(binary.LittleEndian.Uint16(chunk[8:10])&0x3fff)
			if w!=0&&(w!=cw||h!=ch){return false};w,h=cw,ch
		case "VP8L":
			if len(chunk)<5||chunk[0]!=0x2f{return false};frames++
			bits:=binary.LittleEndian.Uint32(chunk[1:5]);cw:=1+int(bits&0x3fff);ch:=1+int((bits>>14)&0x3fff)
			if w!=0&&(w!=cw||h!=ch){return false};w,h=cw,ch
		case "ALPH": // Pixel alpha is retained; EXIF/ICC/XMP/animation are not.
		default:return false
		}
		padded:=end+(size&1);if padded>uint64(len(raw)){return false};pos=int(padded)
	}
	return frames==1&&w==width&&h==height
}

// Check runs a real tiny encode during startup. A missing or broken dependency
// fails deployment explicitly rather than accepting uploads without WebP output.
func Check(ctx context.Context)error{
	var source bytes.Buffer
	if err:=png.Encode(&source,image.NewNRGBA(image.Rect(0,0,1,1)));err!=nil{return ErrUnavailable}
	_,err:=EncodePNG(ctx,source.Bytes(),1,1);return err
}
