package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/appstate"
	"github.com/Karl-Dai/ModbusSim/go/internal/project"
)

// Test the actual bound App surface with a wire peer, rather than a fake Manager.
func TestDesktopBridgeWireEventsAndShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()
	app := newApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	type event struct {
		name    string
		payload interface{}
	}
	events := make(chan event, 64)
	app.emit = func(name string, payload interface{}) { events <- event{name, payload} }
	conn, err := app.CreateConnection(appstate.CreateConnectionRequest{
		Transport: project.Transport{Type: project.TransportTCP, Host: "127.0.0.1", Port: port}, SlaveID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.StartConnection(conn.ID); err != nil {
		t.Fatal(err)
	}
	peer, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strings.TrimPrefix(ln.Addr().String(), "127.0.0.1:")), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	_ = peer.SetDeadline(time.Now().Add(3 * time.Second))
	// FC06: write HR0 = 42 through a real TCP request.
	if _, err := peer.Write([]byte{0, 1, 0, 0, 0, 6, 1, 6, 0, 0, 0, 42}); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 12)
	if _, err := io.ReadFull(peer, response); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(response[10:]) != 42 {
		t.Fatalf("response %x", response)
	}
	value, err := app.ReadRegister(conn.ID, 1, "holding_register", 0)
	if err != nil || value.Value != 42 {
		t.Fatalf("read: %+v %v", value, err)
	}
	var gotChange, gotLog, gotState bool
	deadline := time.After(3 * time.Second)
	for !(gotChange && gotLog && gotState) {
		select {
		case e := <-events:
			data, err := json.Marshal(e.payload)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "connection_id") || strings.Contains(string(data), "register_type") {
				t.Fatalf("legacy DTO leaked: %s", data)
			}
			switch e.name {
			case "register-value-changed":
				change := e.payload.(RegisterValueEvent)
				gotChange = change.ConnectionID == conn.ID && len(change.Changes) == 1 && change.Changes[0].Value == 42
			case "log-appended":
				gotLog = e.payload.(appstate.LogAppendedEvent).ConnectionID == conn.ID
			case "slave-connection-state":
				gotState = e.payload.(appstate.ConnectionStateEvent).State == appstate.Running
			}
		case <-deadline:
			t.Fatal("missing desktop events")
		}
	}
	logs, err := app.Logs(conn.ID)
	if err != nil || len(logs) != 2 || logs[0].FunctionCode != "FC06" || logs[0].Detail != "W 0 = 0x002a" {
		t.Fatalf("logs %+v %v", logs, err)
	}
	connections := app.ListConnections()
	if len(connections) != 1 || len(connections[0].Clients) != 1 {
		t.Fatalf("clients %+v", connections)
	}
	app.shutdown(context.Background())
	if _, err := peer.Read(make([]byte, 1)); err == nil {
		t.Fatal("shutdown left client open")
	}
	if len(app.ListConnections()) != 0 {
		t.Fatal("shutdown kept connections")
	}
}

func TestNativePointConfigAndProjectRoundTrip(t *testing.T) {
	app := newApp()
	defer app.shutdown(context.Background())
	if _, err := app.CreateConnection(appstate.CreateConnectionRequest{Transport: project.Transport{Type: project.TransportTCP, Host: "invalid", Port: 5020}, SlaveID: 1}); err == nil {
		t.Fatal("accepted invalid bind address")
	}
	if len(app.ListConnections()) != 0 {
		t.Fatal("invalid create altered state")
	}
	conn, err := app.CreateConnection(appstate.CreateConnectionRequest{Transport: project.Transport{Type: project.TransportTCP, Host: "127.0.0.1", Port: 5020}, SlaveID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SetPointMutation(SetPointMutationRequest{ConnectionID: conn.ID, SlaveID: 1, RegisterType: "coil", Address: 0, Config: project.MutationConfig{Enabled: true, Mode: "flip", PeriodMS: 500}}); err != nil {
		t.Fatal(err)
	}
	points, err := app.ListRegisters(conn.ID, 1, "coil")
	if err != nil || len(points) == 0 || points[0].Mutation == nil || points[0].Mutation.PeriodMS != 500 {
		t.Fatalf("configs: %+v %v", points, err)
	}
	path := filepath.Join(t.TempDir(), "native.modbusproj")
	if err := app.SaveProject(path); err != nil {
		t.Fatal(err)
	}
	if _, err := app.LoadProject(path); err != nil {
		t.Fatal(err)
	}
	points, err = app.ListRegisters(conn.ID, 1, "coil")
	if err != nil || points[0].Mutation == nil || points[0].Mutation.PeriodMS != 500 {
		t.Fatal("project lost config")
	}
	if app.MutationRunning() {
		t.Fatal("loading enabled mutation unexpectedly")
	}
}
