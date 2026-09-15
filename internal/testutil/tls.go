// Package testutil provides isolated TLS credentials for integration tests.
package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type TLSFiles struct {
	Certificate                          tls.Certificate
	CertFile, KeyFile, CAFile, TokenFile string
	Token                                string
}

// NewTLS creates a short-lived CA and server keypair. Nothing is installed in
// system trust; private keys exist only in this test's temporary directory.
func NewTLS(t testing.TB, customize ...func(*x509.Certificate)) TLSFiles {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("secure mode requires POSIX file permissions")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "flagctl test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"localhost", "flagd.test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	for _, change := range customize {
		change(server)
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	files := TLSFiles{CertFile: filepath.Join(directory, "server.crt"), KeyFile: filepath.Join(directory, "server.key"), CAFile: filepath.Join(directory, "ca.crt"), TokenFile: filepath.Join(directory, "api.token"), Token: strings.Repeat("a", 64)}
	contents := map[string][]byte{
		files.CertFile:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		files.KeyFile:   pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		files.CAFile:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		files.TokenFile: []byte(files.Token + "\n"),
	}
	for path, data := range contents {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	files.Certificate, err = tls.X509KeyPair(contents[files.CertFile], contents[files.KeyFile])
	if err != nil {
		t.Fatal(err)
	}
	return files
}
