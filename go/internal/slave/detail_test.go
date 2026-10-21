// Log-detail grammar tests: the RX detail string is byte-compared against the
// Rust slave's format_request_detail during the dual-stack period, so every
// function code's shape is pinned here rather than left to e2e side effects.
package slave

import (
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/logcol"
	"github.com/Karl-Dai/ModbusSim/go/internal/pdu"
)

func TestFormatRequestDetail(t *testing.T) {
	cases := []struct {
		name string
		req  *pdu.Request
		want string
	}{
		{"FC01", &pdu.Request{FunctionCode: pdu.FCReadCoils, Address: 0, Quantity: 5}, "R 0 x5"},
		{"FC02", &pdu.Request{FunctionCode: pdu.FCReadDiscreteInputs, Address: 7, Quantity: 2}, "R 7 x2"},
		{"FC03", &pdu.Request{FunctionCode: pdu.FCReadHoldingRegisters, Address: 100, Quantity: 123}, "R 100 x123"},
		{"FC04", &pdu.Request{FunctionCode: pdu.FCReadInputRegisters, Address: 1, Quantity: 2}, "R 1 x2"},
		{"FC05 on", &pdu.Request{FunctionCode: pdu.FCWriteSingleCoil, Address: 10, CoilOn: true}, "W 10 = true"},
		{"FC05 off", &pdu.Request{FunctionCode: pdu.FCWriteSingleCoil, Address: 10, CoilOn: false}, "W 10 = false"},
		// Rust renders FC06 with {:#06x}: lowercase, zero-padded, 0x prefix included.
		{"FC06 padded", &pdu.Request{FunctionCode: pdu.FCWriteSingleRegister, Address: 100, Value: 0x0A0B}, "W 100 = 0x0a0b"},
		{"FC06 low", &pdu.Request{FunctionCode: pdu.FCWriteSingleRegister, Address: 100, Value: 0x0001}, "W 100 = 0x0001"},
		{"FC06 zero", &pdu.Request{FunctionCode: pdu.FCWriteSingleRegister, Address: 100, Value: 0x0000}, "W 100 = 0x0000"},
		{"FC06 full", &pdu.Request{FunctionCode: pdu.FCWriteSingleRegister, Address: 100, Value: 0xFFFF}, "W 100 = 0xffff"},
		{"FC15", &pdu.Request{FunctionCode: pdu.FCWriteMultipleCoils, Address: 20, Bits: []bool{true, false, true}}, "W 20 x3"},
		{"FC16", &pdu.Request{FunctionCode: pdu.FCWriteMultipleRegisters, Address: 100, Registers: make([]uint16, 123)}, "W 100 x123"},
		{"unknown FC", &pdu.Request{FunctionCode: 0x08}, "?"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatRequestDetail(tc.req); got != tc.want {
				t.Errorf("FormatRequestDetail = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLogRxUsesRequestDetail guards the regression where the RX entry carried
// the response's "OK" detail instead of the request description.
func TestLogRxUsesRequestDetail(t *testing.T) {
	srv := NewServer()
	if err := srv.AddDevice(WithDefaultRegisters(1, "dev", 99)); err != nil {
		t.Fatal(err)
	}
	srv.Log = logcol.NewCollector()

	srv.LogRx(&pdu.Request{FunctionCode: pdu.FCReadHoldingRegisters, Address: 0, Quantity: 5})
	srv.LogResponse(&pdu.Request{FunctionCode: pdu.FCReadHoldingRegisters, Address: 0, Quantity: 5},
		[]byte{pdu.FCReadHoldingRegisters, 0x0A, 0, 5})

	entries := srv.Log.GetAll()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Direction != logcol.Rx || entries[0].Detail != "R 0 x5" {
		t.Errorf("rx = %s %q, want rx %q", entries[0].Direction, entries[0].Detail, "R 0 x5")
	}
	if entries[1].Direction != logcol.Tx || entries[1].Detail != "OK" {
		t.Errorf("tx = %s %q, want tx %q", entries[1].Direction, entries[1].Detail, "OK")
	}
}

// TestLogResponseExceptionDetail pins the numeric exception form used by the
// RTU, ASCII, RTU-over-TCP and TLS listeners. Rust's plain-TCP listener instead
// formats the exception enum's Debug name; see the open item in
// docs/brainstorm/2026-09-19-go-fullstack-rewrite-design.md.
func TestLogResponseExceptionDetail(t *testing.T) {
	srv := NewServer()
	if err := srv.AddDevice(WithDefaultRegisters(1, "dev", 99)); err != nil {
		t.Fatal(err)
	}
	srv.Log = logcol.NewCollector()

	req := &pdu.Request{FunctionCode: pdu.FCReadHoldingRegisters, Address: 500, Quantity: 1}
	srv.LogResponse(req, pdu.BuildException(pdu.FCReadHoldingRegisters, 0x02))

	entries := srv.Log.GetAll()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if got, want := entries[0].Detail, "ERR: exception 0x02"; got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
}
