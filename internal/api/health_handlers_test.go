package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/service"
)

func TestAdminHealthEndpointEnabledReturnsStats(t *testing.T) {
	server := setupAdminTestServer(t, ServerOptions{
		EnableAdminHealth: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	req.RemoteAddr = "127.0.0.1:4000"
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var body adminHealthResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}

	if body.Status != "ok" {
		t.Fatalf("expected status ok, got %q", body.Status)
	}
	if body.Queue.Depth != 0 {
		t.Fatalf("expected queue depth 0, got %d", body.Queue.Depth)
	}
	if body.Cache.CodeIntel.MaxItems <= 0 {
		t.Fatalf("expected positive cache max_items, got %d", body.Cache.CodeIntel.MaxItems)
	}
}

func TestAdminHealthEndpointDeniedForNonAllowlistedIP(t *testing.T) {
	server := setupAdminTestServer(t, ServerOptions{
		EnableAdminHealth: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	req.RemoteAddr = "203.0.113.10:4000"
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.Code)
	}
}

func TestAdminHealthEndpointSupportsCustomCIDRAllowlist(t *testing.T) {
	server := setupAdminTestServer(t, ServerOptions{
		EnableAdminHealth: true,
		AdminAllowedCIDRs: []string{"203.0.113.0/24"},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	req.RemoteAddr = "203.0.113.7:4000"
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
}

func TestPprofEndpointsRespectRouteGuards(t *testing.T) {
	server := setupAdminTestServer(t, ServerOptions{
		EnablePprof: true,
	})

	allowedReq := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	allowedReq.RemoteAddr = "127.0.0.1:4000"
	allowedResp := httptest.NewRecorder()
	server.ServeHTTP(allowedResp, allowedReq)
	if allowedResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for local pprof access, got %d", allowedResp.Code)
	}

	blockedReq := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	blockedReq.RemoteAddr = "203.0.113.10:4000"
	blockedResp := httptest.NewRecorder()
	server.ServeHTTP(blockedResp, blockedReq)
	if blockedResp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for blocked pprof access, got %d", blockedResp.Code)
	}
}

func TestPprofEndpointDisabledByDefault(t *testing.T) {
	server := setupAdminTestServer(t, ServerOptions{})

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.RemoteAddr = "127.0.0.1:4000"
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.Code)
	}
}

func setupAdminTestServer(t *testing.T, opts ServerOptions) *Server {
	t.Helper()

	ctx := context.Background()
	tmpDir := t.TempDir()

	db, err := database.OpenSQLite(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	authSvc := auth.NewService("test-secret-123456", 24*time.Hour)
	repoSvc := service.NewRepoService(db, filepath.Join(tmpDir, "repos"))
	return NewServerWithOptions(db, authSvc, repoSvc, opts)
}
