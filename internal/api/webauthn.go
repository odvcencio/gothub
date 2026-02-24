package api

import (
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	defaultWebAuthnOrigin      = "http://localhost:3000"
	defaultWebAuthnDisplayName = "gothub"
)

func initWebAuthn() *webauthn.WebAuthn {
	origin := strings.TrimSpace(os.Getenv("GOTHUB_WEBAUTHN_ORIGIN"))
	if origin == "" {
		origin = defaultWebAuthnOrigin
	}
	rpID := strings.TrimSpace(os.Getenv("GOTHUB_WEBAUTHN_RPID"))
	if rpID == "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Hostname() == "" {
			slog.Error("init webauthn", "error", "invalid GOTHUB_WEBAUTHN_ORIGIN", "origin", origin)
			return nil
		}
		rpID = parsed.Hostname()
	}

	engine, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: defaultWebAuthnDisplayName,
		RPOrigins:     []string{origin},
	})
	if err != nil {
		slog.Error("init webauthn", "error", err)
		return nil
	}
	return engine
}
