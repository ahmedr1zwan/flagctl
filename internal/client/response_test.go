package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
)

var operations = []struct {
	name string
	run  func(context.Context, *client.Client) error
}{
	{"create", func(ctx context.Context, c *client.Client) error {
		_, err := c.Create(ctx, "dev", flags.CreateInput{Key: "checkout", Description: "keep", Enabled: true})
		return err
	}},
	{"list", func(ctx context.Context, c *client.Client) error { _, err := c.List(ctx, "dev"); return err }},
	{"get", func(ctx context.Context, c *client.Client) error { _, err := c.Get(ctx, "dev", "checkout"); return err }},
	{"update", func(ctx context.Context, c *client.Client) error {
		enabled := true
		_, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &enabled})
		return err
	}},
	{"delete", func(ctx context.Context, c *client.Client) error { return c.Delete(ctx, "dev", "checkout") }},
}

func TestResponseValidation(t *testing.T) {
	for _, operation := range operations[:4] {
		t.Run(operation.name, func(t *testing.T) {
			for _, mode := range []string{"valid", "additive", "wrong_environment", "missing_enabled", "null_description", "invalid_key", "invalid_description", "invalid_timestamp", "missing_timestamp", "malformed", "trailing", "wrong_type", "wrong_content_type", "oversized", "wrong_status"} {
				t.Run(mode, func(t *testing.T) {
					s := serve(t, func(w http.ResponseWriter, r *http.Request) {
						io.Copy(io.Discard, r.Body)
						flag := fixture()
						raw, _ := json.Marshal(flag)
						var object map[string]any
						json.Unmarshal(raw, &object)
						switch mode {
						case "additive":
							object["future_metadata"] = map[string]any{"revision": 2}
						case "wrong_environment":
							object["environment"] = "prod"
						case "missing_enabled":
							delete(object, "enabled")
						case "null_description":
							object["description"] = nil
						case "invalid_key":
							object["key"] = "../checkout"
						case "invalid_description":
							object["description"] = strings.Repeat("x", 1025)
						case "invalid_timestamp":
							object["created_at"] = "bad date " + marker
						case "missing_timestamp":
							delete(object, "updated_at")
						case "wrong_type":
							object["enabled"] = "true"
						}
						var payload any = object
						if operation.name == "list" {
							payload = map[string]any{"flags": []any{object}, "future_field": true}
						}
						raw, _ = json.Marshal(payload)
						switch mode {
						case "malformed":
							raw = []byte(marker)
						case "trailing":
							raw = append(raw, []byte(" {}")...)
						case "oversized":
							raw = []byte(strings.Repeat(" ", (8<<20)+1))
						}
						contentType := "application/json"
						if mode == "wrong_content_type" {
							contentType = "text/html"
						}
						w.Header().Set("Content-Type", contentType)
						status := 200
						if operation.name == "create" {
							status = 201
						}
						if mode == "wrong_status" {
							status = 202
						}
						w.WriteHeader(status)
						w.Write(raw)
					})
					err := operation.run(t.Context(), newClient(t, s.URL, 5*time.Second))
					valid := mode == "valid" || mode == "additive"
					if (err == nil) != valid {
						t.Fatalf("response validation = %v, want valid=%t", err, valid)
					}
					if err != nil && strings.Contains(err.Error(), marker) {
						t.Error("raw response leaked into error")
					}
				})
			}
		})
	}
	for _, body := range []string{`{}`, `{"flags":null}`, `{"flags":[null]}`} {
		s := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		})
		if _, err := newClient(t, s.URL, time.Second).List(t.Context(), "dev"); err == nil {
			t.Error("invalid list accepted")
		}
	}
	for _, operation := range []int{0, 2, 3} {
		s := serve(t, func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			if r.Method == "POST" {
				w.WriteHeader(201)
			}
			flag := fixture()
			flag.Key = "different"
			json.NewEncoder(w).Encode(flag)
		})
		if err := operations[operation].run(t.Context(), newClient(t, s.URL, time.Second)); err == nil {
			t.Error("accepted response for another key")
		}
	}
	t.Run("mismatched update", func(t *testing.T) {
		s := serve(t, func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(fixture())
		})
		c := newClient(t, s.URL, time.Second)
		disabled, empty := false, ""
		for _, input := range []flags.UpdateInput{{Enabled: &disabled}, {Description: &empty}} {
			if _, err := c.Update(t.Context(), "dev", "checkout", input); err == nil {
				t.Error("accepted response that ignored update")
			}
		}
	})
	// The body limit is inclusive; valid JSON plus whitespace at exactly 8 MiB works.
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		body := `{"flags":[]}`
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body+strings.Repeat(" ", (8<<20)-len(body)))
	})
	if _, err := newClient(t, s.URL, 5*time.Second).List(t.Context(), "dev"); err != nil {
		t.Fatalf("exact response limit rejected: %v", err)
	}
}

func TestRedirectsAndFailuresAreNotRetried(t *testing.T) {
	var targetHits atomic.Int32
	target := serve(t, func(w http.ResponseWriter, r *http.Request) { targetHits.Add(1); w.WriteHeader(500) })
	for _, status := range []int{301, 302, 307, 308, 500} {
		for _, operation := range operations {
			t.Run(fmt.Sprintf("%d/%s", status, operation.name), func(t *testing.T) {
				var hits atomic.Int32
				s := serve(t, func(w http.ResponseWriter, r *http.Request) {
					io.Copy(io.Discard, r.Body)
					hits.Add(1)
					w.Header().Set("Location", target.URL+"/?token="+marker)
					w.WriteHeader(status)
					io.WriteString(w, marker)
				})
				err := operation.run(t.Context(), newClient(t, s.URL, time.Second))
				requireAPIError(t, err, status, "")
				if strings.Contains(err.Error(), marker) || hits.Load() != 1 {
					t.Fatalf("unsafe error or retry: %v, hits=%d", err, hits.Load())
				}
			})
		}
	}
	if targetHits.Load() != 0 {
		t.Error("redirect target received requests")
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	for _, operation := range operations {
		for _, mode := range []string{"timeout before headers", "timeout reading body", "cancel"} {
			t.Run(operation.name+"/"+mode, func(t *testing.T) {
				// DELETE's 204 has no body, so only its header wait can time out.
				if operation.name == "delete" && mode == "timeout reading body" {
					return
				}
				received, release := make(chan struct{}, 1), make(chan struct{})
				s := serve(t, func(w http.ResponseWriter, r *http.Request) {
					io.Copy(io.Discard, r.Body)
					received <- struct{}{}
					if mode == "timeout reading body" {
						w.Header().Set("Content-Type", "application/json")
						status := 200
						if operation.name == "create" {
							status = 201
						}
						w.WriteHeader(status)
						w.(http.Flusher).Flush()
					}
					select {
					case <-r.Context().Done():
					case <-release:
					}
				})
				t.Cleanup(func() { close(release) })
				timeout := 100 * time.Millisecond
				if mode == "cancel" {
					timeout = 5 * time.Second
				}
				c := newClient(t, s.URL, timeout)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				result := make(chan error, 1)
				go func() { result <- operation.run(ctx, c) }()
				if mode == "cancel" {
					select {
					case <-received:
						cancel()
					case <-time.After(5 * time.Second):
						t.Fatal("request never arrived")
					}
				}
				want := "timed out"
				if mode == "cancel" {
					want = "canceled"
				}
				select {
				case err := <-result:
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("got %v, want %s", err, want)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("request did not stop")
				}
			})
		}
	}
}

func TestTransportProtections(t *testing.T) {
	var proxyHits atomic.Int32
	proxy := serve(t, func(w http.ResponseWriter, r *http.Request) { proxyHits.Add(1); w.WriteHeader(502) })
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(name, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"flags":[]}`)
	})
	if _, err := newClient(t, s.URL, time.Second).List(t.Context(), "dev"); err != nil {
		t.Fatal(err)
	}
	if proxyHits.Load() != 0 {
		t.Error("local flag data was sent to a proxy")
	}
	secure := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS request reached handler") }))
	secure.Config.ErrorLog = log.New(io.Discard, "", 0)
	secure.StartTLS()
	t.Cleanup(secure.Close)
	if _, err := newClient(t, secure.URL, time.Second).List(t.Context(), "dev"); err == nil || !strings.Contains(err.Error(), "TLS trust") {
		t.Fatalf("untrusted TLS = %v", err)
	}
	for _, mode := range []string{"oversized headers", "truncated body"} {
		s := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if mode == "oversized headers" {
				w.Header().Set("X-Large", strings.Repeat("x", 17<<10)+marker)
			} else {
				w.Header().Set("Content-Length", "1000")
			}
			io.WriteString(w, `{"flags":[]}`)
		})
		_, err := newClient(t, s.URL, time.Second).List(t.Context(), "dev")
		if err == nil || strings.Contains(err.Error(), marker) {
			t.Fatalf("transport error = %v", err)
		}
	}
}
