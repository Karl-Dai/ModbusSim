package main

import (
	"context"
	"github.com/Karl-Dai/ModbusSim/go/internal/masterstate"
	"github.com/Karl-Dai/ModbusSim/go/internal/serial"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sync"
)

var Version = "0.17.4-go-dev"

type App struct {
	mu      sync.RWMutex
	ctx     context.Context
	manager *masterstate.Manager
	emit    func(string, any)
}

func newApp() *App {
	a := &App{}
	a.emit = func(name string, payload any) {
		a.mu.RLock()
		ctx := a.ctx
		a.mu.RUnlock()
		if ctx != nil {
			runtime.EventsEmit(ctx, name, payload)
		}
	}
	a.manager = masterstate.NewManager(masterstate.Events{Emit: func(name string, payload any) { a.emit(name, payload) }})
	return a
}
func (a *App) startup(ctx context.Context) { a.mu.Lock(); a.ctx = ctx; a.mu.Unlock() }
func (a *App) shutdown(context.Context)    { a.manager.CloseAll() }
func (a *App) GetVersion() string          { return Version }
func (a *App) CreateConnection(s masterstate.Settings) (masterstate.ConnectionInfo, error) {
	return a.manager.CreateConnection(s)
}
func (a *App) UpdateConnection(id string, s masterstate.Settings) error {
	return a.manager.UpdateConnection(id, s)
}
func (a *App) GetSettings(id string) (masterstate.Settings, error) { return a.manager.GetSettings(id) }
func (a *App) ListConnections() []masterstate.ConnectionInfo       { return a.manager.ListConnections() }
func (a *App) Connect(id string) error                             { return a.manager.Connect(id) }
func (a *App) Disconnect(id string) error                          { return a.manager.Disconnect(id) }
func (a *App) DeleteConnection(id string) error                    { return a.manager.DeleteConnection(id) }
func (a *App) AddScanGroup(id string, g masterstate.ScanGroup) (masterstate.ScanGroup, error) {
	return a.manager.AddScanGroup(id, g)
}
func (a *App) UpdateScanGroup(id string, g masterstate.ScanGroup) error {
	return a.manager.UpdateScanGroup(id, g)
}
func (a *App) RemoveScanGroup(id, groupID string) error {
	return a.manager.RemoveScanGroup(id, groupID)
}
func (a *App) ListScanGroups(id string) ([]masterstate.ScanGroup, error) {
	return a.manager.ListScanGroups(id)
}
func (a *App) StartPolling(id, groupID string) error { return a.manager.StartPolling(id, groupID) }
func (a *App) StopPolling(id, groupID string) error  { return a.manager.StopPolling(id, groupID) }
func (a *App) StartAllPolling(id string) error       { return a.manager.StartAllPolling(id) }
func (a *App) StopAllPolling(id string) error        { return a.manager.StopAllPolling(id) }
func (a *App) GetPollData(id, groupID string) (*masterstate.ReadResult, error) {
	return a.manager.GetPollData(id, groupID)
}
func (a *App) ReadOnce(id string, g masterstate.ScanGroup) (masterstate.ReadResult, error) {
	return a.manager.ReadOnce(id, g)
}
func (a *App) WriteSingleRegister(id string, address, value uint16) error {
	return a.manager.WriteSingleRegister(id, address, value)
}
func (a *App) WriteSingleCoil(id string, address uint16, value bool) error {
	return a.manager.WriteSingleCoil(id, address, value)
}
func (a *App) WriteMultipleRegisters(id string, address uint16, values []uint16) error {
	return a.manager.WriteMultipleRegisters(id, address, values)
}
func (a *App) WriteMultipleCoils(id string, address uint16, values []bool) error {
	return a.manager.WriteMultipleCoils(id, address, values)
}
func (a *App) StartSlaveIDScan(id string, req masterstate.SlaveScanRequest) (string, error) {
	return a.manager.StartSlaveIDScan(id, req)
}
func (a *App) StartRegisterScan(id string, req masterstate.RegisterScanRequest) (string, error) {
	return a.manager.StartRegisterScan(id, req)
}
func (a *App) CancelScan(id, kind string) error               { return a.manager.CancelScan(id, kind) }
func (a *App) Logs(id string) ([]masterstate.LogEntry, error) { return a.manager.Logs(id) }
func (a *App) ClearLogs(id string) error                      { return a.manager.ClearLogs(id) }
func (a *App) ExportLogsCSV(id string) (string, error)        { return a.manager.ExportLogsCSV(id) }
func (a *App) LoadProject(path string) (int, error)           { return a.manager.LoadProject(path) }
func (a *App) SaveProject(path string) error                  { return a.manager.SaveProject(path) }
func (a *App) ListSerialPorts() ([]string, error)             { return serial.ListPorts() }
