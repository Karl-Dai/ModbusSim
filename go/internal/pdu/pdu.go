// Package pdu implements Modbus request PDU parsing and response PDU
// building for function codes FC01-FC06, FC15, FC16. Ported from
// crates/modbussim-core/src/pdu.rs with identical semantics.
package pdu

import "fmt"

// Request is a parsed Modbus request PDU.
type Request struct {
	// FunctionCode is the raw function code byte (0x01-0x10).
	FunctionCode uint8

	// Read requests (FC01-FC04) and writes (FC05/06): address.
	Address uint16
	// Quantity for FC01-FC04, FC15/16.
	Quantity uint16
	// Value for FC05 (coil true/false) and FC06.
	Value   uint16 // FC06 register value
	CoilOn  bool   // FC05: value decoded from 0xFF00/0x0000
	// Values for FC15 (bits) / FC16 (registers).
	Bits      []bool
	Registers []uint16
}

// FC constants.
const (
	FCReadCoils              = 0x01
	FCReadDiscreteInputs     = 0x02
	FCReadHoldingRegisters   = 0x03
	FCReadInputRegisters     = 0x04
	FCWriteSingleCoil        = 0x05
	FCWriteSingleRegister    = 0x06
	FCWriteMultipleCoils     = 0x0F
	FCWriteMultipleRegisters = 0x10
)

// IsWrite reports whether the request mutates registers.
func (r *Request) IsWrite() bool {
	switch r.FunctionCode {
	case FCWriteSingleCoil, FCWriteSingleRegister, FCWriteMultipleCoils, FCWriteMultipleRegisters:
		return true
	}
	return false
}

// ParseError distinguishes exception codes 0x01 (illegal function) and
// 0x03 (illegal data value), mirroring Rust's PduParseError.
type ParseError struct {
	Code uint8
	Msg  string
}

func (e *ParseError) Error() string { return e.Msg }

// ExceptionCode maps the parse error to a Modbus exception code:
// unsupported function -> 0x01, malformed data -> 0x03.
func (e *ParseError) ExceptionCode() uint8 { return e.Code }

func illegalDataValue(format string, args ...any) *ParseError {
	return &ParseError{Code: 0x03, Msg: fmt.Sprintf(format, args...)}
}

func unsupportedFunction(fc uint8) *ParseError {
	return &ParseError{Code: 0x01, Msg: fmt.Sprintf("unsupported function code: 0x%02X", fc)}
}

func requireExactLen(fc uint8, data []byte, expected int) error {
	if len(data) != expected {
		return illegalDataValue("FC%02X: expected %d bytes, got %d", fc, expected, len(data))
	}
	return nil
}

func be16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }

// ParseRequest parses a request PDU into a Request.
func ParseRequest(pdu []byte) (*Request, error) {
	if len(pdu) == 0 {
		return nil, illegalDataValue("PDU is empty")
	}
	fc := pdu[0]
	data := pdu[1:]

	switch fc {
	case FCReadCoils, FCReadDiscreteInputs, FCReadHoldingRegisters, FCReadInputRegisters:
		if err := requireExactLen(fc, data, 4); err != nil {
			return nil, err
		}
		return &Request{
			FunctionCode: fc,
			Address:      be16(data[0:2]),
			Quantity:     be16(data[2:4]),
		}, nil

	case FCWriteSingleCoil:
		if err := requireExactLen(fc, data, 4); err != nil {
			return nil, err
		}
		raw := be16(data[2:4])
		switch raw {
		case 0xFF00:
			return &Request{FunctionCode: fc, Address: be16(data[0:2]), CoilOn: true}, nil
		case 0x0000:
			return &Request{FunctionCode: fc, Address: be16(data[0:2]), CoilOn: false}, nil
		default:
			return nil, illegalDataValue("FC05: invalid coil value 0x%04X", raw)
		}

	case FCWriteSingleRegister:
		if err := requireExactLen(fc, data, 4); err != nil {
			return nil, err
		}
		return &Request{FunctionCode: fc, Address: be16(data[0:2]), Value: be16(data[2:4])}, nil

	case FCWriteMultipleCoils:
		// address(2) + quantity(2) + byte_count(1) + bytes
		if len(data) < 5 {
			return nil, illegalDataValue("FC0F: too short, got %d", len(data))
		}
		address := be16(data[0:2])
		quantity := int(be16(data[2:4]))
		byteCount := int(data[4])
		expectedByteCount := (quantity + 7) / 8
		if byteCount != expectedByteCount || len(data) != 5+byteCount {
			return nil, illegalDataValue("FC0F: byte count %d, expected %d; payload length %d",
				byteCount, expectedByteCount, len(data))
		}
		bits := make([]bool, quantity)
		for i := 0; i < quantity; i++ {
			bits[i] = data[5+i/8]>>(i%8)&1 == 1
		}
		return &Request{FunctionCode: fc, Address: address, Quantity: uint16(quantity), Bits: bits}, nil

	case FCWriteMultipleRegisters:
		// address(2) + quantity(2) + byte_count(1) + words
		if len(data) < 5 {
			return nil, illegalDataValue("FC10: too short, got %d", len(data))
		}
		address := be16(data[0:2])
		quantity := int(be16(data[2:4]))
		byteCount := int(data[4])
		if byteCount != quantity*2 || len(data) != 5+byteCount {
			return nil, illegalDataValue("FC10: byte count %d, expected %d; payload length %d",
				byteCount, quantity*2, len(data))
		}
		regs := make([]uint16, quantity)
		for i := 0; i < quantity; i++ {
			regs[i] = be16(data[5+i*2:])
		}
		return &Request{FunctionCode: fc, Address: address, Quantity: uint16(quantity), Registers: regs}, nil

	default:
		return nil, unsupportedFunction(fc)
	}
}

// ResponseData is the successful result of executing a request, used to
// build the response PDU.
type ResponseData struct {
	// Bits is set for FC01/FC02 read responses.
	Bits []bool
	// Registers is set for FC03/FC04 read responses.
	Registers []uint16
	// For FC05 echo: address and coil state.
	WriteAddress uint16
	WriteCoilOn  bool
	// For FC06 echo: register value.
	WriteValue uint16
	// For FC15/16 echo: quantity (with WriteAddress).
	WriteQuantity uint16
}

// BuildResponse builds a normal response PDU for the given function code.
func BuildResponse(fc uint8, data *ResponseData) []byte {
	switch fc {
	case FCReadCoils, FCReadDiscreteInputs:
		byteCount := (len(data.Bits) + 7) / 8
		packed := make([]byte, byteCount)
		for i, bit := range data.Bits {
			if bit {
				packed[i/8] |= 1 << (i % 8)
			}
		}
		out := make([]byte, 0, 2+byteCount)
		out = append(out, fc, uint8(byteCount))
		return append(out, packed...)

	case FCReadHoldingRegisters, FCReadInputRegisters:
		byteCount := len(data.Registers) * 2
		out := make([]byte, 0, 2+byteCount)
		out = append(out, fc, uint8(byteCount))
		for _, r := range data.Registers {
			out = append(out, byte(r>>8), byte(r&0xFF))
		}
		return out

	case FCWriteSingleCoil:
		hi := byte(0x00)
		if data.WriteCoilOn {
			hi = 0xFF
		}
		return []byte{fc, byte(data.WriteAddress >> 8), byte(data.WriteAddress & 0xFF), hi, 0x00}

	case FCWriteSingleRegister:
		return []byte{
			fc,
			byte(data.WriteAddress >> 8), byte(data.WriteAddress & 0xFF),
			byte(data.WriteValue >> 8), byte(data.WriteValue & 0xFF),
		}

	default: // FC15 / FC16 echo
		return []byte{
			fc,
			byte(data.WriteAddress >> 8), byte(data.WriteAddress & 0xFF),
			byte(data.WriteQuantity >> 8), byte(data.WriteQuantity & 0xFF),
		}
	}
}

// BuildException builds an exception response PDU: [fc | 0x80, exception_code].
func BuildException(fc uint8, exceptionCode uint8) []byte {
	return []byte{fc | 0x80, exceptionCode}
}
