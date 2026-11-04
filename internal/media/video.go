package media

import (
	"encoding/binary"
)

// maxVideoBytes is the largest video accepted. Videos are stored as uploaded: the free VM can't
// convert them (TODO: ffmpeg in its own container once the VM has more CPU and memory).
const maxVideoBytes = 50 << 20

// Video is what an upload's header says about it.
type Video struct {
	ContentType   string // video/mp4 or video/quicktime
	Width, Height int    // as shown, after the track's rotation
}

// ProbeVideo checks that data is an MP4 or MOV file with a video track and returns its size.
// It walks the file's boxes (ISO base media format) to the first video track header ("tkhd"),
// checking every size against the data, and never decodes the video itself.
func ProbeVideo(data []byte) (Video, error) {
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		return Video{}, unsupported("Upload an MP4 or MOV video.")
	}
	v := Video{ContentType: "video/mp4"}
	if string(data[8:12]) == "qt  " {
		v.ContentType = "video/quicktime"
	}
	moov := findBox(data, "moov")
	if moov == nil {
		return Video{}, unsupported("This video can't be read. Export it again as MP4.")
	}
	for trak := range boxes(moov, "trak") {
		tkhd := findBox(trak, "tkhd")
		if w, h, ok := trackSize(tkhd); ok {
			v.Width, v.Height = w, h
			return v, nil
		}
	}
	return Video{}, unsupported("This file has no video in it.")
}

// boxes yields the payload of every box named name directly inside data.
func boxes(data []byte, name string) func(yield func([]byte) bool) {
	return func(yield func([]byte) bool) {
		for i := 0; i+8 <= len(data); {
			size := int(binary.BigEndian.Uint32(data[i:]))
			header := 8
			switch size {
			case 0: // runs to the end
				size = len(data) - i
			case 1: // 64-bit size follows
				if i+16 > len(data) {
					return
				}
				big := binary.BigEndian.Uint64(data[i+8:])
				if big > uint64(len(data)-i) {
					return
				}
				size, header = int(big), 16 //nolint:gosec // big <= len(data)-i, checked just above
			}
			if size < header || i+size > len(data) {
				return
			}
			if string(data[i+4:i+8]) == name && !yield(data[i+header:i+size]) {
				return
			}
			i += size
		}
	}
}

func findBox(data []byte, name string) []byte {
	for b := range boxes(data, name) {
		return b
	}
	return nil
}

// trackSize reads a track header's width and height (16.16 fixed point), swapped when its
// matrix turns the picture on its side, as phones do for portrait videos. Audio tracks have
// no size and return false.
func trackSize(tkhd []byte) (w, h int, ok bool) {
	if len(tkhd) < 4 {
		return 0, 0, false
	}
	// After version and flags: times, track id and duration, 20 bytes in version 0 and 32
	// in version 1; then 8 reserved, 8 of layer/group/volume, the 36-byte matrix, the size.
	matrix := 4 + 20 + 16
	if tkhd[0] == 1 {
		matrix = 4 + 32 + 16
	}
	if len(tkhd) < matrix+36+8 {
		return 0, 0, false
	}
	w = int(binary.BigEndian.Uint32(tkhd[matrix+36:]) >> 16)
	h = int(binary.BigEndian.Uint32(tkhd[matrix+40:]) >> 16)
	if w == 0 || h == 0 {
		return 0, 0, false
	}
	a := binary.BigEndian.Uint32(tkhd[matrix:])
	d := binary.BigEndian.Uint32(tkhd[matrix+16:])
	if a == 0 && d == 0 { // a 90° or 270° turn
		w, h = h, w
	}
	return w, h, true
}
