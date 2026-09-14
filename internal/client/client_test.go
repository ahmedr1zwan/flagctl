package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/api"
	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/ahmedr1zwan/flagctl/internal/store"
)

const marker = "synthetic-sensitive-value"

func newClient(t *testing.T, endpoint string, timeout time.Duration) *client.Client {
	t.Helper()
	c, err := client.New(endpoint, timeout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func serve(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return s
}

func fixture() flags.Flag {
	return flags.Flag{Key: "checkout", Environment: "dev", Description: "keep", Enabled: true,
		CreatedAt: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 9, 13, 0, 0, 1, 0, time.UTC)}
}

func TestServerConfiguration(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:8080", "http://127.0.0.2", "http://localhost/", "https://LOCALHOST:443", "http://[::1]:8080", "http://[::ffff:127.0.0.1]:8080"} {
		newClient(t, endpoint, time.Second)
	}
	for _, endpoint := range []string{"", "not a URL", "file:///tmp/flags.db", "http://example.com", "http://0.0.0.0:8080", "http://[::]:8080", "http://localhost:0", "http://localhost:65536", "http://localhost:", "http://localhost:invalid", "http://[::1%25lo0]:8080", "http://localhost/v1", "http://localhost/%2f", "http://localhost/?", "http://localhost/#", "http://user:" + marker + "@localhost", "http://localhost/?token=" + marker, "http://localhost/#" + marker} {
		c, err := client.New(endpoint, time.Second)
		if c != nil {
			c.Close()
			t.Error("constructed client for invalid endpoint")
		}
		if err == nil || strings.Contains(err.Error(), marker) {
			t.Errorf("invalid endpoint error = %v", err)
		}
	}
	for _, timeout := range []time.Duration{0, -time.Second} {
		if c, err := client.New(client.DefaultServer, timeout); err == nil {
			c.Close()
			t.Error("accepted non-positive timeout")
		}
	}
}

func TestLifecycleAgainstService(t *testing.T) {
	t.Parallel()
	backend, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	s := httptest.NewUnstartedServer(nil)
	s.Config.Handler = api.NewHandler(backend, s.Listener.Addr().String())
	s.Start()
	t.Cleanup(s.Close)
	c := newClient(t, s.URL, 5*time.Second)
	ctx := t.Context()
	if items, err := c.List(ctx, "dev"); err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty list = %+v, %v", items, err)
	}
	created, err := c.Create(ctx, "dev", flags.CreateInput{Key: "checkout", Description: "original", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Get(ctx, "dev", "checkout"); err != nil || got != created {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if got, err := c.List(ctx, "dev"); err != nil || !reflect.DeepEqual(got, []flags.Flag{created}) {
		t.Fatalf("list = %+v, %v", got, err)
	}
	prod, err := c.Create(ctx, "prod", flags.CreateInput{Key: "checkout"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Create(ctx, "dev", flags.CreateInput{Key: "checkout"})
	requireAPIError(t, err, 409, "already_exists")
	disabled, empty := false, ""
	updated, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &disabled})
	if err != nil || updated.Enabled || updated.Description != "original" || !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("partial update = %+v, %v", updated, err)
	}
	cleared, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{Description: &empty})
	if err != nil || cleared.Enabled || cleared.Description != "" {
		t.Fatalf("clear description = %+v, %v", cleared, err)
	}
	if got, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &disabled, Description: &empty}); err != nil || got != cleared {
		t.Fatalf("no-op = %+v, %v", got, err)
	}
	if err := c.Delete(ctx, "dev", "checkout"); err != nil {
		t.Fatal(err)
	}
	_, err = c.Get(ctx, "dev", "checkout")
	requireAPIError(t, err, 404, "not_found")
	_, err = c.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &disabled})
	requireAPIError(t, err, 404, "not_found")
	requireAPIError(t, c.Delete(ctx, "dev", "checkout"), 404, "not_found")
	if got, err := c.Get(ctx, "prod", "checkout"); err != nil || got != prod {
		t.Fatalf("dev changed prod: %+v, %v", got, err)
	}
}

func TestRequestSerialization(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		body               map[string]any
		operation          func(context.Context, *client.Client) error
	}{
		{"create", "POST", "/v1/environments/dev/flags", map[string]any{"key": "checkout", "description": "", "enabled": false}, func(ctx context.Context, c *client.Client) error {
			_, err := c.Create(ctx, "dev", flags.CreateInput{Key: "checkout"})
			return err
		}},
		{"list", "GET", "/v1/environments/dev/flags", nil, func(ctx context.Context, c *client.Client) error { _, err := c.List(ctx, "dev"); return err }},
		{"get", "GET", "/v1/environments/dev/flags/checkout", nil, func(ctx context.Context, c *client.Client) error { _, err := c.Get(ctx, "dev", "checkout"); return err }},
		{"false", "PATCH", "/v1/environments/dev/flags/checkout", map[string]any{"enabled": false}, func(ctx context.Context, c *client.Client) error {
			value := false
			_, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &value})
			return err
		}},
		{"empty", "PATCH", "/v1/environments/dev/flags/checkout", map[string]any{"description": ""}, func(ctx context.Context, c *client.Client) error {
			value := ""
			_, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{Description: &value})
			return err
		}},
		{"delete", "DELETE", "/v1/environments/dev/flags/checkout", nil, func(ctx context.Context, c *client.Client) error { return c.Delete(ctx, "dev", "checkout") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			s := serve(t, func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.Method != tc.method || r.URL.Path != tc.path || r.Header.Get("Accept") != "application/json" {
					t.Error("wrong method, path, or Accept header")
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				flag := fixture()
				if tc.body == nil {
					if len(raw) != 0 || r.Header.Get("Content-Type") != "" {
						t.Error("bodyless operation sent a body/content type")
					}
				} else {
					var body map[string]any
					if err := json.Unmarshal(raw, &body); err != nil || !reflect.DeepEqual(body, tc.body) {
						t.Errorf("request body = %s, want %+v", raw, tc.body)
					}
					if r.Header.Get("Content-Type") != "application/json" {
						t.Error("missing JSON request content type")
					}
					if value, ok := body["enabled"].(bool); ok {
						flag.Enabled = value
					}
					if value, ok := body["description"].(string); ok {
						flag.Description = value
					}
				}
				if r.Method == "DELETE" {
					w.WriteHeader(204)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "POST" {
					w.WriteHeader(201)
				}
				if tc.name == "list" {
					json.NewEncoder(w).Encode(map[string]any{"flags": []flags.Flag{flag}})
				} else {
					json.NewEncoder(w).Encode(flag)
				}
			})
			if err := tc.operation(t.Context(), newClient(t, s.URL+"/", time.Second)); err != nil {
				t.Fatal(err)
			}
			if hits.Load() != 1 {
				t.Fatalf("expected one request, got %d", hits.Load())
			}
		})
	}
}

func TestInvalidInputsNeverReachService(t *testing.T) {
	var hits atomic.Int32
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(500) })
	c := newClient(t, s.URL, time.Second)
	ctx := t.Context()
	value := true
	for _, identity := range [][2]string{{"DEV", "checkout"}, {"dev", "../checkout"}} {
		if _, err := c.Create(ctx, identity[0], flags.CreateInput{Key: identity[1]}); err == nil {
			t.Error("invalid create accepted")
		}
		if _, err := c.Get(ctx, identity[0], identity[1]); err == nil {
			t.Error("invalid get accepted")
		}
		if _, err := c.Update(ctx, identity[0], identity[1], flags.UpdateInput{Enabled: &value}); err == nil {
			t.Error("invalid update accepted")
		}
		if err := c.Delete(ctx, identity[0], identity[1]); err == nil {
			t.Error("invalid delete accepted")
		}
	}
	if _, err := c.List(ctx, "DEV"); err == nil {
		t.Error("invalid list accepted")
	}
	if _, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{}); err == nil {
		t.Error("empty update accepted")
	}
	if hits.Load() != 0 {
		t.Errorf("invalid inputs sent %d requests", hits.Load())
	}
}

func requireAPIError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var apiError *client.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != status || apiError.Code != code {
		t.Fatalf("API error = %v, want %d/%s", err, status, code)
	}
}

func TestAPIErrorSanitization(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{400, "invalid_request"}, {403, "forbidden"}, {404, "not_found"}, {405, "method_not_allowed"}, {409, "already_exists"}, {413, "request_too_large"}, {415, "unsupported_media_type"}, {500, "internal_error"}, {502, marker}, {500, ""}} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.code), func(t *testing.T) {
			s := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if tc.code == "" {
					io.WriteString(w, "invalid JSON "+marker)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": tc.code, "message": marker}})
			})
			_, err := newClient(t, s.URL, time.Second).Get(t.Context(), "dev", "checkout")
			code := tc.code
			if code == marker {
				code = ""
			}
			requireAPIError(t, err, tc.status, code)
			if strings.Contains(err.Error(), marker) {
				t.Error("raw API error content leaked")
			}
		})
	}
}
