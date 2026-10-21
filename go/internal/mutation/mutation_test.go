package mutation

import (
	"math"
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func TestFlip(t *testing.T) {
	cfg := Default()
	cfg.Mode = ModeFlip
	cfg.Min, cfg.Max = 0, 100
	// current <= (lo+hi)/2 -> hi
	next, _ := ComputeNextValue(10, register.TypeUInt16, cfg, Up)
	if next != 100 {
		t.Errorf("flip low = %v, want 100", next)
	}
	next, _ = ComputeNextValue(60, register.TypeUInt16, cfg, Up)
	if next != 0 {
		t.Errorf("flip high = %v, want 0", next)
	}
}

func TestIncrementTriangle(t *testing.T) {
	cfg := Default()
	cfg.Mode = ModeIncrement
	cfg.Step = 10
	cfg.Min, cfg.Max = 0, 25
	d := InitialFor(ModeIncrement)
	if d != Up {
		t.Fatal("initial should be Up")
	}
	v, d := ComputeNextValue(0, register.TypeUInt16, cfg, d)
	if v != 10 || d != Up {
		t.Errorf("step1 = %v dir=%v", v, d)
	}
	v, d = ComputeNextValue(v, register.TypeUInt16, cfg, d)
	if v != 20 || d != Up {
		t.Errorf("step2 = %v dir=%v", v, d)
	}
	v, d = ComputeNextValue(v, register.TypeUInt16, cfg, d)
	if v != 25 || d != Down {
		t.Errorf("step3 = %v dir=%v (want 25/Down)", v, d)
	}
	v, d = ComputeNextValue(v, register.TypeUInt16, cfg, d)
	if v != 15 || d != Down {
		t.Errorf("step4 = %v dir=%v", v, d)
	}
}

func TestDecrementInitial(t *testing.T) {
	if InitialFor(ModeDecrement) != Down {
		t.Error("decrement initial should be Down")
	}
}

func TestRandomInRange(t *testing.T) {
	cfg := Default()
	cfg.Mode = ModeRandom
	cfg.Min, cfg.Max = 10, 20
	for i := 0; i < 100; i++ {
		v, _ := ComputeNextValue(0, register.TypeUInt16, cfg, Up)
		if v < 10 || v > 20 {
			t.Fatalf("random = %v out of range", v)
		}
	}
}

func TestApplyPointMutationCoil(t *testing.T) {
	m := register.NewRegisterMap()
	m.WriteCoil(5, false)
	def := register.RegisterDef{Address: 5, RegisterType: register.Coil}
	d := ApplyPointMutation(m, def, Default(), Up)
	if !m.Coils[5] || d != Up {
		t.Errorf("coil after = %v dir=%v", m.Coils[5], d)
	}
	ApplyPointMutation(m, def, Default(), Up)
	if m.Coils[5] {
		t.Error("coil should flip back")
	}
}

func TestApplyPointMutationHoldingU16(t *testing.T) {
	m := register.NewRegisterMap()
	m.WriteHoldingRegister(10, 0)
	def := register.RegisterDef{Address: 10, RegisterType: register.HoldingRegType, DataType: register.TypeUInt16, Endian: register.DefaultEndian}
	cfg := Default()
	cfg.Mode = ModeIncrement
	cfg.Step = 5
	cfg.Min, cfg.Max = 0, 100
	ApplyPointMutation(m, def, cfg, Up)
	if m.HoldingRegisters[10] != 5 {
		t.Errorf("holding = %d, want 5", m.HoldingRegisters[10])
	}
}

func TestApplyPointMutationFloat32Endian(t *testing.T) {
	m := register.NewRegisterMap()
	m.WriteHoldingRegisters(20, encodeFloat(25.0))
	def := register.RegisterDef{Address: 20, RegisterType: register.HoldingRegType, DataType: register.TypeFloat, Endian: register.DefaultEndian}
	cfg := Default()
	cfg.Mode = ModeIncrement
	cfg.Step = 1.5
	cfg.Min, cfg.Max = 0, 100
	ApplyPointMutation(m, def, cfg, Up)
	raw := []uint16{m.HoldingRegisters[20], m.HoldingRegisters[21]}
	v, err := register.DecodeValue(raw, register.TypeFloat, register.DefaultEndian)
	if err != nil {
		t.Fatal(err)
	}
	if v != 26.5 {
		t.Errorf("float32 mutation = %v, want 26.5", v)
	}
}

func encodeFloat(v float64) []uint16 {
	regs, _ := register.EncodeValue(v, register.TypeFloat, register.DefaultEndian)
	return regs
}

func TestNormalizeBitAreasForceFlip(t *testing.T) {
	for _, rt := range []register.RegisterType{register.Coil, register.DiscreteInput} {
		cfg := Config{Enabled: true, Mode: ModeRandom, PeriodMs: 1, Min: math.NaN(), Max: 1e9, Step: -5}
		// Bit areas ignore the numeric fields entirely, so nonsense is accepted.
		if err := Normalize(rt, &cfg); err != nil {
			t.Fatalf("%s: %v", rt, err)
		}
		if cfg.Mode != ModeFlip {
			t.Errorf("%s mode = %s, want flip", rt, cfg.Mode)
		}
		if cfg.PeriodMs != BaseTickMS {
			t.Errorf("%s period = %d, clamped to %d", rt, cfg.PeriodMs, BaseTickMS)
		}
	}
}

func TestNormalizeRejectsBadNumbers(t *testing.T) {
	tests := []struct {
		name  string
		cfg   Config
		wants string
	}{
		{
			"nan min",
			Config{Mode: ModeIncrement, Min: math.NaN(), Max: 10, Step: 1},
			"mutation min, max and step must be finite",
		},
		{
			"inf max",
			Config{Mode: ModeRandom, Min: 0, Max: math.Inf(1), Step: 1},
			"mutation min, max and step must be finite",
		},
		{
			"min above max",
			Config{Mode: ModeIncrement, Min: 20, Max: 10, Step: 1},
			"mutation min must not exceed max",
		},
		{
			"zero step while ramping",
			Config{Mode: ModeIncrement, Min: 0, Max: 10, Step: 0},
			"mutation step must be greater than zero",
		},
		{
			"negative step while ramping down",
			Config{Mode: ModeDecrement, Min: 0, Max: 10, Step: -1},
			"mutation step must be greater than zero",
		},
	}
	for _, tc := range tests {
		cfg := tc.cfg
		err := Normalize(register.HoldingRegType, &cfg)
		if err == nil || err.Error() != tc.wants {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.wants)
		}
	}
}

func TestNormalizeAcceptsAndClamps(t *testing.T) {
	// A random mode may keep a zero step, and equal bounds are legal.
	cfg := Config{Mode: ModeRandom, PeriodMs: 5000, Min: 7, Max: 7, Step: 0}
	if err := Normalize(register.HoldingRegType, &cfg); err != nil {
		t.Fatalf("random with zero step: %v", err)
	}
	if cfg.PeriodMs != 5000 {
		t.Errorf("period changed to %d", cfg.PeriodMs)
	}

	// Flip on a register area need not ramp, so a zero step passes too.
	cfg = Config{Mode: ModeFlip, PeriodMs: 10, Min: 0, Max: 100, Step: 0}
	if err := Normalize(register.InputRegister, &cfg); err != nil {
		t.Fatalf("flip with zero step: %v", err)
	}
	if cfg.PeriodMs != BaseTickMS {
		t.Errorf("period = %d, want clamped to %d", cfg.PeriodMs, BaseTickMS)
	}

	// Inverted bounds are rejected even though ComputeNextValue would swap them.
	if err := Normalize(register.HoldingRegType, &Config{Mode: ModeFlip, Min: 10, Max: 0}); err == nil {
		t.Error("min above max should be rejected for register areas")
	}
}
