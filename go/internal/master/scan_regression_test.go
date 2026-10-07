package master

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScanPreservesHighAddressBytes(t *testing.T) {
	tr := &mockTransport{responses: [][]byte{{3, 2, 0, 1}, {3, 2, 0, 2}}}
	c := testConnection(tr)
	c.Config.Requests.MaxReadRegisters = 1
	if _, err := c.readWithTransport(context.Background(), tr, 1, ReadHoldingRegisters, 0x12FF, 2); err != nil {
		t.Fatal(err)
	}
	for i, request := range tr.exchanges {
		if addr := binary.BigEndian.Uint16(request[1:3]); addr != uint16(0x12FF+i) {
			t.Errorf("batch %d address=%04X, want %04X", i, addr, 0x12FF+i)
		}
	}
}

func TestReadRejectsUnsupportedFunctionsBeforeExchange(t *testing.T) {
	tr := &mockTransport{}
	c := testConnection(tr)
	for _, fc := range []ReadFunction{0, 5, 255} {
		if _, err := c.Read(context.Background(), fc, 0, 1); err == nil {
			t.Errorf("read accepted function %d", fc)
		}
		if _, err := c.StartScanGroup(&ScanGroup{ID: "bad", Function: fc, Quantity: 1, IntervalMs: 1}); err == nil {
			c.StopAllScans()
			t.Errorf("scan accepted function %d", fc)
		}
	}
	if len(tr.exchanges) != 0 {
		t.Fatal("invalid function was exchanged")
	}
}

func TestRestartedScanRetainsRegistration(t *testing.T) {
	firstStarted := make(chan struct{})
	shutdown := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	tr := &functionTransport{exchangeFn: func(uint8, []byte, time.Duration) ([]byte, error) {
		select {
		case <-shutdown:
			return nil, fmt.Errorf("closed")
		default:
		}
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return []byte{3, 2, 0, 1}, nil
	}}
	c := testConnection(tr)
	group := &ScanGroup{ID: "restart", Function: ReadHoldingRegisters, Quantity: 1, IntervalMs: 1}
	oldEvents, err := c.StartScanGroup(group)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first scan did not start")
	}
	newEvents, err := c.StartScanGroup(group)
	if err != nil {
		close(releaseFirst)
		t.Fatal(err)
	}
	defer func() {
		close(shutdown)
		c.StopAllScans()
		for range newEvents {
		}
	}()
	close(releaseFirst)
	for range oldEvents {
	}
	for i := 0; i < 10; i++ {
		select {
		case _, ok := <-newEvents:
			if !ok {
				t.Fatal("replacement scan stopped")
			}
		case <-time.After(time.Second):
			t.Fatal("replacement scan stalled")
		}
		if !c.IsScanActive(group.ID) {
			t.Fatal("old scan removed replacement registration")
		}
	}
}

func TestConcurrentScanStartsRemainStoppable(t *testing.T) {
	shutdown := make(chan struct{})
	tr := &functionTransport{exchangeFn: func(uint8, []byte, time.Duration) ([]byte, error) {
		select {
		case <-shutdown:
			return nil, fmt.Errorf("closed")
		default:
			return []byte{3, 2, 0, 1}, nil
		}
	}}
	c := testConnection(tr)
	events := make(chan (<-chan PollEvent), 32)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ch, err := c.StartScanGroup(&ScanGroup{ID: "same", Function: ReadHoldingRegisters, Quantity: 1, IntervalMs: 1})
			if err != nil {
				t.Error(err)
				return
			}
			events <- ch
		}()
	}
	close(start)
	wg.Wait()
	close(events)
	c.StopAllScans()
	defer close(shutdown)
	deadline := time.After(time.Second)
	for ch := range events {
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					goto next
				}
			case <-deadline:
				t.Fatal("an overwritten scan remained active after StopAllScans")
			}
		}
	next:
	}
	if c.IsPolling() {
		t.Fatal("stopped scans remain registered")
	}
}
