package slave

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/pdu"
)

func TestWithDefaultRegistersFullAddressRange(t *testing.T) {
	dev := WithDefaultRegisters(1, "full address space", 65535)
	if got := len(dev.RegisterDefs); got != 4*65536 {
		t.Fatalf("got %d definitions, want %d", got, 4*65536)
	}
	for name, count := range map[string]int{
		"coils":             len(dev.RegisterMap.Coils),
		"discrete inputs":   len(dev.RegisterMap.DiscreteInputs),
		"holding registers": len(dev.RegisterMap.HoldingRegisters),
		"input registers":   len(dev.RegisterMap.InputRegisters),
	} {
		if count != 65536 {
			t.Errorf("%s: got %d addresses, want 65536", name, count)
		}
	}
}

// boundaryRequest builds valid payloads independently of the request parser.
func boundaryRequest(fc byte, address, quantity uint16) []byte {
	out := []byte{fc, byte(address >> 8), byte(address), byte(quantity >> 8), byte(quantity)}
	switch fc {
	case pdu.FCWriteMultipleCoils:
		byteCount := (int(quantity) + 7) / 8
		out = append(out, byte(byteCount))
		out = append(out, bytes.Repeat([]byte{0xFF}, byteCount)...)
	case pdu.FCWriteMultipleRegisters:
		byteCount := 2 * int(quantity)
		out = append(out, byte(byteCount))
		out = append(out, bytes.Repeat([]byte{0xAB}, byteCount)...)
	}
	return out
}

func TestProcessRequestAddressBounds(t *testing.T) {
	for _, fc := range []byte{pdu.FCReadCoils, pdu.FCReadDiscreteInputs,
		pdu.FCReadHoldingRegisters, pdu.FCReadInputRegisters,
		pdu.FCWriteMultipleCoils, pdu.FCWriteMultipleRegisters} {
		t.Run(fmt.Sprintf("FC%02X", fc), func(t *testing.T) {
			srv := NewServer()
			dev := NewDevice(1, "boundary")
			for _, address := range []uint16{0, 65534, 65535} {
				dev.RegisterMap.Coils[address] = false
				dev.RegisterMap.DiscreteInputs[address] = false
				dev.RegisterMap.HoldingRegisters[address] = 0x1234
				dev.RegisterMap.InputRegisters[address] = 0x1234
			}
			if err := srv.AddDevice(dev); err != nil {
				t.Fatal(err)
			}
			callbacks := 0
			srv.ChangeCallback = func(changes []Change) { callbacks++ }

			// Address zero exists deliberately: wrapping must still be rejected.
			got := srv.ProcessRequest(1, boundaryRequest(fc, 65535, 2))
			want := []byte{fc | 0x80, ExcIllegalDataAddress}
			if !bytes.Equal(got, want) {
				t.Fatalf("overflow response = % X, want % X", got, want)
			}
			if dev.RegisterMap.Coils[0] || dev.RegisterMap.Coils[65535] ||
				dev.RegisterMap.HoldingRegisters[0] != 0x1234 ||
				dev.RegisterMap.HoldingRegisters[65535] != 0x1234 || callbacks != 0 {
				t.Fatal("rejected range changed registers or invoked a callback")
			}

			for _, bounds := range [][2]uint16{{65534, 2}, {65535, 1}} {
				got = srv.ProcessRequest(1, boundaryRequest(fc, bounds[0], bounds[1]))
				if len(got) == 0 || got[0] != fc {
					t.Errorf("address %d, quantity %d: response = % X", bounds[0], bounds[1], got)
				}
			}
		})
	}
}

func TestProcessRequestQuantityBounds(t *testing.T) {
	for _, tc := range []struct {
		fc  byte
		max uint16
	}{
		{pdu.FCReadCoils, 2000}, {pdu.FCReadDiscreteInputs, 2000},
		{pdu.FCReadHoldingRegisters, 125}, {pdu.FCReadInputRegisters, 125},
		{pdu.FCWriteMultipleCoils, 1968}, {pdu.FCWriteMultipleRegisters, 123},
	} {
		t.Run(fmt.Sprintf("FC%02X", tc.fc), func(t *testing.T) {
			srv := NewServer()
			dev := WithDefaultRegisters(1, "quantity", tc.max)
			if err := srv.AddDevice(dev); err != nil {
				t.Fatal(err)
			}
			for _, quantity := range []uint16{0, tc.max + 1} {
				got := srv.ProcessRequest(1, boundaryRequest(tc.fc, 0, quantity))
				want := []byte{tc.fc | 0x80, ExcIllegalDataValue}
				if !bytes.Equal(got, want) {
					t.Errorf("quantity %d: response = % X, want % X", quantity, got, want)
				}
			}
			got := srv.ProcessRequest(1, boundaryRequest(tc.fc, 0, tc.max))
			if len(got) == 0 || got[0] != tc.fc {
				t.Errorf("maximum quantity: response = % X", got)
			}
		})
	}
}

func TestProcessRequestSingleWriteLastAddress(t *testing.T) {
	srv := NewServer()
	dev := NewDevice(1, "last address")
	dev.RegisterMap.Coils[65535] = false
	dev.RegisterMap.HoldingRegisters[65535] = 0
	if err := srv.AddDevice(dev); err != nil {
		t.Fatal(err)
	}
	for _, request := range [][]byte{
		{pdu.FCWriteSingleCoil, 0xFF, 0xFF, 0xFF, 0x00},
		{pdu.FCWriteSingleRegister, 0xFF, 0xFF, 0xAB, 0xCD},
	} {
		if got := srv.ProcessRequest(1, request); !bytes.Equal(got, request) {
			t.Errorf("request % X: response = % X, want echo", request, got)
		}
	}
	if !dev.RegisterMap.Coils[65535] || dev.RegisterMap.HoldingRegisters[65535] != 0xABCD {
		t.Fatal("single write did not update address 65535")
	}
}

func TestProcessRequestRejectsMalformedWritePayload(t *testing.T) {
	for _, fc := range []byte{pdu.FCWriteSingleCoil, pdu.FCWriteSingleRegister,
		pdu.FCWriteMultipleCoils, pdu.FCWriteMultipleRegisters} {
		t.Run(fmt.Sprintf("FC%02X", fc), func(t *testing.T) {
			srv := testServer(t)
			callbacks := 0
			srv.ChangeCallback = func(changes []Change) { callbacks++ }
			request := boundaryRequest(fc, 0, 1)
			if fc == pdu.FCWriteSingleCoil {
				request[3], request[4] = 0xFF, 0x00
			}
			for _, malformed := range [][]byte{request[:len(request)-1], append(append([]byte(nil), request...), 0)} {
				got := srv.ProcessRequest(1, malformed)
				want := []byte{fc | 0x80, ExcIllegalDataValue}
				if !bytes.Equal(got, want) {
					t.Errorf("malformed request % X: response = % X, want % X", malformed, got, want)
				}
			}
			dev, _ := srv.GetDevice(1)
			if dev.RegisterMap.Coils[0] || dev.RegisterMap.HoldingRegisters[0] != 0 || callbacks != 0 {
				t.Fatal("malformed payload changed registers or invoked a callback")
			}
		})
	}
}
