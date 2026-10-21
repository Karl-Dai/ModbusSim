// Package master implements the Modbus master (client) side across the five
// transports, with request pacing, batched reads, and scan-group polling.
// Ported from crates/modbussim-core/src/{master,rtu_master,ascii_master,
// rtu_tcp_master,tls_master,request,reconnect}.rs with identical semantics.
package master

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/serial"
	"github.com/Karl-Dai/ModbusSim/go/internal/socks5"
)

// TransportKind selects the connection transport (serde tag values match
// Rust's Transport enum).
type TransportKind string

const (
	TransportTCP        TransportKind = "tcp"
	TransportTcpTls     TransportKind = "tcp_tls"
	TransportRTU        TransportKind = "rtu"
	TransportASCII      TransportKind = "ascii"
	TransportRTUOverTCP TransportKind = "rtu_over_tcp"
)

// Transport is the connection target description, mirroring Rust's Transport
// enum (serde internally-tagged: {"type": "tcp", "host": ..., "port": ...}).
type Transport struct {
	Type   TransportKind  `json:"type"`
	Host   string         `json:"host,omitempty"`
	Port   uint16         `json:"port,omitempty"`
	Serial *serial.Config `json:"-"`
}

// TLSConfig mirrors Rust's TlsConfig (master side, transport.rs).
type TLSConfig struct {
	Enabled            bool   `json:"enabled"`
	CAFile             string `json:"ca_file"`
	CertFile           string `json:"cert_file"`
	KeyFile            string `json:"key_file"`
	PKCS12File         string `json:"pkcs12_file"`
	PKCS12Password     string `json:"pkcs12_password"`
	AcceptInvalidCerts bool   `json:"accept_invalid_certs"`
}

// Config is the master connection configuration (MasterConfig).
type Config struct {
	TargetAddress string          `json:"target_address"`
	Port          uint16          `json:"port"`
	SlaveID       uint8           `json:"slave_id"`
	TimeoutMs     uint64          `json:"timeout_ms"`
	Requests      RequestSettings `json:"requests"`
	TLS           TLSConfig       `json:"tls"`
	Socks5        socks5.Config   `json:"socks5"`
}

// DefaultConfig mirrors MasterConfig::default().
func DefaultConfig() Config {
	return Config{
		TargetAddress: "127.0.0.1",
		Port:          502,
		SlaveID:       1,
		TimeoutMs:     3000,
		Requests:      DefaultRequestSettings(),
	}
}

// RequestSettings are per-connection request limits (request.rs).
type RequestSettings struct {
	IntervalMs       uint16 `json:"interval_ms"`
	MaxReadRegisters uint16 `json:"max_read_registers"`
	MaxReadBits      uint16 `json:"max_read_bits"`
}

// DefaultRequestSettings mirrors RequestSettings::default().
func DefaultRequestSettings() RequestSettings {
	return RequestSettings{IntervalMs: 0, MaxReadRegisters: 125, MaxReadBits: 2000}
}

// Validate mirrors RequestSettings::validate.
func (s RequestSettings) Validate() error {
	if s.IntervalMs > 60000 {
		return fmt.Errorf("request interval must be between 0 and 60000 ms")
	}
	if s.MaxReadRegisters < 1 || s.MaxReadRegisters > 125 {
		return fmt.Errorf("registers per read request must be between 1 and 125")
	}
	if s.MaxReadBits < 1 || s.MaxReadBits > 2000 {
		return fmt.Errorf("bits per read request must be between 1 and 2000")
	}
	return nil
}

// RequestPacer enforces a quiet gap after each request completes
// (request.rs). The gap starts at completion, not at acquire.
type RequestPacer struct {
	interval       time.Duration
	token          chan struct{}
	lastCompletion time.Time // accessed only while holding token
}

func NewRequestPacer(interval time.Duration) *RequestPacer {
	p := &RequestPacer{interval: interval, token: make(chan struct{}, 1)}
	p.token <- struct{}{}
	return p
}

// Acquire holds exclusive ownership until release, including the quiet gap.
func (p *RequestPacer) Acquire(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.token:
	}
	if err := ctx.Err(); err != nil {
		p.token <- struct{}{}
		return nil, err
	}
	wait := time.Until(p.lastCompletion.Add(p.interval))
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			p.token <- struct{}{}
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		p.token <- struct{}{}
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { p.lastCompletion = time.Now(); p.token <- struct{}{} }) }, nil
}

// State is the connection state (MasterState).
type State string

const (
	StateDisconnected State = "disconnected"
	StateConnected    State = "connected"
	StateReconnecting State = "reconnecting"
	StateError        State = "error"
)

// ReadFunction selects which read function code to use.
type ReadFunction uint8

const (
	ReadCoils            ReadFunction = iota + 1 // FC01
	ReadDiscreteInputs                           // FC02
	ReadHoldingRegisters                         // FC03
	ReadInputRegisters                           // FC04
)

// FCByte returns the wire function code.
func (f ReadFunction) FCByte() uint8 {
	switch f {
	case ReadCoils:
		return 0x01
	case ReadDiscreteInputs:
		return 0x02
	case ReadHoldingRegisters:
		return 0x03
	case ReadInputRegisters:
		return 0x04
	}
	return 0
}

// ReadResult is the decoded payload of a read (ReadResult in Rust,
// serde adjacently tagged: {"type": "...", "data": [...]}).
type ReadResult struct {
	Kind      string   `json:"type"`
	Bits      []bool   `json:"bits,omitempty"`
	Registers []uint16 `json:"data,omitempty"`
}

// Error is the master error type, mirroring MasterError variants.
type Error struct {
	Kind      string // "timeout" | "exception" | "transport" | "not_connected" | "invalid_config" | "cancelled"
	Exception uint8  // Modbus exception code when Kind == "exception"
	Msg       string
}

func (e *Error) Error() string { return e.Msg }

func errTimeout(msg string) *Error   { return &Error{Kind: "timeout", Msg: msg} }
func errTransport(msg string) *Error { return &Error{Kind: "transport", Msg: msg} }
func errNotConnected() *Error        { return &Error{Kind: "not_connected", Msg: "not connected"} }
func errCancelled() *Error           { return &Error{Kind: "cancelled", Msg: "cancelled"} }
func errException(code uint8) *Error {
	return &Error{Kind: "exception", Exception: code, Msg: fmt.Sprintf("Modbus exception: 0x%02X", code)}
}

// transport is an open connection capable of exchangePDUs.
type transport interface {
	// exchange sends a request PDU for the given slave and returns the
	// response PDU.
	exchange(slaveID uint8, reqPDU []byte, timeout time.Duration) ([]byte, error)
	close() error
}

// Connection is a master connection to a Modbus slave via any transport.
type Connection struct {
	Config          Config
	Transport       Transport
	ReconnectPolicy ReconnectPolicy

	onConnectionLost func()

	mu         sync.Mutex
	state      State
	tr         transport
	pacer      *RequestPacer
	scanMu     sync.Mutex
	scanTasks  map[string]*scanTask
	scanOp     sync.Mutex
	generation uint64
	connecting bool
	log        LogSink
}

// LogSink receives communication log entries (wired to logcol.Collector by
// the app layer).
type LogSink interface {
	AddRequest(direction string, fc uint8, detail string)
}

// NewConnection creates a connection (not yet connected).
func NewConnection(cfg Config, tr Transport) *Connection {
	return &Connection{
		Config:          cfg,
		Transport:       tr,
		ReconnectPolicy: DefaultReconnectPolicy(),
		state:           StateDisconnected,
		pacer:           NewRequestPacer(time.Duration(cfg.Requests.IntervalMs) * time.Millisecond),
	}
}

// SetLogSink attaches a log sink.
func (c *Connection) SetLogSink(s LogSink) { c.log = s }

// State returns the current connection state.
func (c *Connection) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Connect opens the transport.
func (c *Connection) Connect(ctx context.Context) error {
	if err := c.Config.Requests.Validate(); err != nil {
		return &Error{Kind: "invalid_config", Msg: err.Error()}
	}
	c.mu.Lock()
	if c.state == StateConnected || c.connecting {
		c.mu.Unlock()
		return &Error{Kind: "transport", Msg: "already connected"}
	}
	c.connecting = true
	generation := c.generation
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.generation == generation {
			c.connecting = false
		}
		c.mu.Unlock()
	}()
	timeout := time.Duration(c.Config.TimeoutMs) * time.Millisecond

	var tr transport
	var err error
	switch c.Transport.Type {
	case TransportTCP:
		tr, err = newTCPTransportContext(ctx, c.Transport.Host, c.Transport.Port, c.Config.Socks5, timeout)
	case TransportRTU:
		tr, err = newSerialTransport(c.Transport.Serial, false)
	case TransportASCII:
		tr, err = newSerialTransport(c.Transport.Serial, true)
	case TransportRTUOverTCP:
		tr, err = newRTUTCPTransportContext(ctx, c.Transport.Host, c.Transport.Port, c.Config.Socks5, timeout)
	case TransportTcpTls:
		tr, err = newTLSTransport(ctx, c.Transport.Host, c.Transport.Port, c.Config.TLS, c.Config.Socks5, timeout)
	default:
		err = errTransport(fmt.Sprintf("unknown transport: %s", c.Transport.Type))
	}
	if err != nil {
		return err
	}

	c.mu.Lock()
	if c.generation != generation || ctx.Err() != nil {
		c.mu.Unlock()
		tr.close()
		return context.Canceled
	}
	c.tr = tr
	c.state = StateConnected
	c.mu.Unlock()
	return nil
}

// Disconnect closes the transport and stops all scans. Idempotent.
func (c *Connection) Disconnect() error {
	c.mu.Lock()
	c.generation++
	c.connecting = false
	tr := c.tr
	c.tr = nil
	c.state = StateDisconnected
	c.mu.Unlock()
	var err error
	if tr != nil {
		err = tr.close()
	}
	c.StopAllScans()
	return err
}

func (c *Connection) getTransport() (transport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tr == nil {
		return nil, errNotConnected()
	}
	return c.tr, nil
}

// exchange runs one paced request/response cycle with logging.
func (c *Connection) exchange(ctx context.Context, slaveID uint8, reqPDU []byte, timeout time.Duration, fc uint8, txDetail string) ([]byte, error) {
	tr, err := c.getTransport()
	if err != nil {
		return nil, err
	}
	release, err := c.pacer.Acquire(ctx)
	if err != nil {
		return nil, errCancelled()
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	current := c.tr == tr
	c.mu.Unlock()
	if !current {
		return nil, errNotConnected()
	}
	if c.log != nil {
		c.log.AddRequest("tx", fc, txDetail)
	}
	resp, err := tr.exchange(slaveID, reqPDU, timeout)
	if err != nil {
		if c.log != nil {
			c.log.AddRequest("rx", fc, fmt.Sprintf("ERR: %v", err))
		}
		return nil, err
	}
	if len(resp) > 0 && resp[0]&0x80 != 0 {
		exc := uint8(0)
		if len(resp) > 1 {
			exc = resp[1]
		}
		if c.log != nil {
			c.log.AddRequest("rx", fc, fmt.Sprintf("ERR: exception 0x%02X", exc))
		}
		return nil, errException(exc)
	}
	if c.log != nil {
		c.log.AddRequest("rx", fc, "OK")
	}
	return resp, nil
}

// Read executes a read of a continuous range as bounded batched requests,
// mirroring execute_read_batched. Returns one complete result in address
// order.
func (c *Connection) Read(ctx context.Context, function ReadFunction, startAddress, quantity uint16) (*ReadResult, error) {
	return c.ReadSlave(ctx, c.Config.SlaveID, function, startAddress, quantity, time.Duration(c.Config.TimeoutMs)*time.Millisecond)
}

// ReadSlave never changes the connection's default station or timeout.
func (c *Connection) ReadSlave(ctx context.Context, slaveID uint8, function ReadFunction, startAddress, quantity uint16, timeout time.Duration) (*ReadResult, error) {
	settings := c.Config.Requests
	if err := settings.Validate(); err != nil {
		return nil, &Error{Kind: "invalid_config", Msg: err.Error()}
	}
	if function.FCByte() == 0 || slaveID < 1 || slaveID > 247 || timeout <= 0 {
		return nil, &Error{Kind: "invalid_config", Msg: "invalid read function, slave ID or timeout"}
	}
	if quantity == 0 || uint32(startAddress)+uint32(quantity) > 65536 {
		return nil, &Error{Kind: "invalid_config", Msg: "read range must contain 1..65535 addresses and end at or before 65535"}
	}
	limit := settings.MaxReadRegisters
	bit := function == ReadCoils || function == ReadDiscreteInputs
	if bit {
		limit = settings.MaxReadBits
	}
	kinds := map[ReadFunction]string{ReadCoils: "coils", ReadDiscreteInputs: "discrete_inputs", ReadHoldingRegisters: "holding_registers", ReadInputRegisters: "input_registers"}
	out := &ReadResult{Kind: kinds[function]}
	for offset := uint32(0); offset < uint32(quantity); {
		count := uint16(min(uint32(limit), uint32(quantity)-offset))
		address := uint16(uint32(startAddress) + offset)
		req := []byte{function.FCByte(), byte(address >> 8), byte(address), byte(count >> 8), byte(count)}
		resp, err := c.exchange(ctx, slaveID, req, timeout, function.FCByte(), fmt.Sprintf("R %d x%d", address, count))
		if err != nil {
			return nil, err
		}
		part, err := parseReadResponse(function, resp)
		if err != nil {
			return nil, err
		}
		if bit {
			if len(part.Bits) < int(count) {
				return nil, errTransport("short bit response")
			}
			out.Bits = append(out.Bits, part.Bits[:count]...)
		} else {
			if len(part.Registers) != int(count) {
				return nil, errTransport("invalid register response length")
			}
			out.Registers = append(out.Registers, part.Registers...)
		}
		offset += uint32(count)
	}
	return out, nil
}

// parseReadResponse decodes a read response PDU (mirrors
// parse_read_response_pdu).
func parseReadResponse(function ReadFunction, resp []byte) (*ReadResult, error) {
	if len(resp) < 2 {
		return nil, errTransport("short read response")
	}
	if resp[0] != function.FCByte() {
		return nil, errTransport("unexpected read function code")
	}
	if int(resp[1]) != len(resp)-2 {
		return nil, errTransport("invalid read byte count")
	}
	if function >= ReadHoldingRegisters && resp[1]%2 != 0 {
		return nil, errTransport("odd register byte count")
	}
	byteCount := int(resp[1])
	data := []byte(nil)
	if len(resp) > 2 {
		data = resp[2:]
	}
	switch function {
	case ReadCoils, ReadDiscreteInputs:
		var bits []bool
		for byteIdx := 0; byteIdx < byteCount; byteIdx++ {
			for bitIdx := 0; bitIdx < 8; bitIdx++ {
				if byteIdx < len(data) {
					bits = append(bits, data[byteIdx]>>bitIdx&1 == 1)
				}
			}
		}
		kind := "coils"
		if function == ReadDiscreteInputs {
			kind = "discrete_inputs"
		}
		return &ReadResult{Kind: kind, Bits: bits}, nil

	default:
		var regs []uint16
		for i := 0; i+1 < len(data); i += 2 {
			regs = append(regs, binary.BigEndian.Uint16(data[i:i+2]))
		}
		kind := "holding_registers"
		if function == ReadInputRegisters {
			kind = "input_registers"
		}
		return &ReadResult{Kind: kind, Registers: regs}, nil
	}
}

// WriteSingleCoil writes one coil (FC05).
func (c *Connection) WriteSingleCoil(ctx context.Context, address uint16, value bool) error {
	return c.WriteSingleCoilSlave(ctx, c.Config.SlaveID, address, value)
}

func (c *Connection) WriteSingleCoilSlave(ctx context.Context, slaveID uint8, address uint16, value bool) error {
	coilValue := uint16(0x0000)
	if value {
		coilValue = 0xFF00
	}
	reqPDU := []byte{0x05}
	var ab [4]byte
	binary.BigEndian.PutUint16(ab[0:2], address)
	binary.BigEndian.PutUint16(ab[2:4], coilValue)
	reqPDU = append(reqPDU, ab[:]...)

	timeout := time.Duration(c.Config.TimeoutMs) * time.Millisecond
	resp, err := c.exchange(ctx, slaveID, reqPDU, timeout, 0x05, fmt.Sprintf("W %d = %v", address, value))
	if err != nil {
		return err
	}
	return checkWriteEcho(resp, reqPDU[:5])
}

// WriteSingleRegister writes one holding register (FC06).
func (c *Connection) WriteSingleRegister(ctx context.Context, address, value uint16) error {
	return c.WriteSingleRegisterSlave(ctx, c.Config.SlaveID, address, value)
}

func (c *Connection) WriteSingleRegisterSlave(ctx context.Context, slaveID uint8, address, value uint16) error {
	reqPDU := []byte{0x06}
	var ab [4]byte
	binary.BigEndian.PutUint16(ab[0:2], address)
	binary.BigEndian.PutUint16(ab[2:4], value)
	reqPDU = append(reqPDU, ab[:]...)

	timeout := time.Duration(c.Config.TimeoutMs) * time.Millisecond
	resp, err := c.exchange(ctx, slaveID, reqPDU, timeout, 0x06, fmt.Sprintf("W %d = 0x%04X", address, value))
	if err != nil {
		return err
	}
	return checkWriteEcho(resp, reqPDU[:5])
}

// WriteMultipleCoils writes a coil range (FC15).
func (c *Connection) WriteMultipleCoils(ctx context.Context, address uint16, values []bool) error {
	return c.WriteMultipleCoilsSlave(ctx, c.Config.SlaveID, address, values)
}

func (c *Connection) WriteMultipleCoilsSlave(ctx context.Context, slaveID uint8, address uint16, values []bool) error {
	if len(values) < 1 || len(values) > 1968 || uint32(address)+uint32(len(values)) > 65536 {
		return &Error{Kind: "invalid_config", Msg: "invalid coil write range"}
	}
	quantity := uint16(len(values))
	byteCount := (len(values) + 7) / 8
	coilBytes := make([]byte, byteCount)
	for i, v := range values {
		if v {
			coilBytes[i/8] |= 1 << (i % 8)
		}
	}
	reqPDU := []byte{0x0F}
	var ab [4]byte
	binary.BigEndian.PutUint16(ab[0:2], address)
	binary.BigEndian.PutUint16(ab[2:4], quantity)
	reqPDU = append(reqPDU, ab[:]...)
	reqPDU = append(reqPDU, uint8(byteCount))
	reqPDU = append(reqPDU, coilBytes...)

	timeout := time.Duration(c.Config.TimeoutMs) * time.Millisecond
	resp, err := c.exchange(ctx, slaveID, reqPDU, timeout, 0x0F, fmt.Sprintf("W %d x%d", address, len(values)))
	if err != nil {
		return err
	}
	return checkWriteEcho(resp, reqPDU[:5])
}

// WriteMultipleRegisters writes a register range (FC16).
func (c *Connection) WriteMultipleRegisters(ctx context.Context, address uint16, values []uint16) error {
	return c.WriteMultipleRegistersSlave(ctx, c.Config.SlaveID, address, values)
}

func (c *Connection) WriteMultipleRegistersSlave(ctx context.Context, slaveID uint8, address uint16, values []uint16) error {
	if len(values) < 1 || len(values) > 123 || uint32(address)+uint32(len(values)) > 65536 {
		return &Error{Kind: "invalid_config", Msg: "invalid register write range"}
	}
	reqPDU := []byte{0x10}
	var ab [4]byte
	binary.BigEndian.PutUint16(ab[0:2], address)
	binary.BigEndian.PutUint16(ab[2:4], uint16(len(values)))
	reqPDU = append(reqPDU, ab[:]...)
	reqPDU = append(reqPDU, uint8(len(values)*2))
	for _, v := range values {
		var vb [2]byte
		binary.BigEndian.PutUint16(vb[:], v)
		reqPDU = append(reqPDU, vb[:]...)
	}

	timeout := time.Duration(c.Config.TimeoutMs) * time.Millisecond
	resp, err := c.exchange(ctx, slaveID, reqPDU, timeout, 0x10, fmt.Sprintf("W %d x%d", address, len(values)))
	if err != nil {
		return err
	}
	return checkWriteEcho(resp, reqPDU[:5])
}

// checkWriteResponse validates a write echo (mirrors check_write_response).
func checkWriteResponse(resp []byte, expectedFC uint8) error {
	if len(resp) == 0 {
		return errTransport("empty response")
	}
	if resp[0]&0x80 != 0 {
		exc := uint8(0)
		if len(resp) > 1 {
			exc = resp[1]
		}
		return errException(exc)
	}
	if resp[0] != expectedFC {
		return errTransport(fmt.Sprintf("unexpected function code in response: expected 0x%02X, got 0x%02X", expectedFC, resp[0]))
	}
	return nil
}

func checkWriteEcho(resp, want []byte) error {
	if err := checkWriteResponse(resp, want[0]); err != nil {
		return err
	}
	if len(resp) != len(want) {
		return errTransport("invalid write echo length")
	}
	for i := range want {
		if resp[i] != want[i] {
			return errTransport("write echo mismatch")
		}
	}
	return nil
}
