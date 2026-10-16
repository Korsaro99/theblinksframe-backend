package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/gin-gonic/gin"
)

// maxJSONBody caps request bodies on /api/v1. JSON requests are small; this stops a client
// from making the server read megabytes. Media uploads (M2) will get their own limit.
const maxJSONBody = 1 << 20 // 1 MiB

type ginContextKey struct{}

func ginFrom(ctx context.Context) *gin.Context {
	c, _ := ctx.Value(ginContextKey{}).(*gin.Context)
	return c
}

// openAPIValidator checks every /api/v1 request against api/openapi.yaml before a handler
// runs, in two steps:
//
//  1. Security: the `security` declared on the operation decides which checks run (access
//     cookie, CSRF header, refresh cookie). Protection therefore comes from the contract,
//     so a new admin endpoint cannot be added to the spec and forgotten in the code.
//  2. Parameters and body: types, required fields, enums, lengths. Handlers can then trust
//     the shape of what they decode; business rules are still theirs to check.
//
// Security runs first and stops the request, so an anonymous caller learns nothing from
// validation messages.
func openAPIValidator(spec *openapi3.T, authenticate openapi3filter.AuthenticationFunc, log *slog.Logger) (gin.HandlerFunc, error) {
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		return nil, fmt.Errorf("openapi router: %w", err)
	}
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBody)
		}
		route, pathParams, err := router.FindRoute(c.Request)
		if err != nil {
			// gin matched a route that the spec does not have: the code and spec disagree.
			if !errors.Is(err, routers.ErrPathNotFound) {
				log.ErrorContext(c.Request.Context(), "openapi route lookup failed",
					slog.String("requestId", RequestIDFrom(c)), slog.Any("error", err))
			}
			WriteProblem(c, http.StatusNotFound, "NOT_FOUND", "There is nothing at this address.")
			return
		}
		ctx := context.WithValue(c.Request.Context(), ginContextKey{}, c)

		security := route.Operation.Security
		if security == nil {
			security = &route.Spec.Security
		}
		authInput := &openapi3filter.RequestValidationInput{
			Request: c.Request, PathParams: pathParams, Route: route,
			Options: &openapi3filter.Options{AuthenticationFunc: authenticate},
		}
		if err := openapi3filter.ValidateSecurityRequirements(ctx, authInput, *security); err != nil {
			writeAuthProblem(c, err, log)
			return
		}

		input := &openapi3filter.RequestValidationInput{
			Request: c.Request, PathParams: pathParams, Route: route,
			Options: &openapi3filter.Options{
				AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, // done above
				MultiError:         true,                                  // report every bad field at once
			},
		}
		if err := openapi3filter.ValidateRequest(ctx, input); err != nil {
			writeValidationProblem(c, err)
			return
		}
		c.Next()
	}, nil
}

// authProblemKey holds the problem an authentication check chose, so the response can say
// 401 vs 403 precisely instead of kin-openapi's generic security error.
const authProblemKey = "authProblem"

type authProblem struct {
	status int
	code   string
	detail string
}

func (p authProblem) Error() string { return p.code }

func failAuth(c *gin.Context, status int, code, detail string) error {
	p := authProblem{status: status, code: code, detail: detail}
	c.Set(authProblemKey, p)
	return p
}

func writeAuthProblem(c *gin.Context, err error, log *slog.Logger) {
	if v, ok := c.Get(authProblemKey); ok {
		p := v.(authProblem)
		WriteProblem(c, p.status, p.code, p.detail)
		return
	}
	// No check chose a problem: an unexpected error (e.g. the database is down).
	log.ErrorContext(c.Request.Context(), "authentication failed unexpectedly",
		slog.String("requestId", RequestIDFrom(c)), slog.Any("error", err))
	WriteProblem(c, http.StatusInternalServerError, "INTERNAL", "Something went wrong on our side. Please try again.")
}

// writeValidationProblem turns kin-openapi errors into one problem response: 422 with a list
// of fields when the body has the wrong content, 400 when the request itself is malformed.
func writeValidationProblem(c *gin.Context, err error) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		WriteProblem(c, http.StatusRequestEntityTooLarge, "TOO_LARGE", "The request is too large.")
		return
	}
	var fields []FieldProblem
	malformed := false
	var walk func(error, string)
	// Type switch, not errors.As. errors.As unwraps a parameter RequestError into the
	// schema MultiError inside it (kin-openapi reports every schema failure that way),
	// which drops the parameter name. A bad query value such as pageSize=101 would
	// then look like a body problem and answer 422 instead of 400.
	walk = func(err error, param string) {
		switch e := err.(type) { //nolint:errorlint // errors.As unwraps RequestError into its schema MultiError and the parameter name is lost
		case openapi3.MultiError:
			for _, item := range e {
				walk(item, param)
			}
		case *openapi3filter.RequestError:
			if e.Parameter != nil {
				malformed = true
				param = e.Parameter.Name
			}
			if e.Err == nil {
				malformed = true
				fields = append(fields, FieldProblem{Field: param, Message: e.Reason})
				return
			}
			walk(e.Err, param)
		case *openapi3.SchemaError:
			field := jsonPath(e.JSONPointer())
			if param != "" {
				field = strings.TrimSuffix(param+"."+field, ".")
			}
			fields = append(fields, FieldProblem{Field: field, Message: e.Reason})
		default:
			// oneOf/allOf wrappers are unexported; unwrap one step without errors.As.
			if u, ok := err.(interface{ Unwrap() error }); ok {
				if inner := u.Unwrap(); inner != nil {
					walk(inner, param)
					return
				}
			}
			// Not JSON, wrong Content-Type, missing body, unparsable parameter.
			malformed = true
			fields = append(fields, FieldProblem{Field: param, Message: err.Error()})
		}
	}
	walk(err, "")

	if malformed {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.", fields...)
		return
	}
	WriteProblem(c, http.StatusUnprocessableEntity, "VALIDATION", "Some details are missing or not valid.", fields...)
}

// jsonPath turns ["items","0","quantity"] into "items[0].quantity".
func jsonPath(pointer []string) string {
	var b strings.Builder
	for _, p := range pointer {
		if _, err := strconv.Atoi(p); err == nil {
			b.WriteString("[" + p + "]")
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(p)
	}
	return b.String()
}
