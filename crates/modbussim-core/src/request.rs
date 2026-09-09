//! Per-connection request limits and pacing, shared by polling, reads, writes and discovery.

use serde::{Deserialize, Serialize};
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::{Mutex, MutexGuard};
use tokio::time::Instant;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct RequestSettings {
    /// Quiet time after a response/error before the next request on this connection.
    pub interval_ms: u64,
    /// FC03/FC04: number of 16-bit registers per request, not decoded values or bytes.
    pub max_read_registers: u16,
    /// FC01/FC02: number of bits per request.
    pub max_read_bits: u16,
}

impl Default for RequestSettings {
    fn default() -> Self {
        Self {
            interval_ms: 0,
            max_read_registers: 125,
            max_read_bits: 2000,
        }
    }
}

impl RequestSettings {
    pub fn validate(&self) -> Result<(), String> {
        if self.interval_ms > 60_000 {
            return Err("request interval must be between 0 and 60000 ms".into());
        }
        if !(1..=125).contains(&self.max_read_registers) {
            return Err("registers per read request must be between 1 and 125".into());
        }
        if !(1..=2000).contains(&self.max_read_bits) {
            return Err("bits per read request must be between 1 and 2000".into());
        }
        Ok(())
    }
}

#[derive(Clone)]
pub struct RequestPacer {
    interval: Duration,
    last_completion: Arc<Mutex<Option<Instant>>>,
}

impl RequestPacer {
    pub fn new(interval: Duration) -> Self {
        Self {
            interval,
            last_completion: Arc::new(Mutex::new(None)),
        }
    }

    pub async fn acquire(&self) -> RequestPermit<'_> {
        let last_completion = self.last_completion.lock().await;
        if let Some(last) = *last_completion {
            tokio::time::sleep_until(last + self.interval).await;
        }
        RequestPermit { last_completion }
    }
}

pub struct RequestPermit<'a> {
    last_completion: MutexGuard<'a, Option<Instant>>,
}

impl Drop for RequestPermit<'_> {
    fn drop(&mut self) {
        *self.last_completion = Some(Instant::now());
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn old_and_partial_settings_keep_defaults() {
        let old: RequestSettings = serde_json::from_str("{}").unwrap();
        assert_eq!(old, RequestSettings::default());
        let partial: RequestSettings = serde_json::from_str(r#"{"interval_ms":50}"#).unwrap();
        assert_eq!(partial.interval_ms, 50);
        assert_eq!(partial.max_read_registers, 125);
        assert_eq!(partial.max_read_bits, 2000);
        assert!(RequestSettings {
            max_read_registers: 0,
            ..old.clone()
        }
        .validate()
        .is_err());
        assert!(RequestSettings {
            max_read_bits: 2001,
            ..old.clone()
        }
        .validate()
        .is_err());
        assert!(RequestSettings {
            interval_ms: 60001,
            ..old
        }
        .validate()
        .is_err());
    }

    #[tokio::test(start_paused = true)]
    async fn gap_starts_after_completion_and_cancelled_wait_does_not_extend_it() {
        let pacer = RequestPacer::new(Duration::from_millis(50));
        let start = Instant::now();
        let permit = pacer.acquire().await;
        tokio::time::sleep(Duration::from_millis(100)).await;
        drop(permit);
        assert!(
            tokio::time::timeout(Duration::from_millis(20), pacer.acquire())
                .await
                .is_err()
        );
        let _next = pacer.acquire().await;
        assert_eq!(Instant::now() - start, Duration::from_millis(150));
    }
}
