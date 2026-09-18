package master

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRequestSettingsValidate(t *testing.T) {
	if err := DefaultRequestSettings().Validate(); err != nil {
		t.Errorf("default: %v", err)
	}
	s := RequestSettings{IntervalMs: 60001, MaxReadRegisters: 125, MaxReadBits: 2000}
	if err := s.Validate(); err == nil {
		t.Error("interval 60001: expected error")
	}
	s = RequestSettings{IntervalMs: 0, MaxReadRegisters: 0, MaxReadBits: 2000}
	if err := s.Validate(); err == nil {
		t.Error("max registers 0: expected error")
	}
	s = RequestSettings{IntervalMs: 0, MaxReadRegisters: 125, MaxReadBits: 2001}
	if err := s.Validate(); err == nil {
		t.Error("max bits 2001: expected error")
	}
}

func TestRequestPacerGapStartsAfterCompletion(t *testing.T) {
	p := NewRequestPacer(50 * time.Millisecond)
	ctx := context.Background()

	release, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	release()

	// The 50 ms gap starts at release, so ~20 ms later Acquire must not finish.
	start := time.Now()
	ctxTimeout, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := p.Acquire(ctxTimeout); err == nil {
		t.Error("Acquire should not have completed within 20ms of release")
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Errorf("returned too early: %v", elapsed)
	}

	// After the full gap it completes.
	release2, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release2()
}

func TestReconnectPolicyDelays(t *testing.T) {
	p := DefaultReconnectPolicy()
	cases := []struct {
		attempt uint32
		want    time.Duration
	}{
		{0, 1000 * time.Millisecond},
		{1, 2000 * time.Millisecond},
		{2, 4000 * time.Millisecond},
		{10, 30000 * time.Millisecond}, // clamped
	}
	for _, c := range cases {
		if got := p.NextDelay(c.attempt); got != c.want {
			t.Errorf("NextDelay(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestScanGroupValidate(t *testing.T) {
	g := ScanGroup{ID: "1", Function: ReadHoldingRegisters, StartAddress: 0, Quantity: 10, IntervalMs: 100}
	if err := g.Validate(); err != nil {
		t.Errorf("valid: %v", err)
	}
	g.IntervalMs = 0
	if err := g.Validate(); err == nil {
		t.Error("zero interval: expected error")
	}
	g.IntervalMs = 100
	g.Quantity = 10
	g.StartAddress = 65530
	if err := g.Validate(); err == nil {
		t.Error("range overflow: expected error")
	}
	sid := uint8(0)
	g.StartAddress, g.Quantity = 0, 10
	g.SlaveID = &sid
	if err := g.Validate(); err == nil {
		t.Error("slave 0: expected error")
	}
}

func TestParseReadResponse(t *testing.T) {
	// FC03 response: 3 registers.
	resp := []byte{0x03, 0x06, 0x00, 0x01, 0x00, 0x02, 0x00, 0x03}
	r, err := parseReadResponse(ReadHoldingRegisters, resp)
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "holding_registers" || len(r.Registers) != 3 || r.Registers[2] != 3 {
		t.Errorf("result = %+v", r)
	}
	// FC01 response: bits packed 0x0D, 0x01.
	resp = []byte{0x01, 0x02, 0x0D, 0x01}
	r, err = parseReadResponse(ReadCoils, resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Bits) != 16 || !r.Bits[0] || r.Bits[1] || !r.Bits[8] {
		t.Errorf("bits = %v", r.Bits)
	}
}

func TestCheckWriteResponse(t *testing.T) {
	if err := checkWriteResponse([]byte{0x05, 0x00, 0x0A, 0xFF, 0x00}, 0x05); err != nil {
		t.Errorf("valid echo: %v", err)
	}
	if err := checkWriteResponse([]byte{0x83, 0x02}, 0x03); err == nil {
		t.Error("exception: expected error")
	} else if e := err.(*Error); e.Kind != "exception" || e.Exception != 0x02 {
		t.Errorf("kind = %+v", e)
	}
	if err := checkWriteResponse([]byte{0x06, 0x00, 0x00, 0x00, 0x00}, 0x05); err == nil {
		t.Error("wrong FC: expected error")
	}
}

// mockTransport records exchanges and returns scripted responses.
type mockTransport struct {
	responses [][]byte
	exchanges [][]byte
	delay     time.Duration
}

func (m *mockTransport) exchange(slaveID uint8, reqPDU []byte, timeout time.Duration) ([]byte, error) {
	m.exchanges = append(m.exchanges, reqPDU)
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("no scripted response")
	}
	r := m.responses[0]
	m.responses = m.responses[1:]
	return r, nil
}

func (m *mockTransport) close() error { return nil }

var _ transport = (*mockTransport)(nil)

// TestBatchedReads verifies a 300-register read is split into 125/125/50.
func TestBatchedReads(t *testing.T) {
	mock := &mockTransport{}
	for i := 0; i < 3; i++ {
		n := 125
		if i == 2 {
			n = 50
		}
		resp := []byte{0x03, byte(n * 2)}
		for j := 0; j < n; j++ {
			resp = append(resp, 0, 0)
		}
		mock.responses = append(mock.responses, resp)
	}

	c := NewConnection(Config{SlaveID: 1, TimeoutMs: 1000, Requests: DefaultRequestSettings()}, Transport{Type: TransportTCP})
	c.mu.Lock()
	c.tr = mock
	c.state = StateConnected
	c.mu.Unlock()

	result, err := c.Read(context.Background(), ReadHoldingRegisters, 0, 300)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Registers) != 300 {
		t.Fatalf("registers = %d, want 300", len(result.Registers))
	}
	if len(mock.exchanges) != 3 {
		t.Fatalf("exchanges = %d, want 3", len(mock.exchanges))
	}
	// Verify request addressing: 0/125, 125/125, 250/50.
	checks := []struct {
		addr, qty uint16
	}{
		{(uint16(mock.exchanges[0][1]) << 8) | uint16(mock.exchanges[0][2]), (uint16(mock.exchanges[0][3]) << 8) | uint16(mock.exchanges[0][4])},
		{(uint16(mock.exchanges[1][1]) << 8) | uint16(mock.exchanges[1][2]), (uint16(mock.exchanges[1][3]) << 8) | uint16(mock.exchanges[1][4])},
		{(uint16(mock.exchanges[2][1]) << 8) | uint16(mock.exchanges[2][2]), (uint16(mock.exchanges[2][3]) << 8) | uint16(mock.exchanges[2][4])},
	}
	want := []struct{ addr, qty uint16 }{{0, 125}, {125, 125}, {250, 50}}
	for i, c := range checks {
		if c != want[i] {
			t.Errorf("batch %d: addr=%d qty=%d, want %d/%d", i, c.addr, c.qty, want[i].addr, want[i].qty)
		}
	}
}

// TestScanGroupLifecycle runs a scan group against a mock and stops it.
func TestScanGroupLifecycle(t *testing.T) {
	mock := &mockTransport{}
	mock.responses = append(mock.responses, []byte{0x03, 0x02, 0x12, 0x34})

	c := NewConnection(Config{SlaveID: 1, TimeoutMs: 1000, Requests: DefaultRequestSettings()}, Transport{Type: TransportTCP})
	c.mu.Lock()
	c.tr = mock
	c.state = StateConnected
	c.mu.Unlock()

	events, err := c.StartScanGroup(&ScanGroup{
		ID: "g1", Name: "test", Function: ReadHoldingRegisters,
		StartAddress: 0, Quantity: 1, IntervalMs: 50, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsScanActive("g1") || !c.IsPolling() {
		t.Fatal("scan should be active")
	}

	select {
	case ev := <-events:
		if ev.Err != nil {
			t.Fatalf("poll error: %v", ev.Err)
		}
		if ev.Data.Kind != "holding_registers" || len(ev.Data.Registers) != 1 || ev.Data.Registers[0] != 0x1234 {
			t.Fatalf("data = %+v", ev.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no poll event received")
	}

	if err := c.StopScanGroup("g1"); err != nil {
		t.Fatal(err)
	}
	if c.IsPolling() {
		t.Fatal("scan should be stopped")
	}
}

// TestTransportLostDetection verifies that 3 consecutive transport errors
// fire the connection-lost callback.
func TestTransportLostStreak(t *testing.T) {
	mock := &mockTransport{} // always returns "no scripted response" error
	lost := make(chan struct{}, 1)

	c := NewConnection(Config{SlaveID: 1, TimeoutMs: 1000, Requests: DefaultRequestSettings()}, Transport{Type: TransportTCP})
	c.mu.Lock()
	c.tr = mock
	c.state = StateConnected
	c.mu.Unlock()
	c.SetConnectionLostCallback(func() { lost <- struct{}{} })

	events, err := c.StartScanGroup(&ScanGroup{
		ID: "g1", Function: ReadHoldingRegisters,
		StartAddress: 0, Quantity: 1, IntervalMs: 10, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-lost:
		// expected after 3 consecutive failures
	case <-time.After(2 * time.Second):
		t.Fatal("connection-lost callback not fired")
	}

	// The channel should be closed by the exiting pollLoop.
	for range events {
	}
	if c.IsPolling() {
		t.Fatal("poll task should have exited")
	}
}
