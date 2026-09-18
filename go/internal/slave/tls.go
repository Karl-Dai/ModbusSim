// TLS server-side configuration for the Modbus TCP+TLS slave.
// Ported from crates/modbussim-core/src/tls_slave.rs: loads identity from
// PKCS#12 (priority) or PEM cert+key, min TLS 1.2, optional client cert
// authentication against a CA. Uses crypto/tls + x/crypto/pkcs12 (in-memory,
// no OS keychain).
package slave

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	sslmatepkcs12 "software.sslmate.com/src/go-pkcs12"
)

// TLSConfig mirrors Rust's SlaveTlsConfig (transport.rs) field names.
type TLSConfig struct {
	Enabled          bool   `json:"enabled"`
	CertFile         string `json:"cert_file"`
	KeyFile          string `json:"key_file"`
	CAFile           string `json:"ca_file"`
	RequireClientCert bool  `json:"require_client_cert"`
	PKCS12File       string `json:"pkcs12_file"`
	PKCS12Password   string `json:"pkcs12_password"`
}

// BuildTLSConfig builds a *tls.Config for the server side. Uses
// software.sslmate.com/src/go-pkcs12 which, like the Rust version's OpenSSL
// legacy-provider retry, also decodes legacy RC2/3DES PKCS#12 exports.
func BuildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	var cert tls.Certificate

	switch {
	case cfg.PKCS12File != "":
		p12Bytes, readErr := os.ReadFile(cfg.PKCS12File)
		if readErr != nil {
			return nil, fmt.Errorf("Failed to read PKCS#12 file '%s': %w", cfg.PKCS12File, readErr)
		}
		privKey, leaf, caCerts, err := sslmatepkcs12.DecodeChain(p12Bytes, cfg.PKCS12Password)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse PKCS#12: %w", err)
		}
		cert.PrivateKey = privKey
		// crypto/tls requires Certificate[0] to be the leaf; the pkcs12
		// CA certs follow as the chain.
		if leaf != nil {
			cert.Leaf = leaf
			cert.Certificate = append(cert.Certificate, leaf.Raw)
		}
		for _, ca := range caCerts {
			cert.Certificate = append(cert.Certificate, ca.Raw)
		}

	case cfg.CertFile != "" && cfg.KeyFile != "":
		certBytes, readErr := os.ReadFile(cfg.CertFile)
		if readErr != nil {
			return nil, fmt.Errorf("Failed to read cert file '%s': %w", cfg.CertFile, readErr)
		}
		keyBytes, readErr := os.ReadFile(cfg.KeyFile)
		if readErr != nil {
			return nil, fmt.Errorf("Failed to read key file '%s': %w", cfg.KeyFile, readErr)
		}
		parsed, parseErr := tls.X509KeyPair(certBytes, keyBytes)
		if parseErr != nil {
			return nil, fmt.Errorf("Failed to parse PEM certificate/key (use an unencrypted key or password-protected PKCS#12): %w", parseErr)
		}
		cert = parsed

	default:
		return nil, fmt.Errorf("No certificate configured: set pkcs12_file or cert_file+key_file")
	}

	out := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if cfg.RequireClientCert {
		if cfg.CAFile == "" {
			return nil, fmt.Errorf("Client certificate authentication requires a CA file")
		}
		caBytes, readErr := os.ReadFile(cfg.CAFile)
		if readErr != nil {
			return nil, fmt.Errorf("Failed to read CA file '%s': %w", cfg.CAFile, readErr)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("CA file contains no certificates")
		}
		out.ClientCAs = pool
		out.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return out, nil
}
