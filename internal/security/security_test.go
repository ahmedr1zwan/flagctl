package security_test

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/security"
	"github.com/ahmedr1zwan/flagctl/internal/testutil"
)

func TestTokenFilesAndAuthentication(t *testing.T) {
	files := testutil.NewTLS(t)
	for _, suffix := range []string{"", "\n", "\r\n"} {
		if err := os.WriteFile(files.TokenFile, []byte(files.Token+suffix), 0600); err != nil {
			t.Fatal(err)
		}
		token, err := security.ReadTokenFile(files.TokenFile)
		if err != nil {
			t.Fatal(err)
		}
		if !token.IsSet() || !token.Matches([]string{"Bearer " + files.Token}) || !token.Matches([]string{"bearer " + files.Token}) {
			t.Fatal("valid token rejected")
		}
		for _, header := range [][]string{nil, {""}, {"Basic " + files.Token}, {"Bearer  " + files.Token}, {"Bearer " + files.Token + " "}, {"Bearer " + strings.Repeat("b", 64)}, {"Bearer " + files.Token, "Bearer " + files.Token}, {"Bearer " + strings.ToUpper(files.Token)}} {
			if token.Matches(header) {
				t.Error("invalid authorization accepted")
			}
		}
		if strings.Contains(fmt.Sprintf("%s %v %+v %#v", token, token, token, token), files.Token) {
			t.Fatal("formatting exposed token")
		}
	}
	for _, value := range []string{"", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("A", 64), strings.Repeat("g", 64), files.Token + "\r", files.Token + "\n\n", " " + files.Token} {
		if err := os.WriteFile(files.TokenFile, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		token, err := security.ReadTokenFile(files.TokenFile)
		if err == nil || token.IsSet() || strings.Contains(err.Error(), files.Token) || strings.Contains(err.Error(), files.TokenFile) {
			t.Fatal("invalid token was accepted or exposed")
		}
	}
	var empty security.Token
	if empty.Matches([]string{"Bearer "}) {
		t.Fatal("zero token authenticated")
	}
}

func TestCredentialFileBoundaries(t *testing.T) {
	files := testutil.NewTLS(t)
	for _, mode := range []os.FileMode{0644, 0640, 0604} {
		if err := os.Chmod(files.TokenFile, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := security.ReadTokenFile(files.TokenFile); err == nil {
			t.Error("shared token file accepted")
		}
	}
	if err := os.Chmod(files.TokenFile, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked-token")
	if err := os.Symlink(files.TokenFile, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		if _, err := security.ReadTokenFile(path); err == nil {
			t.Error("unsafe path accepted")
		}
	}
	if err := os.Chmod(files.KeyFile, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := security.ServerTLS(files.CertFile, files.KeyFile); err == nil {
		t.Fatal("shared TLS key accepted")
	}
}

func TestTLSConfiguration(t *testing.T) {
	files, other := testutil.NewTLS(t), testutil.NewTLS(t)
	server, err := security.ServerTLS(files.CertFile, files.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	if server.MinVersion != tls.VersionTLS13 || server.Certificates[0].Leaf == nil {
		t.Fatal("server TLS policy missing")
	}
	client, err := security.ClientTLS(files.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	if client.MinVersion != tls.VersionTLS13 || client.InsecureSkipVerify {
		t.Fatal("client TLS policy missing")
	}
	if _, err := server.Certificates[0].Leaf.Verify(x509.VerifyOptions{Roots: client.RootCAs, DNSName: "localhost"}); err != nil {
		t.Fatal(err)
	}
	system, err := security.ClientTLS("")
	if err != nil || system.RootCAs != nil {
		t.Fatal("system trust default changed")
	}
	for _, paths := range [][2]string{{files.CertFile, other.KeyFile}, {"missing", files.KeyFile}, {files.CertFile, "missing"}, {files.TokenFile, files.KeyFile}, {files.CertFile, files.TokenFile}} {
		if _, err := security.ServerTLS(paths[0], paths[1]); err == nil {
			t.Error("invalid certificate/key accepted")
		}
	}
	for _, path := range []string{"missing", files.TokenFile, t.TempDir()} {
		if _, err := security.ClientTLS(path); err == nil {
			t.Error("invalid CA accepted")
		}
	}
	for _, offset := range []time.Duration{-3 * time.Hour, 3 * time.Hour} {
		expired := testutil.NewTLS(t, func(cert *x509.Certificate) {
			cert.NotBefore = cert.NotBefore.Add(offset)
			cert.NotAfter = cert.NotAfter.Add(offset)
		})
		if _, err := security.ServerTLS(expired.CertFile, expired.KeyFile); err == nil {
			t.Error("certificate validity ignored")
		}
	}
	large := filepath.Join(t.TempDir(), "large.crt")
	if err := os.WriteFile(large, []byte(strings.Repeat("x", 129<<10)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := security.ClientTLS(large); err == nil {
		t.Fatal("oversized certificate file accepted")
	}
}

func TestOrigins(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1", "http://localhost:80/", "https://example.com:443", "https://[::1]", "https://[2001:db8::1]:8443", "https://192.0.2.1"} {
		if _, err := security.ParseOrigin(value); err != nil {
			t.Errorf("valid origin %s: %v", value, err)
		}
	}
	for _, value := range []string{"", "https://", "https://user:secret@example.com", "https://example.com/a", "https://example.com?", "https://example.com#", "ftp://example.com", "https://example.com:0", "https://example.com:65536", "https://example.com:", "https://[::1%25lo0]", "https://0.0.0.0", "https://[::]", "https://[::ffff:0.0.0.0]", "https://224.0.0.1", "https://example.com.", "https://-example.com", "https://example-.com", "https://a..com", "https://éxample.com", "https://a_b.com"} {
		if _, err := security.ParseOrigin(value); err == nil {
			t.Errorf("invalid origin accepted: %s", value)
		}
	}
}
