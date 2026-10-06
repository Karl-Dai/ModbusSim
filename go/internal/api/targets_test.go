package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Karl-Dai/ModbusSim/go/internal/app"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

func targetTestAPI(t *testing.T) (*http.ServeMux, *app.State) {
	t.Helper()
	state := app.NewState()
	for _, id := range []string{"a", "b"} {
		conn := app.NewConnection(app.TransportConfig{Type: "tcp"}, app.SlaveTLSConfig{})
		for _, unit := range []uint8{1, 2, 247} {
			dev := slave.WithDefaultRegisters(unit, "device", 10)
			dev.RegisterMap.WriteHoldingRegister(5, uint16(unit))
			if err := conn.Server().AddDevice(dev); err != nil {
				t.Fatal(err)
			}
		}
		state.AddConnection(id, conn)
	}
	mux := http.NewServeMux()
	NewServer(state).Routes(mux)
	return mux, state
}

func requestTarget(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
	return response
}

func assertTargetValuesUnchanged(t *testing.T, state *app.State) {
	t.Helper()
	for _, id := range []string{"a", "b"} {
		for _, unit := range []uint8{1, 2, 247} {
			value, ok := state.ReadRegister(id, unit, register.HoldingRegType, 5)
			if !ok || value != uint16(unit) {
				t.Errorf("%s/%d value = %d, %v; want %d", id, unit, value, ok, unit)
			}
			defs, err := state.ListRegisters(id, unit)
			if err != nil {
				t.Fatal(err)
			}
			if len(defs) != 44 {
				t.Errorf("%s/%d definitions = %d, want 44", id, unit, len(defs))
			}
			for _, def := range defs {
				if def.Address > 10 {
					t.Errorf("%s/%d unexpected definition at %d", id, unit, def.Address)
				}
			}
		}
	}
}

func TestDevicePathRejectsBodyTargetMismatch(t *testing.T) {
	for _, tc := range []struct{ name, method, suffix, body string }{
		{"write_unit", "POST", "/write", `{"slave_id":1,"register_type":"holding_register","address":5,"value":99}`},
		{"read_unit", "POST", "/read", `{"slave_id":1,"register_type":"holding_register","address":5,"count":1}`},
		{"add_unit", "POST", "/registers", `{"connection_id":"a","slave_id":1,"register_type":"holding_register","data_type":"uint16","address":50}`},
		{"add_connection", "POST", "/registers", `{"connection_id":"b","slave_id":2,"register_type":"holding_register","data_type":"uint16","address":50}`},
		{"update_unit", "PUT", "/registers", `{"connection_id":"a","slave_id":1,"register_type":"holding_register","data_type":"uint16","address":50,"original_address":5,"original_register_type":"holding_register"}`},
		{"update_connection", "PUT", "/registers", `{"connection_id":"b","slave_id":2,"register_type":"holding_register","data_type":"uint16","address":50,"original_address":5,"original_register_type":"holding_register"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux, state := targetTestAPI(t)
			response := requestTarget(mux, tc.method, "/api/connections/a/devices/2"+tc.suffix, tc.body)
			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", response.Code, response.Body)
			}
			assertTargetValuesUnchanged(t, state)
		})
	}
}

func TestDevicePathRejectsInvalidSlaveID(t *testing.T) {
	for _, id := range []string{"invalid", "-1", "0", "248", "255", "256", "999999999999999999999"} {
		for _, operation := range []struct{ method, suffix, body string }{
			{"DELETE", "", ""},
			{"DELETE", "/registers/5/holding_register", ""},
			{"POST", "/write", `{"slave_id":1,"register_type":"holding_register","address":5,"value":99}`},
			{"POST", "/read", `{"slave_id":1,"register_type":"holding_register","address":5,"count":1}`},
			{"GET", "/registers", ""},
			{"POST", "/registers", `{"connection_id":"a","slave_id":1,"register_type":"holding_register","data_type":"uint16","address":50}`},
			{"PUT", "/registers", `{"connection_id":"a","slave_id":1,"register_type":"holding_register","data_type":"uint16","address":50,"original_address":5,"original_register_type":"holding_register"}`},
		} {
			t.Run(id+operation.method+operation.suffix, func(t *testing.T) {
				mux, state := targetTestAPI(t)
				response := requestTarget(mux, operation.method, "/api/connections/a/devices/"+id+operation.suffix, operation.body)
				if response.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400: %s", response.Code, response.Body)
				}
				assertTargetValuesUnchanged(t, state)
			})
		}
	}
}

func TestDevicePathUsesURLAndAcceptsMatchingLegacyBody(t *testing.T) {
	for _, unit := range []uint8{1, 2, 247} {
		for _, supplied := range []bool{false, true} {
			t.Run(fmt.Sprintf("unit_%d/body_%v", unit, supplied), func(t *testing.T) {
				mux, state := targetTestAPI(t)
				prefix := fmt.Sprintf("/api/connections/a/devices/%d", unit)
				target := ""
				if supplied {
					target = fmt.Sprintf(`"connection_id":"a","slave_id":%d,`, unit)
				}
				for _, op := range []struct{ method, suffix, payload string }{
					{"POST", "/write", `"register_type":"holding_register","address":5,"value":99`},
					{"POST", "/read", `"register_type":"holding_register","address":5,"count":1`},
					{"POST", "/registers", `"register_type":"holding_register","data_type":"uint16","address":50`},
					{"PUT", "/registers", `"register_type":"holding_register","data_type":"uint16","address":51,"original_address":50,"original_register_type":"holding_register"`},
				} {
					response := requestTarget(mux, op.method, prefix+op.suffix, "{"+target+op.payload+"}")
					if response.Code != http.StatusOK {
						t.Fatalf("%s %s status = %d: %s", op.method, op.suffix, response.Code, response.Body)
					}
					if op.suffix == "/read" {
						var result readResponse
						if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
							t.Fatal(err)
						}
						if len(result.Values) != 1 || result.Values[0] != 99 {
							t.Errorf("read = %v, want [99]", result.Values)
						}
					}
				}
				response := requestTarget(mux, "DELETE", prefix+"/registers/51/holding_register", "")
				if response.Code != http.StatusOK {
					t.Fatalf("delete status = %d: %s", response.Code, response.Body)
				}
				for _, other := range []uint8{1, 2, 247} {
					got, ok := state.ReadRegister("a", other, register.HoldingRegType, 5)
					want := uint16(other)
					if other == unit {
						want = 99
					}
					if !ok || got != want {
						t.Errorf("device %d = %d, %v; want %d", other, got, ok, want)
					}
				}
			})
		}
	}
}

func TestReadRegisterRejectsAddressWrapAndEmptyRange(t *testing.T) {
	for _, tc := range []struct {
		address, count uint16
		wantStatus     int
	}{
		{65535, 2, http.StatusBadRequest},
		{65534, 3, http.StatusBadRequest},
		{0, 0, http.StatusBadRequest},
		{65535, 1, http.StatusOK},
		{65534, 2, http.StatusOK},
	} {
		t.Run(fmt.Sprintf("%d_%d", tc.address, tc.count), func(t *testing.T) {
			mux, state := targetTestAPI(t)
			conn, _ := state.Connection("a")
			dev, _ := conn.Server().GetDevice(1)
			dev.RegisterMap.WriteHoldingRegister(0, 11)
			dev.RegisterMap.WriteHoldingRegister(65534, 12)
			dev.RegisterMap.WriteHoldingRegister(65535, 13)
			body := fmt.Sprintf(`{"slave_id":1,"register_type":"holding_register","address":%d,"count":%d}`, tc.address, tc.count)
			response := requestTarget(mux, "POST", "/api/connections/a/devices/1/read", body)
			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.wantStatus, response.Body)
			}
			if tc.wantStatus == http.StatusOK {
				var result readResponse
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Values) != int(tc.count) || result.Values[len(result.Values)-1] != 13 {
					t.Errorf("boundary read = %v", result.Values)
				}
			}
		})
	}
}

func TestDevicePathRejectsInvalidBodySlaveID(t *testing.T) {
	for _, id := range []string{"0", "248", "255", "256", "-1", `"invalid"`} {
		for _, operation := range []struct{ method, suffix, payload string }{
			{"POST", "/write", `"register_type":"holding_register","address":5,"value":99`},
			{"POST", "/read", `"register_type":"holding_register","address":5,"count":1`},
			{"POST", "/registers", `"connection_id":"a","register_type":"holding_register","data_type":"uint16","address":50`},
			{"PUT", "/registers", `"connection_id":"a","register_type":"holding_register","data_type":"uint16","address":50,"original_address":5,"original_register_type":"holding_register"`},
		} {
			t.Run(id+operation.method+operation.suffix, func(t *testing.T) {
				mux, state := targetTestAPI(t)
				body := `{"slave_id":` + id + "," + operation.payload + "}"
				response := requestTarget(mux, operation.method, "/api/connections/a/devices/2"+operation.suffix, body)
				if response.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400: %s", response.Code, response.Body)
				}
				assertTargetValuesUnchanged(t, state)
			})
		}
	}
}

func TestDeviceCreationRejectsInvalidSlaveIDs(t *testing.T) {
	for _, id := range []string{"0", "248", "255", "256", "-1"} {
		for _, path := range []string{"/api/connections", "/api/connections/a/devices"} {
			t.Run(id+path, func(t *testing.T) {
				mux, state := targetTestAPI(t)
				response := requestTarget(mux, "POST", path, `{"slave_id":`+id+`,"name":"invalid","transport":{"type":"tcp","host":"127.0.0.1","port":0}}`)
				if response.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400: %s", response.Code, response.Body)
				}
				if got := len(state.ConnectionIDs()); got != 2 {
					t.Errorf("connection count = %d, want 2", got)
				}
				conn, _ := state.Connection("a")
				if got := conn.DeviceCount(); got != 3 {
					t.Errorf("device count = %d, want 3", got)
				}
			})
		}
	}
}

func TestConnectionCreationDefaultAndBounds(t *testing.T) {
	for _, tc := range []struct {
		body string
		want uint8
	}{
		{`{}`, 1}, {`{"slave_id":1}`, 1}, {`{"slave_id":247}`, 247},
	} {
		t.Run(tc.body, func(t *testing.T) {
			mux, state := targetTestAPI(t)
			response := requestTarget(mux, "POST", "/api/connections", tc.body)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body)
			}
			var info connectionInfo
			if err := json.Unmarshal(response.Body.Bytes(), &info); err != nil {
				t.Fatal(err)
			}
			conn, ok := state.Connection(info.ID)
			if !ok {
				t.Fatal("created connection is missing")
			}
			if _, ok := conn.Server().GetDevice(tc.want); !ok {
				t.Errorf("created device %d is missing", tc.want)
			}
		})
	}
}

func TestReadTrueDiscreteInput(t *testing.T) {
	mux, state := targetTestAPI(t)
	conn, _ := state.Connection("a")
	dev, _ := conn.Server().GetDevice(2)
	dev.RegisterMap.DiscreteInputs[5] = true
	response := requestTarget(mux, "POST", "/api/connections/a/devices/2/read", `{"slave_id":2,"register_type":"discrete_input","address":5,"count":1}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	var result readResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Values) != 1 || result.Values[0] != 1 {
		t.Errorf("true discrete input = %v, want [1]", result.Values)
	}
}
