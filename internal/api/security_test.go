package api_test

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/api"
	"github.com/ahmedr1zwan/flagctl/internal/security"
	"github.com/ahmedr1zwan/flagctl/internal/testutil"
)

func TestSecureAPIAuthorization(t *testing.T) {
	server, files := testutil.NewSecureAPI(t)
	config, err := security.ClientTLS(files.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: config}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	cases := []struct {
		name, method, path string
		headers            http.Header
		host               string
		want               int
	}{
		{name: "anonymous health", method: "GET", path: "/healthz", want: 200},
		{name: "anonymous head health", method: "HEAD", path: "/healthz", want: 200},
		{name: "health mutations require auth", method: "POST", path: "/healthz", want: 401},
		{name: "missing", method: "POST", path: collection, want: 401},
		{name: "wrong", method: "POST", path: collection, headers: http.Header{"Authorization": {"Bearer " + strings.Repeat("b", 64)}}, want: 401},
		{name: "duplicate", method: "POST", path: collection, headers: http.Header{"Authorization": {"Bearer " + files.Token, "Bearer " + files.Token}}, want: 401},
		{name: "query is not authentication", method: "POST", path: collection + "?access_token=" + files.Token, want: 401},
		{name: "cookie is not authentication", method: "POST", path: collection, headers: http.Header{"Cookie": {"token=" + files.Token}}, want: 401},
		{name: "foreign origin", method: "POST", path: collection, headers: http.Header{"Authorization": {"Bearer " + files.Token}, "Origin": {"https://evil.test"}}, want: 403},
		{name: "foreign host", method: "POST", path: collection, headers: http.Header{"Authorization": {"Bearer " + files.Token}}, host: "evil.test", want: 403},
		{name: "authenticated create", method: "POST", path: collection, headers: http.Header{"Authorization": {"Bearer " + files.Token}}, want: 201},
		{name: "anonymous read", method: "GET", path: collection, want: 401},
		{name: "anonymous patch", method: "PATCH", path: collection + "/secure", want: 401},
		{name: "anonymous delete", method: "DELETE", path: collection + "/secure", want: 401},
		{name: "authenticated read", method: "GET", path: collection + "/secure", headers: http.Header{"Authorization": {"Bearer " + files.Token}}, want: 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), tc.method, server.URL+tc.path, strings.NewReader(`{"key":"secure"}`))
			if err != nil {
				t.Fatal(err)
			}
			for name, values := range tc.headers {
				req.Header[name] = values
			}
			req.Header.Set("Content-Type", "application/json")
			if tc.host != "" {
				req.Host = tc.host
			}
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != tc.want {
				t.Fatalf("status=%d, want %d", response.StatusCode, tc.want)
			}
			if strings.Contains(string(body), files.Token) {
				t.Fatal("response exposed token")
			}
			if tc.want == 401 && response.Header.Get("WWW-Authenticate") != `Bearer realm="flagctl"` {
				t.Error("missing bearer challenge")
			}
			if response.Header.Get("Cache-Control") != "no-store" {
				t.Error("missing cache protection")
			}
		})
	}
	// The first successful create is 201, proving rejected writes did not create it;
	// the final read proves the rejected delete did not remove it.
}

func TestSecureHandlerRequiresActualTLSAndConfiguredHost(t *testing.T) {
	files := testutil.NewTLS(t)
	token, err := security.ReadTokenFile(files.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"http://localhost", "https://user:secret@localhost", "https://localhost/path"} {
		if _, err := api.NewSecureHandler(nil, origin, token); err == nil {
			t.Error("invalid secure origin accepted")
		}
	}
	if _, err := api.NewSecureHandler(nil, "https://localhost", security.Token{}); err == nil {
		t.Fatal("empty token accepted")
	}
	for _, origin := range []string{"https://localhost", "https://localhost:443", "https://[::1]", "https://[::1]:443"} {
		handler, err := api.NewSecureHandler(nil, origin, token)
		if err != nil {
			t.Fatal(err)
		}
		for _, hasTLS := range []bool{false, true} {
			request := httptest.NewRequest("GET", origin+"/healthz", nil)
			request.TLS = nil
			request.Header.Set("X-Forwarded-Proto", "https")
			if hasTLS {
				request.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			want := 400
			if hasTLS {
				want = 200
			}
			if response.Code != want {
				t.Errorf("TLS=%v status=%d", hasTLS, response.Code)
			}
		}
	}
}
