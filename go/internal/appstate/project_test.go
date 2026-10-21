package appstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/project"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// smallProject is a hand-built slave document: small enough to assert on, and
// loading it is the cheapest way to get a connection with a few points instead
// of the 80k-point default device.
func smallProject() *project.Project {
	conn := project.DefaultConnection()
	conn.ID = "slave_1"
	conn.Transport = Transport{Type: project.TransportTCP, Host: "127.0.0.1", Port: 15999}
	conn.Devices = []*project.Device{{
		SlaveID: 5, Name: "pump",
		Points: []*project.Point{
			{Address: 0, RegisterType: register.HoldingRegType, DataType: register.TypeUInt16, Name: "pressure"},
			{Address: 2, RegisterType: register.HoldingRegType, DataType: register.TypeUInt32, Endian: register.EndianLittle},
			{Address: 0, RegisterType: register.Coil, DataType: register.TypeBool},
			{Address: 7, RegisterType: register.InputRegister, DataType: register.TypeUInt16},
		},
		Values: project.Values{
			Coils:            map[uint16]bool{0: true},
			HoldingRegisters: map[uint16]uint16{0: 11, 2: 22, 3: 33},
		},
	}}
	return &project.Project{Version: project.CurrentVersion, Type: project.TypeSlave, Connections: []*project.Connection{conn}}
}

func TestProjectRoundTrip(t *testing.T) {
	m := NewManager(Events{})
	devices, err := m.ApplyProject(smallProject())
	if err != nil {
		t.Fatal(err)
	}
	if devices != 1 {
		t.Fatalf("devices = %d", devices)
	}
	if info := connectionByID(t, m, "slave_1"); info.State != Stopped || info.DeviceCount != 1 {
		t.Errorf("connection = %+v", info)
	}
	if m.MutationRunning() {
		t.Error("the mutation switch must start off")
	}

	if err := m.SetPointMutation(SetPointMutationRequest{
		ConnectionID: "slave_1", SlaveID: 5, RegisterType: "holding_register", Address: 0, Config: mutationEnabled(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDataSource(SetDataSourceRequest{
		ConnectionID: "slave_1", SlaveID: 5, RegisterType: "input_register", Address: 7,
		Source:           datasource.Source{Type: datasource.KindFixed, Value: 4242},
		UpdateIntervalMS: mutation.BaseTickMS,
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteRegister(WriteRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 5, RegisterType: "holding_register", Address: 2, Value: 0x1234,
	}); err != nil {
		t.Fatal(err)
	}
	m.SetMutationRunning(true)

	saved := m.Project()
	if len(saved.Connections) != 1 {
		t.Fatalf("connections = %+v", saved.Connections)
	}
	conn := saved.Connections[0]
	if conn.ID != "slave_1" || conn.Name != "127.0.0.1:15999" {
		t.Errorf("connection = %s (%s)", conn.ID, conn.Name)
	}
	if conn.DefaultSlaveID != 1 || conn.TimeoutMS != 3000 {
		t.Errorf("connection defaults = %+v", conn)
	}
	// A slave project never reads the master-side fields but keeps their defaults.
	if err := conn.Requests.Validate(); err != nil {
		t.Errorf("request defaults = %+v: %v", conn.Requests, err)
	}
	if len(conn.Devices) != 1 || len(conn.Devices[0].Points) != 4 {
		t.Fatalf("device = %+v", conn.Devices)
	}

	path := filepath.Join(t.TempDir(), "saved"+project.FileExtension)
	if err := m.SaveProject(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "period_ms") || !strings.Contains(string(raw), `"PeriodMS"`) {
		t.Error("a v2 file must use Go key names")
	}
	var head struct{ Version int }
	if err := json.Unmarshal(raw, &head); err != nil {
		t.Fatal(err)
	}
	if head.Version != project.CurrentVersion {
		t.Errorf("version = %d", head.Version)
	}

	// Reload into a fresh registry and check the whole document came back.
	rec := &recorded{}
	second := NewManager(rec.events())
	defer second.CloseAll()
	count, err := second.LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reloaded devices = %d", count)
	}
	if second.MutationRunning() {
		t.Error("loading a project must leave the mutation switch off")
	}
	// The "slave_N" sequence continues after the loaded ids.
	next, err := second.CreateConnection(CreateConnectionRequest{
		Transport: Transport{Type: project.TransportTCP, Host: "127.0.0.1", Port: 16000}, SlaveID: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != "slave_2" {
		t.Errorf("next id = %s", next.ID)
	}
	if err := second.DeleteConnection(next.ID); err != nil {
		t.Fatal(err)
	}

	defs, err := second.ListRegisters("slave_1", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 4 || defs[0].Name != "pressure" ||
		defs[1].DataType != register.TypeUInt32 || defs[1].Endian != register.EndianLittle {
		t.Errorf("definitions = %+v", defs)
	}
	// Raw contents survive; the point the data source owns does not matter yet.
	for address, want := range map[uint16]uint16{0: 11, 2: 0x1234, 3: 33} {
		if value := readValueFrom(t, second, "slave_1", 5, "holding_register", address); value != want {
			t.Errorf("holding@%d = %d, want %d", address, value, want)
		}
	}
	if value := readValueFrom(t, second, "slave_1", 5, "coil", 0); value != 1 {
		t.Errorf("coil@0 = %d", value)
	}
	mutations, err := second.ListPointMutations("slave_1", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(mutations) != 1 || mutations[0].Address != 0 || mutations[0].Mode != mutation.ModeIncrement {
		t.Errorf("mutations = %+v", mutations)
	}
	sources, err := second.ListDataSources("slave_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].SlaveID != 5 || sources[0].Address != 7 {
		t.Errorf("data sources = %+v", sources)
	}

	// A loaded data source drives at once; a loaded mutation waits for the switch.
	waitFor(t, "the loaded data source to generate", func() bool {
		return readValueFrom(t, second, "slave_1", 5, "input_register", 7) == 4242
	})
	before := readValueFrom(t, second, "slave_1", 5, "holding_register", 0)
	time.Sleep(3 * mutation.BaseTickMS * time.Millisecond)
	if value := readValueFrom(t, second, "slave_1", 5, "holding_register", 0); value != before {
		t.Errorf("loaded mutation ran with the switch off: %d -> %d", before, value)
	}
	second.SetMutationRunning(true)
	waitFor(t, "the loaded mutation to step", func() bool {
		return readValueFrom(t, second, "slave_1", 5, "holding_register", 0) != before
	})
	// Values written after the load are reported to the UI like any other change.
	waitFor(t, "generated changes to be reported", func() bool {
		return rec.countChanges(0) > 0 || rec.countChanges(7) > 0
	})
}

func TestProjectNamesFollowTransport(t *testing.T) {
	rec := &recorded{}
	m := NewManager(rec.events())
	defer m.CloseAll()

	transports := []Transport{
		{Type: project.TransportTCP, Host: "0.0.0.0", Port: 15020},
		{Type: project.TransportRTUOverTCP, Host: "127.0.0.1", Port: 15021},
		{Type: project.TransportTCPTLS, Host: "127.0.0.1", Port: 15022},
		{Type: project.TransportRTU, Serial: &project.SerialConfig{Port: "/dev/ttyUSB0", BaudRate: 9600, DataBits: 8, StopBits: 1}},
		{Type: project.TransportASCII, Serial: &project.SerialConfig{Port: "/dev/ttyUSB1", BaudRate: 19200, DataBits: 7, StopBits: 2, Parity: project.ParityEven}},
	}
	want := []string{
		"0.0.0.0:15020",
		"rtu-tcp://127.0.0.1:15021",
		"tls://127.0.0.1:15022",
		"rtu:///dev/ttyUSB0",
		"ascii:///dev/ttyUSB1",
	}
	for i, transport := range transports {
		if _, err := m.CreateConnection(CreateConnectionRequest{Transport: transport, SlaveID: uint8(i + 1)}); err != nil {
			t.Fatal(err)
		}
		conn := m.Project().Connections[i]
		if conn.Name != want[i] {
			t.Errorf("%s name = %q, want %q", transport.Type, conn.Name, want[i])
		}
	}
	// Serial connections round-trip through their port settings.
	reloaded := NewManager(Events{})
	defer reloaded.CloseAll()
	if _, err := reloaded.ApplyProject(m.Project()); err != nil {
		t.Fatal(err)
	}
	list := reloaded.ListConnections()
	if len(list) != len(transports) {
		t.Fatalf("connections = %+v", list)
	}
	for i, want := range transports {
		got, err := reloaded.GetTransport(list[i].ID)
		if err != nil || got.Type != want.Type || got.Host != want.Host || got.Port != want.Port {
			t.Errorf("transport %d = %+v (%v), want %+v", i, got, err, want)
			continue
		}
		switch {
		case want.Serial == nil && got.Serial != nil:
			t.Errorf("transport %d gained serial settings: %+v", i, got.Serial)
		case want.Serial != nil && got.Serial == nil:
			t.Errorf("transport %d lost its serial settings", i)
		case want.Serial != nil && *want.Serial != *got.Serial:
			t.Errorf("serial %d = %+v, want %+v", i, got.Serial, want.Serial)
		}
	}
}

func TestApplyProjectRejectsInvalidDocuments(t *testing.T) {
	m := NewManager(Events{})
	defer m.CloseAll()
	if _, err := m.ApplyProject(smallProject()); err != nil {
		t.Fatal(err)
	}

	master := project.New(project.TypeMaster)
	master.Connections = []*project.Connection{project.DefaultConnection()}
	cases := []struct {
		name  string
		proj  *project.Project
		wants string
	}{
		{"master project", master, "project is not a slave project"},
		{"missing id", withConnections(func(c *project.Connection) { c.ID = "" }), "connection 0 is missing an id"},
		{"duplicate id", withConnections(func(c *project.Connection) { c.ID = "slave_1" }, func(c *project.Connection) { c.ID = "slave_1" }), "duplicate connection id slave_1"},
		{"missing host", withConnections(func(c *project.Connection) { c.Transport.Host = "" }), "transport host is required"},
		{"zero port", withConnections(func(c *project.Connection) { c.Transport.Port = 0 }), "transport port must be between 1 and 65535"},
		{"bad slave id", withConnections(func(c *project.Connection) { c.Devices[0].SlaveID = 248 }), "slave 248: slave ID must be between 1 and 247"},
		{"unknown data type", withConnections(func(c *project.Connection) { c.Devices[0].Points[0].DataType = "uint128" }), "unknown data type: uint128"},
		{"overlapping points", withConnections(func(c *project.Connection) { c.Devices[0].Points[1].Address = 0 }), "register holding_register@0 overlaps point at address 0"},
		{"both drivers", withConnections(func(c *project.Connection) {
			c.Devices[0].Points[0].Mutation = project.DefaultMutationConfig()
			c.Devices[0].Points[0].Mutation.Enabled = true
			c.Devices[0].Points[0].DataSource = &project.DataSourceConfig{
				Source: project.DataSource{Type: datasource.KindFixed}, UpdateIntervalMS: 1000,
			}
		}), "cannot use mutation and a data source simultaneously"},
		{"bad data source", withConnections(func(c *project.Connection) {
			c.Devices[0].Points[1].DataSource = &project.DataSourceConfig{
				Source: project.DataSource{Type: datasource.KindCsvPlayback}, UpdateIntervalMS: 1000,
			}
		}), "CSV playback requires at least one value"},
	}

	for _, tc := range cases {
		if _, err := m.ApplyProject(tc.proj); err == nil || !strings.Contains(err.Error(), tc.wants) {
			t.Errorf("%s: err = %v, want one containing %q", tc.name, err, tc.wants)
		}
		// A rejected load must leave the running registry alone.
		if list := m.ListConnections(); len(list) != 1 || list[0].ID != "slave_1" {
			t.Fatalf("%s: connections after rejection = %+v", tc.name, list)
		}
	}
}

func TestApplyProjectReplacesRunningState(t *testing.T) {
	rec := &recorded{}
	m, port := newTestConnection(t, rec)
	if err := m.StartConnection("slave_1"); err != nil {
		t.Fatal(err)
	}

	proj := smallProject()
	proj.Connections[0].ID = "gateway"
	proj.Connections[0].Transport.Port = port
	count, err := m.ApplyProject(proj)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("devices = %d", count)
	}
	// The old connection is gone and its listener released with it.
	if list := m.ListConnections(); len(list) != 1 || list[0].ID != "gateway" {
		t.Fatalf("connections = %+v", list)
	}
	if list := m.ListConnections(); list[0].State != Stopped {
		t.Errorf("loaded connection state = %s, want stopped", list[0].State)
	}
	if err := m.StartConnection("gateway"); err != nil {
		t.Fatalf("the freed port is not reusable: %v", err)
	}
	if err := m.StopConnection("gateway"); err != nil {
		t.Fatal(err)
	}
	states := rec.stateOf("slave_1")
	if len(states) != 1 || states[0] != Running {
		t.Errorf("the replaced connection must not emit a stop event: %v", states)
	}
	// Its devices and points came from the document, not the default sweep.
	if devices, err := m.ListDevices("gateway"); err != nil || len(devices) != 1 || devices[0].Name != "pump" {
		t.Errorf("devices = %+v (%v)", devices, err)
	}
}

func TestLoadProjectMigratesLegacyFile(t *testing.T) {
	m := NewManager(Events{})
	defer m.CloseAll()
	count, err := m.LoadProject(filepath.Join("..", "project", "testdata", "v1-slave-legacy-blocks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("devices = %d", count)
	}
	// A v1 file keeps its own connection ids, so the minted sequence restarts.
	created, err := m.CreateConnection(CreateConnectionRequest{
		Transport: Transport{Type: project.TransportTCP, Host: "127.0.0.1", Port: 16010}, SlaveID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "slave_1" {
		t.Errorf("created id = %s", created.ID)
	}
	if devices, err := m.ListDevices("conn_rtu"); err != nil || len(devices) != 1 || devices[0].SlaveID != 3 {
		t.Errorf("devices = %+v (%v)", devices, err)
	}
	defs, err := m.ListRegisters("conn_rtu", 3, "")
	if err != nil {
		t.Fatal(err)
	}
	// Coils 0..2, inputs 10..11, the 4-word holding block as two uint32 points,
	// and the 2-word float32 input block as one point.
	if len(defs) != 8 {
		t.Fatalf("definitions = %+v", defs)
	}
	holding, err := m.ListRegisters("conn_rtu", 3, "holding_register")
	if err != nil {
		t.Fatal(err)
	}
	if len(holding) != 2 || holding[0].DataType != register.TypeUInt32 ||
		holding[0].Endian != register.EndianMidLittle || holding[1].Address != 102 {
		t.Errorf("holding definitions = %+v", holding)
	}
	if holding[0].Name != "Total" {
		t.Errorf("holding name = %q", holding[0].Name)
	}
	inputs, err := m.ListRegisters("conn_rtu", 3, "input_register")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].DataType != register.TypeFloat || inputs[0].Address != 20 {
		t.Errorf("input definitions = %+v", inputs)
	}
	if got, err := m.GetTransport("conn_rtu"); err != nil || got.Type != project.TransportRTU ||
		got.Serial == nil || got.Serial.BaudRate != 19200 || got.Serial.Parity != project.ParityEven {
		t.Errorf("transport = %+v (%v)", got, err)
	}

	path := filepath.Join(t.TempDir(), "re-saved"+project.FileExtension)
	if err := m.SaveProject(path); err != nil {
		t.Fatal(err)
	}
	if _, err := m.LoadProject(path); err != nil {
		t.Fatalf("re-loading the v2 save: %v", err)
	}
	// An unknown version is rejected the way Rust rejected it.
	if err := os.WriteFile(path, []byte(`{"Version":3,"Type":"slave"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.LoadProject(path); err == nil || !strings.Contains(err.Error(), "unsupported project version: 3") {
		t.Errorf("version 3: err = %v", err)
	}
}

// withConnections builds a one-connection slave document from the defaults,
// letting each case start from the same valid baseline.
func withConnections(edits ...func(*project.Connection)) *project.Project {
	proj := smallProject()
	for _, edit := range edits {
		edit(proj.Connections[0])
	}
	if len(edits) > 1 {
		second := smallProject().Connections[0]
		second.ID = proj.Connections[0].ID
		for _, edit := range edits[1:] {
			edit(second)
		}
		proj.Connections = append(proj.Connections, second)
	}
	return proj
}
