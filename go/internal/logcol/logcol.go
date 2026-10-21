// Package logcol implements communication log entries and a bounded,
// thread-safe log collector. Ported from crates/modbussim-core/src/
// log_entry.rs and log_collector.rs with identical semantics.
package logcol

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Direction of a Modbus communication frame.
type Direction string

const (
	Rx Direction = "rx" // received (inbound)
	Tx Direction = "tx" // sent (outbound)
)

// String returns the two-letter display form used in logs and CSV.
func (d Direction) String() string {
	if d == Tx {
		return "TX"
	}
	return "RX"
}

// FunctionCode is a Modbus function code appearing in log entries.
type FunctionCode uint8

const (
	FCReadCoils              FunctionCode = 0x01
	FCReadDiscreteInputs     FunctionCode = 0x02
	FCReadHoldingRegisters   FunctionCode = 0x03
	FCReadInputRegisters     FunctionCode = 0x04
	FCWriteSingleCoil        FunctionCode = 0x05
	FCWriteSingleRegister    FunctionCode = 0x06
	FCWriteMultipleCoils     FunctionCode = 0x0F
	FCWriteMultipleRegisters FunctionCode = 0x10
)

// FromU8 converts a raw byte to a FunctionCode, or 0 when unsupported.
func FromU8(v uint8) (FunctionCode, bool) {
	switch FunctionCode(v) {
	case FCReadCoils, FCReadDiscreteInputs, FCReadHoldingRegisters, FCReadInputRegisters,
		FCWriteSingleCoil, FCWriteSingleRegister, FCWriteMultipleCoils, FCWriteMultipleRegisters:
		return FunctionCode(v), true
	}
	return 0, false
}

// Name returns the "FCxx" display name.
func (fc FunctionCode) Name() string {
	switch fc {
	case FCReadCoils:
		return "FC01"
	case FCReadDiscreteInputs:
		return "FC02"
	case FCReadHoldingRegisters:
		return "FC03"
	case FCReadInputRegisters:
		return "FC04"
	case FCWriteSingleCoil:
		return "FC05"
	case FCWriteSingleRegister:
		return "FC06"
	case FCWriteMultipleCoils:
		return "FC15"
	case FCWriteMultipleRegisters:
		return "FC16"
	}
	return "FC??"
}

// Entry is a single entry in the communication log.
type Entry struct {
	Timestamp    time.Time    `json:"timestamp"`
	Direction    Direction    `json:"direction"`
	FunctionCode FunctionCode `json:"function_code"`
	Detail       string       `json:"detail"`
	RawBytes     []byte       `json:"raw_bytes,omitempty"`
}

// NewEntry creates an entry stamped with the current UTC time.
func NewEntry(direction Direction, fc FunctionCode, detail string) Entry {
	return Entry{Timestamp: time.Now().UTC(), Direction: direction, FunctionCode: fc, Detail: detail}
}

// NewEntryWithRaw creates an entry with raw frame bytes included.
func NewEntryWithRaw(direction Direction, fc FunctionCode, detail string, raw []byte) Entry {
	e := NewEntry(direction, fc, detail)
	e.RawBytes = raw
	return e
}

// CSVRow formats the entry for CSV export, matching Rust's to_csv_row:
// "Timestamp,Direction,Function,Detail,RawBytes" with quoted detail/raw.
func (e Entry) CSVRow() string {
	ts := e.Timestamp.Format("2006-01-02 15:04:05.000")
	raw := ""
	if len(e.RawBytes) > 0 {
		parts := make([]string, len(e.RawBytes))
		for i, b := range e.RawBytes {
			parts[i] = fmt.Sprintf("%02X", b)
		}
		raw = strings.Join(parts, " ")
	}
	return fmt.Sprintf("%q,%s,%s,%q,%q", ts, e.Direction, e.FunctionCode.Name(), e.Detail, raw)
}

// CSVHeader is the CSV header row.
const CSVHeader = "Timestamp,Direction,Function,Detail,RawBytes"

// DefaultCapacity mirrors Rust's MAX_LOG_ENTRIES.
const DefaultCapacity = 10000

// AppendCallback runs once per appended entry, e.g. to emit a GUI event. It is
// called after the collector's lock is released, so it may read from the
// collector; keep it fast and non-blocking all the same.
type AppendCallback func(entry Entry)

// Collector is a bounded, thread-safe ring of recent log entries.
type Collector struct {
	mu       sync.Mutex
	entries  []Entry
	capacity int
	dropped  uint64
	onAppend AppendCallback
}

// NewCollector creates a collector with the default capacity.
func NewCollector() *Collector {
	return NewCollectorWithCapacity(DefaultCapacity)
}

// NewCollectorWithCapacity creates a collector with an explicit capacity.
func NewCollectorWithCapacity(capacity int) *Collector {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Collector{capacity: capacity}
}

// SetAppendCallback installs (or with nil removes) the on-append callback.
// Safe to call at any time, mirroring LogCollector::set_append_callback.
func (c *Collector) SetAppendCallback(cb AppendCallback) {
	c.mu.Lock()
	c.onAppend = cb
	c.mu.Unlock()
}

// TryAdd appends an entry, evicting the oldest when full, and notifies the
// append callback outside the lock.
func (c *Collector) TryAdd(entry Entry) {
	c.mu.Lock()
	if len(c.entries) >= c.capacity {
		c.entries = c.entries[1:]
		c.dropped++
	}
	c.entries = append(c.entries, entry)
	cb := c.onAppend
	c.mu.Unlock()

	if cb != nil {
		cb(entry)
	}
}

// GetAll returns a snapshot of all buffered entries (oldest first).
func (c *Collector) GetAll() []Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Entry, len(c.entries))
	copy(out, c.entries)
	return out
}

// GetRecent returns up to the n newest entries, oldest first.
func (c *Collector) GetRecent(n int) []Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n < 0 || n > len(c.entries) {
		n = len(c.entries)
	}
	out := make([]Entry, n)
	copy(out, c.entries[len(c.entries)-n:])
	return out
}

// CountWithin counts entries newer than the window. Entries are appended in
// time order, so the scan starts at the newest end (Rust's take_while) and
// stops at the first stale entry.
func (c *Collector) CountWithin(window time.Duration) int {
	cutoff := time.Now().UTC().Add(-window)
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for i := len(c.entries) - 1; i >= 0; i-- {
		if c.entries[i].Timestamp.Before(cutoff) {
			break
		}
		n++
	}
	return n
}

// Clear empties the collector.
func (c *Collector) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

// ExportCSV renders the buffered entries plus header as CSV text.
func (c *Collector) ExportCSV() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	b.WriteString(CSVHeader)
	b.WriteByte('\n')
	for _, e := range c.entries {
		b.WriteString(e.CSVRow())
		b.WriteByte('\n')
	}
	return b.String()
}

// ExportText renders the entries in the human-readable log format, matching
// Rust's "[timestamp] DIR FC - detail" lines.
func (c *Collector) ExportText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	for _, e := range c.entries {
		ts := e.Timestamp.Format("2006-01-02 15:04:05.000")
		fmt.Fprintf(&b, "[%s] %s %s - %s\n", ts, e.Direction, e.FunctionCode.Name(), e.Detail)
	}
	return b.String()
}
