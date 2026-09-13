// Command flagd runs the local feature-flag service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/api"
	"github.com/ahmedr1zwan/flagctl/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("flagd stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	options := flag.NewFlagSet("flagd", flag.ContinueOnError)
	listen := options.String("listen", "127.0.0.1:8080", "loopback IP:port to listen on (IPv6: [::1]:8080)")
	dataDir := options.String("data-dir", "data", "private directory for flags.db (relative to the working directory)")
	if err := options.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if options.NArg() != 0 {
		return errors.New("unexpected positional arguments; use --help for usage")
	}
	addr, err := parseListenAddress(*listen)
	if err != nil {
		return err
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr.String())
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	startupCtx, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
	flagStore, err := store.Open(startupCtx, *dataDir)
	cancelStartup()
	if err != nil {
		return err
	}
	defer flagStore.Close()

	server := &http.Server{
		Handler:           api.NewHandler(flagStore, listener.Addr().String()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()
	slog.Info("flagd listening", "address", listener.Addr().String())

	select {
	case err := <-serveErrors:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown: %w", err)
		}
		if err := <-serveErrors; !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
		slog.Info("flagd stopped")
		return nil
	}
}

// Only literal loopback addresses are allowed until network authentication and
// transport security exist. Avoid DNS resolution when enforcing this boundary.
func parseListenAddress(value string) (netip.AddrPort, error) {
	addr, err := netip.ParseAddrPort(value)
	if err != nil {
		return netip.AddrPort{}, errors.New("--listen must be a loopback IP:port, such as 127.0.0.1:8080 or [::1]:8080")
	}
	if addr.Addr().Zone() != "" || !addr.Addr().Unmap().IsLoopback() {
		return netip.AddrPort{}, errors.New("--listen must use a loopback IP; network access requires authentication and transport security, which are not implemented yet")
	}
	return addr, nil
}
