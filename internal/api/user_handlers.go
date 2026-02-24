package api

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/odvcencio/gothub/internal/auth"
	"github.com/odvcencio/gothub/internal/models"
	"golang.org/x/crypto/ssh"
)

type createSSHKeyRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

func (s *Server) handleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	keys, err := s.db.ListSSHKeys(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if keys == nil {
		keys = []models.SSHKey{}
	}
	jsonResponse(w, http.StatusOK, keys)
}

func (s *Server) handleCreateSSHKey(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	var req createSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.PublicKey == "" {
		jsonError(w, "name and public_key are required", http.StatusBadRequest)
		return
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(req.PublicKey))
	if err != nil {
		jsonError(w, "invalid SSH public key", http.StatusBadRequest)
		return
	}

	fp := fmt.Sprintf("%x", md5.Sum(pubKey.Marshal()))
	key := &models.SSHKey{
		UserID:      claims.UserID,
		Name:        req.Name,
		Fingerprint: fp,
		PublicKey:   req.PublicKey,
		KeyType:     pubKey.Type(),
	}
	if err := s.db.CreateSSHKey(r.Context(), key); err != nil {
		jsonError(w, "key already exists", http.StatusConflict)
		return
	}
	jsonResponse(w, http.StatusCreated, key)
}

func (s *Server) handleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	id, ok := parsePathPositiveInt64(w, r, "id", "key ID")
	if !ok {
		return
	}
	if err := s.db.DeleteSSHKey(r.Context(), id, claims.UserID); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
