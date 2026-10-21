package logcol

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewEntry(t *testing.T) {
	e := NewEntry(Rx, FCReadHoldingRegisters, "R 0 x10")
	if e.Direction != Rx || e.FunctionCode != FCReadHoldingRegisters || e.Detail != "R 0 x10" {
		t.Errorf("entry = %+v", e)
	}
	if e.RawBytes != nil {
		t.Error("raw bytes should be nil")
	}
}

func TestNewEntryWithRaw(t *testing.T) {
	raw := []byte{0x01, 0x06, 0x00, 0x0A, 0x00, 0x2A}
	e := NewEntryWithRaw(Tx, FCWriteSingleRegister, "W 10 = 42", raw)
	if len(e.RawBytes) != 6 {
		t.Errorf("raw len = %d", len(e.RawBytes))
	}
}

func TestFunctionCodeFromU8(t *testing.T) {
	if fc, ok := FromU8(0x01); !ok || fc != FCReadCoils {
		t.Error("FC01")
	}
	if fc, ok := FromU8(0x10); !ok || fc != FCWriteMultipleRegisters {
		t.Error("FC16")
	}
	if _, ok := FromU8(0xFF); ok {
		t.Error("0xFF should be unsupported")
	}
}

func TestFunctionCodeName(t *testing.T) {
	if FCReadHoldingRegisters.Name() != "FC03" || FCWriteMultipleCoils.Name() != "FC15" {
		t.Error("names")
	}
}

func TestCSVRow(t *testing.T) {
	e := NewEntry(Rx, FCReadHoldingRegisters, "R 0 x2")
	row := e.CSVRow()
	if !strings.Contains(row, "RX") || !strings.Contains(row, "FC03") || !strings.Contains(row, `"R 0 x2"`) {
		t.Errorf("row = %s", row)
	}
}

func TestCollectorBounded(t *testing.T) {
	c := NewCollectorWithCapacity(3)
	for i := 0; i < 5; i++ {
		c.TryAdd(NewEntry(Tx, FCReadCoils, "n"))
	}
	all := c.GetAll()
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
	if all[0].Detail != "n" {
		t.Error("entries should be uniform")
	}
}

func TestCollectorClearAndExport(t *testing.T) {
	c := NewCollector()
	c.TryAdd(NewEntry(Rx, FCReadCoils, "R 0 x1"))
	if csv := c.ExportCSV(); !strings.HasPrefix(csv, CSVHeader) {
		t.Errorf("csv = %q", csv)
	}
	if txt := c.ExportText(); !strings.Contains(txt, "FC01") {
		t.Errorf("text = %q", txt)
	}
	c.Clear()
	if len(c.GetAll()) != 0 {
		t.Error("clear failed")
	}
}

func TestCollectorConcurrent(t *testing.T) {
	c := NewCollectorWithCapacity(100)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.TryAdd(NewEntry(Rx, FCReadCoils, "x"))
				c.GetAll()
			}
		}()
	}
	wg.Wait()
}

func TestDefaultCapacityMatchesRust(t *testing.T) {
	if DefaultCapacity != 10000 {
		t.Errorf("DefaultCapacity = %d, want 10000", DefaultCapacity)
	}
}

func TestAppendCallback(t *testing.T) {
	c := NewCollectorWithCapacity(10)
	var seen []string
	c.SetAppendCallback(func(e Entry) { seen = append(seen, e.Detail) })

	c.TryAdd(NewEntry(Rx, FCReadCoils, "first"))
	c.TryAdd(NewEntry(Tx, FCWriteSingleRegister, "second"))

	if strings.Join(seen, ",") != "first,second" {
		t.Fatalf("callback saw %v", seen)
	}
}

// The callback runs after the lock is released, so it may read the collector
// back -- Rust drops the write guard before calling for the same reason.
func TestAppendCallbackMayReadCollector(t *testing.T) {
	c := NewCollectorWithCapacity(10)
	done := make(chan int, 1)
	c.SetAppendCallback(func(Entry) { done <- len(c.GetAll()) })

	c.TryAdd(NewEntry(Rx, FCReadCoils, "a"))
	select {
	case n := <-done:
		if n != 1 {
			t.Errorf("len seen by callback = %d, want 1", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback deadlocked reading the collector")
	}
}

func TestSetAppendCallbackNilDisables(t *testing.T) {
	c := NewCollector()
	calls := 0
	c.SetAppendCallback(func(Entry) { calls++ })
	c.TryAdd(NewEntry(Rx, FCReadCoils, "a"))
	c.SetAppendCallback(nil)
	c.TryAdd(NewEntry(Rx, FCReadCoils, "b"))
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

// Evicted entries must not fire the callback: only what is appended is seen.
func TestAppendCallbackSeesEvictedRing(t *testing.T) {
	c := NewCollectorWithCapacity(2)
	var seen []string
	c.SetAppendCallback(func(e Entry) { seen = append(seen, e.Detail) })
	for _, d := range []string{"1", "2", "3"} {
		c.TryAdd(NewEntry(Rx, FCReadCoils, d))
	}
	if strings.Join(seen, ",") != "1,2,3" {
		t.Errorf("callback saw %v", seen)
	}
	if got := c.GetAll(); len(got) != 2 || got[0].Detail != "2" {
		t.Errorf("ring = %v", got)
	}
}

func TestGetRecent(t *testing.T) {
	c := NewCollectorWithCapacity(10)
	for i := 0; i < 5; i++ {
		c.TryAdd(NewEntry(Rx, FCReadCoils, string(rune('a'+i))))
	}
	recent := c.GetRecent(2)
	if len(recent) != 2 || recent[0].Detail != "d" || recent[1].Detail != "e" {
		t.Fatalf("recent = %v", recent)
	}
	if len(c.GetRecent(100)) != 5 {
		t.Error("n above length should clamp")
	}
	if len(c.GetRecent(0)) != 0 {
		t.Error("n = 0 should return nothing")
	}
	if len(c.GetRecent(-1)) != 5 {
		t.Error("negative n should return everything, like Rust's saturating_sub")
	}
}

func TestCountWithinBoundary(t *testing.T) {
	c := NewCollectorWithCapacity(10)
	old := NewEntry(Rx, FCReadCoils, "old")
	old.Timestamp = time.Now().UTC().Add(-10 * time.Second)
	c.TryAdd(old)
	for i := 0; i < 3; i++ {
		c.TryAdd(NewEntry(Rx, FCReadCoils, "new"))
	}
	if got := c.CountWithin(time.Second); got != 3 {
		t.Errorf("CountWithin = %d, want 3", got)
	}
	if got := c.CountWithin(time.Hour); got != 4 {
		t.Errorf("CountWithin(1h) = %d, want 4", got)
	}
}

func TestExportTextFormat(t *testing.T) {
	c := NewCollector()
	e := NewEntry(Tx, FCWriteSingleRegister, "W 10 = 42")
	e.Timestamp = time.Date(2026, 3, 5, 7, 8, 9, 123000000, time.UTC)
	c.TryAdd(e)
	if got, want := c.ExportText(), "[2026-03-05 07:08:09.123] TX FC06 - W 10 = 42\n"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}
