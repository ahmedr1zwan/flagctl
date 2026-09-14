package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/api"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/ahmedr1zwan/flagctl/internal/store"
)

const collection = "/v1/environments/dev/flags"
const sensitiveMarker = "synthetic-sensitive-value"

type testAPI struct {
	server *httptest.Server
	client *http.Client
}

type reply struct {
	status int
	header http.Header
	body   []byte
}

func newAPI(t *testing.T, backend api.FlagStore) *testAPI {
	t.Helper()
	if backend == nil {
		s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "data"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		})
		backend = s
	}
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = api.NewHandler(backend, server.Listener.Addr().String())
	server.Start()
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 10 * time.Second
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &testAPI{server: server, client: client}
}

func (a *testAPI) request(t *testing.T, method, path, body string, headers map[string]string) reply {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, a.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if method == http.MethodPost || method == http.MethodPatch {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		if name == "Host" {
			request.Host = value
		} else {
			request.Header.Set(name, value)
		}
	}
	return a.send(t, request)
}

func (a *testAPI) send(t *testing.T, request *http.Request) reply {
	t.Helper()
	response, err := a.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing response security headers: %v", response.Header)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("unexpected CORS access")
	}
	return reply{status: response.StatusCode, header: response.Header, body: body}
}

func requireStatus(t *testing.T, response reply, status int) {
	t.Helper()
	if response.status != status {
		t.Fatalf("HTTP %d, want %d; body: %s", response.status, status, response.body)
	}
}

func requireError(t *testing.T, response reply, status int, code string) {
	t.Helper()
	requireStatus(t, response, status)
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != code || body.Error.Message == "" || response.header.Get("Content-Type") != "application/json" {
		t.Fatalf("invalid error envelope: %s", response.body)
	}
	if bytes.Contains(response.body, []byte(sensitiveMarker)) {
		t.Error("sensitive request/storage data appeared in an error response")
	}
}

func readFlag(t *testing.T, response reply, status int) flags.Flag {
	t.Helper()
	requireStatus(t, response, status)
	var flag flags.Flag
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.body, &flag); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(response.body, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"key", "environment", "description", "enabled", "created_at", "updated_at"} {
		if value, exists := fields[name]; !exists || bytes.Equal(value, []byte("null")) {
			t.Errorf("missing/null flag field %q", name)
		}
	}
	if response.header.Get("Content-Type") != "application/json" || flag.CreatedAt.IsZero() || flag.UpdatedAt.IsZero() {
		t.Fatalf("invalid flag headers/timestamps: %+v", flag)
	}
	return flag
}

func readList(t *testing.T, response reply) []flags.Flag {
	t.Helper()
	requireStatus(t, response, 200)
	var list struct {
		Flags []flags.Flag `json:"flags"`
	}
	if err := json.Unmarshal(response.body, &list); err != nil {
		t.Fatal(err)
	}
	if list.Flags == nil || response.header.Get("Content-Type") != "application/json" {
		t.Fatalf("list must contain a JSON array, including when empty: %s", response.body)
	}
	return list.Flags
}

func TestFlagLifecycle(t *testing.T) {
	a := newAPI(t, nil)
	if empty := readList(t, a.request(t, "GET", collection, "", nil)); len(empty) != 0 {
		t.Fatalf("new environment is not empty: %+v", empty)
	}
	response := a.request(t, "POST", collection, `{"key":"checkout","description":"New checkout"}`, nil)
	created := readFlag(t, response, 201)
	if created.Key != "checkout" || created.Environment != "dev" || created.Enabled || created.Description != "New checkout" || !created.CreatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("creation defaults/identity incorrect: %+v", created)
	}
	if response.header.Get("Location") != collection+"/checkout" {
		t.Errorf("unexpected Location: %q", response.header.Get("Location"))
	}
	prod := readFlag(t, a.request(t, "POST", "/v1/environments/prod/flags", `{"key":"checkout","enabled":true}`, nil), 201)
	requireError(t, a.request(t, "POST", collection, `{"key":"checkout","enabled":true}`, nil), 409, "already_exists")
	if got := readFlag(t, a.request(t, "GET", collection+"/checkout", "", nil), 200); got != created {
		t.Fatalf("duplicate changed the flag: %+v", got)
	}
	updated := readFlag(t, a.request(t, "PATCH", collection+"/checkout", `{"enabled":true}`, nil), 200)
	if !updated.Enabled || updated.Description != created.Description || !updated.CreatedAt.Equal(created.CreatedAt) || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("partial update/timestamps incorrect: %+v", updated)
	}
	if got := readFlag(t, a.request(t, "PATCH", collection+"/checkout", `{"enabled":true}`, nil), 200); got != updated {
		t.Fatalf("no-op changed the flag: %+v", got)
	}
	cleared := readFlag(t, a.request(t, "PATCH", collection+"/checkout", `{"description":""}`, nil), 200)
	if !cleared.Enabled || cleared.Description != "" {
		t.Fatalf("description-only update changed enabled: %+v", cleared)
	}
	disabled := readFlag(t, a.request(t, "PATCH", collection+"/checkout", `{"enabled":false,"description":"Restored"}`, nil), 200)
	if disabled.Enabled || disabled.Description != "Restored" || !disabled.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("explicit false/combined update incorrect: %+v", disabled)
	}
	for _, key := range []string{"z", "a"} {
		readFlag(t, a.request(t, "POST", collection, fmt.Sprintf(`{"key":%q}`, key), nil), 201)
	}
	listed := readList(t, a.request(t, "GET", collection, "", nil))
	if len(listed) != 3 || listed[0].Key != "a" || listed[1] != disabled || listed[2].Key != "z" {
		t.Fatalf("list ordering/content incorrect: %+v", listed)
	}
	// HEAD must be checked over HTTP: ResponseRecorder does not suppress bodies.
	for _, path := range []string{"/healthz", collection, collection + "/checkout"} {
		response := a.request(t, "HEAD", path, "", nil)
		requireStatus(t, response, 200)
		if len(response.body) != 0 || response.header.Get("Content-Type") != "application/json" {
			t.Errorf("HEAD returned a body or wrong content type: %s", path)
		}
	}
	deleted := a.request(t, "DELETE", collection+"/checkout", "", nil)
	requireStatus(t, deleted, 204)
	if len(deleted.body) != 0 || deleted.header.Get("Content-Type") != "" {
		t.Errorf("DELETE 204 must have no body/content type: %+v", deleted)
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		requireError(t, a.request(t, method, collection+"/checkout", `{"enabled":true}`, nil), 404, "not_found")
	}
	if got := readFlag(t, a.request(t, "GET", "/v1/environments/prod/flags/checkout", "", nil), 200); got != prod {
		t.Fatalf("dev operations changed prod: %+v", got)
	}
}

func TestRoutesAndMethods(t *testing.T) {
	a := newAPI(t, nil)
	health := a.request(t, "GET", "/healthz", "", nil)
	requireStatus(t, health, 200)
	var healthBody struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(health.body, &healthBody); err != nil || healthBody.Status != "ok" {
		t.Fatalf("health = %s", health.body)
	}
	for _, path := range []string{"/", "/healthz/", "/v2/environments/dev/flags", "/.env", "/debug/pprof"} {
		requireError(t, a.request(t, "GET", path, "", nil), 404, "not_found")
	}
	for _, tc := range []struct{ path, method, allow string }{
		{"/healthz", "POST", "GET, HEAD"},
		{collection, "PUT", "GET, HEAD, POST"},
		{collection + "/checkout", "PUT", "GET, HEAD, PATCH, DELETE"},
	} {
		response := a.request(t, tc.method, tc.path, "", nil)
		requireError(t, response, 405, "method_not_allowed")
		if response.header.Get("Allow") != tc.allow {
			t.Errorf("Allow = %q, want %q", response.header.Get("Allow"), tc.allow)
		}
	}
	for _, path := range []string{"/v1/environments/DEV/flags", collection + "/Bad-Key", collection + "/encoded%2Fslash"} {
		requireError(t, a.request(t, "GET", path, "", nil), 400, "invalid_request")
	}
}

func TestOriginAndHostProtectionsBeforeMutation(t *testing.T) {
	a := newAPI(t, nil)
	created := readFlag(t, a.request(t, "POST", collection, `{"key":"checkout"}`, nil), 201)
	for _, headers := range []map[string]string{
		{"Host": "unrelated.example"},
		{"Host": "localhost:1"},
		{"Origin": "https://unrelated.example"},
		{"Sec-Fetch-Site": "cross-site"},
	} {
		for _, method := range []string{"POST", "PATCH", "DELETE"} {
			path, body := collection+"/checkout", `{"enabled":true}`
			if method == "POST" {
				path, body = collection, `{"key":"blocked"}`
			}
			requireError(t, a.request(t, method, path, body, headers), 403, "forbidden")
		}
	}
	if got := readFlag(t, a.request(t, "GET", collection+"/checkout", "", nil), 200); got != created {
		t.Fatalf("blocked request mutated the flag: %+v", got)
	}
	requireError(t, a.request(t, "GET", collection+"/blocked", "", nil), 404, "not_found")
	for _, headers := range []map[string]string{
		{"Origin": a.server.URL},
		{"Sec-Fetch-Site": "same-origin"},
	} {
		readFlag(t, a.request(t, "PATCH", collection+"/checkout", `{"enabled":true}`, headers), 200)
	}
	_, port, err := net.SplitHostPort(a.server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, a.request(t, "GET", "/healthz", "", map[string]string{"Host": "LOCALHOST:" + port}), 200)
	// Safe cross-origin reads receive no CORS grant, so browsers cannot read them.
	requireStatus(t, a.request(t, "GET", collection, "", map[string]string{"Origin": "https://unrelated.example"}), 200)
}

func TestDefaultPortHostAliases(t *testing.T) {
	for _, tc := range []struct {
		address, host string
		status        int
	}{
		{"127.0.0.1:80", "127.0.0.1", 200},
		{"127.0.0.1:80", "localhost", 200},
		{"[::1]:80", "[::1]", 200},
		{"[::1]:80", "localhost", 200},
		{"127.0.0.1:80", "unrelated.example", 403},
		{"127.0.0.1:80", "127.0.0.2", 403},
		{"127.0.0.1:8080", "localhost", 403},
	} {
		request := httptest.NewRequest("GET", "http://localhost/healthz", nil)
		request.Host = tc.host
		response := httptest.NewRecorder()
		api.NewHandler(failingStore{}, tc.address).ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Errorf("address=%s Host=%s: status=%d, want=%d", tc.address, tc.host, response.Code, tc.status)
		}
	}
}

// A store that fails all operations lets tests exercise failure mapping without
// depending on SQLite's driver-specific error messages or damaging databases.
type failingStore struct{ err error }

func (s failingStore) Create(context.Context, string, flags.CreateInput) (flags.Flag, error) {
	return flags.Flag{}, s.err
}
func (s failingStore) List(context.Context, string) ([]flags.Flag, error) { return nil, s.err }
func (s failingStore) Get(context.Context, string, string) (flags.Flag, error) {
	return flags.Flag{}, s.err
}
func (s failingStore) Update(context.Context, string, string, flags.UpdateInput) (flags.Flag, error) {
	return flags.Flag{}, s.err
}
func (s failingStore) Delete(context.Context, string, string) error { return s.err }

func TestStorageFailuresDoNotLeakDetails(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	a := newAPI(t, failingStore{err: errors.New("database path/query: " + sensitiveMarker)})
	for _, tc := range []struct{ method, path, body string }{
		{"POST", collection, `{"key":"checkout"}`},
		{"GET", collection, ""},
		{"GET", collection + "/checkout", ""},
		{"PATCH", collection + "/checkout", `{"enabled":false}`},
		{"DELETE", collection + "/checkout", ""},
	} {
		requireError(t, a.request(t, tc.method, tc.path+"?token="+sensitiveMarker, tc.body, map[string]string{"Authorization": sensitiveMarker}), 500, "internal_error")
	}
	if strings.Contains(logs.String(), sensitiveMarker) || !strings.Contains(logs.String(), "flag storage operation failed") {
		t.Fatalf("expected sanitized storage diagnostics, got %q", logs.String())
	}
	// Wrapped domain errors must retain their public status/code mapping.
	a = newAPI(t, failingStore{err: fmt.Errorf("wrapped: %w", store.ErrAlreadyExists)})
	requireError(t, a.request(t, "POST", collection, `{"key":"checkout"}`, nil), 409, "already_exists")
	a = newAPI(t, failingStore{err: fmt.Errorf("wrapped: %w", store.ErrNotFound)})
	requireError(t, a.request(t, "GET", collection+"/checkout", "", nil), 404, "not_found")
}

type observingStore struct {
	failingStore
	observe func(context.Context)
}

func (s observingStore) Get(ctx context.Context, _, _ string) (flags.Flag, error) {
	s.observe(ctx)
	return flags.Flag{}, store.ErrNotFound
}

func TestRequestContextIsBoundedAndCanceled(t *testing.T) {
	for _, alreadyCanceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		if alreadyCanceled {
			cancel()
		}
		var observed context.Context
		backend := observingStore{observe: func(ctx context.Context) {
			observed = ctx
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 5*time.Second {
				t.Error("storage request lacks bounded deadline")
			}
			if alreadyCanceled && !errors.Is(ctx.Err(), context.Canceled) {
				t.Error("request cancellation was not propagated")
			}
		}}
		handler := api.NewHandler(backend, "127.0.0.1:8080")
		request := httptest.NewRequest("GET", "http://127.0.0.1:8080"+collection+"/checkout", nil).WithContext(ctx)
		handler.ServeHTTP(httptest.NewRecorder(), request)
		if observed == nil || !errors.Is(observed.Err(), context.Canceled) {
			t.Error("request context was not released after responding")
		}
	}
}
