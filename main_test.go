package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCertificate writes a self-signed certificate for name, and its key.
func writeCertificate(t *testing.T, dir, name string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		DNSNames: []string{name}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	os.WriteFile(filepath.Join(dir, "cert.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(filepath.Join(dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
}

func TestTLSConfigServesTheCurrentCertificate(t *testing.T) {
	dir := t.TempDir()
	config := tlsConfig(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
	if _, err := config.GetCertificate(&tls.ClientHelloInfo{}); err == nil {
		t.Error("served a certificate that does not exist")
	}
	for _, name := range []string{"first.example", "renewed.example"} {
		writeCertificate(t, dir, name)
		cert, err := config.GetCertificate(&tls.ClientHelloInfo{})
		if err != nil {
			t.Fatal(err)
		}
		leaf, _ := x509.ParseCertificate(cert.Certificate[0])
		if leaf.Subject.CommonName != name {
			t.Errorf("served %s, want %s", leaf.Subject.CommonName, name)
		}
	}
}

func TestLocalAddressKeepsTheScheme(t *testing.T) {
	for _, tc := range []struct {
		start  func(http.Handler) *httptest.Server
		scheme string
	}{{httptest.NewServer, "http://"}, {httptest.NewTLSServer, "https://"}} {
		server := tc.start(newFakeServer(t))
		resp, err := server.Client().Get(server.URL + "/System/Info/Public")
		if err != nil {
			t.Fatal(err)
		}
		var info PublicSystemInfo
		json.NewDecoder(resp.Body).Decode(&info)
		resp.Body.Close()
		server.Close()
		if !strings.HasPrefix(info.LocalAddress, tc.scheme) {
			t.Errorf("LocalAddress %q, want %s", info.LocalAddress, tc.scheme)
		}
	}
}
