package api

import "net/http"

func (s *Server) handleAuthCapabilities(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]any{
		"password_auth_enabled": s.passwordAuth,
		"magic_link_enabled":    true,
		"ssh_auth_enabled":      true,
		"passkey_enabled":       s.passkey != nil,
	})
}
