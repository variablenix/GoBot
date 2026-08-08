package bot

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

// saslMechanism returns the configured SASL mechanism, defaulting to PLAIN
// for existing password-based deployments and EXTERNAL when a client
// certificate is configured.
func saslMechanism(identity IdentityConfig, server ServerConfig) (string, error) {
	if strings.TrimSpace(server.ClientKey) != "" && strings.TrimSpace(server.ClientCert) == "" {
		return "", fmt.Errorf("server.client_key requires server.client_cert")
	}
	mechanism := strings.ToUpper(strings.TrimSpace(identity.SASLMechanism))
	if mechanism == "" {
		if strings.TrimSpace(server.ClientCert) != "" {
			mechanism = "EXTERNAL"
		} else if identity.SASLUser != "" || identity.SASLPass != "" {
			mechanism = "PLAIN"
		}
	}

	switch mechanism {
	case "":
		return "", nil
	case "PLAIN", "EXTERNAL":
		return mechanism, nil
	default:
		return "", fmt.Errorf("unsupported SASL mechanism %q; use plain or external", mechanism)
	}
}

func loadTLSClientCertificate(certPath, keyPath string) (tls.Certificate, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read client certificate %q: %w", certPath, err)
	}

	if keyPath != "" {
		keyPEM, err := os.ReadFile(keyPath)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("read client key %q: %w", keyPath, err)
		}
		certificate, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("parse client certificate/key: %w", err)
		}
		return certificate, nil
	}

	// Some IRC networks document a combined PEM containing the certificate and
	// private key. Extract both blocks so that format works without requiring
	// the private key to be duplicated into another file.
	var certificatePEM, privateKeyPEM []byte
	remaining := certPEM
	for len(remaining) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil {
			break
		}
		remaining = rest
		switch block.Type {
		case "CERTIFICATE":
			certificatePEM = append(certificatePEM, pem.EncodeToMemory(block)...)
		case "PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY":
			privateKeyPEM = append(privateKeyPEM, pem.EncodeToMemory(block)...)
		}
	}
	if len(certificatePEM) == 0 || len(privateKeyPEM) == 0 {
		return tls.Certificate{}, fmt.Errorf("client certificate file %q must contain a CERTIFICATE and private key PEM block", certPath)
	}

	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse combined client certificate/key: %w", err)
	}
	return certificate, nil
}

func clientCertificateFingerprint(certificate tls.Certificate) (string, error) {
	if len(certificate.Certificate) == 0 {
		return "", fmt.Errorf("client certificate does not contain a certificate")
	}
	hash := sha256.Sum256(certificate.Certificate[0])
	return hex.EncodeToString(hash[:]), nil
}

func validNickServTarget(target string) bool {
	if target == "" {
		return false
	}
	for _, r := range target {
		if r <= 0x20 || r == 0x7f || r == ':' || r == ',' {
			return false
		}
	}
	return true
}
