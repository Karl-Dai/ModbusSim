package logcol

import (
	"strings"
	"sync"
	"testing"
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
