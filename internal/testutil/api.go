package testutil

import (
	"io"
	"log"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ahmedr1zwan/flagctl/internal/api"
	"github.com/ahmedr1zwan/flagctl/internal/security"
	"github.com/ahmedr1zwan/flagctl/internal/store"
)

// NewSecureAPI creates a real TLS API and isolated SQLite store, with cleanup.
func NewSecureAPI(t testing.TB) (*httptest.Server, TLSFiles) {
	t.Helper()
	files := NewTLS(t)
	backend, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewUnstartedServer(nil)
	t.Cleanup(server.Close)
	token, err := security.ReadTokenFile(files.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler, err = api.NewSecureHandler(backend, "https://"+server.Listener.Addr().String(), token)
	if err != nil {
		t.Fatal(err)
	}
	server.TLS, err = security.ServerTLS(files.CertFile, files.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	return server, files
}
