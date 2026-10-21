// Serial-port slave listeners: RTU (interframe-silence delimited) and ASCII
// (':' ... CRLF framed). Ported from crates/modbussim-core/src/rtu_slave.rs
// and ascii_slave.rs. Uses go.bug.st/serial (pure Go, no CGO for I/O).
//
// Note: go.bug.st/serial returns (0, nil) on read-timeout, which this file
// treats as "deadline elapsed".
package slave

import (
	"context"
	"fmt"
	"time"

	goserial "go.bug.st/serial"

	"github.com/Karl-Dai/ModbusSim/go/internal/frame"
	"github.com/Karl-Dai/ModbusSim/go/internal/pdu"
	"github.com/Karl-Dai/ModbusSim/go/internal/serial"
)

func parityMode(p serial.Parity) goserial.Parity {
	switch p {
	case serial.ParityOdd:
		return goserial.OddParity
	case serial.ParityEven:
		return goserial.EvenParity
	default:
		return goserial.NoParity
	}
}

func dataBits(d uint8) int {
	switch d {
	case 5, 6, 7:
		return int(d)
	default:
		return 8
	}
}

func stopBits(s uint8) goserial.StopBits {
	if s == 2 {
		return goserial.TwoStopBits
	}
	return goserial.OneStopBit
}

func openPort(cfg serial.Config) (goserial.Port, error) {
	port, err := goserial.Open(cfg.Port, &goserial.Mode{
		BaudRate: int(cfg.BaudRate),
		DataBits: dataBits(cfg.DataBits),
		Parity:   parityMode(cfg.Parity),
		StopBits: stopBits(cfg.StopBits),
	})
	if err != nil {
		return nil, fmt.Errorf("Failed to open serial port %s: %w", cfg.Port, err)
	}
	return port, nil
}

// readPort reads with a deadline; n==0, err==nil means the deadline elapsed
// (go.bug.st/serial timeout semantics).
func readPort(port goserial.Port, buf []byte, timeout time.Duration) (int, error) {
	if err := port.SetReadTimeout(timeout); err != nil {
		return 0, err
	}
	return port.Read(buf)
}

// RunRTUSerial runs an RTU slave on the given serial port. Blocks until ctx
// is cancelled or an unrecoverable I/O error occurs. Frames are delimited by
// interframe silence (mirrors rtu_slave.rs).
func RunRTUSerial(ctx context.Context, cfg serial.Config, srv *Server) error {
	port, err := openPort(cfg)
	if err != nil {
		return err
	}
	defer port.Close()

	interframe := serial.InterframeDelay(cfg.BaudRate)
	buf := make([]byte, 256)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Short read so cancellation stays responsive.
		n, err := readPort(port, buf, 100*time.Millisecond)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			return fmt.Errorf("Serial port read error: %w", err)
		}
		if n == 0 {
			continue
		}
		frameBuf := append([]byte(nil), buf[:n]...)

		// Keep reading until interframe silence indicates end-of-frame.
		for {
			n, err := readPort(port, buf, interframe)
			if err != nil {
				return fmt.Errorf("Serial port read error: %w", err)
			}
			if n == 0 {
				break // interframe silence elapsed -- frame is complete
			}
			frameBuf = append(frameBuf, buf[:n]...)
		}

		if _, err := decodeAndProcessRTU(srv, frameBuf); err != nil {
			warn("RTU decode error: %v", err)
			continue
		}
	}
}

// RunASCIISerial runs an ASCII slave on the given serial port. Frames are
// ':' + hex + CRLF (mirrors ascii_slave.rs byte-at-a-time accumulation).
func RunASCIISerial(ctx context.Context, cfg serial.Config, srv *Server) error {
	port, err := openPort(cfg)
	if err != nil {
		return err
	}
	defer port.Close()

	var acc []byte
	single := make([]byte, 1)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, err := readPort(port, single, 100*time.Millisecond)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			return fmt.Errorf("Serial port read error: %w", err)
		}
		if n == 0 {
			continue
		}

		acc = append(acc, single[0])
		// A frame starts with ':' and ends with CRLF.
		if len(acc) >= 2 && acc[len(acc)-2] == '\r' && acc[len(acc)-1] == '\n' {
			frameBytes := acc
			acc = nil
			af, err := frame.DecodeASCII(frameBytes)
			if err != nil {
				warn("ASCII decode error: %v", err)
				continue
			}
			if req, perr := pdu.ParseRequest(af.PDU); perr == nil {
				srv.LogRx(req)
			}
			respPDU := srv.ProcessRequest(af.SlaveID, af.PDU)
			if respPDU == nil {
				continue // unknown slave ID: silent drop
			}
			if req, perr := pdu.ParseRequest(af.PDU); perr == nil {
				srv.LogResponse(req, respPDU)
			}
			respFrame := frame.EncodeASCII(af.SlaveID, respPDU)
			if _, werr := port.Write(respFrame); werr != nil {
				return fmt.Errorf("Serial port write error: %w", werr)
			}
		}
		// Guard against runaway accumulation on garbage input.
		if len(acc) > 1024 {
			acc = nil
		}
	}
}
