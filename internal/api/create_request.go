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

// Decode field-by-field so encoding/json cannot silently accept duplicate keys,
// case-insensitive field names, or null values in primitive fields.
func decodeCreate(w http.ResponseWriter, r *http.Request) (flags.CreateInput, error) {
	var input flags.CreateInput
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil {
		return input, err
	}
	invalid := errors.New("invalid create request")
	if !utf8.Valid(body) {
		return input, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return input, invalid
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return input, invalid
		}
		name, ok := token.(string)
		if !ok || seen[name] {
			return input, invalid
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return input, invalid
		}
		switch name {
		case "key":
			err = json.Unmarshal(value, &input.Key)
		case "description":
			err = json.Unmarshal(value, &input.Description)
		case "enabled":
			err = json.Unmarshal(value, &input.Enabled)
		default:
			return input, invalid
		}
		if err != nil {
			return input, invalid
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return input, invalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || !seen["key"] {
		return input, invalid
	}
	return input, nil
}
