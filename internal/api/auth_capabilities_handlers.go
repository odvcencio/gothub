package api

import "net/http"

func (s *Server) handleAuthCapabilities(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]any{
		"magic_link_enabled": true,
		"ssh_auth_enabled":   true,
		"passkey_enabled":    s.passkey != nil,
	})
}
