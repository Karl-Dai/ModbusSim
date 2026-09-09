use crate::{
    commands::{build_master_connection, CreateMasterRequest},
    state::AppState,
};
use modbussim_core::{
    master::{MasterConfig, MasterState},
    reconnect::ReconnectPolicy,
    transport::Transport,
};
use serde::Serialize;
use tauri::State;

#[derive(Serialize)]
pub struct MasterConnectionSettings {
    config: MasterConfig,
    transport: Transport,
    reconnect_policy: ReconnectPolicy,
}

#[tauri::command]
pub async fn get_master_connection_settings(
    state: State<'_, AppState>,
    connection_id: String,
) -> Result<MasterConnectionSettings, String> {
    let connections = state.master_connections.read().await;
    let connection = &connections
        .get(&connection_id)
        .ok_or("Connection not found")?
        .connection;
    Ok(MasterConnectionSettings {
        config: connection.config.clone(),
        transport: connection.transport.clone(),
        reconnect_policy: connection.reconnect_policy.clone(),
    })
}

async fn update_settings(
    state: &AppState,
    connection_id: &str,
    request: CreateMasterRequest,
) -> Result<(), String> {
    let mut connections = state.master_connections.write().await;
    let entry = connections
        .get_mut(connection_id)
        .ok_or("Connection not found")?;
    if entry.connection.state() != MasterState::Disconnected
        || entry.connection.is_polling()
        || entry
            .reconnect_handle
            .lock()
            .await
            .as_ref()
            .is_some_and(|handle| !handle.is_finished())
    {
        return Err("Disconnect the connection before changing its settings".into());
    }

    // Rebuild the disconnected runtime so request pacing also uses the new
    // settings. Keep the entry, scan groups and log collector intact.
    let connection = build_master_connection(request, entry.log_collector.clone())?;
    match &connection.transport {
        Transport::Tcp { host, port }
        | Transport::TcpTls { host, port }
        | Transport::RtuOverTcp { host, port } => {
            if host.trim().is_empty() || *port == 0 {
                return Err("A target address and port between 1 and 65535 are required".into());
            }
        }
        Transport::Rtu(serial) | Transport::Ascii(serial) => {
            if serial.port.trim().is_empty()
                || serial.baud_rate == 0
                || !(5..=8).contains(&serial.data_bits)
                || !(1..=2).contains(&serial.stop_bits)
            {
                return Err("Invalid serial port settings".into());
            }
        }
    }
    if !(1..=247).contains(&connection.config.slave_id) || connection.config.timeout_ms == 0 {
        return Err("Slave ID must be between 1 and 247 and timeout must be positive".into());
    }
    entry.connection = connection;
    entry.cached_data.clear();
    Ok(())
}

#[tauri::command]
pub async fn update_master_connection(
    state: State<'_, AppState>,
    connection_id: String,
    request: CreateMasterRequest,
) -> Result<(), String> {
    update_settings(&state, &connection_id, request).await
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::MasterConnectionState;
    use modbussim_core::{log_collector::LogCollector, master::ScanGroup};
    use serde_json::json;
    use std::{collections::HashMap, sync::Arc, time::Duration};
    use tokio::{net::TcpListener, sync::Mutex};

    fn request(port: u16) -> CreateMasterRequest {
        serde_json::from_value(json!({
            "transport": { "type": "tcp", "host": "127.0.0.1", "port": port },
            "slave_id": 7, "timeout_ms": 1500,
            "requests": { "interval_ms": 25, "max_read_registers": 60, "max_read_bits": 1000 }
        }))
        .unwrap()
    }

    async fn fixture() -> AppState {
        let state = AppState::new();
        let log_collector = Arc::new(LogCollector::new());
        state.master_connections.write().await.insert(
            "existing".into(),
            MasterConnectionState {
                connection: build_master_connection(request(502), log_collector.clone()).unwrap(),
                scan_groups: vec![ScanGroup {
                    id: "group_1".into(),
                    name: "Keep me".into(),
                    function: modbussim_core::master::ReadFunction::ReadHoldingRegisters,
                    start_address: 42,
                    quantity: 10,
                    interval_ms: 500,
                    enabled: true,
                    slave_id: Some(3),
                }],
                log_collector,
                cached_data: HashMap::new(),
                reconnect_handle: Arc::new(Mutex::new(None)),
            },
        );
        state
    }

    #[tokio::test]
    async fn edits_existing_connection_and_connects_to_new_endpoint() {
        let state = fixture().await;
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();
        update_settings(&state, "existing", request(port))
            .await
            .unwrap();
        let mut connections = state.master_connections.write().await;
        assert_eq!(connections.len(), 1);
        let entry = connections.get_mut("existing").unwrap();
        assert_eq!(entry.scan_groups[0].id, "group_1");
        assert_eq!(entry.scan_groups[0].start_address, 42);
        assert_eq!(entry.scan_groups[0].slave_id, Some(3));
        assert_eq!(entry.connection.config.port, port);
        assert_eq!(entry.connection.config.slave_id, 7);
        assert_eq!(entry.connection.config.timeout_ms, 1500);
        entry.connection.connect().await.unwrap();
        let (_socket, _) = tokio::time::timeout(Duration::from_secs(2), listener.accept())
            .await
            .unwrap()
            .unwrap();
        drop(connections);
        assert!(update_settings(&state, "existing", request(502))
            .await
            .unwrap_err()
            .contains("Disconnect"));
        state
            .master_connections
            .write()
            .await
            .get_mut("existing")
            .unwrap()
            .connection
            .disconnect()
            .await
            .unwrap();
    }

    #[tokio::test]
    async fn invalid_edits_preserve_original_configuration() {
        let state = fixture().await;
        assert!(update_settings(&state, "existing", request(0))
            .await
            .is_err());
        let mut invalid = request(15020);
        invalid.requests.max_read_registers = 126;
        assert!(update_settings(&state, "existing", invalid).await.is_err());
        let connections = state.master_connections.read().await;
        assert_eq!(connections["existing"].connection.config.port, 502);
        assert_eq!(connections["existing"].scan_groups.len(), 1);
    }

    #[tokio::test]
    async fn active_reconnect_supervisor_blocks_edits() {
        let state = fixture().await;
        let slot = state.master_connections.read().await["existing"]
            .reconnect_handle
            .clone();
        *slot.lock().await = Some(tokio::spawn(std::future::pending()));
        assert!(update_settings(&state, "existing", request(15020))
            .await
            .unwrap_err()
            .contains("Disconnect"));
        slot.lock().await.take().unwrap().abort();
    }

    #[tokio::test]
    async fn saves_tls_proxy_and_serial_settings() {
        let state = fixture().await;
        let tls_request = serde_json::from_value(json!({
            "transport": { "type": "tcp_tls", "host": "example.test", "port": 802 },
            "slave_id": 2, "timeout_ms": 5000, "use_tls": true,
            "ca_file": "ca.pem", "pkcs12_file": "client.p12", "pkcs12_password": "test-only",
            "socks5": { "enabled": true, "host": "127.0.0.1", "port": 1080, "username": "", "password": "" }
        })).unwrap();
        update_settings(&state, "existing", tls_request)
            .await
            .unwrap();
        {
            let connections = state.master_connections.read().await;
            let config = &connections["existing"].connection.config;
            assert_eq!(config.tls.ca_file, "ca.pem");
            assert_eq!(config.tls.pkcs12_password, "test-only");
            assert!(config.socks5.enabled);
        }
        let serial_request = serde_json::from_value(json!({
            "transport": { "type": "rtu", "serial_port": "/dev/test", "baud_rate": 19200, "data_bits": 7, "stop_bits": 2, "parity": "even" },
            "slave_id": 3, "timeout_ms": 2000
        })).unwrap();
        update_settings(&state, "existing", serial_request)
            .await
            .unwrap();
        let connections = state.master_connections.read().await;
        let entry = &connections["existing"];
        assert_eq!(entry.connection.config.target_address, "/dev/test");
        let Transport::Rtu(serial) = &entry.connection.transport else {
            panic!("expected RTU")
        };
        assert_eq!(serial.baud_rate, 19200);
        assert_eq!(serial.parity, modbussim_core::transport::Parity::Even);
        assert_eq!(entry.scan_groups.len(), 1);
    }
}
