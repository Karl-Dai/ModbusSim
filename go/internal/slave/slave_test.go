package slave

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/mbap"
	"github.com/Karl-Dai/ModbusSim/go/internal/pdu"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	srv := NewServer()
	dev := WithDefaultRegisters(1, "dev", 99)
	if err := srv.AddDevice(dev); err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestAddDuplicateDevice(t *testing.T) {
	srv := testServer(t)
	if err := srv.AddDevice(WithDefaultRegisters(1, "dup", 0)); err == nil {
		t.Error("duplicate ID: expected error")
	}
	if err := srv.RemoveDevice(2); err == nil {
		t.Error("missing ID: expected error")
	}
}

func TestProcessRequestRead(t *testing.T) {
	srv := testServer(t)
	dev, _ := srv.GetDevice(1)
	dev.RegisterMap.WriteHoldingRegisters(0, []uint16{0x1111, 0x2222, 0x3333})

	resp := srv.ProcessRequest(1, []byte{0x03, 0x00, 0x00, 0x00, 0x03})
	want := []byte{0x03, 0x06, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33}
	if fmt.Sprintf("% X", resp) != fmt.Sprintf("% X", want) {
		t.Errorf("read resp = % X, want % X", resp, want)
	}
}

func TestProcessRequestUnknownSlaveSilentDrop(t *testing.T) {
	srv := testServer(t)
	if resp := srv.ProcessRequest(99, []byte{0x03, 0x00, 0x00, 0x00, 0x01}); resp != nil {
		t.Errorf("unknown slave should drop, got % X", resp)
	}
}

func TestProcessRequestException(t *testing.T) {
	srv := testServer(t)
	// FC03 quantity 0 -> IllegalDataValue
	resp := srv.ProcessRequest(1, []byte{0x03, 0x00, 0x00, 0x00, 0x00})
	if len(resp) != 2 || resp[0] != 0x83 || resp[1] != ExcIllegalDataValue {
		t.Errorf("qty=0 resp = % X", resp)
	}
	// FC03 quantity 126 > 125 -> IllegalDataValue
	resp = srv.ProcessRequest(1, []byte{0x03, 0x00, 0x00, 0x00, 126})
	if resp[1] != ExcIllegalDataValue {
		t.Errorf("qty=126 resp = % X", resp)
	}
	// Read unregistered address -> IllegalDataAddress
	resp = srv.ProcessRequest(1, []byte{0x03, 0x7F, 0xFF, 0x00, 0x01})
	if resp[1] != ExcIllegalDataAddress {
		t.Errorf("unregistered resp = % X", resp)
	}
}

func TestProcessRequestWriteAndCallback(t *testing.T) {
	var got []Change
	srv := testServer(t)
	srv.ChangeCallback = func(changes []Change) { got = changes }

	resp := srv.ProcessRequest(1, []byte{0x06, 0x00, 0x05, 0xAB, 0xCD})
	if len(resp) != 5 || resp[0] != 0x06 {
		t.Errorf("write resp = % X", resp)
	}
	dev, _ := srv.GetDevice(1)
	if v := dev.RegisterMap.HoldingRegisters[5]; v != 0xABCD {
		t.Errorf("stored = %04X", v)
	}
	if len(got) != 1 || got[0].Address != 5 || got[0].Value != 0xABCD || got[0].RegisterType != register.HoldingRegType {
		t.Errorf("changes = %+v", got)
	}
}

func TestTCPEndToEnd(t *testing.T) {
	srv := testServer(t)
	dev, _ := srv.GetDevice(1)
	dev.RegisterMap.WriteHoldingRegisters(10, []uint16{0xBEEF})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = RunTCP(ctx, ln, srv) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	// FC03: read addr 10 x1, transaction id 7
	if err := mbap.WriteFrame(conn, 7, 1, []byte{0x03, 0x00, 0x0A, 0x00, 0x01}); err != nil {
		t.Fatal(err)
	}
	header, resp, err := mbap.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if header.TransactionID != 7 || header.UnitID != 1 {
		t.Errorf("header = %+v", header)
	}
	want := []byte{0x03, 0x02, 0xBE, 0xEF}
	if fmt.Sprintf("% X", resp) != fmt.Sprintf("% X", want) {
		t.Errorf("resp = % X, want % X", resp, want)
	}

	// Write then read back (FC06).
	if err := mbap.WriteFrame(conn, 8, 1, []byte{0x06, 0x00, 0x0A, 0x12, 0x34}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mbap.ReadFrame(conn); err != nil {
		t.Fatal(err)
	}
	if err := mbap.WriteFrame(conn, 9, 1, []byte{0x03, 0x00, 0x0A, 0x00, 0x01}); err != nil {
		t.Fatal(err)
	}
	_, resp2, err := mbap.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	want2 := []byte{0x03, 0x02, 0x12, 0x34}
	if fmt.Sprintf("% X", resp2) != fmt.Sprintf("% X", want2) {
		t.Errorf("resp2 = % X, want % X", resp2, want2)
	}
}

func TestRTUOverTCPEndToEnd(t *testing.T) {
	srv := testServer(t)
	dev, _ := srv.GetDevice(1)
	dev.RegisterMap.WriteHoldingRegisters(0, []uint16{42})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = RunRTUOverTCP(ctx, ln, srv) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	// RTU frame: slave 1, FC03 addr0 x1.
	req := EncodeRTUFrame(1, []byte{0x03, 0x00, 0x00, 0x00, 0x01})
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	f, err := DecodeRTUFrame(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if f.SlaveID != 1 || len(f.PDU) != 4 || f.PDU[0] != 0x03 || f.PDU[1] != 2 || f.PDU[2] != 0x00 || f.PDU[3] != 0x2A {
		t.Errorf("resp frame: slave=%d pdu=% X", f.SlaveID, f.PDU)
	}
}

func TestParseErrorBecomesException(t *testing.T) {
	srv := testServer(t)
	// FC0F with wrong byte count -> exception 0x03
	resp := srv.ProcessRequest(1, []byte{0x0F, 0x00, 0x00, 0x00, 9, 1, 0})
	if len(resp) != 2 || resp[0] != 0x8F || resp[1] != 0x03 {
		t.Errorf("resp = % X", resp)
	}
	// Unsupported FC -> exception 0x01
	resp = srv.ProcessRequest(1, []byte{0x2B, 0x00})
	if len(resp) != 2 || resp[0] != 0xAB || resp[1] != 0x01 {
		t.Errorf("resp = % X", resp)
	}
	_ = pdu.FCReadCoils
}
