// Package project implements the .modbusproj project-file format:
// ProjectFile / ConnectionConfig / DeviceConfig with transport variants,
// plus version migration on load. Ported from
// crates/modbussim-core/src/project.rs with byte-compatible JSON.
package project

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/config"
	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/socks5"
)

// ProjectType discriminates slave and master projects.
type ProjectType string

const (
	TypeSlave  ProjectType = "slave"
	TypeMaster ProjectType = "master"
)

// TlsConfig (master side) and SlaveTlsConfig reuse the transport-layer
// structs; their JSON tags already match serde.
type TlsConfig = master.TLSConfig

// ReconnectPolicy is the project-file form of master.ReconnectPolicy with
// millisecond fields (serde-compatible; the runtime type uses Duration).
type ReconnectPolicy struct {
	Enabled        bool    `json:"enabled"`
	InitialDelayMs uint64  `json:"initial_delay_ms"`
	MaxDelayMs     uint64  `json:"max_delay_ms"`
	BackoffFactor  float64 `json:"backoff_factor"`
	MaxAttempts    uint32  `json:"max_attempts"` // 0 = unlimited
}

// DefaultReconnectPolicy mirrors serde defaults (enabled, 1000/30000/2/unlimited).
func DefaultReconnectPolicy() ReconnectPolicy {
	return ReconnectPolicy{Enabled: true, InitialDelayMs: 1000, MaxDelayMs: 30000, BackoffFactor: 2}
}

// ToRuntime converts to the master-side runtime policy.
func (p ReconnectPolicy) ToRuntime() master.ReconnectPolicy {
	return master.ReconnectPolicy{
		Enabled:       p.Enabled,
		InitialDelay:  msDuration(p.InitialDelayMs, 1000),
		MaxDelay:      msDuration(p.MaxDelayMs, 30000),
		BackoffFactor: p.BackoffFactor,
		MaxAttempts:   p.MaxAttempts,
	}
}

func msDuration(ms uint64, fallback uint64) time.Duration {
	if ms == 0 {
		ms = fallback
	}
	return time.Duration(ms) * time.Millisecond
}

// RequestSettings aliases the master type (JSON tags already match serde).
type RequestSettings = master.RequestSettings

// DefaultRequestSettings mirrors master.DefaultRequestSettings.
func DefaultRequestSettings() RequestSettings {
	return master.DefaultRequestSettings()
}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

// TransportConfig mirrors the serde internally-tagged TransportConfig enum.
// Serial variants inline their fields with a string "port"; TCP variants use
// a numeric "port". Use NewTCP/NewSerial constructors and the Accessors.
type TransportConfig struct {
	Type      string          `json:"type"`
	Host      string          `json:"host,omitempty"`
	Port      uint16          `json:"port,omitempty"`
	PortName  string          `json:"portName,omitempty"` // never serialized; see MarshalJSON
	BaudRate  uint32          `json:"baud_rate,omitempty"`
	DataBits  uint8           `json:"data_bits,omitempty"`
	StopBits  uint8           `json:"stop_bits,omitempty"`
	Parity    string          `json:"parity,omitempty"`
	ClientTLS *TlsConfig      `json:"client_tls,omitempty"`
	ServerTLS *SlaveTLSConfig `json:"server_tls,omitempty"`
}

// SlaveTLSConfig mirrors Rust's SlaveTlsConfig (slave side).
type SlaveTLSConfig struct {
	Enabled           bool   `json:"enabled"`
	CertFile          string `json:"cert_file"`
	KeyFile           string `json:"key_file"`
	CAFile            string `json:"ca_file"`
	RequireClientCert bool   `json:"require_client_cert"`
	PKCS12File        string `json:"pkcs12_file"`
	PKCS12Password    string `json:"pkcs12_password"`
}

// NewTCP builds a TCP transport config.
func NewTCP(host string, port uint16) TransportConfig {
	return TransportConfig{Type: "tcp", Host: host, Port: port}
}

// NewSerial builds an RTU/ASCII serial transport config; ascii selects ASCII.
func NewSerial(ascii bool, port string, baud uint32, dataBits, stopBits uint8, parity string) TransportConfig {
	t := "rtu"
	if ascii {
		t = "ascii"
	}
	return TransportConfig{Type: t, PortName: port, BaudRate: baud, DataBits: dataBits, StopBits: stopBits, Parity: parity}
}

// MarshalJSON writes the serde shape: serial "port" is a string, TCP "port"
// a number; TLS blocks are emitted only when set (serde Option).
func (t TransportConfig) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{"type": t.Type}
	switch t.Type {
	case "rtu", "ascii":
		m["port"] = t.PortName
		m["baud_rate"] = t.BaudRate
		m["data_bits"] = t.DataBits
		m["stop_bits"] = t.StopBits
		m["parity"] = t.Parity
	default:
		m["port"] = t.Port
		if t.Host != "" {
			m["host"] = t.Host
		}
		if t.ClientTLS != nil {
			m["client_tls"] = t.ClientTLS
		}
		if t.ServerTLS != nil {
			m["server_tls"] = t.ServerTLS
		}
	}
	return json.Marshal(m)
}

// UnmarshalJSON dispatches on the "type" tag so both spellings of "port"
// land in the right field.
func (t *TransportConfig) UnmarshalJSON(b []byte) error {
	var probe struct {
		Type  string          `json:"type"`
		Host  string          `json:"host"`
		Port  json.RawMessage `json:"port"`
		Baud  uint32          `json:"baud_rate"`
		Bits  uint8           `json:"data_bits"`
		Stop  uint8           `json:"stop_bits"`
		Par   string          `json:"parity"`
		ClTLS *TlsConfig      `json:"client_tls"`
		SvTLS *SlaveTLSConfig `json:"server_tls"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	t.Type = probe.Type
	t.Host = probe.Host
	t.BaudRate = probe.Baud
	t.DataBits = probe.Bits
	t.StopBits = probe.Stop
	t.Parity = probe.Par
	t.ClientTLS = probe.ClTLS
	t.ServerTLS = probe.SvTLS
	switch probe.Type {
	case "rtu", "ascii":
		var s string
		if len(probe.Port) > 0 {
			if err := json.Unmarshal(probe.Port, &s); err != nil {
				return err
			}
		}
		t.PortName = s
		t.Port = 0
	default:
		var p uint16
		if len(probe.Port) > 0 {
			if err := json.Unmarshal(probe.Port, &p); err != nil {
				return err
			}
		}
		t.Port = p
		t.PortName = ""
	}
	return nil
}

// ---------------------------------------------------------------------------
// Register blocks and devices
// ---------------------------------------------------------------------------

// RegisterBlockConfig is a register block definition (legacy block format).
type RegisterBlockConfig struct {
	Address  uint16            `json:"address"`
	Count    uint16            `json:"count"`
	DataType *string           `json:"data_type,omitempty"`
	Endian   *string           `json:"endian,omitempty"`
	Values   []json.RawMessage `json:"values,omitempty"`
	Names    map[string]string `json:"names,omitempty"`
}

// RegistersConfig groups register blocks by type.
type RegistersConfig struct {
	Coils          []RegisterBlockConfig `json:"coils,omitempty"`
	DiscreteInputs []RegisterBlockConfig `json:"discrete_inputs,omitempty"`
	Holding        []RegisterBlockConfig `json:"holding,omitempty"`
	Input          []RegisterBlockConfig `json:"input,omitempty"`
}

// DeviceConfig is a slave device definition in a project file.
type DeviceConfig struct {
	SlaveID      uint8                  `json:"slave_id"`
	Name         string                 `json:"name,omitempty"`
	RegisterDefs []register.RegisterDef `json:"register_defs,omitempty"`
	Registers    RegistersConfig        `json:"registers"`
	Values       config.RegisterValues  `json:"values"`
}

// ScanGroupConfig is a scan group definition (master projects).
type ScanGroupConfig struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	SlaveID      uint8  `json:"slave_id"`
	FunctionCode uint8  `json:"function_code"`
	StartAddress uint16 `json:"start_address"`
	Count        uint16 `json:"count"`
	IntervalMs   uint64 `json:"interval_ms"`
	Enabled      bool   `json:"enabled"`
}

// ConnectionConfig is one connection definition in a project file.
type ConnectionConfig struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Transport       TransportConfig   `json:"transport"`
	Devices         []DeviceConfig    `json:"devices,omitempty"`
	ScanGroups      []ScanGroupConfig `json:"scan_groups,omitempty"`
	DefaultSlaveID  uint8             `json:"default_slave_id"`
	TimeoutMs       uint64            `json:"timeout_ms"`
	Requests        RequestSettings   `json:"requests"`
	ReconnectPolicy ReconnectPolicy   `json:"reconnect_policy"`
	Socks5          *socks5.Config    `json:"socks5,omitempty"`
}

// UnmarshalJSON applies serde's field defaults for missing values.
func (c *ConnectionConfig) UnmarshalJSON(b []byte) error {
	type raw ConnectionConfig
	r := raw{
		DefaultSlaveID:  1,
		TimeoutMs:       3000,
		Requests:        DefaultRequestSettings(),
		ReconnectPolicy: DefaultReconnectPolicy(),
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	// serde skip_serializing_if for socks5 means absent == disabled.
	if r.Socks5 == nil {
		r.Socks5 = nil
	}
	*c = ConnectionConfig(r)
	return nil
}

// ---------------------------------------------------------------------------
// Project file
// ---------------------------------------------------------------------------

// ProjectFile is the top-level project file structure.
type ProjectFile struct {
	Version     uint32             `json:"version"`
	Type        ProjectType        `json:"type"`
	Connections []ConnectionConfig `json:"connections"`
}

// NewSlave returns an empty slave project (version 1).
func NewSlave() ProjectFile {
	return ProjectFile{Version: 1, Type: TypeSlave}
}

// NewMaster returns an empty master project (version 1).
func NewMaster() ProjectFile {
	return ProjectFile{Version: 1, Type: TypeMaster}
}

// SaveProject writes the project as pretty-printed JSON.
func SaveProject(p *ProjectFile, path string) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize project: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("failed to write project file: %w", err)
	}
	return nil
}

// LoadProject reads a project file, migrating older formats.
func LoadProject(path string) (ProjectFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectFile{}, fmt.Errorf("failed to read project file: %w", err)
	}
	return MigrateProject(string(data))
}

// MigrateProject parses project JSON and migrates it to the current version.
// Only version 1 exists so far.
func MigrateProject(data string) (ProjectFile, error) {
	var probe struct {
		Version *uint64 `json:"version"`
	}
	if err := json.Unmarshal([]byte(data), &probe); err != nil {
		return ProjectFile{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if probe.Version == nil {
		return ProjectFile{}, fmt.Errorf("missing or invalid version field")
	}
	switch *probe.Version {
	case 1:
		var p ProjectFile
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return ProjectFile{}, fmt.Errorf("failed to parse project v1: %w", err)
		}
		return p, nil
	default:
		return ProjectFile{}, fmt.Errorf("unsupported project version: %d", *probe.Version)
	}
}
