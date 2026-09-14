package api_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestInvalidJSONDoesNotMutate(t *testing.T) {
	a := newAPI(t, nil)
	created := readFlag(t, a.request(t, "POST", collection, `{"key":"checkout","enabled":true,"description":"keep"}`, nil), 201)
	for _, tc := range []struct{ name, create, update string }{
		{"empty body", "", ""},
		{"array", `[]`, `[]`},
		{"null object", `null`, `null`},
		{"no required fields", `{}`, `{}`},
		{"trailing JSON", `{"key":"invalid"} {}`, `{"enabled":false} {}`},
		{"trailing garbage", `{"key":"invalid"}x`, `{"enabled":false}x`},
		{"unknown field", `{"key":"invalid","extra":"` + sensitiveMarker + `"}`, `{"enabled":false,"extra":"` + sensitiveMarker + `"}`},
		{"case mismatch", `{"Key":"invalid"}`, `{"Enabled":false}`},
		{"duplicate", `{"key":"invalid","key":"another"}`, `{"enabled":false,"enabled":true}`},
		{"escaped duplicate", `{"key":"invalid","\u006bey":"another"}`, `{"enabled":false,"\u0065nabled":true}`},
		{"null enabled", `{"key":"invalid","enabled":null}`, `{"enabled":null}`},
		{"null description", `{"key":"invalid","description":null}`, `{"description":null}`},
		{"wrong bool type", `{"key":"invalid","enabled":"false"}`, `{"enabled":"false"}`},
		{"wrong description type", `{"key":"invalid","description":2}`, `{"description":2}`},
		{"supplied identity", `{"key":"invalid","environment":"prod"}`, `{"environment":"prod"}`},
		{"supplied creation time", `{"key":"invalid","created_at":"2026-09-13T00:00:00Z"}`, `{"created_at":"2026-09-13T00:00:00Z"}`},
		{"supplied update time", `{"key":"invalid","updated_at":"2026-09-13T00:00:00Z"}`, `{"updated_at":"2026-09-13T00:00:00Z"}`},
		{"oversized description", `{"key":"invalid","description":"` + strings.Repeat("é", 513) + `"}`, `{"description":"` + strings.Repeat("é", 513) + `"}`},
		{"invalid UTF8", "{\"key\":\"invalid\",\"description\":\"\xff\"}", "{\"description\":\"\xff\"}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireError(t, a.request(t, "POST", collection, tc.create, nil), 400, "invalid_request")
			requireError(t, a.request(t, "PATCH", collection+"/checkout", tc.update, nil), 400, "invalid_request")
			if got := readFlag(t, a.request(t, "GET", collection+"/checkout", "", nil), 200); got != created {
				t.Fatalf("invalid PATCH changed the record: %+v", got)
			}
		})
	}
	for _, body := range []string{`{"key":null}`, `{"key":2}`, `{"key":"Bad Key"}`} {
		requireError(t, a.request(t, "POST", collection, body, nil), 400, "invalid_request")
	}
	requireError(t, a.request(t, "PATCH", collection+"/checkout", `{"key":"renamed"}`, nil), 400, "invalid_request")
	// No failed creation may leave behind either of the keys used above.
	for _, key := range []string{"invalid", "another", "renamed"} {
		requireError(t, a.request(t, "GET", collection+"/"+key, "", nil), 404, "not_found")
	}
	if listed := readList(t, a.request(t, "GET", collection, "", nil)); len(listed) != 1 || listed[0] != created {
		t.Fatalf("invalid requests changed the stored flags: %+v", listed)
	}
}

func TestMediaTypesAndBodyLimits(t *testing.T) {
	a := newAPI(t, nil)
	created := readFlag(t, a.request(t, "POST", collection, `{"key":"checkout"}`, nil), 201)
	for _, headers := range []map[string]string{
		{"Content-Type": ""},
		{"Content-Type": "text/plain"},
		{"Content-Type": "application/json; charset=iso-8859-1"},
		{"Content-Encoding": "gzip"},
	} {
		requireError(t, a.request(t, "POST", collection, `{"key":"invalid"}`, headers), 415, "unsupported_media_type")
		requireError(t, a.request(t, "PATCH", collection+"/checkout", `{"enabled":true}`, headers), 415, "unsupported_media_type")
	}
	if got := readFlag(t, a.request(t, "GET", collection+"/checkout", "", nil), 200); got != created {
		t.Fatalf("rejected media type mutated record: %+v", got)
	}
	requireError(t, a.request(t, "GET", collection+"/invalid", "", nil), 404, "not_found")
	headers := map[string]string{"Content-Type": "application/json; charset=UTF-8", "Content-Encoding": "identity"}
	boundary := `{"key":"boundary","description":"` + strings.Repeat("é", 512) + `"}`
	boundary += strings.Repeat(" ", (16<<10)-len(boundary))
	flag := readFlag(t, a.request(t, "POST", collection, boundary, headers), 201)
	if len(flag.Description) != 1024 {
		t.Fatal("description byte boundary did not round-trip")
	}
	update := `{"enabled":true}`
	update += strings.Repeat(" ", (16<<10)-len(update))
	flag = readFlag(t, a.request(t, "PATCH", collection+"/checkout", update, headers), 200)
	if !flag.Enabled {
		t.Fatal("exact body limit rejected a valid update")
	}
	for _, chunked := range []bool{false, true} {
		for _, method := range []string{"POST", "PATCH"} {
			path, body := collection+"/checkout", update+" "
			if method == "POST" {
				path, body = collection, boundary+" "
			}
			request, err := http.NewRequestWithContext(t.Context(), method, a.server.URL+path, io.NopCloser(strings.NewReader(body)))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			if chunked {
				request.ContentLength = -1
			} else {
				request.ContentLength = int64(len(body))
			}
			requireError(t, a.send(t, request), 413, "request_too_large")
			if got := readFlag(t, a.request(t, "GET", collection+"/checkout", "", nil), 200); got != flag {
				t.Fatalf("oversized request mutated record: %+v", got)
			}
		}
	}
}
