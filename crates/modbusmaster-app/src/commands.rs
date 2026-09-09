//! Tauri commands for ModbusMaster.

use modbussim_core::project::{self, ProjectFile};

use crate::state::{
    AppState, CachedPollData, ConnectionStateEvent, FoundRegisterDto, LogAppendedEvent,
    MasterConnectionInfo, MasterConnectionState, PollDataPayload, PollErrorPayload, ReadResultDto,
    RegisterScanEvent, RegisterValueDto, ScanGroupInfo, SlaveIdScanEvent,
};
use modbussim_core::log_collector::LogCollector;
use modbussim_core::log_entry::LogEntry;
use modbussim_core::log_helpers;
use modbussim_core::master::{
    scan_registers_paced, scan_slave_ids_paced, MasterConfig, MasterConnection, MasterState,
    ReadFunction, ReadResult, ScanGroup,
};
use modbussim_core::parse::{parse_read_function, read_function_to_string};
use modbussim_core::reconnect::ReconnectPolicy;
use modbussim_core::request::RequestSettings;
use modbussim_core::socks5::Socks5Config;
use modbussim_core::tools;
use modbussim_core::transport::{self, Parity, SerialConfig, TlsConfig, Transport};
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use std::time::Duration;
use tauri::{AppHandle, Emitter, State};
use tokio::sync::{broadcast, mpsc, oneshot, Mutex};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn read_result_to_dto(
    scan_group_id: &str,
    function: ReadFunction,
    start_address: u16,
    result: &ReadResult,
    timestamp: &str,
) -> ReadResultDto {
    let values = match result {
        ReadResult::Coils(vals) => vals
            .iter()
            .enumerate()
            .map(|(i, v)| RegisterValueDto {
                address: start_address + i as u16,
                raw_value: if *v { 1 } else { 0 },
                display_value: if *v {
                    "ON".to_string()
                } else {
                    "OFF".to_string()
                },
                is_bool: true,
            })
            .collect(),
        ReadResult::DiscreteInputs(vals) => vals
            .iter()
            .enumerate()
            .map(|(i, v)| RegisterValueDto {
                address: start_address + i as u16,
                raw_value: if *v { 1 } else { 0 },
                display_value: if *v {
                    "ON".to_string()
                } else {
                    "OFF".to_string()
                },
                is_bool: true,
            })
            .collect(),
        ReadResult::HoldingRegisters(vals) => vals
            .iter()
            .enumerate()
            .map(|(i, v)| RegisterValueDto {
                address: start_address + i as u16,
                raw_value: *v as u64,
                display_value: v.to_string(),
                is_bool: false,
            })
            .collect(),
        ReadResult::InputRegisters(vals) => vals
            .iter()
            .enumerate()
            .map(|(i, v)| RegisterValueDto {
                address: start_address + i as u16,
                raw_value: *v as u64,
                display_value: v.to_string(),
                is_bool: false,
            })
            .collect(),
    };

    ReadResultDto {
        scan_group_id: scan_group_id.to_string(),
        function: read_function_to_string(function).to_string(),
        start_address,
        values,
        timestamp: timestamp.to_string(),
        error: None,
    }
}

fn now_timestamp() -> String {
    chrono_like_now()
}

fn chrono_like_now() -> String {
    use std::time::SystemTime;
    let now = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default();
    let secs = now.as_secs();
    let millis = now.as_millis() % 1000;
    // Simple ISO-ish format
    format!("{}.{:03}", secs, millis)
}

// ---------------------------------------------------------------------------
// Transport Request Types
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum TransportRequest {
    Tcp {
        host: String,
        port: u16,
    },
    TcpTls {
        host: String,
        port: u16,
    },
    Rtu {
        serial_port: String,
        baud_rate: u32,
        data_bits: u8,
        stop_bits: u8,
        parity: String,
    },
    Ascii {
        serial_port: String,
        baud_rate: u32,
        data_bits: u8,
        stop_bits: u8,
        parity: String,
    },
    RtuOverTcp {
        host: String,
        port: u16,
    },
}

fn parse_parity(s: &str) -> Parity {
    match s {
        "odd" => Parity::Odd,
        "even" => Parity::Even,
        _ => Parity::None,
    }
}

fn to_transport(req: &TransportRequest) -> Transport {
    match req {
        TransportRequest::Tcp { host, port } => Transport::Tcp {
            host: host.clone(),
            port: *port,
        },
        TransportRequest::TcpTls { host, port } => Transport::TcpTls {
            host: host.clone(),
            port: *port,
        },
        TransportRequest::Rtu {
            serial_port,
            baud_rate,
            data_bits,
            stop_bits,
            parity,
        } => Transport::Rtu(SerialConfig {
            port: serial_port.clone(),
            baud_rate: *baud_rate,
            data_bits: *data_bits,
            stop_bits: *stop_bits,
            parity: parse_parity(parity),
        }),
        TransportRequest::Ascii {
            serial_port,
            baud_rate,
            data_bits,
            stop_bits,
            parity,
        } => Transport::Ascii(SerialConfig {
            port: serial_port.clone(),
            baud_rate: *baud_rate,
            data_bits: *data_bits,
            stop_bits: *stop_bits,
            parity: parse_parity(parity),
        }),
        TransportRequest::RtuOverTcp { host, port } => Transport::RtuOverTcp {
            host: host.clone(),
            port: *port,
        },
    }
}

// ---------------------------------------------------------------------------
// Connection Commands
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct CreateMasterRequest {
    pub transport: TransportRequest,
    pub slave_id: u8,
    pub timeout_ms: Option<u64>,
    #[serde(default)]
    pub requests: RequestSettings,
    #[serde(default)]
    pub reconnect_policy: ReconnectPolicy,
    pub use_tls: Option<bool>,
    pub ca_file: Option<String>,
    pub cert_file: Option<String>,
    pub key_file: Option<String>,
    pub pkcs12_file: Option<String>,
    pub pkcs12_password: Option<String>,
    pub accept_invalid_certs: Option<bool>,
    pub socks5: Option<Socks5Config>,
}

#[tauri::command]
pub async fn create_master_connection(
    state: State<'_, AppState>,
    request: CreateMasterRequest,
) -> Result<MasterConnectionInfo, String> {
    request.requests.validate()?;
    request.reconnect_policy.validate()?;
    let id = {
        let mut counter = state.next_conn_id.write().await;
        let id = format!("master_{}", *counter);
        *counter += 1;
        id
    };

    let transport = to_transport(&request.transport);
    let socks5 = request.socks5.unwrap_or_default();
    socks5.validate()?;
    if socks5.enabled && matches!(&transport, Transport::Rtu(_) | Transport::Ascii(_)) {
        return Err("SOCKS5 is only supported by TCP-based transports".to_string());
    }

    let (target_address, port) = match &transport {
        Transport::Tcp { host, port }
        | Transport::TcpTls { host, port }
        | Transport::RtuOverTcp { host, port } => (host.clone(), *port),
        Transport::Rtu(sc) | Transport::Ascii(sc) => (sc.port.clone(), 0),
    };

    let config = MasterConfig {
        target_address: target_address.clone(),
        port,
        slave_id: request.slave_id,
        timeout_ms: request.timeout_ms.unwrap_or(3000),
        requests: request.requests,
        tls: TlsConfig {
            enabled: request.use_tls.unwrap_or(false),
            ca_file: request.ca_file.unwrap_or_default(),
            cert_file: request.cert_file.unwrap_or_default(),
            key_file: request.key_file.unwrap_or_default(),
            pkcs12_file: request.pkcs12_file.unwrap_or_default(),
            pkcs12_password: request.pkcs12_password.unwrap_or_default(),
            accept_invalid_certs: request.accept_invalid_certs.unwrap_or(false),
        },
        socks5,
    };

    let log_collector = Arc::new(LogCollector::new());
    let mut connection =
        MasterConnection::new(config.clone(), transport).with_log_collector(log_collector.clone());
    connection.reconnect_policy = request.reconnect_policy;

    let info = MasterConnectionInfo {
        id: id.clone(),
        target_address: config.target_address.clone(),
        port: config.port,
        slave_id: config.slave_id,
        state: format!("{:?}", connection.state()),
        scan_group_count: 0,
    };

    state.master_connections.write().await.insert(
        id,
        MasterConnectionState {
            connection,
            scan_groups: Vec::new(),
            log_collector,
            cached_data: std::collections::HashMap::new(),
            reconnect_handle: Arc::new(Mutex::new(None)),
        },
    );

    Ok(info)
}

// ---------------------------------------------------------------------------
// Connection state helpers
// ---------------------------------------------------------------------------

/// Stable PascalCase tags exposed to the frontend (matches existing
/// listeners that compare against "Connected"/"Disconnected").
fn state_tag(state: MasterState) -> &'static str {
    match state {
        MasterState::Connected => "Connected",
        MasterState::Disconnected => "Disconnected",
        MasterState::Reconnecting => "Reconnecting",
        MasterState::Error => "Error",
    }
}

fn emit_state(app: &AppHandle, conn_id: &str, tag: &str) {
    let _ = app.emit(
        "master-connection-state",
        ConnectionStateEvent {
            id: conn_id.to_string(),
            state: tag.to_string(),
        },
    );
}

/// Abort the supervisor task associated with `conn_id`, if any. Used when
/// the user explicitly disconnects or deletes the connection so that the
/// auto-reconnect loop does not race with manual control.
async fn abort_supervisor(
    master_conns: &Arc<
        tokio::sync::RwLock<std::collections::HashMap<String, MasterConnectionState>>,
    >,
    conn_id: &str,
) {
    let handle_slot = {
        let conns = master_conns.read().await;
        conns.get(conn_id).map(|cs| cs.reconnect_handle.clone())
    };
    if let Some(handle_arc) = handle_slot {
        if let Some(handle) = handle_arc.lock().await.take() {
            handle.abort();
        }
    }
}

/// Long-lived task that watches for transport-loss notifications and
/// drives an exponential-backoff reconnect loop. One instance is spawned
/// per `connect_master` success and lives until either the supervisor is
/// aborted (manual disconnect/delete) or the policy gives up.
async fn run_supervisor(
    app: AppHandle,
    master_conns: Arc<
        tokio::sync::RwLock<std::collections::HashMap<String, MasterConnectionState>>,
    >,
    conn_id: String,
    mut lost_rx: broadcast::Receiver<()>,
) {
    loop {
        // Wait for the next transport-loss signal. Lagged or closed means
        // the supervisor has nothing useful to do.
        match lost_rx.recv().await {
            Ok(()) => {}
            Err(broadcast::error::RecvError::Lagged(_)) => continue,
            Err(broadcast::error::RecvError::Closed) => return,
        }
        // Drain any extras so we only run one reconnect cycle per burst.
        while lost_rx.try_recv().is_ok() {}

        // Snapshot policy + enabled scan groups under a short read lock.
        let snapshot = {
            let conns = master_conns.read().await;
            let Some(cs) = conns.get(&conn_id) else {
                return; // connection deleted
            };
            let policy = cs.connection.reconnect_policy.clone();
            let groups: Vec<String> = cs
                .scan_groups
                .iter()
                .filter(|g| g.enabled)
                .map(|g| g.id.clone())
                .collect();
            (policy, groups)
        };
        let (policy, enabled_groups): (ReconnectPolicy, Vec<String>) = snapshot;

        if !policy.enabled {
            // Auto-reconnect disabled: surface the loss as Disconnected and
            // wait for either the user or another lost signal.
            {
                let mut conns = master_conns.write().await;
                if let Some(cs) = conns.get_mut(&conn_id) {
                    let _ = cs.connection.disconnect().await;
                }
            }
            emit_state(&app, &conn_id, "Disconnected");
            continue;
        }

        emit_state(&app, &conn_id, "Reconnecting");

        // Tear down the dead transport before retrying.
        {
            let mut conns = master_conns.write().await;
            if let Some(cs) = conns.get_mut(&conn_id) {
                let _ = cs.connection.disconnect().await;
            } else {
                return;
            }
        }

        // Backoff retry loop.
        let mut attempt: u32 = 0;
        let outcome = loop {
            tokio::time::sleep(policy.delay_for_attempt(attempt)).await;

            let res = {
                let mut conns = master_conns.write().await;
                let Some(cs) = conns.get_mut(&conn_id) else {
                    return;
                };
                cs.connection.connect().await
            };

            if res.is_ok() {
                break Ok(());
            }
            attempt += 1;
            if !policy.should_retry(attempt) {
                break Err(());
            }
        };

        match outcome {
            Ok(()) => {
                emit_state(&app, &conn_id, "Connected");

                // Refresh subscription so we observe the next loss against
                // the new transport's lifetime.
                let new_rx = {
                    let conns = master_conns.read().await;
                    let Some(cs) = conns.get(&conn_id) else {
                        return;
                    };
                    cs.connection.subscribe_connection_lost()
                };
                lost_rx = new_rx;

                // Restart polling for groups that were enabled before loss.
                for gid in enabled_groups {
                    let _ = start_polling_inner(&app, &master_conns, &conn_id, &gid).await;
                }
            }
            Err(()) => {
                emit_state(&app, &conn_id, "Error");
                return;
            }
        }
    }
}

#[tauri::command]
pub async fn connect_master(
    app: AppHandle,
    state: State<'_, AppState>,
    connection_id: String,
) -> Result<(), String> {
    // Stop any leftover supervisor before we touch the transport.
    abort_supervisor(&state.master_connections, &connection_id).await;

    // Connect, capture a fresh lost-signal receiver, and grab the shared
    // reconnect_handle slot — all under one write lock.
    let (lost_rx, handle_slot, current_state) = {
        let mut conns = state.master_connections.write().await;
        let conn_state = conns
            .get_mut(&connection_id)
            .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

        conn_state
            .connection
            .connect()
            .await
            .map_err(|e| format!("{}", e))?;

        // Wire log-append push to frontend so LogPanel doesn't have to poll.
        let log_handle = app.clone();
        let log_conn_id = connection_id.clone();
        conn_state
            .log_collector
            .set_append_callback(std::sync::Arc::new(move |_entry| {
                let _ = log_handle.emit(
                    "log-appended",
                    LogAppendedEvent {
                        connection_id: log_conn_id.clone(),
                    },
                );
            }));

        (
            conn_state.connection.subscribe_connection_lost(),
            conn_state.reconnect_handle.clone(),
            conn_state.connection.state(),
        )
    };

    emit_state(&app, &connection_id, state_tag(current_state));

    // Spawn the supervisor for auto-reconnect.
    let supervisor = tokio::spawn(run_supervisor(
        app.clone(),
        state.master_connections.clone(),
        connection_id.clone(),
        lost_rx,
    ));
    *handle_slot.lock().await = Some(supervisor);

    Ok(())
}

#[tauri::command]
pub async fn disconnect_master(
    app: AppHandle,
    state: State<'_, AppState>,
    connection_id: String,
) -> Result<(), String> {
    // Cancel any in-flight reconnect first so it cannot race with us.
    abort_supervisor(&state.master_connections, &connection_id).await;

    {
        let mut conns = state.master_connections.write().await;
        let conn_state = conns
            .get_mut(&connection_id)
            .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

        conn_state
            .connection
            .disconnect()
            .await
            .map_err(|e| format!("{}", e))?;
    }

    emit_state(&app, &connection_id, "Disconnected");
    Ok(())
}

#[tauri::command]
pub async fn delete_master_connection(
    state: State<'_, AppState>,
    connection_id: String,
) -> Result<(), String> {
    abort_supervisor(&state.master_connections, &connection_id).await;

    let mut conns = state.master_connections.write().await;
    let mut conn_state = conns
        .remove(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    // Ensure disconnected
    let _ = conn_state.connection.disconnect().await;
    Ok(())
}

#[tauri::command]
pub async fn list_master_connections(
    state: State<'_, AppState>,
) -> Result<Vec<MasterConnectionInfo>, String> {
    let conns = state.master_connections.read().await;
    let mut list: Vec<MasterConnectionInfo> = conns
        .iter()
        .map(|(id, cs)| MasterConnectionInfo {
            id: id.clone(),
            target_address: cs.connection.config.target_address.clone(),
            port: cs.connection.config.port,
            slave_id: cs.connection.config.slave_id,
            state: format!("{:?}", cs.connection.state()),
            scan_group_count: cs.scan_groups.len(),
        })
        .collect();
    list.sort_by(|a, b| a.id.cmp(&b.id));
    Ok(list)
}

// ---------------------------------------------------------------------------
// Scan Group Commands
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct AddScanGroupRequest {
    pub name: String,
    pub function: String,
    pub start_address: u16,
    pub quantity: u16,
    pub interval_ms: u64,
    pub enabled: Option<bool>,
    pub slave_id: Option<u8>,
}

#[tauri::command]
pub async fn add_scan_group(
    state: State<'_, AppState>,
    connection_id: String,
    request: AddScanGroupRequest,
) -> Result<ScanGroupInfo, String> {
    let function = parse_read_function(&request.function)?;
    let group_id = uuid::Uuid::new_v4().to_string();

    let group = ScanGroup {
        id: group_id.clone(),
        name: request.name,
        function,
        start_address: request.start_address,
        quantity: request.quantity,
        interval_ms: request.interval_ms,
        enabled: request.enabled.unwrap_or(true),
        slave_id: request.slave_id,
    };
    group.validate()?;

    let mut conns = state.master_connections.write().await;
    let conn_state = conns
        .get_mut(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    let info = ScanGroupInfo {
        id: group.id.clone(),
        name: group.name.clone(),
        function: read_function_to_string(group.function).to_string(),
        start_address: group.start_address,
        quantity: group.quantity,
        interval_ms: group.interval_ms,
        enabled: group.enabled,
        is_polling: false,
        slave_id: group.slave_id,
    };

    conn_state.scan_groups.push(group);
    Ok(info)
}

#[derive(Debug, Deserialize)]
pub struct UpdateScanGroupRequest {
    pub name: Option<String>,
    pub function: Option<String>,
    pub start_address: Option<u16>,
    pub quantity: Option<u16>,
    pub interval_ms: Option<u64>,
    pub enabled: Option<bool>,
}

#[tauri::command]
pub async fn update_scan_group(
    state: State<'_, AppState>,
    connection_id: String,
    group_id: String,
    request: UpdateScanGroupRequest,
) -> Result<ScanGroupInfo, String> {
    let mut conns = state.master_connections.write().await;
    let conn_state = conns
        .get_mut(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    let group = conn_state
        .scan_groups
        .iter_mut()
        .find(|g| g.id == group_id)
        .ok_or_else(|| format!("Scan group not found: {}", group_id))?;

    let mut updated = group.clone();
    if let Some(name) = request.name {
        updated.name = name;
    }
    if let Some(function) = request.function {
        updated.function = parse_read_function(&function)?;
    }
    if let Some(addr) = request.start_address {
        updated.start_address = addr;
    }
    if let Some(qty) = request.quantity {
        updated.quantity = qty;
    }
    if let Some(ms) = request.interval_ms {
        updated.interval_ms = ms;
    }
    if let Some(enabled) = request.enabled {
        updated.enabled = enabled;
    }
    updated.validate()?;
    *group = updated;

    let is_polling = conn_state.connection.is_scan_active(&group_id);

    Ok(ScanGroupInfo {
        id: group.id.clone(),
        name: group.name.clone(),
        function: read_function_to_string(group.function).to_string(),
        start_address: group.start_address,
        quantity: group.quantity,
        interval_ms: group.interval_ms,
        enabled: group.enabled,
        is_polling,
        slave_id: group.slave_id,
    })
}

#[tauri::command]
pub async fn remove_scan_group(
    state: State<'_, AppState>,
    connection_id: String,
    group_id: String,
) -> Result<(), String> {
    let mut conns = state.master_connections.write().await;
    let conn_state = conns
        .get_mut(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    // Stop polling if active
    conn_state
        .connection
        .stop_scan_group(&group_id)
        .await
        .map_err(|e| format!("{}", e))?;

    conn_state.scan_groups.retain(|g| g.id != group_id);
    conn_state.cached_data.remove(&group_id);
    Ok(())
}

#[tauri::command]
pub async fn list_scan_groups(
    state: State<'_, AppState>,
    connection_id: String,
) -> Result<Vec<ScanGroupInfo>, String> {
    let conns = state.master_connections.read().await;
    let conn_state = conns
        .get(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    Ok(conn_state
        .scan_groups
        .iter()
        .map(|g| ScanGroupInfo {
            id: g.id.clone(),
            name: g.name.clone(),
            function: read_function_to_string(g.function).to_string(),
            start_address: g.start_address,
            quantity: g.quantity,
            interval_ms: g.interval_ms,
            enabled: g.enabled,
            is_polling: conn_state.connection.is_scan_active(&g.id),
            slave_id: g.slave_id,
        })
        .collect())
}

// ---------------------------------------------------------------------------
// Polling Commands
// ---------------------------------------------------------------------------

/// Internal helper: start polling for a single group and spawn bridge task.
async fn start_polling_inner(
    app: &AppHandle,
    master_conns: &std::sync::Arc<
        tokio::sync::RwLock<std::collections::HashMap<String, MasterConnectionState>>,
    >,
    connection_id: &str,
    group_id: &str,
) -> Result<(), String> {
    let (mut rx, function, start_address) = {
        let mut conns = master_conns.write().await;
        let conn_state = conns
            .get_mut(connection_id)
            .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

        let group = conn_state
            .scan_groups
            .iter()
            .find(|g| g.id == group_id)
            .cloned()
            .ok_or_else(|| format!("Scan group not found: {}", group_id))?;

        let rx = conn_state
            .connection
            .start_scan_group(&group)
            .await
            .map_err(|e| format!("{}", e))?;

        (rx, group.function, group.start_address)
    };

    // Spawn a bridge task to forward poll events to Tauri events and cache data
    let app_handle = app.clone();
    let conn_id = connection_id.to_string();
    let sg_id = group_id.to_string();
    let cache_ref = master_conns.clone();

    tokio::spawn(async move {
        while let Some(event) = rx.recv().await {
            match event {
                modbussim_core::master::PollEvent::Data(result) => {
                    let ts = now_timestamp();
                    let dto = read_result_to_dto(&sg_id, function, start_address, &result, &ts);

                    // Update cache
                    {
                        let mut conns = cache_ref.write().await;
                        if let Some(cs) = conns.get_mut(&conn_id) {
                            cs.cached_data.insert(
                                sg_id.clone(),
                                CachedPollData {
                                    result: result.clone(),
                                    timestamp: ts.clone(),
                                },
                            );
                        }
                    }

                    let _ = app_handle.emit(
                        "master-poll-data",
                        PollDataPayload {
                            connection_id: conn_id.clone(),
                            scan_group_id: sg_id.clone(),
                            result: dto,
                        },
                    );
                }
                modbussim_core::master::PollEvent::Error(err) => {
                    let _ = app_handle.emit(
                        "master-poll-error",
                        PollErrorPayload {
                            connection_id: conn_id.clone(),
                            scan_group_id: sg_id.clone(),
                            error: err,
                        },
                    );
                }
            }
        }
    });

    Ok(())
}

#[tauri::command]
pub async fn start_polling(
    app: AppHandle,
    state: State<'_, AppState>,
    connection_id: String,
    group_id: String,
) -> Result<(), String> {
    let master_conns = state.master_connections.clone();
    start_polling_inner(&app, &master_conns, &connection_id, &group_id).await
}

#[tauri::command]
pub async fn stop_polling(
    state: State<'_, AppState>,
    connection_id: String,
    group_id: String,
) -> Result<(), String> {
    let mut conns = state.master_connections.write().await;
    let conn_state = conns
        .get_mut(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    conn_state
        .connection
        .stop_scan_group(&group_id)
        .await
        .map_err(|e| format!("{}", e))?;

    Ok(())
}

#[tauri::command]
pub async fn start_all_polling(
    app: AppHandle,
    state: State<'_, AppState>,
    connection_id: String,
) -> Result<(), String> {
    let master_conns = state.master_connections.clone();

    // Collect enabled group IDs
    let group_ids: Vec<String> = {
        let conns = master_conns.read().await;
        let conn_state = conns
            .get(&connection_id)
            .ok_or_else(|| format!("Connection not found: {}", connection_id))?;
        conn_state
            .scan_groups
            .iter()
            .filter(|g| g.enabled)
            .map(|g| g.id.clone())
            .collect()
    };

    for group_id in group_ids {
        start_polling_inner(&app, &master_conns, &connection_id, &group_id).await?;
    }
    Ok(())
}

#[tauri::command]
pub async fn stop_all_polling(
    state: State<'_, AppState>,
    connection_id: String,
) -> Result<(), String> {
    let mut conns = state.master_connections.write().await;
    let conn_state = conns
        .get_mut(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    conn_state.connection.stop_all_scans().await;
    Ok(())
}

#[tauri::command]
pub async fn get_poll_data(
    state: State<'_, AppState>,
    connection_id: String,
    group_id: String,
) -> Result<Option<ReadResultDto>, String> {
    let conns = state.master_connections.read().await;
    let conn_state = conns
        .get(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    let group = conn_state
        .scan_groups
        .iter()
        .find(|g| g.id == group_id)
        .ok_or_else(|| format!("Scan group not found: {}", group_id))?;

    if let Some(cached) = conn_state.cached_data.get(&group_id) {
        Ok(Some(read_result_to_dto(
            &group_id,
            group.function,
            group.start_address,
            &cached.result,
            &cached.timestamp,
        )))
    } else {
        Ok(None)
    }
}

// ---------------------------------------------------------------------------
// Read/Write Commands
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct ReadOnceRequest {
    pub function: String,
    pub start_address: u16,
    pub quantity: u16,
}

#[tauri::command]
pub async fn read_once(
    state: State<'_, AppState>,
    connection_id: String,
    request: ReadOnceRequest,
) -> Result<ReadResultDto, String> {
    let function = parse_read_function(&request.function)?;
    let mut conns = state.master_connections.write().await;
    let conn_state = conns
        .get_mut(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    let result = conn_state
        .connection
        .read(function, request.start_address, request.quantity)
        .await
        .map_err(|e| format!("{}", e))?;

    let ts = now_timestamp();
    Ok(read_result_to_dto(
        "__read_once__",
        function,
        request.start_address,
        &result,
        &ts,
    ))
}

#[derive(Debug, Deserialize)]
pub struct WriteSingleRegRequest {
    pub address: u16,
    pub value: u16,
}

#[tauri::command]
pub async fn write_single_register(
    state: State<'_, AppState>,
    connection_id: String,
    request: WriteSingleRegRequest,
) -> Result<(), String> {
    let mut conns = state.master_connections.write().await;
    let conn_state = conns
        .get_mut(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    conn_state
        .connection
        .write_single_register(request.address, request.value)
        .await
        .map_err(|e| format!("{}", e))
}

#[derive(Debug, Deserialize)]
pub struct WriteSingleCoilRequest {
    pub address: u16,
    pub value: bool,
}

#[tauri::command]
pub async fn write_single_coil(
    state: State<'_, AppState>,
    connection_id: String,
    request: WriteSingleCoilRequest,
) -> Result<(), String> {
    let mut conns = state.master_connections.write().await;
    let conn_state = conns
        .get_mut(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    conn_state
        .connection
        .write_single_coil(request.address, request.value)
        .await
        .map_err(|e| format!("{}", e))
}

#[derive(Debug, Deserialize)]
pub struct WriteMultiRegsRequest {
    pub address: u16,
    pub values: Vec<u16>,
}

#[tauri::command]
pub async fn write_multiple_registers(
    state: State<'_, AppState>,
    connection_id: String,
    request: WriteMultiRegsRequest,
) -> Result<(), String> {
    let mut conns = state.master_connections.write().await;
    let conn_state = conns
        .get_mut(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    conn_state
        .connection
        .write_multiple_registers(request.address, &request.values)
        .await
        .map_err(|e| format!("{}", e))
}

#[derive(Debug, Deserialize)]
pub struct WriteMultiCoilsRequest {
    pub address: u16,
    pub values: Vec<bool>,
}

#[tauri::command]
pub async fn write_multiple_coils(
    state: State<'_, AppState>,
    connection_id: String,
    request: WriteMultiCoilsRequest,
) -> Result<(), String> {
    let mut conns = state.master_connections.write().await;
    let conn_state = conns
        .get_mut(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    conn_state
        .connection
        .write_multiple_coils(request.address, &request.values)
        .await
        .map_err(|e| format!("{}", e))
}

// ---------------------------------------------------------------------------
// Log Commands
// ---------------------------------------------------------------------------

#[tauri::command]
pub async fn get_communication_logs(
    state: State<'_, AppState>,
    connection_id: String,
) -> Result<Vec<LogEntry>, String> {
    let conns = state.master_connections.read().await;
    let conn_state = conns
        .get(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    Ok(log_helpers::get_all_logs(&conn_state.log_collector).await)
}

#[tauri::command]
pub async fn clear_communication_logs(
    state: State<'_, AppState>,
    connection_id: String,
) -> Result<(), String> {
    let conns = state.master_connections.read().await;
    let conn_state = conns
        .get(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    log_helpers::clear_logs(&conn_state.log_collector).await;
    Ok(())
}

#[tauri::command]
pub async fn export_logs_csv(
    state: State<'_, AppState>,
    connection_id: String,
) -> Result<String, String> {
    let conns = state.master_connections.read().await;
    let conn_state = conns
        .get(&connection_id)
        .ok_or_else(|| format!("Connection not found: {}", connection_id))?;

    Ok(log_helpers::export_csv(&conn_state.log_collector).await)
}

// ---------------------------------------------------------------------------
// Tool Commands (stateless, reuse core logic)
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct PlcToModbusRequest {
    pub plc_address: u32,
}

#[derive(Debug, Serialize)]
pub struct PlcToModbusResult {
    pub register_type: String,
    pub modbus_address: u16,
}

#[tauri::command]
pub fn convert_plc_to_modbus(request: PlcToModbusRequest) -> Result<PlcToModbusResult, String> {
    let result = tools::plc_to_modbus_address(request.plc_address).map_err(|e| format!("{}", e))?;
    Ok(PlcToModbusResult {
        register_type: format!("{:?}", result.address_type),
        modbus_address: result.address,
    })
}

#[derive(Debug, Deserialize)]
pub struct ModbusToPlcRequest {
    pub register_type: String,
    pub modbus_address: u16,
}

#[tauri::command]
pub fn convert_modbus_to_plc(request: ModbusToPlcRequest) -> Result<u32, String> {
    let addr_type = match request.register_type.as_str() {
        "Coil" | "coil" => tools::ModbusAddressType::Coil,
        "DiscreteInput" | "discrete_input" => tools::ModbusAddressType::DiscreteInput,
        "InputRegister" | "input_register" => tools::ModbusAddressType::InputRegister,
        "HoldingRegister" | "holding_register" => tools::ModbusAddressType::HoldingRegister,
        _ => return Err(format!("Unknown register type: {}", request.register_type)),
    };
    Ok(tools::modbus_to_plc_address(
        request.modbus_address,
        addr_type,
    ))
}

#[tauri::command]
pub fn calculate_crc16(data: Vec<u8>) -> u16 {
    tools::crc16(&data)
}

#[tauri::command]
pub fn calculate_lrc(data: Vec<u8>) -> u8 {
    tools::lrc(&data)
}

// ---------------------------------------------------------------------------
// Scan Commands
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct SlaveIdScanRequest {
    pub start_id: Option<u8>,
    pub end_id: Option<u8>,
    pub timeout_ms: Option<u64>,
}

#[tauri::command]
pub async fn start_slave_id_scan(
    app: AppHandle,
    state: State<'_, AppState>,
    connection_id: String,
    request: SlaveIdScanRequest,
) -> Result<(), String> {
    let timeout_ms = request.timeout_ms.unwrap_or(500);
    let start_id = request.start_id.unwrap_or(1);
    let end_id = request.end_id.unwrap_or(247);

    // Extract ctx handle and original slave_id (short lock)
    let (ctx_handle, original_slave_id, pacer) = {
        let conns = state.master_connections.read().await;
        let cs = conns
            .get(&connection_id)
            .ok_or_else(|| format!("connection {} not found", connection_id))?;
        let ctx = cs.connection.get_ctx_handle().map_err(|e| e.to_string())?;
        (
            ctx,
            cs.connection.config.slave_id,
            cs.connection.request_pacer(),
        )
    };

    // Create cancel channel
    let (cancel_tx, cancel_rx) = oneshot::channel();
    let scan_key = format!("{}:slave_scan", connection_id);
    state
        .active_scans
        .write()
        .await
        .insert(scan_key.clone(), cancel_tx);

    let (progress_tx, mut progress_rx) = mpsc::channel(32);
    let conn_id = connection_id.clone();
    let active_scans = state.active_scans.clone();
    let scan_key_clone = scan_key.clone();

    // Spawn scan task
    tokio::spawn(async move {
        scan_slave_ids_paced(
            ctx_handle,
            original_slave_id,
            start_id,
            end_id,
            Duration::from_millis(timeout_ms),
            pacer,
            cancel_rx,
            progress_tx,
        )
        .await;
    });

    // Spawn bridge task to forward progress as Tauri events
    tokio::spawn(async move {
        while let Some(progress) = progress_rx.recv().await {
            let _ = app.emit(
                "scan-slave-progress",
                SlaveIdScanEvent {
                    connection_id: conn_id.clone(),
                    current_id: progress.current_id,
                    total: progress.total,
                    found_ids: progress.found_ids,
                    done: progress.done,
                    cancelled: progress.cancelled,
                },
            );
            if progress.done || progress.cancelled {
                break;
            }
        }
        // Cleanup
        active_scans.write().await.remove(&scan_key_clone);
    });

    Ok(())
}

#[derive(Debug, Deserialize)]
pub struct RegisterScanRequest {
    pub function: String,
    pub start_address: u16,
    pub end_address: u16,
    pub chunk_size: Option<u16>,
    pub timeout_ms: Option<u64>,
}

#[tauri::command]
pub async fn start_register_scan(
    app: AppHandle,
    state: State<'_, AppState>,
    connection_id: String,
    request: RegisterScanRequest,
) -> Result<(), String> {
    let function = parse_read_function(&request.function)?;
    let mut chunk_size = request.chunk_size.unwrap_or(10);
    let timeout_ms = request.timeout_ms.unwrap_or(1000);

    // Extract ctx handle (short lock)
    let (ctx_handle, slave_id, pacer) = {
        let conns = state.master_connections.read().await;
        let cs = conns
            .get(&connection_id)
            .ok_or_else(|| format!("connection {} not found", connection_id))?;
        let limits = &cs.connection.config.requests;
        let limit = match function {
            ReadFunction::ReadCoils | ReadFunction::ReadDiscreteInputs => limits.max_read_bits,
            _ => limits.max_read_registers,
        };
        chunk_size = chunk_size.clamp(1, limit);
        (
            cs.connection.get_ctx_handle().map_err(|e| e.to_string())?,
            cs.connection.config.slave_id,
            cs.connection.request_pacer(),
        )
    };

    // Create cancel channel
    let (cancel_tx, cancel_rx) = oneshot::channel();
    let scan_key = format!("{}:register_scan", connection_id);
    state
        .active_scans
        .write()
        .await
        .insert(scan_key.clone(), cancel_tx);

    let (progress_tx, mut progress_rx) = mpsc::channel(32);
    let conn_id = connection_id.clone();
    let active_scans = state.active_scans.clone();
    let scan_key_clone = scan_key.clone();
    let end_address = request.end_address;

    // Spawn scan task
    tokio::spawn(async move {
        scan_registers_paced(
            ctx_handle,
            Some(slave_id),
            function,
            request.start_address,
            end_address,
            chunk_size,
            Duration::from_millis(timeout_ms),
            pacer,
            cancel_rx,
            progress_tx,
        )
        .await;
    });

    // Spawn bridge task
    tokio::spawn(async move {
        while let Some(progress) = progress_rx.recv().await {
            let _ = app.emit(
                "scan-register-progress",
                RegisterScanEvent {
                    connection_id: conn_id.clone(),
                    current_address: progress.current_address,
                    end_address: progress.end_address,
                    found_count: progress.found_registers.len() as u16,
                    found_registers: progress
                        .found_registers
                        .iter()
                        .map(|r| FoundRegisterDto {
                            address: r.address,
                            value: r.value,
                        })
                        .collect(),
                    done: progress.done,
                    cancelled: progress.cancelled,
                },
            );
            if progress.done || progress.cancelled {
                break;
            }
        }
        active_scans.write().await.remove(&scan_key_clone);
    });

    Ok(())
}

#[tauri::command]
pub async fn cancel_scan(
    state: State<'_, AppState>,
    connection_id: String,
    scan_type: String,
) -> Result<(), String> {
    let scan_key = format!("{}:{}", connection_id, scan_type);
    let sender = state.active_scans.write().await.remove(&scan_key);
    if let Some(tx) = sender {
        let _ = tx.send(());
    }
    Ok(())
}

#[tauri::command]
pub fn parse_hex(hex_string: String) -> Result<Vec<u8>, String> {
    tools::parse_hex_string(&hex_string).map_err(|e| format!("{}", e))
}

// ---------------------------------------------------------------------------
// Project File Commands
// ---------------------------------------------------------------------------

fn read_function_to_fc(function: ReadFunction) -> u8 {
    match function {
        ReadFunction::ReadCoils => 1,
        ReadFunction::ReadDiscreteInputs => 2,
        ReadFunction::ReadHoldingRegisters => 3,
        ReadFunction::ReadInputRegisters => 4,
    }
}

#[tauri::command]
pub async fn save_project_file(state: State<'_, AppState>, path: String) -> Result<(), String> {
    let conns = state.master_connections.read().await;
    let mut proj = ProjectFile::new_master();

    for (id, conn_state) in conns.iter() {
        let config = &conn_state.connection.config;
        let (name, proj_transport) = match &conn_state.connection.transport {
            Transport::Tcp { host, port } => (
                format!("{}:{}", host, port),
                project::TransportConfig::Tcp {
                    host: host.clone(),
                    port: *port,
                },
            ),
            Transport::RtuOverTcp { host, port } => (
                format!("rtu-tcp://{}:{}", host, port),
                project::TransportConfig::RtuOverTcp {
                    host: host.clone(),
                    port: *port,
                },
            ),
            Transport::Rtu(sc) => (
                format!("rtu://{}", sc.port),
                project::TransportConfig::Rtu {
                    port: sc.port.clone(),
                    baud_rate: sc.baud_rate,
                    data_bits: sc.data_bits,
                    stop_bits: sc.stop_bits,
                    parity: format!("{:?}", sc.parity).to_lowercase(),
                },
            ),
            Transport::Ascii(sc) => (
                format!("ascii://{}", sc.port),
                project::TransportConfig::Ascii {
                    port: sc.port.clone(),
                    baud_rate: sc.baud_rate,
                    data_bits: sc.data_bits,
                    stop_bits: sc.stop_bits,
                    parity: format!("{:?}", sc.parity).to_lowercase(),
                },
            ),
            Transport::TcpTls { host, port } => (
                format!("tls://{}:{}", host, port),
                project::TransportConfig::TcpTls {
                    host: host.clone(),
                    port: *port,
                    client_tls: Box::new(config.tls.clone()),
                    server_tls: Default::default(),
                },
            ),
        };
        let conn_config = project::ConnectionConfig {
            id: id.clone(),
            name,
            transport: proj_transport,
            devices: vec![],
            scan_groups: conn_state
                .scan_groups
                .iter()
                .map(|sg| project::ScanGroupConfig {
                    id: sg.id.clone(),
                    name: sg.name.clone(),
                    slave_id: sg.slave_id.unwrap_or(config.slave_id),
                    function_code: read_function_to_fc(sg.function),
                    start_address: sg.start_address,
                    count: sg.quantity,
                    interval_ms: sg.interval_ms,
                    enabled: sg.enabled,
                })
                .collect(),
            default_slave_id: config.slave_id,
            timeout_ms: config.timeout_ms,
            requests: config.requests.clone(),
            reconnect_policy: conn_state.connection.reconnect_policy.clone(),
            socks5: config.socks5.clone(),
        };
        proj.connections.push(conn_config);
    }

    project::save_project(&proj, std::path::Path::new(&path))
}

fn read_function_from_fc(function_code: u8) -> Result<ReadFunction, String> {
    match function_code {
        1 => Ok(ReadFunction::ReadCoils),
        2 => Ok(ReadFunction::ReadDiscreteInputs),
        3 => Ok(ReadFunction::ReadHoldingRegisters),
        4 => Ok(ReadFunction::ReadInputRegisters),
        _ => Err(format!("unsupported read function code {function_code}")),
    }
}

fn master_transport_from_project(
    transport: project::TransportConfig,
) -> (Transport, TlsConfig, String, u16) {
    match transport {
        project::TransportConfig::Tcp { host, port } => (
            Transport::Tcp {
                host: host.clone(),
                port,
            },
            TlsConfig::default(),
            host,
            port,
        ),
        project::TransportConfig::TcpTls {
            host,
            port,
            client_tls,
            ..
        } => (
            Transport::TcpTls {
                host: host.clone(),
                port,
            },
            *client_tls,
            host,
            port,
        ),
        project::TransportConfig::RtuOverTcp { host, port } => (
            Transport::RtuOverTcp {
                host: host.clone(),
                port,
            },
            TlsConfig::default(),
            host,
            port,
        ),
        project::TransportConfig::Rtu {
            port,
            baud_rate,
            data_bits,
            stop_bits,
            parity,
        } => {
            let serial = SerialConfig {
                port: port.clone(),
                baud_rate,
                data_bits,
                stop_bits,
                parity: parse_parity(&parity),
            };
            (Transport::Rtu(serial), TlsConfig::default(), port, 0)
        }
        project::TransportConfig::Ascii {
            port,
            baud_rate,
            data_bits,
            stop_bits,
            parity,
        } => {
            let serial = SerialConfig {
                port: port.clone(),
                baud_rate,
                data_bits,
                stop_bits,
                parity: parse_parity(&parity),
            };
            (Transport::Ascii(serial), TlsConfig::default(), port, 0)
        }
    }
}

#[tauri::command]
pub async fn load_project_file(state: State<'_, AppState>, path: String) -> Result<usize, String> {
    let project = project::load_project(std::path::Path::new(&path))?;
    if project.project_type != project::ProjectType::Master {
        return Err("project is not a master project".to_string());
    }

    let mut loaded = std::collections::HashMap::new();
    let mut total_scan_groups = 0;
    for config in project.connections {
        if loaded.contains_key(&config.id) {
            return Err(format!("duplicate connection id {}", config.id));
        }
        if !(1..=247).contains(&config.default_slave_id) {
            return Err(format!(
                "connection {} has invalid default slave ID {}",
                config.id, config.default_slave_id
            ));
        }
        config
            .socks5
            .validate()
            .map_err(|error| format!("connection {}: {error}", config.id))?;
        if config.socks5.enabled
            && matches!(
                &config.transport,
                project::TransportConfig::Rtu { .. } | project::TransportConfig::Ascii { .. }
            )
        {
            return Err(format!(
                "connection {} uses SOCKS5 with a serial transport",
                config.id
            ));
        }
        let socks5 = config.socks5.clone();
        config.requests.validate()?;
        config.reconnect_policy.validate()?;
        let (transport, tls, target_address, port) =
            master_transport_from_project(config.transport);
        let master_config = MasterConfig {
            target_address,
            port,
            slave_id: config.default_slave_id,
            timeout_ms: config.timeout_ms.max(1),
            requests: config.requests,
            tls,
            socks5,
        };
        let log_collector = Arc::new(LogCollector::new());
        let mut connection = MasterConnection::new(master_config, transport)
            .with_log_collector(log_collector.clone());
        connection.reconnect_policy = config.reconnect_policy;
        let mut scan_groups = Vec::with_capacity(config.scan_groups.len());
        for scan in config.scan_groups {
            let function = read_function_from_fc(scan.function_code)?;
            if scan.count == 0 || (scan.start_address as u32 + scan.count as u32) > 65_536 {
                return Err(format!("invalid scan group {} address or count", scan.name));
            }
            if scan.interval_ms == 0 || !(1..=247).contains(&scan.slave_id) {
                return Err(format!(
                    "invalid scan group {} interval or slave ID",
                    scan.name
                ));
            }
            scan_groups.push(ScanGroup {
                id: if scan.id.is_empty() {
                    uuid::Uuid::new_v4().to_string()
                } else {
                    scan.id
                },
                name: scan.name,
                function,
                start_address: scan.start_address,
                quantity: scan.count,
                interval_ms: scan.interval_ms,
                enabled: scan.enabled,
                slave_id: Some(scan.slave_id),
            });
            total_scan_groups += 1;
        }
        loaded.insert(
            config.id,
            MasterConnectionState {
                connection,
                scan_groups,
                log_collector,
                cached_data: std::collections::HashMap::new(),
                reconnect_handle: Arc::new(Mutex::new(None)),
            },
        );
    }

    let active_scans = {
        let mut scans = state.active_scans.write().await;
        scans.drain().map(|(_, sender)| sender).collect::<Vec<_>>()
    };
    for sender in active_scans {
        let _ = sender.send(());
    }

    let mut current = state.master_connections.write().await;
    for connection in current.values_mut() {
        if let Some(handle) = connection.reconnect_handle.lock().await.take() {
            handle.abort();
        }
        connection
            .connection
            .disconnect()
            .await
            .map_err(|error| format!("failed to disconnect current connection: {error}"))?;
    }
    *current = loaded;
    let next_id = current
        .keys()
        .filter_map(|id| id.strip_prefix("master_")?.parse::<u32>().ok())
        .max()
        .unwrap_or(0)
        + 1;
    drop(current);
    *state.next_conn_id.write().await = next_id;
    Ok(total_scan_groups)
}

// ---------------------------------------------------------------------------
// Serial Port Commands
// ---------------------------------------------------------------------------

#[tauri::command]
pub fn list_serial_ports() -> Vec<transport::SerialPortInfo> {
    transport::list_serial_ports()
}
