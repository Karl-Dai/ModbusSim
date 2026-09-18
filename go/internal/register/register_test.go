package register

import (
	"encoding/json"
	"testing"
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
