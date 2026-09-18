// Tests for the config.rs port: validation, value round trips, JSON
// import/export and cross-platform compatibility with Rust-authored files.
package config

import (
	"strings"
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func TestDeviceConfigValidation(t *testing.T) {
	c := DeviceConfig{SlaveID: 1, Name: "Test"}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	c.SlaveID = 0
	if err := c.Validate(); err == nil {
		t.Fatal("slave_id 0 should be rejected")
	}
}

func TestDeviceConfigRejectsOverlap(t *testing.T) {
	c := DeviceConfig{SlaveID: 1, Registers: []RegisterDefEntry{
		{Address: 10, RegisterType: register.HoldingRegType, DataType: register.TypeFloat, Endian: register.EndianBig},
		{Address: 11, RegisterType: register.HoldingRegType, DataType: register.TypeUInt16, Endian: register.EndianBig},
	}}
	if err := c.Validate(); err == nil {
		t.Fatal("multi-word overlap should be rejected")
	}
}

func TestRegisterValuesRoundtrip(t *testing.T) {
	m := register.NewRegisterMap()
	m.WriteHoldingRegister(0, 100)
	m.WriteHoldingRegister(1, 200)
	m.WriteCoil(5, true)

	values := ValuesFromRegisterMap(m)
	m2 := register.NewRegisterMap()
	values.ApplyTo(m2)

	if got := m2.ReadHoldingRegisters(0, 2); got[0] != 100 || got[1] != 200 {
		t.Fatalf("holding = %v", got)
	}
	if got := m2.ReadCoils(5, 1); !got[0] {
		t.Fatal("coil 5 should be true")
	}
}

func TestRegisterValuesApplyToExisting(t *testing.T) {
	m := register.NewRegisterMap()
	m.WriteHoldingRegister(0, 1)
	v := RegisterValues{HoldingRegisters: [][2]interface{}{{0, uint16(9)}, {50, uint16(7)}}}
	v.ApplyToExisting(m)
	if got := m.ReadHoldingRegisters(0, 1); got[0] != 9 {
		t.Fatalf("existing addr should update, got %v", got)
	}
	if _, ok := m.HoldingRegisters[50]; ok {
		t.Fatal("undeclared addr must not be created")
	}
}

func TestAppConfigJSONExportImport(t *testing.T) {
	config := NewAppConfig()
	config.Name = "Test Config"

	json, err := config.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	imported, err := FromJSON(json)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Version != 1 || imported.Name != "Test Config" {
		t.Fatalf("imported = %+v", imported)
	}
}

func TestAppConfigDuplicatePorts(t *testing.T) {
	mkConn := func() ConnectionConfig {
		return ConnectionConfig{Transport: masterTransport("0.0.0.0", 502)}
	}
	config := NewAppConfig()
	config.Connections = []ConnectionConfig{mkConn(), mkConn()}
	if err := config.Validate(); err == nil {
		t.Fatal("duplicate ports should be rejected")
	}
}

func TestCrossPlatformCompatibility(t *testing.T) {
	// A config authored by the Rust app: u_int16 spelling, "type" key for
	// the register type, two-element value arrays.
	json := `{
		"version": 1,
		"name": "CrossPlatform Test",
		"connections": [{
			"transport": {"type": "tcp", "host": "0.0.0.0", "port": 502},
			"devices": [{
				"slave_id": 1, "name": "Device 1",
				"registers": [{
					"address": 0, "type": "holding_register", "data_type": "u_int16",
					"endian": "big", "name": "HR0", "comment": "Test register", "value": 1234
				}]
			}],
			"auto_start": false
		}]
	}`
	config, err := FromJSON(json)
	if err != nil {
		t.Fatal(err)
	}
	dev := config.Connections[0].Devices[0]
	if dev.SlaveID != 1 || len(dev.Registers) != 1 || dev.Registers[0].Value == nil || *dev.Registers[0].Value != 1234 {
		t.Fatalf("devices = %+v", dev)
	}
	if dev.Registers[0].DataType != register.TypeUInt16 {
		t.Fatalf("data type = %q", dev.Registers[0].DataType)
	}

	exported, err := config.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromJSON(exported); err != nil {
		t.Fatalf("re-import failed: %v", err)
	}
}

func TestMutationConfigSurvivesRoundtrip(t *testing.T) {
	config := NewAppConfig()
	period := uint64(750)
	config.Connections = []ConnectionConfig{{
		Transport: masterTransport("0.0.0.0", 5020),
		Devices: []DeviceConfig{{
			SlaveID: 7, Name: "Pump",
			Registers: []RegisterDefEntry{{
				Address: 10, RegisterType: register.HoldingRegType,
				DataType: register.TypeFloat, Endian: register.EndianBig,
				Name: "speed",
				Mutation: &register.MutationConfig{
					Enabled: true, Mode: register.MutationModeIncrement,
					PeriodMs: period, Step: 0.5, Min: 0, Max: 10,
				},
			}},
		}},
	}}

	json, err := config.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := FromJSON(json)
	if err != nil {
		t.Fatal(err)
	}
	mut := loaded.Connections[0].Devices[0].Registers[0].Mutation
	if mut == nil || mut.Mode != register.MutationModeIncrement || mut.PeriodMs != 750 {
		t.Fatalf("mutation = %+v", mut)
	}
}

func TestMutationPeriodDefaultOnImport(t *testing.T) {
	// A mutation block without period_ms gets the serde default (1000).
	config, err := FromJSON(`{
		"version": 1,
		"connections": [{
			"transport": {"type": "tcp", "host": "0.0.0.0", "port": 502},
			"devices": [{"slave_id": 1, "registers": [{
				"address": 0, "type": "coil", "data_type": "bool",
				"mutation": {"enabled": true, "mode": "flip", "step": 1, "min": 0, "max": 1}
			}]}]
		}]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	mut := config.Connections[0].Devices[0].Registers[0].Mutation
	if mut == nil || mut.PeriodMs != register.DefaultMutationPeriodMs {
		t.Fatalf("mutation = %+v", mut)
	}
}

func TestEndianDefaultsToBig(t *testing.T) {
	config, err := FromJSON(`{
		"version": 1,
		"connections": [{
			"transport": {"type": "tcp", "host": "0.0.0.0", "port": 502},
			"devices": [{"slave_id": 1, "registers": [
				{"address": 0, "type": "holding_register", "data_type": "uint16"}
			]}]
		}]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	e := config.Connections[0].Devices[0].Registers[0].Endian
	if e != register.EndianBig {
		t.Fatalf("endian = %q", e)
	}
}

func TestFromJSONErrors(t *testing.T) {
	if _, err := FromJSON("not json"); err == nil || !strings.Contains(err.Error(), "failed to parse JSON") {
		t.Fatalf("err = %v", err)
	}
	bad := NewAppConfig()
	bad.Version = 0
	if _, err := bad.ToJSON(); err == nil {
		t.Fatal("version 0 should fail validation")
	}
}

func masterTransport(host string, port uint16) master.Transport {
	return master.Transport{Type: master.TransportTCP, Host: host, Port: port}
}
