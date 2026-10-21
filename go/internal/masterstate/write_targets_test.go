package masterstate

import "testing"

func TestScanGroupWritesAndStoppedReadbackUseSameSlave(t *testing.T) {
	h := serve(t, 0)
	m, id := newTestManager(t, h, Events{})
	target := uint8(7)
	group, err := m.AddScanGroup(id, ScanGroup{Name: "Slave 7 coils", Function: "read_coils", Quantity: 2, IntervalMS: 1000, Enabled: true, SlaveID: &target})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.WriteSingleCoilSlave(id, &target, 0, true); err != nil {
		t.Fatal(err)
	}
	result, err := m.RefreshScanGroup(id, group.ID)
	if err != nil || len(result.Values) != 2 || result.Values[0].RawValue != 1 {
		t.Fatalf("readback: %+v %v", result, err)
	}
	cached, err := m.GetPollData(id, group.ID)
	if err != nil || cached == nil || cached.Values[0].RawValue != 1 {
		t.Fatalf("cache: %+v %v", cached, err)
	}
	defaultRead, err := m.ReadOnce(id, ScanGroup{Function: "read_coils", Quantity: 1})
	if err != nil || defaultRead.Values[0].RawValue != 0 {
		t.Fatalf("default station was modified: %+v %v", defaultRead, err)
	}
	if err := m.WriteMultipleCoilsSlave(id, &target, 0, []bool{false, true}); err != nil {
		t.Fatal(err)
	}
	result, err = m.RefreshScanGroup(id, group.ID)
	if err != nil || result.Values[0].RawValue != 0 || result.Values[1].RawValue != 1 {
		t.Fatalf("multiple coils: %+v %v", result, err)
	}
	if err := m.WriteSingleRegisterSlave(id, &target, 0, 42); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteMultipleRegistersSlave(id, &target, 1, []uint16{43, 44}); err != nil {
		t.Fatal(err)
	}
	words, err := m.ReadOnce(id, ScanGroup{Function: "read_holding_registers", Quantity: 3, SlaveID: &target})
	if err != nil || words.Values[0].RawValue != 42 || words.Values[2].RawValue != 44 {
		t.Fatalf("register targets: %+v %v", words, err)
	}
	defaultRead, err = m.ReadOnce(id, ScanGroup{Function: "read_holding_registers", Quantity: 3})
	if err != nil || defaultRead.Values[0].RawValue != 0 || defaultRead.Values[2].RawValue != 0 {
		t.Fatalf("default registers modified: %+v %v", defaultRead, err)
	}
	if err := m.WriteSingleCoilSlave(id, nil, 0, true); err != nil {
		t.Fatal(err)
	}
	defaultRead, err = m.ReadOnce(id, ScanGroup{Function: "read_coils", Quantity: 1})
	if err != nil || defaultRead.Values[0].RawValue != 1 {
		t.Fatalf("nil target: %+v %v", defaultRead, err)
	}
	for _, invalid := range []uint8{0, 248, 255} {
		if m.WriteSingleCoilSlave(id, &invalid, 0, true) == nil {
			t.Fatal("invalid station accepted")
		}
	}
}
