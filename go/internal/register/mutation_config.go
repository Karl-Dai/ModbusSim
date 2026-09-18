// Per-point mutation configuration persisted on RegisterDef. Lives in the
// register package because RegisterDef references it and mutation imports
// register; the mutation package aliases these types to keep its own API.
// JSON field names match serde (project-file compatibility).
package register

import (
	"encoding/json"
)

// MutationMode is how a point's value changes on each mutation tick.
type MutationMode string

const (
	MutationModeFlip      MutationMode = "flip"
	MutationModeIncrement MutationMode = "increment"
	MutationModeDecrement MutationMode = "decrement"
	MutationModeRandom    MutationMode = "random"
)

// DefaultMutationPeriodMs mirrors serde's default_period_ms.
const DefaultMutationPeriodMs = 1000

// MutationConfig is the per-point mutation configuration (Rust MutationConfig).
type MutationConfig struct {
	Enabled  bool         `json:"enabled"`
	Mode     MutationMode `json:"mode"`
	PeriodMs uint64       `json:"period_ms"`
	Step     float64      `json:"step"`
	Min      float64      `json:"min"`
	Max      float64      `json:"max"`
}

// UnmarshalJSON applies serde's default_period_ms when the field is missing.
func (c *MutationConfig) UnmarshalJSON(b []byte) error {
	type raw MutationConfig
	r := raw{PeriodMs: DefaultMutationPeriodMs}
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*c = MutationConfig(r)
	return nil
}

// legacyDataTypeAliases maps serde snake_case spellings (u_int16 etc., as
// written by the Rust app) and legacy aliases onto the canonical Go strings.
var legacyDataTypeAliases = map[DataType]DataType{
	"u_int16": TypeUInt16,
	"u_int32": TypeUInt32,
	"uint16":  TypeUInt16,
	"uint32":  TypeUInt32,
}

// NormalizeDataType maps a serde-spelled data type onto the canonical value.
func NormalizeDataType(d DataType) DataType {
	if canon, ok := legacyDataTypeAliases[d]; ok {
		return canon
	}
	return d
}

// UnmarshalJSON defaults Endian to Big (serde default) and normalizes
// legacy data-type spellings so Rust-authored project files load cleanly.
func (d *RegisterDef) UnmarshalJSON(b []byte) error {
	type raw RegisterDef
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	if r.Endian == "" {
		r.Endian = DefaultEndian
	}
	r.DataType = NormalizeDataType(r.DataType)
	*d = RegisterDef(r)
	return nil
}
