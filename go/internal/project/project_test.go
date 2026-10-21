package project

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := sampleProject()
	var buf bytes.Buffer
	if err := Save(&buf, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(&buf)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip changed the project:\n got %#v\nwant %#v", got, p)
	}
}

func TestSaveUsesGoNames(t *testing.T) {
	var buf bytes.Buffer
	if err := Save(&buf, sampleProject()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	var keys []string
	collectKeys(t, doc, "", &keys)
	for _, key := range keys {
		if isAddressKey(key) {
			continue // register areas are keyed by address, not by field name
		}
		if strings.Contains(key, "_") {
			t.Errorf("key %q is snake_case", key)
			continue
		}
		if key[0] < 'A' || key[0] > 'Z' {
			t.Errorf("key %q does not start with an uppercase letter", key)
		}
	}
	want := []string{"Version", "Type", "Connections", "ID", "TimeoutMS", "DefaultSlaveID", "MaxReadRegisters", "ScanGroups", "BackoffFactor", "MaxAttempts", "SlaveID", "Points", "Values", "HoldingRegisters", "RegisterType", "DataSource", "PeriodMS", "FunctionCode", "UpdateIntervalMS"}
	for _, key := range want {
		if !containsString(keys, key) {
			t.Errorf("expected key %q in output, got %v", key, keys)
		}
	}

	csv, err := json.Marshal(DataSource{Type: datasource.KindCsvPlayback, Values: []uint16{1, 2}, LoopPlayback: true})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{`"Type":"csv_playback"`, `"LoopPlayback":true`, `"Values":[1,2]`} {
		if !strings.Contains(string(csv), key) {
			t.Errorf("DataSource JSON missing %s: %s", key, csv)
		}
	}
}

func TestSaveOmitsUnsetPointers(t *testing.T) {
	p := New(TypeMaster)
	p.Connections = append(p.Connections, &Connection{
		ID: "c1", Name: "plain",
		Transport:      Transport{Type: TransportTCP, Host: "127.0.0.1", Port: 502},
		DefaultSlaveID: 1,
		Requests:       DefaultRequestSettings(),
	})
	var buf bytes.Buffer
	if err := Save(&buf, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out := buf.String()
	for _, banned := range []string{`"Socks5":null`, `"MaxAttempts":null`, `"Serial":null`, `"TLS":null`, `"ServerTLS":null`, `"DataSource":null`, `"Mutation":null`, `"Values":null`} {
		if strings.Contains(out, banned) {
			t.Errorf("unset fields must be absent, found %s in %s", banned, out)
		}
	}
	// A TCP transport has nothing to say about serial ports or TLS material.
	for _, banned := range []string{"Serial", "TLS", "ServerTLS", "Socks5", "MaxAttempts"} {
		if strings.Contains(out, `"`+banned+`"`) {
			t.Errorf("transport carried irrelevant field %s: %s", banned, out)
		}
	}
}

func TestSaveStampsVersion(t *testing.T) {
	p := sampleProject()
	p.Version = 1
	var buf bytes.Buffer
	if err := Save(&buf, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.Contains(buf.String(), `"Version": 2`) {
		t.Errorf("Save must stamp the current version:\n%s", buf.String())
	}
	if p.Version != CurrentVersion {
		t.Errorf("Save left Version=%d", p.Version)
	}
}

func TestLoadRejectsUnknownVersion(t *testing.T) {
	p, err := LoadBytes([]byte(`{"Version": 99, "Type": "master", "Connections": []}`))
	if err == nil {
		t.Fatalf("accepted version 99, returned %#v", p)
	}
	if !strings.Contains(err.Error(), "unsupported project version: 99") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	if _, err := LoadBytes([]byte(`{"Version":`)); err == nil {
		t.Error("accepted truncated JSON")
	}
	if _, err := LoadBytes([]byte(`{}`)); err == nil {
		t.Error("accepted a document without a version")
	}
}

func TestSaveFileAndLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proj"+FileExtension)
	if err := SaveFile(path, sampleProject()); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !reflect.DeepEqual(got, sampleProject()) {
		t.Error("file round trip changed the project")
	}
}

func TestValuesRoundTripThroughRegisterMap(t *testing.T) {
	values := Values{
		Coils:            map[uint16]bool{0: true, 7: false},
		DiscreteInputs:   map[uint16]bool{3: true},
		HoldingRegisters: map[uint16]uint16{100: 1234, 200: 0},
		InputRegisters:   map[uint16]uint16{20: 5},
	}
	m := values.ToRegisterMap()
	if !m.Coils[0] || !m.DiscreteInputs[3] || m.HoldingRegisters[100] != 1234 || m.InputRegisters[20] != 5 {
		t.Fatalf("map materialisation lost values: %#v", m)
	}
	if len(m.Coils) != 2 || len(m.HoldingRegisters) != 2 {
		t.Errorf("expected all addresses present: %#v", m)
	}
	if got := ValuesFromMap(m); !reflect.DeepEqual(got, values) {
		t.Errorf("snapshot differs:\n got %#v\nwant %#v", got, values)
	}
}

func TestPointConversions(t *testing.T) {
	def := register.RegisterDef{
		Address: 300, RegisterType: register.InputRegister,
		DataType: register.TypeFloat, Endian: register.EndianLittle,
		Name: "Temperature", Comment: "pt100",
	}
	mut := &mutation.Config{Enabled: false, Mode: mutation.ModeIncrement, PeriodMs: 250, Step: 2, Min: 0, Max: 10}
	src := &datasource.Config{
		Source:           datasource.Source{Type: datasource.KindSine, Amplitude: 20, Frequency: 0.5, Offset: 60, Phase: 0.25},
		UpdateIntervalMs: 250,
	}
	pt := NewPoint(def, mut, src)
	if got := pt.ToDef(); got != def {
		t.Errorf("ToDef() = %#v, want %#v", got, def)
	}
	if got := pt.ToMutation(); got == nil || *got != *mut {
		t.Errorf("ToMutation() = %#v, want %#v", got, mut)
	}
	engine := pt.ToDataSource()
	if err := engine.Validate(); err != nil {
		t.Errorf("imported data source rejected by the engine: %v", err)
	}
	if engine.Source.Type != datasource.KindSine || engine.Source.Amplitude != 20 || engine.UpdateIntervalMs != 250 {
		t.Errorf("ToDataSource() = %#v", engine)
	}
}

func TestValidateCatchesBadDocuments(t *testing.T) {
	tests := []struct {
		name  string
		s     func(*Project)
		wants string
	}{
		{
			name:  "missing connection id",
			s:     func(p *Project) { p.Connections[0].ID = "" },
			wants: "missing an id",
		},
		{
			name:  "duplicate connection id",
			s:     func(p *Project) { p.Connections[1].ID = p.Connections[0].ID },
			wants: "duplicate connection id",
		},
		{
			name:  "tcp without a port",
			s:     func(p *Project) { p.Connections[0].Transport.Port = 0 },
			wants: "port must be between",
		},
		{
			name:  "serial without a port",
			s:     func(p *Project) { p.Connections[1].Transport.Serial.Port = "" },
			wants: "serial port is required",
		},
		{
			name:  "default slave id out of range",
			s:     func(p *Project) { p.Connections[0].DefaultSlaveID = 248 },
			wants: "between 1 and 247",
		},
		{
			name:  "request batch too large",
			s:     func(p *Project) { p.Connections[0].Requests.MaxReadRegisters = 126 },
			wants: "registers per read request",
		},
		{
			name: "reconnect factor below one",
			s: func(p *Project) {
				p.Connections[0].Reconnect.Enabled = true
				p.Connections[0].Reconnect.BackoffFactor = 0.5
			},
			wants: "backoff factor must be between 1 and 10",
		},
		{
			name:  "socks5 without credentials",
			s:     func(p *Project) { p.Connections[0].Socks5.Password = "pw" },
			wants: "both be set or both be empty",
		},
		{
			name:  "overlapping points",
			s:     func(p *Project) { p.Connections[0].Devices[0].Points[2].Address = 100 },
			wants: "overlaps point at address",
		},
		{
			name: "mutation and data source together",
			s: func(p *Project) {
				p.Connections[0].Devices[0].Points[0].DataSource = &DataSourceConfig{Source: DataSource{Type: datasource.KindFixed}}
			},
			wants: "cannot use mutation and a data source simultaneously",
		},
		{
			name: "zero-interval data source",
			s: func(p *Project) {
				p.Connections[0].Devices[0].Points[0].Mutation.Enabled = false
				p.Connections[0].Devices[0].Points[0].DataSource = &DataSourceConfig{Source: DataSource{Type: datasource.KindSawtooth}, UpdateIntervalMS: 0}
			},
			wants: "update interval must be greater than zero",
		},
		{
			name:  "scan group beyond the address space",
			s:     func(p *Project) { g := p.Connections[1].ScanGroups[0]; g.StartAddress = 65535; g.Count = 2 },
			wants: "read range must contain",
		},
		{
			name: "client auth without a CA",
			s: func(p *Project) {
				p.Connections[0].Transport.ServerTLS = &ServerTLS{Enabled: true, RequireClientCert: true, PKCS12File: "s.p12"}
			},
			wants: "requires a CA file",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := sampleProject()
			tc.s(p)
			err := p.Validate()
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wants)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("got %q, want it to contain %q", err, tc.wants)
			}
		})
	}
}

func TestValidateAcceptsSample(t *testing.T) {
	if err := sampleProject().Validate(); err != nil {
		t.Fatalf("sample project rejected: %v", err)
	}
}

// sampleProject builds a v2 document that exercises every field group.
func sampleProject() *Project {
	attempts := uint32(5)
	p := New(TypeSlave)
	tcp := &Connection{
		ID:   "conn_tls",
		Name: "TLS listener",
		Transport: Transport{
			Type: TransportTCPTLS, Host: "0.0.0.0", Port: 502,
			TLS:       &ClientTLS{Enabled: true, CAFile: "ca.pem", PKCS12File: "client.p12", PKCS12Password: "pw"},
			ServerTLS: &ServerTLS{Enabled: true, CAFile: "ca.pem", RequireClientCert: true, PKCS12File: "server.p12", PKCS12Password: "pw"},
		},
		Devices: []*Device{{
			SlaveID: 11,
			Name:    "Boiler",
			Points: []*Point{
				{Address: 0, RegisterType: register.Coil, DataType: register.TypeBool, Endian: register.DefaultEndian, Name: "Pump", Mutation: &MutationConfig{Enabled: true, Mode: mutation.ModeFlip, PeriodMS: 500, Step: 1, Max: 1}},
				{Address: 100, RegisterType: register.HoldingRegType, DataType: register.TypeUInt16, Endian: register.EndianBig, Name: "Setpoint"},
				{Address: 200, RegisterType: register.HoldingRegType, DataType: register.TypeUInt32, Endian: register.EndianMidBig, Name: "Energy"},
				{Address: 300, RegisterType: register.InputRegister, DataType: register.TypeFloat, Endian: register.EndianLittle, Name: "Temperature", DataSource: &DataSourceConfig{
					Source:           DataSource{Type: datasource.KindSine, Amplitude: 20, Frequency: 0.5, Offset: 60, Phase: 0.25},
					UpdateIntervalMS: 250,
				}},
			},
			Values: Values{
				Coils:            map[uint16]bool{0: true},
				HoldingRegisters: map[uint16]uint16{100: 1234, 200: 0, 201: 4660},
				InputRegisters:   map[uint16]uint16{300: 0, 301: 0},
			},
		}},
		DefaultSlaveID: 11,
		TimeoutMS:      1500,
		Requests:       RequestSettings{IntervalMS: 20, MaxReadRegisters: 100, MaxReadBits: 1000},
		Reconnect:      ReconnectPolicy{Enabled: false, InitialDelayMS: 250, MaxDelayMS: 5000, BackoffFactor: 1.5},
		Socks5:         &Socks5Config{Host: "proxy.internal", Port: 1080},
	}
	rtu := &Connection{
		ID:        "conn_rtu",
		Name:      "RTU gateway",
		Transport: Transport{Type: TransportRTU, Serial: &SerialConfig{Port: "/dev/tty.usbserial", BaudRate: 19200, DataBits: 8, StopBits: 2, Parity: ParityEven}},
		ScanGroups: []*ScanGroup{{
			ID: "sg1", Name: "SG-1488", SlaveID: 1, FunctionCode: 3,
			StartAddress: 0, Count: 130, IntervalMS: 1000, Enabled: true,
		}},
		DefaultSlaveID: 1,
		TimeoutMS:      3000,
		Requests:       DefaultRequestSettings(),
		Reconnect:      ReconnectPolicy{Enabled: true, InitialDelayMS: 200, MaxDelayMS: 1000, BackoffFactor: 2, MaxAttempts: &attempts},
	}
	p.Connections = append(p.Connections, tcp, rtu)
	return p
}

func isAddressKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func collectKeys(t *testing.T, node interface{}, path string, out *[]string) {
	t.Helper()
	switch v := node.(type) {
	case map[string]interface{}:
		for key, child := range v {
			*out = append(*out, key)
			collectKeys(t, child, path+"."+key, out)
		}
	case []interface{}:
		for _, child := range v {
			collectKeys(t, child, path, out)
		}
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
