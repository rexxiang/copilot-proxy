package middleware

import (
	"encoding/json"
	"net/http"
)

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		_, _ = w.Write([]byte(`{"error":"internal error"}`))
	}
}
