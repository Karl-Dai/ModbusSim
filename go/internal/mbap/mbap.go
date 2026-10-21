// Package mbap implements the MBAP (Modbus Application Protocol) frame
// encoding/decoding used by Modbus TCP and Modbus TCP+TLS.
// Ported from crates/modbussim-core/src/mbap.rs with identical semantics.
package mbap

import (
	"encoding/binary"
	"fmt"
	"io"
)

// HeaderLen is the 7-byte MBAP header length.
const HeaderLen = 7

// MaxPDULen is the maximum Modbus PDU length (function code + data).
const MaxPDULen = 253

// Header is the 7-byte MBAP header.
type Header struct {
	TransactionID uint16
	ProtocolID    uint16
	Length        uint16 // = unit_id byte + PDU length
	UnitID        uint8
}

// NewHeader builds a header for a PDU of pduLen bytes.
func NewHeader(transactionID uint16, unitID uint8, pduLen int) Header {
	return Header{
		TransactionID: transactionID,
		ProtocolID:    0,
		Length:        uint16(pduLen + 1),
		UnitID:        unitID,
	}
}

// PDULen returns the PDU length implied by the header.
func (h Header) PDULen() int {
	if h.Length > 0 {
		return int(h.Length - 1)
	}
	return 0
}

// Encode serializes the header into a 7-byte array.
func (h Header) Encode() [HeaderLen]byte {
	var buf [HeaderLen]byte
	binary.BigEndian.PutUint16(buf[0:2], h.TransactionID)
	binary.BigEndian.PutUint16(buf[2:4], h.ProtocolID)
	binary.BigEndian.PutUint16(buf[4:6], h.Length)
	buf[6] = h.UnitID
	return buf
}

// DecodeHeader parses a 7-byte MBAP header.
func DecodeHeader(buf []byte) Header {
	return Header{
		TransactionID: binary.BigEndian.Uint16(buf[0:2]),
		ProtocolID:    binary.BigEndian.Uint16(buf[2:4]),
		Length:        binary.BigEndian.Uint16(buf[4:6]),
		UnitID:        buf[6],
	}
}

// ReadFrame reads one MBAP frame (header + PDU) from r.
func ReadFrame(r io.Reader) (Header, []byte, error) {
	var hdrBuf [HeaderLen]byte
	if _, err := io.ReadFull(r, hdrBuf[:]); err != nil {
		return Header{}, nil, err
	}
	header := DecodeHeader(hdrBuf[:])

	pduLen := header.PDULen()
	if pduLen == 0 || pduLen > MaxPDULen {
		return Header{}, nil, fmt.Errorf("invalid MBAP PDU length: %d", pduLen)
	}
	pdu := make([]byte, pduLen)
	if _, err := io.ReadFull(r, pdu); err != nil {
		return Header{}, nil, err
	}
	return header, pdu, nil
}

// WriteFrame writes one MBAP frame (header + PDU) to w.
func WriteFrame(w io.Writer, transactionID uint16, unitID uint8, pdu []byte) error {
	header := NewHeader(transactionID, unitID, len(pdu))
	encoded := header.Encode()
	if _, err := w.Write(encoded[:]); err != nil {
		return err
	}
	if _, err := w.Write(pdu); err != nil {
		return err
	}
	if f, ok := w.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}
