use crate::state::AppState;
use modbussim_core::{slave::ConnectionState, transport::Transport};
use tauri::State;

#[tauri::command]
pub async fn get_slave_transport(
    state: State<'_, AppState>,
    id: String,
) -> Result<Transport, String> {
    let connections = state.slave_connections.read().await;
    Ok(connections
        .get(&id)
        .ok_or("Connection not found")?
        .connection
        .transport
        .clone())
}

pub fn validate_transport(transport: &Transport) -> Result<(), String> {
    match transport {
        Transport::Tcp { host, port }
        | Transport::TcpTls { host, port }
        | Transport::RtuOverTcp { host, port } => {
            host.parse::<std::net::IpAddr>()
                .map_err(|_| "Bind address must be an IP address")?;
            if *port == 0 {
                return Err("Port must be between 1 and 65535".into());
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
    Ok(())
}

#[tauri::command]
pub async fn update_slave_transport(
    state: State<'_, AppState>,
    id: String,
    transport: Transport,
) -> Result<(), String> {
    validate_transport(&transport)?;
    let mut connections = state.slave_connections.write().await;
    let conn = connections.get_mut(&id).ok_or("Connection not found")?;
    if conn.connection.state() != ConnectionState::Stopped {
        return Err("Stop the connection before changing its settings".into());
    }
    if std::mem::discriminant(&conn.connection.transport) != std::mem::discriminant(&transport) {
        return Err("Changing transport type requires a new connection".into());
    }
    conn.connection.transport = transport;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn validates_bind_address_and_port() {
        assert!(validate_transport(&Transport::Tcp {
            host: "127.0.0.1".into(),
            port: 15020
        })
        .is_ok());
        assert!(validate_transport(&Transport::Tcp {
            host: "::1".into(),
            port: 15020
        })
        .is_ok());
        assert!(validate_transport(&Transport::Tcp {
            host: "not-an-ip".into(),
            port: 15020
        })
        .is_err());
        assert!(validate_transport(&Transport::Tcp {
            host: "127.0.0.1".into(),
            port: 0
        })
        .is_err());
    }
}
