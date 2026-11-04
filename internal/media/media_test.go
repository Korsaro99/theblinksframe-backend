package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"slices"
	"testing"
)

// testJPEG makes a w×h JPEG whose left half is red and right half blue, so rotation shows.
func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{R: 255, A: 255}
			if x >= w/2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withExif inserts an APP1 Exif segment with the given orientation and a fake GPS tag.
func withExif(jpg []byte, orientation uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08") // big-endian, first directory at 8
	entries := []struct {
		tag, typ uint16
		value    uint32
	}{
		{0x0112, 3, uint32(orientation) << 16}, // Orientation, SHORT, left-aligned
		{0x8825, 4, 0},                         // GPS directory pointer: its presence is what matters
	}
	tiff = binary.BigEndian.AppendUint16(tiff, uint16(len(entries)))
	for _, e := range entries {
		tiff = binary.BigEndian.AppendUint16(tiff, e.tag)
		tiff = binary.BigEndian.AppendUint16(tiff, e.typ)
		tiff = binary.BigEndian.AppendUint32(tiff, 1)
		tiff = binary.BigEndian.AppendUint32(tiff, e.value)
	}
	tiff = append(tiff, 0, 0, 0, 0)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1}
	app1 = binary.BigEndian.AppendUint16(app1, uint16(len(seg)+2))
	app1 = append(app1, seg...)
	return slices.Concat(jpg[:2], app1, jpg[2:])
}

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func isRed(c color.Color) bool { r, g, b, _ := c.RGBA(); return r > 0xc000 && g < 0x4000 && b < 0x4000 }
func isBlue(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return b > 0xc000 && r < 0x4000 && g < 0x4000
}

func TestProcessPhotoSizes(t *testing.T) {
	tests := []struct {
		w, h       int
		wantWidths []int
	}{
		{3000, 2000, []int{400, 800, 1600}},
		{1200, 900, []int{400, 800, 1200}},
		{800, 600, []int{400, 800}},
		{500, 500, []int{400, 500}},
		{400, 400, []int{400}},
	}
	for _, tt := range tests {
		out, err := ProcessPhoto(testJPEG(t, tt.w, tt.h))
		if err != nil {
			t.Fatalf("%dx%d: %v", tt.w, tt.h, err)
		}
		var widths []int
		for _, v := range out {
			widths = append(widths, v.Width)
			if want := tt.h * v.Width / tt.w; v.Height != want {
				t.Errorf("%dx%d: variant %d is %d high, want %d", tt.w, tt.h, v.Width, v.Height, want)
			}
		}
		if !slices.Equal(widths, tt.wantWidths) {
			t.Errorf("%dx%d: widths %v, want %v", tt.w, tt.h, widths, tt.wantWidths)
		}
	}
}

func TestProcessPhotoTurnsPhonePhotosUpright(t *testing.T) {
	// Stored 1000×600 on its side, with orientation 6: shown as 600×1000, turned clockwise,
	// so the red left half ends up on top.
	out, err := ProcessPhoto(withExif(testJPEG(t, 1000, 600), 6))
	if err != nil {
		t.Fatal(err)
	}
	top := out[len(out)-1]
	if top.Width != 600 || top.Height != 1000 {
		t.Fatalf("size %dx%d, want 600x1000", top.Width, top.Height)
	}
	img := decode(t, top.JPEG)
	if !isRed(img.At(300, 100)) || !isBlue(img.At(300, 900)) {
		t.Errorf("not turned clockwise: top %v, bottom %v", img.At(300, 100), img.At(300, 900))
	}
	for _, v := range out {
		if bytes.Contains(v.JPEG, []byte("Exif")) {
			t.Errorf("variant %d still has EXIF (and could have the GPS location)", v.Width)
		}
	}

	out, _ = ProcessPhoto(withExif(testJPEG(t, 1000, 600), 3)) // upside down
	img = decode(t, out[len(out)-1].JPEG)
	if !isBlue(img.At(100, 300)) || !isRed(img.At(900, 300)) {
		t.Errorf("not turned upside down: left %v, right %v", img.At(100, 300), img.At(900, 300))
	}
}

func TestProcessPhotoPutsTransparencyOnWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 600, 600)) // fully transparent
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	out, err := ProcessPhoto(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decode(t, out[0].JPEG).At(10, 10).RGBA()
	if r < 0xf000 || g < 0xf000 || b < 0xf000 {
		t.Errorf("transparent pixel became %x %x %x, want white", r, g, b)
	}
}

func TestProcessPhotoRefuses(t *testing.T) {
	bomb := testJPEG(t, 400, 400)
	// Claim 30000×30000 in the frame header (SOF0): the size is read before any decoding.
	if i := bytes.Index(bomb, []byte{0xFF, 0xC0}); i > 0 {
		binary.BigEndian.PutUint16(bomb[i+5:], 30000)
		binary.BigEndian.PutUint16(bomb[i+7:], 30000)
	}
	tests := map[string][]byte{
		"html named .jpg": []byte("<!doctype html><script>alert(1)</script>"),
		"pdf":             []byte("%PDF-1.7\n..."),
		"too small":       testJPEG(t, 600, 300),
		"over 24 MP":      bomb,
		"truncated":       testJPEG(t, 800, 800)[:300],
	}
	for name, data := range tests {
		if _, err := ProcessPhoto(data); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", name, err)
		}
	}
}

func TestJPEGOrientationIgnoresBrokenExif(t *testing.T) {
	jpg := testJPEG(t, 400, 400)
	broken := withExif(jpg, 6)
	broken[len(jpg[:2])+4+6+4] = 0xFF // point the first directory far past the end
	for name, data := range map[string][]byte{"none": jpg, "broken": broken, "short": {0xFF, 0xD8, 0xFF}} {
		if got := jpegOrientation(data); got != 1 {
			t.Errorf("%s: orientation %d, want 1", name, got)
		}
	}
	if got := jpegOrientation(withExif(jpg, 8)); got != 8 {
		t.Errorf("orientation %d, want 8", got)
	}
}

// box builds an ISO base media box.
func box(name string, payload ...[]byte) []byte {
	body := slices.Concat(payload...)
	b := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	return append(append(b, name...), body...)
}

// tkhd builds a version-0 track header with the given size and a 90° turn when rotated.
func tkhd(w, h int, rotated bool) []byte {
	p := make([]byte, 4+20+16+36+8)
	m := p[40:76]
	a, b, c, d := uint32(0x00010000), uint32(0), uint32(0), uint32(0x00010000)
	if rotated {
		a, b, c, d = 0, 0x00010000, 0xFFFF0000, 0
	}
	binary.BigEndian.PutUint32(m[0:], a)
	binary.BigEndian.PutUint32(m[4:], b)
	binary.BigEndian.PutUint32(m[12:], c)
	binary.BigEndian.PutUint32(m[16:], d)
	binary.BigEndian.PutUint32(m[32:], 0x40000000)
	binary.BigEndian.PutUint32(p[76:], uint32(w)<<16)
	binary.BigEndian.PutUint32(p[80:], uint32(h)<<16)
	return box("tkhd", p)
}

// TestVideo is a minimal MP4 for tests elsewhere: an audio track, then a 1920×1080 video
// track recorded on its side (shown 1080×1920).
func testVideo(brand string) []byte {
	return slices.Concat(
		box("ftyp", []byte(brand), []byte{0, 0, 0, 0}),
		box("mdat", make([]byte, 64)),
		box("moov", box("mvhd", make([]byte, 100)),
			box("trak", tkhd(0, 0, false)),
			box("trak", tkhd(1920, 1080, true))),
	)
}

func TestProbeVideo(t *testing.T) {
	v, err := ProbeVideo(testVideo("isom"))
	if err != nil || v.ContentType != "video/mp4" || v.Width != 1080 || v.Height != 1920 {
		t.Errorf("mp4: %+v, %v; want video/mp4 1080x1920", v, err)
	}
	if v, _ := ProbeVideo(testVideo("qt  ")); v.ContentType != "video/quicktime" {
		t.Errorf("mov: %s, want video/quicktime", v.ContentType)
	}

	noVideo := slices.Concat(box("ftyp", []byte("isom"), []byte{0, 0, 0, 0}), box("moov", box("trak", tkhd(0, 0, false))))
	lying := box("ftyp", []byte("isom"))
	binary.BigEndian.PutUint32(lying, 1<<30) // a box claiming to be 1 GB long
	tests := map[string][]byte{
		"not a video":  []byte("RIFF....AVI LIST"),
		"no moov":      box("ftyp", []byte("isom"), []byte{0, 0, 0, 0}),
		"only audio":   noVideo,
		"lying length": append(lying, box("moov")...),
		"truncated":    testVideo("isom")[:60],
	}
	for name, data := range tests {
		if _, err := ProbeVideo(data); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", name, err)
		}
	}
}
