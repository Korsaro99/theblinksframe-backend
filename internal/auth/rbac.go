package auth

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"

	"theblinksframe-backend/internal/models"
)

//go:embed rbac_model.conf
var rbacModel string

//go:embed rbac_policy.csv
var rbacPolicy string

// Authorizer decides which staff role may call which admin route. The rules live in
// rbac_policy.csv, compiled into the binary so they are reviewed and versioned like code.
type Authorizer struct {
	e *casbin.Enforcer
}

// NewAuthorizer loads the embedded Casbin model and policy.
func NewAuthorizer() (*Authorizer, error) {
	m, err := model.NewModelFromString(rbacModel)
	if err != nil {
		return nil, fmt.Errorf("rbac model: %w", err)
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("rbac enforcer: %w", err)
	}
	// Load line by line so a malformed rule stops startup instead of being skipped.
	for i, line := range strings.Split(rbacPolicy, "\n") {
		line = strings.TrimSpace(line)
		if err := persist.LoadPolicyLine(line, e.GetModel()); err != nil {
			return nil, fmt.Errorf("rbac_policy.csv line %d: %w", i+1, err)
		}
	}
	if err := e.BuildRoleLinks(); err != nil {
		return nil, fmt.Errorf("rbac roles: %w", err)
	}
	return &Authorizer{e: e}, nil
}

// Allowed reports whether role may call method on route, where route is the gin route
// template (c.FullPath()), e.g. "/api/v1/admin/orders/:orderId".
func (a *Authorizer) Allowed(role models.StaffRole, route, method string) bool {
	ok, err := a.e.Enforce(string(role), route, method)
	return err == nil && ok
}
