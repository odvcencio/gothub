package api

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
)

var defaultAdminRouteCIDRs = []string{
	"127.0.0.1/32",
	"::1/128",
}

type adminRouteAccess struct {
	allowList []*net.IPNet
	clientIP  func(*http.Request) string
}

func newAdminRouteAccess(cidrs []string, clientIP func(*http.Request) string) adminRouteAccess {
	if clientIP == nil {
		clientIP = func(r *http.Request) string {
			host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
			if err == nil && host != "" {
				return host
			}
			return strings.TrimSpace(r.RemoteAddr)
		}
	}
	return adminRouteAccess{
		allowList: parseAdminRouteCIDRs(cidrs),
		clientIP:  clientIP,
	}
}

func parseAdminRouteCIDRs(cidrs []string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		if ip := net.ParseIP(value); ip != nil {
			bits := 128
			if v4 := ip.To4(); v4 != nil {
				ip = v4
				bits = 32
			}
			result = append(result, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}

		_, block, err := net.ParseCIDR(value)
		if err != nil {
			slog.Warn("invalid admin CIDR allowlist entry; ignoring", "cidr", value, "error", err)
			continue
		}
		result = append(result, block)
	}
	return result
}

func (a adminRouteAccess) wrap(next http.Handler) http.Handler {
	if next == nil {
		return http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.allows(r) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a adminRouteAccess) allows(r *http.Request) bool {
	if len(a.allowList) == 0 {
		return false
	}
	ip := net.ParseIP(strings.TrimSpace(a.clientIP(r)))
	if ip == nil {
		return false
	}
	for _, block := range a.allowList {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) registerPprofRoutes() {
	guard := s.adminRouteAccess.wrap
	s.mux.Handle("GET /debug/pprof/", guard(http.HandlerFunc(pprof.Index)))
	s.mux.Handle("GET /debug/pprof/cmdline", guard(http.HandlerFunc(pprof.Cmdline)))
	s.mux.Handle("GET /debug/pprof/profile", guard(http.HandlerFunc(pprof.Profile)))
	s.mux.Handle("GET /debug/pprof/symbol", guard(http.HandlerFunc(pprof.Symbol)))
	s.mux.Handle("POST /debug/pprof/symbol", guard(http.HandlerFunc(pprof.Symbol)))
	s.mux.Handle("GET /debug/pprof/trace", guard(http.HandlerFunc(pprof.Trace)))
	s.mux.Handle("GET /debug/pprof/{profile}", guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		profile := strings.TrimSpace(r.PathValue("profile"))
		if profile == "" {
			http.NotFound(w, r)
			return
		}
		pprof.Handler(profile).ServeHTTP(w, r)
	})))
}
