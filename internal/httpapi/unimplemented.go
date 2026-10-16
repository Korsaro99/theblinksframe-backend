package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/httpapi/gen"
)

// unimplemented answers 501 for operations in api/openapi.yaml that later milestones build.
// Server embeds it; when an operation is implemented on Server, delete its stub here.
type unimplemented struct{}

func notImplemented(c *gin.Context) {
	WriteProblem(c, http.StatusNotImplemented, "NOT_IMPLEMENTED", "This part of the API is not built yet.")
}

func (unimplemented) AdminListOrders(c *gin.Context, _ gen.AdminListOrdersParams) { notImplemented(c) }

func (unimplemented) AdminCreateOrder(c *gin.Context, _ gen.AdminCreateOrderParams) {
	notImplemented(c)
}

func (unimplemented) AdminGetOrderPipeline(c *gin.Context) { notImplemented(c) }

func (unimplemented) AdminGetOrder(c *gin.Context, _ gen.OrderId) { notImplemented(c) }

func (unimplemented) AdminPatchOrder(c *gin.Context, _ gen.OrderId) { notImplemented(c) }

func (unimplemented) AdminConfirmPayment(c *gin.Context, _ gen.OrderId) { notImplemented(c) }

func (unimplemented) AdminTransitionOrder(c *gin.Context, _ gen.OrderId) { notImplemented(c) }

func (unimplemented) AdminUndoOrderTransition(c *gin.Context, _ gen.OrderId) { notImplemented(c) }

func (unimplemented) QuoteCart(c *gin.Context) { notImplemented(c) }

func (unimplemented) ListBrands(c *gin.Context, _ gen.ListBrandsParams) { notImplemented(c) }

func (unimplemented) GetBrand(c *gin.Context, _ gen.Slug) { notImplemented(c) }

func (unimplemented) GetMenu(c *gin.Context) { notImplemented(c) }

func (unimplemented) GetProduct(c *gin.Context, _ gen.Slug) { notImplemented(c) }

func (unimplemented) CreateOrder(c *gin.Context, _ gen.CreateOrderParams) { notImplemented(c) }

func (unimplemented) TrackOrder(c *gin.Context) { notImplemented(c) }

func (unimplemented) PayOrder(c *gin.Context, _ gen.OrderId) { notImplemented(c) }

func (unimplemented) GetOrderPayment(c *gin.Context, _ gen.OrderId) { notImplemented(c) }

func (unimplemented) ReceivePaymentWebhook(c *gin.Context, _ gen.ReceivePaymentWebhookParamsProvider) {
	notImplemented(c)
}
