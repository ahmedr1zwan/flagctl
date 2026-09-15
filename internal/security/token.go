// Package security handles explicit credential files, TLS trust, and origins.
package security

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

// Token is loaded from a private file. Formatting it never reveals its value.
type Token struct{ value string }

func (Token) String() string   { return "[redacted token]" }
func (Token) GoString() string { return "[redacted token]" }
func (t Token) IsSet() bool    { return t.value != "" }

// Authorization returns sensitive header data; callers must never log it.
func (t Token) Authorization() string { return "Bearer " + t.value }

// Matches accepts one Bearer header and compares fixed-size hashes in constant
// time. The token is opaque and case-sensitive; the authentication scheme is not.
func (t Token) Matches(headers []string) bool {
	if !t.IsSet() || len(headers) != 1 {
		return false
	}
	scheme, candidate, ok := strings.Cut(headers[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || len(candidate) != 64 {
		return false
	}
	expectedHash := sha256.Sum256([]byte(t.value))
	actualHash := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(expectedHash[:], actualHash[:]) == 1
}

// ReadTokenFile accepts 32 random bytes encoded as 64 lowercase hexadecimal
// characters, optionally followed by LF or CRLF. It never returns file contents
// or paths in errors. Tokens are read once; restart/reconfigure to rotate them.
func ReadTokenFile(path string) (Token, error) {
	data, err := readFile(path, true, 66)
	if err != nil {
		return Token{}, errors.New("could not read token file; use a private regular file (chmod 600), not a symlink")
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(data), "\r\n"), "\n")
	if len(value) != 64 || strings.ToLower(value) != value {
		return Token{}, errors.New("token file must contain 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return Token{}, errors.New("token file must contain 64 lowercase hexadecimal characters")
	}
	return Token{value: value}, nil
}
