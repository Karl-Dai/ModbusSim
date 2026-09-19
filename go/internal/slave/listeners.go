// Listener implementations for the five slave transports.
package slave

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/frame"
	"github.com/Karl-Dai/ModbusSim/go/internal/mbap"
	"github.com/Karl-Dai/ModbusSim/go/internal/pdu"
)

// Listener is a running slave server. Call Stop to shut it down.
type Listener struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Stop shuts the listener down and waits for it to exit. Idempotent.
func (l *Listener) Stop() {
	if l == nil {
		return
	}
	l.cancel()
	<-l.done
}

// RunTCP runs a Modbus TCP (MBAP) slave on ln (already bound). Blocks until
// ctx is cancelled or ln fails. Each client connection is handled in its own
// goroutine.
func RunTCP(ctx context.Context, ln net.Listener, srv *Server) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	l := &Listener{cancel: cancel, done: make(chan struct{})}
	defer close(l.done)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			return fmt.Errorf("accept error: %w", err)
		}
		go func() {
			defer conn.Close()
			handleMBAPClient(ctx, conn, srv)
		}()
	}
}

// RunTCPServer binds host:port and runs RunTCP on it.
func RunTCPServer(ctx context.Context, host string, port uint16, srv *Server) (net.Addr, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return nil, err
	}
	go func() {
		_ = RunTCP(ctx, ln, srv)
	}()
	return ln.Addr(), nil
}

// handleMBAPClient serves one client connection with MBAP framing,
// mirroring tokio_modbus's server loop as used by SlaveService: unknown
// slave IDs get no response; parse errors produce exception responses.
func handleMBAPClient(ctx context.Context, conn net.Conn, srv *Server) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Idle deadline: 60 seconds without a request disconnects the client.
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		header, reqPDU, err := mbap.ReadFrame(reader)
		if err != nil {
			return // EOF, timeout, or malformed frame: drop connection
		}

		req, parseErr := pdu.ParseRequest(reqPDU)
		if parseErr != nil {
			req = &pdu.Request{FunctionCode: firstByte(reqPDU)}
		} else {
			srv.logOneInbound(req)
		}

		respPDU := srv.ProcessRequest(header.UnitID, reqPDU)
		if respPDU == nil {
			continue // unknown slave ID: silent drop
		}
		if parseErr == nil {
			srv.LogResponse(req, respPDU)
		}

		_ = conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
		if err := mbap.WriteFrame(conn, header.TransactionID, header.UnitID, respPDU); err != nil {
			return
		}
	}
}

func firstByte(b []byte) uint8 {
	if len(b) > 0 {
		return b[0]
	}
	return 0
}

func (s *Server) logOneInbound(req *pdu.Request) {
	if s.Log == nil {
		return
	}
	if _, ok := parseOK(req); ok {
		s.LogRx(req)
	}
}

func parseOK(req *pdu.Request) (*pdu.Request, bool) {
	switch req.FunctionCode {
	case pdu.FCReadCoils, pdu.FCReadDiscreteInputs, pdu.FCReadHoldingRegisters, pdu.FCReadInputRegisters,
		pdu.FCWriteSingleCoil, pdu.FCWriteSingleRegister, pdu.FCWriteMultipleCoils, pdu.FCWriteMultipleRegisters:
		return req, true
	}
	return nil, false
}

// RunTLS runs a Modbus TCP+TLS slave. tlsConfig must already be built (see
// tls.go BuildTLSConfig). Framing is identical MBAP over the TLS stream.
func RunTLS(ctx context.Context, ln net.Listener, tlsConfig *tls.Config, srv *Server) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	l := &Listener{cancel: cancel, done: make(chan struct{})}
	defer close(l.done)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			return fmt.Errorf("TLS accept error: %w", err)
		}
		tlsConn := tls.Server(conn, tlsConfig)
		go func() {
			defer tlsConn.Close()
			// Handshake with a deadline so a bad client cannot hang a goroutine.
			_ = tlsConn.SetDeadline(time.Now().Add(10 * time.Second))
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				warn("TLS handshake failed: %v", err)
				return
			}
			_ = tlsConn.SetDeadline(time.Time{})
			handleMBAPClient(ctx, tlsConn, srv)
		}()
	}
}

// RunRTUOverTCP runs a slave where frames use RTU format (slave_id + PDU +
// CRC, no MBAP header) over TCP. A 60-second idle timeout disconnects
// clients. Mirrors rtu_tcp_slave.rs including the >256-byte discard rule.
func RunRTUOverTCP(ctx context.Context, ln net.Listener, srv *Server) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	l := &Listener{cancel: cancel, done: make(chan struct{})}
	defer close(l.done)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			return fmt.Errorf("RTU-over-TCP accept error: %w", err)
		}
		go func() {
			defer conn.Close()
			handleRTUOverTCPClient(ctx, conn, srv)
		}()
	}
}

func handleRTUOverTCPClient(ctx context.Context, conn net.Conn, srv *Server) {
	reader := bufio.NewReader(conn)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		// Read at least one full RTU frame: decode attempts start at 4 bytes.
		frameBuf, err := readUntilRTUFrame(reader)
		if err != nil {
			return // EOF, timeout, or buffer overflow
		}

		respFrame, err := decodeAndProcessRTU(srv, frameBuf)
		if err != nil {
			continue // decode error: skip (mirrors Rust's continue)
		}
		if respFrame == nil {
			continue // unknown slave: silent drop
		}
		_ = conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
		if _, err := conn.Write(respFrame); err != nil {
			return
		}
	}
}

// readUntilRTUFrame accumulates bytes until they decode as one RTU frame,
// mirroring rtu_tcp_slave.rs's loop (>= 4 bytes, try decode, discard on >256).
func readUntilRTUFrame(reader *bufio.Reader) ([]byte, error) {
	var frameBuf []byte
	single := make([]byte, 1)
	for {
		if len(frameBuf) >= 4 {
			if _, err := DecodeRTUFrame(frameBuf); err == nil {
				return frameBuf, nil
			}
			if len(frameBuf) > 256 {
				return nil, fmt.Errorf("RTU-over-TCP frame buffer overflow, discarding")
			}
		}
		if _, err := reader.Read(single); err != nil {
			return nil, err
		}
		frameBuf = append(frameBuf, single[0])
	}
}

// RTUFrame is the decoded wire frame (slave_id + PDU + CRC16).
type RTUWireFrame struct {
	SlaveID uint8
	PDU     []byte
}

// DecodeRTUFrame decodes a full RTU wire frame.
func DecodeRTUFrame(data []byte) (*RTUWireFrame, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("RTU frame too short: %d bytes (minimum 4)", len(data))
	}
	f, err := frame.DecodeRTU(data)
	if err != nil {
		return nil, err
	}
	return &RTUWireFrame{SlaveID: f.SlaveID, PDU: f.PDU}, nil
}

// EncodeRTUFrame encodes slave_id + PDU + CRC.
func EncodeRTUFrame(slaveID uint8, pduBytes []byte) []byte {
	return frame.EncodeRTU(slaveID, pduBytes)
}

// decodeAndProcessRTU decodes frameBuf as an RTU frame, runs the request
// through the server with RX/TX logging, and returns the encoded response
// frame. Returns (nil, nil) when the slave ID is unknown (silent drop).
func decodeAndProcessRTU(srv *Server, frameBuf []byte) ([]byte, error) {
	f, err := frame.DecodeRTU(frameBuf)
	if err != nil {
		return nil, err
	}
	if req, err := pdu.ParseRequest(f.PDU); err == nil {
		srv.LogRx(req)
	}
	respPDU := srv.ProcessRequest(f.SlaveID, f.PDU)
	if respPDU == nil {
		return nil, nil
	}
	if req, err := pdu.ParseRequest(f.PDU); err == nil {
		srv.LogResponse(req, respPDU)
	}
	return frame.EncodeRTU(f.SlaveID, respPDU), nil
}

// discardAll drains r until EOF (used on unrecoverable errors).
func discardAll(r io.Reader) {
	_, _ = io.Copy(io.Discard, r)
}
