// Package frame implements Modbus RTU and ASCII frame encoding/decoding.
// Ported from crates/modbussim-core/src/frame.rs with identical semantics.
package frame

import (
	"fmt"

	"github.com/Karl-Dai/ModbusSim/go/internal/tools"
)

// RTUFrame is a decoded RTU frame.
type RTUFrame struct {
	SlaveID uint8
	PDU     []byte
}

// ASCIIFrame is a decoded ASCII frame.
type ASCIIFrame struct {
	SlaveID uint8
	PDU     []byte
}

func hexCharUpper(nibble byte) byte {
	if nibble < 10 {
		return '0' + nibble
	}
	return 'A' + nibble - 10
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

// EncodeRTU builds [slave_id | PDU | CRC16_lo | CRC16_hi].
func EncodeRTU(slaveID uint8, pdu []byte) []byte {
	raw := make([]byte, 0, 1+len(pdu))
	raw = append(raw, slaveID)
	raw = append(raw, pdu...)
	return tools.AppendCRC16(raw)
}

// DecodeRTU validates minimum length (>= 4 bytes) and CRC, then extracts
// slave_id and PDU.
func DecodeRTU(data []byte) (*RTUFrame, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("RTU frame too short: %d bytes (minimum 4)", len(data))
	}
	if !tools.VerifyCRC16(data) {
		return nil, fmt.Errorf("RTU frame CRC check failed")
	}
	return &RTUFrame{
		SlaveID: data[0],
		PDU:     data[1 : len(data)-2],
	}, nil
}

// EncodeASCII builds ':' + HEX(slave_id + PDU + LRC) + "\r\n" with uppercase hex.
func EncodeASCII(slaveID uint8, pdu []byte) []byte {
	raw := make([]byte, 0, 1+len(pdu))
	raw = append(raw, slaveID)
	raw = append(raw, pdu...)
	withLRC := tools.AppendLRC(raw)

	out := make([]byte, 0, 1+len(withLRC)*2+2)
	out = append(out, ':')
	for _, b := range withLRC {
		out = append(out, hexCharUpper(b>>4), hexCharUpper(b&0x0F))
	}
	out = append(out, '\r', '\n')
	return out
}

// DecodeASCII validates ':' prefix and CRLF suffix, hex-decodes the body,
// verifies LRC, then extracts slave_id and PDU.
func DecodeASCII(data []byte) (*ASCIIFrame, error) {
	if len(data) == 0 || data[0] != ':' {
		return nil, fmt.Errorf("ASCII frame missing ':' prefix")
	}
	if len(data) < 2 || data[len(data)-2] != '\r' || data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("ASCII frame missing CRLF suffix")
	}
	hexBytes := data[1 : len(data)-2]
	if len(hexBytes)%2 != 0 {
		return nil, fmt.Errorf("ASCII frame has odd number of hex characters")
	}
	if len(hexBytes) < 6 {
		// Minimum: 1 byte slave_id + 1 byte PDU + 1 byte LRC = 3 bytes -> 6 hex chars
		return nil, fmt.Errorf("ASCII frame hex content too short")
	}
	decoded := make([]byte, len(hexBytes)/2)
	for i := 0; i < len(hexBytes); i += 2 {
		hi, ok1 := hexVal(hexBytes[i])
		lo, ok2 := hexVal(hexBytes[i+1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("invalid hex character: %c", hexBytes[i])
		}
		decoded[i/2] = hi<<4 | lo
	}
	if !tools.VerifyLRC(decoded) {
		return nil, fmt.Errorf("ASCII frame LRC check failed")
	}
	return &ASCIIFrame{
		SlaveID: decoded[0],
		PDU:     decoded[1 : len(decoded)-1],
	}, nil
}
