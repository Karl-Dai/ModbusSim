// Scan-group polling for the master side. Ported from master.rs's
// start_scan_group / stop_scan_group / stop_all_scans with the same
// transport-lost detection (3 consecutive transport errors; Modbus
// exceptions do not count).
package master

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

// ScanGroup is a named group of registers to scan periodically.
type ScanGroup struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Function     ReadFunction `json:"function"`
	StartAddress uint16       `json:"start_address"`
	Quantity     uint16       `json:"quantity"`
	IntervalMs   uint64       `json:"interval_ms"`
	Enabled      bool         `json:"enabled"`
	// SlaveID overrides the connection default when non-nil.
	SlaveID *uint8 `json:"slave_id,omitempty"`
}

// Validate mirrors ScanGroup::validate.
func (g *ScanGroup) Validate() error {
	if g.Function.FCByte() == 0 {
		return fmt.Errorf("unsupported read function")
	}
	if g.Quantity == 0 || uint32(g.StartAddress)+uint32(g.Quantity) > 65536 {
		return fmt.Errorf("read range must contain 1..65535 addresses and end at or before 65535")
	}
	if g.IntervalMs == 0 {
		return fmt.Errorf("poll interval must be greater than zero")
	}
	if g.SlaveID != nil && (*g.SlaveID < 1 || *g.SlaveID > 247) {
		return fmt.Errorf("slave ID must be between 1 and 247")
	}
	return nil
}

// PollEvent is one event emitted by a polling task (PollEvent in Rust).
type PollEvent struct {
	Data *ReadResult // set on success
	Err  error       // set on failure
}

// transportLostStreak mirrors TRANSPORT_LOST_STREAK.
const transportLostStreak = 3

// scanTask tracks one running scan group.
type scanTask struct {
	cancel context.CancelFunc
	events chan PollEvent
}

// StartScanGroup starts polling for a scan group and returns the event
// channel for this group. Starting an already-active group restarts it.
func (c *Connection) StartScanGroup(group *ScanGroup) (<-chan PollEvent, error) {
	if err := group.Validate(); err != nil {
		return nil, &Error{Kind: "invalid_config", Msg: err.Error()}
	}
	if err := c.Config.Requests.Validate(); err != nil {
		return nil, &Error{Kind: "invalid_config", Msg: err.Error()}
	}
	tr, err := c.getTransport()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan PollEvent, 100)

	slaveID := c.Config.SlaveID
	if group.SlaveID != nil {
		slaveID = *group.SlaveID
	}

	task := &scanTask{cancel: cancel, events: events}
	c.scanMu.Lock()
	if c.scanTasks == nil {
		c.scanTasks = map[string]*scanTask{}
	}
	previous := c.scanTasks[group.ID]
	c.scanTasks[group.ID] = task
	c.scanMu.Unlock()
	if previous != nil {
		previous.cancel()
	}

	// Own a snapshot: later caller edits must not change a running scan.
	groupCopy := *group
	go c.pollLoop(ctx, tr, task, slaveID, &groupCopy)
	return events, nil
}

// pollLoop runs the periodic read for one scan group.
func (c *Connection) pollLoop(ctx context.Context, tr transport, task *scanTask, slaveID uint8, group *ScanGroup) {
	events := task.events
	defer func() {
		c.scanMu.Lock()
		if c.scanTasks[group.ID] == task {
			delete(c.scanTasks, group.ID)
		}
		c.scanMu.Unlock()
		close(events)
	}()
	interval := time.Duration(group.IntervalMs) * time.Millisecond
	transportErrStreak := 0

	for {
		// One batched read; Modbus exceptions do not count toward the streak.
		result, err := c.readWithTransport(ctx, tr, slaveID, group.Function, group.StartAddress, group.Quantity)

		if err != nil && err == context.Canceled {
			return
		}

		if err == nil {
			transportErrStreak = 0
		} else if e, ok := err.(*Error); ok && e.Kind == "exception" {
			transportErrStreak = 0
		} else {
			transportErrStreak++
		}

		event := PollEvent{}
		if err != nil {
			event.Err = err
		} else {
			event.Data = result
		}
		select {
		case events <- event:
		case <-ctx.Done():
			return
		}

		if transportErrStreak >= transportLostStreak {
			c.notifyConnectionLost()
			return
		}

		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return
		}
	}
}

// readWithTransport reads via a specific transport (bypasses getTransport so
// the poll keeps the transport it started with).
func (c *Connection) readWithTransport(ctx context.Context, tr transport, slaveID uint8, function ReadFunction, startAddress, quantity uint16) (*ReadResult, error) {
	settings := c.Config.Requests
	if err := settings.Validate(); err != nil {
		return nil, &Error{Kind: "invalid_config", Msg: err.Error()}
	}
	if function.FCByte() == 0 || quantity == 0 || uint32(startAddress)+uint32(quantity) > 65536 {
		return nil, &Error{Kind: "invalid_config", Msg: "invalid read function or address range"}
	}
	limit := settings.MaxReadRegisters
	kind := "holding_registers"
	if function == ReadCoils || function == ReadDiscreteInputs {
		limit = settings.MaxReadBits
		kind = "coils"
		if function == ReadDiscreteInputs {
			kind = "discrete_inputs"
		}
	}
	if function == ReadHoldingRegisters {
		kind = "holding_registers"
	} else if function == ReadInputRegisters {
		kind = "input_registers"
	}

	var allBits []bool
	var allRegs []uint16
	offset := uint32(0)
	for offset < uint32(quantity) {
		count := uint16(limit)
		if remain := uint32(quantity) - offset; uint32(count) > remain {
			count = uint16(remain)
		}

		release, err := c.pacer.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		reqPDU := []byte{function.FCByte()}
		// address & count big-endian
		var ab [4]byte
		binary.BigEndian.PutUint16(ab[0:2], uint16(uint32(startAddress)+offset))
		binary.BigEndian.PutUint16(ab[2:4], count)
		reqPDU = append(reqPDU, ab[:]...)

		timeout := time.Duration(c.Config.TimeoutMs) * time.Millisecond
		if c.log != nil {
			c.log.AddRequest("tx", function.FCByte(), fmt.Sprintf("R %d x%d", uint32(startAddress)+offset, count))
		}
		resp, err := tr.exchange(slaveID, reqPDU, timeout)
		release()
		if err != nil {
			if c.log != nil {
				c.log.AddRequest("rx", function.FCByte(), fmt.Sprintf("R %d x%d: ERR: %v", uint32(startAddress)+offset, count, err))
			}
			return nil, err
		}
		if c.log != nil {
			detail := "OK"
			if len(resp) > 0 && resp[0]&0x80 != 0 {
				exc := uint8(0)
				if len(resp) > 1 {
					exc = resp[1]
				}
				detail = fmt.Sprintf("ERR: exception 0x%02X", exc)
			}
			c.log.AddRequest("rx", function.FCByte(), fmt.Sprintf("R %d x%d: %s", uint32(startAddress)+offset, count, detail))
		}
		part, err := parseReadResponse(function, resp)
		if err != nil {
			return nil, err
		}
		if function == ReadCoils || function == ReadDiscreteInputs {
			if len(part.Bits) != (int(count)+7)/8*8 {
				return nil, errTransport("unexpected bit response length")
			}
			allBits = append(allBits, part.Bits[:count]...)
		} else {
			if len(part.Registers) != int(count) {
				return nil, errTransport("unexpected register response length")
			}
			allRegs = append(allRegs, part.Registers...)
		}
		offset += uint32(count)
	}

	if allBits != nil {
		return &ReadResult{Kind: kind, Bits: allBits}, nil
	}
	return &ReadResult{Kind: kind, Registers: allRegs}, nil
}

// connectionLostMu guards the connection-lost callback registration.
var connectionLostMu sync.Mutex

// ConnectionLostCallback is invoked when poll tasks observe the transport
// going dead. Set via SetConnectionLostCallback.
var _ = connectionLostMu.Lock

// notifyConnectionLost fires the app-registered callback, if any.
func (c *Connection) notifyConnectionLost() {
	if c.onConnectionLost != nil {
		c.onConnectionLost()
	}
}

// SetConnectionLostCallback registers the callback fired when a poll task
// observes the transport going dead (3 consecutive transport errors).
func (c *Connection) SetConnectionLostCallback(cb func()) {
	c.onConnectionLost = cb
}

// StopScanGroup stops one scan group's polling task (idempotent).
func (c *Connection) StopScanGroup(groupID string) error {
	c.scanMu.Lock()
	task, ok := c.scanTasks[groupID]
	if ok {
		delete(c.scanTasks, groupID)
	}
	c.scanMu.Unlock()
	if ok {
		task.cancel()
	}
	return nil
}

// StopAllScans stops all active scan groups.
func (c *Connection) StopAllScans() {
	c.scanMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(c.scanTasks))
	for _, task := range c.scanTasks {
		cancels = append(cancels, task.cancel)
	}
	c.scanTasks = map[string]*scanTask{}
	c.scanMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// IsScanActive reports whether a specific scan group is actively polling.
func (c *Connection) IsScanActive(groupID string) bool {
	c.scanMu.Lock()
	defer c.scanMu.Unlock()
	_, ok := c.scanTasks[groupID]
	return ok
}

// IsPolling reports whether any polling is active.
func (c *Connection) IsPolling() bool {
	c.scanMu.Lock()
	defer c.scanMu.Unlock()
	return len(c.scanTasks) > 0
}

// unused guard to keep the time import when refactoring.
var _ = time.Millisecond
