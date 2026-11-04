package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/orders"
)

// undoRoute is the Casbin route for undo. canUndo asks the same policy the router enforces,
// so the button and the permission can't disagree.
const undoRoute = "/api/v1/admin/orders/:orderId/undo"

// AdminGetOrder implements GET /admin/orders/{orderId}.
func (s *Server) AdminGetOrder(c *gin.Context, orderID gen.OrderId) {
	o, err := s.orders.Get(c.Request.Context(), orderID)
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, s.toGenAdminOrder(c, o))
}

// AdminTransitionOrder implements POST /admin/orders/{orderId}/transition.
func (s *Server) AdminTransitionOrder(c *gin.Context, orderID gen.OrderId) {
	var req gen.TransitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	p, _ := PrincipalFrom(c) // the route requires a login, checked before the handler
	// TODO(M5): pass req.NotifyCustomer on once the outbox exists; nothing is sent yet.
	o, err := s.orders.Transition(c.Request.Context(), orderID, orders.Move{
		To: orders.Status(req.To), Note: req.Note, RiderID: req.RiderId, ActorID: p.Staff.ID,
	})
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, s.toGenAdminOrder(c, o))
}

// AdminUndoOrderTransition implements POST /admin/orders/{orderId}/undo (OWNER only, by the
// Casbin policy).
func (s *Server) AdminUndoOrderTransition(c *gin.Context, orderID gen.OrderId) {
	p, _ := PrincipalFrom(c)
	o, err := s.orders.Undo(c.Request.Context(), orderID, p.Staff.ID)
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, s.toGenAdminOrder(c, o))
}

// AdminCreateOrder implements POST /admin/orders: a WhatsApp order or a shop sale.
func (s *Server) AdminCreateOrder(c *gin.Context, params gen.AdminCreateOrderParams) {
	var req gen.AdminCreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	p, _ := PrincipalFrom(c)
	in := fromGenManualOrder(req)
	in.ActorID = p.Staff.ID
	if params.IdempotencyKey != nil {
		in.IdempotencyKey = *params.IdempotencyKey
	}
	o, err := s.orders.CreateManual(c.Request.Context(), in)
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusCreated, s.toGenAdminOrder(c, o))
}

// AdminConfirmPayment implements POST /admin/orders/{orderId}/confirm-payment.
func (s *Server) AdminConfirmPayment(c *gin.Context, orderID gen.OrderId) {
	var req gen.ManualPayment
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	p, _ := PrincipalFrom(c)
	o, err := s.orders.ConfirmPayment(c.Request.Context(), orderID, fromGenManualPayment(req), p.Staff.ID)
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, s.toGenAdminOrder(c, o))
}

func fromGenManualOrder(in gen.AdminCreateOrderRequest) orders.ManualOrder {
	out := orders.ManualOrder{
		Source:         string(in.Source),
		CustomerName:   in.Customer.Name,
		CustomerPhone:  in.Customer.Phone,
		Items:          make([]orders.ManualItem, len(in.Items)),
		Fulfilment:     orders.Fulfilment(in.Fulfilment),
		DeliveryZoneID: in.DeliveryZoneId,
		PrivateNote:    in.PrivateNote,
	}
	for i, it := range in.Items {
		out.Items[i] = orders.ManualItem{
			ProductID: it.ProductId, Quantity: it.Quantity, ColorName: it.ColorName, UnitPricePesewas: it.UnitPricePesewas,
		}
	}
	if a := in.Address; a != nil {
		out.Address = &orders.Address{Area: a.Area, Landmark: a.Landmark, AddressLine: a.AddressLine, GhanaPostGPS: a.GhanaPostGps}
	}
	if in.Payment != nil {
		pay := fromGenManualPayment(*in.Payment)
		out.Payment = &pay
	}
	return out
}

func fromGenManualPayment(in gen.ManualPayment) orders.ManualPayment {
	return orders.ManualPayment{Network: string(in.Network), Reference: in.Reference}
}

// writeOrderError answers an orders.Store error with the right problem response.
func (s *Server) writeOrderError(c *gin.Context, err error) {
	var cart *orders.CartError
	switch {
	case errors.Is(err, orders.ErrNotFound):
		WriteProblem(c, http.StatusNotFound, "NOT_FOUND", "We couldn't find that order.")
	case errors.Is(err, orders.ErrTransitionNotAllowed):
		WriteProblem(c, http.StatusConflict, "TRANSITION_NOT_ALLOWED",
			"This order can't move to that step now. It may have just been changed; reload it.")
	case errors.Is(err, orders.ErrPaymentCantBeUndone):
		WriteProblem(c, http.StatusConflict, "NOTHING_TO_UNDO",
			"A payment can't be undone. Cancel the order instead.")
	case errors.Is(err, orders.ErrNothingToUndo):
		WriteProblem(c, http.StatusConflict, "NOTHING_TO_UNDO", "There is no step to undo.")
	case errors.Is(err, orders.ErrOrderNotPayable):
		WriteProblem(c, http.StatusConflict, "ORDER_NOT_PAYABLE",
			"This order is not waiting for payment. It may already be paid or cancelled; reload it.")
	case errors.Is(err, orders.ErrDuplicatePaymentReference):
		WriteProblem(c, http.StatusConflict, "DUPLICATE_PAYMENT_REFERENCE",
			"That transaction ID is already recorded for a payment. Check it against the MoMo message.")
	case errors.As(err, &cart):
		code, detail := "OUT_OF_STOCK", "Some items don't have enough stock left. Check your bag and try again."
		if errors.Is(err, orders.ErrProductUnavailable) {
			code, detail = "PRODUCT_UNAVAILABLE", "Some items are no longer on sale. Check your bag and try again."
		}
		fields := make([]FieldProblem, len(cart.Lines))
		for i, l := range cart.Lines {
			fields[i] = FieldProblem{Field: l.Field, Message: l.Message}
		}
		WriteProblem(c, http.StatusConflict, code, detail, fields...)
	case errors.Is(err, orders.ErrTooManyAttempts):
		WriteProblem(c, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS",
			"This order has had too many payment attempts. Please message us on WhatsApp to pay.")
	case errors.Is(err, orders.ErrPaymentsOff), errors.Is(err, orders.ErrProviderUnavailable):
		s.log.WarnContext(c.Request.Context(), "payment provider unavailable",
			slog.String("requestId", RequestIDFrom(c)), slog.Any("error", err))
		WriteProblem(c, http.StatusBadGateway, "PROVIDER_ERROR",
			"We couldn't start the mobile money payment. Please try again, or order on WhatsApp.")
	default:
		s.writeCatalogError(c, err) // validation errors, and the 500 for anything else
	}
}

func (s *Server) toGenAdminOrder(c *gin.Context, o orders.Order) gen.AdminOrder {
	out := gen.AdminOrder{
		Id:                 o.ID,
		Number:             o.Number,
		Source:             gen.OrderSource(o.Source),
		Status:             gen.OrderStatus(o.Status),
		Fulfilment:         gen.Fulfilment(o.Fulfilment),
		Customer:           gen.Customer{Id: o.CustomerID, Name: o.CustomerName, Phone: o.CustomerPhone},
		ItemCount:          o.ItemCount(),
		SubtotalPesewas:    o.SubtotalPesewas,
		DeliveryFeePesewas: o.DeliveryFeePesewas,
		DiscountPesewas:    o.DiscountPesewas,
		TotalPesewas:       o.TotalPesewas,
		NeedsAttention:     o.NeedsAttention,
		AttentionReason:    o.AttentionReason,
		PrivateNote:        o.PrivateNote,
		AllowedTransitions: toGenStatuses(o.AllowedTransitions()),
		// TODO(M5): one preview per allowed transition, from the Settings templates.
		NotificationPreviews: []gen.NotificationPreview{},
		CreatedAt:            o.CreatedAt.UTC(),
		UpdatedAt:            o.UpdatedAt.UTC(),
		PaidAt:               utcOrNil(o.PaidAt),
		DeliveredAt:          utcOrNil(o.DeliveredAt),
		CancelledAt:          utcOrNil(o.CancelledAt),
		Items:                make([]gen.OrderItem, len(o.Items)),
		Payments:             make([]gen.Payment, len(o.Payments)),
		Events:               make([]gen.OrderEvent, len(o.Events)),
	}
	if p, ok := PrincipalFrom(c); ok {
		out.CanUndo = o.Undoable() && s.authz.Allowed(p.Staff.Role, undoRoute, http.MethodPost)
	}
	if o.DeliveryZoneID != nil && o.DeliveryZoneName != nil {
		out.DeliveryZone = &gen.DeliveryZoneRef{Id: *o.DeliveryZoneID, Name: *o.DeliveryZoneName}
	}
	if o.Area != nil && o.Landmark != nil {
		out.Address = &gen.Address{Area: *o.Area, Landmark: *o.Landmark, AddressLine: o.AddressLine, GhanaPostGps: o.GhanaPostGPS}
	}
	if o.Rider != nil {
		r := toGenRider(*o.Rider)
		out.Rider = &r
	}
	for i, it := range o.Items {
		out.Items[i] = toGenOrderItem(it)
	}
	for i, p := range o.Payments {
		out.Payments[i] = gen.Payment{
			Id: p.ID, Provider: gen.PaymentProvider(p.Provider), Network: gen.PaymentNetwork(p.Network),
			AmountPesewas: p.AmountPesewas, Reference: p.Reference, Status: gen.PaymentStatus(p.Status),
			CreatedAt: p.CreatedAt.UTC(), PaidAt: utcOrNil(p.PaidAt),
		}
	}
	for i, e := range o.Events {
		ev := gen.OrderEvent{
			Id: e.ID, ToStatus: gen.OrderStatus(e.ToStatus), Note: e.Note,
			NotifiedVia: gen.NotificationChannel(e.NotifiedVia), CreatedAt: e.CreatedAt.UTC(),
		}
		if e.FromStatus != nil {
			from := gen.OrderStatus(*e.FromStatus)
			ev.FromStatus = &from
		}
		if e.ActorID != nil && e.ActorName != nil {
			ev.Actor = &gen.StaffRef{Id: *e.ActorID, Name: *e.ActorName}
		}
		out.Events[i] = ev
	}
	return out
}

func toGenOrderItem(it orders.Item) gen.OrderItem {
	return gen.OrderItem{
		ProductId: it.ProductID, ModelName: it.ModelName, BrandName: it.BrandName, ColorName: it.ColorName,
		Quantity: it.Quantity, UnitPricePesewas: it.UnitPricePesewas, ListPricePesewas: it.ListPricePesewas,
		LineTotalPesewas: it.LineTotalPesewas,
	}
}

func toGenStatuses(in []orders.Status) []gen.OrderStatus {
	out := make([]gen.OrderStatus, len(in))
	for i, st := range in {
		out[i] = gen.OrderStatus(st)
	}
	return out
}

// utcOrNil returns t in UTC, or nil when it is not set.
func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
