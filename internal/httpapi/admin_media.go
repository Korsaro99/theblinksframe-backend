package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/media"
	"theblinksframe-backend/internal/models"
)

// uploadReadTime is how long an upload's body may take to arrive: 50 MB over slow mobile data
// takes minutes, far longer than the server's 30-second read timeout for other requests.
const uploadReadTime = 10 * time.Minute

// AdminUploadProductMedia implements POST /admin/products/{productId}/media. The body is the
// file itself; the validator lifted the size limit to the spec's x-upload-max-bytes.
func (s *Server) AdminUploadProductMedia(c *gin.Context, productID openapi_types.UUID, params gen.AdminUploadProductMediaParams) {
	_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now().Add(uploadReadTime))
	role := media.RoleOther
	if params.Role != nil {
		role = string(*params.Role)
	}
	m, err := s.media.Upload(c.Request.Context(), productID, role, c.Request.Body)
	if err != nil {
		s.writeMediaError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toGenMedia(m))
}

// AdminPatchProductMedia implements PATCH /admin/products/{productId}/media/{mediaId}.
func (s *Server) AdminPatchProductMedia(c *gin.Context, productID, mediaID openapi_types.UUID) {
	var req gen.MediaPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	var role *string
	if req.Role != nil {
		r := string(*req.Role)
		role = &r
	}
	m, err := s.media.Patch(c.Request.Context(), productID, mediaID, role, req.Position)
	if err != nil {
		s.writeMediaError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenMedia(m))
}

// AdminDeleteProductMedia implements DELETE /admin/products/{productId}/media/{mediaId}.
func (s *Server) AdminDeleteProductMedia(c *gin.Context, productID, mediaID openapi_types.UUID) {
	if err := s.media.Delete(c.Request.Context(), productID, mediaID); err != nil {
		s.writeMediaError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// writeMediaError answers a media.Library error with the right problem response.
func (s *Server) writeMediaError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, media.ErrNotFound):
		WriteProblem(c, http.StatusNotFound, "NOT_FOUND", "We couldn't find that. It may have been deleted.")
	case errors.Is(err, media.ErrUnsupported):
		WriteProblem(c, http.StatusUnprocessableEntity, "UNSUPPORTED_FILE", humanPart(err))
	case errors.Is(err, media.ErrTooLarge):
		WriteProblem(c, http.StatusRequestEntityTooLarge, "TOO_LARGE", humanPart(err))
	case errors.Is(err, media.ErrMediaLimit):
		WriteProblem(c, http.StatusConflict, "MEDIA_LIMIT", "This product already has 12 photos and videos. Delete one first.")
	case errors.Is(err, media.ErrCoverRequired):
		WriteProblem(c, http.StatusConflict, "COVER_REQUIRED",
			"A product on the website needs a cover photo. Make another photo the cover first.")
	case errors.Is(err, media.ErrCoverMustBePhoto):
		WriteProblem(c, http.StatusConflict, "COVER_MUST_BE_PHOTO", "The cover must be a photo, not a video.")
	case errors.Is(err, media.ErrOff), errors.Is(err, media.ErrBusy):
		WriteProblem(c, http.StatusServiceUnavailable, "MEDIA_UNAVAILABLE",
			"Uploads aren't available right now. Please try again in a minute.")
	default:
		s.internalError(c, "media request failed", err)
	}
}

// humanPart is the message after "unsupported file: " or "file too large: ", which the media
// package writes for people.
func humanPart(err error) string {
	msg := err.Error()
	if _, after, ok := strings.Cut(msg, ": "); ok {
		return after
	}
	return msg
}

func toGenMedia(m models.ProductMedia) gen.Media {
	return gen.Media{
		Id: m.ID, Type: gen.MediaType(m.Type), Role: gen.MediaRole(m.Role),
		Url: m.URL, Width: m.Width, Height: m.Height, Variants: toGenVariants(m.Variants),
	}
}

// toGenVariants returns nil (absent in JSON) for media without resized copies.
func toGenVariants(vs models.MediaVariants) *[]gen.ImageVariant {
	if len(vs) == 0 {
		return nil
	}
	out := make([]gen.ImageVariant, len(vs))
	for i, v := range vs {
		out[i] = gen.ImageVariant{Url: v.URL, Width: v.Width, Height: v.Height}
	}
	return &out
}
