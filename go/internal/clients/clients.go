// Ported from crates/modbussim-core/src/clients.rs: runtime-only registry of
// connected network clients. The Rust version clones the TcpStream for
// close-all; Go keeps the net.Conn itself (only used by the accept loop
// between requests, so no concurrent-use conflict for the shutdown path).
package clients

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// ClientInfo describes one connected client (serde-compatible field names).
type ClientInfo struct {
	ID          uint64 `json:"id"`
	PeerAddress string `json:"peer_address"`
	ConnectedAt string `json:"connected_at"`
}

type registry struct {
	closed  bool
	nextID  uint64
	entries map[uint64]*entry
}

type entry struct {
	info ClientInfo
	conn net.Conn
}

// ConnectedClients tracks live client connections so the UI can list them
// and the server can force-close them all on stop.
type ConnectedClients struct {
	mu  sync.Mutex
	reg registry
}

func New() *ConnectedClients {
	return &ConnectedClients{reg: registry{entries: map[uint64]*entry{}}}
}

// List returns a snapshot of connected clients, ordered by id.
func (c *ConnectedClients) List() []ClientInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ClientInfo, 0, len(c.reg.entries))
	for id := uint64(1); id <= c.reg.nextID; id++ {
		if e, ok := c.reg.entries[id]; ok {
			out = append(out, e.info)
		}
	}
	return out
}

// Track registers a connection and returns its id. Use Remove(id) when the
// client disconnects (Go has no Drop; defer the remove at accept time).
func (c *ConnectedClients) Track(conn net.Conn) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reg.closed {
		_ = conn.Close()
		return 0, fmt.Errorf("listener stopped")
	}
	c.reg.nextID++
	id := c.reg.nextID
	c.reg.entries[id] = &entry{
		info: ClientInfo{
			ID:          id,
			PeerAddress: conn.RemoteAddr().String(),
			ConnectedAt: time.Now().UTC().Format(time.RFC3339),
		},
		conn: conn,
	}
	return id, nil
}

// Remove forgets one client (called on disconnect, mirrors ClientGuard::drop).
func (c *ConnectedClients) Remove(id uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.reg.entries, id)
}

// CloseAll marks the registry closed and force-closes every live connection.
func (c *ConnectedClients) CloseAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reg.closed = true
	for id, e := range c.reg.entries {
		_ = e.conn.Close()
		delete(c.reg.entries, id)
	}
}
