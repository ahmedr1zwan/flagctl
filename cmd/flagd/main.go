// Command flagd runs the feature-flag service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
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

// Set by GoReleaser; source builds identify themselves as development builds.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("flagd stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "healthcheck" {
		return runHealthcheck(ctx, args[1:])
	}
	options := flag.NewFlagSet("flagd", flag.ContinueOnError)
	options.Usage = func() {
		fmt.Fprintln(options.Output(), "Usage: flagd [options] | flagd healthcheck [options]")
		options.PrintDefaults()
	}
	listen := options.String("listen", "127.0.0.1:8080", "literal IP:port to listen on (loopback by default)")
	dataDir := options.String("data-dir", "data", "private directory for flags.db (relative to the working directory)")
	showVersion := options.Bool("version", false, "print version and exit")
	var access accessOptions
	options.BoolVar(&access.allowNetwork, "allow-network", false, "allow network listeners; requires TLS, a token file, and a public origin")
	options.StringVar(&access.publicOrigin, "public-origin", "", "HTTPS origin clients use; determines accepted Host")
	options.StringVar(&access.tokenFile, "token-file", "", "private file containing a 64-character hexadecimal bearer token")
	options.StringVar(&access.certFile, "tls-cert-file", "", "PEM server certificate chain")
	options.StringVar(&access.keyFile, "tls-key-file", "", "private PEM server key file")
	if err := options.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if options.NArg() != 0 {
		return errors.New("unexpected positional arguments; use --help for usage")
	}
	if *showVersion {
		_, err := fmt.Fprintln(os.Stdout, "flagd version", version)
		return err
	}
	addr, err := parseListenAddress(*listen, access.allowNetwork)
	if err != nil {
		return err
	}
	secure, err := access.load(addr)
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
	handler := api.NewHandler(flagStore, listener.Addr().String())
	if secure.tls != nil {
		if secure.origin == "" {
			secure.origin = "https://" + listener.Addr().String()
		}
		handler, err = api.NewSecureHandler(flagStore, secure.origin, secure.token)
		if err != nil {
			return err
		}
	}

	server := &http.Server{
		Handler:           handler,
		TLSConfig:         secure.tls,
		ErrorLog:          log.New(serverErrorWriter{}, "", 0),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() {
		if secure.tls != nil {
			serveErrors <- server.ServeTLS(listener, "", "")
		} else {
			serveErrors <- server.Serve(listener)
		}
	}()
	slog.Info("flagd listening", "address", listener.Addr().String(), "tls", secure.tls != nil)

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

// Avoid DNS resolution when enforcing the explicit network opt-in boundary.
func parseListenAddress(value string, allowNetwork bool) (netip.AddrPort, error) {
	addr, err := netip.ParseAddrPort(value)
	if err != nil {
		return netip.AddrPort{}, errors.New("--listen must be a literal IP:port, such as 127.0.0.1:8080 or [::1]:8080")
	}
	if addr.Addr().Zone() != "" || addr.Addr().Unmap().IsMulticast() {
		return netip.AddrPort{}, errors.New("--listen cannot use multicast addresses or IPv6 zones")
	}
	if !allowNetwork && !addr.Addr().Unmap().IsLoopback() {
		return netip.AddrPort{}, errors.New("network listeners require --allow-network with TLS, a token file, and a public origin")
	}
	return addr, nil
}
