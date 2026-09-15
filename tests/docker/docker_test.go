// Package docker_test verifies the shipped Compose application against a local engine.
package docker_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/ahmedr1zwan/flagctl/internal/security"
	"github.com/ahmedr1zwan/flagctl/internal/testutil"
)

func TestComposeLifecycle(t *testing.T) {
	if os.Getenv("FLAGCTL_DOCKER_TEST") != "1" {
		t.Skip("set FLAGCTL_DOCKER_TEST=1 to build and run the Docker integration test")
	}
	if runtime.GOOS == "windows" || os.Getuid() <= 0 || os.Getgid() <= 0 {
		t.Fatal("run this test as a non-root macOS/Linux user with Docker access")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal("Docker CLI and Compose are required")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	files := testutil.NewTLS(t)
	work := t.TempDir()
	// Reserve an available host port briefly, then use it for the published port
	// and public origin. No fixed development port is assumed.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	project := fmt.Sprintf("flagctl-test-%d", time.Now().UnixNano())
	env := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, "FLAGCTL_") && !strings.HasPrefix(name, "COMPOSE_") {
			env = append(env, entry)
		}
	}
	env = append(env, "FLAGCTL_UID="+strconv.Itoa(os.Getuid()), "FLAGCTL_GID="+strconv.Itoa(os.Getgid()), "FLAGCTL_PORT="+port,
		"FLAGCTL_TOKEN_FILE="+files.TokenFile, "FLAGCTL_CA_FILE="+files.CAFile, "FLAGCTL_TLS_CERT_FILE="+files.CertFile, "FLAGCTL_TLS_KEY_FILE="+files.KeyFile)
	command := func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, docker, args...)
		cmd.Dir = root
		cmd.Env = env
		cmd.Stdin = input
		return cmd.CombinedOutput()
	}
	mustRun := func(args ...string) []byte {
		t.Helper()
		output, err := command(t.Context(), nil, args...)
		if bytes.Contains(output, []byte(files.Token)) {
			t.Fatal("Docker output exposed the test token")
		}
		if err != nil {
			t.Fatalf("docker %s failed: %v\n%s", args[0], err, output)
		}
		return output
	}
	// A remote builder/daemon cannot bind-mount these local credential files.
	endpoint := strings.TrimSpace(string(mustRun("context", "inspect", "--format", "{{.Endpoints.docker.Host}}")))
	if override := os.Getenv("DOCKER_HOST"); override != "" {
		endpoint = override
	}
	if !strings.HasPrefix(endpoint, "unix://") {
		t.Fatal("Docker test requires a local engine with a Unix socket")
	}
	mustRun("compose", "version")
	base := []string{"compose", "--project-name", project, "--env-file", os.DevNull, "-f", filepath.Join(root, "compose.yaml")}
	compose := func(args ...string) []byte { return mustRun(append(append([]string{}, base...), args...)...) }
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		args := append(append([]string{}, base...), "down", "--volumes", "--rmi", "local", "--remove-orphans")
		output, err := command(ctx, nil, args...)
		if err != nil {
			t.Errorf("isolated Docker cleanup failed: %v\n%s", err, output)
		}
	})

	t.Log("Checking that Docker excludes credential files from the build context")
	canary, err := os.CreateTemp(filepath.Join(root, "internal/api"), "docker-context-*.token")
	if err != nil {
		t.Fatal(err)
	}
	canaryPath := canary.Name()
	t.Cleanup(func() { os.Remove(canaryPath) })
	if _, err := canary.WriteString("synthetic-docker-context-secret"); err != nil {
		canary.Close()
		t.Fatal(err)
	}
	canary.Close()
	exported := filepath.Join(work, "context")
	output, err := command(t.Context(), strings.NewReader("FROM scratch\nCOPY . /\n"), "build", "--file", "-", "--output", "type=local,dest="+exported, ".")
	if err != nil {
		t.Fatalf("context export failed: %v\n%s", err, output)
	}
	count := 0
	err = filepath.WalkDir(exported, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(exported, path)
		if err != nil {
			return err
		}
		allowed := relative == "go.mod" || relative == "go.sum"
		for _, directory := range []string{"cmd/flagd", "internal/api", "internal/flags", "internal/security", "internal/store"} {
			allowed = allowed || (filepath.Dir(relative) == directory && strings.HasSuffix(relative, ".go") && !strings.HasSuffix(relative, "_test.go"))
		}
		if !allowed {
			return fmt.Errorf("unexpected file in Docker build context: %s", relative)
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 10 {
		t.Fatal("build context omitted application source")
	}
	os.Remove(canaryPath)

	t.Log("Building and starting the TLS service with an isolated named volume")
	compose("up", "--build", "--wait", "--wait-timeout", "90")
	container := strings.TrimSpace(string(compose("ps", "--quiet", "flagd")))
	if container == "" {
		t.Fatal("Compose did not start flagd")
	}
	imageID := strings.TrimSpace(string(mustRun("inspect", "--format", "{{.Image}}", container)))
	platform := strings.TrimSpace(string(mustRun("image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", imageID)))
	if requested := os.Getenv("DOCKER_DEFAULT_PLATFORM"); requested != "" && requested != platform {
		t.Fatalf("container platform = %s, requested %s", platform, requested)
	}
	t.Logf("Running container image for %s", platform)
	var inspection []struct {
		Config struct {
			User string
			Env  []string
		}
		HostConfig struct {
			ReadonlyRootfs bool
			CapDrop        []string
			SecurityOpt    []string
			PortBindings   map[string][]struct{ HostIp, HostPort string }
		}
		Mounts []struct {
			Destination string
			RW          bool
			Type        string
		}
		State struct{ Health struct{ Status string } }
	}
	if err := json.Unmarshal(mustRun("inspect", container), &inspection); err != nil {
		t.Fatal(err)
	}
	if len(inspection) != 1 {
		t.Fatal("unexpected container inspection")
	}
	inspected := inspection[0]
	if inspected.Config.User != fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()) || !inspected.HostConfig.ReadonlyRootfs {
		t.Fatal("container user/root filesystem policy differs from Compose")
	}
	if !strings.Contains(strings.Join(inspected.HostConfig.CapDrop, ","), "ALL") || !strings.Contains(strings.Join(inspected.HostConfig.SecurityOpt, ","), "no-new-privileges") {
		t.Fatal("container privilege restrictions missing")
	}
	bindings := inspected.HostConfig.PortBindings["8443/tcp"]
	if len(bindings) != 1 || bindings[0].HostIp != "127.0.0.1" || bindings[0].HostPort != port {
		t.Fatal("service port is not restricted to host loopback")
	}
	for _, mount := range inspected.Mounts {
		if strings.HasPrefix(mount.Destination, "/run/secrets/") && mount.RW {
			t.Fatal("credential mount is writable")
		}
	}
	if inspected.State.Health.Status != "healthy" {
		t.Fatal("TLS container health check failed")
	}

	endpoint = "https://localhost:" + port
	service, err := client.NewWithConfig(client.Config{Server: endpoint, Timeout: 5 * time.Second, TokenFile: files.TokenFile, CAFile: files.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	created, err := service.Create(t.Context(), "dev", flags.CreateInput{Key: "persisted", Description: "container lifecycle"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(t.Context(), "prod", flags.CreateInput{Key: "persisted", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := security.ClientTLS(files.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	t.Cleanup(transport.CloseIdleConnections)
	httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := httpClient.Get(endpoint + "/v1/environments/dev/flags")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("anonymous flag access was accepted")
	}

	// Exercise the actual CLI executable against the container too.
	cli := filepath.Join(work, "flagctl")
	build := exec.CommandContext(t.Context(), filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-o", cli, "./cmd/flagctl")
	build.Dir = root
	build.Env = append(env, "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("CLI build failed: %v\n%s", err, output)
	}
	cliRun := exec.CommandContext(t.Context(), cli, "--server", endpoint, "flags", "toggle", "persisted", "--env", "dev", "--enabled=true", "--output", "json")
	cliRun.Env = env
	output, err = cliRun.CombinedOutput()
	if err != nil {
		t.Fatalf("container CLI request failed: %v\n%s", err, output)
	}
	var toggled flags.Flag
	if err := json.Unmarshal(output, &toggled); err != nil || !toggled.Enabled || toggled.CreatedAt != created.CreatedAt {
		t.Fatal("CLI update response was incorrect")
	}

	t.Log("Verifying persistence through restart and complete container replacement")
	service.Close()
	transport.CloseIdleConnections()
	compose("restart", "flagd")
	compose("up", "--wait", "--wait-timeout", "60")
	if got, err := service.Get(t.Context(), "dev", "persisted"); err != nil || got != toggled {
		t.Fatal("restart lost the flag", err)
	}
	service.Close()
	compose("down")
	compose("up", "--wait", "--wait-timeout", "60")
	if got, err := service.Get(t.Context(), "dev", "persisted"); err != nil || got != toggled {
		t.Fatal("container replacement lost the flag", err)
	}
	if got, err := service.Get(t.Context(), "prod", "persisted"); err != nil || !got.Enabled {
		t.Fatal("environment isolation/persistence failed", err)
	}
	container = strings.TrimSpace(string(compose("ps", "--quiet", "flagd")))
	archive := tar.NewReader(bytes.NewReader(mustRun("cp", container+":/var/lib/flagctl/data", "-")))
	sawDatabase := false
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Mode&0077 != 0 || header.Uid != os.Getuid() {
			t.Fatalf("database directory/file lacks private ownership: %s", header.Name)
		}
		sawDatabase = sawDatabase || strings.HasSuffix(header.Name, "/flags.db")
	}
	if !sawDatabase {
		t.Fatal("persistent volume did not contain flags.db")
	}
	for _, environment := range []string{"dev", "prod"} {
		if err := service.Delete(t.Context(), environment, "persisted"); err != nil {
			t.Fatal(err)
		}
		if remaining, err := service.List(t.Context(), environment); err != nil || len(remaining) != 0 {
			t.Fatal("delete did not persist", err)
		}
	}
	service.Close()
	compose("stop")
	if exit := strings.TrimSpace(string(mustRun("inspect", "--format", "{{.State.ExitCode}}", container))); exit != "0" {
		t.Fatal("service did not shut down cleanly")
	}
	logs := compose("logs", "--no-color")
	if bytes.Contains(logs, []byte(files.Token)) {
		t.Fatal("container logs exposed credentials")
	}
	t.Log("Verified TLS/authentication, CLI, isolation, private files, volume persistence, and clean shutdown")
}
