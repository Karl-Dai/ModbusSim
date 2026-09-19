package frame

import (
	"bytes"
	"testing"
)

var testPDU = []byte{0x03, 0x00, 0x00, 0x00, 0x0A}

func TestEncodeRTUStructure(t *testing.T) {
	f := EncodeRTU(0x01, testPDU)
	if f[0] != 0x01 || !bytes.Equal(f[1:6], testPDU) || len(f) != 8 {
		t.Errorf("structure: % X", f)
	}
}

func TestEncodeRTUCRCValid(t *testing.T) {
	f := EncodeRTU(0x01, testPDU)
	if _, err := DecodeRTU(f); err != nil {
		t.Errorf("encoded frame should decode: %v", err)
	}
}

func TestDecodeRTURoundtrip(t *testing.T) {
	f, err := DecodeRTU(EncodeRTU(0x01, testPDU))
	if err != nil {
		t.Fatal(err)
	}
	if f.SlaveID != 0x01 || !bytes.Equal(f.PDU, testPDU) {
		t.Errorf("roundtrip: %+v", f)
	}
}

func TestDecodeRTUErrors(t *testing.T) {
	if _, err := DecodeRTU([]byte{0x01, 0x03}); err == nil {
		t.Error("too short: expected error")
	}
	bad := EncodeRTU(0x01, testPDU)
	bad[len(bad)-1] ^= 0xFF
	if _, err := DecodeRTU(bad); err == nil {
		t.Error("bad CRC: expected error")
	}
}

func TestEncodeASCIIStructure(t *testing.T) {
	f := EncodeASCII(0x01, testPDU)
	if f[0] != ':' || f[len(f)-2] != '\r' || f[len(f)-1] != '\n' {
		t.Errorf("structure: % X", f)
	}
}

func TestEncodeASCIIHexContent(t *testing.T) {
	// From Rust test: body must be "01030000000AF2"
	f := EncodeASCII(0x01, testPDU)
	body := string(f[1 : len(f)-2])
	if body != "01030000000AF2" {
		t.Errorf("body = %q, want 01030000000AF2", body)
	}
}

func TestDecodeASCIIRoundtrip(t *testing.T) {
	f, err := DecodeASCII(EncodeASCII(0x01, testPDU))
	if err != nil {
		t.Fatal(err)
	}
	if f.SlaveID != 0x01 || !bytes.Equal(f.PDU, testPDU) {
		t.Errorf("roundtrip: %+v", f)
	}
}

func TestDecodeASCIIBadLRC(t *testing.T) {
	f := EncodeASCII(0x01, testPDU)
	n := len(f)
	f[n-4] = '0'
	f[n-3] = '0'
	if _, err := DecodeASCII(f); err == nil {
		t.Error("bad LRC: expected error")
	}
}
