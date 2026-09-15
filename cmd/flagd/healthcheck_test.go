package main

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/testutil"
)

func TestHealthcheckUsesPublicHostAndVerifiedTLS(t *testing.T) {
	files := testutil.NewTLS(t)
	var hits atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Host != "flagd.test:9443" || r.URL.Path != "/healthz" || r.Method != "GET" {
			t.Error("probe did not preserve the public origin")
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("health probe sent credentials")
		}
		io.WriteString(w, `{"status":"ok"}`)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{files.Certificate}, MinVersion: tls.VersionTLS13}
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	args := []string{"healthcheck", "--server", "https://flagd.test:9443", "--connect", server.Listener.Addr().String(), "--ca-file", files.CAFile}
	// No DNS record or public-port listener is needed inside the container.
	if err := run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatal("probe did not reach the service")
	}
	for _, extra := range [][]string{{"--ca-file", ""}, {"--server", "https://wrong.test"}, {"--ca-file", testutil.NewTLS(t).CAFile}} {
		if err := run(t.Context(), append(append([]string{}, args...), extra...)); err == nil {
			t.Fatal("invalid TLS trust or hostname accepted")
		}
	}
	if hits.Load() != 1 {
		t.Fatal("TLS failure reached the HTTP handler")
	}
}

func TestHealthcheckRejectsInvalidConfiguration(t *testing.T) {
	for _, args := range [][]string{
		{"--server", "http://localhost"}, {"--server", "https://user:synthetic-secret@localhost"},
		{"--server", "https://localhost/path"}, {"--connect", "0.0.0.0:8443"},
		{"--connect", "192.0.2.1:8443"}, {"--connect", "127.0.0.1:0"},
		{"--connect", "localhost:8443"}, {"--ca-file", "synthetic-secret"}, {"unexpected"}, {"--unknown"},
	} {
		err := runHealthcheck(t.Context(), args)
		if err == nil {
			t.Fatal("invalid probe configuration accepted")
		}
		if strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("configuration error exposed input")
		}
	}
	if err := runHealthcheck(t.Context(), []string{"--help"}); err != nil {
		t.Fatal(err)
	}
}

func TestHealthcheckResponseFailuresAndCancellation(t *testing.T) {
	files := testutil.NewTLS(t)
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer destination.Close()
	t.Setenv("HTTPS_PROXY", destination.URL)
	t.Setenv("HTTP_PROXY", destination.URL)
	t.Setenv("NO_PROXY", "")
	for _, tc := range []struct {
		name, body string
		status     int
		delay      bool
	}{
		{"unhealthy", `{"status":"ok"}`, 503, false}, {"bad body", `{"status":"down"}`, 200, false},
		{"invalid JSON", "synthetic-secret", 200, false}, {"oversized", strings.Repeat("x", 1025), 200, false},
		{"redirect", "", 302, false}, {"cancellation", "", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.delay {
					<-r.Context().Done()
					return
				}
				if tc.status == 302 {
					w.Header().Set("Location", destination.URL)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{files.Certificate}, MinVersion: tls.VersionTLS13}
			server.StartTLS()
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			err := runHealthcheck(ctx, []string{"--server", server.URL, "--connect", server.Listener.Addr().String(), "--ca-file", files.CAFile})
			if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("expected safe health failure: %v", err)
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("probe followed a redirect or used a proxy")
	}
}
