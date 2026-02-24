package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/models"
	"golang.org/x/crypto/ssh"
)

const (
	magicLinkTTL       = 15 * time.Minute
	sshChallengeTTL    = 5 * time.Minute
	webauthnSessionTTL = 10 * time.Minute
)

func (s *Server) handleRequestMagicLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		jsonError(w, "email is required", http.StatusBadRequest)
		return
	}

	resp := map[string]any{"sent": true}
	user, err := s.db.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Avoid user enumeration.
			jsonResponse(w, http.StatusOK, resp)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	token, err := randomToken(32)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	expires := time.Now().Add(magicLinkTTL).UTC()
	if err := s.db.CreateMagicLinkToken(r.Context(), &models.MagicLinkToken{
		UserID:    user.ID,
		TokenHash: sha256Hex(token),
		ExpiresAt: expires,
	}); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Until outbound email delivery is integrated, expose token for first-party UI.
	resp["token"] = token
	resp["expires_at"] = expires
	slog.Info("magic link issued", "user_id", user.ID, "email", email)
	jsonResponse(w, http.StatusOK, resp)
}

func (s *Server) handleVerifyMagicLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		jsonError(w, "token is required", http.StatusBadRequest)
		return
	}

	user, err := s.db.ConsumeMagicLinkToken(r.Context(), sha256Hex(token), time.Now().UTC())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jwtToken, err := s.authSvc.GenerateToken(user.ID, user.Username)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, tokenResponse{Token: jwtToken, User: user})
}

func (s *Server) handleSSHChallenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Fingerprint string `json:"fingerprint,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		jsonError(w, "username is required", http.StatusBadRequest)
		return
	}

	user, err := s.db.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	keys, err := s.db.ListSSHKeys(r.Context(), user.ID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(keys) == 0 {
		jsonError(w, "no SSH keys registered for user", http.StatusBadRequest)
		return
	}
	var selected *models.SSHKey
	fp := strings.TrimSpace(req.Fingerprint)
	if fp == "" {
		selected = &keys[0]
	} else {
		for i := range keys {
			if keys[i].Fingerprint == fp {
				selected = &keys[i]
				break
			}
		}
	}
	if selected == nil {
		jsonError(w, "ssh key fingerprint not found", http.StatusBadRequest)
		return
	}

	challengeID := uuid.NewString()
	expires := time.Now().Add(sshChallengeTTL).UTC()
	challengeText := fmt.Sprintf(
		"gothub-ssh-auth-v1\nchallenge:%s\nuser:%s\nfingerprint:%s\nexpires:%d\n",
		challengeID,
		user.Username,
		selected.Fingerprint,
		expires.Unix(),
	)
	if err := s.db.CreateSSHAuthChallenge(r.Context(), &models.SSHAuthChallenge{
		ID:          challengeID,
		UserID:      user.ID,
		Fingerprint: selected.Fingerprint,
		Challenge:   challengeText,
		ExpiresAt:   expires,
	}); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"challenge_id": challengeID,
		"challenge":    challengeText,
		"fingerprint":  selected.Fingerprint,
		"expires_at":   expires,
	})
}

func (s *Server) handleSSHVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChallengeID     string `json:"challenge_id"`
		Signature       string `json:"signature"`        // base64(ssh.Signature.Blob)
		SignatureFormat string `json:"signature_format"` // e.g. ssh-ed25519, rsa-sha2-512
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ChallengeID) == "" || strings.TrimSpace(req.Signature) == "" {
		jsonError(w, "challenge_id and signature are required", http.StatusBadRequest)
		return
	}

	challenge, err := s.db.ConsumeSSHAuthChallenge(r.Context(), strings.TrimSpace(req.ChallengeID), time.Now().UTC())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "invalid or expired challenge", http.StatusUnauthorized)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	key, err := s.db.GetSSHKeyByFingerprint(r.Context(), challenge.Fingerprint)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.PublicKey))
	if err != nil {
		jsonError(w, "stored ssh key is invalid", http.StatusInternalServerError)
		return
	}

	sigBlob, err := decodeBase64(req.Signature)
	if err != nil {
		jsonError(w, "signature must be valid base64", http.StatusBadRequest)
		return
	}
	sigFormat := strings.TrimSpace(req.SignatureFormat)
	if sigFormat == "" {
		sigFormat = pubKey.Type()
	}
	if err := pubKey.Verify([]byte(challenge.Challenge), &ssh.Signature{
		Format: sigFormat,
		Blob:   sigBlob,
	}); err != nil {
		jsonError(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	user, err := s.db.GetUserByID(r.Context(), challenge.UserID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jwtToken, err := s.authSvc.GenerateToken(user.ID, user.Username)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, tokenResponse{Token: jwtToken, User: user})
}

func (s *Server) handleBeginWebAuthnRegistration(w http.ResponseWriter, r *http.Request) {
	if s.passkey == nil {
		jsonError(w, "webauthn is not configured", http.StatusServiceUnavailable)
		return
	}
	claims := auth.GetClaims(r.Context())
	user, err := s.db.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	waUser, err := s.loadWebAuthnUser(r.Context(), user)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	options, sessionData, err := s.passkey.BeginRegistration(waUser, webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred))
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if sessionData.Expires.IsZero() {
		sessionData.Expires = time.Now().Add(webauthnSessionTTL).UTC()
	}
	raw, err := json.Marshal(sessionData)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	sessionID := uuid.NewString()
	if err := s.db.CreateWebAuthnSession(r.Context(), &models.WebAuthnSession{
		ID:        sessionID,
		UserID:    user.ID,
		Flow:      "register",
		DataJSON:  string(raw),
		ExpiresAt: sessionData.Expires,
	}); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"options":    options,
	})
}

func (s *Server) handleFinishWebAuthnRegistration(w http.ResponseWriter, r *http.Request) {
	if s.passkey == nil {
		jsonError(w, "webauthn is not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		SessionID  string          `json:"session_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SessionID) == "" || len(req.Credential) == 0 {
		jsonError(w, "session_id and credential are required", http.StatusBadRequest)
		return
	}

	claims := auth.GetClaims(r.Context())
	sessionRec, err := s.db.ConsumeWebAuthnSession(r.Context(), strings.TrimSpace(req.SessionID), "register", time.Now().UTC())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "invalid or expired session", http.StatusUnauthorized)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sessionRec.UserID != claims.UserID {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	user, err := s.db.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	waUser, err := s.loadWebAuthnUser(r.Context(), user)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(sessionRec.DataJSON), &sessionData); err != nil {
		jsonError(w, "invalid session data", http.StatusInternalServerError)
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		jsonError(w, "invalid credential response", http.StatusBadRequest)
		return
	}
	credential, err := s.passkey.CreateCredential(waUser, sessionData, parsed)
	if err != nil {
		jsonError(w, "credential validation failed", http.StatusBadRequest)
		return
	}
	credRaw, err := json.Marshal(credential)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	credID := base64.RawURLEncoding.EncodeToString(credential.ID)
	if err := s.db.CreateWebAuthnCredential(r.Context(), &models.WebAuthnCredential{
		UserID:       user.ID,
		CredentialID: credID,
		DataJSON:     string(credRaw),
	}); err != nil {
		jsonError(w, "credential already exists", http.StatusConflict)
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]any{"credential_id": credID})
}

func (s *Server) handleBeginWebAuthnLogin(w http.ResponseWriter, r *http.Request) {
	if s.passkey == nil {
		jsonError(w, "webauthn is not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		jsonError(w, "username is required", http.StatusBadRequest)
		return
	}
	user, err := s.db.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	waUser, err := s.loadWebAuthnUser(r.Context(), user)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(waUser.credentials) == 0 {
		jsonError(w, "no passkeys registered for user", http.StatusBadRequest)
		return
	}

	options, sessionData, err := s.passkey.BeginLogin(waUser)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if sessionData.Expires.IsZero() {
		sessionData.Expires = time.Now().Add(webauthnSessionTTL).UTC()
	}
	raw, err := json.Marshal(sessionData)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	sessionID := uuid.NewString()
	if err := s.db.CreateWebAuthnSession(r.Context(), &models.WebAuthnSession{
		ID:        sessionID,
		UserID:    user.ID,
		Flow:      "login",
		DataJSON:  string(raw),
		ExpiresAt: sessionData.Expires,
	}); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"options":    options,
	})
}

func (s *Server) handleFinishWebAuthnLogin(w http.ResponseWriter, r *http.Request) {
	if s.passkey == nil {
		jsonError(w, "webauthn is not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		SessionID  string          `json:"session_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SessionID) == "" || len(req.Credential) == 0 {
		jsonError(w, "session_id and credential are required", http.StatusBadRequest)
		return
	}

	sessionRec, err := s.db.ConsumeWebAuthnSession(r.Context(), strings.TrimSpace(req.SessionID), "login", time.Now().UTC())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "invalid or expired session", http.StatusUnauthorized)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	user, err := s.db.GetUserByID(r.Context(), sessionRec.UserID)
	if err != nil {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	waUser, err := s.loadWebAuthnUser(r.Context(), user)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(sessionRec.DataJSON), &sessionData); err != nil {
		jsonError(w, "invalid session data", http.StatusInternalServerError)
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(req.Credential)
	if err != nil {
		jsonError(w, "invalid credential response", http.StatusBadRequest)
		return
	}
	credential, err := s.passkey.ValidateLogin(waUser, sessionData, parsed)
	if err != nil {
		jsonError(w, "passkey verification failed", http.StatusUnauthorized)
		return
	}
	credRaw, err := json.Marshal(credential)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	if err := s.db.UpdateWebAuthnCredential(r.Context(), &models.WebAuthnCredential{
		UserID:       user.ID,
		CredentialID: base64.RawURLEncoding.EncodeToString(credential.ID),
		DataJSON:     string(credRaw),
		LastUsedAt:   &now,
	}); err != nil {
		slog.Warn("update webauthn credential", "error", err, "user_id", user.ID)
	}

	jwtToken, err := s.authSvc.GenerateToken(user.ID, user.Username)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, tokenResponse{Token: jwtToken, User: user})
}

type webAuthnUser struct {
	user        *models.User
	credentials []webauthn.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte {
	return []byte(strconv.FormatInt(u.user.ID, 10))
}

func (u *webAuthnUser) WebAuthnName() string {
	return u.user.Username
}

func (u *webAuthnUser) WebAuthnDisplayName() string {
	return u.user.Username
}

func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

func (s *Server) loadWebAuthnUser(ctx context.Context, user *models.User) (*webAuthnUser, error) {
	rows, err := s.db.ListWebAuthnCredentials(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	creds := make([]webauthn.Credential, 0, len(rows))
	for _, row := range rows {
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(row.DataJSON), &c); err != nil {
			return nil, fmt.Errorf("decode webauthn credential %s: %w", row.CredentialID, err)
		}
		creds = append(creds, c)
	}
	return &webAuthnUser{user: user, credentials: creds}, nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func decodeBase64(value string) ([]byte, error) {
	decoders := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range decoders {
		if out, err := enc.DecodeString(value); err == nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 input")
}
