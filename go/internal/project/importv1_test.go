package project

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func mustImport(t *testing.T, name string) *Project {
	t.Helper()
	p, err := LoadBytes(fixture(t, name))
	if err != nil {
		t.Fatalf("import %s: %v", name, err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("imported %s fails validation: %v", name, err)
	}
	if p.Version != CurrentVersion {
		t.Errorf("%s imported as version %d", name, p.Version)
	}
	return p
}

func TestImportV1SlaveTLS(t *testing.T) {
	p := mustImport(t, "v1-slave-tls.json")
	if p.Type != TypeSlave {
		t.Errorf("Type = %q", p.Type)
	}
	if len(p.Connections) != 1 {
		t.Fatalf("got %d connections", len(p.Connections))
	}
	conn := p.Connections[0]
	if conn.ID != "conn_tls" || conn.Name != "TLS listener" {
		t.Errorf("identity = %q/%q", conn.ID, conn.Name)
	}
	if conn.DefaultSlaveID != 11 || conn.TimeoutMS != 1500 {
		t.Errorf("defaults = %d/%d", conn.DefaultSlaveID, conn.TimeoutMS)
	}

	tr := conn.Transport
	if tr.Type != TransportTCPTLS || tr.Host != "0.0.0.0" || tr.Port != 502 {
		t.Errorf("transport = %#v", tr)
	}
	if tr.Serial != nil {
		t.Errorf("tcp_tls must not carry serial settings: %#v", tr.Serial)
	}
	if tr.TLS == nil || !tr.TLS.Enabled || tr.TLS.CAFile != "/certs/ca.pem" ||
		tr.TLS.PKCS12File != "/certs/client.p12" || tr.TLS.PKCS12Password != "clientpw" || tr.TLS.AcceptInvalidCerts {
		t.Errorf("client_tls = %#v", tr.TLS)
	}
	if tr.ServerTLS == nil || !tr.ServerTLS.Enabled || !tr.ServerTLS.RequireClientCert ||
		tr.ServerTLS.CAFile != "/certs/ca.pem" || tr.ServerTLS.PKCS12File != "/certs/server.p12" ||
		tr.ServerTLS.PKCS12Password != "serverpw" {
		t.Errorf("server_tls = %#v", tr.ServerTLS)
	}

	wantRequests := RequestSettings{IntervalMS: 20, MaxReadRegisters: 100, MaxReadBits: 1000}
	if conn.Requests != wantRequests {
		t.Errorf("requests = %#v, want %#v", conn.Requests, wantRequests)
	}
	wantReconnect := ReconnectPolicy{Enabled: false, InitialDelayMS: 250, MaxDelayMS: 5000, BackoffFactor: 1.5}
	if conn.Reconnect != wantReconnect {
		t.Errorf("reconnect = %#v, want %#v", conn.Reconnect, wantReconnect)
	}
	if conn.Reconnect.MaxAttempts != nil {
		t.Errorf("an explicit null max_attempts must mean unlimited, got %d", *conn.Reconnect.MaxAttempts)
	}
	if conn.Socks5 == nil || conn.Socks5.Host != "proxy.internal" || conn.Socks5.Port != 1080 ||
		conn.Socks5.Username != "ops" || conn.Socks5.Password != "pw" {
		t.Errorf("socks5 = %#v", conn.Socks5)
	}
}

func TestImportV1Points(t *testing.T) {
	p := mustImport(t, "v1-slave-tls.json")
	dev := p.Connections[0].Devices[0]
	if dev.SlaveID != 11 || dev.Name != "Boiler" {
		t.Fatalf("device = %d/%q", dev.SlaveID, dev.Name)
	}
	if len(dev.Points) != 6 {
		t.Fatalf("got %d points", len(dev.Points))
	}

	byAddress := map[uint16]*Point{}
	for _, pt := range dev.Points {
		byAddress[pt.Address] = pt
	}

	pump := byAddress[0]
	if pump.RegisterType != register.Coil || pump.DataType != register.TypeBool || pump.Name != "Pump" || pump.Comment != "circulation pump" {
		t.Errorf("coil = %#v", pump)
	}
	if pump.Endian != register.EndianBig {
		t.Errorf("missing endian must default to big, got %q", pump.Endian)
	}
	if pump.Mutation == nil || !pump.Mutation.Enabled || pump.Mutation.Mode != mutation.ModeFlip || pump.Mutation.PeriodMS != 500 {
		t.Errorf("mutation = %#v", pump.Mutation)
	}

	setpoint := byAddress[100]
	if setpoint.DataType != register.TypeUInt16 || setpoint.Endian != register.EndianBig || setpoint.Mutation != nil {
		t.Errorf("u_int16 spelling = %#v", setpoint)
	}
	energy := byAddress[200]
	if energy.DataType != register.TypeUInt32 || energy.Endian != register.EndianMidBig || energy.Comment != "" {
		t.Errorf("u_int32 spelling = %#v", energy)
	}
	alias := byAddress[500]
	if alias.DataType != register.TypeUInt16 {
		t.Errorf("the uint16 alias must import as %q, got %q", register.TypeUInt16, alias.DataType)
	}
	if alias.Endian != register.EndianBig {
		t.Errorf("missing endian on an aliased point = %q", alias.Endian)
	}

	temp := byAddress[300]
	if temp.DataType != register.TypeFloat || temp.Endian != register.EndianLittle {
		t.Errorf("temperature = %#v", temp)
	}
	if temp.DataSource == nil {
		t.Fatalf("temperature lost its data source")
	}
	src := temp.DataSource
	if src.UpdateIntervalMS != 250 {
		t.Errorf("update_interval_ms = %d", src.UpdateIntervalMS)
	}
	if src.Source.Type != datasource.KindSine || src.Source.Amplitude != 20 || src.Source.Frequency != 0.5 ||
		src.Source.Offset != 60 || src.Source.Phase != 0.25 {
		t.Errorf("sine source = %#v", src.Source)
	}
	engine := temp.ToDataSource()
	if engine == nil || engine.Source.Type != datasource.KindSine || engine.UpdateIntervalMs != 250 {
		t.Errorf("ToDataSource() = %#v", engine)
	}
	if got := temp.ToDef(); got.Address != 300 || got.DataType != register.TypeFloat || got.Endian != register.EndianLittle {
		t.Errorf("ToDef() = %#v", got)
	}
	alarm := byAddress[400]
	if alarm.RegisterType != register.DiscreteInput || alarm.DataSource != nil {
		t.Errorf("discrete input = %#v", alarm)
	}
}

func TestImportV1ValuesBecomeMaps(t *testing.T) {
	p := mustImport(t, "v1-slave-tls.json")
	values := p.Connections[0].Devices[0].Values
	want := Values{
		Coils:            map[uint16]bool{0: true},
		DiscreteInputs:   map[uint16]bool{400: false},
		HoldingRegisters: map[uint16]uint16{100: 1234, 200: 0, 201: 4660},
		InputRegisters:   map[uint16]uint16{500: 7},
	}
	if !reflect.DeepEqual(values, want) {
		t.Errorf("values =\n%#v\nwant\n%#v", values, want)
	}
	m := values.ToRegisterMap()
	if m.HoldingRegisters[201] != 4660 || !m.Coils[0] {
		t.Errorf("register map = %#v", m)
	}
}

func TestImportV1LegacyBlocks(t *testing.T) {
	p := mustImport(t, "v1-slave-legacy-blocks.json")
	conn := p.Connections[0]
	if conn.Transport.Type != TransportRTU {
		t.Errorf("transport = %q", conn.Transport.Type)
	}
	serial := conn.Transport.Serial
	if serial == nil || serial.Port != "/dev/tty.usbserial" || serial.BaudRate != 19200 ||
		serial.DataBits != 8 || serial.StopBits != 2 || serial.Parity != ParityEven {
		t.Errorf("serial = %#v", serial)
	}

	dev := conn.Devices[0]
	// A uint32 block of count 4 yields two points, not four; bit blocks stay
	// one point per address.
	if len(dev.Points) != 3+2+2+1 {
		t.Fatalf("got %d points: %#v", len(dev.Points), dev.Points)
	}
	byAddress := map[uint16]*Point{}
	for _, pt := range dev.Points {
		byAddress[pt.Address] = pt
	}
	if byAddress[0].Name != "Run" || byAddress[1].Name != "" || byAddress[2].Name != "Fault" {
		t.Errorf("coil names = %q/%q/%q", byAddress[0].Name, byAddress[1].Name, byAddress[2].Name)
	}
	if byAddress[100].Name != "Total" || byAddress[102].Name != "Relative" {
		t.Errorf("holding names = %q/%q", byAddress[100].Name, byAddress[102].Name)
	}
	if byAddress[100].DataType != register.TypeUInt32 || byAddress[100].Endian != register.EndianMidLittle {
		t.Errorf("holding point = %#v", byAddress[100])
	}
	if byAddress[20].DataType != register.TypeFloat || byAddress[20].RegisterType != register.InputRegister {
		t.Errorf("input point = %#v", byAddress[20])
	}
	if byAddress[10].RegisterType != register.DiscreteInput || byAddress[10].DataType != register.TypeBool {
		t.Errorf("discrete input point = %#v", byAddress[10])
	}
	for _, pt := range dev.Points {
		if pt.Mutation != nil || pt.DataSource != nil {
			t.Errorf("legacy point %d carried behaviour it never had: %#v", pt.Address, pt)
		}
	}

	want := Values{
		Coils:            map[uint16]bool{0: true, 1: false, 2: true},
		DiscreteInputs:   map[uint16]bool{10: false, 11: false},
		HoldingRegisters: map[uint16]uint16{100: 1, 101: 2, 102: 3, 103: 4},
		InputRegisters:   map[uint16]uint16{20: 0, 21: 0},
	}
	if !reflect.DeepEqual(dev.Values, want) {
		t.Errorf("block values =\n%#v\nwant\n%#v", dev.Values, want)
	}
}

func TestImportV1MasterSample(t *testing.T) {
	p := mustImport(t, "v1-master-saved.json")
	if p.Type != TypeMaster {
		t.Errorf("Type = %q", p.Type)
	}
	if len(p.Connections) != 2 {
		t.Fatalf("got %d connections", len(p.Connections))
	}
	first := p.Connections[0]
	if first.Transport.Type != TransportTCP || first.Transport.Host != "127.0.0.1" || first.Transport.Port != 54264 {
		t.Errorf("transport = %#v", first.Transport)
	}
	group := first.ScanGroups[0]
	if group.ID != "f601a28f-1dad-4373-a6ad-2b091d02ce35" || group.Name != "SG-1488" ||
		group.SlaveID != 1 || group.FunctionCode != 3 || group.StartAddress != 0 ||
		group.Count != 130 || group.IntervalMS != 1000 || !group.Enabled {
		t.Errorf("scan group = %#v", group)
	}
	if first.Requests.MaxReadRegisters != 40 || first.Requests.MaxReadBits != 64 || first.Requests.IntervalMS != 50 {
		t.Errorf("requests = %#v", first.Requests)
	}
	if first.Reconnect.MaxAttempts == nil || *first.Reconnect.MaxAttempts != 3 {
		t.Errorf("max_attempts = %#v", first.Reconnect.MaxAttempts)
	}
	if first.Socks5 != nil {
		t.Errorf("a v1 file omits socks5 when disabled, got %#v", first.Socks5)
	}
	if len(first.Devices) != 0 {
		t.Errorf("master project carried %d devices", len(first.Devices))
	}

	second := p.Connections[1]
	if second.Transport.Type != TransportRTUOverTCP || second.Transport.Port != 503 {
		t.Errorf("rtu_over_tcp = %#v", second.Transport)
	}
	if second.Socks5 != nil {
		t.Errorf(`"enabled": false must import as no proxy, got %#v`, second.Socks5)
	}
	if second.Reconnect.MaxAttempts != nil {
		t.Errorf("a missing max_attempts must mean unlimited, got %d", *second.Reconnect.MaxAttempts)
	}
}

func TestImportV1FillsRustDefaults(t *testing.T) {
	p := mustImport(t, "v1-minimal.json")
	conn := p.Connections[0]
	if conn.DefaultSlaveID != 1 {
		t.Errorf("default_slave_id = %d, want 1", conn.DefaultSlaveID)
	}
	if conn.TimeoutMS != 3000 {
		t.Errorf("timeout_ms = %d, want 3000", conn.TimeoutMS)
	}
	if conn.Requests != DefaultRequestSettings() {
		t.Errorf("requests = %#v, want %#v", conn.Requests, DefaultRequestSettings())
	}
	if conn.Reconnect != DefaultReconnectPolicy() {
		t.Errorf("reconnect = %#v, want %#v", conn.Reconnect, DefaultReconnectPolicy())
	}
	if conn.Socks5 != nil {
		t.Errorf("socks5 = %#v", conn.Socks5)
	}
	serial := conn.Transport.Serial
	if serial.BaudRate != 9600 || serial.DataBits != 8 || serial.StopBits != 1 || serial.Parity != ParityNone {
		t.Errorf("zero serial fields should fall back to defaults: %#v", serial)
	}
	if !conn.ScanGroups[0].Enabled {
		t.Error("a scan group without an enabled key is enabled in v1")
	}
	if conn.ScanGroups[0].ID != "" {
		t.Errorf("id = %q", conn.ScanGroups[0].ID)
	}
}

func TestImportV1RejectsUnknownTokens(t *testing.T) {
	tests := []struct{ name, json, wants string }{
		{
			name:  "future version",
			json:  `{"version": 3, "type": "master", "connections": []}`,
			wants: "unsupported project version: 3",
		},
		{
			name:  "unknown project type",
			json:  `{"version": 1, "type": "plant", "connections": []}`,
			wants: `unknown project type "plant"`,
		},
		{
			name:  "unknown transport",
			json:  `{"version": 1, "type": "master", "connections": [{"id":"a","name":"b","transport":{"type":"udp"}}]}`,
			wants: `unknown transport type "udp"`,
		},
		{
			name:  "transport missing",
			json:  `{"version": 1, "type": "master", "connections": [{"id":"a","name":"b"}]}`,
			wants: "missing its transport",
		},
		{
			name:  "tcp port as a string",
			json:  `{"version": 1, "type": "master", "connections": [{"id":"a","name":"b","transport":{"type":"tcp","host":"h","port":"502"}}]}`,
			wants: "failed to parse project v1",
		},
		{
			name:  "unknown register type",
			json:  slaveWithDef(`"register_type": "memory", "data_type": "u_int16"`),
			wants: `unknown register type "memory"`,
		},
		{
			name:  "unknown data type",
			json:  slaveWithDef(`"register_type": "holding_register", "data_type": "float64"`),
			wants: `unknown data type "float64"`,
		},
		{
			name:  "unknown endian",
			json:  slaveWithDef(`"register_type": "holding_register", "data_type": "u_int16", "endian": "middle"`),
			wants: `unknown endian "middle"`,
		},
		{
			name:  "tuple value of the wrong type",
			json:  `{"version":1,"type":"slave","connections":[{"id":"a","name":"b","transport":{"type":"tcp","host":"h","port":1},"devices":[{"slave_id":1,"values":{"coils":[["x",true]]}}]}]}`,
			wants: "register address",
		},
		{
			name:  "legacy block count not divisible",
			json:  `{"version":1,"type":"slave","connections":[{"id":"a","name":"b","transport":{"type":"tcp","host":"h","port":1},"devices":[{"slave_id":1,"registers":{"holding":[{"address":0,"count":3,"data_type":"uint32"}]}}]}]}`,
			wants: "not divisible by data width 2",
		},
		{
			name:  "legacy word value out of range",
			json:  `{"version":1,"type":"slave","connections":[{"id":"a","name":"b","transport":{"type":"tcp","host":"h","port":1},"devices":[{"slave_id":1,"registers":{"holding":[{"address":0,"count":1,"values":[70000]}]}}]}]}`,
			wants: "invalid word value at legacy address 0",
		},
		{
			name:  "legacy bit value not a bool or number",
			json:  `{"version":1,"type":"slave","connections":[{"id":"a","name":"b","transport":{"type":"tcp","host":"h","port":1},"devices":[{"slave_id":1,"registers":{"coils":[{"address":0,"count":1,"values":["yes"]}]}}]}]}`,
			wants: "invalid bit value at legacy address 0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := LoadBytes([]byte(tc.json))
			if err == nil {
				t.Fatalf("accepted a broken document: %#v", p)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("got %q, want it to contain %q", err, tc.wants)
			}
		})
	}
}

// TestImportThenSaveThenLoadIsStable checks that the importer's output is a
// fixed point: writing v2 and reading it back must not drift.
func TestImportThenSaveThenLoadIsStable(t *testing.T) {
	for _, name := range []string{"v1-slave-tls.json", "v1-slave-legacy-blocks.json", "v1-master-saved.json", "v1-minimal.json"} {
		t.Run(name, func(t *testing.T) {
			original := mustImport(t, name)

			var first bytes.Buffer
			if err := Save(&first, original); err != nil {
				t.Fatalf("Save: %v", err)
			}
			reread, err := Load(bytes.NewReader(first.Bytes()))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !reflect.DeepEqual(reread, original) {
				t.Errorf("v2 round trip drifted:\n%#v\n%#v", reread, original)
			}
			var second bytes.Buffer
			if err := Save(&second, reread); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if first.String() != second.String() {
				t.Errorf("re-saving changed the bytes:\n%s\n%s", first.String(), second.String())
			}
			if !strings.Contains(first.String(), `"Version": 2`) {
				t.Errorf("migrated file is not stamped v2:\n%s", first.String())
			}
			if strings.Contains(first.String(), `"register_type"`) || strings.Contains(first.String(), `"u_int16"`) {
				t.Errorf("migrated file still carries v1 tokens:\n%s", first.String())
			}
		})
	}
}

func TestImportedV1DataSourcesSatisfyTheEngine(t *testing.T) {
	p := mustImport(t, "v1-slave-tls.json")
	for _, pt := range p.Connections[0].Devices[0].Points {
		if cfg := pt.ToDataSource(); cfg != nil {
			if err := cfg.Validate(); err != nil {
				t.Errorf("point %d: data source rejected: %v", pt.Address, err)
			}
		}
		if cfg := pt.ToMutation(); cfg != nil {
			if cfg.Mode == "" || cfg.PeriodMs == 0 {
				t.Errorf("point %d: mutation defaults not filled: %#v", pt.Address, cfg)
			}
		}
	}
}

// slaveWithDef wraps one register definition in a minimal v1 slave project.
func slaveWithDef(def string) string {
	return `{"version":1,"type":"slave","connections":[{"id":"a","name":"b",` +
		`"transport":{"type":"tcp","host":"h","port":1},` +
		`"devices":[{"slave_id":1,"register_defs":[{"address":7,` + def + `}]}]}]}`
}

func TestImportV1KeepsUnmigratedTokensOutOfTheModel(t *testing.T) {
	// DataType and the transport tag are the two places where a careless port
	// would leak a serde spelling into v2 output.
	p := mustImport(t, "v1-slave-tls.json")
	var buf bytes.Buffer
	if err := Save(&buf, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("Save output unreadable: %v", err)
	}
	types := map[register.DataType]bool{}
	for _, pt := range p.Connections[0].Devices[0].Points {
		types[pt.DataType] = true
	}
	if types[register.DataType("u_int16")] {
		t.Error("u_int16 survived into the v2 model")
	}
	if !types[register.TypeUInt16] || !types[register.TypeUInt32] {
		t.Errorf("data types lost: %#v", types)
	}
}
