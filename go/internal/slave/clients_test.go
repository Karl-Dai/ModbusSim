// Tests for the connected-client registry and its wiring into the TCP-family
// listeners.
package slave

import (
	"bufio"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/Karl-Dai/ModbusSim/go/internal/mbap"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func pipePair() (net.Conn, net.Conn) {
	return net.Pipe()
}

func TestConnectedClientsTrackAndList(t *testing.T) {
	c := NewConnectedClients()

	clientA, serverA := pipePair()
	defer clientA.Close()
	guardA, err := c.Track(serverA)
	if err != nil {
		t.Fatalf("track A: %v", err)
	}

	clientB, serverB := pipePair()
	defer clientB.Close()
	if _, err := c.Track(serverB); err != nil {
		t.Fatalf("track B: %v", err)
	}

	list := c.List()
	if len(list) != 2 {
		t.Fatalf("list = %+v, want 2 entries", list)
	}
	// IDs ascend in registration order, mirroring Rust's BTreeMap iteration.
	if list[0].ID != 1 || list[1].ID != 2 {
		t.Errorf("ids = %d,%d, want 1,2", list[0].ID, list[1].ID)
	}
	if list[0].PeerAddress != serverA.RemoteAddr().String() {
		t.Errorf("peer = %q, want %q", list[0].PeerAddress, serverA.RemoteAddr())
	}
	if _, err := time.Parse(time.RFC3339Nano, list[0].ConnectedAt); err != nil {
		t.Errorf("connected_at %q is not RFC3339: %v", list[0].ConnectedAt, err)
	}
	if c.Count() != 2 {
		t.Errorf("count = %d, want 2", c.Count())
	}

	guardA.Release()
	if c.Count() != 1 {
		t.Errorf("count after release = %d, want 1", c.Count())
	}
	if rest := c.List(); len(rest) != 1 || rest[0].ID != 2 {
		t.Errorf("released client still listed: %+v", rest)
	}
	guardA.Release() // idempotent
	if c.Count() != 1 {
		t.Errorf("count = %d, want 1", c.Count())
	}
}

func TestConnectedClientsCloseAll(t *testing.T) {
	c := NewConnectedClients()

	inbound, outbound := pipePair()
	defer outbound.Close()
	guard, err := c.Track(inbound)
	if err != nil {
		t.Fatal(err)
	}

	c.CloseAll()

	// CloseAll shuts the socket down and unregisters it, so the guard's later
	// release is a no-op.
	if _, err := outbound.Read(make([]byte, 1)); err == nil {
		t.Error("client socket should be closed")
	}
	if c.Count() != 0 {
		t.Errorf("count = %d, want 0", c.Count())
	}
	guard.Release()

	// A closed registry stays closed: late arrivals are refused and shut down.
	late, latePeer := pipePair()
	defer late.Close()
	if _, err := c.Track(latePeer); err == nil {
		t.Fatal("track after CloseAll should fail")
	}
	if _, err := late.Read(make([]byte, 1)); err == nil {
		t.Error("refused socket should be closed")
	}
}

func TestNilGuardRelease(t *testing.T) {
	var g *ClientGuard
	g.Release() // must not panic
}

// waitForClients polls until the registry reports want clients; the accept
// loop registers asynchronously.
func waitForClients(t *testing.T, c *ConnectedClients, want int) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if c.Count() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("client count stayed at %d, want %d", c.Count(), want)
}

func dialEcho(t *testing.T, addr string, request []byte) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := mbap.WriteFrame(conn, 7, 1, request); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if _, _, err := mbap.ReadFrame(conn); err != nil {
		conn.Close()
		t.Fatalf("read response: %v", err)
	}
	return conn
}

func TestTCPListenerTracksClients(t *testing.T) {
	srv := testServer(t)
	clients := NewConnectedClients()
	srv.Clients = clients

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = RunTCP(ctx, ln, srv) }()

	// dialEcho proves the connection is being served, not merely tracked.
	conn := dialEcho(t, ln.Addr().String(), []byte{0x03, 0x00, 0x00, 0x00, 0x02})
	defer conn.Close()

	waitForClients(t, clients, 1)
	if got := clients.List()[0].PeerAddress; got != conn.LocalAddr().String() {
		t.Errorf("peer = %q, want %q", got, conn.LocalAddr())
	}

	// CloseAll is what stopping a connection does: live masters get dropped.
	clients.CloseAll()
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Error("server should have closed the client socket")
	}
	waitForClients(t, clients, 0)

	// A refused accept must not hang: the new socket is closed at once.
	refused, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer refused.Close()
	_ = refused.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := refused.Read(make([]byte, 1)); err == nil {
		t.Error("refused client should be disconnected")
	}
	if clients.Count() != 0 {
		t.Errorf("refused client should not be registered, count = %d", clients.Count())
	}
}

// A server with no registry still serves requests; tracking is optional.
func TestTCPListenerWithoutRegistry(t *testing.T) {
	srv := testServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = RunTCP(ctx, ln, srv) }()

	conn := dialEcho(t, ln.Addr().String(), []byte{0x03, 0x00, 0x00, 0x00, 0x02})
	conn.Close()
}

func TestRTUOverTCPListenerTracksClients(t *testing.T) {
	srv := testServer(t)
	clients := NewConnectedClients()
	srv.Clients = clients

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = RunRTUOverTCP(ctx, ln, srv) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(EncodeRTUFrame(1, []byte{0x03, 0x00, 0x00, 0x00, 0x02})); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(bufio.NewReader(conn), make([]byte, 9)); err != nil {
		t.Fatalf("read RTU response: %v", err)
	}

	waitForClients(t, clients, 1)
	clients.CloseAll()
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Error("server should have closed the RTU-over-TCP client")
	}
}

func TestWithDefaultAndRandomRegisters(t *testing.T) {
	t.Run("zeros", func(t *testing.T) {
		d := WithDefaultRegisters(1, "dev", 100)
		if d.SlaveID != 1 || d.Name != "dev" {
			t.Errorf("identity = %d %q", d.SlaveID, d.Name)
		}
		// 4 areas x 101 addresses.
		if len(d.RegisterDefs) != 404 {
			t.Errorf("defs = %d, want 404", len(d.RegisterDefs))
		}
		if len(d.RegisterMap.Coils) != 101 || len(d.RegisterMap.DiscreteInputs) != 101 {
			t.Errorf("bit areas = %d/%d, want 101/101", len(d.RegisterMap.Coils), len(d.RegisterMap.DiscreteInputs))
		}
		if len(d.RegisterMap.HoldingRegisters) != 101 || len(d.RegisterMap.InputRegisters) != 101 {
			t.Errorf("register areas = %d/%d, want 101/101", len(d.RegisterMap.HoldingRegisters), len(d.RegisterMap.InputRegisters))
		}
		for _, def := range d.RegisterDefs[:4] {
			if def.Endian != register.DefaultEndian {
				t.Errorf("endian = %s, want %s", def.Endian, register.DefaultEndian)
			}
		}
		for a := 0; a <= 100; a++ {
			if d.RegisterMap.Coils[uint16(a)] || d.RegisterMap.DiscreteInputs[uint16(a)] {
				t.Fatal("default init must not set bits")
			}
			if d.RegisterMap.HoldingRegisters[uint16(a)] != 0 || d.RegisterMap.InputRegisters[uint16(a)] != 0 {
				t.Fatal("default init must not write words")
			}
		}
	})

	t.Run("random", func(t *testing.T) {
		d := WithRandomRegisters(1, "dev", 100)
		if len(d.RegisterDefs) != 404 {
			t.Errorf("defs = %d, want 404", len(d.RegisterDefs))
		}
		var setBits, nonZeroWords int
		for a := 0; a <= 100; a++ {
			if d.RegisterMap.Coils[uint16(a)] {
				setBits++
			}
			if d.RegisterMap.DiscreteInputs[uint16(a)] {
				setBits++
			}
			if d.RegisterMap.HoldingRegisters[uint16(a)] != 0 {
				nonZeroWords++
			}
			if d.RegisterMap.InputRegisters[uint16(a)] != 0 {
				nonZeroWords++
			}
		}
		// Statistically certain over 202 draws, and it catches a seed path that
		// silently stays at zero.
		if setBits == 0 {
			t.Error("random init produced no set bits")
		}
		if nonZeroWords == 0 {
			t.Error("random init produced no non-zero words")
		}
	})

	// A full-width top address must terminate.
	t.Run("address 65535", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			d := WithDefaultRegisters(1, "dev", 65535)
			if len(d.RegisterMap.Coils) != 65536 {
				t.Errorf("coils = %d, want 65536", len(d.RegisterMap.Coils))
			}
		}()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("init with maxAddress 65535 did not terminate")
		}
	})
}
