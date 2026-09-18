package tools

import (
	"bytes"
	"testing"
)

// Test vectors mirror crates/modbussim-core/src/tools.rs unit tests.

func TestPLCToModbus(t *testing.T) {
	cases := []struct {
		plc  uint32
		addr uint16
		typ  AddressType
		err  bool
	}{
		{40000, 0, HoldingReg, false},
		{40100, 100, HoldingReg, false},
		{30000, 0, InputRegister, false},
		{0, 0, Coil, false},
		{10000, 0, DiscreteInput, false},
		{50001, 0, 0, true},
	}
	for _, c := range cases {
		got, err := PLCToModbus(c.plc)
		if c.err {
			if err == nil {
				t.Errorf("PLCToModbus(%d): expected error", c.plc)
			}
			continue
		}
		if err != nil {
			t.Errorf("PLCToModbus(%d): unexpected error: %v", c.plc, err)
			continue
		}
		if got.Address != c.addr || got.Type != c.typ {
			t.Errorf("PLCToModbus(%d) = %v, want addr=%d type=%d", c.plc, got, c.addr, c.typ)
		}
	}
}

func TestModbusToPLCRoundtrip(t *testing.T) {
	if got := ModbusToPLC(0, HoldingReg); got != 40000 {
		t.Errorf("ModbusToPLC(0, HoldingReg) = %d, want 40000", got)
	}
	if got := ModbusToPLC(100, HoldingReg); got != 40100 {
		t.Errorf("ModbusToPLC(100) = %d, want 40100", got)
	}
	for _, plc := range []uint32{40001, 30500} {
		a, err := PLCToModbus(plc)
		if err != nil {
			t.Fatal(err)
		}
		if back := ModbusToPLC(a.Address, a.Type); back != plc {
			t.Errorf("roundtrip %d -> %d", plc, back)
		}
	}
}

func TestCRC16KnownValues(t *testing.T) {
	// Standard Modbus test frame: request FC03 addr 0 x2 from slave 1.
	// Widely published CRC for "01 03 00 00 00 02" is 0xC40B (lo=0xC4, hi=0x0B).
	data := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x02}
	withCRC := AppendCRC16(data)
	if !bytes.Equal(withCRC, []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x02, 0xC4, 0x0B}) {
		t.Errorf("AppendCRC16 = % X, want 01 03 00 00 00 02 C4 0B", withCRC)
	}
	if !VerifyCRC16(withCRC) {
		t.Error("VerifyCRC16(valid) = false")
	}
	if VerifyCRC16([]byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x0A, 0x00, 0x00}) {
		t.Error("VerifyCRC16(invalid) = true")
	}
	if VerifyCRC16([]byte{0x01}) || VerifyCRC16(nil) {
		t.Error("VerifyCRC16(too short) = true")
	}
	if CRC16(nil) != 0xFFFF {
		t.Errorf("CRC16(empty) = %04X, want FFFF", CRC16(nil))
	}
}

func TestLRC(t *testing.T) {
	data := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x0A}
	withLRC := AppendLRC(data)
	if len(withLRC) != 7 {
		t.Fatalf("AppendLRC len = %d, want 7", len(withLRC))
	}
	if !VerifyLRC(withLRC) {
		t.Error("VerifyLRC(valid) = false")
	}
	if VerifyLRC(append(data, 0x00)) {
		t.Error("VerifyLRC(invalid) = true")
	}
	if VerifyLRC(nil) {
		t.Error("VerifyLRC(empty) = true")
	}
	if LRC(nil) != 0x00 {
		t.Errorf("LRC(empty) = %02X, want 00", LRC(nil))
	}
}

func TestParseHexString(t *testing.T) {
	for _, in := range []string{"01 02 03 04", "01,02,03,04", "01020304", "01 02,03 04", "ab cd ef"} {
		got, err := ParseHexString(in)
		if err != nil {
			t.Errorf("ParseHexString(%q): %v", in, err)
			continue
		}
		want := []byte{0x01, 0x02, 0x03, 0x04}
		if in == "ab cd ef" {
			want = []byte{0xAB, 0xCD, 0xEF}
		}
		if !bytes.Equal(got, want) {
			t.Errorf("ParseHexString(%q) = % X, want % X", in, got, want)
		}
	}
	for _, in := range []string{"", "   \t\n  "} {
		got, err := ParseHexString(in)
		if err != nil || len(got) != 0 {
			t.Errorf("ParseHexString(%q) = %v, %v; want empty, nil", in, got, err)
		}
	}
	if _, err := ParseHexString("012"); err == nil {
		t.Error("ParseHexString(odd digits): expected error")
	}
	if _, err := ParseHexString("01 02 GG"); err == nil {
		t.Error("ParseHexString(invalid char): expected error")
	}
}

func TestFormatHex(t *testing.T) {
	data := []byte{0x01, 0x02, 0x0A, 0xFF}
	if got := FormatHex(data, " "); got != "01 02 0A FF" {
		t.Errorf("FormatHex(space) = %q", got)
	}
	if got := FormatHex(data, ""); got != "01020AFF" {
		t.Errorf("FormatHex(none) = %q", got)
	}
	if got := FormatHex(nil, " "); got != "" {
		t.Errorf("FormatHex(empty) = %q", got)
	}
}
