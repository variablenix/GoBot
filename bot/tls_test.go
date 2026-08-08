package bot

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSASLMechanismDefaultsToExternalForClientCertificate(t *testing.T) {
	got, err := saslMechanism(IdentityConfig{}, ServerConfig{ClientCert: "/tmp/ouch.pem"})
	if err != nil || got != "EXTERNAL" {
		t.Fatalf("saslMechanism() = %q, %v; want EXTERNAL", got, err)
	}
}

func TestSASLMechanismDefaultsToPlainForPasswordAuth(t *testing.T) {
	got, err := saslMechanism(IdentityConfig{SASLUser: "Echo", SASLPass: "secret"}, ServerConfig{})
	if err != nil || got != "PLAIN" {
		t.Fatalf("saslMechanism() = %q, %v; want PLAIN", got, err)
	}
}

func TestLoadTLSClientCertificateSupportsCombinedPEM(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          newSerialNumber(t),
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	combined := append(
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustMarshalPKCS8(t, privateKey)}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...,
	)
	path := filepath.Join(t.TempDir(), "client.pem")
	if err := os.WriteFile(path, combined, 0o600); err != nil {
		t.Fatal(err)
	}

	certificate, err := loadTLSClientCertificate(path, "")
	if err != nil {
		t.Fatalf("loadTLSClientCertificate() error = %v", err)
	}
	if len(certificate.Certificate) != 1 {
		t.Fatalf("loaded %d certificates, want 1", len(certificate.Certificate))
	}
}

func newSerialNumber(t *testing.T) *big.Int {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	return serial
}

func mustMarshalPKCS8(t *testing.T, privateKey ed25519.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return der
}
