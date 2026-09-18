// Integration tests for the app layer: connection lifecycle, device and
// register management, project save/load round trip, mutation and data
// source ticks, and an end-to-end master poll through a live connection.
package app

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func newTestState(t *testing.T) (*State, string) {
	t.Helper()
	s := NewState()
	id := s.NextConnectionID()
	conn := NewConnection(
		TransportConfig{Type: "tcp", Host: "127.0.0.1", Port: 0},
		SlaveTLSConfig{},
	)
	if err := conn.AddDevice(1, "dev", ""); err != nil {
		t.Fatal(err)
	}
	s.AddConnection(id, conn)
	return s, id
}

func TestConnectionLifecycle(t *testing.T) {
	s, id := newTestState(t)
	conn, _ := s.Connection(id)

	if err := conn.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if conn.State() != StateRunning {
		t.Fatalf("state = %s", conn.State())
	}
	if err := conn.Stop(); err != nil {
		t.Fatal(err)
	}
	if conn.State() != StateStopped {
		t.Fatalf("after stop = %s", conn.State())
	}
}

func TestDeleteConnectionCleansUp(t *testing.T) {
	s, id := newTestState(t)
	key := ParsePointKey(id, 1, register.HoldingRegType, 0)
	cfg := datasource.Config{Source: datasource.Source{Type: datasource.KindFixed, Value: 1}, UpdateIntervalMs: 1000}
	if err := s.SetDataSource(key, cfg); err != nil {
		t.Fatal(err)
	}
	if len(s.DataSourceKeys()) != 1 {
		t.Fatal("data source should be registered")
	}
	if err := s.DeleteConnection(id); err != nil {
		t.Fatal(err)
	}
	if len(s.DataSourceKeys()) != 0 {
		t.Fatal("data sources should be cleaned with connection")
	}
	if _, ok := s.Connection(id); ok {
		t.Fatal("connection should be gone")
	}
}

func TestRegisterCRUDAndValidation(t *testing.T) {
	s, id := newTestState(t)

	// Overlap must be rejected (default device owns 0..20000 in all areas).
	err := s.AddRegister(AddRegisterRequest{
		ConnectionID: id, SlaveID: 1, Address: 0,
		RegisterType: "holding_register", DataType: "uint16",
	})
	if err == nil {
		t.Fatal("overlapping register should be rejected")
	}

	// Valid addition outside the default range.
	err = s.AddRegister(AddRegisterRequest{
		ConnectionID: id, SlaveID: 1, Address: 30000,
		RegisterType: "holding_register", DataType: "uint16", Name: strPtr("spare"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defs, err := s.ListRegisters(id, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range defs {
		if d.Address == 30000 && d.Name == "spare" {
			found = true
		}
	}
	if !found {
		t.Fatal("added register missing from list")
	}

	// Write and read back.
	if err := s.WriteRegister(id, 1, register.HoldingRegType, 30000, 0xBEEF); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.ReadRegister(id, 1, register.HoldingRegType, 30000); !ok || v != 0xBEEF {
		t.Fatalf("read = %d %v", v, ok)
	}
}

func strPtr(s string) *string { return &s }

func TestUpdateRegisterPreservesMutation(t *testing.T) {
	s, id := newTestState(t)

	// Seed a mutation on holding@0 via SetPointMutation.
	key := ParsePointKey(id, 1, register.HoldingRegType, 0)
	cfg := register.MutationConfig{
		Enabled: true, Mode: register.MutationModeIncrement,
		PeriodMs: 100, Step: 1, Min: 0, Max: 10,
	}
	if err := s.SetPointMutation(key, cfg); err != nil {
		t.Fatal(err)
	}

	// Update the same point (rename), preserving the mutation.
	empty := ""
	if err := s.UpdateRegister(AddRegisterRequest{
		ConnectionID: id, SlaveID: 1, Address: 0,
		RegisterType: "holding_register", DataType: "uint16",
		Name: &empty, Comment: &empty,
	}, 0, "holding_register"); err != nil {
		t.Fatal(err)
	}

	defs, _ := s.ListRegisters(id, 1)
	var mut *register.MutationConfig
	for _, d := range defs {
		if d.Address == 0 && d.RegisterType == register.HoldingRegType {
			mut = d.Mutation
		}
	}
	if mut == nil || !mut.Enabled || mut.PeriodMs != 100 {
		t.Fatalf("mutation after update = %+v", mut)
	}
	if len(s.ListPointMutations()) != 1 {
		t.Fatalf("runtime mutations = %v", s.ListPointMutations())
	}
}

func TestMutationTick(t *testing.T) {
	s, id := newTestState(t)
	key := ParsePointKey(id, 1, register.HoldingRegType, 0)
	cfg := register.MutationConfig{
		Enabled: true, Mode: register.MutationModeIncrement,
		PeriodMs: 100, Step: 2, Min: 0, Max: 100,
	}
	if err := s.SetPointMutation(key, cfg); err != nil {
		t.Fatal(err)
	}
	s.SetMutationRunning(true)

	before, _ := s.ReadRegister(id, 1, register.HoldingRegType, 0)
	now := time.Now().Add(1 * time.Second)
	if n := s.MutationTick(now); n != 1 {
		t.Fatalf("applied = %d", n)
	}
	after, _ := s.ReadRegister(id, 1, register.HoldingRegType, 0)
	want := uint16(int(before) + 2)
	if after != want {
		t.Fatalf("value = %d, want %d", after, before+2)
	}

	// Master switch off -> no ticks.
	s.SetMutationRunning(false)
	if n := s.MutationTick(time.Now().Add(1 * time.Second)); n != 0 {
		t.Fatalf("applied while off = %d", n)
	}
}

func TestDataSourceTick(t *testing.T) {
	s, id := newTestState(t)
	key := ParsePointKey(id, 1, register.HoldingRegType, 0)
	cfg := datasource.Config{
		Source:           datasource.Source{Type: datasource.KindCounter, Start: 10, Step: 3},
		UpdateIntervalMs: 100,
	}
	if err := s.SetDataSource(key, cfg); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(1 * time.Second)
	if n := s.DataSourceTick(now); n != 1 {
		t.Fatalf("applied = %d", n)
	}
	v, _ := s.ReadRegister(id, 1, register.HoldingRegType, 0)
	if v != 10 {
		t.Fatalf("first tick value = %d, want 10 (start)", v)
	}
	if n := s.DataSourceTick(now.Add(1 * time.Second)); n != 1 {
		t.Fatalf("second applied = %d", n)
	}
	v, _ = s.ReadRegister(id, 1, register.HoldingRegType, 0)
	if v != 13 {
		t.Fatalf("second tick value = %d, want 13", v)
	}
}

func TestProjectSaveLoadRoundTrip(t *testing.T) {
	s, id := newTestState(t)
	conn, _ := s.Connection(id)
	dev0, ok := conn.Server().GetDevice(1)
	if !ok {
		t.Fatal("device missing")
	}
	dev0.RegisterMap.WriteHoldingRegister(0, 0x1234)
	dev0.RegisterMap.WriteCoil(3, true)

	// A mutation and a data source to verify persistence.
	if err := s.SetPointMutation(ParsePointKey(id, 1, register.HoldingRegType, 5), register.MutationConfig{
		Enabled: true, Mode: register.MutationModeFlip, PeriodMs: 500, Step: 1, Min: 0, Max: 1,
	}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "proj.modbusproj")
	if err := s.SaveProjectFile(path); err != nil {
		t.Fatal(err)
	}

	s2 := NewState()
	n, err := s2.LoadProjectFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("devices loaded = %d", n)
	}
	if _, ok := s2.Connection(id); !ok {
		t.Fatal("connection id not restored")
	}
	d2, ok := conn2Device(s2, id, 1)
	if !ok {
		t.Fatal("device not restored")
	}
	if got := d2.RegisterMap.ReadHoldingRegisters(0, 1)[0]; got != 0x1234 {
		t.Fatalf("holding 0 = %04X", got)
	}
	if !d2.RegisterMap.ReadCoils(3, 1)[0] {
		t.Fatal("coil 3 not restored")
	}
	if len(s2.ListPointMutations()) != 1 {
		t.Fatalf("mutations = %v", s2.ListPointMutations())
	}
	if s2.MutationRunning() {
		t.Fatal("mutation should start paused")
	}
}

func TestLoadProjectRejectsMaster(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.modbusproj")
	data := map[string]interface{}{"version": 1, "type": "master", "connections": []interface{}{}}
	b, _ := json.Marshal(data)
	if err := writeFile(path, b); err != nil {
		t.Fatal(err)
	}
	s := NewState()
	if _, err := s.LoadProjectFile(path); err == nil {
		t.Fatal("master project should be rejected")
	}
}

func TestToolFunctions(t *testing.T) {
	// CRC16 of "01 03 00 00 00 02" is a known vector; verify format only.
	crc, err := CalculateCRC16("01 03 00 00 00 02")
	if err != nil || len(crc) != 4 {
		t.Fatalf("crc = %q, %v", crc, err)
	}
	lrc, err := CalculateLRC("11 03 00 6B 00 03")
	if err != nil || len(lrc) != 2 {
		t.Fatalf("lrc = %q, %v", lrc, err)
	}
	if _, err := ParseHex("zz"); err == nil {
		t.Fatal("bad hex should fail")
	}

	// PLC conversions: 40001 -> holding register 1 (1-based within area).
	rt, addr, err := ConvertPlcToModbus(40001)
	if err != nil || rt != register.HoldingRegType || addr != 1 {
		t.Fatalf("plc->modbus = %s %d %v", rt, addr, err)
	}
	plc, err := ConvertModbusToPlc(1, "holding_register")
	if err != nil || plc != 40001 {
		t.Fatalf("modbus->plc = %d %v", plc, err)
	}
}

func TestLogsPaginated(t *testing.T) {
	s, id := newTestState(t)
	log := s.LogCollector(id)
	log.TryAdd(logcolEntryFor("tx", "one"))
	log.TryAdd(logcolEntryFor("rx", "two"))

	all, err := s.GetLogs(id, 0, 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("logs = %v, %v", all, err)
	}
	page, err := s.GetLogs(id, 1, 1)
	if err != nil || len(page) != 1 || page[0].Detail != "two" {
		t.Fatalf("page = %v, %v", page, err)
	}
	if err := s.ClearLogs(id); err != nil {
		t.Fatal(err)
	}
	all, _ = s.GetLogs(id, 0, 0)
	if len(all) != 0 {
		t.Fatal("logs should be cleared")
	}
}

func TestMasterPollsLiveAppConnection(t *testing.T) {
	s, id := newTestState(t)
	conn, _ := s.Connection(id)

	// Bind to a real port and run the listener.
	transport := conn.Transport()
	transport.Port = freePort(t)
	setTransport(t, s, id, transport)
	if err := conn.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Stop() }()

	time.Sleep(50 * time.Millisecond)
	dev, _ := conn.Server().GetDevice(1)
	dev.RegisterMap.WriteHoldingRegisters(100, []uint16{0xABCD})

	masterConn := master.NewConnection(
		master.Config{TargetAddress: "127.0.0.1", Port: transport.Port, SlaveID: 1, TimeoutMs: 2000, Requests: master.DefaultRequestSettings()},
		master.Transport{Type: master.TransportTCP, Host: "127.0.0.1", Port: transport.Port},
	)
	if err := masterConn.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer masterConn.Disconnect()
	res, err := masterConn.Read(t.Context(), master.ReadHoldingRegisters, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Registers[0] != 0xABCD {
		t.Fatalf("read = %04X", res.Registers[0])
	}
}
