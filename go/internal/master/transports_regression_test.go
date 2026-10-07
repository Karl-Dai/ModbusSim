package master

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/mbap"
)

func TestMBAPTransportRejectsMismatchedHeader(t *testing.T) {
	for _, kind := range []string{"TCP", "TLS"} {
		for _, tc := range []struct {
			name   string
			header mbap.Header
			want   string
		}{
			{"transaction", mbap.Header{TransactionID: 2, Length: 5, UnitID: 7}, "transaction"},
			{"protocol", mbap.Header{TransactionID: 1, ProtocolID: 1, Length: 5, UnitID: 7}, "protocol"},
			{"unit", mbap.Header{TransactionID: 1, Length: 5, UnitID: 8}, "unit"},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				tr := scriptedMBAPTransport(t, kind, responseFrame(tc.header, []byte{3, 2, 0, 42}), 1)
				resp, err := tr.exchange(7, []byte{3, 0, 0, 0, 1}, time.Second)
				if err == nil {
					t.Fatalf("accepted mismatched %s: response %x", tc.name, resp)
				}
				if !strings.Contains(strings.ToLower(err.Error()), tc.want) {
					t.Errorf("error %q does not identify %s mismatch", err, tc.want)
				}
			})
		}
	}
}

func TestMBAPTransportAcceptsMatchingResponse(t *testing.T) {
	for _, kind := range []string{"TCP", "TLS"} {
		for _, tc := range []struct {
			name string
			pdu  []byte
		}{
			{"normal", []byte{3, 2, 0, 42}},
			{"exception", []byte{0x83, 2}},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				pdu := tc.pdu
				tr := scriptedMBAPTransport(t, kind, responseFrame(mbap.NewHeader(1, 7, len(pdu)), pdu), 1)
				resp, err := tr.exchange(7, []byte{3, 0, 0, 0, 1}, time.Second)
				if err != nil || !bytes.Equal(resp, pdu) {
					t.Fatalf("exchange = %x, %v; want %x", resp, err, pdu)
				}
			})
		}
	}
}

func TestMBAPTransportPreservesCoalescedResponses(t *testing.T) {
	for _, kind := range []string{"TCP", "TLS"} {
		t.Run(kind, func(t *testing.T) {
			first := []byte{3, 2, 0, 42}
			second := []byte{3, 2, 0, 43}
			// Model two complete frames already queued in the byte stream. Reading
			// one frame must leave the next intact, even across exchange calls.
			responses := append(responseFrame(mbap.NewHeader(1, 7, len(first)), first),
				responseFrame(mbap.NewHeader(2, 7, len(second)), second)...)
			tr := scriptedMBAPTransport(t, kind, responses, 2)
			for i, want := range [][]byte{first, second} {
				resp, err := tr.exchange(7, []byte{3, 0, byte(i), 0, 1}, time.Second)
				if err != nil || !bytes.Equal(resp, want) {
					t.Fatalf("exchange %d = %x, %v; want %x", i+1, resp, err, want)
				}
			}
		})
	}
}

func TestRTUTCPTransportKeepsOverallDeadline(t *testing.T) {
	pdu := []byte{3, 2, 0, 42}
	conn := &transportTestConn{input: bytes.NewReader(encodeRTUFrame(7, pdu)), chunkSize: 1}
	tr := &rtuTCPTransport{conn: conn}
	resp, err := tr.exchange(7, []byte{3, 0, 0, 0, 1}, time.Second)
	if err != nil || !bytes.Equal(resp, pdu) {
		t.Fatalf("fragmented exchange = %x, %v; want %x", resp, err, pdu)
	}
	if conn.deadline.IsZero() {
		t.Fatal("exchange did not set an overall deadline")
	}
	for _, deadline := range conn.readDeadlines {
		if deadline.After(conn.deadline) {
			t.Fatalf("read deadline extended beyond the overall deadline by %v", deadline.Sub(conn.deadline))
		}
	}
}

func responseFrame(header mbap.Header, pdu []byte) []byte {
	encoded := header.Encode()
	return append(encoded[:], pdu...)
}

// scriptedMBAPTransport gives TCP a deterministic stream and TLS an actual
// client/server session. TLS sends coalesced frames in one application record
// so a discarded read-ahead buffer loses the second frame deterministically.
func scriptedMBAPTransport(t *testing.T, kind string, response []byte, requestCount int) transport {
	t.Helper()
	if kind == "TCP" {
		return &tcpTransport{conn: &transportTestConn{input: bytes.NewReader(response)}}
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	clientRaw, serverRaw := net.Pipe()
	client := tls.Client(clientRaw, &tls.Config{InsecureSkipVerify: true}) // Test-only self-signed certificate.
	server := tls.Server(serverRaw, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{certificate}, PrivateKey: privateKey}}})
	serverDone := make(chan error, 1)
	go func() {
		defer serverRaw.Close()
		_ = serverRaw.SetDeadline(time.Now().Add(3 * time.Second))
		if _, _, err := mbap.ReadFrame(server); err != nil {
			serverDone <- err
			return
		}
		if _, err := server.Write(response); err != nil {
			serverDone <- err
			return
		}
		for i := 1; i < requestCount; i++ {
			if _, _, err := mbap.ReadFrame(server); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()
	t.Cleanup(func() {
		clientRaw.Close()
		if err := <-serverDone; err != nil {
			t.Errorf("TLS fixture: %v", err)
		}
	})
	return &tlsTransport{conn: client}
}

type transportTestConn struct {
	input         *bytes.Reader
	chunkSize     int
	deadline      time.Time
	readDeadlines []time.Time
}

func (c *transportTestConn) Read(p []byte) (int, error) {
	if c.chunkSize > 0 && len(p) > c.chunkSize {
		p = p[:c.chunkSize]
	}
	return c.input.Read(p)
}
func (c *transportTestConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *transportTestConn) Close() error                { return nil }
func (c *transportTestConn) LocalAddr() net.Addr         { return nil }
func (c *transportTestConn) RemoteAddr() net.Addr        { return nil }
func (c *transportTestConn) SetDeadline(t time.Time) error {
	c.deadline = t
	return nil
}
func (c *transportTestConn) SetReadDeadline(t time.Time) error {
	c.readDeadlines = append(c.readDeadlines, t)
	return nil
}
func (c *transportTestConn) SetWriteDeadline(time.Time) error { return nil }
