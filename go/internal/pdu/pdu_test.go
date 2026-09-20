package pdu

import (
	"bytes"
	"testing"
)

func TestParseFC03(t *testing.T) {
	req, err := ParseRequest([]byte{0x03, 0x00, 0x6B, 0x00, 0x03})
	if err != nil {
		t.Fatal(err)
	}
	if req.Address != 0x6B || req.Quantity != 3 {
		t.Errorf("got addr=%d qty=%d, want 0x6B/3", req.Address, req.Quantity)
	}
}

func TestParseFC05(t *testing.T) {
	req, err := ParseRequest([]byte{0x05, 0x00, 0x0A, 0xFF, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	if req.Address != 10 || !req.CoilOn {
		t.Errorf("on: got addr=%d on=%v", req.Address, req.CoilOn)
	}
	req, err = ParseRequest([]byte{0x05, 0x00, 0x0A, 0x00, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	if req.Address != 10 || req.CoilOn {
		t.Errorf("off: got addr=%d on=%v", req.Address, req.CoilOn)
	}
	// invalid coil value -> exception 0x03
	_, err = ParseRequest([]byte{0x05, 0x00, 0x00, 0x12, 0x34})
	if pe, ok := err.(*ParseError); !ok || pe.ExceptionCode() != 0x03 {
		t.Errorf("invalid coil: %v", err)
	}
}

func TestParseFC10(t *testing.T) {
	req, err := ParseRequest([]byte{0x10, 0x00, 0x01, 0x00, 0x02, 0x04, 0x00, 0x0A, 0x01, 0x02})
	if err != nil {
		t.Fatal(err)
	}
	if req.Address != 1 || !equalRegs(req.Registers, []uint16{0x000A, 0x0102}) {
		t.Errorf("got addr=%d regs=%v", req.Address, req.Registers)
	}
}

func equalRegs(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseErrors(t *testing.T) {
	if _, err := ParseRequest(nil); err == nil {
		t.Error("empty PDU: expected error")
	}
	if _, err := ParseRequest([]byte{0x2B, 0x00}); err == nil {
		t.Error("unsupported FC: expected error")
	} else if pe := err.(*ParseError); pe.ExceptionCode() != 0x01 {
		t.Errorf("unsupported FC exception = %02X, want 01", pe.ExceptionCode())
	}
	// Malformed supported functions -> exception 0x03
	for name, pdu := range map[string][]byte{
		"extra byte":     {0x03, 0, 0, 0, 1, 0},
		"invalid coil":   {0x05, 0, 0, 0x12, 0x34},
		"bad byte count": {0x0F, 0, 0, 0, 9, 1, 0},
	} {
		_, err := ParseRequest(pdu)
		if pe, ok := err.(*ParseError); !ok || pe.ExceptionCode() != 0x03 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestBuildResponseReadRegisters(t *testing.T) {
	got := BuildResponse(0x03, &ResponseData{Registers: []uint16{1, 2, 3}})
	want := []byte{0x03, 0x06, 0x00, 0x01, 0x00, 0x02, 0x00, 0x03}
	if !bytes.Equal(got, want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

func TestBuildResponseReadBits(t *testing.T) {
	// T,F,T,T,F,F,F,F,T -> bytes 0x0D, 0x01 (from Rust test)
	got := BuildResponse(0x01, &ResponseData{Bits: []bool{true, false, true, true, false, false, false, false, true}})
	want := []byte{0x01, 2, 0x0D, 0x01}
	if !bytes.Equal(got, want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

func TestBuildException(t *testing.T) {
	got := BuildException(0x03, 0x02)
	if !bytes.Equal(got, []byte{0x83, 0x02}) {
		t.Errorf("got % X", got)
	}
}

func TestBuildResponseWriteSingleCoil(t *testing.T) {
	got := BuildResponse(0x05, &ResponseData{WriteAddress: 10, WriteCoilOn: true})
	want := []byte{0x05, 0x00, 0x0A, 0xFF, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

func TestBuildResponseWriteMultiple(t *testing.T) {
	got := BuildResponse(0x10, &ResponseData{WriteAddress: 1, WriteQuantity: 10})
	want := []byte{0x10, 0x00, 0x01, 0x00, 0x0A}
	if !bytes.Equal(got, want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

func TestIsWrite(t *testing.T) {
	for _, fc := range []uint8{0x05, 0x06, 0x0F, 0x10} {
		if !(&Request{FunctionCode: fc}).IsWrite() {
			t.Errorf("FC%02X should be write", fc)
		}
	}
	for _, fc := range []uint8{0x01, 0x02, 0x03, 0x04} {
		if (&Request{FunctionCode: fc}).IsWrite() {
			t.Errorf("FC%02X should not be write", fc)
		}
	}
}
