package main

import (
	"testing"

	"github.com/odvcencio/gothub/internal/config"
)

func TestTrustedProxyCIDRsUsesConfiguredList(t *testing.T) {
	cfg := config.Default()
	cfg.Server.TrustedProxies = []string{"10.0.0.0/8"}
	t.Setenv("GOTHUB_TRUST_PROXY", "true")

	got := trustedProxyCIDRs(cfg)
	if len(got) != 1 {
		t.Fatalf("trustedProxyCIDRs length = %d, want 1", len(got))
	}
	if got[0] != "10.0.0.0/8" {
		t.Fatalf("trustedProxyCIDRs[0] = %q, want %q", got[0], "10.0.0.0/8")
	}
}

func TestTrustedProxyCIDRsUsesLegacyTrustAllFallback(t *testing.T) {
	cfg := config.Default()
	t.Setenv("GOTHUB_TRUST_PROXY", "true")

	got := trustedProxyCIDRs(cfg)
	if len(got) != 2 {
		t.Fatalf("trustedProxyCIDRs length = %d, want 2", len(got))
	}
	if got[0] != "0.0.0.0/0" {
		t.Fatalf("trustedProxyCIDRs[0] = %q, want %q", got[0], "0.0.0.0/0")
	}
	if got[1] != "::/0" {
		t.Fatalf("trustedProxyCIDRs[1] = %q, want %q", got[1], "::/0")
	}
}
