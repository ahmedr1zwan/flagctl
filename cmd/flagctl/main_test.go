package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Run the actual entry point in a child test executable, so os.Exit and signal
// handling are exercised without terminating the parent test runner.
func TestMain(m *testing.M) {
	if os.Getenv("FLAGCTL_TEST_PROCESS") == "1" {
		for index, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{"flagctl"}, os.Args[index+1:]...)
				main()
				os.Exit(0)
			}
		}
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func childCommand(t *testing.T, endpoint string, args ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, executable, append([]string{"--"}, args...)...)
	// Inherit only ordinary runtime paths, not developer credentials/configuration.
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "SYSTEMROOT"} {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	command.Env = append(command.Env, "FLAGCTL_TEST_PROCESS=1", "FLAGCTL_SERVER="+endpoint, "GORACE=atexit_sleep_ms=0")
	// Include child execution in the parent's coverage report and keep coverage
	// runtime warnings off the application streams that these tests assert.
	if directory := flag.Lookup("test.gocoverdir"); directory != nil && directory.Value.String() != "" {
		command.Env = append(command.Env, "GOCOVERDIR="+directory.Value.String())
	}
	return command
}

func TestProcessOutputAndExitStatus(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/missing") {
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"code":"not_found","message":"synthetic-sensitive-value"}}`)
			return
		}
		io.WriteString(w, `{"flags":[]}`)
	}))
	defer s.Close()
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"help", []string{"--help"}, 0},
		{"success", []string{"flags", "list", "--env", "dev", "--output", "json"}, 0},
		{"invalid input", []string{"flags", "list"}, 1},
		{"API failure", []string{"flags", "get", "missing", "--env", "dev"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := childCommand(t, s.URL, tc.args...)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			if code != tc.code {
				t.Fatalf("exit=%d, stdout=%q, stderr=%q", code, stdout.String(), stderr.String())
			}
			if code != 0 {
				if stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "Error:") || strings.Contains(stderr.String(), "synthetic-sensitive-value") {
					t.Fatalf("failure streams: stdout=%q, stderr=%q", stdout.String(), stderr.String())
				}
			} else if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr: %s", stderr.String())
			}
			if tc.name == "success" && !json.Valid(stdout.Bytes()) {
				t.Fatalf("stdout is not machine-readable JSON: %s", stdout.String())
			}
		})
	}
}

func TestInterruptCancelsRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sending os.Interrupt to a process is unsupported on Windows")
	}
	received, release := make(chan struct{}, 1), make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer s.Close()
	defer close(release)
	command := childCommand(t, s.URL, "flags", "list", "--env", "dev")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			command.Process.Kill()
			command.Wait()
		}
	}()
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("child request never arrived")
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err := command.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "canceled") {
		t.Fatalf("interrupt: %v, stdout=%q, stderr=%q", err, stdout.String(), stderr.String())
	}
}
