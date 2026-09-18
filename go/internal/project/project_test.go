// Tests for the project.rs port: save/load round trips, transport serde
// variants, serde defaults, and compatibility with Rust-authored files.
package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/config"
	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/socks5"
)

func TestNewProjectTypes(t *testing.T) {
	s := NewSlave()
	if s.Version != 1 || s.Type != TypeSlave || len(s.Connections) != 0 {
		t.Fatalf("slave = %+v", s)
	}
	m := NewMaster()
	if m.Type != TypeMaster {
		t.Fatalf("master = %+v", m)
	}
}

func TestSaveAndLoadSlaveProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.modbusproj")
	p := NewSlave()
	dt := "uint16"
	p.Connections = []ConnectionConfig{{
		ID: "conn-1", Name: "Local TCP",
		Transport:       NewTCP("127.0.0.1", 502),
		TimeoutMs:       3000,
		Requests:        DefaultRequestSettings(),
		ReconnectPolicy: DefaultReconnectPolicy(),
		Devices: []DeviceConfig{{
			SlaveID: 1, Name: "Slave 1",
			Registers: RegistersConfig{
				Holding: []RegisterBlockConfig{{
					Address: 0, Count: 10,
					DataType: &dt,
					Values:   []json.RawMessage{json.RawMessage("0"), json.RawMessage("100")},
					Names:    map[string]string{"0": "Temperature"},
				}},
			},
		}},
	}}

	if err := SaveProject(&p, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != 1 || loaded.Type != TypeSlave || len(loaded.Connections) != 1 {
		t.Fatalf("loaded = %+v", loaded)
	}
	conn := loaded.Connections[0]
	if conn.ID != "conn-1" || conn.Name != "Local TCP" {
		t.Fatalf("conn = %+v", conn)
	}
	if conn.Transport.Type != "tcp" || conn.Transport.Host != "127.0.0.1" || conn.Transport.Port != 502 {
		t.Fatalf("transport = %+v", conn.Transport)
	}
	blk := conn.Devices[0].Registers.Holding[0]
	if blk.Address != 0 || blk.Count != 10 || *blk.DataType != "uint16" || len(blk.Values) != 2 {
		t.Fatalf("block = %+v", blk)
	}
	if blk.Names["0"] != "Temperature" {
		t.Fatalf("names = %v", blk.Names)
	}
}

func TestSaveAndLoadMasterProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.modbusproj")
	p := NewMaster()
	p.Connections = []ConnectionConfig{{
		ID: "conn-m1", Name: "Remote PLC",
		Transport:       NewTCP("192.168.1.10", 502),
		TimeoutMs:       3000,
		Requests:        DefaultRequestSettings(),
		ReconnectPolicy: DefaultReconnectPolicy(),
		ScanGroups: []ScanGroupConfig{{
			ID: "fast-poll", Name: "Fast Poll", SlaveID: 1,
			FunctionCode: 3, StartAddress: 0, Count: 10, IntervalMs: 1000, Enabled: true,
		}},
		Socks5: &socks5.Config{
			Enabled: true, Host: "proxy.example.com", Port: 1080,
			Username: "operator", Password: "secret",
		},
	}}

	if err := SaveProject(&p, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	conn := loaded.Connections[0]
	if len(conn.ScanGroups) != 1 || conn.ScanGroups[0].Name != "Fast Poll" ||
		conn.ScanGroups[0].FunctionCode != 3 || conn.ScanGroups[0].IntervalMs != 1000 {
		t.Fatalf("scan groups = %+v", conn.ScanGroups)
	}
	if conn.Socks5 == nil || !conn.Socks5.Enabled || conn.Socks5.Host != "proxy.example.com" {
		t.Fatalf("socks5 = %+v", conn.Socks5)
	}
}

func TestMutationConfigSurvivesRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mutation.modbusproj")
	p := NewSlave()
	p.Connections = []ConnectionConfig{{
		ID: "slave_1", Name: "Local",
		Transport:       NewTCP("0.0.0.0", 5020),
		TimeoutMs:       3000,
		Requests:        DefaultRequestSettings(),
		ReconnectPolicy: DefaultReconnectPolicy(),
		Devices: []DeviceConfig{{
			SlaveID: 7, Name: "Pump",
			RegisterDefs: []register.RegisterDef{{
				Address: 10, RegisterType: register.HoldingRegType,
				DataType: register.TypeFloat, Endian: register.EndianBig,
				Name: "speed",
				Mutation: &register.MutationConfig{
					Enabled: true, Mode: register.MutationModeIncrement,
					PeriodMs: 750, Step: 0.5, Min: 0, Max: 10,
				},
			}},
		}},
	}}
	if err := SaveProject(&p, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	mut := loaded.Connections[0].Devices[0].RegisterDefs[0].Mutation
	if mut == nil || mut.Mode != register.MutationModeIncrement || mut.PeriodMs != 750 {
		t.Fatalf("mutation = %+v", mut)
	}
	if loaded.Connections[0].Devices[0].Name != "Pump" {
		t.Fatalf("device = %+v", loaded.Connections[0].Devices[0])
	}
}

func TestOldV1DeviceWithoutNewFieldsStillLoads(t *testing.T) {
	jsonStr := `{
		"version": 1,
		"type": "slave",
		"connections": [{
			"id": "legacy",
			"name": "Legacy",
			"transport": { "type": "tcp", "host": "0.0.0.0", "port": 502 },
			"devices": [{ "slave_id": 1, "registers": {} }]
		}]
	}`
	loaded, err := MigrateProject(jsonStr)
	if err != nil {
		t.Fatal(err)
	}
	dev := loaded.Connections[0].Devices[0]
	if dev.Name != "" || len(dev.RegisterDefs) != 0 {
		t.Fatalf("device = %+v", dev)
	}
	if loaded.Connections[0].Socks5 != nil && loaded.Connections[0].Socks5.Enabled {
		t.Fatal("absent socks5 should be disabled")
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := LoadProject("/tmp/does_not_exist_12345.modbusproj"); err == nil {
		t.Fatal("missing file should fail")
	}
	path := filepath.Join(t.TempDir(), "bad.modbusproj")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProject(path); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("err = %v", err)
	}
	if _, err := MigrateProject(`{"version":99,"type":"slave","connections":[]}`); err == nil ||
		!strings.Contains(err.Error(), "unsupported project version") {
		t.Fatal("version 99 should be unsupported")
	}
}

func TestMigrateCurrentVersion(t *testing.T) {
	p, err := MigrateProject(`{"version":1,"type":"slave","connections":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != 1 || p.Type != TypeSlave || len(p.Connections) != 0 {
		t.Fatalf("p = %+v", p)
	}
}

func TestJSONRoundtripPreservesTransportTag(t *testing.T) {
	p := NewSlave()
	p.Connections = []ConnectionConfig{{
		ID: "c1", Name: "Test",
		Transport:       NewTCP("localhost", 5020),
		TimeoutMs:       3000,
		Requests:        DefaultRequestSettings(),
		ReconnectPolicy: DefaultReconnectPolicy(),
	}}
	b, err := json.Marshal(&p)
	if err != nil {
		t.Fatal(err)
	}
	jsonStr := string(b)
	if !strings.Contains(jsonStr, `"type":"tcp"`) {
		t.Fatalf("json = %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"socks5"`) {
		t.Fatal("disabled socks5 should be omitted")
	}
	loaded, err := MigrateProject(jsonStr)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Connections[0].Transport.Host != "localhost" || loaded.Connections[0].Transport.Port != 5020 {
		t.Fatalf("transport = %+v", loaded.Connections[0].Transport)
	}
}

func TestTransportRTUSerde(t *testing.T) {
	p := NewSlave()
	p.Connections = []ConnectionConfig{{
		ID: "c1", Name: "serial",
		Transport:       NewSerial(false, "/dev/ttyUSB0", 9600, 8, 1, "none"),
		TimeoutMs:       3000,
		Requests:        DefaultRequestSettings(),
		ReconnectPolicy: DefaultReconnectPolicy(),
	}}
	b, err := json.Marshal(&p)
	if err != nil {
		t.Fatal(err)
	}
	jsonStr := string(b)
	if !strings.Contains(jsonStr, `"type":"rtu"`) || !strings.Contains(jsonStr, "ttyUSB0") {
		t.Fatalf("json = %s", jsonStr)
	}
	loaded, err := MigrateProject(jsonStr)
	if err != nil {
		t.Fatal(err)
	}
	tr := loaded.Connections[0].Transport
	if tr.PortName != "/dev/ttyUSB0" || tr.BaudRate != 9600 {
		t.Fatalf("transport = %+v", tr)
	}
}

func TestTransportRTUOverTCPSerde(t *testing.T) {
	tr := TransportConfig{Type: "rtu_over_tcp", Host: "10.0.0.1", Port: 502}
	b, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"type":"rtu_over_tcp"`) {
		t.Fatalf("json = %s", b)
	}
	var back TransportConfig
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Host != "10.0.0.1" || back.Port != 502 {
		t.Fatalf("transport = %+v", back)
	}
}

func TestTLSValuesAndDataSourceSurviveRoundtrip(t *testing.T) {
	p := NewSlave()
	serverTLS := &SlaveTLSConfig{Enabled: true, CertFile: "server.pem"}
	clientTLS := &TlsConfig{Enabled: true, CAFile: "ca.pem"}
	step := int16(2)
	p.Connections = []ConnectionConfig{{
		ID: "tls", Name: "TLS",
		Transport: TransportConfig{
			Type: "tcp_tls", Host: "127.0.0.1", Port: 802,
			ClientTLS: clientTLS, ServerTLS: serverTLS,
		},
		TimeoutMs:       3000,
		Requests:        DefaultRequestSettings(),
		ReconnectPolicy: DefaultReconnectPolicy(),
		Devices: []DeviceConfig{{
			SlaveID: 1, Name: "device",
			RegisterDefs: []register.RegisterDef{{
				Address: 7, RegisterType: register.HoldingRegType,
				DataType: register.TypeUInt16, Endian: register.EndianBig,
				Name: "generated",
				DataSource: &datasource.Config{
					Source: datasource.Source{
						Type: datasource.KindCounter, Start: 12, Step: step, Wrap: true,
					},
					UpdateIntervalMs: 250,
				},
			}},
			Values: config.RegisterValues{
				HoldingRegisters: [][2]interface{}{{7, uint16(42)}},
			},
		}},
	}}
	b, err := json.Marshal(&p)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := MigrateProject(string(b))
	if err != nil {
		t.Fatal(err)
	}
	tr := loaded.Connections[0].Transport
	if tr.ClientTLS == nil || !tr.ClientTLS.Enabled || tr.ClientTLS.CAFile != "ca.pem" {
		t.Fatalf("client tls = %+v", tr.ClientTLS)
	}
	if tr.ServerTLS == nil || !tr.ServerTLS.Enabled || tr.ServerTLS.CertFile != "server.pem" {
		t.Fatalf("server tls = %+v", tr.ServerTLS)
	}
	dev := loaded.Connections[0].Devices[0]
	if len(dev.Values.HoldingRegisters) != 1 {
		t.Fatalf("values = %+v", dev.Values)
	}
	if dev.RegisterDefs[0].DataSource == nil {
		t.Fatal("data source should survive")
	}
}

func TestReconnectPolicyDefaultsOnImport(t *testing.T) {
	loaded, err := MigrateProject(`{"version":1,"type":"master","connections":[{
		"id": "c", "name": "c", "transport": {"type": "tcp", "host": "h", "port": 502}
	}]}`)
	if err != nil {
		t.Fatal(err)
	}
	pol := loaded.Connections[0].ReconnectPolicy
	want := DefaultReconnectPolicy()
	if pol != want {
		t.Fatalf("policy = %+v, want %+v", pol, want)
	}
	rt := pol.ToRuntime()
	if rt.InitialDelay != 1000*1000*1000 || rt.MaxDelay != 30000*1000*1000 {
		t.Fatalf("runtime = %+v", rt)
	}
}
