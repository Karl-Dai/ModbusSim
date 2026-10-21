package appstate

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/project"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// Project snapshots the whole registry as an unsaved v2 slave project: every
// connection with its devices, point definitions, simulation config and current
// register contents.
func (m *Manager) Project() *project.Project {
	m.mu.Lock()
	defer m.mu.Unlock()
	proj := project.New(project.TypeSlave)
	for _, id := range m.order {
		conn, ok := m.connections[id]
		if !ok {
			continue
		}
		proj.Connections = append(proj.Connections, conn.projectConnection())
	}
	return proj
}

// SaveProject writes the registry to path.
func (m *Manager) SaveProject(path string) error { return project.SaveFile(path, m.Project()) }

// projectConnection snapshots one connection. The transport-derived name is what
// Rust's save_project_file stores, and the master-side fields keep their
// defaults because a slave project never reads them.
func (c *connection) projectConnection() *project.Connection {
	return &project.Connection{
		ID:             c.id,
		Name:           connectionName(c.transport),
		Transport:      c.transport,
		Devices:        c.projectDevices(),
		DefaultSlaveID: 1,
		TimeoutMS:      3000,
		Requests:       project.DefaultRequestSettings(),
		Reconnect:      project.DefaultReconnectPolicy(),
	}
}

func (c *connection) projectDevices() []*project.Device {
	out := make([]*project.Device, 0, len(c.points))
	for _, id := range c.server.ListDevices() {
		device, ok := c.server.GetDevice(id)
		if !ok {
			continue
		}
		projectDevice := &project.Device{SlaveID: device.SlaveID, Name: device.Name}
		// One lock pass keeps the definitions, the register contents and the
		// simulation configs consistent with each other.
		err := c.server.ViewDevice(id, func(d *slave.Device) {
			for _, def := range d.RegisterDefs {
				cfg := c.points[pointKey{SlaveID: id, RegisterType: def.RegisterType, Address: def.Address}]
				var mutationCfg *mutation.Config
				var sourceCfg *datasource.Config
				if cfg != nil {
					mutationCfg, sourceCfg = cfg.mutation, cfg.source
				}
				projectDevice.Points = append(projectDevice.Points, project.NewPoint(def, mutationCfg, sourceCfg))
			}
			projectDevice.Values = snapshotValues(d.RegisterMap)
		})
		if err == nil {
			out = append(out, projectDevice)
		}
	}
	return out
}

// connectionName labels a connection the way Rust's project save does.
func connectionName(t Transport) string {
	switch t.Type {
	case project.TransportTCP:
		return t.Host + ":" + strconv.Itoa(int(t.Port))
	case project.TransportRTUOverTCP:
		return "rtu-tcp://" + t.Host + ":" + strconv.Itoa(int(t.Port))
	case project.TransportTCPTLS:
		return "tls://" + t.Host + ":" + strconv.Itoa(int(t.Port))
	case project.TransportRTU:
		return "rtu://" + serialPortName(t)
	case project.TransportASCII:
		return "ascii://" + serialPortName(t)
	}
	return string(t.Type)
}

func serialPortName(t Transport) string {
	if t.Serial == nil {
		return ""
	}
	return t.Serial.Port
}

// snapshotValues copies a register map for persistence. The caller holds the
// device lock, because the listeners write these maps concurrently.
func snapshotValues(m *register.RegisterMap) project.Values {
	values := project.Values{
		Coils:            make(map[uint16]bool, len(m.Coils)),
		DiscreteInputs:   make(map[uint16]bool, len(m.DiscreteInputs)),
		HoldingRegisters: make(map[uint16]uint16, len(m.HoldingRegisters)),
		InputRegisters:   make(map[uint16]uint16, len(m.InputRegisters)),
	}
	for address, bit := range m.Coils {
		values.Coils[address] = bit
	}
	for address, bit := range m.DiscreteInputs {
		values.DiscreteInputs[address] = bit
	}
	for address, word := range m.HoldingRegisters {
		values.HoldingRegisters[address] = word
	}
	for address, word := range m.InputRegisters {
		values.InputRegisters[address] = word
	}
	return values
}

// LoadProject replaces the registry with the slave project at path and returns
// the number of devices loaded.
func (m *Manager) LoadProject(path string) (int, error) {
	proj, err := project.LoadFile(path)
	if err != nil {
		return 0, err
	}
	return m.ApplyProject(proj)
}

// ApplyProject replaces the running state with a project. Like Rust it leaves
// the mutation master switch off afterwards: a loaded project must not start
// rewriting its own registers until the operator asks for it.
func (m *Manager) ApplyProject(proj *project.Project) (int, error) {
	if proj.Type != project.TypeSlave {
		return 0, errors.New("project is not a slave project")
	}
	if err := proj.Validate(); err != nil {
		return 0, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeLocked()
	m.connections = map[string]*connection{}
	m.order = nil

	devices := 0
	for _, config := range proj.Connections {
		conn := m.newConnection(config.ID, config.Transport)
		for _, device := range config.Devices {
			if err := conn.loadDevice(device); err != nil {
				return 0, err
			}
			devices++
		}
		m.connections[config.ID] = conn
		m.order = append(m.order, config.ID)
	}
	m.nextID = nextIDAfter(m.connections)
	m.mutationR = false
	return devices, nil
}

// loadDevice rebuilds one persisted device: its points, their simulation config,
// and the stored register contents.
func (c *connection) loadDevice(device *project.Device) error {
	slaveDevice := slave.NewDevice(device.SlaveID, device.Name)
	for _, point := range device.Points {
		def := point.ToDef()
		slaveDevice.RegisterMap.EnsureFromDef(def)
		slaveDevice.RegisterDefs = append(slaveDevice.RegisterDefs, def)
		mutationCfg, sourceCfg := point.ToMutation(), point.ToDataSource()
		if mutationCfg == nil && sourceCfg == nil {
			continue
		}
		key := pointKey{SlaveID: device.SlaveID, RegisterType: def.RegisterType, Address: def.Address}
		c.points[key] = &pointConfig{mutation: mutationCfg, source: sourceCfg}
	}
	applyValues(slaveDevice.RegisterMap, device.Values)
	if err := c.server.AddDevice(slaveDevice); err != nil {
		return fmt.Errorf("failed to load device: %w", err)
	}
	c.resyncDevice(slaveDevice)
	return nil
}

// applyValues restores stored contents into the slots the definitions created,
// mirroring Rust's apply_to_existing: a value with no slot is dropped rather
// than widening the map.
func applyValues(m *register.RegisterMap, values project.Values) {
	for address, bit := range values.Coils {
		if _, ok := m.Coils[address]; ok {
			m.Coils[address] = bit
		}
	}
	for address, bit := range values.DiscreteInputs {
		if _, ok := m.DiscreteInputs[address]; ok {
			m.DiscreteInputs[address] = bit
		}
	}
	for address, word := range values.HoldingRegisters {
		if _, ok := m.HoldingRegisters[address]; ok {
			m.HoldingRegisters[address] = word
		}
	}
	for address, word := range values.InputRegisters {
		if _, ok := m.InputRegisters[address]; ok {
			m.InputRegisters[address] = word
		}
	}
}

// nextIDAfter continues the "slave_N" sequence after a load, so a later create
// cannot collide with an id from the project.
func nextIDAfter(connections map[string]*connection) uint32 {
	var max uint64
	for id := range connections {
		if !strings.HasPrefix(id, "slave_") {
			continue
		}
		if n, err := strconv.ParseUint(id[len("slave_"):], 10, 32); err == nil && n > max {
			max = n
		}
	}
	return uint32(max + 1)
}

// closeLocked stops every connection and its scheduler. The caller holds m.mu.
func (m *Manager) closeLocked() {
	for _, conn := range m.connections {
		conn.stop()
		conn.simulator.Stop()
	}
}

// CloseAll stops and forgets every connection.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeLocked()
	m.connections = map[string]*connection{}
	m.order = nil
	m.nextID = 1
	m.mutationR = false
}
