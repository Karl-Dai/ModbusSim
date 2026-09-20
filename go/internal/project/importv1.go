// Reader for v1 project files produced by the Rust implementation.
//
// The shapes below exist only to decode Rust's serialization; they are not
// part of the public model. Six behaviours of the Rust side are reproduced
// here and covered by importv1_test.go:
//
//  1. Transport is an internally tagged enum, flattened into one struct.
//  2. DataType serializes "u_int16"/"u_int32" (serde snake_case of UInt16) but
//     accepts the "uint16"/"uint32" aliases. The legacy block form stores a
//     free-form string that only the alias spellings can parse.
//  3. register_defs was added later, so an early v1 device may carry only the
//     legacy block form in `registers`.
//  4. Values are [[address, value]] tuples and block names are keyed by the
//     decimal address as a string.
//  5. Where Rust demanded a key (reconnect_policy.enabled, scan-group
//     enabled), the importer is lenient and substitutes Rust's Default impl.
//  6. socks5 is absent when disabled.
package project

import (
	"encoding/json"
	"fmt"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

type v1Project struct {
	Version     int            `json:"version"`
	Type        string         `json:"type"` // project.rs:154 renames project_type
	Connections []v1Connection `json:"connections"`
}

// v1Connection is project.rs:122-140. Rust's field-level #[serde(default)]
// becomes an optional pointer here; required keys are read straight through.
type v1Connection struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Transport       json.RawMessage `json:"transport"`
	Devices         []v1Device      `json:"devices"`
	ScanGroups      []v1ScanGroup   `json:"scan_groups"`
	DefaultSlaveID  *uint8          `json:"default_slave_id"`
	TimeoutMS       *uint64         `json:"timeout_ms"`
	Requests        *v1Requests     `json:"requests"`
	ReconnectPolicy *v1Reconnect    `json:"reconnect_policy"`
	Socks5          *v1Socks5       `json:"socks5"`
}

// v1Device is project.rs:73-86: register_defs, registers and values all carry
// serde defaults, so an early file may set only one of them.
type v1Device struct {
	SlaveID      uint8            `json:"slave_id"`
	Name         string           `json:"name"`
	RegisterDefs []v1RegisterDef  `json:"register_defs"`
	Registers    v1Registers      `json:"registers"`
	Values       v1RegisterValues `json:"values"`
}

// v1RegisterDef is register.rs:61-77, whose endian/name/comment carry serde
// defaults and whose mutation/data_source are Options.
type v1RegisterDef struct {
	Address      uint16 `json:"address"`
	RegisterType string `json:"register_type"`
	DataType     string `json:"data_type"`
	Endian       string `json:"endian"`
	Name         string `json:"name"`
	Comment      string `json:"comment"`

	// Mutation and DataSource are decoded with the engine types, whose JSON
	// tags were already made to match Rust: v1 nested these verbatim.
	Mutation   *mutation.Config   `json:"mutation"`
	DataSource *datasource.Config `json:"data_source"`
}

// v1Registers is project.rs:89-99: the pre-point-set block form.
type v1Registers struct {
	Coils          []v1Block `json:"coils"`
	DiscreteInputs []v1Block `json:"discrete_inputs"`
	Holding        []v1Block `json:"holding"`
	Input          []v1Block `json:"input"`
}

// v1Block is project.rs:58-69: data_type/endian are optional strings parsed
// by parse.rs, values are raw JSON, names are keyed by decimal address.
type v1Block struct {
	Address  uint16            `json:"address"`
	Count    uint16            `json:"count"`
	DataType *string           `json:"data_type"`
	Endian   *string           `json:"endian"`
	Values   []json.RawMessage `json:"values"`
	Names    map[string]string `json:"names"`
}

// v1RegisterValues is config.rs:50-59: four skip-if-empty tuple arrays.
type v1RegisterValues struct {
	Coils            [][2]json.RawMessage `json:"coils"`
	DiscreteInputs   [][2]json.RawMessage `json:"discrete_inputs"`
	HoldingRegisters [][2]json.RawMessage `json:"holding_registers"`
	InputRegisters   [][2]json.RawMessage `json:"input_registers"`
}

// v1ScanGroup is project.rs:103-114; only enabled has a serde default.
type v1ScanGroup struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SlaveID      uint8  `json:"slave_id"`
	FunctionCode uint8  `json:"function_code"`
	StartAddress uint16 `json:"start_address"`
	Count        uint16 `json:"count"`
	IntervalMS   uint64 `json:"interval_ms"`
	Enabled      *bool  `json:"enabled"`
}

// v1Requests is request.rs:11-27, a container-level #[serde(default)].
type v1Requests struct {
	IntervalMS       *uint16 `json:"interval_ms"`
	MaxReadRegisters *uint16 `json:"max_read_registers"`
	MaxReadBits      *uint16 `json:"max_read_bits"`
}

// v1Reconnect is reconnect.rs:6-17; Rust required `enabled` but defaulted the
// rest, and max_attempts is an Option meaning unlimited.
type v1Reconnect struct {
	Enabled        *bool    `json:"enabled"`
	InitialDelayMS *uint64  `json:"initial_delay_ms"`
	MaxDelayMS     *uint64  `json:"max_delay_ms"`
	BackoffFactor  *float64 `json:"backoff_factor"`
	MaxAttempts    *uint32  `json:"max_attempts"`
}

// v1Socks5 is socks5.rs:21-27, a container-level #[serde(default)].
type v1Socks5 struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// v1ClientTLS is transport.rs:43-52. Rust required every field inside the
// object; only the enclosing client_tls key was optional.
type v1ClientTLS struct {
	Enabled            bool   `json:"enabled"`
	CAFile             string `json:"ca_file"`
	CertFile           string `json:"cert_file"`
	KeyFile            string `json:"key_file"`
	PKCS12File         string `json:"pkcs12_file"`
	PKCS12Password     string `json:"pkcs12_password"`
	AcceptInvalidCerts bool   `json:"accept_invalid_certs"`
}

// v1ServerTLS is transport.rs:55-63.
type v1ServerTLS struct {
	Enabled           bool   `json:"enabled"`
	CertFile          string `json:"cert_file"`
	KeyFile           string `json:"key_file"`
	CAFile            string `json:"ca_file"`
	RequireClientCert bool   `json:"require_client_cert"`
	PKCS12File        string `json:"pkcs12_file"`
	PKCS12Password    string `json:"pkcs12_password"`
}

// importV1 converts a v1 document into the current model.
func importV1(data []byte) (*Project, error) {
	var old v1Project
	if err := json.Unmarshal(data, &old); err != nil {
		return nil, fmt.Errorf("failed to parse project v1: %w", err)
	}
	out := &Project{Version: CurrentVersion, Connections: make([]*Connection, 0, len(old.Connections))}
	switch old.Type {
	case "slave":
		out.Type = TypeSlave
	case "master":
		out.Type = TypeMaster
	default:
		return nil, fmt.Errorf("failed to parse project v1: unknown project type %q", old.Type)
	}
	for i := range old.Connections {
		conn, err := importConnection(&old.Connections[i])
		if err != nil {
			return nil, err
		}
		out.Connections = append(out.Connections, conn)
	}
	return out, nil
}

func importConnection(old *v1Connection) (*Connection, error) {
	conn := DefaultConnection()
	conn.ID = old.ID
	conn.Name = old.Name

	transport, err := importTransport(old.Transport)
	if err != nil {
		return nil, err
	}
	conn.Transport = *transport
	// Empty collections stay nil: v2 omits them, so importing must not
	// manufacture empty slices that would then read back as nil.
	if len(old.Devices) > 0 {
		conn.Devices = make([]*Device, 0, len(old.Devices))
		for i := range old.Devices {
			dev, err := importDevice(&old.Devices[i])
			if err != nil {
				return nil, err
			}
			conn.Devices = append(conn.Devices, dev)
		}
	}
	for i := range old.ScanGroups {
		conn.ScanGroups = append(conn.ScanGroups, importScanGroup(&old.ScanGroups[i]))
	}
	if old.DefaultSlaveID != nil {
		conn.DefaultSlaveID = *old.DefaultSlaveID
	}
	if old.TimeoutMS != nil {
		conn.TimeoutMS = *old.TimeoutMS
	}
	if old.Requests != nil {
		conn.Requests = importRequests(old.Requests)
	}
	if old.ReconnectPolicy != nil {
		conn.Reconnect = importReconnect(old.ReconnectPolicy)
	}
	if old.Socks5 != nil && old.Socks5.Enabled {
		conn.Socks5 = &Socks5Config{
			Host:     old.Socks5.Host,
			Port:     old.Socks5.Port,
			Username: old.Socks5.Username,
			Password: old.Socks5.Password,
		}
	}
	return conn, nil
}

func importTransport(raw json.RawMessage) (*Transport, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("failed to parse project v1: connection is missing its transport")
	}
	var kind struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &kind); err != nil {
		return nil, fmt.Errorf("failed to parse project v1: %w", err)
	}

	switch kind.Type {
	case "tcp", "rtu_over_tcp":
		var body struct {
			Host string `json:"host"`
			Port uint16 `json:"port"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, fmt.Errorf("failed to parse project v1: %w", err)
		}
		t := Transport{Host: body.Host, Port: body.Port}
		if kind.Type == "tcp" {
			t.Type = TransportTCP
		} else {
			t.Type = TransportRTUOverTCP
		}
		return &t, nil

	case "tcp_tls":
		var body struct {
			Host      string       `json:"host"`
			Port      uint16       `json:"port"`
			ClientTLS *v1ClientTLS `json:"client_tls"`
			ServerTLS *v1ServerTLS `json:"server_tls"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, fmt.Errorf("failed to parse project v1: %w", err)
		}
		t := Transport{Type: TransportTCPTLS, Host: body.Host, Port: body.Port}
		if body.ClientTLS != nil {
			t.TLS = &ClientTLS{
				Enabled:            body.ClientTLS.Enabled,
				CAFile:             body.ClientTLS.CAFile,
				CertFile:           body.ClientTLS.CertFile,
				KeyFile:            body.ClientTLS.KeyFile,
				PKCS12File:         body.ClientTLS.PKCS12File,
				PKCS12Password:     body.ClientTLS.PKCS12Password,
				AcceptInvalidCerts: body.ClientTLS.AcceptInvalidCerts,
			}
		}
		if body.ServerTLS != nil {
			t.ServerTLS = &ServerTLS{
				Enabled:           body.ServerTLS.Enabled,
				CertFile:          body.ServerTLS.CertFile,
				KeyFile:           body.ServerTLS.KeyFile,
				CAFile:            body.ServerTLS.CAFile,
				RequireClientCert: body.ServerTLS.RequireClientCert,
				PKCS12File:        body.ServerTLS.PKCS12File,
				PKCS12Password:    body.ServerTLS.PKCS12Password,
			}
		}
		return &t, nil

	case "rtu", "ascii":
		// Rust spelled the serial variant's fields inline, with "port" as the
		// device path rather than a TCP port number.
		var body struct {
			Port     string `json:"port"`
			BaudRate uint32 `json:"baud_rate"`
			DataBits uint8  `json:"data_bits"`
			StopBits uint8  `json:"stop_bits"`
			Parity   string `json:"parity"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, fmt.Errorf("failed to parse project v1: %w", err)
		}
		t := Transport{Serial: &SerialConfig{
			Port:     body.Port,
			BaudRate: defaultTo(body.BaudRate, 9600),
			DataBits: defaultTo(body.DataBits, 8),
			StopBits: defaultTo(body.StopBits, 1),
			Parity:   importParity(body.Parity),
		}}
		if kind.Type == "rtu" {
			t.Type = TransportRTU
		} else {
			t.Type = TransportASCII
		}
		return &t, nil

	default:
		return nil, fmt.Errorf("failed to parse project v1: unknown transport type %q", kind.Type)
	}
}

func importParity(s string) Parity {
	switch s {
	case "odd":
		return ParityOdd
	case "even":
		return ParityEven
	default:
		return ParityNone
	}
}

func importScanGroup(old *v1ScanGroup) *ScanGroup {
	g := &ScanGroup{
		ID:           old.ID,
		Name:         old.Name,
		SlaveID:      old.SlaveID,
		FunctionCode: old.FunctionCode,
		StartAddress: old.StartAddress,
		Count:        old.Count,
		IntervalMS:   old.IntervalMS,
		Enabled:      true,
	}
	if old.Enabled != nil {
		g.Enabled = *old.Enabled
	}
	return g
}

func importRequests(old *v1Requests) RequestSettings {
	r := DefaultRequestSettings()
	if old.IntervalMS != nil {
		r.IntervalMS = *old.IntervalMS
	}
	if old.MaxReadRegisters != nil {
		r.MaxReadRegisters = *old.MaxReadRegisters
	}
	if old.MaxReadBits != nil {
		r.MaxReadBits = *old.MaxReadBits
	}
	return r
}

func importReconnect(old *v1Reconnect) ReconnectPolicy {
	p := DefaultReconnectPolicy()
	if old.Enabled != nil {
		p.Enabled = *old.Enabled
	}
	if old.InitialDelayMS != nil {
		p.InitialDelayMS = *old.InitialDelayMS
	}
	if old.MaxDelayMS != nil {
		p.MaxDelayMS = *old.MaxDelayMS
	}
	if old.BackoffFactor != nil {
		p.BackoffFactor = *old.BackoffFactor
	}
	p.MaxAttempts = old.MaxAttempts
	return p
}

func importDevice(old *v1Device) (*Device, error) {
	dev := &Device{SlaveID: old.SlaveID, Name: old.Name}
	if len(old.RegisterDefs) == 0 && hasBlocks(old.Registers) {
		// Early v1 files described registers as blocks; expand them the way the
		// Rust app's legacy_register_defs did.
		defs, values, err := expandBlocks(old.Registers)
		if err != nil {
			return nil, err
		}
		dev.Points = defs
		dev.Values = values
		return dev, nil
	}
	for i := range old.RegisterDefs {
		pt, err := importPoint(&old.RegisterDefs[i])
		if err != nil {
			return nil, err
		}
		dev.Points = append(dev.Points, pt)
	}
	imported, err := importValues(old.Values)
	if err != nil {
		return nil, err
	}
	dev.Values = imported
	return dev, nil
}

func importPoint(old *v1RegisterDef) (*Point, error) {
	registerType, err := importRegisterType(old.RegisterType)
	if err != nil {
		return nil, err
	}
	dataType, err := importDataType(old.DataType)
	if err != nil {
		return nil, err
	}
	endian, err := importEndian(old.Endian)
	if err != nil {
		return nil, err
	}
	def := register.RegisterDef{
		Address:      old.Address,
		RegisterType: registerType,
		DataType:     dataType,
		Endian:       endian,
		Name:         old.Name,
		Comment:      old.Comment,
	}
	mut := old.Mutation
	if mut != nil {
		filled := *mut
		if filled.Mode == "" {
			filled.Mode = mutation.ModeFlip
		}
		if filled.PeriodMs == 0 {
			filled.PeriodMs = mutation.DefaultPeriodMs
		}
		mut = &filled
	}
	src := old.DataSource
	if src != nil && src.UpdateIntervalMs == 0 {
		filled := *src
		filled.UpdateIntervalMs = datasource.DefaultUpdateIntervalMs
		src = &filled
	}
	return NewPoint(def, mut, src), nil
}

func importValues(old v1RegisterValues) (Values, error) {
	v := Values{}
	var err error
	if v.Coils, err = importBitPairs(old.Coils); err != nil {
		return Values{}, err
	}
	if v.DiscreteInputs, err = importBitPairs(old.DiscreteInputs); err != nil {
		return Values{}, err
	}
	if v.HoldingRegisters, err = importWordPairs(old.HoldingRegisters); err != nil {
		return Values{}, err
	}
	if v.InputRegisters, err = importWordPairs(old.InputRegisters); err != nil {
		return Values{}, err
	}
	return v, nil
}

// importBitPairs reads Rust's Vec<(u16, bool)> tuple arrays.
func importBitPairs(pairs [][2]json.RawMessage) (map[uint16]bool, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[uint16]bool, len(pairs))
	for _, pair := range pairs {
		var addr uint16
		if err := json.Unmarshal(pair[0], &addr); err != nil {
			return nil, fmt.Errorf("failed to parse project v1: register address: %w", err)
		}
		var value bool
		if err := json.Unmarshal(pair[1], &value); err != nil {
			return nil, fmt.Errorf("failed to parse project v1: bit value at %d: %w", addr, err)
		}
		out[addr] = value
	}
	return out, nil
}

func importWordPairs(pairs [][2]json.RawMessage) (map[uint16]uint16, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[uint16]uint16, len(pairs))
	for _, pair := range pairs {
		var addr uint16
		if err := json.Unmarshal(pair[0], &addr); err != nil {
			return nil, fmt.Errorf("failed to parse project v1: register address: %w", err)
		}
		var value uint16
		if err := json.Unmarshal(pair[1], &value); err != nil {
			return nil, fmt.Errorf("failed to parse project v1: word value at %d: %w", addr, err)
		}
		out[addr] = value
	}
	return out, nil
}

func importRegisterType(s string) (register.RegisterType, error) {
	switch register.RegisterType(s) {
	case register.Coil, register.DiscreteInput, register.InputRegister, register.HoldingRegType:
		return register.RegisterType(s), nil
	}
	return "", fmt.Errorf("failed to parse project v1: unknown register type %q", s)
}

// importDataType accepts both spellings Rust could have written: the serde
// snake_case form ("u_int16") and the alias the read side declared ("uint16").
func importDataType(s string) (register.DataType, error) {
	switch s {
	case "bool":
		return register.TypeBool, nil
	case "u_int16", "uint16":
		return register.TypeUInt16, nil
	case "int16":
		return register.TypeInt16, nil
	case "u_int32", "uint32":
		return register.TypeUInt32, nil
	case "int32":
		return register.TypeInt32, nil
	case "float32":
		return register.TypeFloat, nil
	}
	return "", fmt.Errorf("failed to parse project v1: unknown data type %q", s)
}

// importEndian maps the empty string to Rust's Endian::Big default.
func importEndian(s string) (register.Endian, error) {
	switch s {
	case "":
		return register.DefaultEndian, nil
	case "big":
		return register.EndianBig, nil
	case "little":
		return register.EndianLittle, nil
	case "mid_big":
		return register.EndianMidBig, nil
	case "mid_little":
		return register.EndianMidLittle, nil
	}
	return "", fmt.Errorf("failed to parse project v1: unknown endian %q", s)
}
