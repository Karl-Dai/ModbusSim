package register

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDataTypeRegisterCount(t *testing.T) {
	if TypeBool.RegisterCount() != 1 || TypeUInt16.RegisterCount() != 1 || TypeInt16.RegisterCount() != 1 {
		t.Error("1-register types")
	}
	if TypeUInt32.RegisterCount() != 2 || TypeInt32.RegisterCount() != 2 || TypeFloat.RegisterCount() != 2 {
		t.Error("2-register types")
	}
}

func TestRegisterTypeJSON(t *testing.T) {
	b, _ := json.Marshal(HoldingRegType)
	if string(b) != `"holding_register"` {
		t.Errorf("json = %s", b)
	}
}

func TestOccupiedRange(t *testing.T) {
	s, e, ok := OccupiedRange(RegisterDef{Address: 100, RegisterType: HoldingRegType, DataType: TypeUInt32})
	if !ok || s != 100 || e != 101 {
		t.Errorf("u32 range = %d-%d, %v", s, e, ok)
	}
	_, _, ok = OccupiedRange(RegisterDef{Address: 65535, RegisterType: HoldingRegType, DataType: TypeUInt32})
	if ok {
		t.Error("overflow should not be ok")
	}
}

func TestValidateDefinitionsOverlap(t *testing.T) {
	err := ValidateDefinitions([]RegisterDef{
		{Address: 10, RegisterType: HoldingRegType, DataType: TypeUInt32},
		{Address: 11, RegisterType: HoldingRegType, DataType: TypeUInt16},
	})
	if err == nil {
		t.Error("overlap: expected error")
	}
	err = ValidateDefinitions([]RegisterDef{
		{Address: 10, RegisterType: HoldingRegType, DataType: TypeUInt32},
		{Address: 12, RegisterType: HoldingRegType, DataType: TypeUInt16},
		{Address: 10, RegisterType: InputRegister, DataType: TypeUInt16}, // different area, fine
	})
	if err != nil {
		t.Errorf("valid set: %v", err)
	}
}

// Rust rejected unknown field values while deserializing a project file; Go's
// decoder accepts any string, so the definition itself has to check them.
func TestValidateDefinitionTokens(t *testing.T) {
	if err := (RegisterDef{Address: 1, RegisterType: HoldingRegType, DataType: TypeUInt16}).Validate(); err != nil {
		t.Errorf("absent endian: %v", err)
	}
	for _, def := range []RegisterDef{
		{RegisterType: "counters", DataType: TypeUInt16},
		{RegisterType: HoldingRegType, DataType: ""},
		{RegisterType: Coil, DataType: TypeBool, Endian: "byte_swap_forever"},
	} {
		if err := def.Validate(); err == nil {
			t.Errorf("%+v accepted", def)
		}
	}
	if err := ValidateDefinitions([]RegisterDef{{RegisterType: HoldingRegType, DataType: "uint64"}}); err == nil ||
		err.Error() != "unknown data type: uint64" {
		t.Errorf("set validation: err = %v", err)
	}
}

func TestRegisterMapReadWrite(t *testing.T) {
	m := NewRegisterMap()
	m.WriteCoils(0, []bool{true, false, true})
	got := m.ReadCoils(0, 4)
	if !got[0] || got[1] || !got[2] || got[3] {
		t.Errorf("coils = %v", got)
	}
	m.WriteHoldingRegisters(5, []uint16{0x0A, 0x0B})
	regs := m.ReadHoldingRegisters(5, 3)
	if regs[0] != 0x0A || regs[1] != 0x0B || regs[2] != 0 {
		t.Errorf("holding = %v", regs)
	}
	if m.HasAllHoldingRegisters(5, 4) {
		t.Error("HasAllHoldingRegisters beyond stored: should be false")
	}
}

func TestEncodeDecodeRoundtrip(t *testing.T) {
	cases := []struct {
		value float64
		typ   DataType
		we    Endian
	}{
		{1234, TypeUInt16, EndianBig},
		{-2, TypeInt16, EndianBig},
		{25, TypeFloat, EndianBig},
		{25, TypeFloat, EndianLittle},
		{25, TypeFloat, EndianMidBig},
		{70000, TypeUInt32, EndianBig},
		{-100000, TypeInt32, EndianBig},
	}
	for _, c := range cases {
		regs, err := EncodeValue(c.value, c.typ, c.we)
		if err != nil {
			t.Errorf("EncodeValue(%v, %s): %v", c.value, c.typ, err)
			continue
		}
		val, err := DecodeValue(regs, c.typ, c.we)
		if err != nil {
			t.Errorf("DecodeValue: %v", err)
			continue
		}
		if val != c.value {
			t.Errorf("roundtrip %v (%s/%s): got %v", c.value, c.typ, c.we, val)
		}
	}
}

func TestValidateRange(t *testing.T) {
	if err := ValidateRange(70000, TypeUInt16); err == nil {
		t.Error("u16 overflow: expected error")
	}
	if err := ValidateRange(1.5, TypeUInt16); err == nil {
		t.Error("fract u16: expected error")
	}
	if err := ValidateRange(2.0, TypeBool); err == nil {
		t.Error("bool 2.0: expected error")
	}
	if err := ValidateRange(1.5, TypeFloat); err != nil {
		t.Errorf("float 1.5: %v", err)
	}
}

func TestEnsureAndRemoveFromDefAtTopAddress(t *testing.T) {
	// A 16-bit point at address 65535 is legal; the inclusive range loop must
	// not wrap around and spin forever.
	done := make(chan struct{})
	go func() {
		defer close(done)
		m := NewRegisterMap()
		def := RegisterDef{Address: 65535, RegisterType: HoldingRegType, DataType: TypeUInt16, Endian: EndianBig}
		m.EnsureFromDef(def)
		if !m.HasAllHoldingRegisters(65535, 1) {
			t.Error("65535 should be allocated")
		}
		m.RemoveFromDef(def)
		if m.HasAllHoldingRegisters(65535, 1) {
			t.Error("65535 should be freed")
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("range loop at address 65535 did not terminate")
	}
}

func TestReplaceDef(t *testing.T) {
	t.Run("moves and shrinks", func(t *testing.T) {
		// Port of Rust's remove_and_replace_definition_update_raw_address_lifecycle:
		// a 32-bit point at 10 becomes a 16-bit point at 20 and keeps its first word.
		original := RegisterDef{Address: 10, RegisterType: HoldingRegType, DataType: TypeUInt32, Endian: EndianBig}
		replacement := RegisterDef{Address: 20, RegisterType: HoldingRegType, DataType: TypeUInt16, Endian: EndianBig}

		m := NewRegisterMap()
		m.EnsureFromDef(original)
		m.WriteHoldingRegisters(10, []uint16{0x1234, 0x5678})

		m.ReplaceDef(original, replacement)

		if m.HasAllHoldingRegisters(10, 1) || m.HasAllHoldingRegisters(11, 1) {
			t.Error("old range should be freed")
		}
		if got := m.HoldingRegisters[20]; got != 0x1234 {
			t.Errorf("word at 20 = %#04x, want 0x1234", got)
		}
		m.RemoveFromDef(replacement)
		if m.HasAllHoldingRegisters(20, 1) {
			t.Error("replacement should be removable")
		}
	})

	t.Run("widening leaves the new word at zero", func(t *testing.T) {
		original := RegisterDef{Address: 3, RegisterType: HoldingRegType, DataType: TypeUInt16, Endian: EndianBig}
		replacement := RegisterDef{Address: 3, RegisterType: HoldingRegType, DataType: TypeUInt32, Endian: EndianBig}
		m := NewRegisterMap()
		m.EnsureFromDef(original)
		m.WriteHoldingRegister(3, 0xAAAA)

		m.ReplaceDef(original, replacement)
		if m.HoldingRegisters[3] != 0xAAAA || m.HoldingRegisters[4] != 0 {
			t.Errorf("range = %v", m.HoldingRegisters)
		}
	})

	t.Run("bit area keeps its value across a move", func(t *testing.T) {
		original := RegisterDef{Address: 7, RegisterType: Coil, DataType: TypeBool, Endian: EndianBig}
		replacement := RegisterDef{Address: 8, RegisterType: Coil, DataType: TypeBool, Endian: EndianBig}
		m := NewRegisterMap()
		m.EnsureFromDef(original)
		m.WriteCoil(7, true)

		m.ReplaceDef(original, replacement)
		if m.HasCoil(7) {
			t.Error("old coil should be gone")
		}
		if !m.Coils[8] {
			t.Error("coil value should move with the point")
		}
	})

	t.Run("switching areas drops the value", func(t *testing.T) {
		original := RegisterDef{Address: 5, RegisterType: Coil, DataType: TypeBool, Endian: EndianBig}
		replacement := RegisterDef{Address: 5, RegisterType: DiscreteInput, DataType: TypeBool, Endian: EndianBig}
		m := NewRegisterMap()
		m.EnsureFromDef(original)
		m.WriteCoil(5, true)

		m.ReplaceDef(original, replacement)
		if m.HasCoil(5) {
			t.Error("coil should be freed when the point moves to FC2")
		}
		if m.DiscreteInputs[5] {
			t.Error("discrete input starts false; values do not cross areas")
		}
	})

	t.Run("input register area", func(t *testing.T) {
		original := RegisterDef{Address: 1, RegisterType: InputRegister, DataType: TypeUInt16, Endian: EndianBig}
		replacement := RegisterDef{Address: 2, RegisterType: InputRegister, DataType: TypeUInt16, Endian: EndianBig}
		m := NewRegisterMap()
		m.EnsureFromDef(original)
		m.InputRegisters[1] = 99

		m.ReplaceDef(original, replacement)
		if _, ok := m.InputRegisters[1]; ok {
			t.Error("old address should be freed")
		}
		if m.InputRegisters[2] != 99 {
			t.Errorf("value at 2 = %d, want 99", m.InputRegisters[2])
		}
	})
}
