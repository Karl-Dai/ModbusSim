use serde::{Deserialize, Serialize};
use std::time::Duration;

/// Policy that controls automatic reconnection behaviour.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReconnectPolicy {
    /// Whether automatic reconnection is enabled.
    pub enabled: bool,
    /// Delay before the first reconnect attempt (milliseconds).
    #[serde(default = "default_initial_delay_ms")]
    pub initial_delay_ms: u64,
    /// Maximum delay between reconnect attempts (milliseconds).
    #[serde(default = "default_max_delay_ms")]
    pub max_delay_ms: u64,
    /// Multiplicative factor applied each attempt.
    #[serde(default = "default_backoff_factor")]
    pub backoff_factor: f64,
    /// Maximum number of attempts. `None` means unlimited.
    #[serde(default)]
    pub max_attempts: Option<u32>,
}

fn default_initial_delay_ms() -> u64 {
    1000
}

fn default_max_delay_ms() -> u64 {
    30_000
}

fn default_backoff_factor() -> f64 {
    2.0
}

impl Default for ReconnectPolicy {
    fn default() -> Self {
        Self {
            enabled: true,
            initial_delay_ms: default_initial_delay_ms(),
            max_delay_ms: default_max_delay_ms(),
            backoff_factor: default_backoff_factor(),
            max_attempts: None,
        }
    }
}

impl ReconnectPolicy {
    pub fn validate(&self) -> Result<(), String> {
        if self.initial_delay_ms == 0 || self.initial_delay_ms > 60_000 {
            return Err("initial reconnect delay must be between 1 and 60000 ms".into());
        }
        if self.max_delay_ms < self.initial_delay_ms || self.max_delay_ms > 600_000 {
            return Err(
                "maximum reconnect delay must be at least the initial delay and at most 600000 ms"
                    .into(),
            );
        }
        if !self.backoff_factor.is_finite() || !(1.0..=10.0).contains(&self.backoff_factor) {
            return Err("reconnect backoff factor must be between 1 and 10".into());
        }
        if self.max_attempts.is_some_and(|n| n == 0 || n > 10_000) {
            return Err(
                "reconnect attempts must be between 1 and 10000, or null for unlimited".into(),
            );
        }
        Ok(())
    }

    /// Compute the delay before `attempt` (0-based).
    ///
    /// delay = initial_delay_ms * backoff_factor^attempt, clamped to max_delay_ms.
    pub fn delay_for_attempt(&self, attempt: u32) -> Duration {
        let delay_ms = self.initial_delay_ms as f64 * self.backoff_factor.powi(attempt as i32);
        let clamped = delay_ms.min(self.max_delay_ms as f64) as u64;
        Duration::from_millis(clamped)
    }

    /// Returns `true` when another retry should be made.
    ///
    /// - Always `false` when `enabled` is `false`.
    /// - When `max_attempts` is `Some(n)`, `false` once `attempt >= n`.
    pub fn should_retry(&self, attempt: u32) -> bool {
        if !self.enabled {
            return false;
        }
        match self.max_attempts {
            Some(max) => attempt < max,
            None => true,
        }
    }
}

/// Tracks the current reconnection state of a `MasterConnection`.
#[derive(Debug, Clone, Serialize)]
#[serde(tag = "state", rename_all = "snake_case")]
pub enum ReconnectState {
    Idle,
    Reconnecting { attempt: u32 },
    GaveUp { attempts: u32 },
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_policy() {
        let p = ReconnectPolicy::default();
        assert!(p.enabled);
        assert_eq!(p.initial_delay_ms, 1000);
        assert_eq!(p.max_delay_ms, 30_000);
        assert_eq!(p.backoff_factor, 2.0);
        assert!(p.max_attempts.is_none());
    }

    #[test]
    fn test_delay_exponential_backoff() {
        let p = ReconnectPolicy::default();
        assert_eq!(p.delay_for_attempt(0), Duration::from_millis(1000));
        assert_eq!(p.delay_for_attempt(1), Duration::from_millis(2000));
        assert_eq!(p.delay_for_attempt(2), Duration::from_millis(4000));
        assert_eq!(p.delay_for_attempt(3), Duration::from_millis(8000));
    }

    #[test]
    fn test_delay_clamped_to_max() {
        let p = ReconnectPolicy::default();
        // attempt 5: 1000 * 2^5 = 32000 > 30000 => clamped to 30000
        assert_eq!(p.delay_for_attempt(5), Duration::from_millis(30_000));
    }

    #[test]
    fn test_should_retry_unlimited() {
        let p = ReconnectPolicy::default();
        for attempt in [0, 1, 100, 1_000_000] {
            assert!(p.should_retry(attempt));
        }
    }

    #[test]
    fn test_should_retry_limited() {
        let p = ReconnectPolicy {
            max_attempts: Some(3),
            ..ReconnectPolicy::default()
        };
        assert!(p.should_retry(0));
        assert!(p.should_retry(1));
        assert!(p.should_retry(2));
        assert!(!p.should_retry(3));
    }

    #[test]
    fn test_should_retry_disabled() {
        let p = ReconnectPolicy {
            enabled: false,
            ..ReconnectPolicy::default()
        };
        for attempt in [0, 1, 100] {
            assert!(!p.should_retry(attempt));
        }
    }

    #[test]
    fn validates_user_configured_backoff() {
        let mut policy = ReconnectPolicy::default();
        assert!(policy.validate().is_ok());
        policy.max_delay_ms = 500;
        assert!(policy.validate().is_err());
        policy.max_delay_ms = 5000;
        policy.backoff_factor = f64::NAN;
        assert!(policy.validate().is_err());
        policy.backoff_factor = 1.0;
        policy.max_attempts = Some(3);
        assert!(policy.validate().is_ok());
        assert_eq!(policy.delay_for_attempt(2), Duration::from_millis(1000));
        assert!(!policy.should_retry(3));
    }

    #[test]
    fn test_reconnect_state_serde() {
        let state = ReconnectState::Reconnecting { attempt: 3 };
        let json = serde_json::to_string(&state).unwrap();
        assert!(json.contains("\"reconnecting\"") || json.contains("reconnecting"));
        assert!(json.contains("3"));
    }
}
