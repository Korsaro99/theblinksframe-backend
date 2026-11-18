package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// unimplemented answers 501 for operations in api/openapi.yaml that later milestones build.
// Server embeds it; when an operation is implemented on Server, delete its stub here.
//
// When the spec gains an operation, add a stub here first so the code compiles before the
// handler exists. With no stubs left the linter reports the type as unused, hence the
// nolint directives below and on the Server field; keep the type for the next stub.
//
//nolint:unused // empty while the code is level with the spec; keep it for the next stub
type unimplemented struct{}

//nolint:unused // used by stubs whenever the spec is ahead of the code
func notImplemented(c *gin.Context) {
	WriteProblem(c, http.StatusNotImplemented, "NOT_IMPLEMENTED", "This part of the API is not built yet.")
}
