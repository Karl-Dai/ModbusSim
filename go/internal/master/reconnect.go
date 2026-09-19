// Reconnect policy. Ported from crates/modbussim-core/src/reconnect.rs.
package master

import "time"

// ReconnectPolicy holds the exponential-backoff reconnect parameters shared
// by all TCP-based master transports.
type ReconnectPolicy struct {
	Enabled       bool          `json:"enabled"`
	InitialDelay  time.Duration `json:"initial_delay_ms"`
	MaxDelay      time.Duration `json:"max_delay_ms"`
	BackoffFactor float64       `json:"backoff_factor"`
	MaxAttempts   uint32        `json:"max_attempts"` // 0 = unlimited
}

// DefaultReconnectPolicy mirrors ReconnectPolicy::default() as used by the
// master-frontend communication options (interval 1s, max 30s, factor 2,
// unlimited attempts).
func DefaultReconnectPolicy() ReconnectPolicy {
	return ReconnectPolicy{
		Enabled:       true,
		InitialDelay:  1000 * time.Millisecond,
		MaxDelay:      30000 * time.Millisecond,
		BackoffFactor: 2,
	}
}

// NextDelay returns the delay after attempt n (0-based), clamped to MaxDelay.
func (p ReconnectPolicy) NextDelay(attempt uint32) time.Duration {
	if !p.Enabled {
		return 0
	}
	delay := float64(p.InitialDelay)
	for i := uint32(0); i < attempt; i++ {
		delay *= p.BackoffFactor
		if delay >= float64(p.MaxDelay) {
			return p.MaxDelay
		}
	}
	if delay > float64(p.MaxDelay) {
		return p.MaxDelay
	}
	return time.Duration(delay)
}
