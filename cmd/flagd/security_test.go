package main

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/ahmedr1zwan/flagctl/internal/security"
	"github.com/ahmedr1zwan/flagctl/internal/testutil"
)

func TestSecureStartupRejectsUnsafeConfiguration(t *testing.T) {
	files := testutil.NewTLS(t)
	credentials := []string{"--token-file", files.TokenFile, "--tls-cert-file", files.CertFile, "--tls-key-file", files.KeyFile}
	for _, args := range [][]string{
		{"--allow-network"}, {"--public-origin", "https://localhost"},
		{"--token-file", files.TokenFile}, {"--tls-cert-file", files.CertFile, "--tls-key-file", files.KeyFile},
		append(append([]string{}, credentials...), "--allow-network"),
		append(append([]string{}, credentials...), "--public-origin", "http://localhost"),
		append(append([]string{}, credentials...), "--public-origin", "https://wrong.test"),
		append(append([]string{}, credentials...), "--public-origin", "https://localhost/path"),
		append(append([]string{}, credentials...), "--token-file", "synthetic-sensitive-value"),
		append(append([]string{}, credentials...), "--tls-key-file", files.TokenFile),
	} {
		directory := filepath.Join(t.TempDir(), "data")
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		err := run(ctx, append([]string{"--listen", "127.0.0.1:0", "--data-dir", directory}, args...))
		cancel()
		if err == nil {
			t.Error("unsafe startup succeeded")
		}
		if err != nil && (strings.Contains(err.Error(), files.Token) || strings.Contains(err.Error(), "synthetic-sensitive-value")) {
			t.Error("startup error exposed credentials")
		}
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Error("invalid security configuration created storage")
		}
	}
	for _, addr := range []string{"0.0.0.0:0", "[::]:0", "192.0.2.1:8080"} {
		if _, err := parseListenAddress(addr, true); err != nil {
			t.Error(err)
		}
	}
	for _, addr := range []string{"localhost:0", "[::1%lo0]:0", "224.0.0.1:0", "[ff02::1]:0"} {
		if _, err := parseListenAddress(addr, true); err == nil {
			t.Error("unsafe address accepted")
		}
	}
}

func TestSecureServiceLifecycleAndPersistence(t *testing.T) {
	files := testutil.NewTLS(t)
	args := []string{"--token-file", files.TokenFile, "--tls-cert-file", files.CertFile, "--tls-key-file", files.KeyFile}
	directory := filepath.Join(t.TempDir(), "data")
	endpoint, stop := startService(t, directory, args...)
	c, err := client.NewWithConfig(client.Config{Server: endpoint, Timeout: time.Second, TokenFile: files.TokenFile, CAFile: files.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	created, err := c.Create(t.Context(), "dev", flags.CreateInput{Key: "persisted"})
	if err != nil {
		t.Fatal(err)
	}
	config, err := security.ClientTLS(files.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: config}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := httpClient.Get(endpoint + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || response.TLS == nil || response.TLS.Version != tls.VersionTLS13 {
		t.Fatal("health did not use TLS 1.3")
	}
	response, err = httpClient.Get(endpoint + "/v1/environments/dev/flags")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("anonymous request accepted")
	}
	response, err = httpClient.Get(strings.Replace(endpoint, "https://", "http://", 1) + "/healthz")
	if err == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode == 200 {
			t.Fatal("plaintext health accepted")
		}
	}
	// Finish the test clients before shutdown, including speculative TLS dials.
	transport.CloseIdleConnections()
	c.Close()
	stop()
	endpoint, stop = startService(t, directory, args...)
	defer stop()
	reopened, err := client.NewWithConfig(client.Config{Server: endpoint, Timeout: time.Second, TokenFile: files.TokenFile, CAFile: files.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.Get(t.Context(), "dev", "persisted"); err != nil || got != created {
		t.Fatal("secure restart lost flags", err)
	}
}

func TestNetworkListenerUsesPublicOrigin(t *testing.T) {
	files := testutil.NewTLS(t)
	endpoint, stop := startService(t, filepath.Join(t.TempDir(), "data"), "--listen", "0.0.0.0:0", "--allow-network", "--public-origin", "https://flagd.test", "--token-file", files.TokenFile, "--tls-cert-file", files.CertFile, "--tls-key-file", files.KeyFile)
	defer stop()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(endpoint, "https://"))
	if err != nil {
		t.Fatal(err)
	}
	config, err := security.ClientTLS(files.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: config, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
	}}
	defer transport.CloseIdleConnections()
	c := &http.Client{Transport: transport, Timeout: time.Second}
	request, err := http.NewRequestWithContext(t.Context(), "GET", "https://flagd.test/v1/environments/dev/flags", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+files.Token)
	for _, host := range []string{"flagd.test", "flagd.test:443", "localhost"} {
		request.Host = host
		response, err := c.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := 200
		if host == "localhost" {
			want = 403
		}
		if response.StatusCode != want {
			t.Fatalf("Host %s = %d", host, response.StatusCode)
		}
		if want == 200 && !strings.Contains(string(body), `"flags":[]`) {
			t.Fatal("network API returned invalid response")
		}
	}
}
