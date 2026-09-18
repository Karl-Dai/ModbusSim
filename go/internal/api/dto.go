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

// mustSlaveID parses the slaveId path value, defaulting to 1 on error.
func mustSlaveID(r *http.Request) uint64 {
	id, err := strconv.ParseUint(r.PathValue("slaveId"), 10, 8)
	if err != nil {
		return 1
	}
	return id
}
