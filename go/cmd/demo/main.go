// cmd/demo exercises the refactored Go stack end to end: it starts a slave
// connection from the app layer, configures a data-source point and a
// mutation point, polls it with the Go master, and prints the captured
// communication log. It exits when done (no long-running process).
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/app"
	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/master"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func main() {
	state := app.NewState()
	conn := app.NewConnection(
		app.TransportConfig{Type: "tcp", Host: "127.0.0.1", Port: 0},
		app.SlaveTLSConfig{},
	)
	id := state.NextConnectionID()
	if err := conn.AddDevice(1, "demo-device", ""); err != nil {
		log.Fatal(err)
	}
	state.AddConnection(id, conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := conn.Start(ctx); err != nil {
		log.Fatal(err)
	}

	// Demo points: a counter data source at HR 300, an increment mutation at HR 301.
	if err := state.SetDataSource(app.ParsePointKey(id, 1, register.HoldingRegType, 300), datasource.Config{
		Source:           datasource.Source{Type: datasource.KindCounter, Start: 1000, Step: 7},
		UpdateIntervalMs: 200,
	}); err != nil {
		log.Fatal(err)
	}
	if err := state.SetPointMutation(app.ParsePointKey(id, 1, register.HoldingRegType, 301), register.MutationConfig{
		Enabled: true, Mode: register.MutationModeIncrement, PeriodMs: 200, Step: 5, Min: 0, Max: 60,
	}); err != nil {
		log.Fatal(err)
	}
	state.SetMutationRunning(true)
	loopCtx, stopLoops := context.WithCancel(context.Background())
	defer stopLoops()
	state.StartDataSourceLoop(loopCtx)
	state.StartMutationLoop(loopCtx)

	// Restart the connection on a real free port (transport was port 0).
	_ = conn.Stop()
	port := freePort()
	transport := conn.Transport()
	transport.Port = port
	conn = app.NewConnection(transport, app.SlaveTLSConfig{})
	if err := conn.AddDevice(1, "demo-device", ""); err != nil {
		log.Fatal(err)
	}
	state.AddConnection(id, conn)
	dev, _ := conn.Server().GetDevice(1)
	dev.RegisterMap.WriteHoldingRegisters(100, []uint16{0xCAFE, 0x1234})
	if err := conn.Start(ctx); err != nil {
		log.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	host := "127.0.0.1"
	masterConn := master.NewConnection(
		master.Config{TargetAddress: host, Port: port, SlaveID: 1, TimeoutMs: 2000, Requests: master.DefaultRequestSettings()},
		master.Transport{Type: master.TransportTCP, Host: host, Port: port},
	)
	if err := masterConn.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	defer masterConn.Disconnect()

	res, err := masterConn.Read(ctx, master.ReadHoldingRegisters, 100, 2)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("FC03 read @100 x2      -> %04X %04X\n", res.Registers[0], res.Registers[1])

	if err := masterConn.WriteSingleRegister(ctx, 100, 0x0A0B); err != nil {
		log.Fatal(err)
	}
	res, _ = masterConn.Read(ctx, master.ReadHoldingRegisters, 100, 1)
	fmt.Printf("FC06 write 100=0x0A0B  -> read back %04X\n", res.Registers[0])

	// Let the data source and mutation tick a few times.
	time.Sleep(700 * time.Millisecond)
	for _, addr := range []uint16{300, 301} {
		v, _ := state.ReadRegister(id, 1, register.HoldingRegType, addr)
		fmt.Printf("point HR%-13d -> %d\n", addr, v)
	}

	logs := conn.Log().GetAll()
	fmt.Printf("\ncommunication log (%d entries, first 4):\n", len(logs))
	for i, e := range logs {
		if i >= 4 {
			break
		}
		fmt.Printf("  %s %s %s %q\n", e.Timestamp.Format("15:04:05.000"), e.Direction, e.FunctionCode.Name(), e.Detail)
	}

	_ = conn.Stop()
	fmt.Println("\ndemo finished, everything stopped.")
}

func freePort() uint16 {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	return uint16(ln.Addr().(*net.TCPAddr).Port)
}
