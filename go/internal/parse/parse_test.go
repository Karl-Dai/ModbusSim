// Tests for parse.rs port: valid/invalid enum string parsing and
// read_function round trips.
package parse

import (
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func TestParseRegisterType(t *testing.T) {
	valid := map[string]register.RegisterType{
		"coil": register.Coil, "discrete_input": register.DiscreteInput,
		"input_register": register.InputRegister, "holding_register": register.HoldingRegType,
	}
	for s, want := range valid {
		got, err := ParseRegisterType(s)
		if err != nil || got != want {
			t.Errorf("ParseRegisterType(%q) = %v, %v", s, got, err)
		}
	}
	for _, s := range []string{"coils", "", "Coil", "bad"} {
		if _, err := ParseRegisterType(s); err == nil {
			t.Errorf("ParseRegisterType(%q) should fail", s)
		}
	}
}

func TestParseEndian(t *testing.T) {
	valid := map[string]register.Endian{
		"big": register.EndianBig, "little": register.EndianLittle,
		"mid_big": register.EndianMidBig, "mid_little": register.EndianMidLittle,
	}
	for s, want := range valid {
		got, err := ParseEndian(s)
		if err != nil || got != want {
			t.Errorf("ParseEndian(%q) = %v, %v", s, got, err)
		}
	}
	for _, s := range []string{"Big", "", "be", "unknown"} {
		if _, err := ParseEndian(s); err == nil {
			t.Errorf("ParseEndian(%q) should fail", s)
		}
	}
}

func TestParseDataType(t *testing.T) {
	valid := map[string]register.DataType{
		"bool": register.TypeBool, "uint16": register.TypeUInt16,
		"int16": register.TypeInt16, "uint32": register.TypeUInt32,
		"int32": register.TypeInt32, "float32": register.TypeFloat,
	}
	for s, want := range valid {
		got, err := ParseDataType(s)
		if err != nil || got != want {
			t.Errorf("ParseDataType(%q) = %v, %v", s, got, err)
		}
	}
	for _, s := range []string{"float64", "", "UInt16", "bad_type"} {
		if _, err := ParseDataType(s); err == nil {
			t.Errorf("ParseDataType(%q) should fail", s)
		}
	}
}

func TestReadFunctionRoundTrip(t *testing.T) {
	all := []master.ReadFunction{
		master.ReadCoils, master.ReadDiscreteInputs,
		master.ReadHoldingRegisters, master.ReadInputRegisters,
	}
	for _, f := range all {
		s, err := ReadFunctionToString(f)
		if err != nil {
			t.Fatal(err)
		}
		back, err := ParseReadFunction(s)
		if err != nil || back != f {
			t.Errorf("round trip %d -> %q -> %d, %v", f, s, back, err)
		}
	}
	for _, s := range []string{"ReadCoils", "", "fc01", "bad_fn"} {
		if _, err := ParseReadFunction(s); err == nil {
			t.Errorf("ParseReadFunction(%q) should fail", s)
		}
	}
}
