package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/chunlea/marionette/pkg/store"
)

// ErrTenantMismatch is returned when an operation would cross a tenant
// boundary: an explicit tenant that disagrees with the request's, or a session
// bound to a workspace or runner belonging to someone else.
var ErrTenantMismatch = errors.New("tenant mismatch")

// tenantFor returns the tenant a newly created entity belongs to.
//
// The request context wins over anything the caller passed. Tenant identity
// comes from the credential that authenticated the request, so an explicit
// tenant on a create request is either redundant or an attempt to write into
// somebody else's tenant - and the second one is refused rather than silently
// corrected, because a caller that asked for the wrong tenant is a caller whose
// next request is also wrong.
//
// With no tenant in context the explicit value is used unchanged, which is what
// keeps single-tenant deployments and internal callers working as before.
func tenantFor(ctx context.Context, explicit *string) (*string, error) {
	ctxTenant, ok := store.TenantFromContext(ctx)
	if !ok {
		return explicit, nil
	}
	if explicit != nil && *explicit != "" && *explicit != ctxTenant {
		return nil, fmt.Errorf("%w: request is for tenant %q, not %q", ErrTenantMismatch, ctxTenant, *explicit)
	}
	return &ctxTenant, nil
}

// sameTenant reports whether two tenant values refer to the same tenant,
// treating NULL and the empty string alike so a single-tenant row and a row
// written before tenancy existed compare equal.
func sameTenant(a, b *string) bool {
	return tenantValue(a) == tenantValue(b)
}

func tenantValue(t *string) string {
	if t == nil {
		return ""
	}
	return *t
}

// requireSameTenant rejects a reference that would cross a tenant boundary.
//
// Row level security stops a query from reading another tenant's rows, but it
// cannot stop a session in tenant A from naming a workspace id it somehow
// learned belongs to tenant B - to the database that is just a foreign key.
// These checks are what keep the two ends of a relationship in one tenant.
func requireSameTenant(kind, id string, owner, other *string) error {
	if sameTenant(owner, other) {
		return nil
	}
	return fmt.Errorf("%w: %s %s belongs to tenant %q", ErrTenantMismatch, kind, id, tenantValue(other))
}
