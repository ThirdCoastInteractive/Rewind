package plugin

import "context"

type tenantContextKey struct{}

// WithTenantScope records the authenticated workspace for request-scoped stores.
// scoped is true for Live requests, including authenticated requests without a tenant.
func WithTenantScope(ctx context.Context, tenant string, scoped bool) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantScope{tenant: tenant, scoped: scoped})
}

// TenantScope returns the request workspace and whether tenant isolation is active.
func TenantScope(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	s, _ := ctx.Value(tenantContextKey{}).(tenantScope)
	return s.tenant, s.scoped
}

type tenantScope struct {
	tenant string
	scoped bool
}
