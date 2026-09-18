// Small accessors shared by the app layer and its tests.
package app

import (
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/logcol"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// LogCollector exposes the connection's log collector.
func (s *State) LogCollector(id string) *logcol.Collector {
	conn, ok := s.Connection(id)
	if !ok {
		return nil
	}
	return conn.Log()
}

// ReadRegister reads one raw register value for a point.
func (s *State) ReadRegister(id string, slaveID uint8, rt register.RegisterType, addr uint16) (uint16, bool) {
	conn, ok := s.Connection(id)
	if !ok {
		return 0, false
	}
	return conn.ReadRegisterValue(slaveID, rt, addr)
}

// WriteRegister writes one register value through the state.
func (s *State) WriteRegister(id string, slaveID uint8, rt register.RegisterType, addr, value uint16) error {
	conn, ok := s.Connection(id)
	if !ok {
		return fmt.Errorf("connection %s not found", id)
	}
	return conn.WriteRegisterValue(slaveID, rt, addr, value)
}

// connectionDevice fetches the device pointer (test helper name).
func conn2Device(s *State, id string, slaveID uint8) (device, bool) {
	conn, ok := s.Connection(id)
	if !ok {
		return device{}, false
	}
	d, ok := conn.Server().GetDevice(slaveID)
	if !ok {
		return device{}, false
	}
	return device{SlaveID: d.SlaveID, Name: d.Name, RegisterMap: d.RegisterMap, RegisterDefs: d.RegisterDefs}, true
}

// device mirrors slave.Device for tests without importing the slave type.
type device struct {
	SlaveID      uint8
	Name         string
	RegisterMap  *register.RegisterMap
	RegisterDefs []register.RegisterDef
}

// freePort binds port 0 and returns the chosen port.
func freePort(t *testing.T) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return uint16(ln.Addr().(*net.TCPAddr).Port)
}

// setTransport replaces a connection's transport (test support).
func setTransport(t *testing.T, s *State, id string, transport TransportConfig) {
	t.Helper()
	conn, ok := s.Connection(id)
	if !ok {
		t.Fatalf("connection %s not found", id)
	}
	conn.mu.Lock()
	conn.transport = transport
	conn.mu.Unlock()
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

func logcolEntryFor(direction, detail string) logcol.Entry {
	fc, _ := logcol.FromU8(3)
	dir := logcol.Rx
	if direction == "tx" {
		dir = logcol.Tx
	}
	return logcol.NewEntry(dir, fc, detail)
}
