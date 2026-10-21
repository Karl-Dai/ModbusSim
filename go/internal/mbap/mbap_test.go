package mbap

import (
	"bytes"
	"testing"
)

func TestHeaderRoundtrip(t *testing.T) {
	h := NewHeader(0x0001, 1, 5)
	encoded := h.Encode()
	decoded := DecodeHeader(encoded[:])
	if h != decoded {
		t.Errorf("roundtrip: %+v != %+v", h, decoded)
	}
}

func TestHeaderFields(t *testing.T) {
	h := NewHeader(0x1234, 7, 5)
	if h.TransactionID != 0x1234 || h.ProtocolID != 0 || h.Length != 6 || h.UnitID != 7 || h.PDULen() != 5 {
		t.Errorf("fields: %+v", h)
	}
}

func TestHeaderEncodeBytes(t *testing.T) {
	h := Header{TransactionID: 0x0001, ProtocolID: 0x0000, Length: 0x0006, UnitID: 0x01}
	got := h.Encode()
	want := []byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x01}
	if !bytes.Equal(got[:], want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

func TestReadWriteFrameRoundtrip(t *testing.T) {
	pdu := []byte{0x03, 0x00, 0x00, 0x00, 0x0A}
	var buf bytes.Buffer
	if err := WriteFrame(&buf, 1, 1, pdu); err != nil {
		t.Fatal(err)
	}
	header, readPDU, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if header.TransactionID != 1 || header.UnitID != 1 || !bytes.Equal(readPDU, pdu) {
		t.Errorf("roundtrip: %+v % X", header, readPDU)
	}
}

func TestReadFrameInvalidLength(t *testing.T) {
	buf := []byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x01}
	_, _, err := ReadFrame(bytes.NewReader(buf))
	if err == nil {
		t.Error("invalid length: expected error")
	}
}
