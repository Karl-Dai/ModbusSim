package masterstate

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Karl-Dai/ModbusSim/go/internal/logcol"
	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"strconv"
	"sync"
	"sync/atomic"
)

type Manager struct {
	mu          sync.Mutex
	workspace   sync.Mutex // serializes registry mutations, never held by data reads
	connections map[string]*connection
	order       []string
	nextID      uint64
	sequence    atomic.Uint64
	closed      bool
	replacing   bool
	events      Events
}
type task struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	id     string
}
type connection struct {
	stopOp         sync.Mutex
	stopping       bool
	id             string
	owner          *Manager
	op             sync.Mutex
	mu             sync.Mutex
	emitMu         sync.Mutex
	settings       Settings
	engine         *master.Connection
	logs           *logcol.Collector
	groups         []ScanGroup
	cache          map[string]ReadResult
	polls          map[string]*task
	scans          map[string]*task
	state          string
	dead           bool
	want           bool
	life           context.Context
	cancelLife     context.CancelFunc
	supervisorDone chan struct{}
}

func NewManager(events Events) *Manager {
	return &Manager{connections: map[string]*connection{}, nextID: 1, events: events}
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func (m *Manager) get(id string) (*connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.replacing {
		return nil, fmt.Errorf("workspace is closed or being replaced")
	}
	c := m.connections[id]
	if c == nil {
		return nil, fmt.Errorf("connection not found: %s", id)
	}
	return c, nil
}
func (m *Manager) newConnection(id string, s Settings) *connection {
	c := &connection{id: id, owner: m, settings: clone(s), logs: logcol.NewCollector(), cache: map[string]ReadResult{}, polls: map[string]*task{}, scans: map[string]*task{}, state: "Disconnected"}
	c.engine = c.settings.engine()
	c.engine.SetLogSink(logSink{c.logs})
	c.logs.SetAppendCallback(func(logcol.Entry) { c.publish("log-appended", LogEvent{c.id}, nil) })
	return c
}

type logSink struct{ collector *logcol.Collector }

func (s logSink) AddRequest(direction string, fc uint8, detail string) {
	s.collector.TryAdd(logcol.NewEntry(logcol.Direction(direction), logcol.FunctionCode(fc), detail))
}
func (c *connection) publish(name string, payload any, valid func() bool) {
	c.emitMu.Lock()
	defer c.emitMu.Unlock()
	c.mu.Lock()
	ok := !c.dead
	if ok && valid != nil {
		ok = valid()
	}
	c.mu.Unlock()
	if ok && c.owner.events.Emit != nil {
		c.owner.events.Emit(name, payload)
	}
}
func (c *connection) setState(state string) {
	c.mu.Lock()
	c.state = state
	c.mu.Unlock()
	c.publish("master-connection-state", ConnectionStateEvent{c.id, state}, func() bool { return c.state == state })
}
func (c *connection) info() ConnectionInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	target := c.settings.Transport.Host
	if sc := c.settings.Transport.Serial; sc != nil {
		target = sc.Port
	}
	return ConnectionInfo{c.id, target, c.settings.Transport.Port, c.settings.SlaveID, c.state, len(c.groups)}
}
func (m *Manager) CreateConnection(s Settings) (ConnectionInfo, error) {
	if err := s.validate(); err != nil {
		return ConnectionInfo{}, err
	}
	m.workspace.Lock()
	defer m.workspace.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.replacing {
		return ConnectionInfo{}, fmt.Errorf("workspace unavailable")
	}
	id := "master_" + strconv.FormatUint(m.nextID, 10)
	m.nextID++
	c := m.newConnection(id, s)
	m.connections[id] = c
	m.order = append(m.order, id)
	return c.info(), nil
}
func (m *Manager) ListConnections() []ConnectionInfo {
	m.mu.Lock()
	list := make([]*connection, 0, len(m.order))
	for _, id := range m.order {
		if c := m.connections[id]; c != nil {
			list = append(list, c)
		}
	}
	m.mu.Unlock()
	out := make([]ConnectionInfo, 0, len(list))
	for _, c := range list {
		out = append(out, c.info())
	}
	return out
}
func (m *Manager) GetSettings(id string) (Settings, error) {
	c, err := m.get(id)
	if err != nil {
		return Settings{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return clone(c.settings), nil
}
func (m *Manager) UpdateConnection(id string, s Settings) error {
	if err := s.validate(); err != nil {
		return err
	}
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead || c.want || c.state == "Connected" || c.state == "Reconnecting" || len(c.polls) > 0 || len(c.scans) > 0 {
		return fmt.Errorf("disconnect before changing connection settings")
	}
	c.settings = clone(s)
	c.engine = c.settings.engine()
	c.engine.SetLogSink(logSink{c.logs})
	return nil
}
func (m *Manager) Connect(id string) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	if c.dead || c.want || c.stopping {
		c.mu.Unlock()
		return fmt.Errorf("connection is already active or removed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.life = ctx
	c.cancelLife = cancel
	c.want = true
	lost := make(chan struct{}, 1)
	c.engine.SetConnectionLostCallback(func() {
		select {
		case lost <- struct{}{}:
		default:
		}
	})
	c.mu.Unlock()
	if err = c.engine.Connect(ctx); err != nil {
		cancel()
		c.mu.Lock()
		c.want = false
		c.mu.Unlock()
		return err
	}
	if ctx.Err() != nil {
		c.engine.Disconnect()
		return ctx.Err()
	}
	c.mu.Lock()
	done := make(chan struct{})
	c.supervisorDone = done
	c.mu.Unlock()
	c.setState("Connected")
	go c.supervise(ctx, done, lost)
	return nil
}
func (c *connection) cancel() {
	c.mu.Lock()
	c.want = false
	if c.cancelLife != nil {
		c.cancelLife()
	}
	for _, t := range c.polls {
		t.cancel()
	}
	for _, t := range c.scans {
		t.cancel()
	}
	c.mu.Unlock()
}
func (c *connection) disconnect(remove bool) {
	c.stopOp.Lock()
	defer c.stopOp.Unlock()
	c.mu.Lock()
	c.stopping = true
	c.mu.Unlock()
	c.cancel()
	c.op.Lock()
	c.mu.Lock()
	if remove {
		c.dead = true
	}
	c.mu.Unlock()
	c.engine.Disconnect()
	c.mu.Lock()
	wait := []chan struct{}{}
	for _, t := range c.polls {
		wait = append(wait, t.done)
	}
	for _, t := range c.scans {
		wait = append(wait, t.done)
	}
	if c.supervisorDone != nil {
		wait = append(wait, c.supervisorDone)
	}
	c.mu.Unlock()
	c.op.Unlock()
	for _, done := range wait {
		<-done
	}
	c.op.Lock()
	c.mu.Lock()
	c.polls = map[string]*task{}
	c.scans = map[string]*task{}
	c.supervisorDone = nil
	c.state = "Disconnected"
	c.stopping = false
	c.mu.Unlock()
	c.op.Unlock()
	c.publish("master-connection-state", ConnectionStateEvent{c.id, "Disconnected"}, func() bool { return c.state == "Disconnected" })
	c.emitMu.Lock()
	c.emitMu.Unlock()
}

func (m *Manager) Disconnect(id string) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.disconnect(false)
	return nil
}
func (m *Manager) DeleteConnection(id string) error {
	m.workspace.Lock()
	defer m.workspace.Unlock()
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.disconnect(true)
	m.mu.Lock()
	delete(m.connections, id)
	for i, v := range m.order {
		if v == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	return nil
}
func (m *Manager) CloseAll() {
	m.workspace.Lock()
	defer m.workspace.Unlock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	list := make([]*connection, 0, len(m.connections))
	for _, c := range m.connections {
		list = append(list, c)
	}
	m.mu.Unlock()
	for _, c := range list {
		c.cancel()
	}
	for _, c := range list {
		c.disconnect(true)
	}
}
