//! Application state management for Tauri.
//!
//! Holds all runtime state: slave connections and log collectors.

use modbussim_core::data_source::DataSourceState;
use modbussim_core::log_collector::LogCollector;
use modbussim_core::mutation::{MutationConfig, MutationDirection};
use modbussim_core::register::{RegisterDef, RegisterType};
use modbussim_core::slave::SlaveConnection;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::atomic::AtomicBool;
use std::sync::Arc;
use tokio::sync::RwLock;
use tokio::time::{Duration, Instant};

pub const MUTATION_BASE_TICK_MS: u64 = 100;

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct MutationKey {
    pub connection_id: String,
    pub slave_id: u8,
    pub register_type: RegisterType,
    pub address: u16,
}

impl MutationKey {
    pub fn new(
        connection_id: impl Into<String>,
        slave_id: u8,
        register_type: RegisterType,
        address: u16,
    ) -> Self {
        Self {
            connection_id: connection_id.into(),
            slave_id,
            register_type,
            address,
        }
    }
}

#[derive(Debug, Clone)]
pub struct MutationRuntimeState {
    pub direction: MutationDirection,
    pub next_due: Instant,
    pub definition: RegisterDef,
    pub config: MutationConfig,
}

impl MutationRuntimeState {
    pub fn new(definition: &RegisterDef, config: &MutationConfig) -> Self {
        Self {
            direction: MutationDirection::initial_for(config.mode),
            next_due: Instant::now() + mutation_period(config.period_ms),
            definition: definition.clone(),
            config: config.clone(),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct DataSourceKey {
    pub connection_id: String,
    pub slave_id: u8,
    pub register_type: RegisterType,
    pub address: u16,
}

impl DataSourceKey {
    pub fn new(
        connection_id: impl Into<String>,
        slave_id: u8,
        register_type: RegisterType,
        address: u16,
    ) -> Self {
        Self {
            connection_id: connection_id.into(),
            slave_id,
            register_type,
            address,
        }
    }
}

pub fn mutation_period(period_ms: u64) -> Duration {
    Duration::from_millis(period_ms.max(MUTATION_BASE_TICK_MS))
}

/// Runtime state for a slave connection.
pub struct SlaveConnectionState {
    pub connection: SlaveConnection,
    pub log_collector: Arc<LogCollector>,
}

/// Application state holding all active connections.
pub struct AppState {
    pub slave_connections: Arc<RwLock<HashMap<String, SlaveConnectionState>>>,
    pub next_slave_id: RwLock<u32>,
    pub data_sources: Arc<RwLock<HashMap<DataSourceKey, DataSourceState>>>,
    /// Master switch for the point-mutation tick task.
    pub mutation_running: Arc<AtomicBool>,
    /// Non-persisted scheduling and triangle-wave state for enabled points.
    pub mutation_runtime: Arc<RwLock<HashMap<MutationKey, MutationRuntimeState>>>,
}

impl Default for AppState {
    fn default() -> Self {
        Self {
            slave_connections: Arc::new(RwLock::new(HashMap::new())),
            next_slave_id: RwLock::new(1),
            data_sources: Arc::new(RwLock::new(HashMap::new())),
            mutation_running: Arc::new(AtomicBool::new(false)),
            mutation_runtime: Arc::new(RwLock::new(HashMap::new())),
        }
    }
}

impl AppState {
    pub fn new() -> Self {
        Self::default()
    }
}

// ---------------------------------------------------------------------------
// DTOs for API responses
// ---------------------------------------------------------------------------

/// Information about a slave connection.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub struct SlaveConnectionInfo {
    pub id: String,
    pub bind_address: String,
    pub port: u16,
    pub state: String,
    pub device_count: usize,
    pub clients: Option<Vec<modbussim_core::clients::ClientInfo>>,
}

/// Information about a slave device.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SlaveDeviceInfo {
    pub slave_id: u8,
    pub name: String,
    pub register_count: usize,
}

/// A single register value for reading/writing.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegisterValueInfo {
    pub address: u16,
    pub value: u16,
}
