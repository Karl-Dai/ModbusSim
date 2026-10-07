package app

import (
	"sync"
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

func TestLocalRegisterAccessConcurrentWithProtocol(t *testing.T) {
	conn := NewConnection(TransportConfig{Type: "tcp"}, SlaveTLSConfig{})
	dev := slave.WithDefaultRegisters(1, "device", 10)
	if err := conn.Server().AddDevice(dev); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, operation := range []func(){
		func() {
			if err := conn.WriteRegisterValue(1, register.HoldingRegType, 5, 42); err != nil {
				t.Error(err)
			}
			if err := conn.WriteRegisterValue(1, register.Coil, 5, 1); err != nil {
				t.Error(err)
			}
		},
		func() {
			conn.ReadRegisterValue(1, register.HoldingRegType, 5)
			conn.ReadRegisterValue(1, register.Coil, 5)
		},
		func() {
			conn.Server().ProcessRequest(1, []byte{6, 0, 5, 0, 99})
			conn.Server().ProcessRequest(1, []byte{5, 0, 5, 0, 0})
			conn.Server().ProcessRequest(1, []byte{3, 0, 5, 0, 1})
			conn.Server().ProcessRequest(1, []byte{1, 0, 5, 0, 1})
		},
	} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range 100 {
				operation()
			}
		}()
	}
	close(start)
	workers.Wait()
}
