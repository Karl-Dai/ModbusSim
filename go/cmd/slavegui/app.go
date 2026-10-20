package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Karl-Dai/ModbusSim/go/internal/appstate"
	"github.com/Karl-Dai/ModbusSim/go/internal/explain"
	"github.com/Karl-Dai/ModbusSim/go/internal/nativedialog"
	"github.com/Karl-Dai/ModbusSim/go/internal/project"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/serial"
	"github.com/Karl-Dai/ModbusSim/go/internal/tools"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Version may be set with -ldflags '-X main.Version=...'.
var Version = "0.17.4"

// App binds native Go DTOs to the desktop; the Manager owns all protocol state.
type App struct {
	ctx     context.Context
	manager *appstate.Manager
	emit    func(string, interface{})
}

type RegisterChange struct {
	SlaveID      uint8
	RegisterType register.RegisterType
	Address      uint16
	Value        uint16
}
type RegisterValueEvent struct {
	ConnectionID string
	Changes      []RegisterChange
}
type ClientInfo struct {
	ID          uint64
	PeerAddress string
	ConnectedAt string
}
type ConnectionInfo struct {
	ID          string
	BindAddress string
	Port        uint16
	State       appstate.ConnectionState
	DeviceCount int
	Clients     []ClientInfo
}
type Point struct {
	Address      uint16
	RegisterType register.RegisterType
	DataType     register.DataType
	Endian       register.Endian
	Name         string
	Comment      string
	Mutation     *project.MutationConfig
	DataSource   *project.DataSourceConfig
}
type LogEntry struct {
	Timestamp    string
	Direction    string
	FunctionCode string
	Detail       string
}
type SetPointMutationRequest struct {
	ConnectionID string
	SlaveID      uint8
	RegisterType string
	Address      uint16
	Config       project.MutationConfig
}
type SetDataSourceRequest struct {
	ConnectionID     string
	SlaveID          uint8
	RegisterType     string
	Address          uint16
	Source           project.DataSource
	UpdateIntervalMS uint64
}
type PLCAddress struct {
	ProtocolAddress uint16
	RegisterType    string
}
type FileFilter struct {
	Name       string
	Extensions []string
}

func newApp() *App {
	a := &App{}
	a.emit = func(name string, payload interface{}) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, name, payload)
		}
	}
	a.manager = appstate.NewManager(appstate.Events{
		LogAppended:            func(e appstate.LogAppendedEvent) { a.emit("log-appended", e) },
		ConnectionStateChanged: func(e appstate.ConnectionStateEvent) { a.emit("slave-connection-state", e) },
		RegisterValueChanged: func(e appstate.RegisterValueEvent) {
			changes := make([]RegisterChange, 0, len(e.Changes))
			for _, c := range e.Changes {
				changes = append(changes, RegisterChange{c.SlaveID, c.RegisterType, c.Address, c.Value})
			}
			a.emit("register-value-changed", RegisterValueEvent{e.ConnectionID, changes})
		},
	})
	return a
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }
func (a *App) shutdown(context.Context)    { a.manager.CloseAll() }
func (a *App) GetVersion() string          { return Version }
func (a *App) CreateConnection(req appstate.CreateConnectionRequest) (ConnectionInfo, error) {
	if err := appstate.ValidateTransport(req.Transport); err != nil {
		return ConnectionInfo{}, err
	}
	info, err := a.manager.CreateConnection(req)
	return connectionInfo(info), err
}
func connectionInfo(info appstate.ConnectionInfo) ConnectionInfo {
	out := ConnectionInfo{ID: info.ID, BindAddress: info.BindAddress, Port: info.Port, State: info.State, DeviceCount: info.DeviceCount}
	if info.Clients != nil {
		out.Clients = make([]ClientInfo, 0, len(info.Clients))
		for _, c := range info.Clients {
			out.Clients = append(out.Clients, ClientInfo{c.ID, c.PeerAddress, c.ConnectedAt})
		}
	}
	return out
}
func (a *App) ListConnections() []ConnectionInfo {
	out := make([]ConnectionInfo, 0)
	for _, info := range a.manager.ListConnections() {
		out = append(out, connectionInfo(info))
	}
	return out
}
func (a *App) StartConnection(id string) error                   { return a.manager.StartConnection(id) }
func (a *App) StopConnection(id string) error                    { return a.manager.StopConnection(id) }
func (a *App) DeleteConnection(id string) error                  { return a.manager.DeleteConnection(id) }
func (a *App) GetTransport(id string) (project.Transport, error) { return a.manager.GetTransport(id) }
func (a *App) UpdateTransport(id string, transport project.Transport) error {
	return a.manager.UpdateTransport(id, transport)
}
func (a *App) AddDevice(req appstate.AddDeviceRequest) (appstate.DeviceInfo, error) {
	return a.manager.AddDevice(req)
}
func (a *App) UpdateDevice(req appstate.UpdateDeviceRequest) (appstate.DeviceInfo, error) {
	return a.manager.UpdateDevice(req)
}
func (a *App) RemoveDevice(id string, slaveID uint8) error {
	return a.manager.RemoveDevice(id, slaveID)
}
func (a *App) ListDevices(id string) ([]appstate.DeviceInfo, error) {
	rows, err := a.manager.ListDevices(id)
	if rows == nil {
		rows = []appstate.DeviceInfo{}
	}
	return rows, err
}
func (a *App) AddRegister(req appstate.AddRegisterRequest) error { return a.manager.AddRegister(req) }
func (a *App) UpdateRegister(req appstate.UpdateRegisterRequest) error {
	return a.manager.UpdateRegister(req)
}
func (a *App) RemoveRegister(id string, slaveID uint8, address uint16, area string) error {
	return a.manager.RemoveRegister(id, slaveID, address, area)
}
func (a *App) ListRegisters(id string, slaveID uint8, area string) ([]Point, error) {
	points, err := a.manager.ListPoints(id, slaveID, area)
	out := make([]Point, 0, len(points))
	for _, p := range points {
		out = append(out, Point{p.Address, p.RegisterType, p.DataType, p.Endian, p.Name, p.Comment, p.Mutation, p.DataSource})
	}
	return out, err
}
func (a *App) ReadRegister(id string, slaveID uint8, area string, address uint16) (appstate.RegisterValueInfo, error) {
	return a.manager.ReadRegister(id, slaveID, area, address)
}
func (a *App) ReadRegistersBulk(id string, slaveID uint8, area string) ([]appstate.RegisterValueInfo, error) {
	rows, err := a.manager.ReadRegistersBulk(id, slaveID, area)
	if rows == nil {
		rows = []appstate.RegisterValueInfo{}
	}
	return rows, err
}
func (a *App) WriteRegister(req appstate.WriteRegisterRequest) error {
	return a.manager.WriteRegister(req)
}
func (a *App) ImportRegisters(req appstate.ImportRegistersRequest) (int, error) {
	return a.manager.ImportRegisters(req)
}
func (a *App) ExportRegisters(id string, slaveID uint8) (string, error) {
	return a.manager.ExportRegisters(id, slaveID)
}
func (a *App) SetPointMutation(req SetPointMutationRequest) error {
	return a.manager.SetPointMutation(appstate.SetPointMutationRequest{ConnectionID: req.ConnectionID, SlaveID: req.SlaveID, RegisterType: req.RegisterType, Address: req.Address, Config: req.Config.ToEngine()})
}
func (a *App) ClearPointMutation(id string, slaveID uint8, area string, address uint16) error {
	return a.manager.ClearPointMutation(id, slaveID, area, address)
}
func (a *App) ListPointMutations(id string, slaveID uint8) ([]appstate.PointMutationInfo, error) {
	rows, err := a.manager.ListPointMutations(id, slaveID)
	if rows == nil {
		rows = []appstate.PointMutationInfo{}
	}
	return rows, err
}
func (a *App) SetMutationRunning(running bool) { a.manager.SetMutationRunning(running) }
func (a *App) MutationRunning() bool           { return a.manager.MutationRunning() }
func (a *App) SetDataSource(req SetDataSourceRequest) error {
	return a.manager.SetDataSource(appstate.SetDataSourceRequest{ConnectionID: req.ConnectionID, SlaveID: req.SlaveID, RegisterType: req.RegisterType, Address: req.Address, Source: req.Source.ToEngine(), UpdateIntervalMS: req.UpdateIntervalMS})
}
func (a *App) RemoveDataSource(id string, slaveID uint8, area string, address uint16) error {
	return a.manager.RemoveDataSource(id, slaveID, area, address)
}
func (a *App) Logs(id string) ([]LogEntry, error) {
	rows, err := a.manager.Logs(id)
	out := make([]LogEntry, 0, len(rows))
	for _, e := range rows {
		out = append(out, LogEntry{e.Timestamp.Format("2006-01-02T15:04:05.000Z07:00"), string(e.Direction), e.FunctionCode.Name(), e.Detail})
	}
	return out, err
}
func (a *App) ClearLogs(id string) error               { return a.manager.ClearLogs(id) }
func (a *App) ExportLogsCSV(id string) (string, error) { return a.manager.ExportLogsCSV(id) }
func (a *App) LoadProject(path string) (int, error)    { return a.manager.LoadProject(path) }
func (a *App) SaveProject(path string) error           { return a.manager.SaveProject(path) }
func (a *App) ListSerialPorts() ([]string, error)      { return serial.ListPorts() }
func (a *App) InspectModbusFrame(data, transport, direction, lang string) (string, error) {
	return explain.Explain(data, transport, direction, explain.LangFromCode(lang))
}
func (a *App) ConvertPLCToModbus(address string) (PLCAddress, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(address), 10, 32)
	if err != nil {
		return PLCAddress{}, fmt.Errorf("invalid PLC address: %w", err)
	}
	result, err := tools.PLCToModbus(uint32(value))
	areas := map[tools.AddressType]string{tools.Coil: "coil", tools.DiscreteInput: "discrete_input", tools.InputRegister: "input_register", tools.HoldingReg: "holding_register"}
	return PLCAddress{result.Address, areas[result.Type]}, err
}
func (a *App) CalculateCRC16(data string) (string, error) {
	bytes, err := tools.ParseHexString(data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04X", tools.CRC16(bytes)), nil
}
func (a *App) CalculateLRC(data string) (string, error) {
	bytes, err := tools.ParseHexString(data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%02X", tools.LRC(bytes)), nil
}
func dialogFilters(filters []FileFilter) []runtime.FileFilter {
	out := make([]runtime.FileFilter, 0, len(filters))
	for _, f := range filters {
		patterns := make([]string, 0, len(f.Extensions))
		for _, ext := range f.Extensions {
			patterns = append(patterns, "*."+ext)
		}
		out = append(out, runtime.FileFilter{DisplayName: f.Name, Pattern: strings.Join(patterns, ";")})
	}
	return out
}
func nativeFilters(filters []FileFilter) []nativedialog.Filter {
	out := make([]nativedialog.Filter, 0, len(filters))
	for _, f := range filters {
		out = append(out, nativedialog.Filter{Name: f.Name, Extensions: f.Extensions})
	}
	return out
}
func (a *App) OpenFileDialog(filters []FileFilter) (string, error) {
	return nativedialog.OpenFile(a.ctx, nativeFilters(filters))
}
func (a *App) SaveFileDialog(defaultPath string, filters []FileFilter) (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{DefaultFilename: filepath.Base(defaultPath), Filters: dialogFilters(filters)})
}
func (a *App) SaveTextExport(path, content string) error {
	return os.WriteFile(path, []byte(content), 0600)
}
