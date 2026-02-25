package database

import (
	"context"
	"strings"
)

type tenantContextKey struct{}

// WithTenantID stores a tenant identifier in context for downstream DB operations.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// TenantIDFromContext retrieves a tenant identifier from context.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	tenantID, ok := ctx.Value(tenantContextKey{}).(string)
	if !ok {
		return "", false
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "", false
	}
	return tenantID, true
}
