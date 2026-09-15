package main

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net/netip"

	"github.com/ahmedr1zwan/flagctl/internal/security"
)

type accessOptions struct {
	allowNetwork bool
	publicOrigin string
	tokenFile    string
	certFile     string
	keyFile      string
}

type accessConfig struct {
	tls    *tls.Config
	token  security.Token
	origin string
}

// Validate security before opening a listener or creating database files.
func (options accessOptions) load(address netip.AddrPort) (accessConfig, error) {
	configured := options.tokenFile != "" || options.certFile != "" || options.keyFile != ""
	if options.allowNetwork || !address.Addr().Unmap().IsLoopback() {
		if !configured || options.publicOrigin == "" {
			return accessConfig{}, errors.New("network access requires TLS certificate/key files, a token file, and --public-origin")
		}
	}
	if !configured {
		if options.publicOrigin != "" {
			return accessConfig{}, errors.New("--public-origin requires TLS and a token file")
		}
		return accessConfig{}, nil
	}
	if options.tokenFile == "" || options.certFile == "" || options.keyFile == "" {
		return accessConfig{}, errors.New("secure mode requires --token-file, --tls-cert-file, and --tls-key-file together")
	}
	token, err := security.ReadTokenFile(options.tokenFile)
	if err != nil {
		return accessConfig{}, err
	}
	config, err := security.ServerTLS(options.certFile, options.keyFile)
	if err != nil {
		return accessConfig{}, err
	}
	hostname := address.Addr().Unmap().String()
	if options.publicOrigin != "" {
		origin, err := security.ParseOrigin(options.publicOrigin)
		if err != nil || origin.Scheme != "https" {
			return accessConfig{}, errors.New("--public-origin must be an HTTPS origin without credentials, paths, queries, or fragments")
		}
		hostname = origin.Hostname()
	}
	if err := config.Certificates[0].Leaf.VerifyHostname(hostname); err != nil {
		return accessConfig{}, errors.New("TLS certificate must cover the public origin hostname (or listen IP when no public origin is set)")
	}
	return accessConfig{tls: config, token: token, origin: options.publicOrigin}, nil
}

// net/http's default error logger can include peer-controlled handshake bytes.
// Keep a diagnostic signal without copying request data into service logs.
type serverErrorWriter struct{}

func (serverErrorWriter) Write(data []byte) (int, error) {
	slog.Warn("HTTP connection failed")
	return len(data), nil
}
