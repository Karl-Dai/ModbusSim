// Project-file persistence (save_project_file / load_project_file) and the
// data-source tick loop, mirroring crates/modbussim-app/src/commands.rs.
package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/config"
	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/project"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// SaveProjectFile writes all slave connections to a .modbusproj file
// (commands.rs save_project_file).
func (s *State) SaveProjectFile(path string) error {
	s.mu.RLock()
	ids := make([]string, 0, len(s.connection))
	for id := range s.connection {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	sort.Strings(ids)

	proj := project.NewSlave()
	for _, id := range ids {
		conn, _ := s.Connection(id)
		t := conn.Transport()
		name, tr := transportToProject(t, conn.tlsCfg)
		devs := make([]project.DeviceConfig, 0, conn.DeviceCount())
		for _, slaveID := range conn.ListDevices() {
			d, _ := conn.Server().GetDevice(slaveID)
			devs = append(devs, project.DeviceConfig{
				SlaveID:      d.SlaveID,
				Name:         d.Name,
				RegisterDefs: d.RegisterDefs,
				Values:       config.ValuesFromRegisterMap(d.RegisterMap),
			})
		}
		proj.Connections = append(proj.Connections, project.ConnectionConfig{
			ID:              id,
			Name:            name,
			Transport:       tr,
			Devices:         devs,
			DefaultSlaveID:  1,
			TimeoutMs:       3000,
			Requests:        project.DefaultRequestSettings(),
			ReconnectPolicy: project.DefaultReconnectPolicy(),
		})
	}
	return project.SaveProject(&proj, path)
}

func transportToProject(t TransportConfig, tlsCfg SlaveTLSConfig) (string, project.TransportConfig) {
	switch t.Type {
	case "tcp":
		return fmt.Sprintf("%s:%d", t.Host, t.Port), project.NewTCP(t.Host, t.Port)
	case "rtu_over_tcp":
		name := fmt.Sprintf("rtu-tcp://%s:%d", t.Host, t.Port)
		tr := project.TransportConfig{Type: "rtu_over_tcp", Host: t.Host, Port: t.Port}
		return name, tr
	case "tcp_tls":
		name := fmt.Sprintf("tls://%s:%d", t.Host, t.Port)
		tr := project.TransportConfig{Type: "tcp_tls", Host: t.Host, Port: t.Port, ServerTLS: projectServerTLS(tlsCfg)}
		return name, tr
	case "rtu":
		name := fmt.Sprintf("rtu://%s", t.PortName)
		return name, project.NewSerial(false, t.PortName, t.BaudRate, t.DataBits, t.StopBits, t.Parity)
	case "ascii":
		name := fmt.Sprintf("ascii://%s", t.PortName)
		return name, project.NewSerial(true, t.PortName, t.BaudRate, t.DataBits, t.StopBits, t.Parity)
	}
	return t.Type, project.NewTCP(t.Host, t.Port)
}

func projectServerTLS(cfg SlaveTLSConfig) *project.SlaveTLSConfig {
	return &project.SlaveTLSConfig{
		Enabled:           cfg.Enabled,
		CertFile:          cfg.CertFile,
		KeyFile:           cfg.KeyFile,
		CAFile:            cfg.CAFile,
		RequireClientCert: cfg.RequireClientCert,
		PKCS12File:        cfg.PKCS12File,
		PKCS12Password:    cfg.PKCS12Password,
	}
}

// firstDeviceID returns the lowest registered slave ID (or 0).
func (c *Connection) firstDeviceID() uint8 {
	ids := c.ListDevices()
	if len(ids) == 0 {
		return 0
	}
	return ids[0]
}

// LoadProjectFile replaces the connection set with the project's contents
// (commands.rs load_project_file). Returns the number of devices loaded.
func (s *State) LoadProjectFile(path string) (int, error) {
	proj, err := project.LoadProject(path)
	if err != nil {
		return 0, err
	}
	if proj.Type != project.TypeSlave {
		return 0, fmt.Errorf("project is not a slave project")
	}

	loaded := map[string]*Connection{}
	loadedMutation := map[PointKey]*MutationRuntime{}
	loadedSources := map[PointKey]*DataSourceRuntime{}
	totalDevices := 0

	for _, cc := range proj.Connections {
		if _, exists := loaded[cc.ID]; exists {
			return 0, fmt.Errorf("duplicate connection id %s", cc.ID)
		}
		transport, tlsCfg := transportFromProject(cc.Transport)
		conn := NewConnection(transport, tlsCfg)
		for _, dc := range cc.Devices {
			device := newDeviceWithDefs(dc)
			defs := device.RegisterDefs
			if err := register.ValidateDefinitions(defs); err != nil {
				return 0, err
			}
			for _, def := range defs {
				if def.Mutation != nil && def.Mutation.Enabled && def.DataSource != nil {
					return 0, fmt.Errorf("register %s@%d cannot use mutation and a data source simultaneously",
						def.RegisterType, def.Address)
				}
				if def.DataSource != nil {
					if err := def.DataSource.Validate(); err != nil {
						return 0, err
					}
					key := PointKey{ConnectionID: cc.ID, SlaveID: dc.SlaveID, RegisterType: def.RegisterType, Address: def.Address}
					loadedSources[key] = &DataSourceRuntime{
						Config:  *def.DataSource,
						State:   datasource.NewState(*def.DataSource),
						NextDue: time.Now().Add(datasourceInterval(*def.DataSource)),
					}
				}
				if def.Mutation != nil && def.Mutation.Enabled {
					key := PointKey{ConnectionID: cc.ID, SlaveID: dc.SlaveID, RegisterType: def.RegisterType, Address: def.Address}
					loadedMutation[key] = &MutationRuntime{
						Direction: mutationInitialFor(def.Mutation.Mode),
						NextDue:   time.Now().Add(MutationPeriod(def.Mutation.PeriodMs)),
						Def:       def,
						Config:    *def.Mutation,
					}
				}
				device.RegisterMap.EnsureFromDef(def)
			}
			dc.Values.ApplyToExisting(device.RegisterMap)
			if err := conn.Server().AddDevice(device); err != nil {
				return 0, fmt.Errorf("failed to load device: %w", err)
			}
			totalDevices++
		}
		loaded[cc.ID] = conn
	}

	// Stop and replace current connections.
	s.mu.RLock()
	old := make([]*Connection, 0, len(s.connection))
	for _, c := range s.connection {
		old = append(old, c)
	}
	s.mu.RUnlock()
	for _, c := range old {
		_ = c.Stop()
	}

	s.mu.Lock()
	s.connection = map[string]*Connection{}
	for id, c := range loaded {
		s.connection[id] = c
	}
	nextID := uint32(0)
	for id := range s.connection {
		if n, ok := strings.CutPrefix(id, "slave_"); ok {
			var v uint32
			if _, err := fmt.Sscanf(n, "%d", &v); err == nil && v > nextID {
				nextID = v
			}
		}
	}
	s.nextID = nextID
	s.dataSources = loadedSources
	s.mutationRun = loadedMutation
	s.mutationRunning = false
	s.mu.Unlock()
	return totalDevices, nil
}

func newDeviceWithDefs(dc project.DeviceConfig) *slave.Device {
	return &slave.Device{
		SlaveID:      dc.SlaveID,
		Name:         dc.Name,
		RegisterMap:  register.NewRegisterMap(),
		RegisterDefs: dc.RegisterDefs,
	}
}

func mutationInitialFor(mode register.MutationMode) mutation.Direction {
	return mutation.InitialFor(mutation.Mode(mode))
}

func transportFromProject(tr project.TransportConfig) (TransportConfig, SlaveTLSConfig) {
	switch tr.Type {
	case "tcp_tls":
		tlsCfg := SlaveTLSConfig{}
		if tr.ServerTLS != nil {
			tlsCfg = SlaveTLSConfig(*tr.ServerTLS)
		}
		return TransportConfig{Type: "tcp_tls", Host: tr.Host, Port: tr.Port}, tlsCfg
	case "rtu_over_tcp":
		return TransportConfig{Type: "rtu_over_tcp", Host: tr.Host, Port: tr.Port}, SlaveTLSConfig{}
	case "rtu":
		return TransportConfig{Type: "rtu", PortName: tr.PortName, BaudRate: tr.BaudRate, DataBits: tr.DataBits, StopBits: tr.StopBits, Parity: tr.Parity}, SlaveTLSConfig{}
	case "ascii":
		return TransportConfig{Type: "ascii", PortName: tr.PortName, BaudRate: tr.BaudRate, DataBits: tr.DataBits, StopBits: tr.StopBits, Parity: tr.Parity}, SlaveTLSConfig{}
	}
	return TransportConfig{Type: "tcp", Host: tr.Host, Port: tr.Port}, SlaveTLSConfig{}
}

// ---------------------------------------------------------------------------
// Data source tick loop
// ---------------------------------------------------------------------------

// DataSourceTick advances every due data source and writes the new value
// into the point's register map. Returns the number of updates applied.
func (s *State) DataSourceTick(now time.Time) int {
	s.mu.RLock()
	type dueSource struct {
		key PointKey
		rt  *DataSourceRuntime
	}
	var due []dueSource
	for k, ds := range s.dataSources {
		if now.Before(ds.NextDue) {
			continue
		}
		due = append(due, dueSource{key: k, rt: ds})
	}
	s.mu.RUnlock()

	applied := 0
	for _, d := range due {
		conn, ok := s.Connection(d.key.ConnectionID)
		if !ok {
			continue
		}
		if _, ok := conn.Server().GetDevice(d.key.SlaveID); !ok {
			continue
		}
		value := d.rt.State.NextValue()
		_ = conn.WriteRegisterValue(d.key.SlaveID, d.key.RegisterType, d.key.Address, value)
		d.rt.NextDue = now.Add(datasourceInterval(d.rt.Config))
		applied++
	}
	return applied
}

// RunDataSourceTickLoop ticks data sources every 100 ms until ctx ends.
func (s *State) RunDataSourceTickLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(MutationBaseTickMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.DataSourceTick(now)
		}
	}
}

// StartDataSourceLoop launches the data-source tick loop in a goroutine.
func (s *State) StartDataSourceLoop(ctx context.Context) {
	go s.RunDataSourceTickLoop(ctx)
}
