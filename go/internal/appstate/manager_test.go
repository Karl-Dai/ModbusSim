package appstate

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/mbap"
	"github.com/Karl-Dai/ModbusSim/go/internal/project"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// recorded collects the events a Manager pushes so tests can assert on them.
type recorded struct {
	mu       sync.Mutex
	appended []string
	changes  []RegisterValueEvent
	states   []ConnectionStateEvent
}

func (r *recorded) events() Events {
	return Events{
		LogAppended: func(e LogAppendedEvent) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.appended = append(r.appended, e.ConnectionID)
		},
		RegisterValueChanged: func(e RegisterValueEvent) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.changes = append(r.changes, e)
		},
		ConnectionStateChanged: func(e ConnectionStateEvent) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.states = append(r.states, e)
		},
	}
}

func (r *recorded) countChanges(address uint16) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, event := range r.changes {
		for _, change := range event.Changes {
			if change.Address == address {
				n++
			}
		}
	}
	return n
}

func (r *recorded) stateOf(id string) []ConnectionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ConnectionState
	for _, event := range r.states {
		if event.ID == id {
			out = append(out, event.State)
		}
	}
	return out
}

func (r *recorded) logCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.appended)
}

// snapshotChanges copies the register-value events seen so far.
func (r *recorded) snapshotChanges() []RegisterValueEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RegisterValueEvent, len(r.changes))
	copy(out, r.changes)
	return out
}

// freePort asks the kernel for a port, then releases it. A busy machine can race
// this, so tests using it fail loudly rather than silently skipping.
func freePort(t *testing.T) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return uint16(port)
}

func tcpTransport(port uint16) Transport {
	return Transport{Type: project.TransportTCP, Host: "127.0.0.1", Port: port}
}

func newTestConnection(t *testing.T, rec *recorded) (*Manager, uint16) {
	t.Helper()
	port := freePort(t)
	m := NewManager(rec.events())
	t.Cleanup(m.CloseAll)
	if _, err := m.CreateConnection(CreateConnectionRequest{
		Transport: tcpTransport(port), SlaveID: 1, Name: "dev",
	}); err != nil {
		t.Fatal(err)
	}
	return m, port
}

func TestTransportTargetAndValidation(t *testing.T) {
	tcp := Transport{Type: project.TransportTCP, Host: "0.0.0.0", Port: 15020}
	if addr, port := TransportTarget(tcp); addr != "0.0.0.0" || port != 15020 {
		t.Errorf("target = %s:%d", addr, port)
	}
	rtu := Transport{
		Type:   project.TransportRTU,
		Serial: &project.SerialConfig{Port: "/dev/tty.usb", BaudRate: 9600, DataBits: 8, StopBits: 1, Parity: project.ParityNone},
	}
	if addr, port := TransportTarget(rtu); addr != "/dev/tty.usb" || port != 0 {
		t.Errorf("serial target = %s:%d, want port name with 0", addr, port)
	}

	cases := []struct {
		name string
		have Transport
		want string
	}{
		{"hostname", Transport{Type: project.TransportTCP, Host: "proxy.internal", Port: 15020}, "Bind address must be an IP address"},
		{"zero port", Transport{Type: project.TransportTCP, Host: "127.0.0.1"}, "Port must be between 1 and 65535"},
		{"missing serial", Transport{Type: project.TransportRTU}, "Invalid serial port settings"},
		{"zero baud", Transport{Type: project.TransportASCII, Serial: &project.SerialConfig{Port: "a", DataBits: 8, StopBits: 1}}, "Invalid serial port settings"},
		{"unknown", Transport{Type: project.TransportType("carrier_pigeon")}, `unknown transport "carrier_pigeon"`},
	}
	for _, tc := range cases {
		err := ValidateTransport(tc.have)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}

	for _, ok := range []Transport{
		{Type: project.TransportTCPTLS, Host: "::1", Port: 15020},
		{Type: project.TransportRTUOverTCP, Host: "10.0.0.1", Port: 1},
		rtu,
	} {
		if err := ValidateTransport(ok); err != nil {
			t.Errorf("%s: unexpected %v", ok.Type, err)
		}
	}
}

func TestCreateMintsIDsAndSeedsDefaultDevice(t *testing.T) {
	rec := &recorded{}
	m := NewManager(rec.events())
	defer m.CloseAll()

	first, err := m.CreateConnection(CreateConnectionRequest{Transport: tcpTransport(15020), SlaveID: 7, Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.CreateConnection(CreateConnectionRequest{
		Transport: tcpTransport(15021), SlaveID: 8, Name: "b", InitMode: "random",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "slave_1" || second.ID != "slave_2" {
		t.Fatalf("ids = %s, %s", first.ID, second.ID)
	}
	if first.State != Stopped {
		t.Errorf("new connection state = %s", first.State)
	}
	if first.DeviceCount != 1 {
		t.Errorf("device count = %d", first.DeviceCount)
	}
	if first.BindAddress != "127.0.0.1" && first.BindAddress != "0.0.0.0" {
		t.Errorf("bind = %s", first.BindAddress)
	}
	if first.Clients == nil {
		t.Error("TCP connection must expose a client list")
	}

	serialConn, err := m.CreateConnection(CreateConnectionRequest{
		Transport: Transport{Type: project.TransportRTU, Serial: &project.SerialConfig{
			Port: "/dev/null", BaudRate: 9600, DataBits: 8, StopBits: 1}},
		SlaveID: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if serialConn.Clients != nil {
		t.Errorf("serial clients = %v, want nil", serialConn.Clients)
	}

	if _, err := m.CreateConnection(CreateConnectionRequest{Transport: tcpTransport(15022), SlaveID: 0}); err == nil ||
		err.Error() != "slave_id must be between 1 and 247" {
		t.Errorf("slave_id 0: err = %v", err)
	}
	if _, err := m.CreateConnection(CreateConnectionRequest{Transport: tcpTransport(15022), SlaveID: 248}); err == nil {
		t.Error("slave_id 248 accepted")
	}

	list := m.ListConnections()
	if len(list) != 3 || list[0].ID != "slave_1" || list[2].ID != "slave_3" {
		t.Fatalf("list = %+v", list)
	}

	// The default device sweeps the four areas over 0..=20000.
	defs, err := m.ListRegisters("slave_1", 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != (defaultMaxAddress+1)*4 {
		t.Errorf("default def count = %d, want %d", len(defs), (defaultMaxAddress+1)*4)
	}
	if value, err := m.ReadRegister("slave_1", 7, "holding_register", 1234); err != nil || value.Value != 0 {
		t.Errorf("zero-seeded register = %+v (%v)", value, err)
	}
	// "random" must actually seed values: the chance of all 20001 words being
	// zero is nil.
	anyNonZero := false
	values, err := m.ReadRegistersBulk("slave_2", 8, "holding_register")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if v.Value != 0 {
			anyNonZero = true
			break
		}
	}
	if !anyNonZero {
		t.Error("random init produced only zeros")
	}
}

func TestTCPConnectionServesAndNotifies(t *testing.T) {
	rec := &recorded{}
	m, port := newTestConnection(t, rec)
	if err := m.StartConnection("slave_1"); err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := mbap.WriteFrame(conn, 3, 1, []byte{0x06, 0x00, 0x0A, 0x2A, 0x2B}); err != nil {
		t.Fatal(err)
	}
	header, resp, err := mbap.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if header.TransactionID != 3 || resp[0] != 0x06 {
		t.Fatalf("header = %+v resp = % X", header, resp)
	}

	// A remote write is reported to the UI, and the exchange is logged.
	deadline := time.Now().Add(2 * time.Second)
	for rec.countChanges(0x0A) == 0 || rec.logCount() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("changes = %+v, log events = %d", rec.changes, rec.logCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
	found := false
	for _, def := range mustDefs(t, m, 1) {
		if def.Address == 0x0A && def.RegisterType == register.HoldingRegType {
			found = true
		}
	}
	if !found {
		t.Error("remote write did not land in the register map")
	}

	// The live client is listed while connected.
	if info := connectionByID(t, m, "slave_1"); len(info.Clients) != 1 {
		t.Errorf("clients = %+v", info.Clients)
	} else if info.Clients[0].PeerAddress == "" {
		t.Error("client has no peer address")
	}

	if err := m.StopConnection("slave_1"); err != nil {
		t.Fatal(err)
	}
	if info := connectionByID(t, m, "slave_1"); len(info.Clients) != 0 {
		t.Errorf("clients after stop = %+v", info.Clients)
	}
	states := rec.stateOf("slave_1")
	if len(states) != 2 || states[0] != Running || states[1] != Stopped {
		t.Errorf("state events = %v", states)
	}
}

func TestStartReportsBindFailureAndRestartReplacesRegistry(t *testing.T) {
	rec := &recorded{}
	m, port := newTestConnection(t, rec)

	occupied, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	err = m.StartConnection("slave_1")
	if err == nil {
		t.Fatal("binding a taken port succeeded")
	}
	if info := connectionByID(t, m, "slave_1"); info.State != Stopped {
		t.Errorf("state after failed start = %s", info.State)
	}

	if err := occupied.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.StartConnection("slave_1"); err != nil {
		t.Fatalf("second start: %v", err)
	}
	if err := m.StartConnection("slave_1"); err != nil {
		t.Fatalf("starting twice: %v", err)
	}
	if states := rec.stateOf("slave_1"); len(states) != 2 {
		t.Errorf("state events = %v, want one per successful start", states)
	}
}

func TestConnectionNotFoundAndDelete(t *testing.T) {
	rec := &recorded{}
	m, _ := newTestConnection(t, rec)

	if _, err := m.ListDevices("nope"); err == nil || err.Error() != "connection nope not found" {
		t.Errorf("err = %v", err)
	}
	if _, err := m.GetTransport("nope"); err == nil || err.Error() != "Connection not found" {
		t.Errorf("GetTransport err = %v", err)
	}
	if err := m.DeleteConnection("slave_1"); err != nil {
		t.Fatal(err)
	}
	if list := m.ListConnections(); len(list) != 0 {
		t.Errorf("connections left = %+v", list)
	}
	// IDs are never reused, even after a delete.
	again, err := m.CreateConnection(CreateConnectionRequest{Transport: tcpTransport(15030), SlaveID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != "slave_2" {
		t.Errorf("id after delete = %s", again.ID)
	}
}

func TestUpdateTransportRules(t *testing.T) {
	rec := &recorded{}
	m, port := newTestConnection(t, rec)

	bad := tcpTransport(port)
	bad.Host = "localhost"
	if err := m.UpdateTransport("slave_1", bad); err == nil ||
		err.Error() != "Bind address must be an IP address" {
		t.Errorf("bad address: err = %v", err)
	}

	if err := m.StartConnection("slave_1"); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateTransport("slave_1", tcpTransport(freePort(t))); err == nil ||
		err.Error() != "Stop the connection before changing its settings" {
		t.Errorf("running: err = %v", err)
	}
	if err := m.StopConnection("slave_1"); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateTransport("slave_1", Transport{Type: project.TransportRTUOverTCP, Host: "127.0.0.1", Port: 15040}); err == nil ||
		err.Error() != "Changing transport type requires a new connection" {
		t.Errorf("type change: err = %v", err)
	}

	updated := tcpTransport(freePort(t))
	if err := m.UpdateTransport("slave_1", updated); err != nil {
		t.Fatal(err)
	}
	if got, err := m.GetTransport("slave_1"); err != nil || got.Port != updated.Port {
		t.Errorf("transport = %+v (%v)", got, err)
	}
}

func TestDeviceLifecycleMovesPointConfigs(t *testing.T) {
	rec := &recorded{}
	m, _ := newTestConnection(t, rec)
	if _, err := m.AddDevice(AddDeviceRequest{ConnectionID: "slave_1", SlaveID: 20, Name: "empty"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddDevice(AddDeviceRequest{ConnectionID: "slave_1", SlaveID: 21, Name: "zero", InitMode: "zero"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddDevice(AddDeviceRequest{ConnectionID: "slave_1", SlaveID: 20, Name: "dup"}); err == nil {
		t.Error("duplicate slave id accepted")
	}
	if _, err := m.AddDevice(AddDeviceRequest{ConnectionID: "slave_1", SlaveID: 0}); err == nil {
		t.Error("slave id 0 accepted")
	}

	if err := m.AddRegister(AddRegisterRequest{
		ConnectionID: "slave_1", SlaveID: 20, Address: 5, RegisterType: "holding_register", DataType: "uint16",
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPointMutation(SetPointMutationRequest{
		ConnectionID: "slave_1", SlaveID: 20, RegisterType: "holding_register", Address: 5,
		Config: mutationEnabled(),
	}); err != nil {
		t.Fatal(err)
	}

	devices, err := m.ListDevices("slave_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 3 || devices[0].SlaveID != 1 || devices[2].SlaveID != 21 {
		t.Fatalf("devices = %+v", devices)
	}
	if devices[1].RegisterCount != 1 {
		t.Errorf("empty device register count = %d", devices[1].RegisterCount)
	}
	if devices[2].RegisterCount != (defaultMaxAddress+1)*4 {
		t.Errorf("zero device register count = %d", devices[2].RegisterCount)
	}

	info, err := m.UpdateDevice(UpdateDeviceRequest{ConnectionID: "slave_1", OriginalSlaveID: 20, SlaveID: 22, Name: "renamed"})
	if err != nil {
		t.Fatal(err)
	}
	if info.RegisterCount != 1 || info.Name != "renamed" {
		t.Errorf("info = %+v", info)
	}
	listed, err := m.ListPointMutations("slave_1", 22)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Address != 5 {
		t.Fatalf("mutation config lost on rename: %+v", listed)
	}
	if _, err := m.ListPointMutations("slave_1", 20); err == nil || err.Error() != "slave 20 not found" {
		t.Errorf("old id still served: err = %v", err)
	}

	if err := m.RemoveDevice("slave_1", 22); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveDevice("slave_1", 22); err == nil {
		t.Error("removing twice succeeded")
	}
	if listed, err := m.ListPointMutations("slave_1", 22); err == nil {
		t.Errorf("mutations survive device removal: %+v", listed)
	}
}

func connectionByID(t *testing.T, m *Manager, id string) ConnectionInfo {
	t.Helper()
	for _, info := range m.ListConnections() {
		if info.ID == id {
			return info
		}
	}
	t.Fatalf("connection %s missing", id)
	return ConnectionInfo{}
}

// connectionFor hands a test the connection behind an id so it can inspect the
// point registry. Nothing else writes that map while the test reads it.
func connectionFor(t *testing.T, m *Manager, id string) *connection {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := m.connection(id)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func mustDefs(t *testing.T, m *Manager, slaveID uint8) []register.RegisterDef {
	t.Helper()
	defs, err := m.ListRegisters("slave_1", slaveID, "")
	if err != nil {
		t.Fatal(err)
	}
	return defs
}

// clientInfo is referenced so the DTO's shape stays tied to the engine's.
var _ = slave.ClientInfo{}
