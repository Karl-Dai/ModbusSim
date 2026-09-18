// Ported from crates/modbussim-core/src/parse.rs: string<->enum parsing for
// register types, endianness, data types and master read functions.
// In Go the enums are string-typed, so "parse" is a validation step and the
// to-string direction is inherent.
package parse

import (
	"fmt"

	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func parseInto[T ~string](s string, valid map[T]bool, what string) (T, error) {
	v := T(s)
	if !valid[v] {
		var zero T
		return zero, fmt.Errorf("unknown %s: %s", what, s)
	}
	return v, nil
}

func ParseRegisterType(s string) (register.RegisterType, error) {
	return parseInto(s, map[register.RegisterType]bool{
		register.Coil: true, register.DiscreteInput: true,
		register.InputRegister: true, register.HoldingRegType: true,
	}, "register type")
}

func ParseEndian(s string) (register.Endian, error) {
	return parseInto(s, map[register.Endian]bool{
		register.EndianBig: true, register.EndianLittle: true,
		register.EndianMidBig: true, register.EndianMidLittle: true,
	}, "endian")
}

func ParseDataType(s string) (register.DataType, error) {
	return parseInto(s, map[register.DataType]bool{
		register.TypeBool: true, register.TypeUInt16: true, register.TypeInt16: true,
		register.TypeUInt32: true, register.TypeInt32: true, register.TypeFloat: true,
	}, "data type")
}

// ParseReadFunction parses a snake_case function name ("read_coils").
func ParseReadFunction(s string) (master.ReadFunction, error) {
	switch s {
	case "read_coils":
		return master.ReadCoils, nil
	case "read_discrete_inputs":
		return master.ReadDiscreteInputs, nil
	case "read_holding_registers":
		return master.ReadHoldingRegisters, nil
	case "read_input_registers":
		return master.ReadInputRegisters, nil
	}
	return 0, fmt.Errorf("unknown function: %s", s)
}

// ReadFunctionToString renders a ReadFunction as its snake_case name
// (mirrors parse.rs read_function_to_string).
func ReadFunctionToString(f master.ReadFunction) (string, error) {
	switch f {
	case master.ReadCoils:
		return "read_coils", nil
	case master.ReadDiscreteInputs:
		return "read_discrete_inputs", nil
	case master.ReadHoldingRegisters:
		return "read_holding_registers", nil
	case master.ReadInputRegisters:
		return "read_input_registers", nil
	}
	return "", fmt.Errorf("unknown function: %d", f)
}
