package datasource

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFixedSource(t *testing.T) {
	s := NewState(Config{Source: Source{Type: KindFixed, Value: 42}, UpdateIntervalMs: 1000})
	for i := 0; i < 3; i++ {
		if v := s.NextValue(); v != 42 {
			t.Fatalf("fixed = %d, want 42", v)
		}
	}
}

func TestRandomSourceInRange(t *testing.T) {
	s := NewState(Config{Source: Source{Type: KindRandom, Min: 10, Max: 20}, UpdateIntervalMs: 1000})
	for i := 0; i < 100; i++ {
		v := s.NextValue()
		if v < 10 || v > 20 {
			t.Fatalf("random out of range: %d", v)
		}
	}
}

func TestCounterIncrement(t *testing.T) {
	s := NewState(Config{Source: Source{Type: KindCounter, Start: 0, Step: 1}, UpdateIntervalMs: 1000})
	for i, want := range []uint16{0, 1, 2} {
		if v := s.NextValue(); v != want {
			t.Fatalf("counter[%d] = %d, want %d", i, v, want)
		}
	}
}

func TestCounterWrap(t *testing.T) {
	s := NewState(Config{Source: Source{Type: KindCounter, Start: 65534, Step: 2, Wrap: true}, UpdateIntervalMs: 1000})
	if v := s.NextValue(); v != 65534 {
		t.Fatalf("first = %d", v)
	}
	if v := s.NextValue(); v != 0 {
		t.Fatalf("wrapped = %d, want 0", v)
	}
}

func TestCounterNoWrapClamp(t *testing.T) {
	s := NewState(Config{Source: Source{Type: KindCounter, Start: 65535, Step: 1}, UpdateIntervalMs: 1000})
	if v := s.NextValue(); v != 65535 {
		t.Fatalf("first = %d", v)
	}
	if v := s.NextValue(); v != 65535 {
		t.Fatalf("clamped = %d, want 65535", v)
	}
}

func TestCsvPlaybackLoop(t *testing.T) {
	s := NewState(Config{Source: Source{Type: KindCsvPlayback, Values: []uint16{10, 20, 30}, LoopPlayback: true}, UpdateIntervalMs: 1000})
	for i, want := range []uint16{10, 20, 30, 10} {
		if v := s.NextValue(); v != want {
			t.Fatalf("loop[%d] = %d, want %d", i, v, want)
		}
	}
}

func TestCsvPlaybackNoLoop(t *testing.T) {
	s := NewState(Config{Source: Source{Type: KindCsvPlayback, Values: []uint16{10, 20, 30}}, UpdateIntervalMs: 1000})
	for i, want := range []uint16{10, 20, 30, 30} {
		if v := s.NextValue(); v != want {
			t.Fatalf("noloop[%d] = %d, want %d", i, v, want)
		}
	}
}

func TestCsvPlaybackEmpty(t *testing.T) {
	s := NewState(Config{Source: Source{Type: KindCsvPlayback}, UpdateIntervalMs: 1000})
	if v := s.NextValue(); v != 0 {
		t.Fatalf("empty = %d", v)
	}
}

func TestJSONRoundtripMatchesRust(t *testing.T) {
	// Rust serde: {"source":{"type":"sine","amplitude":100.0,...},"update_interval_ms":500}
	in := `{"source":{"type":"sine","amplitude":100.0,"frequency":1.0,"offset":32768.0,"phase":0.5},"update_interval_ms":500}`
	var cfg Config
	if err := json.Unmarshal([]byte(in), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Source.Type != KindSine || cfg.Source.Amplitude != 100 || cfg.Source.Frequency != 1 ||
		cfg.Source.Offset != 32768 || cfg.Source.Phase != 0.5 || cfg.UpdateIntervalMs != 500 {
		t.Errorf("parsed = %+v", cfg)
	}
	out, _ := json.Marshal(&cfg)
	var cfg2 Config
	if err := json.Unmarshal(out, &cfg2); err != nil {
		t.Fatal(err)
	}
	if cfg2.Source.Type != cfg.Source.Type || cfg2.Source.Amplitude != cfg.Source.Amplitude ||
		cfg2.Source.Frequency != cfg.Source.Frequency || cfg2.Source.Offset != cfg.Source.Offset ||
		cfg2.Source.Phase != cfg.Source.Phase || cfg2.UpdateIntervalMs != cfg.UpdateIntervalMs {
		t.Errorf("roundtrip: %s", out)
	}
}

func TestValidate(t *testing.T) {
	if err := (&Config{Source: Source{Type: KindRandom, Min: 10, Max: 5}, UpdateIntervalMs: 100}).Validate(); err == nil {
		t.Error("min>max: expected error")
	}
	if err := (&Config{Source: Source{Type: KindFixed, Value: 1}, UpdateIntervalMs: 0}).Validate(); err == nil {
		t.Error("zero interval: expected error")
	}
	if err := (&Config{Source: Source{Type: KindSawtooth, Min: 5, Max: 100}, UpdateIntervalMs: 100}).Validate(); err == nil {
		t.Error("sawtooth zero period: expected error")
	}
}

func TestDueTimeClamped(t *testing.T) {
	s := NewState(Config{Source: Source{Type: KindFixed, Value: 1}, UpdateIntervalMs: 1000})
	now := time.Now()
	s.nextDue = now
	if !s.IsDue(now) {
		t.Error("should be due")
	}
	s.MarkUpdated(now)
	if d := s.nextDue.Sub(now); d != 1000*time.Millisecond {
		t.Errorf("next due = %v, want 1s", d)
	}
	s.Config.UpdateIntervalMs = 1
	s.MarkUpdated(now)
	if d := s.nextDue.Sub(now); d != 100*time.Millisecond {
		t.Errorf("clamped due = %v, want 100ms", d)
	}
}
