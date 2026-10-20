package serial

import "testing"

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.BaudRate != 9600 || c.DataBits != 8 || c.StopBits != 1 || c.Parity != ParityNone {
		t.Errorf("default = %+v", c)
	}
}

func TestInterframeDelay(t *testing.T) {
	if InterframeDelayUS(19200) != 1750 || InterframeDelayUS(115200) != 1750 {
		t.Error("high baud should be fixed 1750µs")
	}
	d := InterframeDelayUS(9600)
	if d < 3500 || d > 4500 {
		t.Errorf("9600 delay = %dµs, want 3500..4500", d)
	}
}

func TestConfigValid(t *testing.T) {
	// The defaults are sane except for the port name, which nobody can guess.
	good := DefaultConfig()
	if good.Valid() {
		t.Fatalf("portless default config accepted: %+v", good)
	}
	good.Port = "/dev/tty.usbserial"
	if !good.Valid() {
		t.Fatalf("config rejected: %+v", good)
	}

	bad := []struct {
		name  string
		patch func(*Config)
	}{
		{"empty port", func(c *Config) { c.Port = "" }},
		{"blank port", func(c *Config) { c.Port = "   " }},
		{"zero baud", func(c *Config) { c.Port = "/dev/tty.usb"; c.BaudRate = 0 }},
		{"4 data bits", func(c *Config) { c.Port = "/dev/tty.usb"; c.DataBits = 4 }},
		{"9 data bits", func(c *Config) { c.Port = "/dev/tty.usb"; c.DataBits = 9 }},
		{"0 stop bits", func(c *Config) { c.Port = "/dev/tty.usb"; c.StopBits = 0 }},
		{"3 stop bits", func(c *Config) { c.Port = "/dev/tty.usb"; c.StopBits = 3 }},
	}
	for _, tc := range bad {
		c := DefaultConfig()
		tc.patch(&c)
		if c.Valid() {
			t.Errorf("%s accepted: %+v", tc.name, c)
		}
	}

	// 5 and 8 data bits, 1 and 2 stop bits are all in range, and the port name
	// is used verbatim even when padded.
	for _, c := range []Config{
		{Port: "/dev/a", BaudRate: 1200, DataBits: 5, StopBits: 1, Parity: ParityEven},
		{Port: "/dev/a", BaudRate: 1200, DataBits: 8, StopBits: 2, Parity: ParityOdd},
		{Port: " /dev/a ", BaudRate: 1200, DataBits: 8, StopBits: 1},
	} {
		if !c.Valid() {
			t.Errorf("rejected %+v", c)
		}
	}
}

// ListPorts touches the host, so this only checks the contract: no panic, and
// any names it does return come back sorted.
func TestListPortsIsSorted(t *testing.T) {
	ports, err := ListPorts()
	if err != nil {
		t.Skipf("port enumeration unavailable: %v", err)
	}
	for i := 1; i < len(ports); i++ {
		if ports[i-1] > ports[i] {
			t.Fatalf("ports not sorted: %v", ports)
		}
	}
}
