package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestParseOptionalQueryPositiveInt_DefaultAndValidValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	value, ok := parseOptionalQueryPositiveInt(rec, req, "limit", "limit", 25)
	if !ok {
		t.Fatal("expected default value parse to succeed")
	}
	if value != 25 {
		t.Fatalf("expected default value 25, got %d", value)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 when no query value is present, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/?limit=17", nil)
	rec = httptest.NewRecorder()
	value, ok = parseOptionalQueryPositiveInt(rec, req, "limit", "limit", 25)
	if !ok {
		t.Fatal("expected explicit positive query value to parse")
	}
	if value != 17 {
		t.Fatalf("expected parsed value 17, got %d", value)
	}

	maxInt := int(^uint(0) >> 1)
	req = httptest.NewRequest(http.MethodGet, "/?limit="+strconv.Itoa(maxInt), nil)
	rec = httptest.NewRecorder()
	value, ok = parseOptionalQueryPositiveInt(rec, req, "limit", "limit", 25)
	if !ok {
		t.Fatal("expected max int query value to parse")
	}
	if value != maxInt {
		t.Fatalf("expected parsed value %d, got %d", maxInt, value)
	}
}

func TestParseOptionalQueryPositiveInt_InvalidValues(t *testing.T) {
	tests := []string{
		"abc",
		"0",
		"-1",
		"9223372036854775808",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?limit="+raw, nil)
			rec := httptest.NewRecorder()
			if _, ok := parseOptionalQueryPositiveInt(rec, req, "limit", "limit", 25); ok {
				t.Fatalf("expected parse failure for %q", raw)
			}
			assertJSONError(t, rec, http.StatusBadRequest, "invalid limit query parameter")
		})
	}
}

func TestParsePathPositiveInt64(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	if _, ok := parsePathPositiveInt64(rec, req, "id", "webhook id"); ok {
		t.Fatal("expected missing path value to fail")
	}
	assertJSONError(t, rec, http.StatusBadRequest, "webhook id is required")

	for _, raw := range []string{"abc", "0", "-1", "9223372036854775808"} {
		t.Run("invalid_"+raw, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.SetPathValue("id", raw)
			rec := httptest.NewRecorder()
			if _, ok := parsePathPositiveInt64(rec, req, "id", "webhook id"); ok {
				t.Fatalf("expected invalid path value %q to fail", raw)
			}
			assertJSONError(t, rec, http.StatusBadRequest, "invalid webhook id")
		})
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetPathValue("id", " 42 ")
	rec = httptest.NewRecorder()
	id, ok := parsePathPositiveInt64(rec, req, "id", "webhook id")
	if !ok {
		t.Fatal("expected valid path value to parse")
	}
	if id != 42 {
		t.Fatalf("expected id 42, got %d", id)
	}
}

func assertJSONError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantError string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("expected status %d, got %d", wantStatus, rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	if got := body["error"]; got != wantError {
		t.Fatalf("expected error %q, got %q", wantError, got)
	}
}
