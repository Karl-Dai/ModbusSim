// Package config implements configuration serialization for the slave app:
// register value snapshots, device/connection/app configs with validation,
// and JSON import/export. Ported from crates/modbussim-core/src/config.rs
// with identical JSON field names.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// ---------------------------------------------------------------------------
// Register value storage (current values at runtime)
// ---------------------------------------------------------------------------

// RegisterValues holds current values for all four areas as (address, value)
// pairs. JSON uses two-element arrays, matching Rust's Vec<(u16, _)>.
type RegisterValues struct {
	Coils            [][2]interface{} `json:"coils,omitempty"`
	DiscreteInputs   [][2]interface{} `json:"discrete_inputs,omitempty"`
	HoldingRegisters [][2]interface{} `json:"holding_registers,omitempty"`
	InputRegisters   [][2]interface{} `json:"input_registers,omitempty"`
}

// ValuesFromRegisterMap snapshots a RegisterMap (config.rs from_register_map).
func ValuesFromRegisterMap(m *register.RegisterMap) RegisterValues {
	out := RegisterValues{}
	for addr, v := range m.Coils {
		out.Coils = append(out.Coils, [2]interface{}{addr, v})
	}
	for addr, v := range m.DiscreteInputs {
		out.DiscreteInputs = append(out.DiscreteInputs, [2]interface{}{addr, v})
	}
	for addr, v := range m.HoldingRegisters {
		out.HoldingRegisters = append(out.HoldingRegisters, [2]interface{}{addr, v})
	}
	for addr, v := range m.InputRegisters {
		out.InputRegisters = append(out.InputRegisters, [2]interface{}{addr, v})
	}
	return out
}

// ApplyTo writes these values into a RegisterMap (creates points).
func (v *RegisterValues) ApplyTo(m *register.RegisterMap) {
	for _, pair := range v.Coils {
		m.Coils[pairU16(pair[0])] = pairBool(pair[1])
	}
	for _, pair := range v.DiscreteInputs {
		m.DiscreteInputs[pairU16(pair[0])] = pairBool(pair[1])
	}
	for _, pair := range v.HoldingRegisters {
		m.HoldingRegisters[pairU16(pair[0])] = pairU16(pair[1])
	}
	for _, pair := range v.InputRegisters {
		m.InputRegisters[pairU16(pair[0])] = pairU16(pair[1])
	}
}

// ApplyToExisting writes values only to addresses already present in the map,
// preventing malformed project files from creating undeclared points.
func (v *RegisterValues) ApplyToExisting(m *register.RegisterMap) {
	for _, pair := range v.Coils {
		addr := pairU16(pair[0])
		if _, ok := m.Coils[addr]; ok {
			m.Coils[addr] = pairBool(pair[1])
		}
	}
	for _, pair := range v.DiscreteInputs {
		addr := pairU16(pair[0])
		if _, ok := m.DiscreteInputs[addr]; ok {
			m.DiscreteInputs[addr] = pairBool(pair[1])
		}
	}
	for _, pair := range v.HoldingRegisters {
		addr := pairU16(pair[0])
		if _, ok := m.HoldingRegisters[addr]; ok {
			m.HoldingRegisters[addr] = pairU16(pair[1])
		}
	}
	for _, pair := range v.InputRegisters {
		addr := pairU16(pair[0])
		if _, ok := m.InputRegisters[addr]; ok {
			m.InputRegisters[addr] = pairU16(pair[1])
		}
	}
}

func pairU16(v interface{}) uint16 {
	switch n := v.(type) {
	case float64:
		return uint16(n)
	case int:
		return uint16(n)
	case uint16:
		return n
	}
	return 0
}

func pairBool(v interface{}) bool {
	switch b := v.(type) {
	case bool:
		return b
	case float64:
		return b != 0
	}
	return false
}

// ---------------------------------------------------------------------------
// Register definition entry (config export format)
// ---------------------------------------------------------------------------

// RegisterDefEntry is a register definition entry for configuration export.
type RegisterDefEntry struct {
	Address      uint16                   `json:"address"`
	RegisterType register.RegisterType    `json:"type"`
	DataType     register.DataType        `json:"data_type"`
	Endian       register.Endian          `json:"endian"`
	Name         string                   `json:"name,omitempty"`
	Comment      string                   `json:"comment,omitempty"`
	Value        *uint16                  `json:"value,omitempty"`
	Mutation     *register.MutationConfig `json:"mutation,omitempty"`
	DataSource   *datasource.Config       `json:"data_source,omitempty"`
}

// UnmarshalJSON applies the same serde defaults as RegisterDef: endian
// defaults to big, and legacy data-type spellings ("u_int16") normalize.
func (e *RegisterDefEntry) UnmarshalJSON(b []byte) error {
	type raw RegisterDefEntry
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	if r.Endian == "" {
		r.Endian = register.DefaultEndian
	}
	r.DataType = register.NormalizeDataType(r.DataType)
	*e = RegisterDefEntry(r)
	return nil
}

// ToRegisterDef converts to a point definition.
func (e *RegisterDefEntry) ToRegisterDef() register.RegisterDef {
	return register.RegisterDef{
		Address:      e.Address,
		RegisterType: e.RegisterType,
		DataType:     e.DataType,
		Endian:       e.Endian,
		Name:         e.Name,
		Comment:      e.Comment,
		Mutation:     e.Mutation,
		DataSource:   e.DataSource,
	}
}

// ---------------------------------------------------------------------------
// Device configuration
// ---------------------------------------------------------------------------

// DeviceConfig is the configuration for a single slave device.
type DeviceConfig struct {
	SlaveID   uint8              `json:"slave_id"`
	Name      string             `json:"name"`
	Registers []RegisterDefEntry `json:"registers,omitempty"`
}

// Validate checks the device config.
func (c *DeviceConfig) Validate() error {
	if c.SlaveID == 0 {
		return fmt.Errorf("invalid config: slave_id must be between 1 and 247")
	}
	defs := make([]register.RegisterDef, 0, len(c.Registers))
	for i := range c.Registers {
		defs = append(defs, c.Registers[i].ToRegisterDef())
	}
	if err := register.ValidateDefinitions(defs); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Connection configuration
// ---------------------------------------------------------------------------

// ConnectionConfig is the configuration for a slave connection
// (transport + all devices).
type ConnectionConfig struct {
	Transport master.Transport `json:"transport"`
	Devices   []DeviceConfig   `json:"devices,omitempty"`
	AutoStart bool             `json:"auto_start"`
}

func transportPort(t *master.Transport) (uint16, bool) {
	switch t.Type {
	case master.TransportTCP, master.TransportRTUOverTCP:
		return t.Port, true
	}
	return 0, false
}

// Validate checks the connection config.
func (c *ConnectionConfig) Validate() error {
	if port, ok := transportPort(&c.Transport); ok && port == 0 {
		return fmt.Errorf("invalid config: port must be non-zero")
	}
	ids := make([]int, 0, len(c.Devices))
	for i := range c.Devices {
		if err := c.Devices[i].Validate(); err != nil {
			return err
		}
		ids = append(ids, int(c.Devices[i].SlaveID))
	}
	sort.Ints(ids)
	for i := 1; i < len(ids); i++ {
		if ids[i] == ids[i-1] {
			return fmt.Errorf("invalid config: duplicate slave_id %d", ids[i])
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Application state
// ---------------------------------------------------------------------------

// AppConfig is the root configuration file format for ModbusSim.
type AppConfig struct {
	Version     uint32             `json:"version"`
	Name        string             `json:"name,omitempty"`
	Connections []ConnectionConfig `json:"connections,omitempty"`
}

// NewAppConfig returns the default app config (version 1).
func NewAppConfig() AppConfig {
	return AppConfig{Version: 1}
}

// Validate checks the entire app config.
func (c *AppConfig) Validate() error {
	if c.Version == 0 {
		return fmt.Errorf("invalid config: version must be 1 or greater")
	}
	ports := make([]int, 0, len(c.Connections))
	for i := range c.Connections {
		if err := c.Connections[i].Validate(); err != nil {
			return err
		}
		if port, ok := transportPort(&c.Connections[i].Transport); ok {
			ports = append(ports, int(port))
		}
	}
	sort.Ints(ports)
	for i := 1; i < len(ports); i++ {
		if ports[i] == ports[i-1] {
			return fmt.Errorf("invalid config: duplicate port %d across connections", ports[i])
		}
	}
	return nil
}

// ToJSON validates and serializes the config to pretty JSON.
func (c *AppConfig) ToJSON() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to parse JSON: %w", err)
	}
	return string(b), nil
}

// FromJSON parses and validates an app config from JSON.
func FromJSON(s string) (AppConfig, error) {
	var config AppConfig
	if err := json.Unmarshal([]byte(s), &config); err != nil {
		return AppConfig{}, fmt.Errorf("failed to parse JSON: %w", err)
	}
	if err := config.Validate(); err != nil {
		return AppConfig{}, err
	}
	return config, nil
}

// Load reads an app config from a file.
func Load(path string) (AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{}, fmt.Errorf("I/O error: %w", err)
	}
	return FromJSON(string(data))
}

// Save validates and writes the config to a file as pretty JSON.
func (c *AppConfig) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	json, err := c.ToJSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(json), 0o644); err != nil {
		return fmt.Errorf("I/O error: %w", err)
	}
	return nil
}
