package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
)

func decodeCreate(w http.ResponseWriter, r *http.Request) (flags.CreateInput, error) {
	var input flags.CreateInput
	seen, err := decodeFields(w, r, map[string]any{
		"key": &input.Key, "description": &input.Description, "enabled": &input.Enabled,
	})
	if err == nil && !seen["key"] {
		err = errors.New("create requires key")
	}
	return input, err
}

func decodeUpdate(w http.ResponseWriter, r *http.Request) (flags.UpdateInput, error) {
	var input flags.UpdateInput
	_, err := decodeFields(w, r, map[string]any{
		"description": &input.Description, "enabled": &input.Enabled,
	})
	return input, err
}

// Decode field-by-field so encoding/json cannot silently accept duplicate keys,
// case-insensitive field names, or null values. Destinations determine the exact
// allowed fields and their types for each operation.
func decodeFields(w http.ResponseWriter, r *http.Request, destinations map[string]any) (map[string]bool, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil {
		return nil, err
	}
	invalid := errors.New("invalid JSON request")
	if !utf8.Valid(body) {
		return nil, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, invalid
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, invalid
		}
		name, ok := token.(string)
		if !ok || seen[name] {
			return nil, invalid
		}
		seen[name] = true
		destination, ok := destinations[name]
		if !ok {
			return nil, invalid
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, invalid
		}
		if err := json.Unmarshal(value, destination); err != nil {
			return nil, invalid
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, invalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, invalid
	}
	return seen, nil
}
