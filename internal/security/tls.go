package security

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"time"
)

// ClientTLS uses system trust by default. An explicit PEM CA bundle replaces
// system roots for this connection; certificate and hostname checks stay enabled.
func ClientTLS(caFile string) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS13}
	if caFile == "" {
		return config, nil
	}
	pem, err := readFile(caFile, false, 128<<10)
	if err != nil {
		return nil, errors.New("could not read CA file; use a regular PEM certificate file, not a symlink")
	}
	config.RootCAs = x509.NewCertPool()
	if !config.RootCAs.AppendCertsFromPEM(pem) {
		return nil, errors.New("CA file must contain PEM certificates")
	}
	return config, nil
}

func ServerTLS(certFile, keyFile string) (*tls.Config, error) {
	certificatePEM, err := readFile(certFile, false, 128<<10)
	if err != nil {
		return nil, errors.New("could not read TLS certificate file; use a regular PEM certificate file, not a symlink")
	}
	keyPEM, err := readFile(keyFile, true, 128<<10)
	if err != nil {
		return nil, errors.New("could not read TLS key file; use a private regular file (chmod 600), not a symlink")
	}
	certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		return nil, errors.New("TLS certificate and private key must be valid, matching PEM files")
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return nil, errors.New("TLS certificate is invalid, expired, or not yet valid")
	}
	certificate.Leaf = leaf
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}}, nil
}
