// Package slave implements the Modbus slave (server) side: a shared device
// registry, request processing, and listeners for the five transports
// (TCP/MBAP, TCP+TLS, RTU-over-TCP, serial RTU, serial ASCII).
// Ported from crates/modbussim-core/src/{slave,rtu_slave,rtu_tcp_slave,
// ascii_slave,tls_slave}.rs with identical semantics.
package slave

import (
	"fmt"
	"log"
	"sync"

	"github.com/Karl-Dai/ModbusSim/go/internal/logcol"
	"github.com/Karl-Dai/ModbusSim/go/internal/pdu"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// Device is a single Modbus slave device with its own register map and
// definitions, mirroring Rust's SlaveDevice.
type Device struct {
	SlaveID      uint8                `json:"slave_id"`
	Name         string               `json:"name"`
	RegisterMap  *register.RegisterMap `json:"register_map"`
	RegisterDefs []register.RegisterDef `json:"register_defs"`
}

// NewDevice creates a device with empty maps.
func NewDevice(slaveID uint8, name string) *Device {
	return &Device{SlaveID: slaveID, Name: name, RegisterMap: register.NewRegisterMap()}
}

// WithDefaultRegisters pre-fills FC1/FC2/FC3/FC4 definitions for addresses
// 0..=maxAddress and initializes the register maps, mirroring Rust's
// SlaveDevice::with_default_registers.
func WithDefaultRegisters(slaveID uint8, name string, maxAddress uint16) *Device {
	d := NewDevice(slaveID, name)
	d.RegisterDefs = make([]register.RegisterDef, 0, (int(maxAddress)+1)*4)
	for addr := uint16(0); addr <= maxAddress; addr++ {
		d.RegisterDefs = append(d.RegisterDefs,
			register.RegisterDef{Address: addr, RegisterType: register.Coil, DataType: register.TypeBool, Endian: register.DefaultEndian},
			register.RegisterDef{Address: addr, RegisterType: register.DiscreteInput, DataType: register.TypeBool, Endian: register.DefaultEndian},
			register.RegisterDef{Address: addr, RegisterType: register.HoldingRegType, DataType: register.TypeUInt16, Endian: register.DefaultEndian},
			register.RegisterDef{Address: addr, RegisterType: register.InputRegister, DataType: register.TypeUInt16, Endian: register.DefaultEndian},
		)
		d.RegisterMap.WriteCoil(addr, false)
		if d.RegisterMap.DiscreteInputs == nil {
			d.RegisterMap.DiscreteInputs = map[uint16]bool{}
		}
		d.RegisterMap.DiscreteInputs[addr] = false
		d.RegisterMap.WriteHoldingRegister(addr, 0)
		if d.RegisterMap.InputRegisters == nil {
			d.RegisterMap.InputRegisters = map[uint16]uint16{}
		}
		d.RegisterMap.InputRegisters[addr] = 0
	}
	return d
}

// Change is one register write triggered by an incoming Modbus request,
// mirroring Rust's RegisterChange.
type Change struct {
	SlaveID      uint8               `json:"slave_id"`
	RegisterType register.RegisterType `json:"register_type"`
	Address      uint16              `json:"address"`
	Value        uint16              `json:"value"`
}

// ChangeCallback is invoked after each successful write with the list of
// mutated registers.
type ChangeCallback func(changes []Change)

// Server is the shared device registry plus logging for one slave
// connection. All listener implementations share one Server.
type Server struct {
	mu             sync.RWMutex
	devices        map[uint8]*Device
	Log            *logcol.Collector // nil disables logging
	ChangeCallback ChangeCallback    // nil disables change callbacks
}

// NewServer creates a server with an empty device registry.
func NewServer() *Server {
	return &Server{devices: map[uint8]*Device{}}
}

// AddDevice registers a device; duplicate IDs are an error (mirrors
// SlaveError::DuplicateSlaveId).
func (s *Server) AddDevice(d *Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.devices[d.SlaveID]; exists {
		return fmt.Errorf("slave ID %d already exists", d.SlaveID)
	}
	s.devices[d.SlaveID] = d
	return nil
}

// RemoveDevice unregisters a device.
func (s *Server) RemoveDevice(slaveID uint8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.devices[slaveID]; !exists {
		return fmt.Errorf("slave ID %d not found", slaveID)
	}
	delete(s.devices, slaveID)
	return nil
}

// GetDevice returns a snapshot reference to a device (do not mutate maps
// without holding the server lock via Read/Write helpers).
func (s *Server) GetDevice(slaveID uint8) (*Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[slaveID]
	return d, ok
}

// ListDevices returns the registered device IDs.
func (s *Server) ListDevices() []uint8 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]uint8, 0, len(s.devices))
	for id := range s.devices {
		ids = append(ids, id)
	}
	return ids
}

// exception codes.
const (
	ExcIllegalFunction     = 0x01
	ExcIllegalDataAddress  = 0x02
	ExcIllegalDataValue    = 0x03
)

// ProcessRequest handles a Modbus request PDU for the given slave. Returns
// the response PDU, or nil when the slave ID is unknown (silent drop),
// mirroring Rust's process_request.
func (s *Server) ProcessRequest(slaveID uint8, requestPDU []byte) []byte {
	req, err := pdu.ParseRequest(requestPDU)
	if err != nil {
		pe := err.(*pdu.ParseError)
		fc := uint8(0)
		if len(requestPDU) > 0 {
			fc = requestPDU[0]
		}
		return pdu.BuildException(fc, pe.ExceptionCode())
	}
	if len(requestPDU) == 0 {
		return nil
	}
	fc := requestPDU[0]

	if req.IsWrite() {
		s.mu.Lock()
		defer s.mu.Unlock()
		device, ok := s.devices[slaveID]
		if !ok {
			return nil
		}
		data, exc := executeWrite(device.RegisterMap, req)
		if exc != 0 {
			return pdu.BuildException(fc, exc)
		}
		if s.ChangeCallback != nil {
			changes := changesFromRequest(slaveID, req)
			if len(changes) > 0 {
				s.ChangeCallback(changes)
			}
		}
		return pdu.BuildResponse(fc, data)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	device, ok := s.devices[slaveID]
	if !ok {
		return nil
	}
	data, exc := executeRead(device.RegisterMap, req)
	if exc != 0 {
		return pdu.BuildException(fc, exc)
	}
	return pdu.BuildResponse(fc, data)
}

// validateQuantity validates quantity and address overflow (mirrors
// Rust's validate_quantity).
func validateQuantity(addr, quantity, maxQuantity uint16) uint8 {
	if quantity == 0 || quantity > maxQuantity {
		return ExcIllegalDataValue
	}
	if uint32(addr)+uint32(quantity) > 65536 {
		return ExcIllegalDataAddress
	}
	return 0
}

// executeRead handles FC01-FC04. Returns (data, exceptionCode); exception 0
// means success.
func executeRead(m *register.RegisterMap, req *pdu.Request) (*pdu.ResponseData, uint8) {
	switch req.FunctionCode {
	case pdu.FCReadCoils: // max 2000
		if exc := validateQuantity(req.Address, req.Quantity, 2000); exc != 0 {
			return nil, exc
		}
		if !m.HasAllCoils(req.Address, req.Quantity) {
			return nil, ExcIllegalDataAddress
		}
		return &pdu.ResponseData{Bits: m.ReadCoils(req.Address, req.Quantity)}, 0

	case pdu.FCReadDiscreteInputs: // max 2000
		if exc := validateQuantity(req.Address, req.Quantity, 2000); exc != 0 {
			return nil, exc
		}
		if !m.HasAllDiscreteInputs(req.Address, req.Quantity) {
			return nil, ExcIllegalDataAddress
		}
		return &pdu.ResponseData{Bits: m.ReadDiscreteInputs(req.Address, req.Quantity)}, 0

	case pdu.FCReadHoldingRegisters: // max 125
		if exc := validateQuantity(req.Address, req.Quantity, 125); exc != 0 {
			return nil, exc
		}
		if !m.HasAllHoldingRegisters(req.Address, req.Quantity) {
			return nil, ExcIllegalDataAddress
		}
		return &pdu.ResponseData{Registers: m.ReadHoldingRegisters(req.Address, req.Quantity)}, 0

	case pdu.FCReadInputRegisters: // max 125
		if exc := validateQuantity(req.Address, req.Quantity, 125); exc != 0 {
			return nil, exc
		}
		if !m.HasAllInputRegisters(req.Address, req.Quantity) {
			return nil, ExcIllegalDataAddress
		}
		return &pdu.ResponseData{Registers: m.ReadInputRegisters(req.Address, req.Quantity)}, 0

	default:
		return nil, ExcIllegalFunction
	}
}

// executeWrite handles FC05/06/15/16. Returns (data, exceptionCode).
func executeWrite(m *register.RegisterMap, req *pdu.Request) (*pdu.ResponseData, uint8) {
	switch req.FunctionCode {
	case pdu.FCWriteSingleCoil:
		if !m.HasCoil(req.Address) {
			return nil, ExcIllegalDataAddress
		}
		m.WriteCoil(req.Address, req.CoilOn)
		return &pdu.ResponseData{WriteAddress: req.Address, WriteCoilOn: req.CoilOn}, 0

	case pdu.FCWriteSingleRegister:
		if !m.HasHoldingRegister(req.Address) {
			return nil, ExcIllegalDataAddress
		}
		m.WriteHoldingRegister(req.Address, req.Value)
		return &pdu.ResponseData{WriteAddress: req.Address, WriteValue: req.Value}, 0

	case pdu.FCWriteMultipleCoils: // max 1968
		if exc := validateQuantity(req.Address, req.Quantity, 1968); exc != 0 {
			return nil, exc
		}
		if !m.HasAllCoils(req.Address, req.Quantity) {
			return nil, ExcIllegalDataAddress
		}
		m.WriteCoils(req.Address, req.Bits)
		return &pdu.ResponseData{WriteAddress: req.Address, WriteQuantity: req.Quantity}, 0

	case pdu.FCWriteMultipleRegisters: // max 123
		if exc := validateQuantity(req.Address, req.Quantity, 123); exc != 0 {
			return nil, exc
		}
		if !m.HasAllHoldingRegisters(req.Address, req.Quantity) {
			return nil, ExcIllegalDataAddress
		}
		m.WriteHoldingRegisters(req.Address, req.Registers)
		return &pdu.ResponseData{WriteAddress: req.Address, WriteQuantity: req.Quantity}, 0

	default:
		return nil, ExcIllegalFunction
	}
}

// ChangesFromRequest builds the side-effect list for a successful write
// request (mirrors Rust's changes_from_modbus_request).
func ChangesFromRequest(slaveID uint8, req *pdu.Request) []Change {
	switch req.FunctionCode {
	case pdu.FCWriteSingleCoil:
		v := uint16(0)
		if req.CoilOn {
			v = 1
		}
		return []Change{{SlaveID: slaveID, RegisterType: register.Coil, Address: req.Address, Value: v}}

	case pdu.FCWriteSingleRegister:
		return []Change{{SlaveID: slaveID, RegisterType: register.HoldingRegType, Address: req.Address, Value: req.Value}}

	case pdu.FCWriteMultipleCoils:
		out := make([]Change, 0, len(req.Bits))
		for i, v := range req.Bits {
			val := uint16(0)
			if v {
				val = 1
			}
			out = append(out, Change{SlaveID: slaveID, RegisterType: register.Coil, Address: req.Address + uint16(i), Value: val})
		}
		return out

	case pdu.FCWriteMultipleRegisters:
		out := make([]Change, 0, len(req.Registers))
		for i, v := range req.Registers {
			out = append(out, Change{SlaveID: slaveID, RegisterType: register.HoldingRegType, Address: req.Address + uint16(i), Value: v})
		}
		return out
	}
	return nil
}

func changesFromRequest(slaveID uint8, req *pdu.Request) []Change {
	return ChangesFromRequest(slaveID, req)
}

// FormatRequestDetail renders the human-readable request detail used in log
// entries (mirrors Rust's format_request).
func FormatRequestDetail(req *pdu.Request) string {
	switch req.FunctionCode {
	case pdu.FCReadCoils, pdu.FCReadDiscreteInputs, pdu.FCReadHoldingRegisters, pdu.FCReadInputRegisters:
		return fmt.Sprintf("R %d x%d", req.Address, req.Quantity)
	case pdu.FCWriteSingleCoil:
		return fmt.Sprintf("W %d = %v", req.Address, req.CoilOn)
	case pdu.FCWriteSingleRegister:
		return fmt.Sprintf("W %d = 0x%04X", req.Address, req.Value)
	case pdu.FCWriteMultipleCoils:
		return fmt.Sprintf("W %d x%d", req.Address, len(req.Bits))
	case pdu.FCWriteMultipleRegisters:
		return fmt.Sprintf("W %d x%d", req.Address, len(req.Registers))
	}
	return "?"
}

// LogRx/LogTx helpers shared by listeners.
func (s *Server) LogRx(req *pdu.Request) {
	s.logOne(logcol.Rx, req, FormatRequestDetail(req), nil)
}

// LogResponse logs the outbound response detail (OK or exception).
func (s *Server) LogResponse(req *pdu.Request, responsePDU []byte) {
	detail := "OK"
	if len(responsePDU) > 0 && responsePDU[0]&0x80 != 0 {
		exc := uint8(0)
		if len(responsePDU) > 1 {
			exc = responsePDU[1]
		}
		detail = fmt.Sprintf("ERR: exception 0x%02X", exc)
	}
	s.logOne(logcol.Tx, req, detail, nil)
}

func (s *Server) logOne(dir logcol.Direction, req *pdu.Request, detail string, raw []byte) {
	if s.Log == nil {
		return
	}
	fc, ok := logcol.FromU8(req.FunctionCode)
	if !ok {
		return
	}
	if raw != nil {
		s.Log.TryAdd(logcol.NewEntryWithRaw(dir, fc, detail, raw))
	} else {
		s.Log.TryAdd(logcol.NewEntry(dir, fc, detail))
	}
}

// warn logs listener errors through the standard logger (mirrors Rust's
// log::warn/error usage in listener tasks).
func warn(format string, args ...any) {
	log.Printf(format, args...)
}
