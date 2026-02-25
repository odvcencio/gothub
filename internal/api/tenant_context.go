package api

import (
	"net/http"
	"net/textproto"
	"strings"

	"github.com/odvcencio/gothub/internal/database"
)

const defaultTenantHeader = "X-Gothub-Tenant-ID"

type tenantContextOptions struct {
	enabled         bool
	headerName      string
	defaultTenantID string
}

func newTenantContextOptions(enabled bool, headerName, defaultTenantID string) tenantContextOptions {
	name := strings.TrimSpace(headerName)
	if name == "" {
		name = defaultTenantHeader
	}
	return tenantContextOptions{
		enabled:         enabled,
		headerName:      textproto.CanonicalMIMEHeaderKey(name),
		defaultTenantID: strings.TrimSpace(defaultTenantID),
	}
}

func tenantContextMiddleware(opts tenantContextOptions, ipResolver clientIPResolver, next http.Handler) http.Handler {
	if !opts.enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := opts.defaultTenantID
		if ipResolver.trustXForwardedFor(r) {
			if headerTenantID := firstHeaderToken(r.Header.Values(opts.headerName)); headerTenantID != "" {
				tenantID = headerTenantID
			}
		}

		if tenantID == "" {
			next.ServeHTTP(w, r)
			return
		}

		next.ServeHTTP(w, r.WithContext(database.WithTenantID(r.Context(), tenantID)))
	})
}

func firstHeaderToken(values []string) string {
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			if token = strings.TrimSpace(token); token != "" {
				return token
			}
		}
	}
	return ""
}
