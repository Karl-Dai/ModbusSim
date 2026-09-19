// Transport implementations for the master side.
package master

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"

	goserial "go.bug.st/serial"
	sslmatepkcs12 "software.sslmate.com/src/go-pkcs12"

	"github.com/Karl-Dai/ModbusSim/go/internal/frame"
	"github.com/Karl-Dai/ModbusSim/go/internal/mbap"
	"github.com/Karl-Dai/ModbusSim/go/internal/serial"
	"github.com/Karl-Dai/ModbusSim/go/internal/socks5"
)

// ---------------------------------------------------------------------------
// TCP (MBAP) transport
// ---------------------------------------------------------------------------

type tcpTransport struct {
	conn net.Conn
	tid  atomic.Uint32
}

func newTCPTransport(host string, port uint16, proxy socks5.Config, timeout time.Duration) (*tcpTransport, error) {
	conn, err := socks5.ConnectTCP(host, port, proxy, timeout)
	if err != nil {
		return nil, mapConnectError(err)
	}
	return &tcpTransport{conn: conn}, nil
}

func mapConnectError(err error) *Error {
	if ce, ok := err.(*socks5.ConnectError); ok {
		if ce.Timeout {
			return errTimeout(ce.Msg)
		}
		return errTransport(ce.Msg)
	}
	return errTransport(err.Error())
}

func (t *tcpTransport) exchange(slaveID uint8, reqPDU []byte, timeout time.Duration) ([]byte, error) {
	tid := uint16(t.tid.Add(1) & 0xFFFF)
	_ = t.conn.SetDeadline(time.Now().Add(timeout))
	if err := mbap.WriteFrame(t.conn, tid, slaveID, reqPDU); err != nil {
		return nil, errTransport(fmt.Sprintf("TCP write: %v", err))
	}
	_, resp, err := mbap.ReadFrame(bufio.NewReader(t.conn))
	if err != nil {
		return nil, errTransport(fmt.Sprintf("TCP read: %v", err))
	}
	return resp, nil
}

func (t *tcpTransport) close() error { return t.conn.Close() }

// ---------------------------------------------------------------------------
// TLS (MBAP over TLS) transport
// ---------------------------------------------------------------------------

type tlsTransport struct {
	conn *tls.Conn
	tid  atomic.Uint32
}

// BuildClientTLSConfig mirrors tls_master.rs's connect_tls config build:
// optional CA, optional client identity (PKCS#12 or PEM), optional skip-verify.
// serverName is the verification hostname (the dial host).
func BuildClientTLSConfig(cfg TLSConfig, serverName string) (*tls.Config, error) {
	out := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.AcceptInvalidCerts {
		out.InsecureSkipVerify = true
	} else {
		out.ServerName = serverName
	}
	if cfg.CAFile != "" {
		caBytes, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, &Error{Kind: "certificate", Msg: fmt.Sprintf("Failed to read CA file '%s': %v", cfg.CAFile, err)}
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, &Error{Kind: "certificate", Msg: "CA file contains no certificates"}
		}
		out.RootCAs = pool
	}
	if cfg.PKCS12File != "" {
		p12Bytes, err := os.ReadFile(cfg.PKCS12File)
		if err != nil {
			return nil, &Error{Kind: "certificate", Msg: fmt.Sprintf("Failed to read PKCS#12 file '%s': %v", cfg.PKCS12File, err)}
		}
		cert, err := sslmateDecodeChain(p12Bytes, cfg.PKCS12Password)
		if err != nil {
			return nil, &Error{Kind: "certificate", Msg: fmt.Sprintf("Failed to parse PKCS#12: %v", err)}
		}
		out.Certificates = []tls.Certificate{cert}
	} else if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, &Error{Kind: "certificate", Msg: fmt.Sprintf("Failed to load client certificate: %v", err)}
		}
		out.Certificates = []tls.Certificate{cert}
	}
	return out, nil
}

func newTLSTransport(ctx context.Context, host string, port uint16, cfg TLSConfig, proxy socks5.Config, timeout time.Duration) (*tlsTransport, error) {
	tlsCfg, err := BuildClientTLSConfig(cfg, host)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	raw, err := socks5.ConnectTCP(host, port, proxy, timeout)
	if err != nil {
		return nil, mapConnectError(err)
	}
	conn := tls.Client(raw, tlsCfg)
	// Apply an overall handshake deadline.
	handshakeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := conn.HandshakeContext(handshakeCtx); err != nil {
		raw.Close()
		return nil, errTransport(fmt.Sprintf("TLS handshake: %v", err))
	}
	return &tlsTransport{conn: conn}, nil
}

func (t *tlsTransport) exchange(slaveID uint8, reqPDU []byte, timeout time.Duration) ([]byte, error) {
	tid := uint16(t.tid.Add(1) & 0xFFFF)
	_ = t.conn.SetDeadline(time.Now().Add(timeout))
	if err := mbap.WriteFrame(t.conn, tid, slaveID, reqPDU); err != nil {
		return nil, errTransport(fmt.Sprintf("TLS write: %v", err))
	}
	_, resp, err := mbap.ReadFrame(bufio.NewReader(t.conn))
	if err != nil {
		return nil, errTransport(fmt.Sprintf("TLS read: %v", err))
	}
	return resp, nil
}

func (t *tlsTransport) close() error { return t.conn.Close() }

// ---------------------------------------------------------------------------
// Serial transports (RTU and ASCII) — shared framing loop
// ---------------------------------------------------------------------------

type serialTransport struct {
	port           goserial.Port
	interframe     time.Duration
	ascii          bool
}

func newSerialTransport(cfg *serial.Config, ascii bool) (*serialTransport, error) {
	if cfg == nil {
		return nil, errTransport("serial configuration required")
	}
	port, err := goserial.Open(cfg.Port, &goserial.Mode{
		BaudRate: int(cfg.BaudRate),
		DataBits: serialDataBits(cfg.DataBits),
		Parity:   serialParity(cfg.Parity),
		StopBits: serialStopBits(cfg.StopBits),
	})
	if err != nil {
		return nil, errTransport(fmt.Sprintf("Failed to open serial port %s: %v", cfg.Port, err))
	}
	return &serialTransport{
		port:     port,
		interframe: serial.InterframeDelay(cfg.BaudRate),
		ascii:    ascii,
	}, nil
}

func serialParity(p serial.Parity) goserial.Parity {
	switch p {
	case serial.ParityOdd:
		return goserial.OddParity
	case serial.ParityEven:
		return goserial.EvenParity
	default:
		return goserial.NoParity
	}
}

func serialDataBits(d uint8) int {
	switch d {
	case 5, 6, 7:
		return int(d)
	default:
		return 8
	}
}

func serialStopBits(s uint8) goserial.StopBits {
	if s == 2 {
		return goserial.TwoStopBits
	}
	return goserial.OneStopBit
}

func (t *serialTransport) writeFrame(slaveID uint8, pduBytes []byte) error {
	var frameBytes []byte
	if t.ascii {
		frameBytes = encodeASCIIFrame(slaveID, pduBytes)
	} else {
		frameBytes = encodeRTUFrame(slaveID, pduBytes)
	}
	_, err := t.port.Write(frameBytes)
	return err
}

// readFrame reads one response frame with an overall deadline, mirroring
// rtu_master.rs / ascii_master.rs: accumulate until the frame validates.
func (t *serialTransport) readFrame(slaveID uint8, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	var acc []byte
	buf := make([]byte, 512)

	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			if len(acc) == 0 {
				return nil, fmt.Errorf("response timeout")
			}
			break
		}
		_ = t.port.SetReadTimeout(remain)
		n, err := t.port.Read(buf)
		if err != nil {
			return nil, errTransport(fmt.Sprintf("read error: %v", err))
		}
		if n > 0 {
			acc = append(acc, buf[:n]...)
			if t.ascii {
				// Complete when the accumulated bytes end with CRLF.
				if len(acc) >= 2 && acc[len(acc)-2] == '\r' && acc[len(acc)-1] == '\n' {
					break
				}
			} else if len(acc) >= 4 {
				if _, err := decodeRTUFrame(acc); err == nil {
					break
				}
			}
		}
		if n == 0 && len(acc) > 0 {
			break
		}
		if n == 0 && time.Until(deadline) <= 0 {
			break
		}
	}

	if len(acc) == 0 {
		return nil, fmt.Errorf("response timeout")
	}

	if t.ascii {
		af, err := decodeASCIIFrame(acc)
		if err != nil {
			return nil, err
		}
		if af.SlaveID != slaveID {
			return nil, fmt.Errorf("slave_id mismatch: expected %d, got %d", slaveID, af.SlaveID)
		}
		return af.PDU, nil
	}
	f, err := decodeRTUFrame(acc)
	if err != nil {
		return nil, err
	}
	if f.SlaveID != slaveID {
		return nil, fmt.Errorf("slave_id mismatch: expected %d, got %d", slaveID, f.SlaveID)
	}
	return f.PDU, nil
}

func (t *serialTransport) exchange(slaveID uint8, reqPDU []byte, timeout time.Duration) ([]byte, error) {
	if err := t.writeFrame(slaveID, reqPDU); err != nil {
		return nil, errTransport(fmt.Sprintf("write error: %v", err))
	}
	if !t.ascii {
		// Wait interframe delay before listening for a response (rtu_master.rs).
		time.Sleep(t.interframe)
	}
	return t.readFrame(slaveID, timeout)
}

func (t *serialTransport) close() error { return t.port.Close() }

// ---------------------------------------------------------------------------
// RTU-over-TCP transport
// ---------------------------------------------------------------------------

type rtuTCPTransport struct {
	conn net.Conn
}

func newRTUTCPTransport(host string, port uint16, proxy socks5.Config, timeout time.Duration) (*rtuTCPTransport, error) {
	conn, err := socks5.ConnectTCP(host, port, proxy, timeout)
	if err != nil {
		return nil, mapConnectError(err)
	}
	return &rtuTCPTransport{conn: conn}, nil
}

func (t *rtuTCPTransport) exchange(slaveID uint8, reqPDU []byte, timeout time.Duration) ([]byte, error) {
	req := encodeRTUFrame(slaveID, reqPDU)
	_ = t.conn.SetDeadline(time.Now().Add(timeout))
	if _, err := t.conn.Write(req); err != nil {
		return nil, errTransport(fmt.Sprintf("write error: %v", err))
	}

	// Accumulate until the response CRC validates (rtu_tcp_master.rs).
	var acc []byte
	buf := make([]byte, 512)
	for {
		if err := t.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return nil, errTransport(err.Error())
		}
		n, err := t.conn.Read(buf)
		if n > 0 {
			acc = append(acc, buf[:n]...)
			if len(acc) >= 4 {
				if _, derr := decodeRTUFrame(acc); derr == nil {
					break
				}
			}
		}
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() && len(acc) > 0 {
				break
			}
			return nil, errTransport(fmt.Sprintf("read error: %v", err))
		}
		if n == 0 {
			break
		}
	}

	f, err := decodeRTUFrame(acc)
	if err != nil {
		return nil, errTransport(err.Error())
	}
	if f.SlaveID != slaveID {
		return nil, errTransport(fmt.Sprintf("slave_id mismatch: expected %d, got %d", slaveID, f.SlaveID))
	}
	return f.PDU, nil
}

func (t *rtuTCPTransport) close() error { return t.conn.Close() }

// ---------------------------------------------------------------------------
// Frame helpers over internal/frame (same encode/decode the slave uses)
// ---------------------------------------------------------------------------

type rtuFrameT struct {
	SlaveID uint8
	PDU     []byte
}

func decodeRTUFrame(data []byte) (*rtuFrameT, error) {
	f, err := frame.DecodeRTU(data)
	if err != nil {
		return nil, err
	}
	return &rtuFrameT{SlaveID: f.SlaveID, PDU: f.PDU}, nil
}

func encodeRTUFrame(slaveID uint8, pduBytes []byte) []byte {
	return frame.EncodeRTU(slaveID, pduBytes)
}

type asciiFrameT struct {
	SlaveID uint8
	PDU     []byte
}

func decodeASCIIFrame(data []byte) (*asciiFrameT, error) {
	f, err := frame.DecodeASCII(data)
	if err != nil {
		return nil, err
	}
	return &asciiFrameT{SlaveID: f.SlaveID, PDU: f.PDU}, nil
}

func encodeASCIIFrame(slaveID uint8, pduBytes []byte) []byte {
	return frame.EncodeASCII(slaveID, pduBytes)
}

// sslmateDecodeChain converts a PKCS#12 blob to a tls.Certificate.
func sslmateDecodeChain(p12Bytes []byte, password string) (tls.Certificate, error) {
	privKey, leaf, caCerts, err := sslmatepkcs12.DecodeChain(p12Bytes, password)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert := tls.Certificate{PrivateKey: privKey, Leaf: leaf}
	if leaf != nil {
		cert.Certificate = append(cert.Certificate, leaf.Raw)
	}
	for _, ca := range caCerts {
		cert.Certificate = append(cert.Certificate, ca.Raw)
	}
	return cert, nil
}

var _ = binary.BigEndian.Uint16
var _ = io.Discard
