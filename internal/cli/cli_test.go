package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/ahmedr1zwan/flagctl/internal/cli"
	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/ahmedr1zwan/flagctl/internal/store"
)

const marker = "synthetic-sensitive-value"

type result struct {
	stdout, stderr string
	err            error
}

func execute(ctx context.Context, args ...string) result {
	var stdout, stderr bytes.Buffer
	command := cli.NewCommand(&stdout, &stderr)
	command.SetArgs(args)
	err := command.ExecuteContext(ctx)
	return result{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func success(t *testing.T, result result) string {
	t.Helper()
	if result.err != nil || result.stderr != "" {
		t.Fatalf("command failed: %v, stderr=%q", result.err, result.stderr)
	}
	return result.stdout
}

func failure(t *testing.T, result result) string {
	t.Helper()
	if result.err == nil || result.stdout != "" || result.stderr != "" {
		t.Fatalf("expected error without success output/double logging: %+v", result)
	}
	return result.err.Error()
}

func decodeFlag(t *testing.T, result result) flags.Flag {
	t.Helper()
	var flag flags.Flag
	if err := json.Unmarshal([]byte(success(t, result)), &flag); err != nil {
		t.Fatal(err)
	}
	return flag
}

func realService(t *testing.T) (*store.Store, *httptest.Server) {
	t.Helper()
	backend, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	s := httptest.NewUnstartedServer(nil)
	s.Config.Handler = api.NewHandler(backend, s.Listener.Addr().String())
	s.Start()
	t.Cleanup(s.Close)
	return backend, s
}

func TestCLIFlagLifecycle(t *testing.T) {
	_, s := realService(t)
	t.Setenv("FLAGCTL_SERVER", s.URL)
	ctx := t.Context()
	created := decodeFlag(t, execute(ctx, "flags", "create", "checkout", "--env", "dev", "--description", "New checkout", "-o", "json"))
	if created.Key != "checkout" || created.Environment != "dev" || created.Enabled || created.Description != "New checkout" {
		t.Fatalf("create = %+v", created)
	}
	if got := decodeFlag(t, execute(ctx, "flags", "get", "checkout", "--env", "dev", "-o", "json")); got != created {
		t.Fatalf("get = %+v", got)
	}
	var listed struct {
		Flags []flags.Flag `json:"flags"`
	}
	if err := json.Unmarshal([]byte(success(t, execute(ctx, "flags", "list", "--env", "dev", "-o", "json"))), &listed); err != nil || !reflect.DeepEqual(listed.Flags, []flags.Flag{created}) {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	prod := decodeFlag(t, execute(ctx, "flags", "create", "checkout", "--env", "prod", "--enabled=true", "-o", "json"))
	if !strings.Contains(failure(t, execute(ctx, "flags", "create", "checkout", "--env", "dev")), "409") {
		t.Error("duplicate error lacks HTTP status")
	}
	enabled := decodeFlag(t, execute(ctx, "flags", "toggle", "checkout", "--env", "dev", "--enabled=true", "-o", "json"))
	if !enabled.Enabled || enabled.Description != created.Description || !enabled.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("enable = %+v", enabled)
	}
	if got := decodeFlag(t, execute(ctx, "flags", "toggle", "checkout", "--env", "dev", "--enabled=true", "-o", "json")); got != enabled {
		t.Fatal("repeated explicit state changed flag")
	}
	disabled := decodeFlag(t, execute(ctx, "flags", "toggle", "checkout", "--env", "dev", "--enabled=false", "-o", "json"))
	if disabled.Enabled || disabled.Description != created.Description {
		t.Fatalf("disable = %+v", disabled)
	}
	deleted := success(t, execute(ctx, "flags", "delete", "checkout", "--env", "dev", "-o", "json"))
	var ack struct {
		Environment, Key string
		Deleted          bool
	}
	if err := json.Unmarshal([]byte(deleted), &ack); err != nil || ack.Environment != "dev" || ack.Key != "checkout" || !ack.Deleted {
		t.Fatalf("delete = %s, %v", deleted, err)
	}
	for _, args := range [][]string{{"get", "checkout"}, {"toggle", "checkout", "--enabled=true"}, {"delete", "checkout"}} {
		if message := failure(t, execute(ctx, append(append([]string{"flags"}, args...), "--env", "dev")...)); !strings.Contains(message, "404") {
			t.Errorf("missing record = %s", message)
		}
	}
	if got := decodeFlag(t, execute(ctx, "flags", "get", "checkout", "--env", "prod", "-o", "json")); got != prod {
		t.Fatal("dev operations affected prod")
	}
	if err := json.Unmarshal([]byte(success(t, execute(ctx, "flags", "list", "--env", "dev", "-o", "json"))), &listed); err != nil || listed.Flags == nil || len(listed.Flags) != 0 {
		t.Fatalf("empty list = %+v, %v", listed, err)
	}
}

func TestTableOutputEscapesTerminalControls(t *testing.T) {
	_, s := realService(t)
	t.Setenv("FLAGCTL_SERVER", s.URL)
	ctx := t.Context()
	if output := success(t, execute(ctx, "flags", "list", "--env", "dev")); len(strings.Split(strings.TrimSpace(output), "\n")) != 1 {
		t.Fatal("empty table should contain only headers")
	}
	description := "line1\nline2\t\x1b]52;c;synthetic\a\u202e"
	for _, args := range [][]string{
		{"create", "checkout", "--description", description},
		{"get", "checkout"},
		{"list"},
		{"toggle", "checkout", "--enabled=true"},
	} {
		output := success(t, execute(ctx, append(append([]string{"flags"}, args...), "--env", "dev")...))
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) != 2 || !reflect.DeepEqual(strings.Fields(lines[0]), []string{"ENVIRONMENT", "KEY", "ENABLED", "DESCRIPTION"}) {
			t.Fatalf("invalid table: %q", output)
		}
		if strings.ContainsAny(output, "\x1b\a\u202e") || !strings.Contains(output, `line1\nline2\t`) {
			t.Fatalf("unescaped controls or missing text: %q", output)
		}
	}
	if got := decodeFlag(t, execute(ctx, "flags", "get", "checkout", "--env", "dev", "-o", "json")); got.Description != description {
		t.Fatal("JSON did not preserve description")
	}
	output := success(t, execute(ctx, "flags", "delete", "checkout", "--env", "dev"))
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 || !reflect.DeepEqual(strings.Fields(lines[0]), []string{"ENVIRONMENT", "KEY", "DELETED"}) || !strings.Contains(lines[1], "true") {
		t.Fatalf("delete table = %q", output)
	}
}

func TestHelpAndConfigurationPrecedence(t *testing.T) {
	badEndpoint := "http://user:" + marker + "@localhost:8080"
	t.Setenv("FLAGCTL_SERVER", badEndpoint)
	for _, args := range [][]string{{"--help"}, {"flags", "--help"}, {"flags", "toggle", "--help"}} {
		output := success(t, execute(t.Context(), args...))
		if strings.Contains(output, marker) || !strings.Contains(output, "--server") || !strings.Contains(output, client.DefaultServer) {
			t.Fatalf("help leaked configuration or omitted default: %q", output)
		}
	}
	var hitsA, hitsB atomic.Int32
	handler := func(hits *atomic.Int32) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"flags":[]}`)
		}
	}
	a := httptest.NewServer(handler(&hitsA))
	t.Cleanup(a.Close)
	b := httptest.NewServer(handler(&hitsB))
	t.Cleanup(b.Close)
	t.Setenv("FLAGCTL_SERVER", a.URL)
	success(t, execute(t.Context(), "flags", "list", "--env", "dev"))
	success(t, execute(t.Context(), "--server", b.URL, "flags", "list", "--env", "dev"))
	t.Setenv("FLAGCTL_SERVER", badEndpoint)
	success(t, execute(t.Context(), "flags", "list", "--env", "dev", "--server", b.URL))
	if hitsA.Load() != 1 || hitsB.Load() != 2 {
		t.Fatalf("configuration precedence: A=%d, B=%d", hitsA.Load(), hitsB.Load())
	}
	if message := failure(t, execute(t.Context(), "flags", "list", "--env", "dev")); strings.Contains(message, marker) {
		t.Error("invalid endpoint leaked")
	}
}

func TestInvalidCommandsMakeNoRequests(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(500) }))
	t.Cleanup(s.Close)
	t.Setenv("FLAGCTL_SERVER", s.URL)
	for _, operation := range []string{"create", "list", "get", "toggle", "delete"} {
		base := []string{"flags", operation}
		if operation != "list" {
			base = append(base, "checkout")
		}
		if operation == "toggle" {
			base = append(base, "--enabled=false")
		}
		failure(t, execute(t.Context(), base...)) // required environment
		for _, extra := range [][]string{{"--env", "DEV"}, {"--env", "dev", "--output", "xml"}, {"--env", "dev", "--timeout", "0s"}, {"--env", "dev", "extra"}} {
			failure(t, execute(t.Context(), append(append([]string{}, base...), extra...)...))
		}
		if operation != "list" {
			args := []string{"flags", operation, "Bad Key", "--env", "dev"}
			if operation == "toggle" {
				args = append(args, "--enabled=false")
			}
			failure(t, execute(t.Context(), args...))
			failure(t, execute(t.Context(), "flags", operation, "--env", "dev"))
		}
	}
	for _, args := range [][]string{
		{"flags", "--env", "dev"}, {"flags", "unknown", "--env", "dev"},
		{"flags", "toggle", "checkout", "--env", "dev"},
		{"flags", "toggle", "checkout", "--env", "dev", "--enabled"},
		{"flags", "toggle", "checkout", "--env", "dev", "--enabled=" + marker},
		{"flags", "create", "checkout", "--env", "dev", "--description", strings.Repeat("x", 1025)},
		{"flags", "list", "--env", "dev", "--server", "http://localhost/?token=" + marker},
		{"flags", "list", "--env", "dev", "--unknown=" + marker},
	} {
		if message := failure(t, execute(t.Context(), args...)); strings.Contains(message, marker) {
			t.Error("invalid option echoed sensitive value")
		}
	}
	if hits.Load() != 0 {
		t.Errorf("invalid commands sent %d requests", hits.Load())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("synthetic output failure") }

func TestOutputFailureReportsCommittedMutation(t *testing.T) {
	backend, s := realService(t)
	t.Setenv("FLAGCTL_SERVER", s.URL)
	for _, format := range []string{"json", "table"} {
		for _, operation := range []string{"create", "get", "list", "toggle", "delete"} {
			command := cli.NewCommand(failingWriter{}, io.Discard)
			args := []string{"flags", operation}
			if operation != "list" {
				args = append(args, format)
			}
			if operation == "toggle" {
				args = append(args, "--enabled=true")
			}
			command.SetArgs(append(args, "--env", "dev", "--output", format))
			err := command.ExecuteContext(t.Context())
			if err == nil {
				t.Fatal("output failure was ignored")
			}
			if operation == "create" || operation == "toggle" {
				flag, getErr := backend.Get(t.Context(), "dev", format)
				if getErr != nil || flag.Enabled != (operation == "toggle") || !strings.Contains(err.Error(), "flag was") {
					t.Fatalf("mutation/diagnostic = %+v, %v, %v", flag, getErr, err)
				}
			}
			if operation == "delete" {
				if _, getErr := backend.Get(t.Context(), "dev", format); !errors.Is(getErr, store.ErrNotFound) || !strings.Contains(err.Error(), "flag was deleted") {
					t.Fatalf("delete/output failure = %v, %v", getErr, err)
				}
			}
		}
	}
}

func TestCommandFailuresAndCancellation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500); io.WriteString(w, marker) }))
	t.Cleanup(s.Close)
	t.Setenv("FLAGCTL_SERVER", s.URL)
	for _, operation := range []string{"create", "get", "list", "toggle", "delete"} {
		args := []string{"flags", operation}
		if operation != "list" {
			args = append(args, "checkout")
		}
		if operation == "toggle" {
			args = append(args, "--enabled=true")
		}
		message := failure(t, execute(t.Context(), append(args, "--env", "dev")...))
		if strings.Contains(message, marker) || !strings.Contains(message, "500") {
			t.Errorf("unsafe/unhelpful error: %s", message)
		}
	}
	for _, mode := range []string{"timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			received, release := make(chan struct{}, 1), make(chan struct{})
			slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- struct{}{}
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			t.Cleanup(slow.Close)
			t.Cleanup(func() { close(release) })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			args := []string{"flags", "get", "checkout", "--env", "dev", "--server", slow.URL}
			if mode == "timeout" {
				args = append(args, "--timeout", "100ms")
			}
			done := make(chan result, 1)
			go func() { done <- execute(ctx, args...) }()
			if mode == "cancel" {
				select {
				case <-received:
					cancel()
				case <-time.After(5 * time.Second):
					t.Fatal("request never arrived")
				}
			}
			select {
			case result := <-done:
				want := "timed out"
				if mode == "cancel" {
					want = "canceled"
				}
				if message := failure(t, result); !strings.Contains(message, want) {
					t.Errorf("got %s, want %s", message, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("command did not stop")
			}
		})
	}
}
