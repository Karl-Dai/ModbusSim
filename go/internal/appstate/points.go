package appstate

import (
	"errors"
	"fmt"
	"sort"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// pointKey names one defined point inside a connection.
type pointKey struct {
	SlaveID      uint8
	RegisterType register.RegisterType
	Address      uint16
}

// pointConfig is a point's simulation drivers. Go's register.RegisterDef has no
// behaviour fields -- they live in the project model's Point instead -- so the
// running connection keeps them beside the definition and feeds the simulator.
type pointConfig struct {
	mutation *mutation.Config
	source   *datasource.Config
}

func (p *pointConfig) behaviour(def register.RegisterDef, slaveID uint8) slave.Behaviour {
	if p == nil {
		return slave.Behaviour{Def: def}
	}
	return slave.Behaviour{Def: def, Mutation: p.mutation, DataSource: p.source}
}

func (p *pointConfig) drivesNothing() bool {
	return p == nil || (p.mutation == nil && p.source == nil)
}

// findDef looks up a point's definition. The caller holds m.mu.
func (c *connection) findDef(slaveID uint8, registerType register.RegisterType, address uint16) (register.RegisterDef, bool, error) {
	var def register.RegisterDef
	var found bool
	err := c.server.ViewDevice(slaveID, func(device *slave.Device) {
		for _, candidate := range device.RegisterDefs {
			if candidate.RegisterType == registerType && candidate.Address == address {
				def, found = candidate, true
				return
			}
		}
	})
	return def, found, err
}

// applyPoint hands one point's drivers to the scheduler, resetting only that
// point's timing state. The caller holds m.mu.
func (c *connection) applyPoint(slaveID uint8, def register.RegisterDef) {
	key := pointKey{SlaveID: slaveID, RegisterType: def.RegisterType, Address: def.Address}
	c.simulator.SetPoint(slaveID, c.points[key].behaviour(def, slaveID))
}

// resyncDevice rebuilds a device's whole point list, used after the definition
// set itself changed. The caller holds m.mu.
func (c *connection) resyncDevice(device *slave.Device) {
	behaviours := make([]slave.Behaviour, 0, len(c.points))
	for _, def := range device.RegisterDefs {
		key := pointKey{SlaveID: device.SlaveID, RegisterType: def.RegisterType, Address: def.Address}
		cfg := c.points[key]
		if cfg.drivesNothing() {
			continue
		}
		behaviours = append(behaviours, cfg.behaviour(def, device.SlaveID))
	}
	c.simulator.SetPoints(device.SlaveID, behaviours)
}

// movePoints re-keys a device's point configs after its slave ID changed. The
// caller holds m.mu.
func (c *connection) movePoints(from, to uint8) {
	for key, cfg := range c.points {
		if key.SlaveID != from {
			continue
		}
		moved := key
		moved.SlaveID = to
		delete(c.points, key)
		c.points[moved] = cfg
	}
	device, ok := c.server.GetDevice(to)
	if !ok {
		return
	}
	c.simulator.SetPoints(from, nil)
	c.resyncDevice(device)
}

// resetPointConfigs drops every simulation config attached to a device's points.
// The caller holds m.mu.
func (c *connection) resetPointConfigs(slaveID uint8) {
	for key := range c.points {
		if key.SlaveID == slaveID {
			delete(c.points, key)
		}
	}
}

// dropDevice forgets everything configured on a removed device.
func (c *connection) dropDevice(slaveID uint8) {
	c.resetPointConfigs(slaveID)
	c.simulator.SetPoints(slaveID, nil)
}

// pointConfigs returns a device's configured points ordered by area and address.
func (c *connection) pointConfigs(slaveID uint8) []pointKey {
	keys := make([]pointKey, 0, len(c.points))
	for key, cfg := range c.points {
		if key.SlaveID == slaveID && !cfg.drivesNothing() {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].RegisterType != keys[j].RegisterType {
			return keys[i].RegisterType < keys[j].RegisterType
		}
		return keys[i].Address < keys[j].Address
	})
	return keys
}

// parseRegisterType accepts the four area names Rust's parse.rs knows.
func parseRegisterType(value string) (register.RegisterType, error) {
	switch t := register.RegisterType(value); t {
	case register.Coil, register.DiscreteInput, register.InputRegister, register.HoldingRegType:
		return t, nil
	}
	return "", fmt.Errorf("unknown register type: %s", value)
}

// SetPointMutationRequest is set_point_mutation's input.
type SetPointMutationRequest struct {
	ConnectionID string
	SlaveID      uint8
	RegisterType string
	Address      uint16
	Config       mutation.Config
}

// SetPointMutation enables or clears one point's mutation. Disabling removes the
// config entirely, so a later enable starts from the values the UI sends.
func (m *Manager) SetPointMutation(req SetPointMutationRequest) error {
	registerType, err := parseRegisterType(req.RegisterType)
	if err != nil {
		return err
	}
	config := req.Config
	if err := mutation.Normalize(registerType, &config); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(req.ConnectionID)
	if err != nil {
		return err
	}
	def, found, err := conn.findDef(req.SlaveID, registerType, req.Address)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("register %s@%d not found", req.RegisterType, req.Address)
	}

	key := pointKey{SlaveID: req.SlaveID, RegisterType: registerType, Address: req.Address}
	stored := conn.points[key]
	if !config.Enabled {
		if stored == nil {
			return nil
		}
		stored.mutation = nil
		if stored.drivesNothing() {
			delete(conn.points, key)
		}
		conn.applyPoint(req.SlaveID, def)
		return nil
	}
	if stored != nil && stored.source != nil {
		return errors.New("remove the point data source before enabling mutation")
	}
	if stored == nil {
		stored = &pointConfig{}
		conn.points[key] = stored
	}
	stored.mutation = &config
	conn.applyPoint(req.SlaveID, def)
	return nil
}

// ClearPointMutation disables one point's mutation, leaving its data source.
func (m *Manager) ClearPointMutation(connectionID string, slaveID uint8, registerType string, address uint16) error {
	area, err := parseRegisterType(registerType)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(connectionID)
	if err != nil {
		return err
	}
	def, found, err := conn.findDef(slaveID, area, address)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("register %s@%d not found", registerType, address)
	}
	key := pointKey{SlaveID: slaveID, RegisterType: area, Address: address}
	stored := conn.points[key]
	if stored == nil {
		return nil
	}
	stored.mutation = nil
	if stored.drivesNothing() {
		delete(conn.points, key)
	}
	conn.applyPoint(slaveID, def)
	return nil
}

// PointMutationInfo echoes one enabled point's full mutation parameters.
type PointMutationInfo struct {
	RegisterType register.RegisterType
	Address      uint16
	Mode         mutation.Mode
	PeriodMS     uint64
	Step         float64
	Min          float64
	Max          float64
}

// ListPointMutations returns the enabled points of one device, in definition
// order, for the simulation drawer's echo-back.
func (m *Manager) ListPointMutations(connectionID string, slaveID uint8) ([]PointMutationInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(connectionID)
	if err != nil {
		return nil, err
	}
	var out []PointMutationInfo
	err = conn.server.ViewDevice(slaveID, func(device *slave.Device) {
		for _, def := range device.RegisterDefs {
			cfg := conn.points[pointKey{SlaveID: slaveID, RegisterType: def.RegisterType, Address: def.Address}]
			if cfg == nil || cfg.mutation == nil || !cfg.mutation.Enabled {
				continue
			}
			out = append(out, PointMutationInfo{
				RegisterType: def.RegisterType,
				Address:      def.Address,
				Mode:         cfg.mutation.Mode,
				PeriodMS:     cfg.mutation.PeriodMs,
				Step:         cfg.mutation.Step,
				Min:          cfg.mutation.Min,
				Max:          cfg.mutation.Max,
			})
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SetDataSourceRequest is set_data_source's input.
type SetDataSourceRequest struct {
	ConnectionID     string
	SlaveID          uint8
	RegisterType     string
	Address          uint16
	Source           datasource.Source
	UpdateIntervalMS uint64
}

// SetDataSource gives one point a generated value stream.
func (m *Manager) SetDataSource(req SetDataSourceRequest) error {
	config := datasource.Config{Source: req.Source, UpdateIntervalMs: req.UpdateIntervalMS}
	if err := config.Validate(); err != nil {
		return err
	}
	registerType, err := parseRegisterType(req.RegisterType)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(req.ConnectionID)
	if err != nil {
		return err
	}
	def, found, err := conn.findDef(req.SlaveID, registerType, req.Address)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("register %s@%d not found", req.RegisterType, req.Address)
	}
	key := pointKey{SlaveID: req.SlaveID, RegisterType: registerType, Address: req.Address}
	stored := conn.points[key]
	if stored != nil && stored.mutation != nil && stored.mutation.Enabled {
		return errors.New("disable point mutation before setting a data source")
	}
	if stored == nil {
		stored = &pointConfig{}
		conn.points[key] = stored
	}
	source := config
	stored.source = &source
	conn.applyPoint(req.SlaveID, def)
	return nil
}

// RemoveDataSource stops generating values for one point, leaving its mutation.
func (m *Manager) RemoveDataSource(connectionID string, slaveID uint8, registerType string, address uint16) error {
	area, err := parseRegisterType(registerType)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(connectionID)
	if err != nil {
		return err
	}
	def, found, err := conn.findDef(slaveID, area, address)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("register not found")
	}
	key := pointKey{SlaveID: slaveID, RegisterType: area, Address: address}
	stored := conn.points[key]
	if stored == nil {
		return nil
	}
	stored.source = nil
	if stored.drivesNothing() {
		delete(conn.points, key)
	}
	conn.applyPoint(slaveID, def)
	return nil
}

// DataSourceInfo is one configured data source.
type DataSourceInfo struct {
	ConnectionID string
	SlaveID      uint8
	RegisterType register.RegisterType
	Address      uint16
	Config       datasource.Config
}

// ListDataSources returns a connection's data sources ordered by device, area and
// address.
func (m *Manager) ListDataSources(connectionID string) ([]DataSourceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(connectionID)
	if err != nil {
		return nil, err
	}
	var out []DataSourceInfo
	for _, deviceID := range m.deviceIDsOf(conn) {
		for _, key := range conn.pointConfigs(deviceID) {
			cfg := conn.points[key]
			if cfg.source == nil {
				continue
			}
			out = append(out, DataSourceInfo{
				ConnectionID: conn.id,
				SlaveID:      key.SlaveID,
				RegisterType: key.RegisterType,
				Address:      key.Address,
				Config:       *cfg.source,
			})
		}
	}
	return out, nil
}

// deviceIDsOf lists a connection's devices by slave ID. The caller holds m.mu.
func (m *Manager) deviceIDsOf(conn *connection) []uint8 {
	ids := conn.server.ListDevices()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
