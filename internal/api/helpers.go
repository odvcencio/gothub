package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func parsePathPositiveInt(w http.ResponseWriter, r *http.Request, key, label string) (int, bool) {
	raw := strings.TrimSpace(r.PathValue(key))
	if raw == "" {
		jsonError(w, label+" is required", http.StatusBadRequest)
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		jsonError(w, "invalid "+label, http.StatusBadRequest)
		return 0, false
	}
	return value, true
}
