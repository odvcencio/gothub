package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/odvcencio/gothub/internal/database"
)

func TestTenantContextMiddlewareDisabledByDefault(t *testing.T) {
	mw := tenantContextMiddleware(
		newTenantContextOptions(false, "", "default-tenant"),
		newClientIPResolver(nil),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tenantID, ok := database.TenantIDFromContext(r.Context()); ok {
				t.Fatalf("tenant context unexpectedly set to %q", tenantID)
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user", nil)
	req.RemoteAddr = "127.0.0.1:10001"
	req.Header.Set(defaultTenantHeader, "tenant-from-header")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestTenantContextMiddlewareResolvesTenant(t *testing.T) {
	tests := []struct {
		name           string
		remoteAddr     string
		headerName     string
		headerValue    string
		defaultTenant  string
		wantTenant     string
		wantTenantSet  bool
		trustedProxies []string
	}{
		{
			name:          "trusted source uses header",
			remoteAddr:    "127.0.0.1:9000",
			headerName:    defaultTenantHeader,
			headerValue:   "tenant-from-header",
			defaultTenant: "tenant-default",
			wantTenant:    "tenant-from-header",
			wantTenantSet: true,
		},
		{
			name:          "untrusted source ignores header and falls back to default",
			remoteAddr:    "203.0.113.10:9000",
			headerName:    defaultTenantHeader,
			headerValue:   "tenant-from-header",
			defaultTenant: "tenant-default",
			wantTenant:    "tenant-default",
			wantTenantSet: true,
		},
		{
			name:          "missing header uses default tenant",
			remoteAddr:    "127.0.0.1:9000",
			defaultTenant: "tenant-default",
			wantTenant:    "tenant-default",
			wantTenantSet: true,
		},
		{
			name:          "custom header is honored",
			remoteAddr:    "127.0.0.1:9000",
			headerName:    "X-Tenant-ID",
			headerValue:   "tenant-custom",
			defaultTenant: "tenant-default",
			wantTenant:    "tenant-custom",
			wantTenantSet: true,
		},
		{
			name:          "tenant context remains absent when no header and no default",
			remoteAddr:    "127.0.0.1:9000",
			headerName:    defaultTenantHeader,
			wantTenantSet: false,
		},
		{
			name:          "first non-empty tenant token is used",
			remoteAddr:    "127.0.0.1:9000",
			headerName:    defaultTenantHeader,
			headerValue:   "  , tenant-a, tenant-b",
			defaultTenant: "tenant-default",
			wantTenant:    "tenant-a",
			wantTenantSet: true,
		},
		{
			name:           "trusted source list can be configured",
			remoteAddr:     "10.11.12.13:8080",
			headerName:     defaultTenantHeader,
			headerValue:    "tenant-network",
			defaultTenant:  "tenant-default",
			wantTenant:     "tenant-network",
			wantTenantSet:  true,
			trustedProxies: []string{"10.0.0.0/8"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mw := tenantContextMiddleware(
				newTenantContextOptions(true, tc.headerName, tc.defaultTenant),
				newClientIPResolver(tc.trustedProxies),
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotTenant, gotTenantSet := database.TenantIDFromContext(r.Context())
					if gotTenantSet != tc.wantTenantSet {
						t.Fatalf("tenant set = %v, want %v (tenant=%q)", gotTenantSet, tc.wantTenantSet, gotTenant)
					}
					if gotTenantSet && gotTenant != tc.wantTenant {
						t.Fatalf("tenant ID = %q, want %q", gotTenant, tc.wantTenant)
					}
					w.WriteHeader(http.StatusNoContent)
				}),
			)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/user", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.headerName != "" && tc.headerValue != "" {
				req.Header.Set(tc.headerName, tc.headerValue)
			}
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
			}
		})
	}
}
