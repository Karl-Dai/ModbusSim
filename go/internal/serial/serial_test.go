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
