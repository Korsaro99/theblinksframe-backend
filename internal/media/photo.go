package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // registers the PNG decoder
	"net/http"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers the WebP decoder
)

// Photo limits. The VM has 1 GB of memory, shared with Postgres and the shop: a 24 MP JPEG
// decodes to about 36 MB (YCbCr), and only one upload is processed at a time.
const (
	maxPhotoBytes  = 15 << 20
	maxPhotoPixels = 24_000_000
	minPhotoSide   = 400
	jpegQuality    = 82
)

// variantWidths are the photo sizes made for srcset, smallest first. Only the ones narrower
// than the photo are made, plus the photo's own width when it is under the largest.
var variantWidths = []int{400, 800, 1600}

// ErrUnsupported means the file is not a photo or video we accept. Its message is for people.
var ErrUnsupported = errors.New("unsupported file")

// unsupported wraps ErrUnsupported with a message to show.
func unsupported(msg string) error { return fmt.Errorf("%w: %s", ErrUnsupported, msg) }

// Encoded is one resized copy of a photo, as JPEG.
type Encoded struct {
	Width, Height int
	JPEG          []byte
}

// ProcessPhoto checks a JPEG, PNG or WebP and returns its variants, smallest first. The output
// is upright (the phone's EXIF rotation applied), on white where the photo was transparent,
// and has no metadata at all: re-encoding drops EXIF, including the GPS location.
func ProcessPhoto(data []byte) ([]Encoded, error) {
	kind := http.DetectContentType(data) // from the bytes, never the file name or header
	switch kind {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, unsupported("Upload a JPEG, PNG or WebP photo.")
	}
	// Read the size before decoding: a small file can claim enormous dimensions (a
	// "decompression bomb") and would otherwise use up the memory.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, unsupported("This photo can't be read. Try saving it again as JPEG.")
	}
	if cfg.Width*cfg.Height > maxPhotoPixels {
		return nil, unsupported("This photo is over 24 megapixels. Resize it and try again.")
	}
	if min(cfg.Width, cfg.Height) < minPhotoSide {
		return nil, unsupported(fmt.Sprintf("This photo is too small: it needs at least %d pixels on each side.", minPhotoSide))
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, unsupported("This photo can't be read. Try saving it again as JPEG.")
	}
	orientation := 1
	if kind == "image/jpeg" {
		orientation = jpegOrientation(data)
	}

	// Width and height as the photo is meant to be seen, after rotation.
	w, h := cfg.Width, cfg.Height
	if orientation >= 5 {
		w, h = h, w
	}
	// Resize the full photo once, to the largest variant, then rotate that small copy (cheaper
	// than rotating the full one); the smaller variants come from it.
	largest := min(w, variantWidths[len(variantWidths)-1])
	top := upright(resize(src, largest, h*largest/w, orientation >= 5), orientation)

	var out []Encoded
	for _, vw := range variantWidths {
		if vw >= largest {
			break
		}
		if e, err := encode(resize(top, vw, h*vw/w, false)); err == nil {
			out = append(out, e)
		} else {
			return nil, err
		}
	}
	e, err := encode(top)
	if err != nil {
		return nil, err
	}
	return append(out, e), nil
}

// resize scales src to width×height on a white background (transparent pixels become white).
// When sideways is true the photo is still on its side, so the target is transposed.
func resize(src image.Image, width, height int, sideways bool) *image.RGBA {
	if sideways {
		width, height = height, width
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func encode(img image.Image) (Encoded, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return Encoded{}, fmt.Errorf("encode jpeg: %w", err)
	}
	b := img.Bounds()
	return Encoded{Width: b.Dx(), Height: b.Dy(), JPEG: buf.Bytes()}, nil
}

// upright applies an EXIF orientation (1–8) to img.
func upright(img *image.RGBA, orientation int) *image.RGBA {
	if orientation <= 1 || orientation > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if orientation >= 5 {
		ow, oh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := range h {
		for x := range w {
			var nx, ny int
			switch orientation {
			case 2: // mirrored
				nx, ny = w-1-x, y
			case 3: // upside down
				nx, ny = w-1-x, h-1-y
			case 4: // upside down, mirrored
				nx, ny = x, h-1-y
			case 5: // on its side, mirrored
				nx, ny = y, x
			case 6: // turned left (the usual portrait phone photo): rotate 90° clockwise
				nx, ny = h-1-y, x
			case 7: // on its side, mirrored the other way
				nx, ny = h-1-y, w-1-x
			case 8: // turned right: rotate 90° anticlockwise
				nx, ny = y, w-1-x
			}
			out.SetRGBA(nx, ny, img.RGBAAt(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}
