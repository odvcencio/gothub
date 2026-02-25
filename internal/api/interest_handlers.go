package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/models"
)

var simpleEmailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

type createInterestSignupRequest struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Company string `json:"company"`
	Message string `json:"message"`
	Source  string `json:"source"`
}

func (s *Server) handleCreateInterestSignup(w http.ResponseWriter, r *http.Request) {
	var req createInterestSignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = strings.TrimSpace(req.Name)
	req.Company = strings.TrimSpace(req.Company)
	req.Message = strings.TrimSpace(req.Message)
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = "web"
	}
	if req.Email == "" || !simpleEmailPattern.MatchString(req.Email) {
		jsonError(w, "valid email is required", http.StatusBadRequest)
		return
	}
	if len(req.Message) > 5000 {
		jsonError(w, "message exceeds 5000 characters", http.StatusBadRequest)
		return
	}

	signup := &models.InterestSignup{
		Email:   req.Email,
		Name:    req.Name,
		Company: req.Company,
		Message: req.Message,
		Source:  req.Source,
	}
	if err := s.db.CreateInterestSignup(r.Context(), signup); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusCreated, signup)
}

func (s *Server) handleListInterestSignups(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	user, err := s.db.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !user.IsAdmin {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	page, perPage := parsePagination(r, 50, 200)
	limit := perPage
	offset := (page - 1) * perPage
	signups, err := s.db.ListInterestSignupsPage(r.Context(), limit, offset)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, signups)
}
