// Package datasource implements the per-register generated-value sources:
// Fixed / Random / Sine / Sawtooth / Triangle / Counter / CsvPlayback.
// Ported from crates/modbussim-core/src/data_source.rs with identical
// semantics, including the 100 ms minimum update period and JSON tags.
package datasource

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

// SourceKind discriminates the source variants; matches serde tag values.
type SourceKind string

const (
	KindFixed       SourceKind = "fixed"
	KindRandom      SourceKind = "random"
	KindSine        SourceKind = "sine"
	KindSawtooth    SourceKind = "sawtooth"
	KindTriangle    SourceKind = "triangle"
	KindCounter     SourceKind = "counter"
	KindCsvPlayback SourceKind = "csv_playback"
)

// Source is one of the seven value sources. Exactly the fields for Kind are
// meaningful; the rest stay zero. JSON serialization matches the Rust enum's
// externally-tagged form: {"type": "...", ...fields}.
type Source struct {
	Type SourceKind `json:"type"`

	// Fixed
	Value uint16 `json:"value,omitempty"`

	// Random / Sawtooth / Triangle
	Min uint16 `json:"min,omitempty"`
	Max uint16 `json:"max,omitempty"`

	// Sine
	Amplitude float64 `json:"amplitude,omitempty"`
	Frequency float64 `json:"frequency,omitempty"`
	Offset    float64 `json:"offset,omitempty"`
	Phase     float64 `json:"phase,omitempty"`

	// Sawtooth / Triangle
	PeriodMs uint64 `json:"period_ms,omitempty"`

	// Counter
	Start uint16 `json:"start,omitempty"`
	Step  int16  `json:"step,omitempty"`
	Wrap  bool   `json:"wrap,omitempty"`

	// CsvPlayback
	Values       []uint16 `json:"values,omitempty"`
	LoopPlayback bool     `json:"loop_playback,omitempty"`
}

// Config pairs a source with its update cadence.
type Config struct {
	Source           Source `json:"source"`
	UpdateIntervalMs uint64 `json:"update_interval_ms"`
}

// DefaultUpdateIntervalMs mirrors serde's default_update_interval_ms.
const DefaultUpdateIntervalMs = 1000

// MinPeriodMs mirrors data_source_period's clamp floor.
const MinPeriodMs = 100

// Validate checks a config the same way Rust's DataSourceConfig::validate.
func (c *Config) Validate() error {
	if c.UpdateIntervalMs == 0 {
		return fmt.Errorf("data source update interval must be greater than zero")
	}
	s := c.Source
	switch s.Type {
	case KindRandom, KindSawtooth, KindTriangle:
		if s.Min > s.Max {
			return fmt.Errorf("data source min must not exceed max")
		}
	case KindSine:
		if math.IsInf(s.Amplitude, 0) || math.IsNaN(s.Amplitude) ||
			math.IsInf(s.Frequency, 0) || math.IsNaN(s.Frequency) ||
			math.IsInf(s.Offset, 0) || math.IsNaN(s.Offset) ||
			math.IsInf(s.Phase, 0) || math.IsNaN(s.Phase) || s.Frequency < 0 {
			return fmt.Errorf("sine parameters must be finite and frequency must be non-negative")
		}
	}
	if s.Type == KindSawtooth && s.PeriodMs == 0 {
		return fmt.Errorf("sawtooth period must be greater than zero")
	}
	if s.Type == KindTriangle && s.PeriodMs < 2 {
		return fmt.Errorf("triangle period must be at least 2 ms")
	}
	if s.Type == KindCsvPlayback && len(s.Values) == 0 {
		return fmt.Errorf("CSV playback requires at least one value")
	}
	return nil
}

// Period returns the clamped update period.
func Period(updateIntervalMs uint64) time.Duration {
	if updateIntervalMs < MinPeriodMs {
		updateIntervalMs = MinPeriodMs
	}
	return time.Duration(updateIntervalMs) * time.Millisecond
}

// State holds the mutable per-source scheduling state, mirroring Rust's
// DataSourceState.
type State struct {
	Config Config

	startTime    time.Time
	counterValue int32
	csvIndex     int
	nextDue      time.Time
}

// NewState creates a state with the first due time one period from now.
func NewState(cfg Config) *State {
	var start int32
	if cfg.Source.Type == KindCounter {
		start = int32(cfg.Source.Start)
	}
	return &State{
		Config:       cfg,
		startTime:    time.Now(),
		counterValue: start,
		nextDue:      time.Now().Add(Period(cfg.UpdateIntervalMs)),
	}
}

// IsDue reports whether the source should produce a new value now.
func (s *State) IsDue(now time.Time) bool {
	return !now.Before(s.nextDue)
}

// MarkUpdated reschedules the next due time.
func (s *State) MarkUpdated(now time.Time) {
	s.nextDue = now.Add(Period(s.Config.UpdateIntervalMs))
}

// NextValue computes the next value, advancing internal state.
func (s *State) NextValue() uint16 {
	src := s.Config.Source
	switch src.Type {
	case KindFixed:
		return src.Value

	case KindRandom:
		// gen_range(min..=max) is inclusive on both ends.
		return uint16(src.Min + uint16(rand.Intn(int(src.Max-src.Min+1))))

	case KindSine:
		tSec := time.Since(s.startTime).Seconds()
		v := src.Offset + src.Amplitude*math.Sin(2*math.Pi*src.Frequency*tSec+src.Phase)
		return uint16(clamp(v, 0, 65535))

	case KindSawtooth:
		if src.PeriodMs == 0 {
			return src.Min
		}
		elapsedMs := uint64(time.Since(s.startTime).Milliseconds())
		pos := elapsedMs % src.PeriodMs
		frac := float64(pos) / float64(src.PeriodMs)
		rng := float64(src.Max) - float64(src.Min)
		return uint16(float64(src.Min) + frac*rng)

	case KindTriangle:
		if src.PeriodMs == 0 {
			return src.Min
		}
		elapsedMs := uint64(time.Since(s.startTime).Milliseconds())
		pos := elapsedMs % src.PeriodMs
		half := src.PeriodMs / 2
		if half == 0 {
			return src.Min
		}
		rng := float64(src.Max) - float64(src.Min)
		if pos < half {
			frac := float64(pos) / float64(half)
			return uint16(float64(src.Min) + frac*rng)
		}
		frac := float64(pos-half) / float64(half)
		return uint16(float64(src.Max) - frac*rng)

	case KindCounter:
		current := uint16(clampInt(s.counterValue, 0, 65535))
		next := s.counterValue + int32(src.Step)
		if src.Wrap {
			s.counterValue = ((next % 65536) + 65536) % 65536
		} else {
			s.counterValue = clampInt(next, 0, 65535)
		}
		return current

	case KindCsvPlayback:
		if len(src.Values) == 0 {
			return 0
		}
		idx := s.csvIndex
		if idx > len(src.Values)-1 {
			idx = len(src.Values) - 1
		}
		val := src.Values[idx]
		if src.LoopPlayback {
			s.csvIndex = (s.csvIndex + 1) % len(src.Values)
		} else if s.csvIndex < len(src.Values)-1 {
			s.csvIndex++
		}
		return val
	}
	return 0
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

func clampInt(v int32, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
