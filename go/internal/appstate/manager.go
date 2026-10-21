package appstate

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/Karl-Dai/ModbusSim/go/internal/logcol"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// Manager owns every live slave connection. It replaces Rust's AppState plus
// the Tauri commands that reach into it: the connection is looked up by the
// "slave_N" id minted here, and every method is safe for concurrent use.
type Manager struct {
	mu          sync.Mutex
	connections map[string]*connection
	// order keeps the creation sequence so the UI list does not reshuffle the
	// way Rust's HashMap iteration does.
	order     []string
	nextID    uint32
	events    Events
	mutationR bool
}

// NewManager returns an empty manager. events may be the zero value, which
// disables push notifications.
func NewManager(events Events) *Manager {
	return &Manager{connections: map[string]*connection{}, nextID: 1, events: events}
}

// CreateConnectionRequest is create_slave_connection's input. InitMode "random"
// seeds the default device with non-zero values.
type CreateConnectionRequest struct {
	Transport Transport
	InitMode  string
	SlaveID   uint8
	Name      string
}

// CreateConnection registers a stopped connection with one default device, as
// Rust does: it does not start the listener.
func (m *Manager) CreateConnection(req CreateConnectionRequest) (ConnectionInfo, error) {
	if req.SlaveID < 1 || req.SlaveID > 247 {
		return ConnectionInfo{}, errors.New("slave_id must be between 1 and 247")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	id := "slave_" + strconv.FormatUint(uint64(m.nextID), 10)
	m.nextID++

	conn := m.newConnection(id, req.Transport)
	var device *slave.Device
	if req.InitMode == "random" {
		device = slave.WithRandomRegisters(req.SlaveID, req.Name, defaultMaxAddress)
	} else {
		device = slave.WithDefaultRegisters(req.SlaveID, req.Name, defaultMaxAddress)
	}
	if err := conn.server.AddDevice(device); err != nil {
		return ConnectionInfo{}, fmt.Errorf("failed to add default device: %w", err)
	}
	m.connections[id] = conn
	m.order = append(m.order, id)
	return conn.info(), nil
}

// newConnection wires one connection's engine, log collector and simulator. The
// simulator ticks for the connection's whole lifetime, not only while its
// transport is running: data sources keep advancing registers on a stopped
// connection, which is what the app-global Rust tick tasks do.
func (m *Manager) newConnection(id string, t Transport) *connection {
	collector := logcol.NewCollector()
	server := slave.NewServer()
	server.Log = collector
	server.Clients = slave.NewConnectedClients()

	conn := &connection{
		id:        id,
		transport: t,
		server:    server,
		collector: collector,
		state:     Stopped,
		points:    map[pointKey]*pointConfig{},
	}
	conn.simulator = slave.NewSimulator(server)
	server.ChangeCallback = func(changes []slave.Change) { m.events.registerValueChanged(id, changes) }
	collector.SetAppendCallback(func(logcol.Entry) { m.events.logAppended(id) })
	conn.simulator.SetMutationEnabled(m.mutationR)
	conn.simulator.Start()
	return conn
}

// StartConnection binds the transport and reports Running to the UI. Starting a
// running connection is a no-op, like Rust.
func (m *Manager) StartConnection(id string) error {
	m.mu.Lock()
	conn, err := m.connection(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	err = conn.start()
	state := conn.state
	m.mu.Unlock()

	if err != nil {
		return fmt.Errorf("failed to start: %w", err)
	}
	m.events.connectionStateChanged(id, state)
	return nil
}

// StopConnection closes the listener and every client socket.
func (m *Manager) StopConnection(id string) error {
	m.mu.Lock()
	conn, err := m.connection(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	conn.stop()
	state := conn.state
	m.mu.Unlock()

	m.events.connectionStateChanged(id, state)
	return nil
}

// DeleteConnection stops a connection and drops it along with its points.
func (m *Manager) DeleteConnection(id string) error {
	m.mu.Lock()
	conn, err := m.connection(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	conn.stop()
	conn.simulator.Stop()
	delete(m.connections, id)
	m.order = removeID(m.order, id)
	m.mu.Unlock()
	return nil
}

// ListConnections returns every connection in creation order.
func (m *Manager) ListConnections() []ConnectionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ConnectionInfo, 0, len(m.connections))
	for _, id := range m.order {
		if conn, ok := m.connections[id]; ok {
			out = append(out, conn.info())
		}
	}
	return out
}

// GetTransport returns a connection's current transport settings.
func (m *Manager) GetTransport(id string) (Transport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(id)
	if err != nil {
		return Transport{}, errors.New("Connection not found")
	}
	return conn.transport, nil
}

// UpdateTransport replaces a stopped connection's settings. The transport family
// cannot change: Rust refuses because the UI, listeners and device list were
// built for the original one.
func (m *Manager) UpdateTransport(id string, t Transport) error {
	if err := ValidateTransport(t); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(id)
	if err != nil {
		return err
	}
	if conn.state != Stopped {
		return errors.New("Stop the connection before changing its settings")
	}
	if conn.transport.Type != t.Type {
		return errors.New("Changing transport type requires a new connection")
	}
	conn.transport = t
	return nil
}

// SetMutationRunning toggles the app-wide mutation switch, fanning the state out
// to every connection's simulator.
func (m *Manager) SetMutationRunning(running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mutationR = running
	for _, conn := range m.connections {
		conn.simulator.SetMutationEnabled(running)
	}
}

// MutationRunning reports the switch.
func (m *Manager) MutationRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mutationR
}

// Logs returns a connection's whole log.
func (m *Manager) Logs(id string) ([]logcol.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(id)
	if err != nil {
		return nil, err
	}
	return conn.collector.GetAll(), nil
}

// ClearLogs empties a connection's log.
func (m *Manager) ClearLogs(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(id)
	if err != nil {
		return err
	}
	conn.collector.Clear()
	return nil
}

// ExportLogsCSV renders a connection's log as CSV text.
func (m *Manager) ExportLogsCSV(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(id)
	if err != nil {
		return "", err
	}
	return conn.collector.ExportCSV(), nil
}

// connection finds a connection by id. The caller holds m.mu.
func (m *Manager) connection(id string) (*connection, error) {
	conn, ok := m.connections[id]
	if !ok {
		return nil, fmt.Errorf("connection %s not found", id)
	}
	return conn, nil
}

// DeviceInfo describes one simulated device.
type DeviceInfo struct {
	SlaveID       uint8
	Name          string
	RegisterCount int
}

// AddDeviceRequest is add_slave_device's input. InitMode selects the seeded
// register set: "random", "zero", or anything else for an empty device.
type AddDeviceRequest struct {
	ConnectionID string
	SlaveID      uint8
	Name         string
	InitMode     string
}

// AddDevice registers a device on a connection.
func (m *Manager) AddDevice(req AddDeviceRequest) (DeviceInfo, error) {
	if req.SlaveID < 1 || req.SlaveID > 247 {
		return DeviceInfo{}, errors.New("slave_id must be between 1 and 247")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(req.ConnectionID)
	if err != nil {
		return DeviceInfo{}, err
	}

	var device *slave.Device
	switch req.InitMode {
	case "random":
		device = slave.WithRandomRegisters(req.SlaveID, req.Name, defaultMaxAddress)
	case "zero":
		device = slave.WithDefaultRegisters(req.SlaveID, req.Name, defaultMaxAddress)
	default:
		device = slave.NewDevice(req.SlaveID, req.Name)
	}
	count := len(device.RegisterDefs)
	if err := conn.server.AddDevice(device); err != nil {
		return DeviceInfo{}, fmt.Errorf("failed to add device: %w", err)
	}
	return DeviceInfo{SlaveID: req.SlaveID, Name: req.Name, RegisterCount: count}, nil
}

// UpdateDeviceRequest renames a device, optionally moving it to a new slave ID.
type UpdateDeviceRequest struct {
	ConnectionID    string
	OriginalSlaveID uint8
	SlaveID         uint8
	Name            string
}

// UpdateDevice renames a device and carries its points and scheduling state to
// the new slave ID.
func (m *Manager) UpdateDevice(req UpdateDeviceRequest) (DeviceInfo, error) {
	if req.SlaveID < 1 || req.SlaveID > 247 {
		return DeviceInfo{}, errors.New("slave_id must be between 1 and 247")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(req.ConnectionID)
	if err != nil {
		return DeviceInfo{}, err
	}
	count, err := conn.server.RenameDevice(req.OriginalSlaveID, req.SlaveID, req.Name)
	if err != nil {
		return DeviceInfo{}, err
	}
	if req.SlaveID != req.OriginalSlaveID {
		conn.movePoints(req.OriginalSlaveID, req.SlaveID)
	}
	return DeviceInfo{SlaveID: req.SlaveID, Name: req.Name, RegisterCount: count}, nil
}

// RemoveDevice drops a device and everything configured on its points.
func (m *Manager) RemoveDevice(connectionID string, slaveID uint8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(connectionID)
	if err != nil {
		return err
	}
	if err := conn.server.RemoveDevice(slaveID); err != nil {
		return fmt.Errorf("failed to remove device: %w", err)
	}
	conn.dropDevice(slaveID)
	return nil
}

// ListDevices returns a connection's devices ordered by slave ID.
func (m *Manager) ListDevices(connectionID string) ([]DeviceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(connectionID)
	if err != nil {
		return nil, err
	}
	ids := conn.server.ListDevices()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]DeviceInfo, 0, len(ids))
	for _, id := range ids {
		var device DeviceInfo
		if err := conn.server.ViewDevice(id, func(d *slave.Device) {
			device = DeviceInfo{SlaveID: id, Name: d.Name, RegisterCount: len(d.RegisterDefs)}
		}); err == nil {
			out = append(out, device)
		}
	}
	return out, nil
}

func removeID(ids []string, id string) []string {
	out := ids[:0]
	for _, existing := range ids {
		if existing != id {
			out = append(out, existing)
		}
	}
	return out
}
