package database

import (
	"context"
	"testing"
)

func TestTenantContextRoundTrip(t *testing.T) {
	ctx := WithTenantID(context.Background(), "tenant-a")
	got, ok := TenantIDFromContext(ctx)
	if !ok {
		t.Fatal("TenantIDFromContext() ok = false, want true")
	}
	if got != "tenant-a" {
		t.Fatalf("TenantIDFromContext() = %q, want %q", got, "tenant-a")
	}
}

func TestTenantContextSkipsEmptyTenant(t *testing.T) {
	base := context.Background()
	ctx := WithTenantID(base, "   ")
	if ctx != base {
		t.Fatal("WithTenantID() returned a new context for empty tenant ID")
	}
	if tenantID, ok := TenantIDFromContext(ctx); ok {
		t.Fatalf("TenantIDFromContext() = %q, want no tenant", tenantID)
	}
}
