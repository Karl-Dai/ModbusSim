package slave

import (
	"sync"
	"testing"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

var simBase = time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

func simFixture(t *testing.T) (*Server, *Simulator) {
	t.Helper()
	server := NewServer()
	if err := server.AddDevice(NewDevice(1, "dev")); err != nil {
		t.Fatalf("AddDevice: %v", err)
	}
	return server, NewSimulator(server)
}

type changeRecorder struct {
	mu      sync.Mutex
	batches [][]Change
}

func (r *changeRecorder) callback(changes []Change) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches = append(r.batches, changes)
}

func (r *changeRecorder) all() []Change {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Change
	for _, batch := range r.batches {
		out = append(out, batch...)
	}
	return out
}

func (r *changeRecorder) batchCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.batches)
}

func mapRegister(addr uint16, dataType register.DataType) register.RegisterDef {
	return register.RegisterDef{
		Address:      addr,
		RegisterType: register.HoldingRegType,
		DataType:     dataType,
		Endian:       register.DefaultEndian,
	}
}

func coilRegister(addr uint16) register.RegisterDef {
	return register.RegisterDef{
		Address:      addr,
		RegisterType: register.Coil,
		DataType:     register.TypeBool,
		Endian:       register.DefaultEndian,
	}
}

func mutationCfg(mode mutation.Mode, periodMS uint64, step, min, max float64) *mutation.Config {
	return &mutation.Config{Enabled: true, Mode: mode, PeriodMs: periodMS, Step: step, Min: min, Max: max}
}

func fixedSource(value uint16, intervalMS uint64) *datasource.Config {
	return &datasource.Config{
		Source:           datasource.Source{Type: datasource.KindFixed, Value: value},
		UpdateIntervalMs: intervalMS,
	}
}

// holdingOf and coilValue read through the server lock so the wall-clock test
// can poll while a tick is running.
func holdingOf(t *testing.T, server *Server, addr uint16) (uint16, bool) {
	t.Helper()
	server.mu.RLock()
	defer server.mu.RUnlock()
	device, ok := server.devices[1]
	if !ok {
		t.Fatalf("device 1 missing")
	}
	value, ok := device.RegisterMap.HoldingRegisters[addr]
	return value, ok
}

func TestMutationTicksOnItsOwnPeriod(t *testing.T) {
	server, sim := simFixture(t)
	sim.SetMutationEnabled(true)
	sim.setPointsAt(1, []Behaviour{{
		Def:      coilRegister(3),
		Mutation: mutationCfg(mutation.ModeFlip, 250, 1, 0, 1),
	}}, simBase)

	sim.Tick(simBase)
	if coilValue(t, server, 3) {
		t.Fatalf("coil flipped before its period elapsed")
	}
	sim.Tick(simBase.Add(250 * time.Millisecond))
	if !coilValue(t, server, 3) {
		t.Fatalf("coil did not flip at its due time")
	}
	sim.Tick(simBase.Add(300 * time.Millisecond))
	if !coilValue(t, server, 3) {
		t.Fatalf("coil flipped twice inside one period")
	}
	sim.Tick(simBase.Add(500 * time.Millisecond))
	if coilValue(t, server, 3) {
		t.Fatalf("coil did not flip back at the second due time")
	}
}

// The mutation master switch gates mutation only; data sources keep ticking.
func TestMutationSwitchDoesNotStopDataSources(t *testing.T) {
	server, sim := simFixture(t)
	device, _ := server.GetDevice(1)
	device.RegisterMap.WriteHoldingRegister(0, 0)
	device.RegisterMap.WriteCoil(3, false)
	sim.setPointsAt(1, []Behaviour{
		{Def: coilRegister(3), Mutation: mutationCfg(mutation.ModeFlip, 100, 1, 0, 1)},
		{Def: mapRegister(0, register.TypeUInt16), DataSource: fixedSource(42, 100)},
	}, simBase)

	sim.Tick(simBase.Add(100 * time.Millisecond))
	if coilValue(t, server, 3) {
		t.Fatalf("mutation ran while the master switch was off")
	}
	if value, _ := holdingOf(t, server, 0); value != 42 {
		t.Fatalf("data source stopped with mutation off: got %d", value)
	}
}

func TestMutationPeriodClampsToBaseTick(t *testing.T) {
	server, sim := simFixture(t)
	sim.SetMutationEnabled(true)
	sim.setPointsAt(1, []Behaviour{{
		Def:      coilRegister(3),
		Mutation: mutationCfg(mutation.ModeFlip, 20, 1, 0, 1),
	}}, simBase)

	sim.Tick(simBase.Add(99 * time.Millisecond))
	if coilValue(t, server, 3) {
		t.Fatalf("period below the base tick fired early")
	}
	sim.Tick(simBase.Add(100 * time.Millisecond))
	if !coilValue(t, server, 3) {
		t.Fatalf("period below the base tick never fired")
	}
}

// Triangle modes walk a direction that the scheduler must carry between ticks.
func TestMutationTriangleWaveRemembersDirection(t *testing.T) {
	server, sim := simFixture(t)
	sim.SetMutationEnabled(true)
	sim.setPointsAt(1, []Behaviour{{
		Def:      mapRegister(7, register.TypeUInt16),
		Mutation: mutationCfg(mutation.ModeIncrement, 100, 4, 0, 10),
	}}, simBase)

	want := []uint16{4, 8, 10, 6, 2, 0, 4}
	for i, expected := range want {
		sim.Tick(simBase.Add(time.Duration((i+1)*100) * time.Millisecond))
		value, _ := holdingOf(t, server, 7)
		if value != expected {
			t.Fatalf("tick %d: got %d want %d", i+1, value, expected)
		}
	}
}

// Mutation creates slots, unlike a data source.
func TestMutationCreatesMissingSlots(t *testing.T) {
	server, sim := simFixture(t)
	sim.SetMutationEnabled(true)
	sim.setPointsAt(1, []Behaviour{{
		Def:      mapRegister(7, register.TypeUInt16),
		Mutation: mutationCfg(mutation.ModeFlip, 100, 1, 0, 100),
	}}, simBase)

	sim.Tick(simBase.Add(100 * time.Millisecond))
	value, ok := holdingOf(t, server, 7)
	if !ok {
		t.Fatalf("mutation did not create the holding register")
	}
	if value != 100 {
		t.Fatalf("got %d want 100", value)
	}
}

func TestDataSourceSkipsMissingSlots(t *testing.T) {
	server, sim := simFixture(t)
	recorder := &changeRecorder{}
	server.ChangeCallback = recorder.callback
	sim.setPointsAt(1, []Behaviour{{
		Def:        mapRegister(9, register.TypeUInt16),
		DataSource: fixedSource(42, 100),
	}}, simBase)

	for i := 1; i <= 3; i++ {
		sim.Tick(simBase.Add(time.Duration(i*100) * time.Millisecond))
	}
	if _, ok := holdingOf(t, server, 9); ok {
		t.Fatalf("data source created an unregistered holding register")
	}
	if len(recorder.all()) != 0 {
		t.Fatalf("suppressed write reported a change: %+v", recorder.all())
	}
}

// The source keeps producing while its write is suppressed, so a slot that
// appears later receives the value of its own tick, not the first one.
func TestDataSourceAdvancesWhileSuppressed(t *testing.T) {
	server, sim := simFixture(t)
	cfg := &datasource.Config{
		Source: datasource.Source{
			Type:         datasource.KindCsvPlayback,
			Values:       []uint16{1, 2, 3},
			LoopPlayback: true,
		},
		UpdateIntervalMs: 100,
	}
	sim.setPointsAt(1, []Behaviour{{Def: mapRegister(0, register.TypeUInt16), DataSource: cfg}}, simBase)

	sim.Tick(simBase.Add(100 * time.Millisecond)) // 1, suppressed
	device, _ := server.GetDevice(1)
	device.RegisterMap.WriteHoldingRegister(0, 0)
	sim.Tick(simBase.Add(200 * time.Millisecond)) // 2, lands
	if value, _ := holdingOf(t, server, 0); value != 2 {
		t.Fatalf("got %d want 2", value)
	}
}

func TestDataSourceCounterSequence(t *testing.T) {
	server, sim := simFixture(t)
	device, _ := server.GetDevice(1)
	device.RegisterMap.WriteHoldingRegister(0, 0)
	recorder := &changeRecorder{}
	server.ChangeCallback = recorder.callback
	sim.setPointsAt(1, []Behaviour{{
		Def: mapRegister(0, register.TypeUInt16),
		DataSource: &datasource.Config{
			Source:           datasource.Source{Type: datasource.KindCounter, Start: 5, Step: 1},
			UpdateIntervalMs: 100,
		},
	}}, simBase)

	sim.Tick(simBase.Add(100 * time.Millisecond))
	sim.Tick(simBase.Add(200 * time.Millisecond))
	changes := recorder.all()
	if len(changes) != 2 {
		t.Fatalf("got %d changes want 2", len(changes))
	}
	if changes[0].Value != 5 || changes[1].Value != 6 {
		t.Fatalf("counter sequence %+v want 5 then 6", changes)
	}
	for _, change := range changes {
		if change.SlaveID != 1 || change.Address != 0 || change.RegisterType != register.HoldingRegType {
			t.Fatalf("unexpected change %+v", change)
		}
	}
}

// Bit areas take value != 0 while the reported change keeps the raw u16.
func TestDataSourceBitAreaReportsRawValue(t *testing.T) {
	server, sim := simFixture(t)
	device, _ := server.GetDevice(1)
	device.RegisterMap.WriteCoil(1, false)
	recorder := &changeRecorder{}
	server.ChangeCallback = recorder.callback
	sim.setPointsAt(1, []Behaviour{{Def: coilRegister(1), DataSource: fixedSource(7, 100)}}, simBase)

	sim.Tick(simBase.Add(100 * time.Millisecond))
	if !coilValue(t, server, 1) {
		t.Fatalf("coil not set from raw value")
	}
	changes := recorder.all()
	if len(changes) != 1 || changes[0].Value != 7 {
		t.Fatalf("got %+v want one change with value 7", changes)
	}
}

func TestChangesBatchIntoOneCallback(t *testing.T) {
	server, sim := simFixture(t)
	device, _ := server.GetDevice(1)
	device.RegisterMap.WriteHoldingRegister(0, 0)
	device.RegisterMap.WriteHoldingRegister(1, 0)
	recorder := &changeRecorder{}
	server.ChangeCallback = recorder.callback
	sim.setPointsAt(1, []Behaviour{
		{Def: mapRegister(0, register.TypeUInt16), DataSource: fixedSource(11, 100)},
		{Def: mapRegister(1, register.TypeUInt16), DataSource: fixedSource(22, 100)},
	}, simBase)

	sim.Tick(simBase.Add(100 * time.Millisecond))
	if recorder.batchCount() != 1 {
		t.Fatalf("got %d callbacks want 1", recorder.batchCount())
	}
	changes := recorder.all()
	if len(changes) != 2 || changes[0].Value != 11 || changes[1].Value != 22 {
		t.Fatalf("got %+v want 11 then 22 in registration order", changes)
	}
}

func TestSetPointsReplacesSchedulingState(t *testing.T) {
	server, sim := simFixture(t)
	device, _ := server.GetDevice(1)
	device.RegisterMap.WriteCoil(3, false)
	sim.SetMutationEnabled(true)
	sim.setPointsAt(1, []Behaviour{{
		Def:      coilRegister(3),
		Mutation: mutationCfg(mutation.ModeFlip, 100, 1, 0, 1),
	}}, simBase)
	sim.SetPoints(1, nil)

	sim.Tick(simBase.Add(100 * time.Millisecond))
	if coilValue(t, server, 3) {
		t.Fatalf("cleared points still mutated")
	}
}

// Disabled mutation carries no runtime, matching Rust's enabled filter.
func TestDisabledMutationIsNotScheduled(t *testing.T) {
	server, sim := simFixture(t)
	cfg := mutationCfg(mutation.ModeFlip, 100, 1, 0, 1)
	cfg.Enabled = false
	sim.SetMutationEnabled(true)
	sim.setPointsAt(1, []Behaviour{{Def: coilRegister(3), Mutation: cfg}}, simBase)

	sim.Tick(simBase.Add(100 * time.Millisecond))
	if _, ok := deviceCoil(t, server, 3); ok {
		t.Fatalf("disabled mutation wrote a coil")
	}
}

func TestSimulatorRunsOnWallClock(t *testing.T) {
	server, sim := simFixture(t)
	device, _ := server.GetDevice(1)
	device.RegisterMap.WriteHoldingRegister(0, 0)
	sim.setPointsAt(1, []Behaviour{{
		Def: mapRegister(0, register.TypeUInt16),
		DataSource: &datasource.Config{
			Source:           datasource.Source{Type: datasource.KindCounter, Start: 1, Step: 1},
			UpdateIntervalMs: BaseTickMS,
		},
	}}, time.Now())

	sim.Start()
	deadline := time.Now().Add(2 * time.Second)
	var advanced uint16
	for time.Now().Before(deadline) {
		if advanced, _ = holdingOf(t, server, 0); advanced >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	sim.Stop()
	if advanced < 3 {
		t.Fatalf("wall-clock ticker never advanced the counter: got %d", advanced)
	}

	if again, _ := holdingOf(t, server, 0); again != advanced {
		t.Fatalf("counter kept advancing after Stop: %d then %d", advanced, again)
	}
}

func coilValue(t *testing.T, server *Server, addr uint16) bool {
	t.Helper()
	value, _ := deviceCoil(t, server, addr)
	return value
}

func deviceCoil(t *testing.T, server *Server, addr uint16) (bool, bool) {
	t.Helper()
	server.mu.RLock()
	defer server.mu.RUnlock()
	device, ok := server.devices[1]
	if !ok {
		t.Fatalf("device 1 missing")
	}
	value, ok := device.RegisterMap.Coils[addr]
	return value, ok
}
