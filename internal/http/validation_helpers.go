package httpx

import (
	"encoding/json"
	stdhttp "net/http"
	"strings"
)

func decodeJSON(r *stdhttp.Request, payload any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(payload)
}

func trimmedQuery(r *stdhttp.Request, key string) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.URL.Query().Get(key))
}

func queryEnabled(r *stdhttp.Request, key string) bool {
	switch strings.ToLower(trimmedQuery(r, key)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func writeInvalidRequestBody(w stdhttp.ResponseWriter) {
	writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
		"error": "invalid request body",
	})
}

func writeValidationFailed(w stdhttp.ResponseWriter, fieldErrors map[string]string) {
	writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
		"error":        "validation failed",
		"field_errors": fieldErrors,
	})
}
