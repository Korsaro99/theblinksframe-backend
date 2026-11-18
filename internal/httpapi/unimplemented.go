package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// unimplemented answers 501 for operations in api/openapi.yaml that later milestones build.
// Server embeds it; when an operation is implemented on Server, delete its stub here.
//
// Every operation in the spec is built now, so it has no stubs. When the spec gains an
// operation, add a stub here first (`func (unimplemented) Op(c *gin.Context) { notImplemented(c) }`)
// so the code compiles before the handler exists.
//
//nolint:unused // empty while the code is level with the spec; keep it for the next stub
type unimplemented struct{}

//nolint:unused // used by stubs whenever the spec is ahead of the code
func notImplemented(c *gin.Context) {
	WriteProblem(c, http.StatusNotImplemented, "NOT_IMPLEMENTED", "This part of the API is not built yet.")
}
