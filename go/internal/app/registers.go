// Register, log and tool command implementations mirroring
// crates/modbussim-app/src/commands.rs (register/log/tool sections).
package app

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/config"
	"github.com/Karl-Dai/ModbusSim/go/internal/logcol"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/tools"
)

// AddRegisterRequest mirrors commands.rs AddRegisterRequest.
type AddRegisterRequest struct {
	ConnectionID string  `json:"connection_id"`
	SlaveID      uint8   `json:"slave_id"`
	Address      uint16  `json:"address"`
	RegisterType string  `json:"register_type"`
	DataType     string  `json:"data_type"`
	Endian       *string `json:"endian"`
	Name         *string `json:"name"`
	Comment      *string `json:"comment"`
}

// RegisterDefFromRequest validates a request and builds the definition
// (mirrors register_def_from_request).
func RegisterDefFromRequest(req AddRegisterRequest) (register.RegisterDef, error) {
	rt, err := parseRegisterTypeString(req.RegisterType)
	if err != nil {
		return register.RegisterDef{}, err
	}
	dt := register.NormalizeDataType(register.DataType(req.DataType))
	if !validDataType(dt) {
		return register.RegisterDef{}, fmt.Errorf("unknown data type: %s", req.DataType)
	}
	endian := register.EndianBig
	if req.Endian != nil && *req.Endian != "" {
		endian = register.Endian(*req.Endian)
		switch endian {
		case register.EndianBig, register.EndianLittle, register.EndianMidBig, register.EndianMidLittle:
		default:
			return register.RegisterDef{}, fmt.Errorf("unknown endian: %s", *req.Endian)
		}
	}
	def := register.RegisterDef{
		Address:      req.Address,
		RegisterType: rt,
		DataType:     dt,
		Endian:       endian,
	}
	if req.Name != nil {
		def.Name = *req.Name
	}
	if req.Comment != nil {
		def.Comment = *req.Comment
	}
	return def, nil
}

func parseRegisterTypeString(s string) (register.RegisterType, error) {
	switch s {
	case "coil":
		return register.Coil, nil
	case "discrete_input":
		return register.DiscreteInput, nil
	case "input_register":
		return register.InputRegister, nil
	case "holding_register":
		return register.HoldingRegType, nil
	}
	return "", fmt.Errorf("unknown register type: %s", s)
}

func validDataType(d register.DataType) bool {
	switch d {
	case register.TypeBool, register.TypeUInt16, register.TypeInt16,
		register.TypeUInt32, register.TypeInt32, register.TypeFloat:
		return true
	}
	return false
}

// AddRegister adds a point to a device with set validation.
func (s *State) AddRegister(req AddRegisterRequest) error {
	conn, ok := s.Connection(req.ConnectionID)
	if !ok {
		return fmt.Errorf("connection %s not found", req.ConnectionID)
	}
	def, err := RegisterDefFromRequest(req)
	if err != nil {
		return err
	}
	dev, ok := conn.Server().GetDevice(req.SlaveID)
	if !ok {
		return fmt.Errorf("slave %d not found", req.SlaveID)
	}
	prospective := append(append([]register.RegisterDef{}, dev.RegisterDefs...), def)
	if err := register.ValidateDefinitions(prospective); err != nil {
		return err
	}
	dev.RegisterMap.EnsureFromDef(def)
	dev.RegisterDefs = append(dev.RegisterDefs, def)
	return nil
}

// UpdateRegister replaces a point, preserving mutation/data-source config
// and remapping runtime mutation/data-source entries (commands.rs
// update_register).
func (s *State) UpdateRegister(req AddRegisterRequest, originalAddress uint16, originalRegisterType string) error {
	origType, err := parseRegisterTypeString(originalRegisterType)
	if err != nil {
		return err
	}
	replacement, err := RegisterDefFromRequest(req)
	if err != nil {
		return err
	}
	conn, ok := s.Connection(req.ConnectionID)
	if !ok {
		return fmt.Errorf("connection %s not found", req.ConnectionID)
	}
	dev, ok := conn.Server().GetDevice(req.SlaveID)
	if !ok {
		return fmt.Errorf("slave %d not found", req.SlaveID)
	}
	index := -1
	for i := range dev.RegisterDefs {
		if dev.RegisterDefs[i].Address == originalAddress && dev.RegisterDefs[i].RegisterType == origType {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("original register not found")
	}
	original := dev.RegisterDefs[index]
	replacement.Mutation = original.Mutation
	replacement.DataSource = original.DataSource

	prospective := append([]register.RegisterDef{}, dev.RegisterDefs...)
	prospective[index] = replacement
	if err := register.ValidateDefinitions(prospective); err != nil {
		return err
	}
	dev.RegisterMap.ReplaceDef(original, replacement)
	dev.RegisterDefs[index] = replacement

	// Remap mutation runtime entry.
	oldKey := PointKey{ConnectionID: req.ConnectionID, SlaveID: req.SlaveID, RegisterType: origType, Address: originalAddress}
	newKey := PointKey{ConnectionID: req.ConnectionID, SlaveID: req.SlaveID, RegisterType: replacement.RegisterType, Address: replacement.Address}
	s.mu.Lock()
	if rt, ok := s.mutationRun[oldKey]; ok {
		delete(s.mutationRun, oldKey)
		if replacement.Mutation != nil && replacement.Mutation.Enabled {
			rt.Def = replacement
			rt.NextDue = time.Now().Add(MutationPeriod(rt.Config.PeriodMs))
			s.mutationRun[newKey] = rt
		}
	}
	if oldKey != newKey {
		if ds, ok := s.dataSources[oldKey]; ok {
			delete(s.dataSources, oldKey)
			s.dataSources[newKey] = ds
		}
	}
	s.mu.Unlock()
	return nil
}

// RemoveRegister deletes a point from a device.
func (s *State) RemoveRegister(connectionID string, slaveID uint8, address uint16, registerType string) error {
	conn, ok := s.Connection(connectionID)
	if !ok {
		return fmt.Errorf("connection %s not found", connectionID)
	}
	rt, err := parseRegisterTypeString(registerType)
	if err != nil {
		return err
	}
	dev, ok := conn.Server().GetDevice(slaveID)
	if !ok {
		return fmt.Errorf("slave %d not found", slaveID)
	}
	index := -1
	for i := range dev.RegisterDefs {
		if dev.RegisterDefs[i].Address == address && dev.RegisterDefs[i].RegisterType == rt {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("register not found")
	}
	original := dev.RegisterDefs[index]
	dev.RegisterMap.RemoveFromDef(original)
	dev.RegisterDefs = append(dev.RegisterDefs[:index], dev.RegisterDefs[index+1:]...)
	key := PointKey{ConnectionID: connectionID, SlaveID: slaveID, RegisterType: rt, Address: address}
	s.ClearPointMutation(key)
	s.RemoveDataSource(key)
	return nil
}

// ListRegisters returns all point definitions of a device.
func (s *State) ListRegisters(connectionID string, slaveID uint8) ([]register.RegisterDef, error) {
	conn, ok := s.Connection(connectionID)
	if !ok {
		return nil, fmt.Errorf("connection %s not found", connectionID)
	}
	dev, ok := conn.Server().GetDevice(slaveID)
	if !ok {
		return nil, fmt.Errorf("slave %d not found", slaveID)
	}
	return dev.RegisterDefs, nil
}

// ExportRegisters serializes a device's points to JSON (def + current value).
func (s *State) ExportRegisters(connectionID string, slaveID uint8) (string, error) {
	conn, ok := s.Connection(connectionID)
	if !ok {
		return "", fmt.Errorf("connection %s not found", connectionID)
	}
	dev, ok := conn.Server().GetDevice(slaveID)
	if !ok {
		return "", fmt.Errorf("slave %d not found", slaveID)
	}
	entries := make([]config.RegisterDefEntry, 0, len(dev.RegisterDefs))
	for _, def := range dev.RegisterDefs {
		entry := config.RegisterDefEntry{
			Address:      def.Address,
			RegisterType: def.RegisterType,
			DataType:     def.DataType,
			Endian:       def.Endian,
			Name:         def.Name,
			Comment:      def.Comment,
			Mutation:     def.Mutation,
			DataSource:   def.DataSource,
		}
		if v, ok := conn.ReadRegisterValue(slaveID, def.RegisterType, def.Address); ok {
			value := v
			entry.Value = &value
		}
		entries = append(entries, entry)
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ImportRegisters loads points from exported JSON, replacing the device's set.
func (s *State) ImportRegisters(connectionID string, slaveID uint8, jsonStr string) (int, error) {
	conn, ok := s.Connection(connectionID)
	if !ok {
		return 0, fmt.Errorf("connection %s not found", connectionID)
	}
	var entries []config.RegisterDefEntry
	if err := json.Unmarshal([]byte(jsonStr), &entries); err != nil {
		return 0, fmt.Errorf("failed to parse JSON: %w", err)
	}
	dev, ok := conn.Server().GetDevice(slaveID)
	if !ok {
		return 0, fmt.Errorf("slave %d not found", slaveID)
	}
	defs := make([]register.RegisterDef, 0, len(entries))
	for _, e := range entries {
		defs = append(defs, e.ToRegisterDef())
	}
	if err := register.ValidateDefinitions(defs); err != nil {
		return 0, err
	}
	for _, def := range dev.RegisterDefs {
		dev.RegisterMap.RemoveFromDef(def)
	}
	dev.RegisterDefs = defs
	for i, def := range defs {
		dev.RegisterMap.EnsureFromDef(def)
		if entries[i].Value != nil {
			_ = conn.WriteRegisterValue(slaveID, def.RegisterType, def.Address, *entries[i].Value)
		}
	}
	return len(defs), nil
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

// GetLogs returns a paginated slice of the connection's log entries.
func (s *State) GetLogs(connectionID string, offset, limit int) ([]logcol.Entry, error) {
	conn, ok := s.Connection(connectionID)
	if !ok {
		return nil, fmt.Errorf("connection %s not found", connectionID)
	}
	if limit <= 0 {
		return conn.Log().GetAll(), nil
	}
	return conn.Log().GetPaginated(offset, limit), nil
}

// ExportLogsCSV returns all log entries as CSV.
func (s *State) ExportLogsCSV(connectionID string) (string, error) {
	conn, ok := s.Connection(connectionID)
	if !ok {
		return "", fmt.Errorf("connection %s not found", connectionID)
	}
	return conn.Log().ExportCSV(), nil
}

// ExportLogsText returns all log entries as plain text.
func (s *State) ExportLogsText(connectionID string) (string, error) {
	conn, ok := s.Connection(connectionID)
	if !ok {
		return "", fmt.Errorf("connection %s not found", connectionID)
	}
	return conn.Log().ExportText(), nil
}

// ClearLogs empties the connection's log collector.
func (s *State) ClearLogs(connectionID string) error {
	conn, ok := s.Connection(connectionID)
	if !ok {
		return fmt.Errorf("connection %s not found", connectionID)
	}
	conn.Log().Clear()
	return nil
}

// SaveTextExport writes text content to a file (commands.rs save_text_export).
func SaveTextExport(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

// ConvertPlcToModbus converts a PLC-style decimal address (e.g. 40123 for
// holding registers) to its Modbus address. PLC notation encodes the area in
// the leading digits, so the return is the area type and address pair via
// tools.Address.
func ConvertPlcToModbus(plc uint32) (register.RegisterType, uint16, error) {
	addr, err := tools.PLCToModbus(plc)
	if err != nil {
		return "", 0, err
	}
	return addrTypeToRegisterType(addr.Type), addr.Address, nil
}

// ConvertModbusToPlc renders a Modbus address in PLC notation.
func ConvertModbusToPlc(address uint16, registerType string) (uint32, error) {
	rt, err := parseRegisterTypeString(registerType)
	if err != nil {
		return 0, err
	}
	at, err := plcAddressType(rt)
	if err != nil {
		return 0, err
	}
	return tools.ModbusToPLC(address, at), nil
}

func plcAddressType(rt register.RegisterType) (tools.AddressType, error) {
	switch rt {
	case register.Coil:
		return tools.Coil, nil
	case register.DiscreteInput:
		return tools.DiscreteInput, nil
	case register.InputRegister:
		return tools.InputRegister, nil
	case register.HoldingRegType:
		return tools.HoldingReg, nil
	}
	return 0, fmt.Errorf("unknown register type: %s", rt)
}

func addrTypeToRegisterType(t tools.AddressType) register.RegisterType {
	switch t {
	case tools.Coil:
		return register.Coil
	case tools.DiscreteInput:
		return register.DiscreteInput
	case tools.InputRegister:
		return register.InputRegister
	}
	return register.HoldingRegType
}

// CalculateCRC16 computes the Modbus RTU CRC of hex bytes ("A2 4B" form).
func CalculateCRC16(hexData string) (string, error) {
	data, err := tools.ParseHexString(hexData)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04X", tools.CRC16(data)), nil
}

// CalculateLRC computes the Modbus ASCII LRC of hex bytes.
func CalculateLRC(hexData string) (string, error) {
	data, err := tools.ParseHexString(hexData)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%02X", tools.LRC(data)), nil
}

// ParseHex parses hex text into bytes (commands.rs parse_hex).
func ParseHex(hexData string) ([]byte, error) {
	return tools.ParseHexString(hexData)
}
