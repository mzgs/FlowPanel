package httpx

import (
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"strings"
)

const maxJSONBodyBytes = 1 << 20

var errInvalidJSONBody = errors.New("invalid JSON body")

func decodeJSON(r *stdhttp.Request, payload any) error {
	return decodeJSONWithLimit(r, payload, maxJSONBodyBytes)
}

func decodeJSONWithLimit(r *stdhttp.Request, payload any, maxBytes int64) error {
	r.Body = stdhttp.MaxBytesReader(nil, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(payload); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errInvalidJSONBody
	}
	return nil
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
