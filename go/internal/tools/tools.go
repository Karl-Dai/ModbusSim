// Package tools provides Modbus utility functions: address conversion,
// checksum calculations, and hex string parsing. Ported from
// crates/modbussim-core/src/tools.rs with identical semantics.
package tools

import (
	"fmt"
	"strings"
)

// AddressType is a Modbus register type for PLC address conversion.
type AddressType int

const (
	Coil          AddressType = 0 // 0x - read/write single bit
	DiscreteInput AddressType = 1 // 1x - read-only single bit
	InputRegister AddressType = 3 // 3x - read-only 16-bit
	HoldingReg    AddressType = 4 // 4x - read/write 16-bit
)

func (t AddressType) PLCPrefix() uint32 {
	switch t {
	case Coil:
		return 0
	case DiscreteInput:
		return 1
	case InputRegister:
		return 3
	case HoldingReg:
		return 4
	}
	return 0
}

// Address is the result of a PLC to Modbus address conversion.
type Address struct {
	Address uint16
	Type    AddressType
}

// PLCToModbus converts a PLC address (e.g. 40001) to a Modbus address.
// The first digit(s) indicate the register type: 0xxxx/1xxxx/3xxxx/4xxxx.
func PLCToModbus(plcAddress uint32) (Address, error) {
	prefix := plcAddress / 10000
	within := plcAddress % 10000
	var t AddressType
	switch prefix {
	case 0:
		t = Coil
	case 1:
		t = DiscreteInput
	case 3:
		t = InputRegister
	case 4:
		t = HoldingReg
	default:
		return Address{}, fmt.Errorf("invalid address format: unknown PLC address prefix: %d", prefix)
	}
	if within > 9999 {
		return Address{}, fmt.Errorf("invalid address format: PLC address %d has invalid offset %d (must be 0-9999)", plcAddress, within)
	}
	return Address{Address: uint16(within), Type: t}, nil
}

// ModbusToPLC converts a Modbus address back to a PLC address.
func ModbusToPLC(address uint16, t AddressType) uint32 {
	return t.PLCPrefix()*10000 + uint32(address)
}

// CRC16 calculates the Modbus RTU CRC-16 (poly 0x8005 bit-reversed = 0xA001,
// init 0xFFFF).
func CRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// AppendCRC16 appends the CRC-16 low byte first, per Modbus RTU.
func AppendCRC16(data []byte) []byte {
	crc := CRC16(data)
	out := make([]byte, 0, len(data)+2)
	out = append(out, data...)
	out = append(out, byte(crc&0xFF), byte(crc>>8))
	return out
}

// VerifyCRC16 checks the CRC-16 of a frame including the CRC bytes.
func VerifyCRC16(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	crc := CRC16(data[:len(data)-2])
	frameCRC := uint16(data[len(data)-2]) | uint16(data[len(data)-1])<<8
	return crc == frameCRC
}

// LRC calculates the Modbus ASCII Longitudinal Redundancy Check: the two's
// complement of the 8-bit sum of all bytes.
func LRC(data []byte) byte {
	var sum uint16
	for _, b := range data {
		sum += uint16(b)
	}
	return byte((0x100 - sum) & 0xFF)
}

// AppendLRC appends the LRC byte to data.
func AppendLRC(data []byte) []byte {
	out := make([]byte, 0, len(data)+1)
	out = append(out, data...)
	return append(out, LRC(data))
}

// VerifyLRC checks the LRC of a frame including the LRC byte.
func VerifyLRC(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	return LRC(data[:len(data)-1]) == data[len(data)-1]
}

// ParseHexString parses a hex string into bytes. Accepts spaces, commas, or
// no separators; upper or lower case. Returns an error on invalid hex
// characters or an odd number of digits.
func ParseHexString(s string) ([]byte, error) {
	var b strings.Builder
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' {
			continue
		}
		b.WriteRune(c)
	}
	cleaned := b.String()
	if cleaned == "" {
		return []byte{}, nil
	}
	if len(cleaned)%2 != 0 {
		return nil, fmt.Errorf("invalid hex string: odd number of hex digits: '%s'", cleaned)
	}
	out := make([]byte, len(cleaned)/2)
	for i := 0; i < len(cleaned); i += 2 {
		hi, ok1 := hexVal(cleaned[i])
		lo, ok2 := hexVal(cleaned[i+1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("invalid hex string: invalid hex value '%s'", cleaned[i:i+2])
		}
		out[i/2] = hi<<4 | lo
	}
	return out, nil
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

// FormatHex formats bytes as an uppercase hex string with a separator.
func FormatHex(data []byte, sep string) string {
	parts := make([]string, len(data))
	for i, b := range data {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, sep)
}
