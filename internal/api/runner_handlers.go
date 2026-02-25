package api

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/models"
)

const (
	runnerTokenAuthScheme  = "runner "
	maxRunnerTokenLifetime = 24 * 365 // hours
)

type createRunnerTokenRequest struct {
	Name           string `json:"name"`
	ExpiresInHours int    `json:"expires_in_hours"`
}

type createRunnerTokenResponse struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Token       string     `json:"token"`
	TokenPrefix string     `json:"token_prefix"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (s *Server) handleCreateRepoRunnerToken(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}

	var req createRunnerTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.ExpiresInHours < 0 || req.ExpiresInHours > maxRunnerTokenLifetime {
		jsonError(w, "expires_in_hours must be between 0 and 8760", http.StatusBadRequest)
		return
	}

	plainToken, tokenHash, prefix, err := generateRunnerToken()
	if err != nil {
		jsonError(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	token := &models.RepoRunnerToken{
		RepoID:          repo.ID,
		Name:            req.Name,
		TokenHash:       tokenHash,
		TokenPrefix:     prefix,
		CreatedByUserID: claims.UserID,
	}
	if req.ExpiresInHours > 0 {
		expiresAt := time.Now().UTC().Add(time.Duration(req.ExpiresInHours) * time.Hour)
		token.ExpiresAt = &expiresAt
	}

	if err := s.db.CreateRepoRunnerToken(r.Context(), token); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := createRunnerTokenResponse{
		ID:          token.ID,
		Name:        token.Name,
		Token:       plainToken,
		TokenPrefix: token.TokenPrefix,
		ExpiresAt:   token.ExpiresAt,
		CreatedAt:   token.CreatedAt,
	}
	jsonResponse(w, http.StatusCreated, resp)
}

func (s *Server) handleListRepoRunnerTokens(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}
	tokens, err := s.db.ListRepoRunnerTokens(r.Context(), repo.ID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, tokens)
}

func (s *Server) handleDeleteRepoRunnerToken(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.authorizeRepoRequest(w, r, true)
	if !ok {
		return
	}
	tokenID, ok := parsePathPositiveInt64(w, r, "id", "runner token id")
	if !ok {
		return
	}
	if err := s.db.DeleteRepoRunnerToken(r.Context(), repo.ID, tokenID); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpsertPRCheckRunByRunnerToken(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(r.PathValue("owner"))
	repoName := strings.TrimSpace(r.PathValue("repo"))
	if owner == "" || repoName == "" {
		jsonError(w, "repository is required", http.StatusBadRequest)
		return
	}
	repo, err := s.repoSvc.Get(r.Context(), owner, repoName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "repository not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	tokenValue := extractRunnerTokenValue(r)
	if tokenValue == "" {
		jsonError(w, "runner token required", http.StatusUnauthorized)
		return
	}
	token, err := s.db.GetRepoRunnerTokenByHash(r.Context(), hashRunnerToken(tokenValue))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "invalid runner token", http.StatusUnauthorized)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if token.RepoID != repo.ID || token.RevokedAt != nil || (token.ExpiresAt != nil && time.Now().UTC().After(*token.ExpiresAt)) {
		jsonError(w, "invalid runner token", http.StatusUnauthorized)
		return
	}
	_ = s.db.TouchRepoRunnerTokenUsed(r.Context(), token.ID, time.Now().UTC())

	number, ok := parsePathPositiveInt(w, r, "number", "pull request number")
	if !ok {
		return
	}
	pr, err := s.prSvc.Get(r.Context(), repo.ID, number)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "pull request not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	run, statusCode, msg := decodePRCheckRunUpsertRequest(r, pr.ID)
	if statusCode != 0 {
		jsonError(w, msg, statusCode)
		return
	}
	if err := s.prSvc.UpsertPRCheckRun(r.Context(), run); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, run)
}

func decodePRCheckRunUpsertRequest(r *http.Request, prID int64) (*models.PRCheckRun, int, string) {
	var req upsertPRCheckRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, http.StatusBadRequest, "invalid request body"
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, http.StatusBadRequest, "name is required"
	}

	status := strings.TrimSpace(strings.ToLower(req.Status))
	switch status {
	case "":
		if strings.TrimSpace(req.Conclusion) != "" {
			status = "completed"
		} else {
			status = "queued"
		}
	case "queued", "in_progress", "completed":
	default:
		return nil, http.StatusBadRequest, "status must be one of queued, in_progress, completed"
	}

	run := &models.PRCheckRun{
		PRID:       prID,
		Name:       req.Name,
		Status:     status,
		Conclusion: req.Conclusion,
		DetailsURL: req.DetailsURL,
		ExternalID: req.ExternalID,
		HeadCommit: req.HeadCommit,
	}
	return run, 0, ""
}

func extractRunnerTokenValue(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader != "" {
		lower := strings.ToLower(authHeader)
		if strings.HasPrefix(lower, runnerTokenAuthScheme) {
			if value := strings.TrimSpace(authHeader[len(runnerTokenAuthScheme):]); value != "" {
				return value
			}
		}
	}
	return strings.TrimSpace(r.Header.Get("X-Gothub-Runner-Token"))
}

func generateRunnerToken() (plain, tokenHash, prefix string, err error) {
	raw := make([]byte, 24)
	if _, err = rand.Read(raw); err != nil {
		return "", "", "", err
	}
	tokenValue := "grt_" + base64.RawURLEncoding.EncodeToString(raw)
	return tokenValue, hashRunnerToken(tokenValue), tokenValue[:min(len(tokenValue), 12)], nil
}

func hashRunnerToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
