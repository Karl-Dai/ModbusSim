// Package api exposes the app layer over HTTP JSON endpoints, mirroring the
// core Tauri commands of crates/modbussim-app. Served by cmd/server together
// with the static React frontend.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/Karl-Dai/ModbusSim/go/internal/app"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// Server mounts the API routes on an http.ServeMux.
type Server struct {
	state *app.State

	mu        sync.Mutex // serializes project save/load and connection creation
	nextCxnID int
}

// NewServer builds the API routes.
func NewServer(state *app.State) *Server {
	s := &Server{state: state}
	return s
}

// Register mounts all API routes on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/connections", s.listConnections)
	mux.HandleFunc("POST /api/connections", s.createConnection)
	mux.HandleFunc("POST /api/connections/{id}/start", s.startConnection)
	mux.HandleFunc("POST /api/connections/{id}/stop", s.stopConnection)
	mux.HandleFunc("DELETE /api/connections/{id}", s.deleteConnection)

	mux.HandleFunc("GET /api/connections/{id}/devices", s.listDevices)
	mux.HandleFunc("POST /api/connections/{id}/devices", s.addDevice)
	mux.HandleFunc("DELETE /api/connections/{id}/devices/{slaveId}", s.removeDevice)

	mux.HandleFunc("GET /api/connections/{id}/devices/{slaveId}/registers", s.listRegisters)
	mux.HandleFunc("POST /api/connections/{id}/devices/{slaveId}/registers", s.addRegister)
	mux.HandleFunc("PUT /api/connections/{id}/devices/{slaveId}/registers", s.updateRegister)
	mux.HandleFunc("DELETE /api/connections/{id}/devices/{slaveId}/registers/{address}/{type}", s.removeRegister)

	mux.HandleFunc("POST /api/connections/{id}/devices/{slaveId}/read", s.readRegister)
	mux.HandleFunc("POST /api/connections/{id}/devices/{slaveId}/write", s.writeRegister)

	mux.HandleFunc("GET /api/connections/{id}/logs", s.getLogs)
	mux.HandleFunc("POST /api/connections/{id}/logs/clear", s.clearLogs)
	mux.HandleFunc("GET /api/connections/{id}/logs/export", s.exportLogs)

	mux.HandleFunc("POST /api/points/mutation", s.setMutation)
	mux.HandleFunc("DELETE /api/points/mutation/{connectionId}/{slaveId}/{type}/{address}", s.clearMutation)
	mux.HandleFunc("GET /api/points/mutation", s.listMutations)
	mux.HandleFunc("POST /api/mutation/running", s.setMutationRunning)

	mux.HandleFunc("POST /api/points/datasource", s.setDataSource)
	mux.HandleFunc("DELETE /api/points/datasource/{connectionId}/{slaveId}/{type}/{address}", s.removeDataSource)
	mux.HandleFunc("GET /api/points/datasource", s.listDataSources)

	mux.HandleFunc("POST /api/tools/crc16", s.toolCRC16)
	mux.HandleFunc("POST /api/tools/lrc", s.toolLRC)
	mux.HandleFunc("POST /api/tools/parse-hex", s.toolParseHex)
	mux.HandleFunc("GET /api/tools/plc-to-modbus", s.plcToModbus)
	mux.HandleFunc("GET /api/tools/modbus-to-plc", s.modbusToPlc)

	mux.HandleFunc("POST /api/project/save", s.saveProject)
	mux.HandleFunc("POST /api/project/load", s.loadProject)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func pathUint(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(r.PathValue(name), 10, 32)
}

// ---------------------------------------------------------------------------
// connections
// ---------------------------------------------------------------------------

type transportDTO struct {
	Type     string `json:"type"`
	Host     string `json:"host,omitempty"`
	Port     uint16 `json:"port,omitempty"`
	PortName string `json:"port_name,omitempty"`
	BaudRate uint32 `json:"baud_rate,omitempty"`
	DataBits uint8  `json:"data_bits,omitempty"`
	StopBits uint8  `json:"stop_bits,omitempty"`
	Parity   string `json:"parity,omitempty"`
}

type connectionInfo struct {
	ID          string `json:"id"`
	BindAddress string `json:"bind_address"`
	Port        uint16 `json:"port"`
	State       string `json:"state"`
	DeviceCount int    `json:"device_count"`
}

func (s *Server) listConnections(w http.ResponseWriter, _ *http.Request) {
	infos := []connectionInfo{}
	for _, id := range s.state.ConnectionIDs() {
		conn, _ := s.state.Connection(id)
		host, port := conn.BindAddress()
		infos = append(infos, connectionInfo{
			ID: id, BindAddress: host, Port: port,
			State: string(conn.State()), DeviceCount: conn.DeviceCount(),
		})
	}
	writeJSON(w, http.StatusOK, infos)
}

type createConnectionRequest struct {
	SlaveID   uint8              `json:"slave_id"`
	Name      string             `json:"name"`
	Transport transportDTO       `json:"transport"`
	UseTLS    bool               `json:"use_tls"`
	ServerTLS app.SlaveTLSConfig `json:"server_tls"`
}

func (s *Server) createConnection(w http.ResponseWriter, r *http.Request) {
	var req createConnectionRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.SlaveID == 0 {
		req.SlaveID = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.state.NextConnectionID()
	conn := app.NewConnection(transportFromDTO(req.Transport), req.ServerTLS)
	if err := conn.AddDevice(req.SlaveID, req.Name, ""); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.state.AddConnection(id, conn)
	host, port := conn.BindAddress()
	writeJSON(w, http.StatusOK, connectionInfo{
		ID: id, BindAddress: host, Port: port,
		State: string(conn.State()), DeviceCount: conn.DeviceCount(),
	})
}

func transportFromDTO(t transportDTO) app.TransportConfig {
	return app.TransportConfig{
		Type: t.Type, Host: t.Host, Port: t.Port,
		PortName: t.PortName, BaudRate: t.BaudRate,
		DataBits: t.DataBits, StopBits: t.StopBits, Parity: t.Parity,
	}
}

func (s *Server) startConnection(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.state.Connection(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("connection not found"))
		return
	}
	if err := conn.Start(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": string(conn.State())})
}

func (s *Server) stopConnection(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.state.Connection(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("connection not found"))
		return
	}
	if err := conn.Stop(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": string(conn.State())})
}

func (s *Server) deleteConnection(w http.ResponseWriter, r *http.Request) {
	if err := s.state.DeleteConnection(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// ---------------------------------------------------------------------------
// devices
// ---------------------------------------------------------------------------

type addDeviceRequest struct {
	SlaveID uint8  `json:"slave_id"`
	Name    string `json:"name"`
	MaxAddr uint16 `json:"max_addr,omitempty"`
}

func (s *Server) addDevice(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.state.Connection(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("connection not found"))
		return
	}
	var req addDeviceRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := conn.AddDevice(req.SlaveID, req.Name, ""); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) removeDevice(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.state.Connection(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("connection not found"))
		return
	}
	id, err := strconv.ParseUint(r.PathValue("slaveId"), 10, 8)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := conn.RemoveDevice(uint8(id)); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.state.Connection(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("connection not found"))
		return
	}
	type devInfo struct {
		SlaveID uint8  `json:"slave_id"`
		Name    string `json:"name"`
	}
	infos := []devInfo{}
	for _, id := range conn.ListDevices() {
		if d, ok := conn.Server().GetDevice(id); ok {
			infos = append(infos, devInfo{SlaveID: d.SlaveID, Name: d.Name})
		}
	}
	writeJSON(w, http.StatusOK, infos)
}

// ---------------------------------------------------------------------------
// registers
// ---------------------------------------------------------------------------

func (s *Server) listRegisters(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	slaveID, err := strconv.ParseUint(r.PathValue("slaveId"), 10, 8)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	defs, err := s.state.ListRegisters(id, uint8(slaveID))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, defs)
}

type registerOpRequest struct {
	app.AddRegisterRequest
	OriginalAddress uint16 `json:"original_address,omitempty"`
	OriginalType    string `json:"original_register_type,omitempty"`
}

func (s *Server) addRegister(w http.ResponseWriter, r *http.Request) {
	// The connection comes from the body's connection_id; keep parity with
	// the Tauri command shape.
	var req app.AddRegisterRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.state.AddRegister(req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) updateRegister(w http.ResponseWriter, r *http.Request) {
	var req registerOpRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.state.UpdateRegister(req.AddRegisterRequest, req.OriginalAddress, req.OriginalType); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) removeRegister(w http.ResponseWriter, r *http.Request) {
	addr, err := strconv.ParseUint(r.PathValue("address"), 10, 16)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.state.RemoveRegister(r.PathValue("id"), uint8(mustSlaveID(r)), uint16(addr), r.PathValue("type")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

type readRequest struct {
	SlaveID uint8  `json:"slave_id"`
	Type    string `json:"register_type"`
	Address uint16 `json:"address"`
	Count   uint16 `json:"count"`
}

type readResponse struct {
	Values []uint16 `json:"values"`
}

func (s *Server) readRegister(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.state.Connection(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("connection not found"))
		return
	}
	var req readRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rt, err := ParseRegisterTypeString(req.Type)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	values := make([]uint16, 0, req.Count)
	for i := uint16(0); i < req.Count; i++ {
		v, ok := conn.ReadRegisterValue(req.SlaveID, rt, req.Address+i)
		if !ok {
			writeErr(w, http.StatusNotFound, fmt.Errorf("register %s@%d not found", req.Type, req.Address+i))
			return
		}
		values = append(values, v)
	}
	writeJSON(w, http.StatusOK, readResponse{Values: values})
}

type writeRequest struct {
	SlaveID uint8  `json:"slave_id"`
	Type    string `json:"register_type"`
	Address uint16 `json:"address"`
	Value   uint16 `json:"value"`
}

func (s *Server) writeRegister(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req writeRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.state.WriteRegister(id, req.SlaveID, register.RegisterType(req.Type), req.Address, req.Value); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// ---------------------------------------------------------------------------
// logs
// ---------------------------------------------------------------------------

func (s *Server) getLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	entries, err := s.state.GetLogs(r.PathValue("id"), offset, limit)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) clearLogs(w http.ResponseWriter, r *http.Request) {
	if err := s.state.ClearLogs(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) exportLogs(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	var (
		content string
		err     error
	)
	if format == "text" {
		content, err = s.state.ExportLogsText(r.PathValue("id"))
	} else {
		content, err = s.state.ExportLogsCSV(r.PathValue("id"))
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(content))
}

// ---------------------------------------------------------------------------
// mutation & data sources
// ---------------------------------------------------------------------------

type mutationRequest struct {
	ConnectionID string                  `json:"connection_id"`
	SlaveID      uint8                   `json:"slave_id"`
	RegisterType string                  `json:"register_type"`
	Address      uint16                  `json:"address"`
	Config       register.MutationConfig `json:"config"`
}

func (s *Server) setMutation(w http.ResponseWriter, r *http.Request) {
	var req mutationRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	key := app.ParsePointKey(req.ConnectionID, req.SlaveID, register.RegisterType(req.RegisterType), req.Address)
	if err := s.state.SetPointMutation(key, req.Config); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) clearMutation(w http.ResponseWriter, r *http.Request) {
	key, err := pointKeyFromPath(r.PathValue("connectionId"), r.PathValue("slaveId"), r.PathValue("type"), r.PathValue("address"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.state.ClearPointMutation(key)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

type keyInfo struct {
	ConnectionID string `json:"connection_id"`
	SlaveID      uint8  `json:"slave_id"`
	RegisterType string `json:"register_type"`
	Address      uint16 `json:"address"`
}

func (s *Server) listMutations(w http.ResponseWriter, _ *http.Request) {
	out := []keyInfo{}
	for _, k := range s.state.ListPointMutations() {
		out = append(out, keyInfo{ConnectionID: k.ConnectionID, SlaveID: k.SlaveID, RegisterType: string(k.RegisterType), Address: k.Address})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setMutationRunning(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Running bool `json:"running"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.state.SetMutationRunning(req.Running)
	writeJSON(w, http.StatusOK, map[string]bool{"running": s.state.MutationRunning()})
}

type dataSourceRequest struct {
	ConnectionID string           `json:"connection_id"`
	SlaveID      uint8            `json:"slave_id"`
	RegisterType string           `json:"register_type"`
	Address      uint16           `json:"address"`
	Config       datasourceConfig `json:"config"`
}

func (s *Server) setDataSource(w http.ResponseWriter, r *http.Request) {
	var req dataSourceRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	key := app.ParsePointKey(req.ConnectionID, req.SlaveID, register.RegisterType(req.RegisterType), req.Address)
	if err := s.state.SetDataSource(key, datasourceConfig(req.Config)); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) removeDataSource(w http.ResponseWriter, r *http.Request) {
	key, err := pointKeyFromPath(r.PathValue("connectionId"), r.PathValue("slaveId"), r.PathValue("type"), r.PathValue("address"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.state.RemoveDataSource(key)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) listDataSources(w http.ResponseWriter, _ *http.Request) {
	out := []keyInfo{}
	for _, k := range s.state.DataSourceKeys() {
		out = append(out, keyInfo{ConnectionID: k.ConnectionID, SlaveID: k.SlaveID, RegisterType: string(k.RegisterType), Address: k.Address})
	}
	writeJSON(w, http.StatusOK, out)
}

func pointKeyFromPath(connID, slaveID, rt, address string) (app.PointKey, error) {
	id, err := strconv.ParseUint(slaveID, 10, 8)
	if err != nil {
		return app.PointKey{}, err
	}
	addr, err := strconv.ParseUint(address, 10, 16)
	if err != nil {
		return app.PointKey{}, err
	}
	return app.ParsePointKey(connID, uint8(id), register.RegisterType(rt), uint16(addr)), nil
}

// ---------------------------------------------------------------------------
// tools
// ---------------------------------------------------------------------------

func (s *Server) toolCRC16(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Data string `json:"data"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	crc, err := app.CalculateCRC16(req.Data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"crc16": crc})
}

func (s *Server) toolLRC(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Data string `json:"data"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	lrc, err := app.CalculateLRC(req.Data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"lrc": lrc})
}

func (s *Server) toolParseHex(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Data string `json:"data"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	bytes, err := app.ParseHex(req.Data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]byte{"bytes": bytes})
}

func (s *Server) plcToModbus(w http.ResponseWriter, r *http.Request) {
	plc, err := strconv.ParseUint(r.URL.Query().Get("address"), 10, 32)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rt, addr, err := app.ConvertPlcToModbus(uint32(plc))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"register_type": string(rt), "address": addr})
}

func (s *Server) modbusToPlc(w http.ResponseWriter, r *http.Request) {
	addr, err := strconv.ParseUint(r.URL.Query().Get("address"), 10, 16)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	plc, err := app.ConvertModbusToPlc(uint16(addr), r.URL.Query().Get("type"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]uint32{"plc_address": plc})
}

// ---------------------------------------------------------------------------
// project
// ---------------------------------------------------------------------------

func (s *Server) saveProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.state.SaveProjectFile(req.Path); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) loadProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	n, err := s.state.LoadProjectFile(req.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"devices": n})
}

func transportDTOFromApp(t app.TransportConfig) transportDTO {
	return transportDTO{
		Type: t.Type, Host: t.Host, Port: t.Port,
		PortName: t.PortName, BaudRate: t.BaudRate,
		DataBits: t.DataBits, StopBits: t.StopBits, Parity: t.Parity,
	}
}

var _ = strings.TrimSpace
