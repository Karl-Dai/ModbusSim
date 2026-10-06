package master

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func testConnection(tr transport) *Connection {
	c := NewConnection(DefaultConfig(), Transport{Type: TransportTCP})
	c.tr, c.state = tr, StateConnected
	return c
}

func TestReadResponseRejectsMalformedPDUs(t *testing.T) {
	for _, tc := range []struct {
		name string
		fc   ReadFunction
		pdu  []byte
	}{
		{"empty", ReadHoldingRegisters, nil},
		{"function_only", ReadHoldingRegisters, []byte{3}},
		{"truncated_exception", ReadHoldingRegisters, []byte{0x83}},
		{"wrong_function", ReadHoldingRegisters, []byte{4, 2, 0, 1}},
		{"wrong_exception_function", ReadHoldingRegisters, []byte{0x84, 2}},
		{"truncated_register", ReadHoldingRegisters, []byte{3, 4, 0, 1}},
		{"extra_register", ReadHoldingRegisters, []byte{3, 2, 0, 1, 0, 2}},
		{"odd_register_bytes", ReadHoldingRegisters, []byte{3, 3, 0, 1, 0}},
		{"truncated_bits", ReadCoils, []byte{1, 2, 1}},
		{"extra_bits", ReadCoils, []byte{1, 1, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("malformed response panicked: %v", r)
				}
			}()
			if _, err := parseReadResponse(tc.fc, tc.pdu); err == nil {
				t.Error("malformed response accepted")
			}
		})
	}
}

func TestReadResponsePreservesModbusException(t *testing.T) {
	_, err := parseReadResponse(ReadHoldingRegisters, []byte{0x83, 2})
	var modbusErr *Error
	if !errors.As(err, &modbusErr) || modbusErr.Kind != "exception" || modbusErr.Exception != 2 {
		t.Fatalf("got %v, want Modbus exception 2", err)
	}
}

func TestBitBatchesExcludePadding(t *testing.T) {
	for _, scan := range []bool{false, true} {
		for _, fc := range []ReadFunction{ReadCoils, ReadDiscreteInputs} {
			t.Run(string(rune('0'+fc))+map[bool]string{false: "_read", true: "_scan"}[scan], func(t *testing.T) {
				tr := &mockTransport{responses: [][]byte{{fc.FCByte(), 1, 0xFD}, {fc.FCByte(), 1, 0xFA}}}
				c := testConnection(tr)
				c.Config.Requests.MaxReadBits = 3
				var got *ReadResult
				var err error
				if scan {
					got, err = c.readWithTransport(context.Background(), tr, 1, fc, 10, 5)
				} else {
					got, err = c.Read(context.Background(), fc, 10, 5)
				}
				if err != nil {
					t.Fatal(err)
				}
				if want := []bool{true, false, true, false, true}; !reflect.DeepEqual(got.Bits, want) {
					t.Fatalf("bits=%v, want %v", got.Bits, want)
				}
			})
		}
	}
}

func TestReadRejectsOversizedResponses(t *testing.T) {
	for _, scan := range []bool{false, true} {
		for _, fc := range []ReadFunction{ReadCoils, ReadHoldingRegisters} {
			tr := &mockTransport{responses: [][]byte{{fc.FCByte(), 4, 0, 1, 0, 2}}}
			c := testConnection(tr)
			var err error
			if scan {
				_, err = c.readWithTransport(context.Background(), tr, 1, fc, 0, 1)
			} else {
				_, err = c.Read(context.Background(), fc, 0, 1)
			}
			if err == nil {
				t.Errorf("scan=%v fc=%v: oversized response accepted", scan, fc)
			}
		}
	}
}

func TestWriteRejectsInvalidEcho(t *testing.T) {
	for _, tc := range []struct {
		name string
		fc   byte
		call func(*Connection) error
		echo []byte
	}{
		{"single_coil", 5, func(c *Connection) error { return c.WriteSingleCoil(context.Background(), 10, true) }, []byte{5, 0, 10, 255, 0}},
		{"single_register", 6, func(c *Connection) error { return c.WriteSingleRegister(context.Background(), 10, 123) }, []byte{6, 0, 10, 0, 123}},
		{"multiple_coils", 15, func(c *Connection) error { return c.WriteMultipleCoils(context.Background(), 10, []bool{true, false}) }, []byte{15, 0, 10, 0, 2}},
		{"multiple_registers", 16, func(c *Connection) error { return c.WriteMultipleRegisters(context.Background(), 10, []uint16{1, 2}) }, []byte{16, 0, 10, 0, 2}},
	} {
		for _, mutation := range []string{"truncated", "address", "value_or_quantity", "trailing"} {
			t.Run(tc.name+"/"+mutation, func(t *testing.T) {
				resp := append([]byte(nil), tc.echo...)
				switch mutation {
				case "truncated":
					resp = resp[:1]
				case "address":
					resp[2]++
				case "value_or_quantity":
					resp[4]++
				case "trailing":
					resp = append(resp, 0)
				}
				if err := tc.call(testConnection(&mockTransport{responses: [][]byte{resp}})); err == nil {
					t.Error("invalid write echo accepted")
				}
			})
		}
	}
}

func TestWriteRejectsInvalidRangeBeforeExchange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		address uint16
		count   int
		coils   bool
	}{
		{"empty_registers", 0, 0, false}, {"too_many_registers", 0, 124, false}, {"wrapping_registers", 65535, 2, false}, {"register_quantity_truncation", 0, 65536, false},
		{"empty_coils", 0, 0, true}, {"too_many_coils", 0, 1969, true}, {"wrapping_coils", 65535, 2, true}, {"coil_quantity_truncation", 0, 65536, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &mockTransport{}
			c := testConnection(tr)
			var err error
			if tc.coils {
				err = c.WriteMultipleCoils(context.Background(), tc.address, make([]bool, tc.count))
			} else {
				err = c.WriteMultipleRegisters(context.Background(), tc.address, make([]uint16, tc.count))
			}
			var e *Error
			if !errors.As(err, &e) || e.Kind != "invalid_config" {
				t.Errorf("error=%v, want invalid_config", err)
			}
			if len(tr.exchanges) != 0 {
				t.Error("invalid range was sent to device")
			}
		})
	}
}

type functionTransport struct {
	exchangeFn func(uint8, []byte, time.Duration) ([]byte, error)
}

func (t *functionTransport) exchange(s uint8, p []byte, d time.Duration) ([]byte, error) {
	return t.exchangeFn(s, p, d)
}
func (*functionTransport) close() error { return nil }

func TestScanExceptionsDoNotDisconnect(t *testing.T) {
	tr := &functionTransport{exchangeFn: func(uint8, []byte, time.Duration) ([]byte, error) { return []byte{0x83, 2}, nil }}
	c := testConnection(tr)
	var lost atomic.Int32
	c.SetConnectionLostCallback(func() { lost.Add(1) })
	events, err := c.StartScanGroup(&ScanGroup{ID: "exceptions", Function: ReadHoldingRegisters, Quantity: 1, IntervalMs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		c.StopAllScans()
		for range events {
		}
	}()
	for i := 0; i < 5; i++ {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("scan stopped on Modbus exceptions")
			}
			var e *Error
			if !errors.As(ev.Err, &e) || e.Kind != "exception" {
				t.Errorf("event=%+v, want exception", ev)
			}
		case <-time.After(time.Second):
			t.Fatal("scan stalled")
		}
	}
	if lost.Load() != 0 {
		t.Fatal("Modbus exception marked connection lost")
	}
}

func TestRequestPacerSerializesInFlightRequests(t *testing.T) {
	p := NewRequestPacer(0)
	release, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	next, err := p.Acquire(ctx)
	if err == nil {
		next()
		t.Error("second request entered before the first completed")
	}
	release()
	next, err = p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	next()
}
