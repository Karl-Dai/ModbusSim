package appstate

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/project"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// RegisterValueInfo is one raw register value, the shape the UI reads and writes.
type RegisterValueInfo struct {
	Address uint16
	Value   uint16
}

// AddRegisterRequest defines a new point. Endian may be empty, which means big,
// as Rust's Option<endian> default does.
type AddRegisterRequest struct {
	ConnectionID string
	SlaveID      uint8
	Address      uint16
	RegisterType string
	DataType     string
	Endian       string
	Name         string
	Comment      string
}

func parseDataType(value string) (register.DataType, error) {
	switch t := register.DataType(value); t {
	case register.TypeBool, register.TypeUInt16, register.TypeInt16,
		register.TypeUInt32, register.TypeInt32, register.TypeFloat:
		return t, nil
	}
	return "", fmt.Errorf("unknown data type: %s", value)
}

func parseEndian(value string) (register.Endian, error) {
	switch e := register.Endian(value); e {
	case register.EndianBig, register.EndianLittle, register.EndianMidBig, register.EndianMidLittle:
		return e, nil
	}
	return "", fmt.Errorf("unknown endian: %s", value)
}

func registerDef(req AddRegisterRequest) (register.RegisterDef, error) {
	registerType, err := parseRegisterType(req.RegisterType)
	if err != nil {
		return register.RegisterDef{}, err
	}
	dataType, err := parseDataType(req.DataType)
	if err != nil {
		return register.RegisterDef{}, err
	}
	endian := register.DefaultEndian
	if req.Endian != "" {
		if endian, err = parseEndian(req.Endian); err != nil {
			return register.RegisterDef{}, err
		}
	}
	return register.RegisterDef{
		Address:      req.Address,
		RegisterType: registerType,
		DataType:     dataType,
		Endian:       endian,
		Name:         req.Name,
		Comment:      req.Comment,
	}, nil
}

// AddRegister defines a point and allocates its registers.
func (m *Manager) AddRegister(req AddRegisterRequest) error {
	def, err := registerDef(req)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(req.ConnectionID)
	if err != nil {
		return err
	}
	return conn.updateDevice(req.SlaveID, func(device *slave.Device) error {
		if err := register.ValidateDefinitions(appendDef(device.RegisterDefs, def)); err != nil {
			return err
		}
		device.RegisterMap.EnsureFromDef(def)
		device.RegisterDefs = append(device.RegisterDefs, def)
		return nil
	})
}

// UpdateRegisterRequest replaces one point's definition, identified by its
// original area and address.
type UpdateRegisterRequest struct {
	ConnectionID         string
	SlaveID              uint8
	OriginalAddress      uint16
	OriginalRegisterType string
	Address              uint16
	RegisterType         string
	DataType             string
	Endian               string
	Name                 string
	Comment              string
}

// UpdateRegister moves or re-types a point. Its simulation config travels with
// it, and a data source keeps generating from where it was.
func (m *Manager) UpdateRegister(req UpdateRegisterRequest) error {
	originalType, err := parseRegisterType(req.OriginalRegisterType)
	if err != nil {
		return err
	}
	replacement, err := registerDef(AddRegisterRequest{
		ConnectionID: req.ConnectionID,
		SlaveID:      req.SlaveID,
		Address:      req.Address,
		RegisterType: req.RegisterType,
		DataType:     req.DataType,
		Endian:       req.Endian,
		Name:         req.Name,
		Comment:      req.Comment,
	})
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(req.ConnectionID)
	if err != nil {
		return err
	}

	var original register.RegisterDef
	err = conn.updateDevice(req.SlaveID, func(device *slave.Device) error {
		index := -1
		for i, def := range device.RegisterDefs {
			if def.Address == req.OriginalAddress && def.RegisterType == originalType {
				index = i
				break
			}
		}
		if index < 0 {
			return errors.New("original register not found")
		}
		original = device.RegisterDefs[index]
		if err := register.ValidateDefinitions(replaceDefAt(device.RegisterDefs, index, replacement)); err != nil {
			return err
		}
		device.RegisterMap.ReplaceDef(original, replacement)
		device.RegisterDefs[index] = replacement
		return nil
	})
	if err != nil {
		return err
	}

	oldKey := pointKey{SlaveID: req.SlaveID, RegisterType: originalType, Address: req.OriginalAddress}
	newKey := pointKey{SlaveID: req.SlaveID, RegisterType: replacement.RegisterType, Address: replacement.Address}
	if stored, ok := conn.points[oldKey]; ok {
		delete(conn.points, oldKey)
		conn.points[newKey] = stored
		conn.simulator.MovePoint(req.SlaveID, original, replacement)
	}
	return nil
}

// RemoveRegister deletes a point and frees its registers. Its simulation config
// goes with it.
func (m *Manager) RemoveRegister(connectionID string, slaveID uint8, address uint16, registerType string) error {
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
	var removed register.RegisterDef
	err = conn.updateDevice(slaveID, func(device *slave.Device) error {
		index := -1
		for i, def := range device.RegisterDefs {
			if def.Address == address && def.RegisterType == area {
				index = i
				break
			}
		}
		if index < 0 {
			return errors.New("register not found")
		}
		removed = device.RegisterDefs[index]
		device.RegisterDefs = append(device.RegisterDefs[:index], device.RegisterDefs[index+1:]...)
		device.RegisterMap.RemoveFromDef(removed)
		return nil
	})
	if err != nil {
		return err
	}
	key := pointKey{SlaveID: slaveID, RegisterType: area, Address: address}
	delete(conn.points, key)
	conn.simulator.SetPoint(slaveID, slave.Behaviour{Def: removed})
	return nil
}

// ReadRegister returns one raw value; an unallocated address reads as zero.
func (m *Manager) ReadRegister(connectionID string, slaveID uint8, registerType string, address uint16) (RegisterValueInfo, error) {
	area, err := parseRegisterType(registerType)
	if err != nil {
		return RegisterValueInfo{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(connectionID)
	if err != nil {
		return RegisterValueInfo{}, err
	}
	var value uint16
	err = conn.server.ViewDevice(slaveID, func(device *slave.Device) {
		value = rawValue(device.RegisterMap, area, address)
	})
	if err != nil {
		return RegisterValueInfo{}, err
	}
	return RegisterValueInfo{Address: address, Value: value}, nil
}

// ReadRegistersBulk returns every defined point of one area, in definition order.
func (m *Manager) ReadRegistersBulk(connectionID string, slaveID uint8, registerType string) ([]RegisterValueInfo, error) {
	area, err := parseRegisterType(registerType)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(connectionID)
	if err != nil {
		return nil, err
	}
	out := make([]RegisterValueInfo, 0)
	err = conn.server.ViewDevice(slaveID, func(device *slave.Device) {
		for _, def := range device.RegisterDefs {
			if def.RegisterType != area {
				continue
			}
			out = append(out, RegisterValueInfo{
				Address: def.Address,
				Value:   rawValue(device.RegisterMap, area, def.Address),
			})
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// WriteRegisterRequest sets one raw value from the UI.
type WriteRegisterRequest struct {
	ConnectionID string
	SlaveID      uint8
	RegisterType string
	Address      uint16
	Value        uint16
}

// WriteRegister writes a value and reports it to the UI. Like Rust's command it
// allocates on demand and notifies without going through the master-facing
// change callback.
func (m *Manager) WriteRegister(req WriteRegisterRequest) error {
	area, err := parseRegisterType(req.RegisterType)
	if err != nil {
		return err
	}
	m.mu.Lock()
	conn, err := m.connection(req.ConnectionID)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	err = conn.server.UpdateDevice(req.SlaveID, func(device *slave.Device) {
		writeRaw(device.RegisterMap, area, req.Address, req.Value)
	})
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.events.registerValueChanged(req.ConnectionID, []slave.Change{{
		SlaveID:      req.SlaveID,
		RegisterType: area,
		Address:      req.Address,
		Value:        req.Value,
	}})
	return nil
}

// ListRegisters returns a device's point definitions, optionally one area only.
func (m *Manager) ListRegisters(connectionID string, slaveID uint8, registerType string) ([]register.RegisterDef, error) {
	var area register.RegisterType
	if registerType != "" {
		parsed, err := parseRegisterType(registerType)
		if err != nil {
			return nil, err
		}
		area = parsed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(connectionID)
	if err != nil {
		return nil, err
	}
	out := make([]register.RegisterDef, 0)
	err = conn.server.ViewDevice(slaveID, func(device *slave.Device) {
		for _, def := range device.RegisterDefs {
			if registerType == "" || def.RegisterType == area {
				out = append(out, def)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ExportRegisters renders a device's points -- definitions plus their simulation
// config -- as pretty JSON for a file export.
func (m *Manager) ExportRegisters(connectionID string, slaveID uint8) (string, error) {
	points, err := m.ListPoints(connectionID, slaveID, "")
	if err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(points, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to serialize: %w", err)
	}
	return string(out), nil
}

// ListPoints returns native point DTOs including their simulation configuration.
// The UI must see these configs when editing an already configured point.
func (m *Manager) ListPoints(connectionID string, slaveID uint8, registerType string) ([]*project.Point, error) {
	if registerType != "" {
		if _, err := parseRegisterType(registerType); err != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(connectionID)
	if err != nil {
		return nil, err
	}
	points := make([]*project.Point, 0)
	err = conn.server.ViewDevice(slaveID, func(device *slave.Device) {
		for _, def := range device.RegisterDefs {
			if registerType != "" && string(def.RegisterType) != registerType {
				continue
			}
			cfg := conn.points[pointKey{SlaveID: slaveID, RegisterType: def.RegisterType, Address: def.Address}]
			var mut *mutation.Config
			var src *datasource.Config
			if cfg != nil {
				mut, src = cfg.mutation, cfg.source
			}
			points = append(points, project.NewPoint(def, mut, src))
		}
	})
	if err != nil {
		return nil, err
	}
	return points, nil
}

// ImportRegistersRequest adds points to a device, optionally replacing its whole
// definition set.
type ImportRegistersRequest struct {
	ConnectionID string
	SlaveID      uint8
	Points       []*project.Point
	// Mode is "append" (default) or "replace". Replacing needs a stopped
	// connection because it discards the register contents.
	Mode string
}

// ImportRegisters loads a definition set, validating the merged result before it
// is applied so a bad import leaves the device untouched.
func (m *Manager) ImportRegisters(req ImportRegistersRequest) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(req.ConnectionID)
	if err != nil {
		return 0, err
	}
	replace := req.Mode == "replace"
	if !replace && req.Mode != "" && req.Mode != "append" {
		return 0, fmt.Errorf("unknown import mode %q", req.Mode)
	}
	if replace && conn.state != Stopped {
		return 0, errors.New("Stop the connection before replacing register definitions")
	}

	imported := make([]register.RegisterDef, 0, len(req.Points))
	for _, point := range req.Points {
		if point == nil {
			return 0, errors.New("imported register must not be null")
		}
		imported = append(imported, point.ToDef())
	}

	err = conn.updateDevice(req.SlaveID, func(device *slave.Device) error {
		prospective := make([]register.RegisterDef, 0, len(device.RegisterDefs)+len(imported))
		if !replace {
			prospective = append(prospective, device.RegisterDefs...)
		}
		prospective = append(prospective, imported...)
		if err := register.ValidateDefinitions(prospective); err != nil {
			return err
		}
		if replace {
			device.RegisterMap = register.NewRegisterMap()
			device.RegisterDefs = nil
		}
		for _, def := range imported {
			device.RegisterMap.EnsureFromDef(def)
			device.RegisterDefs = append(device.RegisterDefs, def)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	// The imported points carry their simulation config, so the registry is
	// rebuilt from them. Rust leaves an imported config inert until the point is
	// edited again; here it starts driving at once.
	conn.resetPointConfigs(req.SlaveID)
	for i, point := range req.Points {
		def := imported[i]
		mutationCfg, sourceCfg := point.ToMutation(), point.ToDataSource()
		if mutationCfg == nil && sourceCfg == nil {
			continue
		}
		key := pointKey{SlaveID: req.SlaveID, RegisterType: def.RegisterType, Address: def.Address}
		conn.points[key] = &pointConfig{mutation: mutationCfg, source: sourceCfg}
	}
	if device, ok := conn.server.GetDevice(req.SlaveID); ok {
		conn.resyncDevice(device)
	}
	return len(imported), nil
}

// updateDevice edits a device and reports "slave N not found" when it is gone.
// fn runs with the server's device lock held, so it must only touch the device.
func (c *connection) updateDevice(slaveID uint8, fn func(*slave.Device) error) error {
	var inner error
	if err := c.server.UpdateDevice(slaveID, func(device *slave.Device) { inner = fn(device) }); err != nil {
		return err
	}
	return inner
}

func appendDef(defs []register.RegisterDef, def register.RegisterDef) []register.RegisterDef {
	out := make([]register.RegisterDef, 0, len(defs)+1)
	out = append(out, defs...)
	return append(out, def)
}

func replaceDefAt(defs []register.RegisterDef, index int, def register.RegisterDef) []register.RegisterDef {
	out := make([]register.RegisterDef, len(defs))
	copy(out, defs)
	out[index] = def
	return out
}

func rawValue(m *register.RegisterMap, area register.RegisterType, address uint16) uint16 {
	switch area {
	case register.Coil:
		if m.Coils[address] {
			return 1
		}
	case register.DiscreteInput:
		if m.DiscreteInputs[address] {
			return 1
		}
	case register.HoldingRegType:
		return m.HoldingRegisters[address]
	case register.InputRegister:
		return m.InputRegisters[address]
	}
	return 0
}

// writeRaw matches Rust: the bit areas store a bool, the word areas the raw u16,
// and the read-only areas are inserted directly since they have no setter.
func writeRaw(m *register.RegisterMap, area register.RegisterType, address uint16, value uint16) {
	switch area {
	case register.Coil:
		m.WriteCoil(address, value != 0)
	case register.DiscreteInput:
		m.DiscreteInputs[address] = value != 0
	case register.HoldingRegType:
		m.WriteHoldingRegister(address, value)
	case register.InputRegister:
		m.InputRegisters[address] = value
	}
}
