package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/security"
)

// The container probe uses the public origin for Host and TLS verification,
// while connecting directly to the listener inside its own network namespace.
// It needs only a CA certificate, never a token or a private key.
func runHealthcheck(ctx context.Context, args []string) error {
	options := flag.NewFlagSet("flagd healthcheck", flag.ContinueOnError)
	server := options.String("server", "https://localhost:8443", "HTTPS public origin to verify")
	connect := options.String("connect", "127.0.0.1:8443", "literal loopback IP:port of the service listener")
	caFile := options.String("ca-file", "", "PEM CA bundle; empty uses system trust")
	if err := options.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid healthcheck option; use healthcheck --help")
	}
	if options.NArg() != 0 {
		return errors.New("healthcheck does not accept positional arguments")
	}
	origin, err := security.ParseOrigin(*server)
	if err != nil || origin.Scheme != "https" {
		return errors.New("healthcheck requires an HTTPS origin without credentials, paths, queries, or fragments")
	}
	address, err := parseListenAddress(*connect, false)
	if err != nil || address.Port() == 0 {
		return errors.New("healthcheck --connect must be a loopback IP and a nonzero port")
	}
	tlsConfig, err := security.ClientTLS(*caFile)
	if err != nil {
		return err
	}
	const timeout = 2 * time.Second
	transport := &http.Transport{
		Proxy:                  nil,
		TLSClientConfig:        tlsConfig,
		TLSHandshakeTimeout:    timeout,
		MaxResponseHeaderBytes: 16 << 10,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, address.String())
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	origin.Path = "/healthz"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin.String(), nil)
	if err != nil {
		return errors.New("could not construct healthcheck request")
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("healthcheck could not reach the service with verified TLS")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1025))
	var health struct {
		Status string `json:"status"`
	}
	if err != nil || len(data) > 1024 || response.StatusCode != http.StatusOK ||
		json.Unmarshal(data, &health) != nil || health.Status != "ok" {
		return errors.New("healthcheck received an unhealthy response")
	}
	return nil
}
