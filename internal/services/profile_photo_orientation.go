// Package services preserves JPEG display orientation while discarding uploaded photo metadata.
// focodebase/fobackend/internal/services/profile_photo_orientation.go
package services

import (
	"encoding/binary"
	"image"
)

// Read only a bounded EXIF orientation tag. Offsets, entry counts and segment
// lengths are checked before access; malformed or absent EXIF means normal
// orientation. No other EXIF field is retained or interpreted.
func profilePhotoOrientation(raw []byte) int {
	if len(raw) < 4 || raw[0] != 0xff || raw[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(raw); {
		if raw[offset] != 0xff {
			return 1
		}
		offset++
		for offset < len(raw) && raw[offset] == 0xff {
			offset++
		}
		if offset >= len(raw) {
			return 1
		}
		marker := raw[offset]
		offset++
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if offset+2 > len(raw) {
			return 1
		}
		size := int(binary.BigEndian.Uint16(raw[offset : offset+2]))
		if size < 2 || size > len(raw)-offset {
			return 1
		}
		segment := raw[offset+2 : offset+size]
		offset += size
		if marker != 0xe1 || len(segment) < 14 || string(segment[:6]) != "Exif\x00\x00" {
			continue
		}
		tiff := segment[6:]
		var order binary.ByteOrder
		switch string(tiff[:2]) {
		case "II":
			order = binary.LittleEndian
		case "MM":
			order = binary.BigEndian
		default:
			return 1
		}
		if order.Uint16(tiff[2:4]) != 42 {
			return 1
		}
		first := uint64(order.Uint32(tiff[4:8]))
		if first+2 > uint64(len(tiff)) {
			return 1
		}
		count := uint64(order.Uint16(tiff[first : first+2]))
		first += 2
		if count > (uint64(len(tiff))-first)/12 {
			return 1
		}
		for i := uint64(0); i < count; i++ {
			entry := tiff[first+i*12 : first+(i+1)*12]
			if order.Uint16(entry[:2]) != 0x0112 {
				continue
			}
			if order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
				return 1
			}
			value := int(order.Uint16(entry[8:10]))
			if value >= 1 && value <= 8 {
				return value
			}
			return 1
		}
	}
	return 1
}

func orientProfilePhoto(source *image.RGBA, orientation int) *image.RGBA {
	if orientation <= 1 || orientation > 8 {
		return source
	}
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dest := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = w - 1 - x
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dy = h - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dest.SetRGBA(dx, dy, source.RGBAAt(x, y))
		}
	}
	return dest
}
