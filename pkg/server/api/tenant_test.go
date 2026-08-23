package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/store"
)

// tenantTestServer builds a server whose API key belongs to a tenant.
func tenantTestServer(t *testing.T, tenantID string, multiTenant bool, opts ...Option) (*Server, string) {
	t.Helper()

	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_tenant" })

	createOpts := auth.CreateAPIKeyOptions{Name: "tenant-key", Scopes: []string{"*"}}
	if tenantID != "" {
		createOpts.TenantID = &tenantID
	}
	_, token, err := apiKeyService.Create(context.Background(), createOpts)
	require.NoError(t, err)

	allOpts := append([]Option{WithAPIKeyService(apiKeyService)}, opts...)
	srv := New(Config{Host: "localhost", Port: 8080, MultiTenant: multiTenant}, zap.NewNop(), allOpts...)
	return srv, token
}

// tenantCapture records the tenant the request context carried by the time it
// reached a handler.
type tenantCapture struct {
	SessionService
	seen  string
	found bool
}

func (c *tenantCapture) List(ctx context.Context, _ ListSessionsOptions) (*store.ListResult[store.Session], error) {
	c.seen, c.found = store.TenantFromContext(ctx)
	return &store.ListResult[store.Session]{}, nil
}

// TestAuthMiddleware_BindsTheKeysTenant is the injection point: the tenant
// comes from the credential, so a handler cannot be reached without it and a
// caller cannot choose it.
func TestAuthMiddleware_BindsTheKeysTenant(t *testing.T) {
	capture := &tenantCapture{}
	srv, token := tenantTestServer(t, "tenant_a", false, WithSessionService(capture))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, capture.found, "the handler must see a tenant")
	assert.Equal(t, "tenant_a", capture.seen)
}

// TestAuthMiddleware_UntenantedKeyIsSingleTenant keeps the zero-config path:
// a key with no tenant is an ordinary single-tenant request.
func TestAuthMiddleware_UntenantedKeyIsSingleTenant(t *testing.T) {
	capture := &tenantCapture{}
	srv, token := tenantTestServer(t, "", false, WithSessionService(capture))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, capture.found, "a tenantless key must stay tenantless")
}

// TestAuthMiddleware_UntenantedKeyIsRefusedInMultiTenantMode: such a key cannot
// be scoped to anything, and serving it would mean showing it either every
// tenant's rows or none.
func TestAuthMiddleware_UntenantedKeyIsRefusedInMultiTenantMode(t *testing.T) {
	capture := &tenantCapture{}
	srv, token := tenantTestServer(t, "", true, WithSessionService(capture))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "tenant_unresolved")
	assert.False(t, capture.found, "the request must not reach a handler")
}

func TestAuthMiddleware_TenantedKeyWorksInMultiTenantMode(t *testing.T) {
	capture := &tenantCapture{}
	srv, token := tenantTestServer(t, "tenant_a", true, WithSessionService(capture))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, capture.found)
	assert.Equal(t, "tenant_a", capture.seen)
}
