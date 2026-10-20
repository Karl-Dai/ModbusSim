package masterstate

import (
	"context"
	"fmt"
	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"strconv"
	"time"
)

func (c *connection) findGroup(id string) (int, error) {
	for i, g := range c.groups {
		if g.ID == id {
			return i, nil
		}
	}
	return -1, fmt.Errorf("scan group not found: %s", id)
}
func (m *Manager) AddScanGroup(id string, g ScanGroup) (ScanGroup, error) {
	if _, err := g.engine(); err != nil {
		return ScanGroup{}, err
	}
	c, err := m.get(id)
	if err != nil {
		return ScanGroup{}, err
	}
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead || c.stopping {
		return ScanGroup{}, fmt.Errorf("connection removed or stopping")
	}
	g = clone(g)
	g.ID = "group_" + strconv.FormatUint(m.sequence.Add(1), 10)
	g.IsPolling = false
	c.groups = append(c.groups, g)
	return clone(g), nil
}
func (m *Manager) ListScanGroups(id string) ([]ScanGroup, error) {
	c, err := m.get(id)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ScanGroup, 0, len(c.groups))
	for _, g := range c.groups {
		g.IsPolling = c.polls[g.ID] != nil
		out = append(out, clone(g))
	}
	return out, nil
}
func (m *Manager) UpdateScanGroup(id string, g ScanGroup) error {
	if _, err := g.engine(); err != nil {
		return err
	}
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	i, err := c.findGroup(g.ID)
	if err != nil || c.dead {
		c.mu.Unlock()
		return fmt.Errorf("scan group unavailable")
	}
	running := c.polls[g.ID] != nil
	c.mu.Unlock()
	c.stopPoll(g.ID)
	c.mu.Lock()
	c.groups[i] = clone(g)
	delete(c.cache, g.ID)
	c.mu.Unlock()
	if running && g.Enabled {
		return c.startPoll(g.ID)
	}
	return nil
}
func (m *Manager) RemoveScanGroup(id, groupID string) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.op.Lock()
	defer c.op.Unlock()
	c.stopPoll(groupID)
	c.mu.Lock()
	defer c.mu.Unlock()
	i, err := c.findGroup(groupID)
	if err != nil {
		return err
	}
	c.groups = append(c.groups[:i], c.groups[i+1:]...)
	delete(c.cache, groupID)
	return nil
}
func (c *connection) startPoll(id string) error {
	c.mu.Lock()
	i, err := c.findGroup(id)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if c.dead || c.stopping || !c.want || c.state != "Connected" {
		c.mu.Unlock()
		return fmt.Errorf("connection is not connected")
	}
	g := clone(c.groups[i])
	life := c.life
	c.mu.Unlock()
	c.stopPoll(id)
	eg, err := g.engine()
	if err != nil {
		return err
	}
	if life.Err() != nil {
		return life.Err()
	}
	events, err := c.engine.StartScanGroup(eg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(life)
	t := &task{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	c.mu.Lock()
	c.polls[id] = t
	c.mu.Unlock()
	go func() {
		defer close(t.done)
		defer func() {
			c.mu.Lock()
			if c.polls[id] == t {
				delete(c.polls, id)
			}
			c.mu.Unlock()
		}()
		for e := range events {
			if ctx.Err() != nil {
				continue
			}
			if e.Err != nil {
				msg := e.Err.Error()
				c.mu.Lock()
				if c.polls[id] == t {
					r, ok := c.cache[id]
					if !ok {
						r = ReadResult{ScanGroupID: id, Function: g.Function, StartAddress: g.StartAddress, Values: []RegisterValue{}}
					}
					r.Error = &msg
					c.cache[id] = r
				}
				c.mu.Unlock()
				c.publish("master-poll-error", PollErrorEvent{c.id, id, msg}, func() bool { return ctx.Err() == nil && c.polls[id] == t })
			} else if e.Data != nil {
				r := resultDTO(g, e.Data)
				c.mu.Lock()
				if ctx.Err() == nil && c.polls[id] == t {
					c.cache[id] = r
				}
				c.mu.Unlock()
				c.publish("master-poll-data", PollDataEvent{c.id, id, r}, func() bool { return ctx.Err() == nil && c.polls[id] == t })
			}
		}
	}()
	return nil
}
func (c *connection) stopPoll(id string) {
	c.mu.Lock()
	t := c.polls[id]
	if t != nil {
		t.cancel()
	}
	c.mu.Unlock()
	c.engine.StopScanGroup(id)
	if t != nil {
		<-t.done
	}
}
func (c *connection) stopPolls() {
	c.mu.Lock()
	tasks := make([]*task, 0, len(c.polls))
	for _, t := range c.polls {
		t.cancel()
		tasks = append(tasks, t)
	}
	c.mu.Unlock()
	c.engine.StopAllScans()
	for _, t := range tasks {
		<-t.done
	}
}
func (m *Manager) StartPolling(id, groupID string) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.op.Lock()
	defer c.op.Unlock()
	return c.startPoll(groupID)
}
func (m *Manager) StopPolling(id, groupID string) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.op.Lock()
	defer c.op.Unlock()
	c.stopPoll(groupID)
	return nil
}
func (m *Manager) StartAllPolling(id string) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	groups := clone(c.groups)
	c.mu.Unlock()
	for _, g := range groups {
		if g.Enabled {
			if err := c.startPoll(g.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
func (m *Manager) StopAllPolling(id string) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.op.Lock()
	defer c.op.Unlock()
	c.stopPolls()
	return nil
}
func (m *Manager) GetPollData(id, groupID string) (*ReadResult, error) {
	c, err := m.get(id)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.cache[groupID]
	if !ok {
		return nil, nil
	}
	out := clone(r)
	return &out, nil
}
func (c *connection) supervise(ctx context.Context, done chan struct{}, lost <-chan struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-lost:
		}
		for len(lost) > 0 {
			<-lost
		}
		c.op.Lock()
		if ctx.Err() != nil {
			c.op.Unlock()
			return
		}
		c.mu.Lock()
		policy := clone(c.settings.Reconnect)
		groups := clone(c.groups)
		for _, t := range c.scans {
			t.cancel()
		}
		c.mu.Unlock()
		c.engine.Disconnect()
		c.stopDiscoveries()
		c.stopPolls()
		if !policy.Enabled {
			c.mu.Lock()
			c.want = false
			c.mu.Unlock()
			c.setState("Disconnected")
			c.op.Unlock()
			return
		}
		c.setState("Reconnecting")
		c.op.Unlock()
		initial, maxDelay := policy.Delays()
		backoff := master.ReconnectPolicy{Enabled: true, InitialDelay: initial, MaxDelay: maxDelay, BackoffFactor: policy.BackoffFactor}
		success := false
		for attempt := uint32(0); policy.MaxAttempts == nil || attempt < *policy.MaxAttempts; attempt++ {
			timer := time.NewTimer(backoff.NextDelay(attempt))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			c.op.Lock()
			if ctx.Err() != nil {
				c.op.Unlock()
				return
			}
			err := c.engine.Connect(ctx)
			c.op.Unlock()
			if err == nil {
				success = true
				break
			}
		}
		c.op.Lock()
		if ctx.Err() != nil {
			c.engine.Disconnect()
			c.op.Unlock()
			return
		}
		if !success {
			c.mu.Lock()
			c.want = false
			c.mu.Unlock()
			c.setState("Error")
			c.op.Unlock()
			return
		}
		c.setState("Connected")
		for _, g := range groups {
			c.mu.Lock()
			i, err := c.findGroup(g.ID)
			enabled := err == nil && c.groups[i].Enabled
			c.mu.Unlock()
			if g.Enabled && enabled {
				_ = c.startPoll(g.ID)
			}
		}
		c.op.Unlock()
	}
}
func (c *connection) request() (context.Context, *master.Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead || c.stopping || c.state != "Connected" || c.life == nil || c.life.Err() != nil {
		return nil, nil, fmt.Errorf("connection is not connected")
	}
	return c.life, c.engine, nil
}
func (m *Manager) ReadOnce(id string, g ScanGroup) (ReadResult, error) {
	c, err := m.get(id)
	if err != nil {
		return ReadResult{}, err
	}
	f, err := readFunction(g.Function)
	if err != nil {
		return ReadResult{}, err
	}
	ctx, engine, err := c.request()
	if err != nil {
		return ReadResult{}, err
	}
	sid := engine.Config.SlaveID
	if g.SlaveID != nil {
		sid = *g.SlaveID
	}
	r, err := engine.ReadSlave(ctx, sid, f, g.StartAddress, g.Quantity, time.Duration(engine.Config.TimeoutMs)*time.Millisecond)
	if err != nil {
		return ReadResult{}, err
	}
	return resultDTO(g, r), nil
}
func (m *Manager) WriteSingleRegister(id string, address, value uint16) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	ctx, e, err := c.request()
	if err != nil {
		return err
	}
	return e.WriteSingleRegister(ctx, address, value)
}
func (m *Manager) WriteSingleCoil(id string, address uint16, value bool) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	ctx, e, err := c.request()
	if err != nil {
		return err
	}
	return e.WriteSingleCoil(ctx, address, value)
}
func (m *Manager) WriteMultipleRegisters(id string, address uint16, values []uint16) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	ctx, e, err := c.request()
	if err != nil {
		return err
	}
	return e.WriteMultipleRegisters(ctx, address, values)
}
func (m *Manager) WriteMultipleCoils(id string, address uint16, values []bool) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	ctx, e, err := c.request()
	if err != nil {
		return err
	}
	return e.WriteMultipleCoils(ctx, address, values)
}
func (m *Manager) Logs(id string) ([]LogEntry, error) {
	c, err := m.get(id)
	if err != nil {
		return nil, err
	}
	out := []LogEntry{}
	for _, e := range c.logs.GetAll() {
		out = append(out, LogEntry{e.Timestamp.Format(time.RFC3339Nano), string(e.Direction), e.FunctionCode.Name(), e.Detail})
	}
	return out, nil
}
func (m *Manager) ClearLogs(id string) error {
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.logs.Clear()
	return nil
}
func (m *Manager) ExportLogsCSV(id string) (string, error) {
	c, err := m.get(id)
	if err != nil {
		return "", err
	}
	return c.logs.ExportCSV(), nil
}
