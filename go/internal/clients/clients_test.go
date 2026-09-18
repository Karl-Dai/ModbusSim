// Tests for the connected-clients registry (clients.rs port).
package clients

import (
	"net"
	"testing"
)

func pipeConn(t *testing.T) net.Conn {
	t.Helper()
	c1, c2 := net.Pipe()
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })
	return c1
}

func TestTrackListRemove(t *testing.T) {
	reg := New()
	id1, err := reg.Track(pipeConn(t))
	if err != nil {
		t.Fatal(err)
	}
	id2, err := reg.Track(pipeConn(t))
	if err != nil {
		t.Fatal(err)
	}

	list := reg.List()
	if len(list) != 2 || list[0].ID != id1 || list[1].ID != id2 {
		t.Fatalf("list = %+v", list)
	}
	if list[0].PeerAddress == "" || list[0].ConnectedAt == "" {
		t.Fatalf("info incomplete: %+v", list[0])
	}

	reg.Remove(id1)
	if got := reg.List(); len(got) != 1 || got[0].ID != id2 {
		t.Fatalf("after remove = %+v", got)
	}
}

func TestCloseAllRejectsNew(t *testing.T) {
	reg := New()
	c := pipeConn(t)
	if _, err := reg.Track(c); err != nil {
		t.Fatal(err)
	}
	reg.CloseAll()
	// CloseAll force-closed the tracked conn.
	if _, err := reg.Track(pipeConn(t)); err == nil {
		t.Fatal("track after close should fail")
	}
	if got := reg.List(); len(got) != 0 {
		t.Fatalf("after close list = %+v", got)
	}
}
