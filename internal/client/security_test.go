package client_test

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/ahmedr1zwan/flagctl/internal/testutil"
)

func TestAuthenticatedClientLifecycle(t *testing.T) {
	server, files := testutil.NewSecureAPI(t)
	c, err := client.NewWithConfig(client.Config{Server: server.URL, Timeout: time.Second, TokenFile: files.TokenFile, CAFile: files.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	created, err := c.Create(t.Context(), "dev", flags.CreateInput{Key: "secure"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Get(t.Context(), "dev", "secure"); err != nil || got != created {
		t.Fatal("get differs from create", err)
	}
	enabled := true
	if got, err := c.Update(t.Context(), "dev", "secure", flags.UpdateInput{Enabled: &enabled}); err != nil || !got.Enabled {
		t.Fatal("update failed", err)
	}
	if got, err := c.List(t.Context(), "dev"); err != nil || len(got) != 1 {
		t.Fatal("list failed", err)
	}
	if err := c.Delete(t.Context(), "dev", "secure"); err != nil {
		t.Fatal(err)
	}
	if got, err := c.List(t.Context(), "dev"); err != nil || len(got) != 0 {
		t.Fatal("delete not persisted", err)
	}
	wrong := filepath.Join(t.TempDir(), "wrong.token")
	if err := os.WriteFile(wrong, []byte(strings.Repeat("b", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	rejected, err := client.NewWithConfig(client.Config{Server: server.URL, Timeout: time.Second, TokenFile: wrong, CAFile: files.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	defer rejected.Close()
	_, err = rejected.List(t.Context(), "dev")
	var apiError *client.APIError
	if !errors.As(err, &apiError) || apiError.Code != "unauthorized" || apiError.StatusCode != 401 {
		t.Fatalf("unexpected authentication error: %v", err)
	}
}

func TestSecureClientConfiguration(t *testing.T) {
	files := testutil.NewTLS(t)
	for _, config := range []client.Config{
		{Server: "http://127.0.0.1", Timeout: time.Second, TokenFile: files.TokenFile},
		{Server: "http://localhost", Timeout: time.Second, CAFile: files.CAFile},
		{Server: "http://example.com", Timeout: time.Second, TokenFile: files.TokenFile},
		{Server: "https://example.com", Timeout: time.Second},
		{Server: "https://localhost", Timeout: time.Second, TokenFile: "synthetic-sensitive-value"},
		{Server: "https://localhost", Timeout: time.Second, CAFile: "synthetic-sensitive-value"},
	} {
		c, err := client.NewWithConfig(config)
		if err == nil {
			c.Close()
			t.Fatal("unsafe configuration accepted")
		}
		if strings.Contains(err.Error(), "synthetic-sensitive-value") {
			t.Fatal("error exposed credential path")
		}
	}
	// Configuration is validated locally without DNS or API requests.
	c, err := client.NewWithConfig(client.Config{Server: "https://flagd.example.invalid", Timeout: time.Second, TokenFile: files.TokenFile, CAFile: files.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestTLSFailuresDoNotSendCredentials(t *testing.T) {
	cases := []string{"untrusted", "wrong CA", "wrong hostname", "expired", "TLS 1.2"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			files := testutil.NewTLS(t, func(cert *x509.Certificate) {
				if name == "wrong hostname" {
					cert.DNSNames = []string{"elsewhere.test"}
					cert.IPAddresses = nil
				}
				if name == "expired" {
					cert.NotBefore = cert.NotBefore.Add(-3 * time.Hour)
					cert.NotAfter = cert.NotAfter.Add(-3 * time.Hour)
				}
			})
			var hits atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{files.Certificate}, MinVersion: tls.VersionTLS13}
			if name == "TLS 1.2" {
				server.TLS.MinVersion = tls.VersionTLS12
				server.TLS.MaxVersion = tls.VersionTLS12
			}
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.StartTLS()
			defer server.Close()
			ca := files.CAFile
			if name == "untrusted" {
				ca = ""
			}
			if name == "wrong CA" {
				ca = testutil.NewTLS(t).CAFile
			}
			c, err := client.NewWithConfig(client.Config{Server: server.URL, Timeout: time.Second, TokenFile: files.TokenFile, CAFile: ca})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			_, err = c.List(t.Context(), "dev")
			if err == nil || hits.Load() != 0 {
				t.Fatal("TLS failure allowed an HTTP request")
			}
			if strings.Contains(err.Error(), files.Token) || strings.Contains(err.Error(), files.TokenFile) {
				t.Fatal("TLS error exposed credentials")
			}
		})
	}
}

func TestAuthenticatedRequestsDoNotFollowRedirectsOrProxies(t *testing.T) {
	var unexpected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { unexpected.Add(1) }))
	defer destination.Close()
	t.Setenv("HTTPS_PROXY", destination.URL)
	t.Setenv("HTTP_PROXY", destination.URL)
	t.Setenv("ALL_PROXY", destination.URL)
	t.Setenv("NO_PROXY", "")
	files := testutil.NewTLS(t)
	var authenticated atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.Header.Get("Authorization") != "Bearer "+files.Token {
			t.Error("missing TLS authorization")
		} else {
			authenticated.Add(1)
		}
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{files.Certificate}, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	defer server.Close()
	c, err := client.NewWithConfig(client.Config{Server: server.URL, Timeout: time.Second, TokenFile: files.TokenFile, CAFile: files.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.List(t.Context(), "dev"); err == nil {
		t.Fatal("redirect accepted")
	}
	if unexpected.Load() != 0 || authenticated.Load() != 1 {
		t.Fatal("request reached proxy or redirect destination")
	}
}
