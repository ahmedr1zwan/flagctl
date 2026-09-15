package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
)

func TestListenAddressBoundary(t *testing.T) {
	for _, address := range []string{"127.0.0.1:0", "127.0.0.2:8080", "[::1]:8080", "[::ffff:127.0.0.1]:8080"} {
		if _, err := parseListenAddress(address, false); err != nil {
			t.Errorf("loopback address rejected: %s: %v", address, err)
		}
	}
	for _, address := range []string{"", "localhost:8080", ":8080", "0.0.0.0:8080", "[::]:8080", "192.0.2.1:8080", "[2001:db8::1]:8080", "[::1%lo0]:8080", "127.0.0.1:-1", "127.0.0.1:65536"} {
		if _, err := parseListenAddress(address, false); err == nil {
			t.Errorf("unsafe/invalid address accepted: %s", address)
		}
	}
}

func TestRunRejectsInvalidOptionsBeforeCreatingData(t *testing.T) {
	for _, args := range [][]string{{"--listen", "0.0.0.0:0"}, {"--listen", "localhost:0"}, {"--unknown"}, {"unexpected"}} {
		directory := filepath.Join(t.TempDir(), "data")
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		err := run(ctx, append([]string{"--listen", "127.0.0.1:0", "--data-dir", directory}, args...))
		cancel()
		if err == nil {
			t.Error("invalid service options succeeded")
		}
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Errorf("invalid options created data: %v", err)
		}
	}
	if err := run(t.Context(), []string{"--help"}); err != nil {
		t.Fatalf("help failed: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	directory := filepath.Join(t.TempDir(), "data")
	if err := run(t.Context(), []string{"--listen", listener.Addr().String(), "--data-dir", directory}); err == nil {
		t.Fatal("occupied port succeeded")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Errorf("occupied port created data: %v", err)
	}
}

type serviceEvent struct {
	Message string `json:"msg"`
	Address string `json:"address"`
	TLS     bool   `json:"tls"`
}
type eventWriter struct{ events chan serviceEvent }

func (w eventWriter) Write(data []byte) (int, error) {
	var event serviceEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return 0, err
	}
	select {
	case w.events <- event:
	default:
	}
	return len(data), nil
}

func startService(t *testing.T, directory string, arguments ...string) (string, func()) {
	t.Helper()
	events := make(chan serviceEvent, 8)
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(eventWriter{events: events}, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var result error
	go func() {
		result = run(ctx, append([]string{"--listen", "127.0.0.1:0", "--data-dir", directory}, arguments...))
		close(done)
	}()
	stop := func() {
		cancel()
		select {
		case <-done:
			if result != nil {
				t.Errorf("service stopped with error: %v", result)
			}
		case <-time.After(10 * time.Second):
			t.Error("service did not shut down")
		}
	}
	t.Cleanup(stop)
	select {
	case event := <-events:
		if event.Message != "flagd listening" || event.Address == "" {
			t.Fatalf("unexpected startup event: %+v", event)
		}
		scheme := "http://"
		if event.TLS {
			scheme = "https://"
		}
		return scheme + event.Address, stop
	case <-done:
		t.Fatalf("service exited before ready: %v", result)
	case <-time.After(10 * time.Second):
		t.Fatal("service did not start")
	}
	return "", stop
}

func TestServiceShutdownAndRestartPersistence(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "data")
	endpoint, stop := startService(t, directory)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := httpClient.Get(endpoint + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("health = %s, %v", body, err)
	}
	c, err := client.New(endpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	created, err := c.Create(t.Context(), "dev", flags.CreateInput{Key: "persisted"})
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if response, err := httpClient.Get(endpoint + "/healthz"); err == nil {
		response.Body.Close()
		t.Error("service still reachable after shutdown")
	}
	endpoint, stopAgain := startService(t, directory)
	defer stopAgain()
	reopened, err := client.New(endpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.Get(t.Context(), "dev", "persisted"); err != nil || got != created {
		t.Fatalf("service restart lost data: %+v, %v", got, err)
	}
}

func TestStorageAndCanceledStartupFailures(t *testing.T) {
	// A file in place of a directory forces startup failure before serving.
	directory := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(directory, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"--listen", "127.0.0.1:0", "--data-dir", directory}); err == nil {
		t.Fatal("invalid data directory succeeded")
	}
	// A canceled startup also returns promptly rather than leaving a running service.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := run(ctx, []string{"--listen", "127.0.0.1:0", "--data-dir", filepath.Join(t.TempDir(), "data")}); err == nil {
		t.Fatal("canceled startup succeeded")
	}
}
