// Package appstate holds the slave application's runtime state: the connection
// registry, per-point simulation config, the event fan-out a GUI binds to, and
// project import/export. It is the Go counterpart of
// crates/modbussim-app/src/{state,commands,connection_settings,mutation,
// data_source}.rs with the Tauri IPC layer dropped -- the bound methods replace
// the #[tauri::command] functions.
package appstate

import (
	"errors"
	"fmt"
	"net"

	"github.com/Karl-Dai/ModbusSim/go/internal/project"
	"github.com/Karl-Dai/ModbusSim/go/internal/serial"
	"github.com/Karl-Dai/ModbusSim/go/internal/slave"
)

// Transport is a slave link. The persisted project model already carries every
// field a slave needs (plus its server TLS identity), so the runtime reuses it.
type Transport = project.Transport

// TransportTarget is what the UI shows for a connection: host and port for the
// TCP family, the port name and 0 for serial, as Rust's SlaveConnectionInfo does.
func TransportTarget(t Transport) (address string, port uint16) {
	switch t.Type {
	case project.TransportTCP, project.TransportTCPTLS, project.TransportRTUOverTCP:
		return t.Host, t.Port
	default:
		if serialCfg, ok := serialConfig(t); ok {
			return serialCfg.Port, 0
		}
		return "", 0
	}
}

// ValidateTransport mirrors connection_settings.rs::validate_transport. The
// checks are stricter than project.Connection.Validate, which also serves master
// projects where a proxy hostname is legitimate: a slave must bind an IP.
func ValidateTransport(t Transport) error {
	switch {
	case t.Type.IsTCP():
		if net.ParseIP(t.Host) == nil {
			return errors.New("Bind address must be an IP address")
		}
		if t.Port == 0 {
			return errors.New("Port must be between 1 and 65535")
		}
	case t.Type.IsSerial():
		cfg, ok := serialConfig(t)
		if !ok || !cfg.Valid() {
			return errors.New("Invalid serial port settings")
		}
	default:
		return fmt.Errorf("unknown transport %q", t.Type)
	}
	return nil
}

func serialConfig(t Transport) (serial.Config, bool) {
	if !t.Type.IsSerial() || t.Serial == nil {
		return serial.Config{}, false
	}
	return serial.Config{
		Port:     t.Serial.Port,
		BaudRate: t.Serial.BaudRate,
		DataBits: t.Serial.DataBits,
		StopBits: t.Serial.StopBits,
		Parity:   serial.Parity(t.Serial.Parity),
	}, true
}

// tlsConfig maps the persisted server TLS identity onto the listener's config.
func tlsConfig(t Transport) slave.TLSConfig {
	cfg := slave.TLSConfig{}
	if t.ServerTLS != nil {
		cfg = slave.TLSConfig{
			Enabled:           t.ServerTLS.Enabled,
			CertFile:          t.ServerTLS.CertFile,
			KeyFile:           t.ServerTLS.KeyFile,
			CAFile:            t.ServerTLS.CAFile,
			RequireClientCert: t.ServerTLS.RequireClientCert,
			PKCS12File:        t.ServerTLS.PKCS12File,
			PKCS12Password:    t.ServerTLS.PKCS12Password,
		}
	}
	return cfg
}
