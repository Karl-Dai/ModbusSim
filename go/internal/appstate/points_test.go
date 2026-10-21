package appstate

import (
	"math"
	"testing"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// mutationEnabled is a word-point mutation that steps once per base tick, so a
// test can see it move without waiting on a long period.
func mutationEnabled() mutation.Config {
	return mutation.Config{
		Enabled:  true,
		Mode:     mutation.ModeIncrement,
		PeriodMs: mutation.BaseTickMS,
		Step:     1,
		Min:      0,
		Max:      1000,
	}
}

// emptyConnection returns a connection whose only device has no points, so tests
// can define registers at known addresses without the default 20001-point sweep.
func emptyConnection(t *testing.T, slaveID uint8) *Manager {
	t.Helper()
	rec := &recorded{}
	m, _ := newTestConnection(t, rec)
	if _, err := m.AddDevice(AddDeviceRequest{ConnectionID: "slave_1", SlaveID: slaveID}); err != nil {
		t.Fatal(err)
	}
	return m
}

func mustAddRegister(t *testing.T, m *Manager, slaveID uint8, address uint16, area, dataType string) {
	t.Helper()
	if err := m.AddRegister(AddRegisterRequest{
		ConnectionID: "slave_1", SlaveID: slaveID, Address: address, RegisterType: area, DataType: dataType,
	}); err != nil {
		t.Fatalf("add register %s@%d: %v", area, address, err)
	}
}

func readValue(t *testing.T, m *Manager, slaveID uint8, area string, address uint16) uint16 {
	return readValueFrom(t, m, "slave_1", slaveID, area, address)
}

func readValueFrom(t *testing.T, m *Manager, connectionID string, slaveID uint8, area string, address uint16) uint16 {
	t.Helper()
	info, err := m.ReadRegister(connectionID, slaveID, area, address)
	if err != nil {
		t.Fatal(err)
	}
	return info.Value
}

// waitFor polls until condition holds; the scheduler runs on real time so tests
// cannot assert on a tick count.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPointMutationNormalization(t *testing.T) {
	m := emptyConnection(t, 30)
	mustAddRegister(t, m, 30, 3, "holding_register", "uint16")
	mustAddRegister(t, m, 30, 4, "coil", "bool")

	request := func(address uint16, area string, config mutation.Config) error {
		return m.SetPointMutation(SetPointMutationRequest{
			ConnectionID: "slave_1", SlaveID: 30, RegisterType: area, Address: address, Config: config,
		})
	}

	bad := mutationEnabled()
	bad.Min, bad.Max = 10, 0
	if err := request(3, "holding_register", bad); err == nil ||
		err.Error() != "mutation min must not exceed max" {
		t.Errorf("inverted bounds: err = %v", err)
	}
	nan := mutationEnabled()
	nan.Min = math.NaN()
	if err := request(3, "holding_register", nan); err == nil ||
		err.Error() != "mutation min, max and step must be finite" {
		t.Errorf("non-finite min: err = %v", err)
	}
	zeroStep := mutationEnabled()
	zeroStep.Step = 0
	if err := request(3, "holding_register", zeroStep); err == nil ||
		err.Error() != "mutation step must be greater than zero" {
		t.Errorf("zero step: err = %v", err)
	}

	// Bit points flip whatever the caller asked for, and their period is clamped
	// up to the scheduler resolution.
	coil := mutation.Config{Enabled: true, Mode: mutation.ModeIncrement, PeriodMs: 1, Step: -1, Min: 9, Max: 2}
	if err := request(4, "coil", coil); err != nil {
		t.Fatal(err)
	}
	listed, err := m.ListPointMutations("slave_1", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Address != 4 || listed[0].RegisterType != register.Coil {
		t.Fatalf("mutations = %+v, want only the coil", listed)
	}
	if listed[0].Mode != mutation.ModeFlip || listed[0].PeriodMS != mutation.BaseTickMS {
		t.Errorf("coil mutation = %+v, want flip at %d ms", listed[0], mutation.BaseTickMS)
	}
	if listed[0].Min != 9 || listed[0].Step != -1 {
		t.Errorf("bit mutation numeric fields were validated: %+v", listed[0])
	}

	if err := request(999, "holding_register", mutationEnabled()); err == nil ||
		err.Error() != "register holding_register@999 not found" {
		t.Errorf("missing point: err = %v", err)
	}
	if err := request(3, "discrete_input", mutationEnabled()); err == nil ||
		err.Error() != "register discrete_input@3 not found" {
		t.Errorf("wrong area: err = %v", err)
	}
	if err := request(3, "screwdriver", mutationEnabled()); err == nil ||
		err.Error() != "unknown register type: screwdriver" {
		t.Errorf("bad area name: err = %v", err)
	}
	if err := m.SetPointMutation(SetPointMutationRequest{
		ConnectionID: "nope", SlaveID: 30, RegisterType: "holding_register", Address: 3, Config: mutationEnabled(),
	}); err == nil || err.Error() != "connection nope not found" {
		t.Errorf("bad connection: err = %v", err)
	}
}

func TestMutationAndDataSourceConflict(t *testing.T) {
	m := emptyConnection(t, 31)
	mustAddRegister(t, m, 31, 6, "holding_register", "uint16")
	setSource := func() error {
		return m.SetDataSource(SetDataSourceRequest{
			ConnectionID: "slave_1", SlaveID: 31, RegisterType: "holding_register", Address: 6,
			Source: datasource.Source{Type: datasource.KindFixed, Value: 7}, UpdateIntervalMS: 1000,
		})
	}

	if err := m.SetPointMutation(SetPointMutationRequest{
		ConnectionID: "slave_1", SlaveID: 31, RegisterType: "holding_register", Address: 6, Config: mutationEnabled(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := setSource(); err == nil || err.Error() != "disable point mutation before setting a data source" {
		t.Fatalf("source over mutation: err = %v", err)
	}
	if err := m.ClearPointMutation("slave_1", 31, "holding_register", 6); err != nil {
		t.Fatal(err)
	}
	if err := setSource(); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPointMutation(SetPointMutationRequest{
		ConnectionID: "slave_1", SlaveID: 31, RegisterType: "holding_register", Address: 6, Config: mutationEnabled(),
	}); err == nil || err.Error() != "remove the point data source before enabling mutation" {
		t.Fatalf("mutation over source: err = %v", err)
	}

	// Disabling a mutation keeps the data source, and vice versa.
	if err := m.SetPointMutation(SetPointMutationRequest{
		ConnectionID: "slave_1", SlaveID: 31, RegisterType: "holding_register", Address: 6,
		Config: mutation.Config{Enabled: false},
	}); err != nil {
		t.Fatal(err)
	}
	sources, err := m.ListDataSources("slave_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("data sources after disabling mutation = %+v", sources)
	}
	if err := m.RemoveDataSource("slave_1", 31, "holding_register", 6); err != nil {
		t.Fatal(err)
	}
	if sources, err := m.ListDataSources("slave_1"); err != nil || len(sources) != 0 {
		t.Errorf("data sources after removal = %+v (%v)", sources, err)
	}
	// Both drivers gone: the point leaves the registry entirely.
	if mutations, err := m.ListPointMutations("slave_1", 31); err != nil || len(mutations) != 0 {
		t.Errorf("mutations = %+v (%v)", mutations, err)
	}
	conn := connectionFor(t, m, "slave_1")
	if len(conn.points) != 0 {
		t.Errorf("point registry = %+v, want empty", conn.points)
	}
	if err := m.RemoveDataSource("slave_1", 31, "holding_register", 6); err != nil {
		t.Errorf("removing an absent source: err = %v", err)
	}
	if err := m.RemoveDataSource("slave_1", 31, "holding_register", 999); err == nil ||
		err.Error() != "register not found" {
		t.Errorf("removing a missing point: err = %v", err)
	}
}

func TestDataSourceValidation(t *testing.T) {
	m := emptyConnection(t, 32)
	mustAddRegister(t, m, 32, 8, "holding_register", "uint16")

	cases := []struct {
		name   string
		source datasource.Source
		want   string
	}{
		{"min above max", datasource.Source{Type: datasource.KindRandom, Min: 5, Max: 1}, "data source min must not exceed max"},
		{"negative frequency", datasource.Source{Type: datasource.KindSine, Frequency: -1}, "sine parameters must be finite and frequency must be non-negative"},
		{"zero sawtooth period", datasource.Source{Type: datasource.KindSawtooth, PeriodMs: 0}, "sawtooth period must be greater than zero"},
		{"short triangle period", datasource.Source{Type: datasource.KindTriangle, PeriodMs: 1}, "triangle period must be at least 2 ms"},
		{"empty CSV", datasource.Source{Type: datasource.KindCsvPlayback}, "CSV playback requires at least one value"},
	}
	for _, tc := range cases {
		err := m.SetDataSource(SetDataSourceRequest{
			ConnectionID: "slave_1", SlaveID: 32, RegisterType: "holding_register", Address: 8,
			Source: tc.source, UpdateIntervalMS: 500,
		})
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	if err := m.SetDataSource(SetDataSourceRequest{
		ConnectionID: "slave_1", SlaveID: 32, RegisterType: "holding_register", Address: 8,
		Source: datasource.Source{Type: datasource.KindFixed, Value: 3},
	}); err == nil || err.Error() != "data source update interval must be greater than zero" {
		t.Errorf("zero interval: err = %v", err)
	}
	if err := m.SetDataSource(SetDataSourceRequest{
		ConnectionID: "slave_1", SlaveID: 32, RegisterType: "holding_register", Address: 999,
		Source: datasource.Source{Type: datasource.KindFixed, Value: 3}, UpdateIntervalMS: 500,
	}); err == nil || err.Error() != "register holding_register@999 not found" {
		t.Errorf("missing point: err = %v", err)
	}

	// The stored config echoes back with its scheduling parameters intact.
	if err := m.SetDataSource(SetDataSourceRequest{
		ConnectionID: "slave_1", SlaveID: 32, RegisterType: "holding_register", Address: 8,
		Source:           datasource.Source{Type: datasource.KindCounter, Start: 100, Step: 2, Wrap: true},
		UpdateIntervalMS: 250,
	}); err != nil {
		t.Fatal(err)
	}
	sources, err := m.ListDataSources("slave_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %+v", sources)
	}
	got := sources[0]
	if got.ConnectionID != "slave_1" || got.SlaveID != 32 || got.Address != 8 ||
		got.RegisterType != register.HoldingRegType || got.Config.UpdateIntervalMs != 250 ||
		got.Config.Source.Type != datasource.KindCounter || !got.Config.Source.Wrap {
		t.Errorf("source = %+v", got)
	}
}

func TestPointMutationDrivesRegistersWhileRunning(t *testing.T) {
	rec := &recorded{}
	m, _ := newTestConnection(t, rec)
	if _, err := m.AddDevice(AddDeviceRequest{ConnectionID: "slave_1", SlaveID: 33}); err != nil {
		t.Fatal(err)
	}
	mustAddRegister(t, m, 33, 9, "holding_register", "uint16")
	if err := m.SetPointMutation(SetPointMutationRequest{
		ConnectionID: "slave_1", SlaveID: 33, RegisterType: "holding_register", Address: 9, Config: mutationEnabled(),
	}); err != nil {
		t.Fatal(err)
	}

	conn := connectionFor(t, m, "slave_1")
	if conn.simulator.MutationEnabled() {
		t.Fatal("a new manager must leave the mutation switch off")
	}
	// With the app-wide switch off the point stays put, but it is still scheduled.
	time.Sleep(3 * mutation.BaseTickMS * time.Millisecond)
	if value := readValue(t, m, 33, "holding_register", 9); value != 0 {
		t.Fatalf("value mutated while the switch was off: %d", value)
	}
	if mutations, err := m.ListPointMutations("slave_1", 33); err != nil || len(mutations) != 1 {
		t.Fatalf("mutations = %+v (%v)", mutations, err)
	}

	m.SetMutationRunning(true)
	waitFor(t, "mutation to step the register", func() bool {
		return readValue(t, m, 33, "holding_register", 9) > 0
	})

	// Toggling a connection's transport does not disturb scheduling; disabling
	// the switch freezes the value where it is.
	m.SetMutationRunning(false)
	if err := m.StartConnection("slave_1"); err != nil {
		t.Fatal(err)
	}
	stopped := readValue(t, m, 33, "holding_register", 9)
	time.Sleep(3 * mutation.BaseTickMS * time.Millisecond)
	if value := readValue(t, m, 33, "holding_register", 9); value != stopped {
		t.Errorf("value moved from %d to %d with mutation disabled", stopped, value)
	}
	if m.MutationRunning() {
		t.Errorf("MutationRunning disagreed with the last SetMutationRunning")
	}
	// A later enable resumes from the stored value instead of the minimum.
	m.SetMutationRunning(true)
	waitFor(t, "mutation to resume", func() bool {
		return readValue(t, m, 33, "holding_register", 9) > stopped
	})
}

func TestDataSourceGeneratesValuesAndNotifies(t *testing.T) {
	rec := &recorded{}
	m, _ := newTestConnection(t, rec)
	if _, err := m.AddDevice(AddDeviceRequest{ConnectionID: "slave_1", SlaveID: 34}); err != nil {
		t.Fatal(err)
	}
	mustAddRegister(t, m, 34, 11, "holding_register", "uint16")
	if err := m.SetDataSource(SetDataSourceRequest{
		ConnectionID: "slave_1", SlaveID: 34, RegisterType: "holding_register", Address: 11,
		Source:           datasource.Source{Type: datasource.KindCounter, Start: 500, Step: 1, Wrap: true},
		UpdateIntervalMS: mutation.BaseTickMS,
	}); err != nil {
		t.Fatal(err)
	}

	// Data sources run even while the mutation switch is off.
	first := readValue(t, m, 34, "holding_register", 11)
	waitFor(t, "the counter source to advance", func() bool {
		return readValue(t, m, 34, "holding_register", 11) != first
	})
	waitFor(t, "generated values to be reported to the UI", func() bool {
		return rec.countChanges(11) > 0
	})

	if err := m.RemoveDataSource("slave_1", 34, "holding_register", 11); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteRegister(WriteRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 34, RegisterType: "holding_register", Address: 11, Value: 4242,
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * mutation.BaseTickMS * time.Millisecond)
	if value := readValue(t, m, 34, "holding_register", 11); value != 4242 {
		t.Errorf("value = %d, want the manual write to stick after the source was removed", value)
	}
}

func TestPointConfigFollowsRegisterEdit(t *testing.T) {
	m := emptyConnection(t, 35)
	mustAddRegister(t, m, 35, 20, "holding_register", "uint16")
	mustAddRegister(t, m, 35, 20, "input_register", "uint16")
	if err := m.SetPointMutation(SetPointMutationRequest{
		ConnectionID: "slave_1", SlaveID: 35, RegisterType: "holding_register", Address: 20, Config: mutationEnabled(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDataSource(SetDataSourceRequest{
		ConnectionID: "slave_1", SlaveID: 35, RegisterType: "input_register", Address: 20,
		Source:           datasource.Source{Type: datasource.KindFixed, Value: 3},
		UpdateIntervalMS: mutation.BaseTickMS,
	}); err != nil {
		t.Fatal(err)
	}

	// Moving a point re-keys exactly its own config; the other area is untouched.
	if err := m.UpdateRegister(UpdateRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 35,
		OriginalAddress: 20, OriginalRegisterType: "holding_register",
		Address: 21, RegisterType: "holding_register", DataType: "uint16",
	}); err != nil {
		t.Fatal(err)
	}
	m.SetMutationRunning(true)
	waitFor(t, "the moved point's mutation to keep running", func() bool {
		return readValue(t, m, 35, "holding_register", 21) > 0
	})
	if readValue(t, m, 35, "input_register", 20) != 3 {
		t.Error("the untouched point lost its data source")
	}
	conn := connectionFor(t, m, "slave_1")
	if len(conn.points) != 2 {
		t.Fatalf("point registry = %+v", conn.points)
	}
}
