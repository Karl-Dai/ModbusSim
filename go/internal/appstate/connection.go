package appstate

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"

	"github.com/Karl-Dai/ModbusSim/go/internal/logcol"
	"github.com/Karl-Dai/ModbusSim/go/internal/project"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// ConnectionState is the lifecycle state of one connection. The spellings match
// Rust's Debug output, which is what the UI displays.
type ConnectionState string

const (
	Stopped ConnectionState = "Stopped"
	Running ConnectionState = "Running"
)

// defaultMaxAddress is the 0..=20000 sweep a newly created connection's default
// device gets, mirroring Rust's create_slave_connection.
const defaultMaxAddress = 20000

// ConnectionInfo is the DTO the UI lists and receives on create.
type ConnectionInfo struct {
	ID          string
	BindAddress string
	Port        uint16
	State       ConnectionState
	DeviceCount int
	// Clients is nil for the serial transports, which have no peer list.
	Clients []slave.ClientInfo
}

// connection owns one slave link: its engine, log collector, simulator and the
// goroutines serving the transport. The Manager's mutex guards its mutable
// fields; the server has its own lock for device data.
type connection struct {
	id        string
	transport Transport

	server    *slave.Server
	simulator *slave.Simulator
	collector *logcol.Collector

	state  ConnectionState
	points map[pointKey]*pointConfig

	cancel context.CancelFunc
	wg     *sync.WaitGroup
}

// start binds the transport and launches its goroutines. Like Rust it replaces
// the client registry first, so a restarted connection never inherits a closed
// one, and serial transports report only a failed bind or a bad certificate --
// a serial port that cannot be opened is logged by the running task instead.
func (c *connection) start() error {
	if c.state == Running {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.server.Clients = slave.NewConnectedClients()

	var wg sync.WaitGroup
	serve := func(run func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := run(); err != nil {
				log.Printf("slave %s: %v", c.id, err)
			}
		}()
	}

	switch c.transport.Type {
	case project.TransportTCP:
		ln, err := c.listen()
		if err != nil {
			cancel()
			return err
		}
		serve(func() error { return slave.RunTCP(ctx, ln, c.server) })
	case project.TransportTCPTLS:
		tlsCfg, err := slave.BuildTLSConfig(tlsConfig(c.transport))
		if err != nil {
			cancel()
			return err
		}
		ln, err := c.listen()
		if err != nil {
			cancel()
			return err
		}
		serve(func() error { return slave.RunTLS(ctx, ln, tlsCfg, c.server) })
	case project.TransportRTUOverTCP:
		ln, err := c.listen()
		if err != nil {
			cancel()
			return err
		}
		serve(func() error { return slave.RunRTUOverTCP(ctx, ln, c.server) })
	case project.TransportRTU:
		cfg, ok := serialConfig(c.transport)
		if !ok {
			cancel()
			return errors.New("Invalid serial port settings")
		}
		serve(func() error { return slave.RunRTUSerial(ctx, cfg, c.server) })
	case project.TransportASCII:
		cfg, ok := serialConfig(c.transport)
		if !ok {
			cancel()
			return errors.New("Invalid serial port settings")
		}
		serve(func() error { return slave.RunASCIISerial(ctx, cfg, c.server) })
	default:
		cancel()
		return fmt.Errorf("unknown transport %q", c.transport.Type)
	}

	c.cancel = cancel
	c.wg = &wg
	c.state = Running
	return nil
}

// stop shuts the transport down and waits for its goroutines, then drops the
// live clients, in that order, as Rust does.
func (c *connection) stop() {
	if c.state == Stopped {
		return
	}
	c.cancel()
	c.wg.Wait()
	if c.server.Clients != nil {
		c.server.Clients.CloseAll()
	}
	c.state = Stopped
}

func (c *connection) listen() (net.Listener, error) {
	address, port := TransportTarget(c.transport)
	return net.Listen("tcp", net.JoinHostPort(address, strconv.Itoa(int(port))))
}

func (c *connection) info() ConnectionInfo {
	address, port := TransportTarget(c.transport)
	info := ConnectionInfo{
		ID:          c.id,
		BindAddress: address,
		Port:        port,
		State:       c.state,
		DeviceCount: len(c.server.ListDevices()),
	}
	if !c.transport.Type.IsSerial() {
		info.Clients = c.server.Clients.List()
	}
	return info
}
