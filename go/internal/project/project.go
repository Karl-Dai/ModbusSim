// Package project defines the persisted project-file model and the importer
// for legacy files. It replaces crates/modbussim-core/src/project.rs.
//
// Two formats exist:
//
//   - v1, written by the Rust implementation: snake_case keys, internally
//     tagged transport enums, tuple-array register values. Read-only.
//   - v2, written by this package: Go's default JSON naming (PascalCase with
//     ID/MS/TCP/TLS/RTU/ASCII initialisms), absent keys instead of null for
//     unset optionals, map-keyed register values.
//
// There is deliberately no v2 -> v1 writer: the Rust apps keep reading v1
// and never see a v2 file.
package project

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// CurrentVersion is the format this package writes.
const CurrentVersion = 2

// FileExtension is the project-file suffix used by both applications.
const FileExtension = ".modbusproj"

// ProjectType selects which application owns the project.
type ProjectType string

const (
	TypeSlave  ProjectType = "slave"
	TypeMaster ProjectType = "master"
)

// Project is the top-level document.
type Project struct {
	Version     int
	Type        ProjectType
	Connections []*Connection
}

// New returns an empty project of the given type stamped with CurrentVersion.
func New(t ProjectType) *Project {
	return &Project{Version: CurrentVersion, Type: t, Connections: []*Connection{}}
}

// StampVersion forces the written version, so a hand-built Project cannot
// save itself as a legacy document.
func (p *Project) StampVersion() { p.Version = CurrentVersion }

// Connection is one saved link plus the devices or scan groups that ride on it.
type Connection struct {
	ID   string
	Name string
	// Transport carries only the fields its Type needs; unused fields are
	// omitted rather than written as zero values.
	Transport Transport

	Devices    []*Device    `json:"Devices,omitempty"`
	ScanGroups []*ScanGroup `json:"ScanGroups,omitempty"`

	DefaultSlaveID uint8 `json:"DefaultSlaveID,omitempty"`
	TimeoutMS      uint64
	Requests       RequestSettings
	Reconnect      ReconnectPolicy
	// Socks5 is nil when no proxy is configured; an enabled proxy always has
	// a host and port, so there is no separate Enabled flag.
	Socks5 *Socks5Config `json:"Socks5,omitempty"`
}

// DefaultConnection mirrors ConnectionConfig's serde defaults in project.rs.
func DefaultConnection() *Connection {
	return &Connection{
		Transport:      Transport{Type: TransportTCP, Host: "127.0.0.1", Port: 502},
		DefaultSlaveID: 1,
		TimeoutMS:      3000,
		Requests:       DefaultRequestSettings(),
		Reconnect:      DefaultReconnectPolicy(),
	}
}

// TransportType selects the wire protocol. The values match Rust so a v1
// "tcp_tls" and a v2 "tcp_tls" mean the same thing.
type TransportType string

const (
	TransportTCP        TransportType = "tcp"
	TransportTCPTLS     TransportType = "tcp_tls"
	TransportRTU        TransportType = "rtu"
	TransportASCII      TransportType = "ascii"
	TransportRTUOverTCP TransportType = "rtu_over_tcp"
)

// IsSerial reports whether the transport runs on a serial port.
func (t TransportType) IsSerial() bool {
	return t == TransportRTU || t == TransportASCII
}

// IsTCP reports whether the transport opens a TCP socket (and therefore
// honours the reconnect policy and the SOCKS5 proxy).
func (t TransportType) IsTCP() bool {
	return t == TransportTCP || t == TransportTCPTLS || t == TransportRTUOverTCP
}

// Transport is the connection target. Unlike Rust's tagged enum this is one
// struct; Type decides which of the optional members are read.
type Transport struct {
	Type TransportType

	Host string `json:"Host,omitempty"`
	Port uint16 `json:"Port,omitempty"`

	Serial *SerialConfig `json:"Serial,omitempty"`

	// TLS is the master-side client identity; ServerTLS is the slave-side
	// listener identity. Only tcp_tls reads either.
	TLS       *ClientTLS `json:"TLS,omitempty"`
	ServerTLS *ServerTLS `json:"ServerTLS,omitempty"`
}

// SerialConfig is a serial port setting shared by RTU and ASCII.
type SerialConfig struct {
	Port     string
	BaudRate uint32
	DataBits uint8
	StopBits uint8
	Parity   Parity
}

// Parity mirrors serial.Parity's string values.
type Parity string

const (
	ParityNone Parity = "none"
	ParityOdd  Parity = "odd"
	ParityEven Parity = "even"
)

// DefaultSerialConfig mirrors SerialConfig::default().
func DefaultSerialConfig() *SerialConfig {
	return &SerialConfig{BaudRate: 9600, DataBits: 8, StopBits: 1, Parity: ParityNone}
}

// ClientTLS mirrors transport.rs's TlsConfig (master connects as a client).
type ClientTLS struct {
	Enabled            bool
	CAFile             string `json:"CAFile,omitempty"`
	CertFile           string `json:"CertFile,omitempty"`
	KeyFile            string `json:"KeyFile,omitempty"`
	PKCS12File         string `json:"PKCS12File,omitempty"`
	PKCS12Password     string `json:"PKCS12Password,omitempty"`
	AcceptInvalidCerts bool   `json:"AcceptInvalidCerts,omitempty"`
}

// ServerTLS mirrors transport.rs's SlaveTlsConfig (slave listens as a server).
type ServerTLS struct {
	Enabled           bool
	CertFile          string `json:"CertFile,omitempty"`
	KeyFile           string `json:"KeyFile,omitempty"`
	CAFile            string `json:"CAFile,omitempty"`
	RequireClientCert bool   `json:"RequireClientCert,omitempty"`
	PKCS12File        string `json:"PKCS12File,omitempty"`
	PKCS12Password    string `json:"PKCS12Password,omitempty"`
}

// Socks5Config mirrors socks5.rs's Socks5Config minus its Enabled flag.
type Socks5Config struct {
	Host     string
	Port     uint16
	Username string `json:"Username,omitempty"`
	Password string `json:"Password,omitempty"`
}

// Device is one simulated slave stored in a slave project.
type Device struct {
	SlaveID uint8
	Name    string   `json:"Name,omitempty"`
	Points  []*Point `json:"Points,omitempty"`
	Values  Values
}

// Values holds raw register contents. v2 uses maps keyed by address, which Go
// encodes as JSON objects; v1 stored [[address, value]] tuples.
type Values struct {
	Coils            map[uint16]bool   `json:"Coils,omitempty"`
	DiscreteInputs   map[uint16]bool   `json:"DiscreteInputs,omitempty"`
	HoldingRegisters map[uint16]uint16 `json:"HoldingRegisters,omitempty"`
	InputRegisters   map[uint16]uint16 `json:"InputRegisters,omitempty"`
}

// Point is one defined register, its interpretation, and its optional
// behaviour. register.RegisterDef is not embedded because its own JSON tags
// would reintroduce snake_case keys into the v2 file.
type Point struct {
	Address      uint16
	RegisterType register.RegisterType
	DataType     register.DataType
	Endian       register.Endian `json:"Endian,omitempty"`
	Name         string          `json:"Name,omitempty"`
	Comment      string          `json:"Comment,omitempty"`

	Mutation   *MutationConfig   `json:"Mutation,omitempty"`
	DataSource *DataSourceConfig `json:"DataSource,omitempty"`
}

// MutationConfig mirrors mutation.Config with v2 key names.
type MutationConfig struct {
	Enabled  bool
	Mode     mutation.Mode
	PeriodMS uint64 `json:"PeriodMS,omitempty"`
	Step     float64
	Min      float64
	Max      float64
}

// DefaultMutationConfig mirrors mutation::Default.
func DefaultMutationConfig() *MutationConfig {
	return &MutationConfig{Mode: mutation.ModeFlip, PeriodMS: mutation.DefaultPeriodMs, Step: 1, Max: 100}
}

// DataSourceConfig mirrors datasource.Config with v2 key names.
type DataSourceConfig struct {
	Source           DataSource
	UpdateIntervalMS uint64 `json:"UpdateIntervalMS,omitempty"`
}

// DataSource mirrors datasource.Source. The union stays flat because the
// generator in internal/datasource reads exactly these alternatives.
type DataSource struct {
	Type datasource.SourceKind

	Value uint16 `json:"Value,omitempty"`

	Min uint16 `json:"Min,omitempty"`
	Max uint16 `json:"Max,omitempty"`

	Amplitude float64 `json:"Amplitude,omitempty"`
	Frequency float64 `json:"Frequency,omitempty"`
	Offset    float64 `json:"Offset,omitempty"`
	Phase     float64 `json:"Phase,omitempty"`

	PeriodMS uint64 `json:"PeriodMS,omitempty"`

	Start uint16 `json:"Start,omitempty"`
	Step  int16  `json:"Step,omitempty"`
	Wrap  bool   `json:"Wrap,omitempty"`

	Values       []uint16 `json:"Values,omitempty"`
	LoopPlayback bool     `json:"LoopPlayback,omitempty"`
}

// ScanGroup is a periodic read performed by a master project.
type ScanGroup struct {
	ID           string `json:"ID,omitempty"`
	Name         string
	SlaveID      uint8
	FunctionCode uint8
	StartAddress uint16
	Count        uint16
	IntervalMS   uint64
	Enabled      bool
}

// RequestSettings mirrors request.rs's RequestSettings.
type RequestSettings struct {
	IntervalMS       uint16
	MaxReadRegisters uint16
	MaxReadBits      uint16
}

// DefaultRequestSettings mirrors RequestSettings::default().
func DefaultRequestSettings() RequestSettings {
	return RequestSettings{MaxReadRegisters: 125, MaxReadBits: 2000}
}

// ReconnectPolicy mirrors reconnect.rs's ReconnectPolicy. MaxAttempts is nil
// for unlimited retries.
type ReconnectPolicy struct {
	Enabled        bool
	InitialDelayMS uint64
	MaxDelayMS     uint64
	BackoffFactor  float64
	MaxAttempts    *uint32 `json:"MaxAttempts,omitempty"`
}

// DefaultReconnectPolicy mirrors ReconnectPolicy::default().
func DefaultReconnectPolicy() ReconnectPolicy {
	return ReconnectPolicy{Enabled: true, InitialDelayMS: 1000, MaxDelayMS: 30000, BackoffFactor: 2}
}

// InitialDelay exposes the policy as durations for callers that drive it.
func (p ReconnectPolicy) Delays() (initial, max time.Duration) {
	return time.Duration(p.InitialDelayMS) * time.Millisecond,
		time.Duration(p.MaxDelayMS) * time.Millisecond
}

// Save writes the project as pretty-printed JSON.
func Save(w io.Writer, p *Project) error {
	p.StampVersion()
	enc := json.NewEncoder(w)
	// Rust's serializer leaves <, > and & alone in names and comments.
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		return fmt.Errorf("failed to serialize project: %w", err)
	}
	return nil
}

// SaveFile writes the project to path, creating it with mode 0644.
func SaveFile(path string, p *Project) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to write project file: %w", err)
	}
	if err := Save(f, p); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Load reads a project of either format, dispatching on its version field.
func Load(r io.Reader) (*Project, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read project file: %w", err)
	}
	return LoadBytes(data)
}

// LoadFile reads a project from disk.
func LoadFile(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read project file: %w", err)
	}
	return LoadBytes(data)
}

// LoadBytes migrates a document: v2 decodes natively, v1 goes through the
// importer, anything else is rejected the way Rust rejected unknown versions.
func LoadBytes(data []byte) (*Project, error) {
	var head struct{ Version int }
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	switch head.Version {
	case CurrentVersion:
		var p Project
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("failed to parse project v%d: %w", CurrentVersion, err)
		}
		return &p, nil
	case 1:
		return importV1(data)
	default:
		return nil, fmt.Errorf("unsupported project version: %d", head.Version)
	}
}
