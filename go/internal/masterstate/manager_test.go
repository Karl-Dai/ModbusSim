package masterstate

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type harness struct {
	srv      *slave.Server
	listener net.Listener
	cancel   context.CancelFunc
	port     uint16
}

func serve(t *testing.T, port uint16) *harness {
	t.Helper()
	srv := slave.NewServer()
	for _, id := range []uint8{1, 7} {
		if err := srv.AddDevice(slave.WithDefaultRegisters(id, fmt.Sprint(id), 65535)); err != nil {
			t.Fatal(err)
		}
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{srv, ln, cancel, uint16(ln.Addr().(*net.TCPAddr).Port)}
	go slave.RunTCP(ctx, ln, srv)
	t.Cleanup(func() {
		cancel()
		ln.Close()
		if srv.Clients != nil {
			srv.Clients.CloseAll()
		}
	})
	return h
}
func newTestManager(t *testing.T, h *harness, events Events) (*Manager, string) {
	t.Helper()
	m := NewManager(events)
	t.Cleanup(m.CloseAll)
	s := DefaultSettings()
	s.Transport.Port = h.port
	s.TimeoutMS = 50
	s.Reconnect.InitialDelayMS = 20
	s.Reconnect.MaxDelayMS = 40
	info, err := m.CreateConnection(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Connect(info.ID); err != nil {
		t.Fatal(err)
	}
	return m, info.ID
}
func eventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func group() ScanGroup {
	return ScanGroup{Name: "HR", Function: "read_holding_registers", Quantity: 3, IntervalMS: 10, Enabled: true}
}
func TestReadWriteConcurrentPollingAndCache(t *testing.T) {
	h := serve(t, 0)
	m, id := newTestManager(t, h, Events{})
	g, err := m.AddScanGroup(id, group())
	if err != nil {
		t.Fatal(err)
	}
	if err = m.StartPolling(id, g.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := m.WriteSingleRegister(id, uint16(10+n), uint16(n+100)); err != nil {
				t.Error(err)
			}
			r, err := m.ReadOnce(id, ScanGroup{Function: g.Function, StartAddress: uint16(10 + n), Quantity: 1})
			if err != nil {
				t.Error(err)
			} else if len(r.Values) != 1 || r.Values[0].RawValue != uint16(n+100) {
				t.Errorf("crossed response: %+v", r)
			}
		}(i)
	}
	wg.Wait()
	if err = m.WriteMultipleCoils(id, 65533, []bool{true, false, true}); err != nil {
		t.Fatal(err)
	}
	r, err := m.ReadOnce(id, ScanGroup{Function: "read_coils", StartAddress: 65533, Quantity: 3})
	if err != nil || len(r.Values) != 3 || r.Values[2].RawValue != 1 {
		t.Fatalf("bits: %+v %v", r, err)
	}
	for _, fn := range []string{"read_discrete_inputs", "read_input_registers"} {
		if _, err = m.ReadOnce(id, ScanGroup{Function: fn, Quantity: 2}); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.WriteSingleCoil(id, 9, true); err != nil {
		t.Fatal(err)
	}
	if err = m.WriteMultipleRegisters(id, 20, []uint16{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { r, _ := m.GetPollData(id, g.ID); return r != nil && len(r.Values) == 3 })
	for i := 0; i < 10; i++ {
		if err = m.StartPolling(id, g.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.StopPolling(id, g.ID); err != nil {
		t.Fatal(err)
	}
	groups, _ := m.ListScanGroups(id)
	if groups[0].IsPolling {
		t.Fatal("stopped task still registered")
	}
	logs, _ := m.Logs(id)
	if len(logs) == 0 {
		t.Fatal("missing logs")
	}
}
func TestDiscoveryTerminalAndCancel(t *testing.T) {
	h := serve(t, 0)
	ev := make(chan any, 1000)
	m, id := newTestManager(t, h, Events{Emit: func(n string, p any) {
		if n == "scan-slave-progress" || n == "scan-register-progress" {
			ev <- p
		}
	}})
	taskID, err := m.StartSlaveIDScan(id, SlaveScanRequest{1, 8, 10})
	if err != nil {
		t.Fatal(err)
	}
	var terminal SlaveScanEvent
	eventually(t, func() bool {
		for len(ev) > 0 {
			if e, ok := (<-ev).(SlaveScanEvent); ok && e.TaskID == taskID && e.Done {
				terminal = e
				return true
			}
		}
		return false
	})
	if fmt.Sprint(terminal.FoundIDs) != "[1 7]" {
		t.Fatalf("found: %v", terminal.FoundIDs)
	}
	regID, err := m.StartRegisterScan(id, RegisterScanRequest{"read_holding_registers", 65533, 65535, 2, 20})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		for len(ev) > 0 {
			if e, ok := (<-ev).(RegisterScanEvent); ok && e.TaskID == regID && e.Done {
				return e.FoundCount == 3 && e.CurrentAddress == 65535
			}
		}
		return false
	})
	scanID, err := m.StartSlaveIDScan(id, SlaveScanRequest{1, 247, 20})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.CancelScan(id, "slave_scan"); err != nil {
		t.Fatal(err)
	}
	count := 0
	for len(ev) > 0 {
		if e, ok := (<-ev).(SlaveScanEvent); ok && e.TaskID == scanID && (e.Done || e.Cancelled) {
			count++
			if !e.Cancelled {
				t.Fatal("expected cancelled")
			}
		}
	}
	if count != 1 {
		t.Fatalf("terminal count=%d", count)
	}
}
func TestProjectTransactionalLoadAndEffectiveStation(t *testing.T) {
	h := serve(t, 0)
	m, id := newTestManager(t, h, Events{})
	g := group()
	g.SlaveID = nil
	m.AddScanGroup(id, g)
	path := filepath.Join(t.TempDir(), "test.modbusproj")
	if err := m.SaveProject(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if raw["Version"] != float64(2) {
		t.Fatal("wrong version")
	}
	bad := filepath.Join(t.TempDir(), "bad.modbusproj")
	os.WriteFile(bad, []byte(`{"Version":99}`), 0600)
	if _, err := m.LoadProject(bad); err == nil {
		t.Fatal("accepted bad file")
	}
	if m.ListConnections()[0].State != "Connected" {
		t.Fatal("failed load changed session")
	}
	if count, err := m.LoadProject(path); err != nil || count != 1 {
		t.Fatalf("load %d %v", count, err)
	}
	if m.ListConnections()[0].State != "Disconnected" {
		t.Fatal("load auto-connected")
	}
	gs, _ := m.ListScanGroups(id)
	if gs[0].SlaveID == nil || *gs[0].SlaveID != 1 {
		t.Fatal("effective station not persisted")
	}
	s := DefaultSettings()
	info, err := m.CreateConnection(s)
	if err != nil || info.ID == id {
		t.Fatalf("ID reused %s %v", info.ID, err)
	}
}
func TestReconnectAndManualDisconnect(t *testing.T) {
	h := serve(t, 0)
	m, id := newTestManager(t, h, Events{})
	g, _ := m.AddScanGroup(id, group())
	m.StartPolling(id, g.ID)
	eventually(t, func() bool { r, _ := m.GetPollData(id, g.ID); return r != nil })
	h.cancel()
	h.listener.Close()
	if h.srv.Clients != nil {
		h.srv.Clients.CloseAll()
	}
	eventually(t, func() bool { return m.ListConnections()[0].State == "Reconnecting" })
	h2 := serve(t, h.port)
	_ = h2
	eventually(t, func() bool {
		r, _ := m.GetPollData(id, g.ID)
		return m.ListConnections()[0].State == "Connected" && r != nil && r.Error == nil
	})
	h2.cancel()
	h2.listener.Close()
	if h2.srv.Clients != nil {
		h2.srv.Clients.CloseAll()
	}
	eventually(t, func() bool { return m.ListConnections()[0].State == "Reconnecting" })
	if err := m.Disconnect(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if m.ListConnections()[0].State != "Disconnected" {
		t.Fatal("connection revived")
	}
}
