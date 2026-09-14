package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestV1Contract(t *testing.T) {
	data, err := os.ReadFile("testdata/v1/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		BaselineCommit string `json:"baseline_commit"`
		Cases          []struct {
			Name            string            `json:"name"`
			Method          string            `json:"method"`
			Path            string            `json:"path"`
			RequestBody     string            `json:"request_body"`
			RequestHeaders  map[string]string `json:"request_headers"`
			Status          int               `json:"status"`
			ResponseHeaders map[string]string `json:"response_headers"`
			Response        json.RawMessage   `json:"response"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.BaselineCommit != "f57c5a4b869909f6d4cbbb607162ccd420d0655b" || len(fixture.Cases) == 0 {
		t.Fatal("missing or replaced v1 contract baseline")
	}
	a := newAPI(t, nil)
	// Cases form one ordered scenario against real storage; do not parallelize.
	for _, tc := range fixture.Cases {
		if !t.Run(tc.Name, func(t *testing.T) {
			response := a.request(t, tc.Method, tc.Path, tc.RequestBody, tc.RequestHeaders)
			requireStatus(t, response, tc.Status)
			for name, want := range tc.ResponseHeaders {
				if got := response.header.Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			if bytes.Equal(tc.Response, []byte("null")) {
				if len(response.body) != 0 {
					t.Errorf("expected no response body, got %s", response.body)
				}
				return
			}
			var expected, actual any
			if err := json.Unmarshal(tc.Response, &expected); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(response.body, &actual); err != nil {
				t.Fatalf("response is not one JSON value: %v", err)
			}
			if err := matchV1JSON("$", expected, actual); err != nil {
				t.Fatal(err)
			}
		}) {
			return
		}
	}
}

// v1 permits new object fields, not removal or changes to existing values/types.
// Arrays retain their exact length and order. Only timestamps and human-readable
// error messages vary: those placeholders still require valid, non-null strings.
func matchV1JSON(path string, expected, actual any) error {
	switch expected := expected.(type) {
	case map[string]any:
		object, ok := actual.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected an object", path)
		}
		for name, want := range expected {
			got, exists := object[name]
			if !exists {
				return fmt.Errorf("%s.%s: required field is missing", path, name)
			}
			if err := matchV1JSON(path+"."+name, want, got); err != nil {
				return err
			}
		}
		return nil
	case []any:
		array, ok := actual.([]any)
		if !ok || len(array) != len(expected) {
			return fmt.Errorf("%s: expected an array of length %d", path, len(expected))
		}
		for index, want := range expected {
			if err := matchV1JSON(fmt.Sprintf("%s[%d]", path, index), want, array[index]); err != nil {
				return err
			}
		}
		return nil
	case string:
		if expected == "<utc-timestamp>" {
			value, ok := actual.(string)
			parsed, err := time.Parse(time.RFC3339Nano, value)
			_, offset := parsed.Zone()
			if !ok || err != nil || parsed.IsZero() || offset != 0 {
				return fmt.Errorf("%s: expected a non-zero UTC RFC3339 timestamp", path)
			}
			return nil
		}
		if expected == "<message>" {
			value, ok := actual.(string)
			if !ok || value == "" {
				return fmt.Errorf("%s: expected a non-empty error message", path)
			}
			return nil
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("%s: got %#v, want %#v", path, actual, expected)
	}
	return nil
}

func TestV1ContractRejectsBreakingChanges(t *testing.T) {
	// Negative controls: the compatibility matcher must fail on breaking changes,
	// while still allowing additions and human-readable message edits.
	for _, tc := range []struct {
		name, expected, actual string
		compatible             bool
	}{
		{"additive field", `{"enabled":false}`, `{"enabled":false,"id":"dev/checkout"}`, true},
		{"additive list metadata", `{"flags":[{"key":"a"}]}`, `{"flags":[{"key":"a","id":"dev/a"}],"count":1}`, true},
		{"message wording", `{"error":{"code":"not_found","message":"<message>"}}`, `{"error":{"code":"not_found","message":"New wording"}}`, true},
		{"missing field", `{"enabled":false}`, `{}`, false},
		{"changed type", `{"enabled":false}`, `{"enabled":"false"}`, false},
		{"changed default", `{"enabled":false}`, `{"enabled":true}`, false},
		{"null field", `{"description":""}`, `{"description":null}`, false},
		{"null list", `{"flags":[]}`, `{"flags":null}`, false},
		{"truncated list", `{"flags":[{"key":"a"},{"key":"z"}]}`, `{"flags":[{"key":"a"}]}`, false},
		{"list reordering", `{"flags":[{"key":"a"},{"key":"z"}]}`, `{"flags":[{"key":"z"},{"key":"a"}]}`, false},
		{"changed error code", `{"error":{"code":"not_found"}}`, `{"error":{"code":"missing"}}`, false},
		{"missing message", `{"message":"<message>"}`, `{"message":""}`, false},
		{"valid timestamp", `{"created_at":"<utc-timestamp>"}`, `{"created_at":"2026-09-14T12:00:00.123Z"}`, true},
		{"timestamp type", `{"created_at":"<utc-timestamp>"}`, `{"created_at":1789387200}`, false},
		{"timestamp format", `{"created_at":"<utc-timestamp>"}`, `{"created_at":"yesterday"}`, false},
		{"timestamp zone", `{"created_at":"<utc-timestamp>"}`, `{"created_at":"2026-09-14T12:00:00-04:00"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var expected, actual any
			if err := json.Unmarshal([]byte(tc.expected), &expected); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.actual), &actual); err != nil {
				t.Fatal(err)
			}
			err := matchV1JSON("$", expected, actual)
			if (err == nil) != tc.compatible {
				t.Fatalf("compatibility = %v, want compatible=%t", err, tc.compatible)
			}
		})
	}
}
