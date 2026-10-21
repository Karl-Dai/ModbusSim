package masterstate

import (
	"context"
	"fmt"
	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"strconv"
	"time"
)

func (c *connection) stopDiscovery(kind string) {
	c.mu.Lock()
	t := c.scans[kind]
	if t != nil {
		t.cancel()
	}
	c.mu.Unlock()
	if t != nil {
		<-t.done
	}
}
func (c *connection) stopDiscoveries() {
	c.mu.Lock()
	tasks := []*task{}
	for _, t := range c.scans {
		t.cancel()
		tasks = append(tasks, t)
	}
	c.mu.Unlock()
	for _, t := range tasks {
		<-t.done
	}
}
func (c *connection) newDiscovery(kind string) (*task, *master.Connection, error) {
	c.stopDiscovery(kind)
	ctx, engine, err := c.request()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	t := &task{ctx: ctx, cancel: cancel, done: make(chan struct{}), id: "scan_" + strconv.FormatUint(c.owner.sequence.Add(1), 10)}
	c.mu.Lock()
	c.scans[kind] = t
	c.mu.Unlock()
	return t, engine, nil
}
func (c *connection) finishDiscovery(kind string, t *task) {
	c.mu.Lock()
	if c.scans[kind] == t {
		delete(c.scans, kind)
	}
	c.mu.Unlock()
	close(t.done)
}
func (m *Manager) StartSlaveIDScan(id string, req SlaveScanRequest) (string, error) {
	if req.StartID < 1 || req.EndID > 247 || req.EndID < req.StartID || req.TimeoutMS < 1 || req.TimeoutMS > 600000 {
		return "", fmt.Errorf("invalid station scan range or timeout")
	}
	c, err := m.get(id)
	if err != nil {
		return "", err
	}
	c.op.Lock()
	defer c.op.Unlock()
	t, engine, err := c.newDiscovery("slave_scan")
	if err != nil {
		return "", err
	}
	go func() {
		defer c.finishDiscovery("slave_scan", t)
		found := []uint8{}
		current := req.StartID
		send := func(done, cancelled bool) {
			p := SlaveScanEvent{c.id, t.id, current, uint16(req.EndID) - uint16(req.StartID) + 1, append([]uint8{}, found...), done, cancelled}
			c.publish("scan-slave-progress", p, func() bool { return c.scans["slave_scan"] == t })
		}
		for n := uint16(req.StartID); n <= uint16(req.EndID); n++ {
			if t.ctx.Err() != nil {
				break
			}
			current = uint8(n)
			_, err := engine.ReadSlave(t.ctx, current, master.ReadHoldingRegisters, 0, 1, time.Duration(req.TimeoutMS)*time.Millisecond)
			if t.ctx.Err() != nil {
				break
			}
			if err == nil {
				found = append(found, current)
			}
			if n != uint16(req.EndID) {
				send(false, false)
			}
		}
		send(t.ctx.Err() == nil, t.ctx.Err() != nil)
	}()
	return t.id, nil
}
func (m *Manager) StartRegisterScan(id string, req RegisterScanRequest) (string, error) {
	f, err := readFunction(req.Function)
	if err != nil {
		return "", err
	}
	if req.EndAddress < req.StartAddress || req.ChunkSize == 0 || req.TimeoutMS < 1 || req.TimeoutMS > 600000 {
		return "", fmt.Errorf("invalid register scan range, chunk size or timeout")
	}
	c, err := m.get(id)
	if err != nil {
		return "", err
	}
	c.op.Lock()
	defer c.op.Unlock()
	t, engine, err := c.newDiscovery("register_scan")
	if err != nil {
		return "", err
	}
	limit := engine.Config.Requests.MaxReadRegisters
	if f == master.ReadCoils || f == master.ReadDiscreteInputs {
		limit = engine.Config.Requests.MaxReadBits
	}
	chunk := min(req.ChunkSize, limit)
	go func() {
		defer c.finishDiscovery("register_scan", t)
		found := []FoundRegister{}
		current := req.StartAddress
		send := func(done, cancelled bool) {
			p := RegisterScanEvent{c.id, t.id, current, req.EndAddress, len(found), append([]FoundRegister{}, found...), done, cancelled}
			c.publish("scan-register-progress", p, func() bool { return c.scans["register_scan"] == t })
		}
		for addr := uint32(req.StartAddress); addr <= uint32(req.EndAddress); {
			if t.ctx.Err() != nil {
				break
			}
			count := uint16(min(uint32(chunk), uint32(req.EndAddress)-addr+1))
			r, err := engine.ReadSlave(t.ctx, engine.Config.SlaveID, f, uint16(addr), count, time.Duration(req.TimeoutMS)*time.Millisecond)
			if t.ctx.Err() != nil {
				break
			}
			if err == nil {
				dto := resultDTO(ScanGroup{Function: req.Function, StartAddress: uint16(addr)}, r)
				for _, v := range dto.Values {
					found = append(found, FoundRegister{v.Address, v.RawValue})
				}
			}
			current = uint16(addr + uint32(count) - 1)
			addr += uint32(count)
			if addr <= uint32(req.EndAddress) {
				send(false, false)
			}
		}
		send(t.ctx.Err() == nil, t.ctx.Err() != nil)
	}()
	return t.id, nil
}
func (m *Manager) CancelScan(id, kind string) error {
	if kind != "slave_scan" && kind != "register_scan" {
		return fmt.Errorf("unknown scan type")
	}
	c, err := m.get(id)
	if err != nil {
		return err
	}
	c.mu.Lock()
	t := c.scans[kind]
	if t != nil {
		t.cancel()
	}
	c.mu.Unlock()
	if t != nil {
		<-t.done
	}
	return nil
}
