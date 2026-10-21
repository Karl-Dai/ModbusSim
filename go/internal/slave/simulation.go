// Per-point background simulation. The Rust app runs the two drivers as
// separate 100 ms tick tasks (crates/modbussim-app/src/mutation.rs:29-85 and
// data_source.rs:17-102); they share one ticker here because each point owns
// its own due time either way.
package slave

import (
	"sort"
	"sync"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// BaseTickMS is the scheduler resolution. Both driver periods are clamped to
// at least this, mirroring Rust's MUTATION_BASE_TICK_MS and
// datasource.MinPeriodMs.
const BaseTickMS = mutation.BaseTickMS

// Behaviour is one point's simulation input: its definition plus the optional
// drivers. A point may carry mutation, a data source, or neither; the app
// rejects both at once (see internal/project validation).
type Behaviour struct {
	Def        register.RegisterDef
	Mutation   *mutation.Config
	DataSource *datasource.Config
}

// pointRuntime is the non-persisted scheduling state for one simulated point.
type pointRuntime struct {
	def      register.RegisterDef
	mutation *mutationRuntime
	source   *sourceRuntime
}

type mutationRuntime struct {
	cfg       mutation.Config
	direction mutation.Direction
	nextDue   time.Time
}

type sourceRuntime struct {
	state *datasource.State
}

// Simulator drives mutation and data sources for one Server. The zero value is
// not usable; create it with NewSimulator.
type Simulator struct {
	server *Server

	mu       sync.Mutex
	points   map[uint8][]*pointRuntime
	mutating bool // master switch, Rust's mutation_running
	stop     chan struct{}
	done     chan struct{}
}

// NewSimulator attaches a simulator to a server. Nothing runs until Start.
func NewSimulator(server *Server) *Simulator {
	return &Simulator{server: server, points: map[uint8][]*pointRuntime{}}
}

// SetPoints replaces slaveID's simulated points. Scheduling state is rebuilt
// from scratch, matching Rust, where editing a point recreates its mutation
// runtime and setting a data source recreates its state.
func (s *Simulator) SetPoints(slaveID uint8, behaviours []Behaviour) {
	s.setPointsAt(slaveID, behaviours, time.Now())
}

func (s *Simulator) setPointsAt(slaveID uint8, behaviours []Behaviour, now time.Time) {
	points := make([]*pointRuntime, 0, len(behaviours))
	for _, b := range behaviours {
		if rt := pointFor(b, now); rt != nil {
			points = append(points, rt)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setDevicePoints(slaveID, points)
}

// SetPoint replaces one point's drivers, leaving the scheduling state of the
// device's other points untouched -- mirroring Rust, where enabling a point
// mutation rewrites a single entry of the runtime map. A behaviour with neither
// mutation nor data source removes the point.
func (s *Simulator) SetPoint(slaveID uint8, b Behaviour) {
	s.setPointAt(slaveID, b, time.Now())
}

func (s *Simulator) setPointAt(slaveID uint8, b Behaviour, now time.Time) {
	runtime := pointFor(b, now)
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.points[slaveID]
	for i, point := range list {
		if point.def.Address != b.Def.Address || point.def.RegisterType != b.Def.RegisterType {
			continue
		}
		if runtime == nil {
			s.setDevicePoints(slaveID, append(list[:i], list[i+1:]...))
		} else {
			list[i] = runtime
			s.setDevicePoints(slaveID, list)
		}
		return
	}
	if runtime != nil {
		s.setDevicePoints(slaveID, append(list, runtime))
	}
}

// setDevicePoints stores a device's point list, dropping the entry when empty.
// The caller holds s.mu.
func (s *Simulator) setDevicePoints(slaveID uint8, points []*pointRuntime) {
	if len(points) == 0 {
		delete(s.points, slaveID)
		return
	}
	s.points[slaveID] = points
}

// MovePoint re-keys a point whose definition changed. Its data source keeps
// generating from where it was, while its mutation schedule restarts, which is
// what Rust's update_register does when it moves a data source state but
// rebuilds the mutation runtime.
func (s *Simulator) MovePoint(slaveID uint8, original, replacement register.RegisterDef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, point := range s.points[slaveID] {
		if point.def.Address != original.Address || point.def.RegisterType != original.RegisterType {
			continue
		}
		point.def = replacement
		if point.mutation != nil {
			cfg := point.mutation.cfg
			point.mutation = &mutationRuntime{
				cfg:       cfg,
				direction: mutation.InitialFor(cfg.Mode),
				nextDue:   time.Now().Add(mutationPeriod(cfg.PeriodMs)),
			}
		}
		return
	}
}

// pointFor builds one point's scheduling state, or nil when it drives nothing.
func pointFor(b Behaviour, now time.Time) *pointRuntime {
	point := &pointRuntime{def: b.Def}
	if b.DataSource != nil {
		point.source = &sourceRuntime{state: datasource.NewStateAt(*b.DataSource, now)}
	}
	if b.Mutation != nil && b.Mutation.Enabled {
		point.mutation = &mutationRuntime{
			cfg:       *b.Mutation,
			direction: mutation.InitialFor(b.Mutation.Mode),
			nextDue:   now.Add(mutationPeriod(b.Mutation.PeriodMs)),
		}
	}
	if point.source == nil && point.mutation == nil {
		return nil
	}
	return point
}

// SetMutationEnabled toggles the mutation master switch. Data sources keep
// running while it is off, as they do in Rust.
func (s *Simulator) SetMutationEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutating = enabled
}

// MutationEnabled reports the master switch.
func (s *Simulator) MutationEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutating
}

// Start launches the tick goroutine. Calling it while running is a no-op.
func (s *Simulator) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		return
	}
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go s.loop(s.stop, s.done)
}

// Stop halts the tick goroutine and waits for the in-flight tick to finish.
// Safe to call when not running.
func (s *Simulator) Stop() {
	s.mu.Lock()
	stop, done := s.stop, s.done
	s.stop, s.done = nil, nil
	s.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

func (s *Simulator) loop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Duration(BaseTickMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			// A Go ticker drops ticks while the receiver is busy, which is
			// tokio's MissedTickBehavior::Skip.
			s.Tick(now)
		}
	}
}

// Tick advances every due point exactly once. Start calls it with the real
// clock; it is exported so callers and tests can drive the scheduler with a
// chosen instant.
func (s *Simulator) Tick(now time.Time) {
	var changes []Change

	s.mu.Lock()
	mutating := s.mutating
	for _, slaveID := range simulatedSlaves(s.points) {
		for _, point := range s.points[slaveID] {
			if rt := point.mutation; rt != nil && mutating && !now.Before(rt.nextDue) {
				rt.nextDue = now.Add(mutationPeriod(rt.cfg.PeriodMs))
				if dir, ok := s.applyMutation(slaveID, point.def, rt); ok {
					rt.direction = dir
				}
			}
			if rt := point.source; rt != nil && rt.state.IsDue(now) {
				value := rt.state.NextValue()
				rt.state.MarkUpdated(now)
				if s.applySource(slaveID, point.def, value) {
					changes = append(changes, Change{
						SlaveID:      slaveID,
						RegisterType: point.def.RegisterType,
						Address:      point.def.Address,
						Value:        value,
					})
				}
			}
		}
	}
	s.mu.Unlock()

	// Reported after the locks drop, like Rust's post-loop event emit.
	if len(changes) > 0 && s.server.ChangeCallback != nil {
		s.server.ChangeCallback(changes)
	}
}

// applyMutation steps one point, returning the new triangle-wave direction. The
// second result is false when the device vanished, in which case Rust leaves the
// stored direction untouched.
func (s *Simulator) applyMutation(slaveID uint8, def register.RegisterDef, rt *mutationRuntime) (mutation.Direction, bool) {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	device, ok := s.server.devices[slaveID]
	if !ok {
		return rt.direction, false
	}
	return mutation.ApplyPointMutation(device.RegisterMap, def, rt.cfg, rt.direction), true
}

// applySource writes a generated value and reports whether it landed. A data
// source never creates a register slot: Rust looks the slot up with get_mut and
// suppresses the event when the address is absent. Values for 32-bit points
// touch only the first word, as they do in Rust.
func (s *Simulator) applySource(slaveID uint8, def register.RegisterDef, value uint16) bool {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	device, ok := s.server.devices[slaveID]
	if !ok {
		return false
	}
	m := device.RegisterMap
	switch def.RegisterType {
	case register.Coil:
		if _, ok := m.Coils[def.Address]; !ok {
			return false
		}
		m.Coils[def.Address] = value != 0
	case register.DiscreteInput:
		if _, ok := m.DiscreteInputs[def.Address]; !ok {
			return false
		}
		m.DiscreteInputs[def.Address] = value != 0
	case register.HoldingRegType:
		if _, ok := m.HoldingRegisters[def.Address]; !ok {
			return false
		}
		m.HoldingRegisters[def.Address] = value
	case register.InputRegister:
		if _, ok := m.InputRegisters[def.Address]; !ok {
			return false
		}
		m.InputRegisters[def.Address] = value
	default:
		return false
	}
	return true
}

// mutationPeriod clamps a mutation period to the base tick, mirroring Rust's
// mutation_period.
func mutationPeriod(periodMS uint64) time.Duration {
	if periodMS < BaseTickMS {
		periodMS = BaseTickMS
	}
	return time.Duration(periodMS) * time.Millisecond
}

func simulatedSlaves(points map[uint8][]*pointRuntime) []uint8 {
	ids := make([]uint8, 0, len(points))
	for id := range points {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
