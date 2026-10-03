package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ProblemContentType is the RFC 9457 media type for error responses.
const ProblemContentType = "application/problem+json"

// Problem is an RFC 9457 error body. Detail is written for people: the frontend shows it
// to the user as is. Code is a stable, machine-readable reason (see Problem in api/openapi.yaml).
type Problem struct {
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Status   int            `json:"status"`
	Detail   string         `json:"detail,omitempty"`
	Instance string         `json:"instance,omitempty"`
	Code     string         `json:"code,omitempty"`
	Errors   []FieldProblem `json:"errors,omitempty"`
}

// FieldProblem points at one invalid request field.
type FieldProblem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// WriteProblem aborts the request with a problem+json response.
func WriteProblem(c *gin.Context, status int, code, detail string, fields ...FieldProblem) {
	p := Problem{
		Type:     "about:blank",
		Title:    http.StatusText(status),
		Status:   status,
		Detail:   detail,
		Instance: c.Request.URL.Path,
		Code:     code,
		Errors:   fields,
	}
	c.Header("Content-Type", ProblemContentType)
	c.AbortWithStatusJSON(status, p)
}
