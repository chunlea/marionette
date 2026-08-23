package store

import "context"

// tenantContextKey is the context key carrying the tenant a request acts for.
// It is unexported and typed so nothing outside this package can forge one.
type tenantContextKey struct{}

// WithTenant returns a context bound to a tenant.
//
// Auth middleware is the only place this should be called in production: the
// tenant comes from the credential that authenticated the request, never from
// anything the caller supplied. An empty id is treated as no tenant, so a key
// with a NULL tenant_id behaves exactly like a single-tenant deployment.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// TenantFromContext returns the tenant bound to ctx, and whether there was one.
//
// No tenant is the normal state for a single-tenant deployment. In multi-tenant
// mode it means the request never went through auth, which the store treats as
// an error rather than as permission to see everything.
func TenantFromContext(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(tenantContextKey{}).(string)
	if !ok || tenantID == "" {
		return "", false
	}
	return tenantID, true
}

// TenantPtr returns the ctx tenant as the *string the models use, or nil.
func TenantPtr(ctx context.Context) *string {
	tenantID, ok := TenantFromContext(ctx)
	if !ok {
		return nil
	}
	return &tenantID
}

// systemAccessKey marks a context as acting for the deployment rather than for
// one tenant.
type systemAccessKey struct{}

// WithSystemAccess returns a context that may read and write across tenants.
//
// This is a deliberate hole in tenant isolation, and it has exactly two callers
// by design: the admin API, whose single operator credential is the
// deployment's root of trust and which exists to see everything, and the
// background jobs, which reap runners and collect chunks for every tenant at
// once. Both are server-side; neither is reachable by an API key.
//
// It is not a substitute for a tenant. A request that should have had one and
// does not is a bug, and marking it system-access would hide that bug behind a
// working screen.
func WithSystemAccess(ctx context.Context) context.Context {
	return context.WithValue(ctx, systemAccessKey{}, true)
}

// IsSystemAccess reports whether ctx may cross tenant boundaries.
func IsSystemAccess(ctx context.Context) bool {
	granted, _ := ctx.Value(systemAccessKey{}).(bool)
	return granted
}
