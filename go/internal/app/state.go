// Application state: connection registry, per-point data sources, and the
// point-mutation tick loop. Mirrors crates/modbussim-app/src/state.rs and
// the mutation/data-source scheduler from mutation.rs / data_source.rs.
package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/mutation"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// MutationBaseTickMs is the base tick of the mutation task
// (state.rs MUTATION_BASE_TICK_MS).
const MutationBaseTickMs = 100

// PointKey identifies one point across connections
// (MutationKey / DataSourceKey in Rust share this shape).
type PointKey struct {
	ConnectionID string
	SlaveID      uint8
	RegisterType register.RegisterType
	Address      uint16
}

// ParsePointKey builds a PointKey from its string form used in keys maps.
func ParsePointKey(connectionID string, slaveID uint8, rt register.RegisterType, addr uint16) PointKey {
	return PointKey{ConnectionID: connectionID, SlaveID: slaveID, RegisterType: rt, Address: addr}
}

func (k PointKey) String() string {
	return fmt.Sprintf("%s/%d/%s/%d", k.ConnectionID, k.SlaveID, k.RegisterType, k.Address)
}

// parseKeyString reverses String() (used for persistence-less runtime maps).
func parseKeyString(s string) (PointKey, error) {
	var k PointKey
	var parts []string
	// split into exactly 4 segments on "/"
	cur := ""
	for _, r := range s {
		if r == '/' {
			parts = append(parts, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	parts = append(parts, cur)
	if len(parts) != 4 {
		return k, fmt.Errorf("invalid key: %s", s)
	}
	id, err := strconv.ParseUint(parts[1], 10, 8)
	if err != nil {
		return k, err
	}
	addr, err := strconv.ParseUint(parts[3], 10, 16)
	if err != nil {
		return k, err
	}
	k.ConnectionID = parts[0]
	k.SlaveID = uint8(id)
	k.RegisterType = register.RegisterType(parts[2])
	k.Address = uint16(addr)
	return k, nil
}

// MutationRuntime is the non-persisted scheduling state for one point
// (MutationRuntimeState in Rust).
type MutationRuntime struct {
	Direction mutation.Direction
	NextDue   time.Time
	Def       register.RegisterDef
	Config    register.MutationConfig
}

// DataSourceRuntime pairs a config with its generator state.
type DataSourceRuntime struct {
	Config  datasource.Config
	State   *datasource.State
	NextDue time.Time
}

// State is the root application state.
type State struct {
	mu         sync.RWMutex
	nextID     uint32
	connection map[string]*Connection

	dataSources map[PointKey]*DataSourceRuntime
	mutationRun map[PointKey]*MutationRuntime

	mutationRunning bool
	tickCancel      context.CancelFunc
}

// NewState creates an empty application state.
func NewState() *State {
	return &State{
		connection:  map[string]*Connection{},
		dataSources: map[PointKey]*DataSourceRuntime{},
		mutationRun: map[PointKey]*MutationRuntime{},
	}
}

// NextConnectionID allocates the next "slave_N" id.
func (s *State) NextConnectionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return fmt.Sprintf("slave_%d", s.nextID)
}

// AddConnection registers a connection under id.
func (s *State) AddConnection(id string, c *Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connection[id] = c
}

// Connection returns the connection with id (nil if absent).
func (s *State) Connection(id string) (*Connection, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.connection[id]
	return c, ok
}

// ConnectionIDs lists registered connection ids (sorted for stable output).
func (s *State) ConnectionIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.connection))
	for id := range s.connection {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// DeleteConnection stops and removes a connection, clearing its points.
func (s *State) DeleteConnection(id string) error {
	s.mu.Lock()
	c, ok := s.connection[id]
	if ok {
		delete(s.connection, id)
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("connection %s not found", id)
	}
	_ = c.Stop()
	s.mu.Lock()
	for k := range s.dataSources {
		if k.ConnectionID == id {
			delete(s.dataSources, k)
		}
	}
	for k := range s.mutationRun {
		if k.ConnectionID == id {
			delete(s.mutationRun, k)
		}
	}
	s.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// Data sources
// ---------------------------------------------------------------------------

// SetDataSource registers (or replaces) a data source for a point.
func (s *State) SetDataSource(key PointKey, cfg datasource.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.connection[key.ConnectionID]
	if !ok {
		return fmt.Errorf("connection %s not found", key.ConnectionID)
	}
	if _, ok := c.Server().GetDevice(key.SlaveID); !ok {
		return fmt.Errorf("device %d not found", key.SlaveID)
	}
	s.dataSources[key] = &DataSourceRuntime{
		Config:  cfg,
		State:   datasource.NewState(cfg),
		NextDue: time.Now().Add(datasourceInterval(cfg)),
	}
	return nil
}

// RemoveDataSource deletes a point's data source.
func (s *State) RemoveDataSource(key PointKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.dataSources, key)
}

// DataSourceKeys lists registered data source keys.
func (s *State) DataSourceKeys() []PointKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PointKey, 0, len(s.dataSources))
	for k := range s.dataSources {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func datasourceInterval(cfg datasource.Config) time.Duration {
	ms := cfg.UpdateIntervalMs
	if ms < datasource.MinPeriodMs {
		ms = datasource.MinPeriodMs
	}
	return time.Duration(ms) * time.Millisecond
}

// ---------------------------------------------------------------------------
// Point mutation
// ---------------------------------------------------------------------------

// MutationPeriod clamps the per-point period to the base tick.
func MutationPeriod(periodMs uint64) time.Duration {
	if periodMs < MutationBaseTickMs {
		periodMs = MutationBaseTickMs
	}
	return time.Duration(periodMs) * time.Millisecond
}

// SetPointMutation enables mutation for one point.
func (s *State) SetPointMutation(key PointKey, cfg register.MutationConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.connection[key.ConnectionID]
	if !ok {
		return fmt.Errorf("connection %s not found", key.ConnectionID)
	}
	dev, ok := c.Server().GetDevice(key.SlaveID)
	if !ok {
		return fmt.Errorf("device %d not found", key.SlaveID)
	}
	// Persist onto the definition.
	var def register.RegisterDef
	found := false
	for i := range dev.RegisterDefs {
		if dev.RegisterDefs[i].Address == key.Address && dev.RegisterDefs[i].RegisterType == key.RegisterType {
			dev.RegisterDefs[i].Mutation = &cfg
			def = dev.RegisterDefs[i]
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("register %s@%d is not defined", key.RegisterType, key.Address)
	}
	s.mutationRun[key] = &MutationRuntime{
		Direction: mutation.InitialFor(mutation.Mode(cfg.Mode)),
		NextDue:   time.Now().Add(MutationPeriod(cfg.PeriodMs)),
		Def:       def,
		Config:    cfg,
	}
	return nil
}

// ClearPointMutation disables mutation for one point.
func (s *State) ClearPointMutation(key PointKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.mutationRun, key)
	if c, ok := s.connection[key.ConnectionID]; ok {
		if dev, ok := c.Server().GetDevice(key.SlaveID); ok {
			for i := range dev.RegisterDefs {
				if dev.RegisterDefs[i].Address == key.Address && dev.RegisterDefs[i].RegisterType == key.RegisterType {
					dev.RegisterDefs[i].Mutation = nil
					break
				}
			}
		}
	}
}

// ListPointMutations returns the keys of enabled mutation points.
func (s *State) ListPointMutations() []PointKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PointKey, 0, len(s.mutationRun))
	for k := range s.mutationRun {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// SetMutationRunning toggles the mutation master switch.
func (s *State) SetMutationRunning(running bool) {
	s.mu.Lock()
	s.mutationRunning = running
	s.mu.Unlock()
}

// MutationRunning reports the master switch state.
func (s *State) MutationRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mutationRunning
}

// MutationTick applies one mutation step to every due point. It is called
// from RunMutationLoop; tests may call it directly.
func (s *State) MutationTick(now time.Time) int {
	s.mu.RLock()
	if !s.mutationRunning {
		s.mu.RUnlock()
		return 0
	}
	// Snapshot due points to avoid holding the lock through register writes.
	type duePoint struct {
		key  PointKey
		rt   *MutationRuntime
		conn *Connection
	}
	var due []duePoint
	for k, rt := range s.mutationRun {
		if now.Before(rt.NextDue) {
			continue
		}
		if c, ok := s.connection[k.ConnectionID]; ok {
			due = append(due, duePoint{key: k, rt: rt, conn: c})
		}
	}
	s.mu.RUnlock()

	applied := 0
	for _, dp := range due {
		dev, ok := dp.conn.Server().GetDevice(dp.key.SlaveID)
		if !ok {
			continue
		}
		dp.rt.Direction = mutation.ApplyPointMutation(
			dev.RegisterMap, dp.rt.Def, dp.rt.Config, dp.rt.Direction,
		)
		dp.rt.NextDue = now.Add(MutationPeriod(dp.rt.Config.PeriodMs))
		applied++
	}
	return applied
}

// StartMutationLoop runs the 100 ms mutation tick until ctx is cancelled.
func (s *State) RunMutationTickLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(MutationBaseTickMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.MutationTick(now)
		}
	}
}

// StartMutationLoop launches the tick loop in a goroutine.
func (s *State) StartMutationLoop(ctx context.Context) {
	go s.RunMutationTickLoop(ctx)
}

// parseKeyString is referenced by tests; keep the import honest.
var _ = parseKeyString
