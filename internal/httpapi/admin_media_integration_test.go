//go:build integration

package httpapi

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Acceptance tests for product photos and videos: upload, change role or position, delete.
// Files go to in-memory storage (api.files); the S3 storage itself is tested against a real
// S3-compatible server in internal/media.

func mediaPath(p models.Product, suffix string) string {
	return "/api/v1/admin/products/" + p.ID.String() + "/media" + suffix
}

// upload sends a file as the request body, the way the admin's file picker will.
func (c *client) upload(path string, data []byte, contentType string) *response {
	return c.doWithHeaders(http.MethodPost, path, string(data), map[string]string{"Content-Type": contentType})
}

func (c *client) uploadOK(p models.Product, query string, data []byte, contentType string) gen.Media {
	c.api.t.Helper()
	var m gen.Media
	c.upload(mediaPath(p, query), data, contentType).wantStatus(http.StatusCreated).decode(&m)
	return m
}

// photo makes a w×h JPEG.
func photo(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xC0
	}
	img.Set(0, 0, color.Black)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// video makes a minimal MP4 with one 1280×720 video track.
func video() []byte {
	b := func(name string, payload ...[]byte) []byte {
		body := slices.Concat(payload...)
		return append(append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), name...), body...)
	}
	tkhd := make([]byte, 84)
	binary.BigEndian.PutUint32(tkhd[40:], 0x00010000) // matrix: not turned
	binary.BigEndian.PutUint32(tkhd[56:], 0x00010000)
	binary.BigEndian.PutUint32(tkhd[76:], 1280<<16)
	binary.BigEndian.PutUint32(tkhd[80:], 720<<16)
	return slices.Concat(b("ftyp", []byte("isom\x00\x00\x02\x00")), b("mdat", make([]byte, 32)),
		b("moov", b("trak", b("tkhd", tkhd))))
}

func TestAdminUploadPhoto(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Status = "DRAFT" })
	owner := api.owner()

	m := owner.uploadOK(p, "?role=COVER", photo(t, 2000, 1500), "image/jpeg")
	if m.Type != gen.PHOTO || m.Role != gen.COVER || m.Width != 1600 || m.Height != 1200 {
		t.Errorf("media = %+v", m)
	}
	if m.Variants == nil || len(*m.Variants) != 3 {
		t.Fatalf("variants = %v, want 400, 800 and 1600", m.Variants)
	}
	prefix := "products/" + p.ID.String() + "/" + m.Id.String()
	for i, w := range []int{400, 800, 1600} {
		v := (*m.Variants)[i]
		if v.Width != w || v.Height != w*3/4 || v.Url != "https://media.test/"+prefix+"/w"+strconv.Itoa(w)+".jpg" {
			t.Errorf("variant %d = %+v", i, v)
		}
	}
	if m.Url != (*m.Variants)[2].Url {
		t.Errorf("url %s, want the largest variant", m.Url)
	}
	if got := api.files.Keys(); len(got) != 3 {
		t.Errorf("public files = %v, want the three sizes only", got)
	}
	if _, ok := api.files.Originals[prefix+"/original"]; !ok {
		t.Error("the original was not kept (privately)")
	}

	// With a cover, the product can go on the website, and its cover has sizes for srcset.
	owner.patch("/api/v1/admin/products/"+p.ID.String(), map[string]any{"status": "PUBLISHED"}).wantStatus(http.StatusOK)
	var page gen.ProductDetail
	api.anon().get("/api/v1/catalog/products/" + p.Slug).wantStatus(http.StatusOK).decode(&page)
	if page.Cover == nil || page.Cover.Variants == nil || len(*page.Cover.Variants) != 3 || len(page.Media) != 1 {
		t.Errorf("storefront page: cover %+v, %d media", page.Cover, len(page.Media))
	}
}

func TestAdminUploadChecksTheFileItself(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	owner := api.owner()

	tests := []struct {
		name        string
		data        []byte
		contentType string
		status      int
		code        string
		detail      string
	}{
		{"a web page named as a JPEG", []byte("<html><script>alert(1)</script></html>"), "image/jpeg", 422, "UNSUPPORTED_FILE", "JPEG, PNG or WebP"},
		{"too small", photo(t, 600, 300), "image/jpeg", 422, "UNSUPPORTED_FILE", "too small"},
		{"photo over 15 MB", append(photo(t, 400, 400), make([]byte, 15<<20)...), "image/jpeg", 413, "TOO_LARGE", "15 MB"},
		{"anything over 50 MB", make([]byte, 51<<20), "video/mp4", 413, "TOO_LARGE", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prob := owner.upload(mediaPath(p, ""), tt.data, tt.contentType).wantProblem(tt.status, tt.code)
			if !strings.Contains(prob.Detail, tt.detail) {
				t.Errorf("detail %q, want it to mention %q", prob.Detail, tt.detail)
			}
		})
	}
	if keys := api.files.Keys(); len(keys) != 0 || api.count("product_media") != 0 {
		t.Errorf("refused uploads left %v and %d rows", keys, api.count("product_media"))
	}

	api.staff().upload(mediaPath(p, ""), photo(t, 800, 800), "image/jpeg").wantProblem(http.StatusForbidden, "FORBIDDEN")
	// Login is checked before the body is read: strangers can't make the server take 50 MB.
	api.anon().upload(mediaPath(p, ""), make([]byte, 51<<20), "video/mp4").wantProblem(http.StatusUnauthorized, "UNAUTHENTICATED")
	owner.upload("/api/v1/admin/products/"+uuid.NewString()+"/media", photo(t, 800, 800), "image/jpeg").
		wantProblem(http.StatusNotFound, "NOT_FOUND")
	owner.upload(mediaPath(p, "?role=FRONT"), photo(t, 800, 800), "image/jpeg").wantStatus(http.StatusBadRequest)
}

func TestAdminUploadVideo(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	owner := api.owner()

	m := owner.uploadOK(p, "?role=WORN", video(), "video/mp4")
	if m.Type != gen.VIDEO || m.Role != gen.WORN || m.Width != 1280 || m.Height != 720 || m.Variants != nil ||
		!strings.HasSuffix(m.Url, "/video.mp4") {
		t.Errorf("video = %+v", m)
	}
	owner.upload(mediaPath(p, "?role=COVER"), video(), "video/mp4").wantProblem(http.StatusConflict, "COVER_MUST_BE_PHOTO")
	owner.patch(mediaPath(p, "/"+m.Id.String()), map[string]any{"role": "COVER"}).
		wantProblem(http.StatusConflict, "COVER_MUST_BE_PHOTO")
}

func TestAdminMediaCoverAndOrder(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta")) // published, and needs a cover from now on
	owner := api.owner()

	first := owner.uploadOK(p, "?role=COVER", photo(t, 800, 800), "image/jpeg")
	second := owner.uploadOK(p, "?role=COVER", photo(t, 800, 800), "image/jpeg")
	roles := func() map[uuid.UUID]gen.MediaRole {
		var ap gen.AdminProduct
		owner.get("/api/v1/admin/products/" + p.ID.String()).wantStatus(http.StatusOK).decode(&ap)
		out := map[uuid.UUID]gen.MediaRole{}
		for _, m := range ap.Media {
			out[m.Id] = m.Role
		}
		return out
	}
	if r := roles(); r[first.Id] != gen.OTHER || r[second.Id] != gen.COVER {
		t.Errorf("after a second cover: %v, want the first moved to OTHER", r)
	}

	var m gen.Media
	owner.patch(mediaPath(p, "/"+first.Id.String()), map[string]any{"role": "COVER", "position": 5}).
		wantStatus(http.StatusOK).decode(&m)
	if r := roles(); r[first.Id] != gen.COVER || r[second.Id] != gen.OTHER {
		t.Errorf("after making the first the cover again: %v", r)
	}

	// A product on the website always keeps a cover.
	owner.patch(mediaPath(p, "/"+first.Id.String()), map[string]any{"role": "SIDE"}).
		wantProblem(http.StatusConflict, "COVER_REQUIRED")
	owner.delete(mediaPath(p, "/"+first.Id.String())).wantProblem(http.StatusConflict, "COVER_REQUIRED")

	owner.delete(mediaPath(p, "/"+second.Id.String())).wantStatus(http.StatusNoContent)
	for _, k := range api.files.Keys() {
		if strings.Contains(k, second.Id.String()) {
			t.Errorf("file %s left after delete", k)
		}
	}
	owner.delete(mediaPath(p, "/"+second.Id.String())).wantProblem(http.StatusNotFound, "NOT_FOUND")
	other := api.product(api.brand("Kente"))
	owner.delete("/api/v1/admin/products/"+other.ID.String()+"/media/"+first.Id.String()).
		wantProblem(http.StatusNotFound, "NOT_FOUND") // media of another product
}

func TestAdminMediaLimit(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	for i := range 12 {
		api.media(p, "OTHER", i)
	}
	api.owner().upload(mediaPath(p, ""), photo(t, 800, 800), "image/jpeg").wantProblem(http.StatusConflict, "MEDIA_LIMIT")
}

func TestAdminDeleteProductDeletesItsFiles(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Status = "DRAFT" })
	owner := api.owner()
	owner.uploadOK(p, "", photo(t, 800, 800), "image/jpeg")
	owner.uploadOK(p, "", video(), "video/mp4")

	owner.delete("/api/v1/admin/products/" + p.ID.String()).wantStatus(http.StatusNoContent)
	if keys := api.files.Keys(); len(keys) != 0 || len(api.files.Originals) != 0 {
		t.Errorf("files left after deleting the product: %v, originals %d", keys, len(api.files.Originals))
	}
}
