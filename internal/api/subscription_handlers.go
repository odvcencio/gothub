package api

import (
	"net/http"
	"time"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/models"
)

type subscriptionStatusResponse struct {
	HasPrivateRepos bool    `json:"has_private_repos"`
	Active          bool    `json:"active"`
	Feature         string  `json:"feature,omitempty"`
	Source          string  `json:"source,omitempty"`
	ExpiresAt       *string `json:"expires_at,omitempty"`
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	entitlements, err := s.db.GetUserEntitlements(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := subscriptionStatusResponse{}
	now := time.Now().UTC()
	for _, e := range entitlements {
		if e.Feature == models.EntitlementFeaturePrivateRepos && e.Active {
			if e.ExpiresAt != nil && e.ExpiresAt.Before(now) {
				continue
			}
			resp.HasPrivateRepos = true
			resp.Active = true
			resp.Feature = e.Feature
			resp.Source = e.Source
			if e.ExpiresAt != nil {
				formatted := e.ExpiresAt.Format(time.RFC3339)
				resp.ExpiresAt = &formatted
			}
			break
		}
	}

	jsonResponse(w, http.StatusOK, resp)
}
