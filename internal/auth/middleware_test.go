package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateTokenInvalidScenarios(t *testing.T) {
	svc := NewService("test-secret-1234567890", time.Hour)
	other := NewService("different-secret-123", time.Hour)

	tokenFromOtherSecret, err := other.GenerateToken(1, "alice")
	if err != nil {
		t.Fatalf("GenerateToken(other): %v", err)
	}
	validToken, err := svc.GenerateToken(2, "bob")
	if err != nil {
		t.Fatalf("GenerateToken(valid): %v", err)
	}

	tests := []struct {
		name     string
		tokenStr string
	}{
		{name: "malformed token", tokenStr: "not-a-jwt"},
		{name: "wrong signing secret", tokenStr: tokenFromOtherSecret},
		{name: "tampered token", tokenStr: mutateLastByte(validToken)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.ValidateToken(tc.tokenStr)
			if err != ErrInvalidToken {
				t.Fatalf("ValidateToken() error = %v, want %v", err, ErrInvalidToken)
			}
		})
	}
}

func TestMiddlewarePassesThroughWithoutBearerToken(t *testing.T) {
	svc := NewService("test-secret-1234567890", time.Hour)

	tests := []struct {
		name   string
		header string
	}{
		{name: "missing auth header"},
		{name: "non bearer auth header", header: "Basic abc123"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nextCalled := false
			handler := Middleware(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				if claims := GetClaims(r.Context()); claims != nil {
					t.Fatalf("GetClaims() = %+v, want nil", claims)
				}
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if !nextCalled {
				t.Fatal("next handler was not called")
			}
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
			}
		})
	}
}

func TestMiddlewareRejectsInvalidBearerToken(t *testing.T) {
	svc := NewService("test-secret-1234567890", time.Hour)
	nextCalled := false
	handler := Middleware(svc)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if nextCalled {
		t.Fatal("next handler should not be called when token is invalid")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), `{"error":"invalid token"}`) {
		t.Fatalf("response body = %q, want invalid token error", rec.Body.String())
	}
}

func TestMiddlewareAddsClaimsToContextForValidToken(t *testing.T) {
	svc := NewService("test-secret-1234567890", time.Hour)
	token, err := svc.GenerateToken(55, "carol")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	nextCalled := false
	handler := Middleware(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		claims := GetClaims(r.Context())
		if claims == nil {
			t.Fatal("GetClaims() = nil, want claims")
		}
		if claims.UserID != 55 || claims.Username != "carol" {
			t.Fatalf("claims = %+v, want user_id=55 username=carol", claims)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("next handler was not called")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestRequireAuthRejectsUnauthenticatedRequests(t *testing.T) {
	nextCalled := false
	handler := RequireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if nextCalled {
		t.Fatal("next handler should not be called for unauthenticated request")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), `{"error":"authentication required"}`) {
		t.Fatalf("response body = %q, want authentication required error", rec.Body.String())
	}
}

func TestRequireAuthAllowsAuthenticatedRequests(t *testing.T) {
	nextCalled := false
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		claims := GetClaims(r.Context())
		if claims == nil {
			t.Fatal("GetClaims() = nil, want claims")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsKey, &Claims{UserID: 77, Username: "dana"}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("next handler was not called")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func mutateLastByte(token string) string {
	if token == "" {
		return token
	}
	last := token[len(token)-1]
	replacement := byte('a')
	if last == replacement {
		replacement = 'b'
	}
	return token[:len(token)-1] + string(replacement)
}
