package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/catalog"
)

// writeCatalogError answers a catalog.Store error with the right problem response.
// Every catalogue handler uses it, so the same error always gets the same status and code.
func (s *Server) writeCatalogError(c *gin.Context, err error) {
	var invalid *catalog.ValidationError
	switch {
	case errors.As(err, &invalid):
		fields := make([]FieldProblem, len(invalid.Fields))
		for i, f := range invalid.Fields {
			fields[i] = FieldProblem{Field: f.Field, Message: f.Message}
		}
		WriteProblem(c, http.StatusUnprocessableEntity, "VALIDATION", "Some details are missing or not valid.", fields...)
	case errors.Is(err, catalog.ErrNotFound):
		WriteProblem(c, http.StatusNotFound, "NOT_FOUND", "We couldn't find that. It may have been deleted.")
	case errors.Is(err, catalog.ErrDuplicate):
		WriteProblem(c, http.StatusConflict, "DUPLICATE", "That name or web address is already used. Choose another.")
	case errors.Is(err, catalog.ErrBrandHasProducts):
		WriteProblem(c, http.StatusConflict, "BRAND_HAS_PRODUCTS", "This brand still has products. Move or delete them first.")
	case errors.Is(err, catalog.ErrProductHasOrders):
		WriteProblem(c, http.StatusConflict, "PRODUCT_HAS_ORDERS", "This product has orders, so it can't be deleted. Hide it instead.")
	case errors.Is(err, catalog.ErrBrandMissing):
		WriteProblem(c, http.StatusUnprocessableEntity, "VALIDATION", "Some details are missing or not valid.",
			FieldProblem{Field: "brandId", Message: "Choose a brand from the list."})
	default:
		s.internalError(c, "catalogue request failed", err)
	}
}
