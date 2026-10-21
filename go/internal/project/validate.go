// Validation of a loaded project, ported from the checks the Rust apps ran on
// load (crates/modbussim-app/src/commands.rs) and the engine validators in
// register.rs, request.rs, reconnect.rs and socks5.rs.
package project

import (
	"fmt"
	"math"

	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

// Validate checks the whole document: connection identity and transport shape,
// device point definitions and their generated-value sources, scan groups, and
// the request/reconnect/proxy limits.
func (p *Project) Validate() error {
	if p.Type != TypeSlave && p.Type != TypeMaster {
		return fmt.Errorf("project type must be slave or master")
	}
	seen := map[string]bool{}
	for i, conn := range p.Connections {
		if err := conn.Validate(); err != nil {
			return fmt.Errorf("connection %d: %w", i, err)
		}
		if conn.ID == "" {
			return fmt.Errorf("connection %d is missing an id", i)
		}
		if seen[conn.ID] {
			return fmt.Errorf("duplicate connection id %s", conn.ID)
		}
		seen[conn.ID] = true
	}
	return nil
}

// Validate checks one connection.
func (c *Connection) Validate() error {
	switch {
	case !c.Transport.Type.IsTCP() && !c.Transport.Type.IsSerial():
		return fmt.Errorf("unknown transport %q", c.Transport.Type)
	case c.Transport.Type.IsTCP():
		if c.Transport.Host == "" {
			return fmt.Errorf("transport host is required")
		}
		if c.Transport.Port == 0 {
			return fmt.Errorf("transport port must be between 1 and 65535")
		}
	default:
		if c.Transport.Serial == nil || c.Transport.Serial.Port == "" {
			return fmt.Errorf("serial port is required for the %s transport", c.Transport.Type)
		}
	}
	if c.Transport.Type == TransportTCPTLS && c.Transport.ServerTLS != nil {
		if err := validateServerTLS(*c.Transport.ServerTLS); err != nil {
			return err
		}
	}
	if c.DefaultSlaveID < 1 || c.DefaultSlaveID > 247 {
		return fmt.Errorf("default slave ID must be between 1 and 247")
	}
	if err := c.Requests.Validate(); err != nil {
		return err
	}
	if c.Reconnect.Enabled {
		if err := c.Reconnect.Validate(); err != nil {
			return err
		}
	}
	if c.Socks5 != nil {
		if c.Socks5.Host == "" {
			return fmt.Errorf("SOCKS5 proxy host is required")
		}
		if c.Socks5.Port == 0 {
			return fmt.Errorf("SOCKS5 proxy port must be between 1 and 65535")
		}
		if (c.Socks5.Username == "") != (c.Socks5.Password == "") {
			return fmt.Errorf("SOCKS5 username and password must either both be set or both be empty")
		}
	}
	for _, dev := range c.Devices {
		if err := dev.Validate(); err != nil {
			return err
		}
	}
	for _, group := range c.ScanGroups {
		if err := group.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks one device and its point set.
func (d *Device) Validate() error {
	if d.SlaveID < 1 || d.SlaveID > 247 {
		return fmt.Errorf("slave %d: slave ID must be between 1 and 247", d.SlaveID)
	}
	defs := make([]register.RegisterDef, 0, len(d.Points))
	for _, pt := range d.Points {
		defs = append(defs, pt.ToDef())
		if pt.Mutation != nil && pt.Mutation.Enabled && pt.DataSource != nil {
			return fmt.Errorf("register %s@%d cannot use mutation and a data source simultaneously",
				pt.RegisterType, pt.Address)
		}
		if pt.DataSource != nil {
			cfg := pt.DataSource.ToEngine()
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("register %s@%d: %w", pt.RegisterType, pt.Address, err)
			}
		}
	}
	if err := register.ValidateDefinitions(defs); err != nil {
		return fmt.Errorf("slave %d: %w", d.SlaveID, err)
	}
	return nil
}

// Validate mirrors RequestSettings::validate.
func (r RequestSettings) Validate() error {
	if r.IntervalMS > 60000 {
		return fmt.Errorf("request interval must be between 0 and 60000 ms")
	}
	if r.MaxReadRegisters < 1 || r.MaxReadRegisters > 125 {
		return fmt.Errorf("registers per read request must be between 1 and 125")
	}
	if r.MaxReadBits < 1 || r.MaxReadBits > 2000 {
		return fmt.Errorf("bits per read request must be between 1 and 2000")
	}
	return nil
}

// Validate mirrors ReconnectPolicy::validate.
func (p ReconnectPolicy) Validate() error {
	if p.InitialDelayMS == 0 || p.InitialDelayMS > 60000 {
		return fmt.Errorf("initial reconnect delay must be between 1 and 60000 ms")
	}
	if p.MaxDelayMS < p.InitialDelayMS || p.MaxDelayMS > 600000 {
		return fmt.Errorf("maximum reconnect delay must be at least the initial delay and at most 600000 ms")
	}
	if math.IsNaN(p.BackoffFactor) || math.IsInf(p.BackoffFactor, 0) || p.BackoffFactor < 1 || p.BackoffFactor > 10 {
		return fmt.Errorf("reconnect backoff factor must be between 1 and 10")
	}
	return nil
}

// Validate mirrors ScanGroup::validate.
func (g *ScanGroup) Validate() error {
	if g.Count == 0 || uint32(g.StartAddress)+uint32(g.Count) > 65536 {
		return fmt.Errorf("read range must contain 1..65535 addresses and end at or before 65535")
	}
	if g.IntervalMS == 0 {
		return fmt.Errorf("poll interval must be greater than zero")
	}
	if g.SlaveID < 1 || g.SlaveID > 247 {
		return fmt.Errorf("slave ID must be between 1 and 247")
	}
	return nil
}

// validateServerTLS mirrors slave.BuildTLSConfig's precondition: requiring a
// client certificate is meaningless without a CA to verify it against.
func validateServerTLS(t ServerTLS) error {
	if t.Enabled && t.RequireClientCert && t.CAFile == "" {
		return fmt.Errorf("client certificate verification requires a CA file")
	}
	return nil
}
