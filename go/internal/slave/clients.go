// Runtime-only registry of connected network clients, ported from
// crates/modbussim-core/src/clients.rs. The GUI lists live clients and
// stopping a connection has to kick them, so the registry tracks the raw
// sockets and can close them all at once.
package slave

import (
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// ClientInfo describes one tracked client.
type ClientInfo struct {
	ID          uint64 `json:"id"`
	PeerAddress string `json:"peer_address"`
	ConnectedAt string `json:"connected_at"`
}

type clientEntry struct {
	info ClientInfo
	conn net.Conn
}

// ConnectedClients is a shareable registry of the sockets a slave connection
// currently serves. A zero value is ready to use.
type ConnectedClients struct {
	mu      sync.Mutex
	closed  bool
	nextID  uint64
	entries map[uint64]clientEntry
}

// NewConnectedClients returns an empty, open registry.
func NewConnectedClients() *ConnectedClients {
	return &ConnectedClients{entries: map[uint64]clientEntry{}}
}

// Track registers conn and returns the guard that unregisters it. The caller
// must hold the guard for the connection's lifetime. Once CloseAll has run the
// registry is closed: conn is shut down here and Track reports an error, which
// is how a stopping listener refuses late arrivals.
func (c *ConnectedClients) Track(conn net.Conn) (*ClientGuard, error) {
	peer := conn.RemoteAddr()
	if peer == nil {
		return nil, fmt.Errorf("client has no peer address")
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = conn.Close()
		return nil, fmt.Errorf("listener stopped")
	}
	if c.entries == nil {
		c.entries = map[uint64]clientEntry{}
	}
	c.nextID++
	id := c.nextID
	c.entries[id] = clientEntry{
		info: ClientInfo{
			ID:          id,
			PeerAddress: peer.String(),
			ConnectedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		conn: conn,
	}
	c.mu.Unlock()

	return &ClientGuard{registry: c, id: id}, nil
}

// List returns the tracked clients in registration order.
func (c *ConnectedClients) List() []ClientInfo {
	c.mu.Lock()
	defer c.mu.Unlock()

	ids := make([]uint64, 0, len(c.entries))
	for id := range c.entries {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	out := make([]ClientInfo, 0, len(ids))
	for _, id := range ids {
		out = append(out, c.entries[id].info)
	}
	return out
}

// Count reports how many clients are live.
func (c *ConnectedClients) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// CloseAll closes the registry and shuts down every live client socket.
// Tracked guards stay valid: releasing one afterwards is a no-op.
func (c *ConnectedClients) CloseAll() {
	c.mu.Lock()
	c.closed = true
	entries := c.entries
	c.entries = map[uint64]clientEntry{}
	c.mu.Unlock()

	for _, e := range entries {
		_ = e.conn.Close()
	}
}

// ClientGuard unregisters its client when released.
type ClientGuard struct {
	registry *ConnectedClients
	id       uint64
	once     sync.Once
}

// Release removes the entry; safe to call more than once and safe after
// CloseAll.
func (g *ClientGuard) Release() {
	if g == nil || g.registry == nil {
		return
	}
	g.once.Do(func() {
		g.registry.mu.Lock()
		delete(g.registry.entries, g.id)
		g.registry.mu.Unlock()
	})
}
