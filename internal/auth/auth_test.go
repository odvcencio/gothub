package auth

import (
	"testing"
	"time"
)

func TestGenerateAndValidateToken(t *testing.T) {
	svc := NewService("test-secret-1234567890", time.Hour)

	token, err := svc.GenerateToken(42, "alice")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.UserID != 42 {
		t.Fatalf("claims.UserID = %d, want 42", claims.UserID)
	}
	if claims.Username != "alice" {
		t.Fatalf("claims.Username = %q, want %q", claims.Username, "alice")
	}
}

func TestValidateTokenExpired(t *testing.T) {
	svc := NewService("test-secret-1234567890", -time.Minute)

	token, err := svc.GenerateToken(7, "expired")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	_, err = svc.ValidateToken(token)
	if err != ErrTokenExpired {
		t.Fatalf("ValidateToken error = %v, want %v", err, ErrTokenExpired)
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	svc := NewService("test-secret-1234567890", time.Hour)

	hash, err := svc.HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if err := svc.CheckPassword(hash, "correct-horse-battery-staple"); err != nil {
		t.Fatalf("CheckPassword(valid): %v", err)
	}

	if err := svc.CheckPassword(hash, "wrong-password"); err != ErrInvalidCredentials {
		t.Fatalf("CheckPassword(invalid) error = %v, want %v", err, ErrInvalidCredentials)
	}
}
