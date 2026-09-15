package cli_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ahmedr1zwan/flagctl/internal/testutil"
)

func TestMain(m *testing.M) {
	// A developer's credentials must never be loaded by an isolated test fixture.
	for _, name := range []string{"FLAGCTL_SERVER", "FLAGCTL_TOKEN_FILE", "FLAGCTL_CA_FILE"} {
		_ = os.Unsetenv(name)
	}
	os.Exit(m.Run())
}

func TestCLISecureConfigurationAndLifecycle(t *testing.T) {
	server, files := testutil.NewSecureAPI(t)
	t.Setenv("FLAGCTL_SERVER", server.URL)
	t.Setenv("FLAGCTL_TOKEN_FILE", files.TokenFile)
	t.Setenv("FLAGCTL_CA_FILE", files.CAFile)
	created := decodeFlag(t, execute(t.Context(), "flags", "create", "secure", "--env", "dev", "-o", "json"))
	if created.Key != "secure" || created.Enabled {
		t.Fatal("unexpected create response")
	}
	if !strings.Contains(success(t, execute(t.Context(), "flags", "list", "--env", "dev")), "secure") {
		t.Fatal("table did not list flag")
	}
	if got := decodeFlag(t, execute(t.Context(), "flags", "toggle", "secure", "--env", "dev", "--enabled=true", "-o", "json")); !got.Enabled {
		t.Fatal("toggle failed")
	}
	success(t, execute(t.Context(), "flags", "delete", "secure", "--env", "dev"))
	if got := success(t, execute(t.Context(), "flags", "list", "--env", "dev", "-o", "json")); !strings.Contains(got, `"flags": []`) {
		t.Fatal("delete did not empty list")
	}
	if message := failure(t, execute(t.Context(), "flags", "list", "--env", "dev", "--token-file=")); !strings.Contains(message, "401") {
		t.Fatal("empty flag did not override environment")
	}
	failure(t, execute(t.Context(), "flags", "list", "--env", "dev", "--ca-file="))
	t.Setenv("FLAGCTL_TOKEN_FILE", "synthetic-sensitive-value")
	t.Setenv("FLAGCTL_CA_FILE", "synthetic-sensitive-value")
	success(t, execute(t.Context(), "flags", "list", "--env", "dev", "--token-file", files.TokenFile, "--ca-file", files.CAFile))
	message := failure(t, execute(t.Context(), "flags", "list", "--env", "dev"))
	help := success(t, execute(t.Context(), "--help"))
	for _, text := range []string{message, help} {
		if strings.Contains(text, "synthetic-sensitive-value") || strings.Contains(text, files.Token) {
			t.Fatal("CLI exposed credential data")
		}
	}
}
