// Small DTO bridges between the API layer and internal packages.
package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/Karl-Dai/ModbusSim/go/internal/datasource"
	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// ParseRegisterTypeString validates a register-type string and returns the
// canonical register.RegisterType.
func ParseRegisterTypeString(s string) (register.RegisterType, error) {
	switch s {
	case "coil":
		return register.Coil, nil
	case "discrete_input":
		return register.DiscreteInput, nil
	case "input_register":
		return register.InputRegister, nil
	case "holding_register":
		return register.HoldingRegType, nil
	}
	return "", fmt.Errorf("unknown register type: %s", s)
}

// datasourceConfig is the wire form of datasource.Config (same JSON shape).
type datasourceConfig = datasource.Config

// requestSlaveID validates the URL target and rejects a conflicting legacy body
// target. An omitted body target uses the device selected by the URL.
func requestSlaveID(r *http.Request, bodyID *uint8) (uint8, error) {
	id, err := strconv.ParseUint(r.PathValue("slaveId"), 10, 8)
	if err != nil || id < 1 || id > 247 {
		return 0, fmt.Errorf("slave_id in URL must be between 1 and 247")
	}
	if bodyID != nil && uint64(*bodyID) != id {
		return 0, fmt.Errorf("slave_id in body must match URL")
	}
	return uint8(id), nil
}
