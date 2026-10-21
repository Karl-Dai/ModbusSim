// Package masterstate owns desktop-independent Modbus master sessions.
package masterstate

import (
	"fmt"
	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"github.com/Karl-Dai/ModbusSim/go/internal/project"
	"github.com/Karl-Dai/ModbusSim/go/internal/serial"
	"github.com/Karl-Dai/ModbusSim/go/internal/socks5"
	"time"
)

type Settings struct {
	Transport project.Transport
	SlaveID   uint8
	TimeoutMS uint64
	Requests  project.RequestSettings
	Reconnect project.ReconnectPolicy
	Socks5    *project.Socks5Config
}

func DefaultSettings() Settings {
	p := project.DefaultConnection()
	return Settings{Transport: p.Transport, SlaveID: p.DefaultSlaveID, TimeoutMS: p.TimeoutMS, Requests: p.Requests, Reconnect: p.Reconnect}
}
func (s Settings) validate() error {
	p := s.project("", nil)
	if err := p.Validate(); err != nil {
		return err
	}
	if s.TimeoutMS < 1 || s.TimeoutMS > 600000 {
		return fmt.Errorf("timeout must be between 1 and 600000 ms")
	}
	if s.Reconnect.MaxAttempts != nil && (*s.Reconnect.MaxAttempts == 0 || *s.Reconnect.MaxAttempts > 10000) {
		return fmt.Errorf("reconnect attempts must be between 1 and 10000, or null for unlimited")
	}
	if s.Transport.Type.IsSerial() {
		if s.Socks5 != nil {
			return fmt.Errorf("SOCKS5 is not available for serial transports")
		}
		sc := s.Transport.Serial
		if sc == nil || !(serial.Config{Port: sc.Port, BaudRate: sc.BaudRate, DataBits: sc.DataBits, StopBits: sc.StopBits, Parity: serial.Parity(sc.Parity)}).Valid() {
			return fmt.Errorf("invalid serial configuration")
		}
		if sc.Parity != "none" && sc.Parity != "odd" && sc.Parity != "even" {
			return fmt.Errorf("invalid serial parity")
		}
	}
	if s.Socks5 != nil {
		p := s.Socks5
		cfg := socks5.Config{Enabled: true, Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password}
		if err := cfg.Validate(); err != nil {
			return err
		}
	}
	if tls := s.Transport.TLS; tls != nil && ((tls.CertFile == "") != (tls.KeyFile == "")) {
		return fmt.Errorf("client certificate and key must both be set")
	}
	return nil
}
func (s Settings) engine() *master.Connection {
	cfg := master.Config{TargetAddress: s.Transport.Host, Port: s.Transport.Port, SlaveID: s.SlaveID, TimeoutMs: s.TimeoutMS, Requests: master.RequestSettings{IntervalMs: s.Requests.IntervalMS, MaxReadRegisters: s.Requests.MaxReadRegisters, MaxReadBits: s.Requests.MaxReadBits}}
	if t := s.Transport.TLS; t != nil {
		cfg.TLS = master.TLSConfig{Enabled: t.Enabled, CAFile: t.CAFile, CertFile: t.CertFile, KeyFile: t.KeyFile, PKCS12File: t.PKCS12File, PKCS12Password: t.PKCS12Password, AcceptInvalidCerts: t.AcceptInvalidCerts}
	}
	if p := s.Socks5; p != nil {
		cfg.Socks5 = socks5.Config{Enabled: true, Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password}
	}
	tr := master.Transport{Type: master.TransportKind(s.Transport.Type), Host: s.Transport.Host, Port: s.Transport.Port}
	if sc := s.Transport.Serial; sc != nil {
		tr.Serial = &serial.Config{Port: sc.Port, BaudRate: sc.BaudRate, DataBits: sc.DataBits, StopBits: sc.StopBits, Parity: serial.Parity(sc.Parity)}
	}
	return master.NewConnection(cfg, tr)
}
func (s Settings) project(id string, groups []*project.ScanGroup) *project.Connection {
	return &project.Connection{ID: id, Transport: s.Transport, DefaultSlaveID: s.SlaveID, TimeoutMS: s.TimeoutMS, Requests: s.Requests, Reconnect: s.Reconnect, Socks5: s.Socks5, ScanGroups: groups}
}

type ConnectionInfo struct {
	ID             string
	TargetAddress  string
	Port           uint16
	SlaveID        uint8
	State          string
	ScanGroupCount int
}
type ScanGroup struct {
	ID           string
	Name         string
	Function     string
	StartAddress uint16
	Quantity     uint16
	IntervalMS   uint64
	Enabled      bool
	IsPolling    bool
	SlaveID      *uint8
}

func (g ScanGroup) engine() (*master.ScanGroup, error) {
	f, err := readFunction(g.Function)
	if err != nil {
		return nil, err
	}
	out := &master.ScanGroup{ID: g.ID, Name: g.Name, Function: f, StartAddress: g.StartAddress, Quantity: g.Quantity, IntervalMs: g.IntervalMS, Enabled: g.Enabled, SlaveID: g.SlaveID}
	return out, out.Validate()
}
func readFunction(s string) (master.ReadFunction, error) {
	switch s {
	case "read_coils":
		return master.ReadCoils, nil
	case "read_discrete_inputs":
		return master.ReadDiscreteInputs, nil
	case "read_holding_registers":
		return master.ReadHoldingRegisters, nil
	case "read_input_registers":
		return master.ReadInputRegisters, nil
	}
	return 0, fmt.Errorf("unknown read function: %s", s)
}
func functionName(fc uint8) (string, error) {
	names := map[uint8]string{1: "read_coils", 2: "read_discrete_inputs", 3: "read_holding_registers", 4: "read_input_registers"}
	s, ok := names[fc]
	if !ok {
		return "", fmt.Errorf("unsupported read function code %d", fc)
	}
	return s, nil
}

type RegisterValue struct {
	Address      uint16
	RawValue     uint16
	DisplayValue string
	IsBool       bool
}
type ReadResult struct {
	ScanGroupID  string
	Function     string
	StartAddress uint16
	Values       []RegisterValue
	Timestamp    string
	Error        *string
}

func resultDTO(g ScanGroup, r *master.ReadResult) ReadResult {
	out := ReadResult{ScanGroupID: g.ID, Function: g.Function, StartAddress: g.StartAddress, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Values: []RegisterValue{}}
	if r.Bits != nil {
		for i, v := range r.Bits {
			n := uint16(0)
			label := "OFF"
			if v {
				n = 1
				label = "ON"
			}
			out.Values = append(out.Values, RegisterValue{uint16(uint32(g.StartAddress) + uint32(i)), n, label, true})
		}
	} else {
		for i, v := range r.Registers {
			out.Values = append(out.Values, RegisterValue{uint16(uint32(g.StartAddress) + uint32(i)), v, fmt.Sprint(v), false})
		}
	}
	return out
}

type ConnectionStateEvent struct {
	ConnectionID string
	State        string
}
type PollDataEvent struct {
	ConnectionID string
	ScanGroupID  string
	Result       ReadResult
}
type PollErrorEvent struct {
	ConnectionID string
	ScanGroupID  string
	Error        string
}
type LogEvent struct{ ConnectionID string }
type LogEntry struct {
	Timestamp    string
	Direction    string
	FunctionCode string
	Detail       string
}
type Events struct {
	Emit func(name string, payload any)
}
type SlaveScanRequest struct {
	StartID   uint8
	EndID     uint8
	TimeoutMS uint64
}
type RegisterScanRequest struct {
	Function     string
	StartAddress uint16
	EndAddress   uint16
	ChunkSize    uint16
	TimeoutMS    uint64
}
type FoundRegister struct {
	Address uint16
	Value   uint16
}
type SlaveScanEvent struct {
	ConnectionID string
	TaskID       string
	CurrentID    uint8
	Total        uint16
	FoundIDs     []uint8
	Done         bool
	Cancelled    bool
}
type RegisterScanEvent struct {
	ConnectionID   string
	TaskID         string
	CurrentAddress uint16
	EndAddress     uint16
	FoundCount     int
	FoundRegisters []FoundRegister
	Done           bool
	Cancelled      bool
}
