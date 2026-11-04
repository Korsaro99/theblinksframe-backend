package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/orders"
	"theblinksframe-backend/internal/payments"
	"theblinksframe-backend/internal/platform/ratelimit"
)

// Public order endpoints: website checkout, mobile money payment, and the provider's webhook.
// None needs a login, so each one that costs something (an order row, a phone prompt) is
// rate-limited per client IP.

// CreateOrder implements POST /orders.
func (s *Server) CreateOrder(c *gin.Context, params gen.CreateOrderParams) {
	if !s.allow(c, s.orderLimit, "Too many orders from here. Please wait a few minutes and try again.") {
		return
	}
	var req gen.CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	in := orders.WebsiteOrder{
		CustomerName: req.Customer.Name, CustomerPhone: req.Customer.Phone,
		Items: make([]orders.CartItem, len(req.Items)), Fulfilment: orders.Fulfilment(req.Fulfilment),
		DeliveryZoneID: req.DeliveryZoneId,
	}
	for i, it := range req.Items {
		in.Items[i] = orders.CartItem{ProductID: it.ProductId, Quantity: it.Quantity, ColorName: it.ColorName}
	}
	if a := req.Address; a != nil {
		in.Address = &orders.Address{Area: a.Area, Landmark: a.Landmark, AddressLine: a.AddressLine, GhanaPostGPS: a.GhanaPostGps}
	}
	if params.IdempotencyKey != nil {
		in.IdempotencyKey = *params.IdempotencyKey
	}
	o, err := s.orders.CreateWebsite(c.Request.Context(), in)
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toGenPublicOrder(o))
}

// PayOrder implements POST /orders/{orderId}/pay.
func (s *Server) PayOrder(c *gin.Context, orderID gen.OrderId) {
	if !s.allow(c, s.payLimit, "Too many payment attempts from here. Please wait a few minutes and try again.") {
		return
	}
	var req gen.PayOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	st, err := s.orders.StartPayment(c.Request.Context(), orderID, string(req.Network), req.MomoNumber)
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, toGenPaymentState(st))
}

// GetOrderPayment implements GET /orders/{orderId}/payment.
func (s *Server) GetOrderPayment(c *gin.Context, orderID gen.OrderId) {
	st, err := s.orders.PaymentState(c.Request.Context(), orderID)
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenPaymentState(st))
}

// ReceivePaymentWebhook implements POST /webhooks/payments/{provider}. The webhook only says
// which payment changed: SettlePayment asks the provider itself before anything is marked
// paid. It answers 200 for anything correctly signed, even if handling it failed, because the
// housekeeping job checks pending payments again; a forged or unsigned call gets 401.
func (s *Server) ReceivePaymentWebhook(c *gin.Context, provider gen.ReceivePaymentWebhookParamsProvider) {
	log := s.log.With(slog.String("requestId", RequestIDFrom(c)), slog.String("provider", string(provider)))
	if s.payments == nil || !strings.EqualFold(string(provider), s.payments.Name()) {
		WriteProblem(c, http.StatusUnauthorized, "WEBHOOK_REJECTED", "This payment provider is not enabled.")
		return
	}
	body, err := io.ReadAll(c.Request.Body) // the validator put the raw body back for us
	if err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request body could not be read.")
		return
	}
	ref, err := s.payments.ParseWebhook(c.Request.Header, body)
	if err != nil {
		log.WarnContext(c.Request.Context(), "payment webhook rejected", slog.Any("error", err))
		WriteProblem(c, http.StatusUnauthorized, "WEBHOOK_REJECTED", "The signature is missing or invalid.")
		return
	}
	// The body is stored with the payment but never logged: it can hold the customer's number.
	switch err := s.orders.SettlePayment(c.Request.Context(), ref, body); {
	case errors.Is(err, orders.ErrUnknownPayment):
		log.WarnContext(c.Request.Context(), "payment webhook for an unknown reference", slog.String("reference", ref))
	case err != nil:
		log.ErrorContext(c.Request.Context(), "payment webhook not handled; housekeeping will retry",
			slog.String("reference", ref), slog.Any("error", err))
	}
	c.Status(http.StatusOK)
}

// allow applies a per-IP rate limit and answers 429 when it is exceeded.
func (s *Server) allow(c *gin.Context, w *ratelimit.Window, detail string) bool {
	ok, wait := w.Allow(c.ClientIP())
	if !ok {
		c.Header("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		WriteProblem(c, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", detail)
	}
	return ok
}

func toGenPublicOrder(o orders.Order) gen.PublicOrder {
	out := gen.PublicOrder{
		Id: o.ID, Number: o.Number, Status: gen.OrderStatus(o.Status), Fulfilment: gen.Fulfilment(o.Fulfilment),
		DeliveryZoneName: o.DeliveryZoneName, Items: make([]gen.OrderItem, len(o.Items)), CreatedAt: o.CreatedAt.UTC(),
		SubtotalPesewas: o.SubtotalPesewas, DeliveryFeePesewas: o.DeliveryFeePesewas,
		DiscountPesewas: o.DiscountPesewas, TotalPesewas: o.TotalPesewas,
	}
	if o.Source == orders.SourceWebsite {
		payBy := o.CreatedAt.Add(orders.PayWithin).UTC()
		out.PayBy = &payBy
	}
	for i, it := range o.Items {
		out.Items[i] = toGenOrderItem(it)
	}
	return out
}

func toGenPaymentState(st orders.PaymentState) gen.PaymentState {
	out := gen.PaymentState{OrderId: st.OrderID, OrderStatus: gen.OrderStatus(st.OrderStatus)}
	if p := st.Latest; p != nil {
		out.Payment = &gen.PaymentAttempt{
			Status: gen.PaymentStatus(p.Status), Network: gen.PaymentNetwork(p.Network),
			AmountPesewas: p.AmountPesewas, FailureReason: p.FailureReason,
		}
		if p.Status == payments.StatusPending && p.NextAction != nil {
			out.NextAction = &gen.NextAction{Type: gen.NextActionType(*p.NextAction), RedirectUrl: p.RedirectURL}
		}
	}
	return out
}
