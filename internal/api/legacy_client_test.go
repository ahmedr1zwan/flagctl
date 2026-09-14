package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestV1HistoricalClient(t *testing.T) {
	directory := filepath.Join("testdata", "v1-client")
	manifest, err := os.ReadFile(filepath.Join(directory, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Commit string            `json:"commit"`
		SHA256 map[string]string `json:"sha256"`
	}
	if err := json.Unmarshal(manifest, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Commit != "afd343df4121e52e8096e05414b71e7373e7e69f" || len(snapshot.SHA256) != 2 {
		t.Fatal("historical client baseline changed; preserve the original v1 snapshot")
	}
	for name, want := range snapshot.SHA256 {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if got := hex.EncodeToString(digest[:]); got != want {
			t.Fatalf("historical source changed: %s", name)
		}
	}
	// Compile the archived sources in their own module. Never resolve the current
	// client or flags package, fetch Git history, or download dependencies here.
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "v1-client")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	environment := legacyEnvironment()
	build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-mod=readonly", "-trimpath", "-o", binary, ".")
	build.Dir = directory
	build.Env = append(environment, "GOENV=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build archived client: %v\n%s", err, output)
	}

	a := newAPI(t, nil)
	command := exec.CommandContext(ctx, binary, a.server.URL)
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "v1 client lifecycle passed" {
		t.Fatalf("archived client failed: %v\n%s", err, output)
	}
	for _, environment := range []string{"dev", "prod"} {
		if items := readList(t, a.request(t, "GET", "/v1/environments/"+environment+"/flags", "", nil)); len(items) != 0 {
			t.Fatalf("archived client left flags in %s", environment)
		}
	}
}

func legacyEnvironment() []string {
	var environment []string
	// Preserve runtime/cache paths only, excluding developer credentials and
	// settings that could redirect the compiler or the archived client's requests.
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "SYSTEMROOT", "GOCACHE", "GOMODCACHE"} {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}
