// Package serial provides serial-port configuration shared by the RTU and
// ASCII transports, plus the RTU interframe delay calculation.
// Ported from crates/modbussim-core/src/transport.rs (serial parts).
package serial

import "time"

// Parity mirrors transport.rs's Parity enum (serde snake_case).
type Parity string

const (
	ParityNone Parity = "none"
	ParityOdd  Parity = "odd"
	ParityEven Parity = "even"
)

// Config is a serial port configuration (SerialConfig in Rust).
type Config struct {
	Port     string `json:"port"`
	BaudRate uint32 `json:"baud_rate"`
	DataBits uint8  `json:"data_bits"`
	StopBits uint8  `json:"stop_bits"`
	Parity   Parity `json:"parity"`
}

// DefaultConfig mirrors SerialConfig::default().
func DefaultConfig() Config {
	return Config{BaudRate: 9600, DataBits: 8, StopBits: 1, Parity: ParityNone}
}

// InterframeDelayUS returns the RTU interframe delay in microseconds:
// a fixed 1750 µs for baud >= 19200, otherwise 3.5 character times
// (11 bits per character), rounded up.
func InterframeDelayUS(baudRate uint32) uint64 {
	if baudRate >= 19200 {
		return 1750
	}
	// 3.5 chars * 11 bits/char = 38.5 bits; µs = 38.5e6 / baud, ceil.
	return (38_500_000 + uint64(baudRate) - 1) / uint64(baudRate)
}

// InterframeDelay returns the delay as a Duration.
func InterframeDelay(baudRate uint32) time.Duration {
	return time.Duration(InterframeDelayUS(baudRate)) * time.Microsecond
}
