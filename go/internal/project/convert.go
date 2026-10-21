// Conversions between the persisted v2 model and the in-memory engine types
// used by internal/register, internal/mutation and internal/datasource.
package project

import (
	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// NewPoint builds a persisted point from its engine definition plus the
// optional behaviour blocks. Nil pointers mean "not configured".
func NewPoint(def register.RegisterDef, mut *mutation.Config, src *datasource.Config) *Point {
	p := &Point{
		Address:      def.Address,
		RegisterType: def.RegisterType,
		DataType:     def.DataType,
		Endian:       def.Endian,
		Name:         def.Name,
		Comment:      def.Comment,
	}
	if mut != nil {
		p.Mutation = mutationFromEngine(*mut)
	}
	if src != nil {
		p.DataSource = dataSourceFromEngine(*src)
	}
	return p
}

// ToDef returns the definition the register engine consumes.
func (p *Point) ToDef() register.RegisterDef {
	return register.RegisterDef{
		Address:      p.Address,
		RegisterType: p.RegisterType,
		DataType:     p.DataType,
		Endian:       p.Endian,
		Name:         p.Name,
		Comment:      p.Comment,
	}
}

// ToMutation returns the mutation engine config, or nil when unconfigured.
func (p *Point) ToMutation() *mutation.Config {
	if p.Mutation == nil {
		return nil
	}
	cfg := p.Mutation.ToEngine()
	return &cfg
}

// ToDataSource returns the generated-value config, or nil when unconfigured.
func (p *Point) ToDataSource() *datasource.Config {
	if p.DataSource == nil {
		return nil
	}
	cfg := p.DataSource.ToEngine()
	return &cfg
}

// ToEngine converts to the mutation package's configuration.
func (m *MutationConfig) ToEngine() mutation.Config {
	return mutation.Config{
		Enabled:  m.Enabled,
		Mode:     m.Mode,
		PeriodMs: m.PeriodMS,
		Step:     m.Step,
		Min:      m.Min,
		Max:      m.Max,
	}
}

func mutationFromEngine(cfg mutation.Config) *MutationConfig {
	return &MutationConfig{
		Enabled:  cfg.Enabled,
		Mode:     cfg.Mode,
		PeriodMS: cfg.PeriodMs,
		Step:     cfg.Step,
		Min:      cfg.Min,
		Max:      cfg.Max,
	}
}

// ToEngine converts the flat source union to the datasource package's shape.
func (s *DataSource) ToEngine() datasource.Source {
	return datasource.Source{
		Type:         s.Type,
		Value:        s.Value,
		Min:          s.Min,
		Max:          s.Max,
		Amplitude:    s.Amplitude,
		Frequency:    s.Frequency,
		Offset:       s.Offset,
		Phase:        s.Phase,
		PeriodMs:     s.PeriodMS,
		Start:        s.Start,
		Step:         s.Step,
		Wrap:         s.Wrap,
		Values:       s.Values,
		LoopPlayback: s.LoopPlayback,
	}
}

// ToEngine converts to the datasource package's configuration.
func (d *DataSourceConfig) ToEngine() datasource.Config {
	return datasource.Config{
		Source:           d.Source.ToEngine(),
		UpdateIntervalMs: d.UpdateIntervalMS,
	}
}

func dataSourceFromEngine(cfg datasource.Config) *DataSourceConfig {
	return &DataSourceConfig{
		Source: DataSource{
			Type:         cfg.Source.Type,
			Value:        cfg.Source.Value,
			Min:          cfg.Source.Min,
			Max:          cfg.Source.Max,
			Amplitude:    cfg.Source.Amplitude,
			Frequency:    cfg.Source.Frequency,
			Offset:       cfg.Source.Offset,
			Phase:        cfg.Source.Phase,
			PeriodMS:     cfg.Source.PeriodMs,
			Start:        cfg.Source.Start,
			Step:         cfg.Source.Step,
			Wrap:         cfg.Source.Wrap,
			Values:       cfg.Source.Values,
			LoopPlayback: cfg.Source.LoopPlayback,
		},
		UpdateIntervalMS: cfg.UpdateIntervalMs,
	}
}

// ToRegisterMap materialises the stored values into an engine register map.
func (v Values) ToRegisterMap() *register.RegisterMap {
	m := register.NewRegisterMap()
	for a, b := range v.Coils {
		m.Coils[a] = b
	}
	for a, b := range v.DiscreteInputs {
		m.DiscreteInputs[a] = b
	}
	for a, w := range v.HoldingRegisters {
		m.HoldingRegisters[a] = w
	}
	for a, w := range v.InputRegisters {
		m.InputRegisters[a] = w
	}
	return m
}

// ValuesFromMap snapshots an engine register map for persistence. Callers that
// save periodically should copy the maps first: this shares them with the
// running device.
func ValuesFromMap(m *register.RegisterMap) Values {
	if m == nil {
		return Values{}
	}
	return Values{
		Coils:            m.Coils,
		DiscreteInputs:   m.DiscreteInputs,
		HoldingRegisters: m.HoldingRegisters,
		InputRegisters:   m.InputRegisters,
	}
}
