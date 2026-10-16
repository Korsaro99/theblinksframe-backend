package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/httpapi/gen"
)

// GetMenu implements GET /catalog/menu.
func (s *Server) GetMenu(c *gin.Context) {
	menu, err := s.catalog.Menu(c.Request.Context())
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenMenu(menu))
}

func toGenMenu(m catalog.Menu) gen.Menu {
	return gen.Menu{
		Men:         toGenMenuSection(m.Men),
		Women:       toGenMenuSection(m.Women),
		Accessories: gen.MenuTotal{Total: m.AccessoriesTotal},
		Brands:      toGenMenuCounts(m.Brands),
	}
}

func toGenMenuSection(sec catalog.MenuSection) gen.MenuSection {
	return gen.MenuSection{
		Total:  sec.Total,
		Brands: toGenMenuCounts(sec.Brands),
		Styles: toGenMenuCounts(sec.Styles),
		Bands:  toGenMenuCounts(sec.Bands),
	}
}

func toGenMenuCounts(in []catalog.MenuCount) []gen.MenuCount {
	// Empty slice, never null: the menus always have a list to iterate.
	out := make([]gen.MenuCount, len(in))
	for i, c := range in {
		out[i] = gen.MenuCount{Key: c.Key, Label: c.Label, Count: c.Count}
	}
	return out
}
