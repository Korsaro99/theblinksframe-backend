package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/orders"
)

// AdminListOrders implements GET /admin/orders.
func (s *Server) AdminListOrders(c *gin.Context, params gen.AdminListOrdersParams) {
	f := orders.ListFilter{NeedsAttention: params.NeedsAttention}
	if params.Status != nil {
		for _, st := range *params.Status {
			f.Statuses = append(f.Statuses, orders.Status(st))
		}
	}
	if params.Source != nil {
		f.Source = string(*params.Source)
	}
	if params.Fulfilment != nil {
		f.Fulfilment = orders.Fulfilment(*params.Fulfilment)
	}
	if params.Q != nil {
		f.Query = *params.Q
	}
	if params.Page != nil {
		f.Page = *params.Page
	}
	if params.PageSize != nil {
		f.PageSize = *params.PageSize
	}

	list, total, err := s.orders.List(c.Request.Context(), f)
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	items := make([]gen.AdminOrderSummary, len(list))
	for i, o := range list {
		items[i] = toGenAdminOrderSummary(o)
	}
	c.JSON(http.StatusOK, gen.AdminOrderSummaryList{Items: items, Total: int(total)})
}

// AdminGetOrderPipeline implements GET /admin/orders/pipeline.
func (s *Server) AdminGetOrderPipeline(c *gin.Context) {
	p, err := s.orders.Pipeline(c.Request.Context(), time.Now())
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, gen.OrderPipeline{
		AwaitingPayment: p.AwaitingPayment, Paid: p.Paid, Packed: p.Packed, OutForDelivery: p.OutForDelivery,
		ReadyForPickup: p.ReadyForPickup, DeliveredThisWeek: p.DeliveredThisWeek,
		NeedsAttention: p.NeedsAttention, ToDo: p.ToDo,
	})
}

func toGenAdminOrderSummary(o orders.Summary) gen.AdminOrderSummary {
	return gen.AdminOrderSummary{
		Id:                 o.ID,
		Number:             o.Number,
		Source:             gen.OrderSource(o.Source),
		Status:             gen.OrderStatus(o.Status),
		Fulfilment:         gen.Fulfilment(o.Fulfilment),
		Customer:           gen.Customer{Id: o.CustomerID, Name: o.CustomerName, Phone: o.CustomerPhone},
		ItemCount:          o.ItemCount,
		TotalPesewas:       o.TotalPesewas,
		NeedsAttention:     o.NeedsAttention,
		AllowedTransitions: toGenStatuses(o.AllowedTransitions()),
		CreatedAt:          o.CreatedAt.UTC(),
		PaidAt:             utcOrNil(o.PaidAt),
	}
}
