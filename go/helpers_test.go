// Test helpers for the e2e suite (indirections to keep e2e_test.go terse).
package go_test

import (
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"

	"github.com/Karl-Dai/ModbusSim/go/internal/logcol"
)

var (
	randReader    = rand.Reader
	ellipticCurve = elliptic.P256
)

// bigOne returns a serial number of 1 for test certificates.
func bigOne() *big.Int { return big.NewInt(1) }

func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

// pemEncodeCert renders a certificate as a PEM block.
func pemEncodeCert(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

func logcolNewCollector() *logcol.Collector { return logcol.NewCollector() }

// logcolSink adapts the master LogSink; master-side detail assertions are
// covered by unit tests, so this sink intentionally discards entries.
type logcolSink struct{}

func (logcolSink) AddRequest(direction string, fc uint8, detail string) {}

