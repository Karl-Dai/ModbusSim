package appstate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/project"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// deviceOf hands a test the live device so it can inspect the register map the
// commands themselves only expose one address at a time.
func deviceOf(t *testing.T, m *Manager, slaveID uint8) *slave.Device {
	t.Helper()
	device, ok := connectionFor(t, m, "slave_1").server.GetDevice(slaveID)
	if !ok {
		t.Fatalf("slave %d missing", slaveID)
	}
	return device
}

func hasSlot(m *register.RegisterMap, area string, address uint16) bool {
	switch register.RegisterType(area) {
	case register.Coil:
		_, ok := m.Coils[address]
		return ok
	case register.DiscreteInput:
		_, ok := m.DiscreteInputs[address]
		return ok
	case register.HoldingRegType:
		_, ok := m.HoldingRegisters[address]
		return ok
	case register.InputRegister:
		_, ok := m.InputRegisters[address]
		return ok
	}
	return false
}

func TestAddRegisterValidatesBeforeAllocating(t *testing.T) {
	m := emptyConnection(t, 40)

	// A 32-bit point claims two words, so the next address is already taken.
	if err := m.AddRegister(AddRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 40, Address: 100, RegisterType: "holding_register", DataType: "uint32",
	}); err != nil {
		t.Fatal(err)
	}
	dup := AddRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 40, Address: 101, RegisterType: "holding_register", DataType: "uint32",
	}
	if err := m.AddRegister(dup); err == nil ||
		err.Error() != "register holding_register@100 overlaps point at address 101" {
		t.Errorf("overlap: err = %v", err)
	}
	defs, err := m.ListRegisters("slave_1", 40, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("definitions after a rejected add = %+v", defs)
	}
	// 101 belongs to the first point; 102 would only exist if the rejected one
	// had been allocated too.
	mapAfter := deviceOf(t, m, 40).RegisterMap
	if !hasSlot(mapAfter, "holding_register", 101) || hasSlot(mapAfter, "holding_register", 102) {
		t.Errorf("slots after a rejected add: 101=%v 102=%v",
			hasSlot(mapAfter, "holding_register", 101), hasSlot(mapAfter, "holding_register", 102))
	}

	if err := m.AddRegister(AddRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 40, Address: 65535, RegisterType: "input_register", DataType: "uint32",
	}); err == nil || err.Error() != "register input_register@65535 exceeds address 65535" {
		t.Errorf("wide point at the top address: err = %v", err)
	}
	for _, bad := range []AddRegisterRequest{
		{ConnectionID: "slave_1", SlaveID: 40, RegisterType: "holding_register", DataType: "uint128"},
		{ConnectionID: "slave_1", SlaveID: 40, RegisterType: "holding", DataType: "uint16"},
		{ConnectionID: "slave_1", SlaveID: 40, RegisterType: "holding_register", DataType: "uint16", Endian: "sideways"},
		{ConnectionID: "slave_1", SlaveID: 99, RegisterType: "holding_register", DataType: "uint16"},
	} {
		if err := m.AddRegister(bad); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
	if err := m.AddRegister(AddRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 40, Address: 7, RegisterType: "holding_register", DataType: "float32",
	}); err != nil {
		t.Fatal(err)
	}
	// Rust's Option<Endian> defaults to big when the field is absent.
	defs, err = m.ListRegisters("slave_1", 40, "holding_register")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 || defs[1].Endian != register.EndianBig || defs[1].DataType != register.TypeFloat {
		t.Errorf("definitions = %+v", defs)
	}
	if err := m.AddRegister(AddRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 40, Address: 20, RegisterType: "holding_register", DataType: "float32",
		Endian: "little",
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := m.ListRegisters("slave_1", 40, ""); err != nil || len(got) != 3 {
		t.Errorf("definitions = %+v (%v)", got, err)
	}
}

func TestUpdateRegisterMovesValuesAndSlots(t *testing.T) {
	m := emptyConnection(t, 41)
	mustAddRegister(t, m, 41, 10, "holding_register", "uint16")
	if err := m.WriteRegister(WriteRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 41, RegisterType: "holding_register", Address: 10, Value: 4242,
	}); err != nil {
		t.Fatal(err)
	}

	move := func(address uint16) error {
		return m.UpdateRegister(UpdateRegisterRequest{
			ConnectionID: "slave_1", SlaveID: 41,
			OriginalAddress: address - 1, OriginalRegisterType: "holding_register",
			Address: address, RegisterType: "holding_register", DataType: "uint16",
		})
	}
	if err := move(11); err != nil {
		t.Fatal(err)
	}
	mapAfter := deviceOf(t, m, 41).RegisterMap
	if !hasSlot(mapAfter, "holding_register", 11) || hasSlot(mapAfter, "holding_register", 10) {
		t.Errorf("slots after the move: 10=%v 11=%v", hasSlot(mapAfter, "holding_register", 10), hasSlot(mapAfter, "holding_register", 11))
	}
	if value := readValue(t, m, 41, "holding_register", 11); value != 4242 {
		t.Errorf("value after the move = %d, want the stored word to travel", value)
	}

	// Moving onto another point is refused and leaves the definition alone.
	mustAddRegister(t, m, 41, 20, "holding_register", "uint16")
	if err := m.UpdateRegister(UpdateRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 41,
		OriginalAddress: 11, OriginalRegisterType: "holding_register",
		Address: 20, RegisterType: "holding_register", DataType: "uint16",
	}); err == nil || err.Error() != "register holding_register@20 overlaps point at address 20" {
		t.Errorf("collision: err = %v", err)
	}
	if value := readValue(t, m, 41, "holding_register", 11); value != 4242 {
		t.Errorf("value after a rejected move = %d", value)
	}

	if err := m.UpdateRegister(UpdateRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 41,
		OriginalAddress: 999, OriginalRegisterType: "holding_register",
		Address: 30, RegisterType: "holding_register", DataType: "uint16",
	}); err == nil || err.Error() != "original register not found" {
		t.Errorf("missing original: err = %v", err)
	}
	// Shrinking a 32-bit point to one word keeps the first word only.
	if err := m.AddRegister(AddRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 41, Address: 40, RegisterType: "holding_register", DataType: "uint32",
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteRegister(WriteRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 41, RegisterType: "holding_register", Address: 41, Value: 7,
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateRegister(UpdateRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 41,
		OriginalAddress: 40, OriginalRegisterType: "holding_register",
		Address: 40, RegisterType: "holding_register", DataType: "uint16",
	}); err != nil {
		t.Fatal(err)
	}
	if value := readValue(t, m, 41, "holding_register", 40); value != 0 {
		t.Errorf("high word survived the shrink: %d", value)
	}
	if hasSlot(deviceOf(t, m, 41).RegisterMap, "holding_register", 41) {
		t.Error("the freed word is still allocated")
	}
}

func TestRemoveRegisterFreesEveryWord(t *testing.T) {
	m := emptyConnection(t, 42)
	if err := m.AddRegister(AddRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 42, Address: 300, RegisterType: "input_register", DataType: "int32",
		Endian: "mid_big",
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDataSource(SetDataSourceRequest{
		ConnectionID: "slave_1", SlaveID: 42, RegisterType: "input_register", Address: 300,
		Source: datasource.Source{Type: datasource.KindFixed, Value: 5}, UpdateIntervalMS: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveRegister("slave_1", 42, 300, "input_register"); err != nil {
		t.Fatal(err)
	}
	mapAfter := deviceOf(t, m, 42).RegisterMap
	if hasSlot(mapAfter, "input_register", 300) || hasSlot(mapAfter, "input_register", 301) {
		t.Error("removing a 32-bit point left words behind")
	}
	if defs, err := m.ListRegisters("slave_1", 42, ""); err != nil || len(defs) != 0 {
		t.Errorf("definitions = %+v (%v)", defs, err)
	}
	conn := connectionFor(t, m, "slave_1")
	if len(conn.points) != 0 {
		t.Errorf("point configs survived removal: %+v", conn.points)
	}
	sources, err := m.ListDataSources("slave_1")
	if err != nil || len(sources) != 0 {
		t.Errorf("data sources = %+v (%v)", sources, err)
	}
	if err := m.RemoveRegister("slave_1", 42, 300, "input_register"); err == nil ||
		err.Error() != "register not found" {
		t.Errorf("removing twice: err = %v", err)
	}
	if err := m.RemoveRegister("slave_1", 42, 300, "nonsense"); err == nil ||
		err.Error() != "unknown register type: nonsense" {
		t.Errorf("bad area: err = %v", err)
	}
}

func TestWriteRegisterAllocatesOnDemand(t *testing.T) {
	rec := &recorded{}
	m, _ := newTestConnection(t, rec)

	// Rust's write_register stores into the map even where no point is defined,
	// and tells the UI about it without going through the master-facing path.
	if err := m.WriteRegister(WriteRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 1, RegisterType: "coil", Address: 60000, Value: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if value := readValue(t, m, 1, "coil", 60000); value != 1 {
		t.Errorf("coil = %d", value)
	}
	if defs, err := m.ListRegisters("slave_1", 1, "coil"); err != nil || len(defs) != (defaultMaxAddress+1) {
		t.Errorf("a manual write must not define points: %d definitions (%v)", len(defs), err)
	}
	waitFor(t, "the manual write to be reported", func() bool {
		return rec.countChanges(60000) > 0
	})
	if events := rec.snapshotChanges(); len(events) != 1 || events[0].ConnectionID != "slave_1" {
		t.Errorf("change events = %+v", events)
	}

	if err := m.WriteRegister(WriteRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 99, RegisterType: "holding_register", Address: 1, Value: 1,
	}); err == nil || err.Error() != "slave 99 not found" {
		t.Errorf("missing device: err = %v", err)
	}
	if err := m.WriteRegister(WriteRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 1, RegisterType: "discrete_input", Address: 1, Value: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if !hasSlot(deviceOf(t, m, 1).RegisterMap, "discrete_input", 1) {
		t.Error("writing a read-only area must still allocate its bit")
	}
}

func TestBulkReadsFollowDefinitions(t *testing.T) {
	m := emptyConnection(t, 43)
	for _, address := range []uint16{5, 1, 9} {
		mustAddRegister(t, m, 43, address, "holding_register", "uint16")
		if err := m.WriteRegister(WriteRegisterRequest{
			ConnectionID: "slave_1", SlaveID: 43, RegisterType: "holding_register", Address: address, Value: address * 2,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mustAddRegister(t, m, 43, 2, "coil", "bool")

	bulk, err := m.ReadRegistersBulk("slave_1", 43, "holding_register")
	if err != nil {
		t.Fatal(err)
	}
	// Definition order, not address order -- the UI keeps the list it imported.
	if len(bulk) != 3 || bulk[0].Address != 5 || bulk[2].Address != 9 {
		t.Fatalf("bulk read = %+v", bulk)
	}
	for _, entry := range bulk {
		if entry.Value != entry.Address*2 {
			t.Errorf("bulk read = %+v", bulk)
			break
		}
	}
	// An address with no slot reads as zero rather than failing.
	if value, err := m.ReadRegister("slave_1", 43, "input_register", 1234); err != nil || value.Value != 0 {
		t.Errorf("unallocated read = %+v (%v)", value, err)
	}
	if _, err := m.ReadRegister("slave_1", 43, "holding_regs", 1); err == nil {
		t.Error("bad area name accepted")
	}
}

func TestImportRegistersIsAtomic(t *testing.T) {
	m := emptyConnection(t, 44)
	point := &project.Point{Address: 10, RegisterType: register.HoldingRegType, DataType: register.TypeUInt16}

	count, err := m.ImportRegisters(ImportRegistersRequest{
		ConnectionID: "slave_1", SlaveID: 44, Points: []*project.Point{point},
	})
	if err != nil || count != 1 {
		t.Fatalf("first import = %d (%v)", count, err)
	}
	if err := m.WriteRegister(WriteRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 44, RegisterType: "holding_register", Address: 10, Value: 42,
	}); err != nil {
		t.Fatal(err)
	}

	// Appending a duplicate is rejected wholesale, value intact.
	if _, err := m.ImportRegisters(ImportRegistersRequest{
		ConnectionID: "slave_1", SlaveID: 44, Points: []*project.Point{point}, Mode: "append",
	}); err == nil {
		t.Error("duplicate append succeeded")
	}
	if value := readValue(t, m, 44, "holding_register", 10); value != 42 {
		t.Errorf("value after a failed append = %d", value)
	}

	// A replacement whose own set is inconsistent must not wipe the device.
	moved := &project.Point{Address: 20, RegisterType: register.HoldingRegType, DataType: register.TypeUInt16}
	if _, err := m.ImportRegisters(ImportRegistersRequest{
		ConnectionID: "slave_1", SlaveID: 44, Points: []*project.Point{moved, moved}, Mode: "replace",
	}); err == nil {
		t.Error("self-overlapping replace succeeded")
	}
	if value := readValue(t, m, 44, "holding_register", 10); value != 42 {
		t.Errorf("value after a failed replace = %d", value)
	}
	defs, err := m.ListRegisters("slave_1", 44, "")
	if err != nil || len(defs) != 1 {
		t.Fatalf("definitions = %+v (%v)", defs, err)
	}

	count, err = m.ImportRegisters(ImportRegistersRequest{
		ConnectionID: "slave_1", SlaveID: 44, Points: []*project.Point{moved}, Mode: "replace",
	})
	if err != nil || count != 1 {
		t.Fatalf("replace = %d (%v)", count, err)
	}
	mapAfter := deviceOf(t, m, 44).RegisterMap
	if hasSlot(mapAfter, "holding_register", 10) {
		t.Error("replace must free the points it dropped")
	}
	if value := readValue(t, m, 44, "holding_register", 10); value != 0 {
		t.Errorf("dropped point still reads %d", value)
	}
	defs, err = m.ListRegisters("slave_1", 44, "")
	if err != nil || len(defs) != 1 || defs[0].Address != 20 {
		t.Errorf("definitions = %+v (%v)", defs, err)
	}
}

func TestImportRegistersModesAndStates(t *testing.T) {
	rec := &recorded{}
	m, _ := newTestConnection(t, rec)
	if _, err := m.AddDevice(AddDeviceRequest{ConnectionID: "slave_1", SlaveID: 45}); err != nil {
		t.Fatal(err)
	}
	must := func(req ImportRegistersRequest) (int, error) {
		req.ConnectionID = "slave_1"
		req.SlaveID = 45
		return m.ImportRegisters(req)
	}
	point := &project.Point{Address: 3, RegisterType: register.HoldingRegType, DataType: register.TypeUInt16}

	if _, err := must(ImportRegistersRequest{Points: []*project.Point{point}, Mode: "merge"}); err == nil ||
		err.Error() != `unknown import mode "merge"` {
		t.Errorf("unknown mode: err = %v", err)
	}
	if _, err := must(ImportRegistersRequest{Points: []*project.Point{nil}}); err == nil ||
		err.Error() != "imported register must not be null" {
		t.Errorf("null point: err = %v", err)
	}
	// The tokens of an imported point are validated too: Rust got that from
	// serde, Go's struct decoder would take any string.
	for _, bad := range []*project.Point{
		{Address: 3, DataType: register.TypeUInt16},
		{Address: 3, RegisterType: register.HoldingRegType, DataType: "uint128"},
		{Address: 3, RegisterType: register.HoldingRegType, DataType: register.TypeUInt32, Endian: "sideways"},
	} {
		if _, err := must(ImportRegistersRequest{Points: []*project.Point{bad}, Mode: "replace"}); err == nil {
			t.Errorf("import of %+v was accepted", bad)
		}
	}

	// Replacing discards register contents, so a running listener must not race it.
	if err := m.StartConnection("slave_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := must(ImportRegistersRequest{Points: []*project.Point{point}, Mode: "replace"}); err == nil ||
		err.Error() != "Stop the connection before replacing register definitions" {
		t.Errorf("replace while running: err = %v", err)
	}
	if _, err := must(ImportRegistersRequest{Points: []*project.Point{point}}); err != nil {
		t.Errorf("append while running: %v", err)
	}
	if err := m.StopConnection("slave_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := must(ImportRegistersRequest{Points: []*project.Point{point}, Mode: "replace"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportRegisters(ImportRegistersRequest{ConnectionID: "nope"}); err == nil ||
		err.Error() != "connection nope not found" {
		t.Errorf("import on a missing connection: err = %v", err)
	}
}

func TestImportedDriversStartLive(t *testing.T) {
	m := emptyConnection(t, 46)
	points := []*project.Point{
		{
			Address: 1, RegisterType: register.HoldingRegType, DataType: register.TypeUInt16,
			Mutation: &project.MutationConfig{
				Enabled: true, Mode: mutation.ModeIncrement, PeriodMS: mutation.BaseTickMS, Step: 1, Max: 500,
			},
		},
		{
			Address: 2, RegisterType: register.HoldingRegType, DataType: register.TypeUInt16,
			DataSource: &project.DataSourceConfig{
				Source:           project.DataSource{Type: datasource.KindFixed, Value: 4321},
				UpdateIntervalMS: mutation.BaseTickMS,
			},
		},
	}
	if _, err := m.ImportRegisters(ImportRegistersRequest{
		ConnectionID: "slave_1", SlaveID: 46, Points: points, Mode: "replace",
	}); err != nil {
		t.Fatal(err)
	}

	// Rust leaves an imported config inert; the Go app wires it into the
	// scheduler, which the project load path needs anyway.
	mutations, err := m.ListPointMutations("slave_1", 46)
	if err != nil {
		t.Fatal(err)
	}
	if len(mutations) != 1 || mutations[0].Address != 1 {
		t.Errorf("mutations = %+v", mutations)
	}
	// Data sources ignore the master switch, so this one lands on its own.
	waitFor(t, "an imported data source to generate", func() bool {
		return readValue(t, m, 46, "holding_register", 2) == 4321
	})
	if value := readValue(t, m, 46, "holding_register", 1); value != 0 {
		t.Errorf("imported mutation ran with the switch off: %d", value)
	}
	m.SetMutationRunning(true)
	waitFor(t, "an imported mutation to step", func() bool {
		return readValue(t, m, 46, "holding_register", 1) > 0
	})
}

func TestExportRegistersRoundTrips(t *testing.T) {
	m := emptyConnection(t, 47)
	if err := m.AddRegister(AddRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 47, Address: 60, RegisterType: "holding_register",
		DataType: "uint32", Endian: "little", Name: "counter", Comment: "from & through <tags>",
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPointMutation(SetPointMutationRequest{
		ConnectionID: "slave_1", SlaveID: 47, RegisterType: "holding_register", Address: 60, Config: mutationEnabled(),
	}); err != nil {
		t.Fatal(err)
	}

	exported, err := m.ExportRegisters("slave_1", 47)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(exported, "[\n  {") {
		t.Errorf("export is not pretty-printed:\n%s", exported)
	}
	var points []*project.Point
	if err := json.Unmarshal([]byte(exported), &points); err != nil {
		t.Fatalf("export does not parse back: %v\n%s", err, exported)
	}
	if len(points) != 1 {
		t.Fatalf("points = %+v", points)
	}
	got := points[0]
	if got.Address != 60 || got.DataType != register.TypeUInt32 || got.Endian != register.EndianLittle ||
		got.Name != "counter" || got.Comment != "from & through <tags>" {
		t.Errorf("point = %+v", got)
	}
	if got.Mutation == nil || !got.Mutation.Enabled || got.Mutation.Mode != mutation.ModeIncrement ||
		got.Mutation.PeriodMS != mutation.BaseTickMS {
		t.Errorf("mutation = %+v", got.Mutation)
	}
	if got.DataSource != nil {
		t.Errorf("data source = %+v, want none", got.DataSource)
	}
	// The export is a definition list, so re-importing it into a fresh device
	// reproduces the point set.
	if _, err := m.AddDevice(AddDeviceRequest{ConnectionID: "slave_1", SlaveID: 48}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportRegisters(ImportRegistersRequest{
		ConnectionID: "slave_1", SlaveID: 48, Points: points, Mode: "replace",
	}); err != nil {
		t.Fatal(err)
	}
	again, err := m.ExportRegisters("slave_1", 48)
	if err != nil {
		t.Fatal(err)
	}
	if again != exported {
		t.Errorf("re-imported export differs:\n%s\n%s", exported, again)
	}
	if _, err := m.ExportRegisters("slave_1", 99); err == nil {
		t.Error("export of a missing device succeeded")
	}
}
