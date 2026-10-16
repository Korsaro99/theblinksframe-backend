package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// ListDeliveryZones implements GET /delivery/zones.
func (s *Server) ListDeliveryZones(c *gin.Context) {
	zones, err := s.delivery.ListActiveZones(c.Request.Context())
	if err != nil {
		s.internalError(c, "list delivery zones", err)
		return
	}
	items := make([]gen.DeliveryZone, len(zones))
	for i, z := range zones {
		items[i] = toGenDeliveryZone(z)
	}
	c.JSON(http.StatusOK, gen.DeliveryZoneList{Items: items, Total: len(items)})
}

// AdminListRiders implements GET /admin/riders.
func (s *Server) AdminListRiders(c *gin.Context) {
	riders, err := s.delivery.ListActiveRiders(c.Request.Context())
	if err != nil {
		s.internalError(c, "list riders", err)
		return
	}
	items := make([]gen.Rider, len(riders))
	for i, r := range riders {
		items[i] = toGenRider(r)
	}
	c.JSON(http.StatusOK, gen.RiderList{Items: items, Total: len(items)})
}

func toGenDeliveryZone(z models.DeliveryZone) gen.DeliveryZone {
	return gen.DeliveryZone{
		Id:               z.ID,
		Name:             z.Name,
		Method:           gen.DeliveryMethod(z.Method),
		FeePesewas:       z.FeePesewas,
		FreeAbovePesewas: z.FreeAbovePesewas,
	}
}

func toGenRider(r models.Rider) gen.Rider {
	return gen.Rider{
		Id:    r.ID,
		Name:  r.Name,
		Phone: r.Phone,
		Kind:  gen.RiderKind(r.Kind),
	}
}
