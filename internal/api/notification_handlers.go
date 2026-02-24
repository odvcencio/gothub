package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/odvcencio/gothub/internal/auth"
)

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	unreadOnly := parseBool(r.URL.Query().Get("unread"))
	notifications, err := s.db.ListNotifications(r.Context(), claims.UserID, unreadOnly)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	page, perPage := parsePagination(r, 30, 200)
	jsonResponse(w, http.StatusOK, paginateSlice(notifications, page, perPage))
}

func (s *Server) handleUnreadNotificationsCount(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	count, err := s.db.CountUnreadNotifications(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]int{"count": count})
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "invalid notification id", http.StatusBadRequest)
		return
	}
	if err := s.db.MarkNotificationRead(r.Context(), id, claims.UserID); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if err := s.db.MarkAllNotificationsRead(r.Context(), claims.UserID); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
