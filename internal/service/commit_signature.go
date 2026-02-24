package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"golang.org/x/crypto/ssh"
)

func verifyCommitSignature(ctx context.Context, db database.DB, commit *object.CommitObj) (bool, string, error) {
	if commit == nil || strings.TrimSpace(commit.Signature) == "" {
		return false, "", nil
	}
	sigFormat, pubBytes, sigBlob, ok := parseSSHCommitSignature(commit.Signature)
	if !ok {
		return false, "", nil
	}

	pubKey, err := ssh.ParsePublicKey(pubBytes)
	if err != nil {
		return false, "", nil
	}
	payload := commitSigningPayloadForVerification(commit)
	if err := pubKey.Verify(payload, &ssh.Signature{Format: sigFormat, Blob: sigBlob}); err != nil {
		return false, "", nil
	}

	fp := fmt.Sprintf("%x", md5.Sum(pubKey.Marshal()))
	key, err := db.GetSSHKeyByFingerprint(ctx, fp)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, "", nil
		}
		return false, "", err
	}
	storedPubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.PublicKey))
	if err != nil {
		return false, "", nil
	}
	if !bytes.Equal(storedPubKey.Marshal(), pubKey.Marshal()) {
		return false, "", nil
	}

	user, err := db.GetUserByID(ctx, key.UserID)
	if err != nil {
		return true, "", nil
	}
	return true, user.Username, nil
}

func parseSSHCommitSignature(raw string) (sigFormat string, pubBytes []byte, sigBlob []byte, ok bool) {
	const prefix = "sshsig-v1:"
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, prefix) {
		return "", nil, nil, false
	}

	rest := strings.TrimPrefix(raw, prefix)
	parts := strings.Split(rest, ":")
	if len(parts) != 3 {
		return "", nil, nil, false
	}

	format := strings.TrimSpace(parts[0])
	pubB64 := strings.TrimSpace(parts[1])
	sigB64 := strings.TrimSpace(parts[2])
	if format == "" || pubB64 == "" || sigB64 == "" {
		return "", nil, nil, false
	}

	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return "", nil, nil, false
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return "", nil, nil, false
	}
	return format, pub, sig, true
}

func commitSigningPayloadForVerification(commit *object.CommitObj) []byte {
	if commit == nil {
		return nil
	}
	copyCommit := *commit
	copyCommit.Signature = ""
	return object.MarshalCommit(&copyCommit)
}
