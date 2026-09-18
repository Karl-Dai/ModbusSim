// Package register implements the four Modbus data areas, point
// definitions with data types and endianness, and definition-set
// validation. Ported from crates/modbussim-core/src/register.rs with
// identical semantics and JSON field names (project-file compatibility).
package register

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
)

// RegisterType enumerates the four Modbus areas.
type RegisterType string

const (
	Coil           RegisterType = "coil"
	DiscreteInput  RegisterType = "discrete_input"
	InputRegister  RegisterType = "input_register"
	HoldingRegType RegisterType = "holding_register"
)

// DataType enumerates the interpreted value types.
type DataType string

const (
	TypeBool   DataType = "bool"
	TypeUInt16 DataType = "uint16"
	TypeInt16  DataType = "int16"
	TypeUInt32 DataType = "uint32"
	TypeInt32  DataType = "int32"
	TypeFloat  DataType = "float32"
)

// RegisterCount returns how many 16-bit registers the data type occupies.
func (d DataType) RegisterCount() uint16 {
	switch d {
	case TypeUInt32, TypeInt32, TypeFloat:
		return 2
	default:
		return 1
	}
}

// Endian is the byte order for multi-register data types.
type Endian string

const (
	EndianBig        Endian = "big"         // AB CD
	EndianLittle     Endian = "little"      // CD AB
	EndianMidBig     Endian = "mid_big"     // BA DC
	EndianMidLittle  Endian = "mid_little"  // DC BA
)

// DefaultEndian mirrors Rust's #[derive(Default)] on Endian (Big).
const DefaultEndian = EndianBig

// RegisterDef is the metadata definition for a register point.
// Field names match the serde serialization of Rust's RegisterDef.
type RegisterDef struct {
	Address      uint16             `json:"address"`
	RegisterType RegisterType       `json:"register_type"`
	DataType     DataType           `json:"data_type"`
	Endian       Endian             `json:"endian"`
	Name         string             `json:"name"`
	Comment      string             `json:"comment"`
	Mutation     *MutationConfig    `json:"mutation,omitempty"`
	DataSource   *datasource.Config `json:"data_source,omitempty"`
}

// OccupiedRange returns the inclusive address range occupied by a point.
// Bit areas always occupy one address; 32-bit values occupy two words.
// Returns ok=false when the range would overflow address 65535.
func OccupiedRange(def RegisterDef) (start, end uint16, ok bool) {
	count := uint16(1)
	switch def.RegisterType {
	case HoldingRegType, InputRegister:
		count = def.DataType.RegisterCount()
	}
	sum := uint32(def.Address) + uint32(count) - 1
	if sum > 65535 {
		return 0, 0, false
	}
	return def.Address, uint16(sum), true
}

// DefinitionError mirrors Rust's RegisterDefinitionError.
type DefinitionError struct {
	Msg string
}

func (e *DefinitionError) Error() string { return e.Msg }

// ValidateDefinitions validates a complete point-definition set in O(n log n)
// time by sorting per area and comparing adjacent ranges.
func ValidateDefinitions(defs []RegisterDef) error {
	type rng struct {
		typ        RegisterType
		start, end uint16
	}
	ranges := make([]rng, 0, len(defs))
	for i := range defs {
		d := defs[i]
		start, end, ok := OccupiedRange(d)
		if !ok {
			return &DefinitionError{Msg: fmt.Sprintf("register %s@%d exceeds address 65535", d.RegisterType, d.Address)}
		}
		ranges = append(ranges, rng{d.RegisterType, start, end})
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].typ != ranges[j].typ {
			return ranges[i].typ < ranges[j].typ
		}
		if ranges[i].start != ranges[j].start {
			return ranges[i].start < ranges[j].start
		}
		return ranges[i].end < ranges[j].end
	})
	for i := 0; i+1 < len(ranges); i++ {
		cur, next := ranges[i], ranges[i+1]
		if cur.typ == next.typ && next.start <= cur.end {
			return &DefinitionError{Msg: fmt.Sprintf("register %s@%d overlaps point at address %d", cur.typ, cur.start, next.start)}
		}
	}
	return nil
}

// RegisterMap stores the four Modbus register areas as sparse maps,
// mirroring Rust's RegisterMap (HashMap<u16, ...>).
type RegisterMap struct {
	Coils            map[uint16]bool `json:"coils"`
	DiscreteInputs   map[uint16]bool `json:"discrete_inputs"`
	HoldingRegisters map[uint16]uint16 `json:"holding_registers"`
	InputRegisters   map[uint16]uint16 `json:"input_registers"`
}

// NewRegisterMap returns an empty map with all four areas initialized.
func NewRegisterMap() *RegisterMap {
	return &RegisterMap{
		Coils:            map[uint16]bool{},
		DiscreteInputs:   map[uint16]bool{},
		HoldingRegisters: map[uint16]uint16{},
		InputRegisters:   map[uint16]uint16{},
	}
}

func (m *RegisterMap) ensure() {
	if m.Coils == nil {
		m.Coils = map[uint16]bool{}
	}
	if m.DiscreteInputs == nil {
		m.DiscreteInputs = map[uint16]bool{}
	}
	if m.HoldingRegisters == nil {
		m.HoldingRegisters = map[uint16]uint16{}
	}
	if m.InputRegisters == nil {
		m.InputRegisters = map[uint16]uint16{}
	}
}

// ReadCoils reads count coils starting at start; missing entries read false.
func (m *RegisterMap) ReadCoils(start, count uint16) []bool {
	out := make([]bool, count)
	for i := uint16(0); i < count; i++ {
		out[i] = m.Coils[start+i]
	}
	return out
}

// WriteCoil writes a single coil.
func (m *RegisterMap) WriteCoil(addr uint16, value bool) {
	m.ensure()
	m.Coils[addr] = value
}

// WriteCoils writes consecutive coils.
func (m *RegisterMap) WriteCoils(start uint16, values []bool) {
	m.ensure()
	for i, v := range values {
		m.Coils[start+uint16(i)] = v
	}
}

// ReadDiscreteInputs reads count discrete inputs; missing entries read false.
func (m *RegisterMap) ReadDiscreteInputs(start, count uint16) []bool {
	out := make([]bool, count)
	for i := uint16(0); i < count; i++ {
		out[i] = m.DiscreteInputs[start+i]
	}
	return out
}

// ReadHoldingRegisters reads count holding registers; missing entries read 0.
func (m *RegisterMap) ReadHoldingRegisters(start, count uint16) []uint16 {
	out := make([]uint16, count)
	for i := uint16(0); i < count; i++ {
		out[i] = m.HoldingRegisters[start+i]
	}
	return out
}

// WriteHoldingRegister writes a single holding register.
func (m *RegisterMap) WriteHoldingRegister(addr, value uint16) {
	m.ensure()
	m.HoldingRegisters[addr] = value
}

// WriteHoldingRegisters writes consecutive holding registers.
func (m *RegisterMap) WriteHoldingRegisters(start uint16, values []uint16) {
	m.ensure()
	for i, v := range values {
		m.HoldingRegisters[start+uint16(i)] = v
	}
}

// ReadInputRegisters reads count input registers; missing entries read 0.
func (m *RegisterMap) ReadInputRegisters(start, count uint16) []uint16 {
	out := make([]uint16, count)
	for i := uint16(0); i < count; i++ {
		out[i] = m.InputRegisters[start+i]
	}
	return out
}

// HasAllCoils reports whether every address in [start, start+count) exists.
func (m *RegisterMap) HasAllCoils(start, count uint16) bool {
	for i := uint16(0); i < count; i++ {
		if _, ok := m.Coils[start+i]; !ok {
			return false
		}
	}
	return true
}

// HasAllDiscreteInputs reports whether every discrete input address exists.
func (m *RegisterMap) HasAllDiscreteInputs(start, count uint16) bool {
	for i := uint16(0); i < count; i++ {
		if _, ok := m.DiscreteInputs[start+i]; !ok {
			return false
		}
	}
	return true
}

// HasAllHoldingRegisters reports whether every holding register address exists.
func (m *RegisterMap) HasAllHoldingRegisters(start, count uint16) bool {
	for i := uint16(0); i < count; i++ {
		if _, ok := m.HoldingRegisters[start+i]; !ok {
			return false
		}
	}
	return true
}

// HasAllInputRegisters reports whether every input register address exists.
func (m *RegisterMap) HasAllInputRegisters(start, count uint16) bool {
	for i := uint16(0); i < count; i++ {
		if _, ok := m.InputRegisters[start+i]; !ok {
			return false
		}
	}
	return true
}

// HasCoil reports whether a single coil address exists.
func (m *RegisterMap) HasCoil(addr uint16) bool {
	_, ok := m.Coils[addr]
	return ok
}

// HasHoldingRegister reports whether a single holding register address exists.
func (m *RegisterMap) HasHoldingRegister(addr uint16) bool {
	_, ok := m.HoldingRegisters[addr]
	return ok
}

// EnsureFromDef allocates storage for a point definition (all addresses it
// occupies), mirroring RegisterMap::ensure_from_def.
func (m *RegisterMap) EnsureFromDef(def RegisterDef) {
	m.ensure()
	start, end, ok := OccupiedRange(def)
	if !ok {
		return
	}
	switch def.RegisterType {
	case Coil:
		for a := start; a <= end; a++ {
			if _, exists := m.Coils[a]; !exists {
				m.Coils[a] = false
			}
		}
	case DiscreteInput:
		for a := start; a <= end; a++ {
			if _, exists := m.DiscreteInputs[a]; !exists {
				m.DiscreteInputs[a] = false
			}
		}
	case HoldingRegType:
		for a := start; a <= end; a++ {
			if _, exists := m.HoldingRegisters[a]; !exists {
				m.HoldingRegisters[a] = 0
			}
		}
	case InputRegister:
		for a := start; a <= end; a++ {
			if _, exists := m.InputRegisters[a]; !exists {
				m.InputRegisters[a] = 0
			}
		}
	}
}

// RemoveFromDef deallocates storage for a point definition, mirroring
// RegisterMap::remove_from_def.
func (m *RegisterMap) RemoveFromDef(def RegisterDef) {
	start, end, ok := OccupiedRange(def)
	if !ok {
		return
	}
	switch def.RegisterType {
	case Coil:
		for a := start; a <= end; a++ {
			delete(m.Coils, a)
		}
	case DiscreteInput:
		for a := start; a <= end; a++ {
			delete(m.DiscreteInputs, a)
		}
	case HoldingRegType:
		for a := start; a <= end; a++ {
			delete(m.HoldingRegisters, a)
		}
	case InputRegister:
		for a := start; a <= end; a++ {
			delete(m.InputRegisters, a)
		}
	}
}

// ValidateRange reports whether value is representable in data_type.
func ValidateRange(value float64, dataType DataType) error {
	valid := false
	switch dataType {
	case TypeBool:
		valid = value == 0 || value == 1
	case TypeUInt16:
		valid = value >= 0 && value <= math.MaxUint16 && value == math.Trunc(value)
	case TypeInt16:
		valid = value >= math.MinInt16 && value <= math.MaxInt16 && value == math.Trunc(value)
	case TypeUInt32:
		valid = value >= 0 && value <= math.MaxUint32 && value == math.Trunc(value)
	case TypeInt32:
		valid = value >= math.MinInt32 && value <= math.MaxInt32 && value == math.Trunc(value)
	case TypeFloat:
		valid = true
	}
	if !valid {
		return fmt.Errorf("value %v out of range for %s", value, dataType)
	}
	return nil
}

// EncodeValue encodes a typed engineering value into one or two raw registers.
func EncodeValue(value float64, dataType DataType, endian Endian) ([]uint16, error) {
	if err := ValidateRange(value, dataType); err != nil {
		return nil, err
	}
	switch dataType {
	case TypeBool:
		if value != 0 {
			return []uint16{1}, nil
		}
		return []uint16{0}, nil
	case TypeUInt16:
		return []uint16{uint16(value)}, nil
	case TypeInt16:
		return []uint16{uint16(int16(value))}, nil
	case TypeUInt32:
		var raw [4]byte
		binary.BigEndian.PutUint32(raw[:], uint32(value))
		return applyEndianEncode(raw, endian), nil
	case TypeInt32:
		var raw [4]byte
		binary.BigEndian.PutUint32(raw[:], uint32(int32(value)))
		return applyEndianEncode(raw, endian), nil
	case TypeFloat:
		var raw [4]byte
		binary.BigEndian.PutUint32(raw[:], math.Float32bits(float32(value)))
		return applyEndianEncode(raw, endian), nil
	}
	return nil, fmt.Errorf("invalid data for conversion")
}

// DecodeValue decodes one or two raw registers into a typed engineering value.
func DecodeValue(regs []uint16, dataType DataType, endian Endian) (float64, error) {
	switch dataType {
	case TypeBool:
		if len(regs) < 1 {
			return 0, fmt.Errorf("invalid data for conversion")
		}
		if regs[0] != 0 {
			return 1, nil
		}
		return 0, nil
	case TypeUInt16:
		if len(regs) < 1 {
			return 0, fmt.Errorf("invalid data for conversion")
		}
		return float64(regs[0]), nil
	case TypeInt16:
		if len(regs) < 1 {
			return 0, fmt.Errorf("invalid data for conversion")
		}
		return float64(int16(regs[0])), nil
	case TypeUInt32:
		if len(regs) < 2 {
			return 0, fmt.Errorf("invalid data for conversion")
		}
		bytes := applyEndianDecode(regs[0], regs[1], endian)
		return float64(binary.BigEndian.Uint32(bytes[:])), nil
	case TypeInt32:
		if len(regs) < 2 {
			return 0, fmt.Errorf("invalid data for conversion")
		}
		bytes := applyEndianDecode(regs[0], regs[1], endian)
		return float64(int32(binary.BigEndian.Uint32(bytes[:]))), nil
	case TypeFloat:
		if len(regs) < 2 {
			return 0, fmt.Errorf("invalid data for conversion")
		}
		bytes := applyEndianDecode(regs[0], regs[1], endian)
		return float64(math.Float32frombits(binary.BigEndian.Uint32(bytes[:]))), nil
	}
	return 0, fmt.Errorf("invalid data for conversion")
}

// applyEndianEncode transforms 4 big-endian bytes into two registers per the
// endian mode (AB CD / CD AB / BA DC / DC BA).
func applyEndianEncode(be [4]byte, endian Endian) []uint16 {
	a, b, c, d := be[0], be[1], be[2], be[3]
	switch endian {
	case EndianLittle: // CD AB
		return []uint16{uint16(c)<<8 | uint16(d), uint16(a)<<8 | uint16(b)}
	case EndianMidBig: // BA DC
		return []uint16{uint16(b)<<8 | uint16(a), uint16(d)<<8 | uint16(c)}
	case EndianMidLittle: // DC BA
		return []uint16{uint16(d)<<8 | uint16(c), uint16(b)<<8 | uint16(a)}
	default: // Big: AB CD
		return []uint16{uint16(a)<<8 | uint16(b), uint16(c)<<8 | uint16(d)}
	}
}

// applyEndianDecode reverses applyEndianEncode back into 4 big-endian bytes.
func applyEndianDecode(reg0, reg1 uint16, endian Endian) [4]byte {
	r0 := [2]byte{byte(reg0 >> 8), byte(reg0 & 0xFF)}
	r1 := [2]byte{byte(reg1 >> 8), byte(reg1 & 0xFF)}
	switch endian {
	case EndianLittle: // reg0=CD, reg1=AB -> ABCD
		return [4]byte{r1[0], r1[1], r0[0], r0[1]}
	case EndianMidBig: // reg0=BA, reg1=DC -> ABCD
		return [4]byte{r0[1], r0[0], r1[1], r1[0]}
	case EndianMidLittle: // reg0=DC, reg1=BA -> ABCD
		return [4]byte{r1[1], r1[0], r0[1], r0[0]}
	default: // Big: reg0=AB, reg1=CD -> ABCD
		return [4]byte{r0[0], r0[1], r1[0], r1[1]}
	}
}
