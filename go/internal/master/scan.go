// Lifecycle-safe periodic polling. Discovery scanning lives in the app layer.
package master

import (
	"context"
	"fmt"
	"time"
)

type ScanGroup struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Function     ReadFunction `json:"function"`
	StartAddress uint16       `json:"start_address"`
	Quantity     uint16       `json:"quantity"`
	IntervalMs   uint64       `json:"interval_ms"`
	Enabled      bool         `json:"enabled"`
	SlaveID      *uint8       `json:"slave_id,omitempty"`
}

func (g *ScanGroup) Validate() error {
	if g.Function.FCByte() == 0 {
		return fmt.Errorf("unsupported read function")
	}
	if g.Quantity == 0 || uint32(g.StartAddress)+uint32(g.Quantity) > 65536 {
		return fmt.Errorf("read range must contain 1..65535 addresses and end at or before 65535")
	}
	if g.IntervalMs == 0 || g.IntervalMs > 86400000 {
		return fmt.Errorf("poll interval must be between 1 and 86400000 ms")
	}
	if g.SlaveID != nil && (*g.SlaveID < 1 || *g.SlaveID > 247) {
		return fmt.Errorf("slave ID must be between 1 and 247")
	}
	return nil
}

type PollEvent struct {
	Data *ReadResult
	Err  error
}

const transportLostStreak = 3

type scanTask struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (c *Connection) StartScanGroup(group *ScanGroup) (<-chan PollEvent, error) {
	if err := group.Validate(); err != nil {
		return nil, err
	}
	c.scanOp.Lock()
	defer c.scanOp.Unlock()
	c.stopScan(group.ID)
	if _, err := c.getTransport(); err != nil {
		return nil, err
	}
	snapshot := *group
	if group.SlaveID != nil {
		id := *group.SlaveID
		snapshot.SlaveID = &id
	}
	ctx, cancel := context.WithCancel(context.Background())
	task := &scanTask{cancel: cancel, done: make(chan struct{})}
	events := make(chan PollEvent, 100)
	c.scanMu.Lock()
	if c.scanTasks == nil {
		c.scanTasks = map[string]*scanTask{}
	}
	c.scanTasks[group.ID] = task
	c.scanMu.Unlock()
	go c.pollLoop(ctx, task, events, &snapshot)
	return events, nil
}
func (c *Connection) pollLoop(ctx context.Context, task *scanTask, events chan PollEvent, g *ScanGroup) {
	defer close(task.done)
	defer close(events)
	defer func() {
		c.scanMu.Lock()
		if c.scanTasks[g.ID] == task {
			delete(c.scanTasks, g.ID)
		}
		c.scanMu.Unlock()
	}()
	slaveID := c.Config.SlaveID
	if g.SlaveID != nil {
		slaveID = *g.SlaveID
	}
	streak := 0
	for {
		result, err := c.ReadSlave(ctx, slaveID, g.Function, g.StartAddress, g.Quantity, time.Duration(c.Config.TimeoutMs)*time.Millisecond)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			streak = 0
		} else if e, ok := err.(*Error); !ok || e.Kind != "exception" {
			streak++
		}
		select {
		case events <- PollEvent{Data: result, Err: err}:
		case <-ctx.Done():
			return
		}
		if streak >= transportLostStreak {
			c.notifyConnectionLost()
			return
		}
		timer := time.NewTimer(time.Duration(g.IntervalMs) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (c *Connection) notifyConnectionLost() {
	c.mu.Lock()
	cb := c.onConnectionLost
	c.mu.Unlock()
	if cb != nil {
		cb()
	}
}
func (c *Connection) SetConnectionLostCallback(cb func()) {
	c.mu.Lock()
	c.onConnectionLost = cb
	c.mu.Unlock()
}
func (c *Connection) stopScan(id string) {
	c.scanMu.Lock()
	t := c.scanTasks[id]
	c.scanMu.Unlock()
	if t != nil {
		t.cancel()
		<-t.done
	}
}
func (c *Connection) StopScanGroup(id string) error {
	c.scanOp.Lock()
	defer c.scanOp.Unlock()
	c.stopScan(id)
	return nil
}
func (c *Connection) StopAllScans() {
	c.scanOp.Lock()
	defer c.scanOp.Unlock()
	c.scanMu.Lock()
	tasks := make([]*scanTask, 0, len(c.scanTasks))
	for _, t := range c.scanTasks {
		tasks = append(tasks, t)
		t.cancel()
	}
	c.scanMu.Unlock()
	for _, t := range tasks {
		<-t.done
	}
}
func (c *Connection) IsScanActive(id string) bool {
	c.scanMu.Lock()
	defer c.scanMu.Unlock()
	_, ok := c.scanTasks[id]
	return ok
}
func (c *Connection) IsPolling() bool {
	c.scanMu.Lock()
	defer c.scanMu.Unlock()
	return len(c.scanTasks) > 0
}
