package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/models"
)

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenResponse struct {
	Token string       `json:"token"`
	User  *models.User `json:"user"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Email == "" {
		jsonError(w, "username and email are required", http.StatusBadRequest)
		return
	}

	hash := ""
	if req.Password == "" {
		// Passwordless accounts keep an unusable password hash so legacy password
		// login remains explicitly disabled for this user.
		random := make([]byte, 32)
		if _, err := rand.Read(random); err != nil {
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		var err error
		hash, err = s.authSvc.HashPassword(string(random))
		if err != nil {
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
	} else {
		var err error
		hash, err = s.authSvc.HashPassword(req.Password)
		if err != nil {
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	user := &models.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hash,
	}
	if err := s.db.CreateUser(r.Context(), user); err != nil {
		jsonError(w, "username or email already taken", http.StatusConflict)
		return
	}

	token, err := s.authSvc.GenerateToken(user.ID, user.Username)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, http.StatusCreated, tokenResponse{Token: token, User: user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, err := s.db.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := s.authSvc.CheckPassword(user.PasswordHash, req.Password); err != nil {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := s.authSvc.GenerateToken(user.ID, user.Username)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, http.StatusOK, tokenResponse{Token: token, User: user})
}

func (s *Server) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	token, err := s.authSvc.GenerateToken(claims.UserID, claims.Username)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	user, err := s.db.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, tokenResponse{Token: token, User: user})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		jsonError(w, "current_password and new_password are required", http.StatusBadRequest)
		return
	}
	if len(req.NewPassword) < 8 {
		jsonError(w, "new password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	user, err := s.db.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	if err := s.authSvc.CheckPassword(user.PasswordHash, req.CurrentPassword); err != nil {
		jsonError(w, "current password is incorrect", http.StatusUnauthorized)
		return
	}
	newHash, err := s.authSvc.HashPassword(req.NewPassword)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.db.UpdateUserPassword(r.Context(), claims.UserID, newHash); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	token, err := s.authSvc.GenerateToken(claims.UserID, claims.Username)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, tokenResponse{Token: token, User: user})
}

func (s *Server) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	user, err := s.db.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, user)
}
