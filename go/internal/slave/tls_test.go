// TLS server-identity loading tests. The PKCS#12 path must put the leaf's own
// DER at the head of tls.Certificate.Certificate: crypto/tls transmits that
// slice and ignores Leaf, so a chain built from CA certs alone makes clients
// validate the CA (which has no IP SAN) instead of the server identity.
package slave

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"os"
	"testing"
	"time"

	sslmatepkcs12 "software.sslmate.com/src/go-pkcs12"
)

// testIdentity returns a CA certificate plus a PKCS#12 blob holding a
// server leaf signed by it, with a 127.0.0.1 SAN.
func testIdentity(t *testing.T) (*x509.Certificate, []byte) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "server"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leafCert, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}

	p12, err := sslmatepkcs12.Encode(rand.Reader, leafKey, leafCert, []*x509.Certificate{caCert}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	return caCert, p12
}

func TestBuildTLSConfigPKCS12ChainLeadsWithLeaf(t *testing.T) {
	caCert, p12 := testIdentity(t)

	dir := t.TempDir()
	p12File := dir + "/server.p12"
	if err := os.WriteFile(p12File, p12, 0o600); err != nil {
		t.Fatal(err)
	}

	tlsCfg, err := BuildTLSConfig(TLSConfig{PKCS12File: p12File, PKCS12Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Fatalf("certificates = %d, want 1", len(tlsCfg.Certificates))
	}
	chain := tlsCfg.Certificates[0].Certificate
	if len(chain) != 2 {
		t.Fatalf("chain length = %d, want 2 (leaf + CA)", len(chain))
	}

	served, err := x509.ParseCertificate(chain[0])
	if err != nil {
		t.Fatal(err)
	}
	if served.IsCA {
		t.Error("first chain element is the CA; the leaf must head the chain")
	}
	if served.Subject.CommonName != "server" {
		t.Errorf("served CN = %q, want %q", served.Subject.CommonName, "server")
	}
	if len(served.IPAddresses) == 0 {
		t.Error("served certificate has no IP SAN, so clients cannot validate 127.0.0.1")
	}

	root, err := x509.ParseCertificate(chain[1])
	if err != nil {
		t.Fatal(err)
	}
	if root.Subject.CommonName != caCert.Subject.CommonName {
		t.Errorf("second chain element = %q, want the CA", root.Subject.CommonName)
	}
}

func TestBuildTLSConfigRequiresCAForClientAuth(t *testing.T) {
	_, p12 := testIdentity(t)
	dir := t.TempDir()
	p12File := dir + "/server.p12"
	if err := os.WriteFile(p12File, p12, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := BuildTLSConfig(TLSConfig{
		PKCS12File: p12File, PKCS12Password: "pw", RequireClientCert: true,
	}); err == nil {
		t.Fatal("RequireClientCert without CAFile should fail")
	}
}

func TestBuildTLSConfigNeedsAnIdentity(t *testing.T) {
	if _, err := BuildTLSConfig(TLSConfig{}); err == nil {
		t.Fatal("empty TLS config should fail: no certificate configured")
	}
}
