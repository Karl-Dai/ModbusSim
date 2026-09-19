// Scan-group polling for the master side. Ported from master.rs's
// start_scan_group / stop_scan_group / stop_all_scans with the same
// transport-lost detection (3 consecutive transport errors; Modbus
// exceptions do not count).
package master

import (
	"context"
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
	// Stop existing poll for this group if any.
	_ = c.StopScanGroup(group.ID)

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

	go c.pollLoop(ctx, tr, events, slaveID, group)

	c.scanMu.Lock()
	if c.scanTasks == nil {
		c.scanTasks = map[string]context.CancelFunc{}
	}
	c.scanTasks[group.ID] = cancel
	c.scanMu.Unlock()

	return events, nil
}

// pollLoop runs the periodic read for one scan group.
func (c *Connection) pollLoop(ctx context.Context, tr transport, events chan PollEvent, slaveID uint8, group *ScanGroup) {
	defer func() {
		c.scanMu.Lock()
		delete(c.scanTasks, group.ID)
		c.scanMu.Unlock()
	}()
	defer close(events)
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
		} else if e, ok := err.(*Error); !ok || e.Kind != "exception" {
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
		reqPDU = append(reqPDU, byte(startAddress>>8&0xFF), 0, 0)
		// address & count big-endian
		reqPDU = reqPDU[:1]
		var ab [4]byte
		ab[0] = byte(uint32(startAddress) + offset >> 8)
		ab[1] = byte(uint32(startAddress) + offset)
		ab[2] = byte(count >> 8)
		ab[3] = byte(count)
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
			if uint16(len(part.Bits)) < count {
				return nil, errTransport("short bit response")
			}
			allBits = append(allBits, part.Bits...)
		} else {
			if uint16(len(part.Registers)) < count {
				return nil, errTransport("short register response")
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
	cancel, ok := c.scanTasks[groupID]
	if ok {
		delete(c.scanTasks, groupID)
	}
	c.scanMu.Unlock()
	if ok {
		cancel()
	}
	return nil
}

// StopAllScans stops all active scan groups.
func (c *Connection) StopAllScans() {
	c.scanMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(c.scanTasks))
	for _, cancel := range c.scanTasks {
		cancels = append(cancels, cancel)
	}
	c.scanTasks = map[string]context.CancelFunc{}
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
