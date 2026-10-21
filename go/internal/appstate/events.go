package appstate

import (
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// LogAppendedEvent says one entry landed in a connection's log. Like Rust's
// log-appended it carries no payload: the GUI re-reads the collector.
type LogAppendedEvent struct {
	ConnectionID string
}

// RegisterValueEvent reports register writes that did not come from a remote
// master's request -- the data source tick and local writes.
type RegisterValueEvent struct {
	ConnectionID string
	Changes      []slave.Change
}

// ConnectionStateEvent reports a connection starting or stopping.
type ConnectionStateEvent struct {
	ID    string
	State ConnectionState
}

// Events are the push channels a GUI subscribes to. Each callback runs on the
// goroutine that produced the event -- a listener, the simulator tick, or the
// caller of the command -- so it must not call back into the Manager.
type Events struct {
	LogAppended            func(LogAppendedEvent)
	RegisterValueChanged   func(RegisterValueEvent)
	ConnectionStateChanged func(ConnectionStateEvent)
}

func (e Events) logAppended(connectionID string) {
	if e.LogAppended != nil {
		e.LogAppended(LogAppendedEvent{ConnectionID: connectionID})
	}
}

func (e Events) registerValueChanged(connectionID string, changes []slave.Change) {
	if e.RegisterValueChanged != nil && len(changes) > 0 {
		e.RegisterValueChanged(RegisterValueEvent{ConnectionID: connectionID, Changes: changes})
	}
}

func (e Events) connectionStateChanged(id string, state ConnectionState) {
	if e.ConnectionStateChanged != nil {
		e.ConnectionStateChanged(ConnectionStateEvent{ID: id, State: state})
	}
}
