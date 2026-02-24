package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPResolverTrustsLoopbackByDefault(t *testing.T) {
	resolver := newClientIPResolver(nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:4000"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 198.51.100.4")

	if got := resolver.clientIPFromRequest(req); got != "203.0.113.7" {
		t.Fatalf("clientIPFromRequest() = %q, want %q", got, "203.0.113.7")
	}
}

func TestClientIPResolverIgnoresForwardedHeaderFromUntrustedProxy(t *testing.T) {
	resolver := newClientIPResolver(nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "198.51.100.10:4000"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")

	if got := resolver.clientIPFromRequest(req); got != "198.51.100.10" {
		t.Fatalf("clientIPFromRequest() = %q, want %q", got, "198.51.100.10")
	}
}

func TestClientIPResolverTrustsConfiguredProxyCIDR(t *testing.T) {
	resolver := newClientIPResolver([]string{"198.51.100.0/24"})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "198.51.100.10:4000"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")

	if got := resolver.clientIPFromRequest(req); got != "203.0.113.7" {
		t.Fatalf("clientIPFromRequest() = %q, want %q", got, "203.0.113.7")
	}
}

func TestAdminRouteAccessUsesTrustedProxyResolver(t *testing.T) {
	resolver := newClientIPResolver([]string{"10.0.0.0/8"})
	access := newAdminRouteAccess([]string{"203.0.113.0/24"}, resolver.clientIPFromRequest)

	allowedReq := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	allowedReq.RemoteAddr = "10.4.5.6:4000"
	allowedReq.Header.Set("X-Forwarded-For", "203.0.113.7")
	if !access.allows(allowedReq) {
		t.Fatal("allows() = false, want true for trusted proxy + allowlisted client")
	}

	blockedReq := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	blockedReq.RemoteAddr = "198.51.100.10:4000"
	blockedReq.Header.Set("X-Forwarded-For", "203.0.113.7")
	if access.allows(blockedReq) {
		t.Fatal("allows() = true, want false when forwarded header comes from untrusted proxy")
	}
}
