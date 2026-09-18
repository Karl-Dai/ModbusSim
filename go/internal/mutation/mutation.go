// Package mutation implements per-point random mutation: persisted
// configuration and pure value-stepping logic. Ported from
// crates/modbussim-core/src/mutation.rs with identical semantics.
package mutation

import (
	"math"
	"math/rand"

	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// Mode is how a point's value changes on each mutation tick.
type Mode = register.MutationMode

const (
	ModeFlip      = register.MutationModeFlip
	ModeIncrement = register.MutationModeIncrement
	ModeDecrement = register.MutationModeDecrement
	ModeRandom    = register.MutationModeRandom
)

// DefaultPeriodMs mirrors Rust's DEFAULT_PERIOD_MS.
const DefaultPeriodMs = register.DefaultMutationPeriodMs

// Config is the per-point mutation configuration, persisted on RegisterDef.
// The persisted type lives in register (RegisterDef references it); this
// alias keeps the mutation package's public API unchanged.
type Config = register.MutationConfig

// Default returns the Rust Default impl values.
func Default() Config {
	return Config{Enabled: false, Mode: ModeFlip, PeriodMs: DefaultPeriodMs, Step: 1, Min: 0, Max: 100}
}

// Direction is the non-persisted triangle-wave direction.
type Direction int

const (
	Up Direction = iota
	Down
)

// InitialFor picks the starting direction for a mode.
func InitialFor(mode Mode) Direction {
	if mode == ModeDecrement {
		return Down
	}
	return Up
}

// quantize rounds integer types and clamps to their range; passes floats through.
func quantize(value float64, dataType register.DataType) float64 {
	switch dataType {
	case register.TypeBool:
		if value != 0 {
			return 1
		}
		return 0
	case register.TypeUInt16:
		return clamp(math.Round(value), 0, math.MaxUint16)
	case register.TypeInt16:
		return clamp(math.Round(value), math.MinInt16, math.MaxInt16)
	case register.TypeUInt32:
		return clamp(math.Round(value), 0, math.MaxUint32)
	case register.TypeInt32:
		return clamp(math.Round(value), math.MinInt32, math.MaxInt32)
	case register.TypeFloat:
		if math.IsNaN(value) {
			return 0
		}
		return clamp(value, -math.MaxFloat32, math.MaxFloat32)
	}
	return value
}

// ComputeNextValue computes the next engineering value and triangle-wave
// direction, mirroring Rust's compute_next_value.
func ComputeNextValue(current float64, dataType register.DataType, cfg Config, direction Direction) (float64, Direction) {
	lo := math.Min(cfg.Min, cfg.Max)
	hi := math.Max(cfg.Min, cfg.Max)
	step := math.Abs(cfg.Step)

	var next float64
	var nextDirection Direction
	switch cfg.Mode {
	case ModeFlip:
		if current <= (lo+hi)/2 {
			next, nextDirection = hi, direction
		} else {
			next, nextDirection = lo, direction
		}

	case ModeIncrement, ModeDecrement:
		if hi <= lo || step == 0 {
			next, nextDirection = lo, direction
		} else if direction == Up {
			candidate := math.Max(current, lo) + step
			if candidate >= hi {
				next, nextDirection = hi, Down
			} else {
				next, nextDirection = candidate, Up
			}
		} else {
			candidate := math.Min(current, hi) - step
			if candidate <= lo {
				next, nextDirection = lo, Up
			} else {
				next, nextDirection = candidate, Down
			}
		}

	case ModeRandom:
		if hi <= lo {
			next, nextDirection = lo, direction
		} else {
			next, nextDirection = lo+rand.Float64()*(hi-lo), direction
		}
	}
	return quantize(next, dataType), nextDirection
}

// ApplyPointMutation applies one mutation tick to a single point in map.
// Bool points invert; numeric points decode by data_type/endian, step in
// engineering-value space, then encode back. Returns the next direction.
func ApplyPointMutation(m *register.RegisterMap, def register.RegisterDef, cfg Config, direction Direction) Direction {
	switch def.RegisterType {
	case register.Coil:
		cur := m.Coils[def.Address]
		m.WriteCoil(def.Address, !cur)
		return direction

	case register.DiscreteInput:
		cur := m.DiscreteInputs[def.Address]
		if m.DiscreteInputs == nil {
			m.DiscreteInputs = map[uint16]bool{}
		}
		m.DiscreteInputs[def.Address] = !cur
		return direction

	case register.HoldingRegType, register.InputRegister:
		isHolding := def.RegisterType == register.HoldingRegType
		count := def.DataType.RegisterCount()
		raw := make([]uint16, count)
		for i := uint16(0); i < count; i++ {
			addr := def.Address + i
			if isHolding {
				raw[i] = m.HoldingRegisters[addr]
			} else {
				raw[i] = m.InputRegisters[addr]
			}
		}
		current, err := register.DecodeValue(raw, def.DataType, def.Endian)
		if err != nil {
			current = 0
		}
		next, nextDirection := ComputeNextValue(current, def.DataType, cfg, direction)
		if encoded, err := register.EncodeValue(next, def.DataType, def.Endian); err == nil {
			for i, w := range encoded {
				addr := def.Address + uint16(i)
				if isHolding {
					m.WriteHoldingRegister(addr, w)
				} else {
					if m.InputRegisters == nil {
						m.InputRegisters = map[uint16]uint16{}
					}
					m.InputRegisters[addr] = w
				}
			}
		}
		return nextDirection
	}
	return direction
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
