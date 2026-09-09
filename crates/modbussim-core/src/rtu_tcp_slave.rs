//! RTU-over-TCP slave server.
//!
//! Standard TCP listener where frames use RTU format (slave_id + PDU + CRC,
//! no MBAP header). Each client connection is handled in a separate task.

use crate::clients::ConnectedClients;
use crate::frame;
use crate::log_entry::{Direction, FunctionCode};
use crate::pdu::parse_request_pdu;
use crate::rtu_slave::{format_request, log_if_enabled, process_request};
use crate::slave::{SharedChangeCallback, SharedDevices, SharedLogCollector};

use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;
use tokio::sync::oneshot;

/// Run an RTU-over-TCP slave server on `host:port`.
///
/// Accepts client connections and spawns a task per client. Each task reads RTU
/// frames from the TCP stream, processes requests, and writes RTU responses.
/// A 60-second idle timeout disconnects the client.
pub async fn run_rtu_tcp_slave(
    host: String,
    port: u16,
    devices: SharedDevices,
    log_collector: SharedLogCollector,
    change_callback: SharedChangeCallback,
    clients: ConnectedClients,
    shutdown_rx: oneshot::Receiver<()>,
) -> Result<(), String> {
    let addr = std::net::SocketAddr::new(
        host.parse().map_err(|e| format!("Invalid address: {e}"))?,
        port,
    );
    let listener = TcpListener::bind(&addr)
        .await
        .map_err(|e| format!("Failed to bind {addr}: {e}"))?;
    log::info!("RTU-over-TCP slave listening on {addr}");

    tokio::pin!(shutdown_rx);

    loop {
        tokio::select! {
            _ = &mut shutdown_rx => {
                log::info!("RTU-over-TCP slave shutting down");
                return Ok(());
            }
            accept_result = listener.accept() => {
                match accept_result {
                    Ok((stream, peer)) => {
                        log::info!("RTU-over-TCP client connected: {peer}");
                        let devices = devices.clone();
                        let log_collector = log_collector.clone();
                        let change_callback = change_callback.clone();
                        let std_stream = stream.into_std().map_err(|e| e.to_string())?;
                        let guard = clients.track(&std_stream).map_err(|e| e.to_string())?;
                        let stream = tokio::net::TcpStream::from_std(std_stream).map_err(|e| e.to_string())?;
                        tokio::spawn(async move {
                            let _guard = guard;
                            if let Err(e) = handle_client(stream, devices, log_collector, change_callback).await {
                                log::warn!("RTU-over-TCP client {peer} error: {e}");
                            }
                            log::info!("RTU-over-TCP client {peer} disconnected");
                        });
                    }
                    Err(e) => {
                        log::error!("RTU-over-TCP accept error: {e}");
                    }
                }
            }
        }
    }
}

/// Handle a single RTU-over-TCP client connection.
///
/// Reads RTU frames from the stream, processes them, and writes responses.
/// Disconnects after 60 seconds of idle.
async fn handle_client(
    mut stream: tokio::net::TcpStream,
    devices: SharedDevices,
    log_collector: SharedLogCollector,
    change_callback: SharedChangeCallback,
) -> Result<(), String> {
    let idle_timeout = Duration::from_secs(60);
    let mut buf = vec![0u8; 256];
    let mut frame_buf: Vec<u8> = Vec::with_capacity(256);

    loop {
        // Read with idle timeout.
        let n = match tokio::time::timeout(idle_timeout, stream.read(&mut buf)).await {
            Ok(Ok(0)) => return Ok(()), // Client closed connection.
            Ok(Ok(n)) => n,
            Ok(Err(e)) => return Err(format!("TCP read error: {e}")),
            Err(_) => return Ok(()), // 60s idle timeout — disconnect.
        };

        frame_buf.extend_from_slice(&buf[..n]);

        // Try to decode complete RTU frames from the buffer.
        // An RTU frame needs at least 4 bytes (slave_id + FC + CRC_lo + CRC_hi).
        while frame_buf.len() >= 4 {
            // Attempt to decode the entire buffer as one RTU frame.
            // RTU-over-TCP typically sends one complete frame per TCP segment,
            // but we handle partial reads by trying to decode what we have.
            match frame::decode_rtu(&frame_buf) {
                Ok(rtu_frame) => {
                    let slave_id = rtu_frame.slave_id;
                    let request_pdu = &rtu_frame.pdu;

                    // Log inbound frame.
                    if let Some(fc_val) = request_pdu.first() {
                        if let Some(fc) = FunctionCode::from_u8(*fc_val) {
                            if let Ok(req) = parse_request_pdu(request_pdu) {
                                log_if_enabled(
                                    &log_collector,
                                    Direction::Rx,
                                    fc,
                                    &format_request(&req),
                                );
                            }
                        }
                    }

                    // Process the request.
                    if let Some(response_pdu) =
                        process_request(slave_id, request_pdu, &devices, &change_callback).await
                    {
                        // Log outbound response.
                        if let Some(fc_val) = request_pdu.first() {
                            if let Some(fc) = FunctionCode::from_u8(*fc_val) {
                                let detail = if response_pdu.first().is_some_and(|b| b & 0x80 != 0)
                                {
                                    format!(
                                        "ERR: exception 0x{:02X}",
                                        response_pdu.get(1).copied().unwrap_or(0)
                                    )
                                } else {
                                    "OK".to_string()
                                };
                                log_if_enabled(&log_collector, Direction::Tx, fc, &detail);
                            }
                        }

                        let response_frame = frame::encode_rtu(slave_id, &response_pdu);
                        if let Err(e) = stream.write_all(&response_frame).await {
                            return Err(format!("TCP write error: {e}"));
                        }
                    }

                    frame_buf.clear();
                }
                Err(_) => {
                    // Not a valid frame yet — might need more data, or it's garbage.
                    // If buffer is getting large, it's likely corrupt; discard.
                    if frame_buf.len() > 256 {
                        log::warn!("RTU-over-TCP frame buffer overflow, discarding");
                        frame_buf.clear();
                    }
                    break;
                }
            }
        }
    }
}
