// Package app implements the application layer: runtime management of slave
// connections (lifecycle, devices, registers, logs, data sources, point
// mutation) mirroring crates/modbussim-app (state.rs + commands.rs) on top of
// the internal packages.
package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/clients"
	"github.com/Karl-Dai/ModbusSim/go/internal/logcol"
	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/serial"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// ConnectionState mirrors Rust's ConnectionState enum as strings.
type ConnectionState string

const (
	StateStopped  ConnectionState = "Stopped"
	StateRunning  ConnectionState = "Running"
	StateStarting ConnectionState = "Starting"
	StateError    ConnectionState = "Error"
)

// SlaveTLSConfig mirrors Rust's SlaveTlsConfig (project-file field names).
type SlaveTLSConfig struct {
	Enabled           bool   `json:"enabled"`
	CertFile          string `json:"cert_file"`
	KeyFile           string `json:"key_file"`
	CAFile            string `json:"ca_file"`
	RequireClientCert bool   `json:"require_client_cert"`
	PKCS12File        string `json:"pkcs12_file"`
	PKCS12Password    string `json:"pkcs12_password"`
}

// TransportConfig is the connection's transport description. It mirrors the
// project package's TransportConfig (same JSON shape) so app-layer DTOs can
// be serialized directly.
type TransportConfig struct {
	Type      string            `json:"type"`
	Host      string            `json:"host,omitempty"`
	Port      uint16            `json:"port,omitempty"`
	PortName  string            `json:"-"`
	BaudRate  uint32            `json:"-"`
	DataBits  uint8             `json:"-"`
	StopBits  uint8             `json:"-"`
	Parity    string            `json:"-"`
	ClientTLS *master.TLSConfig `json:"client_tls,omitempty"`
	ServerTLS *SlaveTLSConfig   `json:"server_tls,omitempty"`
}

// SlaveConnectionState is the runtime state for one slave connection.
type SlaveConnectionState struct {
	State ConnectionState `json:"state"`
}

// Connection manages one slave listener stack: transport, TLS, device
// registry, log collector, and client tracking.
type Connection struct {
	mu        sync.Mutex
	transport TransportConfig
	tlsCfg    SlaveTLSConfig
	server    *slave.Server
	log       *logcol.Collector
	clients   *clients.ConnectedClients
	cancel    context.CancelFunc
	done      chan struct{}
	state     ConnectionState
}

// NewConnection creates a stopped connection.
func NewConnection(transport TransportConfig, tlsCfg SlaveTLSConfig) *Connection {
	log := logcol.NewCollector()
	srv := slave.NewServer()
	srv.Log = log
	return &Connection{
		transport: transport,
		tlsCfg:    tlsCfg,
		server:    srv,
		log:       log,
		clients:   clients.New(),
		state:     StateStopped,
	}
}

// Server exposes the device registry.
func (c *Connection) Server() *slave.Server { return c.server }

// Log exposes the log collector.
func (c *Connection) Log() *logcol.Collector { return c.log }

// Clients exposes the connected-client registry (nil for serial transports).
func (c *Connection) Clients() *clients.ConnectedClients { return c.clients }

// Transport returns the transport config.
func (c *Connection) Transport() TransportConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.transport
}

// State returns the connection lifecycle state.
func (c *Connection) State() ConnectionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// setState updates the lifecycle state (caller holds no lock).
func (c *Connection) setState(s ConnectionState) {
	c.mu.Lock()
	c.state = s
	c.mu.Unlock()
}

// Start launches the listener for the configured transport. TCP variants
// listen on host:port; serial variants open the port directly.
func (c *Connection) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.state == StateRunning {
		c.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.done = make(chan struct{})
	c.state = StateStarting
	transport := c.transport
	tlsCfg := c.tlsCfg
	srv := c.server
	done := c.done
	c.mu.Unlock()

	ln, err := c.listen(ctx, transport, tlsCfg)
	if err != nil {
		cancel()
		close(done)
		c.setState(StateError)
		return fmt.Errorf("failed to start: %w", err)
	}

	go func() {
		defer close(done)
		switch transport.Type {
		case "tcp":
			_ = slave.RunTCP(runCtx, ln, srv)
		case "tcp_tls":
			tlsConfig, cfgErr := slave.BuildTLSConfig(slave.TLSConfig(tlsCfg))
			if cfgErr != nil {
				c.setState(StateError)
				return
			}
			_ = slave.RunTLS(runCtx, ln, tlsConfig, srv)
		case "rtu_over_tcp":
			_ = slave.RunRTUOverTCP(runCtx, ln, srv)
		case "rtu":
			_ = slave.RunRTUSerial(runCtx, c.serialConfig(), srv)
		case "ascii":
			_ = slave.RunASCIISerial(runCtx, c.serialConfig(), srv)
		}
	}()
	c.setState(StateRunning)
	return nil
}

// listen binds the listener for TCP-family transports. Client tracking is
// wrapped in by the listeners via accept loops; we register tracked conns
// through the returned listener only for TCP-family types.
func (c *Connection) listen(ctx context.Context, transport TransportConfig, tlsCfg SlaveTLSConfig) (net.Listener, error) {
	switch transport.Type {
	case "tcp", "rtu_over_tcp":
		return net.Listen("tcp", fmt.Sprintf("%s:%d", transport.Host, transport.Port))
	case "tcp_tls":
		if _, err := slave.BuildTLSConfig(slave.TLSConfig(tlsCfg)); err != nil {
			return nil, err
		}
		return net.Listen("tcp", fmt.Sprintf("%s:%d", transport.Host, transport.Port))
	case "rtu", "ascii":
		return nil, nil // serial transports open their port in RunRTUSerial/RunASCIISerial
	}
	return nil, fmt.Errorf("unsupported transport type: %s", transport.Type)
}

func (c *Connection) serialConfig() serial.Config {
	return serial.Config{
		Port:     c.transport.PortName,
		BaudRate: c.transport.BaudRate,
		DataBits: c.transport.DataBits,
		StopBits: c.transport.StopBits,
		Parity:   serial.Parity(c.transport.Parity),
	}
}

// Stop shuts down the listener and force-closes tracked clients.
func (c *Connection) Stop() error {
	c.mu.Lock()
	if c.state != StateRunning {
		c.state = StateStopped
		c.mu.Unlock()
		return nil
	}
	cancel := c.cancel
	done := c.done
	c.cancel = nil
	c.done = nil
	c.state = StateStopped
	c.mu.Unlock()

	c.clients.CloseAll()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
	return nil
}

// BindAddress returns the display bind address and port for the connection.
func (c *Connection) BindAddress() (string, uint16) {
	t := c.transport
	if t.Type == "rtu" || t.Type == "ascii" {
		return t.PortName, 0
	}
	return t.Host, t.Port
}

// DeviceCount returns the number of registered devices.
func (c *Connection) DeviceCount() int {
	return len(c.server.ListDevices())
}

// ---------------------------------------------------------------------------
// Device and register management (commands.rs device/register commands)
// ---------------------------------------------------------------------------

// AddDevice adds a device, optionally pre-filling default points
// (initMode "random" is accepted and treated as default registers).
func (c *Connection) AddDevice(slaveID uint8, name, initMode string) error {
	if slaveID == 0 {
		return fmt.Errorf("slave_id must be between 1 and 247")
	}
	maxAddr := uint16(20000)
	var dev *slave.Device
	if initMode == "random" {
		dev = slave.WithDefaultRegisters(slaveID, name, maxAddr)
	} else {
		dev = slave.WithDefaultRegisters(slaveID, name, maxAddr)
	}
	return c.server.AddDevice(dev)
}

// RemoveDevice unregisters a device.
func (c *Connection) RemoveDevice(slaveID uint8) error {
	return c.server.RemoveDevice(slaveID)
}

// ListDevices returns registered device IDs.
func (c *Connection) ListDevices() []uint8 {
	return c.server.ListDevices()
}

// ReadRegisterValue reads one register value by type and address.
func (c *Connection) ReadRegisterValue(slaveID uint8, rt register.RegisterType, addr uint16) (uint16, bool) {
	dev, ok := c.server.GetDevice(slaveID)
	if !ok {
		return 0, false
	}
	switch rt {
	case register.Coil:
		v := dev.RegisterMap.ReadCoils(addr, 1)
		if len(v) == 0 {
			return 0, false
		}
		if v[0] {
			return 1, true
		}
		return 0, true
	case register.DiscreteInput:
		v := dev.RegisterMap.ReadDiscreteInputs(addr, 1)
		if len(v) == 0 {
			return 0, false
		}
		if v[0] {
			return 1, false
		}
		return 0, true
	case register.HoldingRegType:
		v := dev.RegisterMap.ReadHoldingRegisters(addr, 1)
		if len(v) == 0 {
			return 0, false
		}
		return v[0], true
	case register.InputRegister:
		v := dev.RegisterMap.ReadInputRegisters(addr, 1)
		if len(v) == 0 {
			return 0, false
		}
		return v[0], true
	}
	return 0, false
}

// WriteRegisterValue writes one register value by type and address.
func (c *Connection) WriteRegisterValue(slaveID uint8, rt register.RegisterType, addr, value uint16) error {
	dev, ok := c.server.GetDevice(slaveID)
	if !ok {
		return fmt.Errorf("device %d not found", slaveID)
	}
	switch rt {
	case register.Coil:
		dev.RegisterMap.WriteCoil(addr, value != 0)
		return nil
	case register.HoldingRegType:
		dev.RegisterMap.WriteHoldingRegister(addr, value)
		return nil
	}
	return fmt.Errorf("register type %s is read-only", rt)
}

// connAddr renders a TCP address for a transport (helper for tests).
func connAddr(host string, port uint16) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

var _ = tls.VersionTLS12
var _ = strings.TrimSpace
