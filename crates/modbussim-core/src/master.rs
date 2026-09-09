use crate::ascii_master::AsciiMasterTransport;
use crate::log_collector::LogCollector;
use crate::log_entry::{Direction, FunctionCode, LogEntry};
use crate::reconnect::ReconnectPolicy;
use crate::request::{RequestPacer, RequestSettings};
use crate::rtu_master::RtuMasterTransport;
use crate::rtu_tcp_master::RtuTcpMasterTransport;
use crate::socks5::{connect_tcp, Socks5Config, TcpConnectError};
use crate::tls_master::TlsMasterConnection;
use crate::transport::{TlsConfig, Transport};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::{broadcast, mpsc, oneshot, Mutex};
use tokio_modbus::prelude::*;
use tokio_modbus::ExceptionCode;

/// Configuration for a master connection.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MasterConfig {
    pub target_address: String,
    pub port: u16,
    pub slave_id: u8,
    #[serde(default = "default_timeout_ms")]
    pub timeout_ms: u64,
    #[serde(default)]
    pub requests: RequestSettings,
    #[serde(default)]
    pub tls: TlsConfig,
    #[serde(default)]
    pub socks5: Socks5Config,
}

fn default_timeout_ms() -> u64 {
    3000
}

impl Default for MasterConfig {
    fn default() -> Self {
        Self {
            target_address: "127.0.0.1".to_string(),
            port: 502,
            slave_id: 1,
            timeout_ms: default_timeout_ms(),
            requests: RequestSettings::default(),
            tls: TlsConfig::default(),
            socks5: Socks5Config::default(),
        }
    }
}

/// Connection state of the master.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum MasterState {
    Disconnected,
    Connected,
    Reconnecting,
    Error,
}

/// Which read function code to use.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ReadFunction {
    /// FC01
    ReadCoils,
    /// FC02
    ReadDiscreteInputs,
    /// FC03
    ReadHoldingRegisters,
    /// FC04
    ReadInputRegisters,
}

/// Result of a read operation.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", content = "data")]
pub enum ReadResult {
    Coils(Vec<bool>),
    DiscreteInputs(Vec<bool>),
    HoldingRegisters(Vec<u16>),
    InputRegisters(Vec<u16>),
}

/// Events emitted by the polling task.
#[derive(Debug, Clone)]
pub enum PollEvent {
    Data(ReadResult),
    Error(String),
}

/// Configuration for a polling task.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PollConfig {
    pub function: ReadFunction,
    pub start_address: u16,
    pub quantity: u16,
    pub interval_ms: u64,
}

/// A named group of registers to scan periodically.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScanGroup {
    pub id: String,
    pub name: String,
    pub function: ReadFunction,
    pub start_address: u16,
    pub quantity: u16,
    pub interval_ms: u64,
    pub enabled: bool,
    /// Optional slave ID override. If None, uses the connection's default slave_id.
    #[serde(default)]
    pub slave_id: Option<u8>,
}

impl ScanGroup {
    pub fn validate(&self) -> Result<(), String> {
        validate_read_range(self.start_address, self.quantity)?;
        if self.interval_ms == 0 {
            return Err("poll interval must be greater than zero".into());
        }
        if self.slave_id.is_some_and(|id| !(1..=247).contains(&id)) {
            return Err("slave ID must be between 1 and 247".into());
        }
        Ok(())
    }
}

fn validate_read_range(start_address: u16, quantity: u16) -> Result<(), String> {
    if quantity == 0 || u32::from(start_address) + u32::from(quantity) > 65_536 {
        return Err("read range must contain 1..65535 addresses and end at or before 65535".into());
    }
    Ok(())
}

// A dropped sender has historically meant "no cancellation" for the public scanners.
// Never poll a oneshot again after try_recv reports Closed.
async fn cancellation_signal(receiver: &mut oneshot::Receiver<()>) {
    match receiver.try_recv() {
        Ok(()) => return,
        Err(oneshot::error::TryRecvError::Empty) => {
            if receiver.await.is_ok() {
                return;
            }
        }
        Err(oneshot::error::TryRecvError::Closed) => {}
    }
    std::future::pending::<()>().await;
}

/// Handle for a running poll task.
struct PollTaskHandle {
    shutdown_tx: oneshot::Sender<()>,
    join_handle: tokio::task::JoinHandle<()>,
}

/// Active transport context — holds the connected transport handle.
#[derive(Clone)]
enum TransportCtx {
    Tcp(Arc<Mutex<client::Context>>),
    Rtu(Arc<RtuMasterTransport>),
    Ascii(Arc<AsciiMasterTransport>),
    RtuTcp(Arc<RtuTcpMasterTransport>),
    TcpTls(Arc<TlsMasterConnection>),
}

/// A master connection that connects to a Modbus slave via any transport.
pub struct MasterConnection {
    pub config: MasterConfig,
    pub transport: Transport,
    pub reconnect_policy: ReconnectPolicy,
    state: MasterState,
    transport_ctx: Option<TransportCtx>,
    poll_tasks: HashMap<String, PollTaskHandle>,
    log_collector: Option<Arc<LogCollector>>,
    /// Broadcast channel for notifying upstream subscribers when the
    /// underlying transport is observed dead (e.g. after repeated read
    /// failures from poll tasks). Capacity is small because subscribers
    /// only need a wakeup, not the full history.
    connection_lost_tx: broadcast::Sender<()>,
    request_pacer: RequestPacer,
}

/// How many consecutive transport-level failures a poll task tolerates
/// before it gives up and signals connection loss.
const TRANSPORT_LOST_STREAK: u32 = 3;

impl MasterConnection {
    pub fn new(config: MasterConfig, transport: Transport) -> Self {
        let (connection_lost_tx, _) = broadcast::channel(4);
        let request_pacer = RequestPacer::new(Duration::from_millis(config.requests.interval_ms));
        Self {
            config,
            transport,
            reconnect_policy: ReconnectPolicy::default(),
            state: MasterState::Disconnected,
            transport_ctx: None,
            poll_tasks: HashMap::new(),
            log_collector: None,
            connection_lost_tx,
            request_pacer,
        }
    }

    /// Subscribe to connection-lost notifications. Each `()` published on
    /// the returned receiver means "poll tasks have observed the transport
    /// going dead". Multiple subscribers are supported; senders use
    /// best-effort delivery (errors when there are no receivers are ignored).
    pub fn subscribe_connection_lost(&self) -> broadcast::Receiver<()> {
        self.connection_lost_tx.subscribe()
    }

    /// Set the log collector for this connection.
    pub fn with_log_collector(mut self, collector: Arc<LogCollector>) -> Self {
        self.log_collector = Some(collector);
        self
    }

    pub fn state(&self) -> MasterState {
        self.state
    }

    /// Connect to the target Modbus slave using the configured transport.
    pub async fn connect(&mut self) -> Result<(), MasterError> {
        self.config
            .requests
            .validate()
            .map_err(MasterError::InvalidConfig)?;
        if self.state == MasterState::Connected {
            return Err(MasterError::AlreadyConnected);
        }

        let timeout = Duration::from_millis(self.config.timeout_ms);

        let ctx = match &self.transport {
            Transport::Tcp { host, port } => {
                let stream = connect_tcp(host, *port, &self.config.socks5, timeout)
                    .await
                    .map_err(map_tcp_connect_error)?;
                let tcp_ctx = tcp::attach_slave(stream, Slave(self.config.slave_id));
                TransportCtx::Tcp(Arc::new(Mutex::new(tcp_ctx)))
            }
            Transport::Rtu(serial_config) => {
                let rtu = RtuMasterTransport::connect(serial_config)
                    .await
                    .map_err(MasterError::ConnectionFailed)?;
                TransportCtx::Rtu(Arc::new(rtu))
            }
            Transport::Ascii(serial_config) => {
                let ascii = AsciiMasterTransport::connect(serial_config)
                    .await
                    .map_err(MasterError::ConnectionFailed)?;
                TransportCtx::Ascii(Arc::new(ascii))
            }
            Transport::RtuOverTcp { host, port } => {
                let rtu_tcp =
                    RtuTcpMasterTransport::connect(host, *port, &self.config.socks5, timeout)
                        .await
                        .map_err(MasterError::ConnectionFailed)?;
                TransportCtx::RtuTcp(Arc::new(rtu_tcp))
            }
            Transport::TcpTls { host, port } => {
                let tls_conn = crate::tls_master::connect_tls(
                    host,
                    *port,
                    &self.config.tls,
                    &self.config.socks5,
                    timeout,
                )
                .await?;
                TransportCtx::TcpTls(Arc::new(tls_conn))
            }
        };

        self.transport_ctx = Some(ctx);
        self.state = MasterState::Connected;
        Ok(())
    }

    /// Disconnect from the target. Idempotent: calling on an already
    /// disconnected connection is a no-op.
    pub async fn disconnect(&mut self) -> Result<(), MasterError> {
        // Always stop polling first so that no task keeps holding a
        // reference to the transport context.
        self.stop_all_scans().await;

        if let Some(TransportCtx::Tcp(tcp_ctx)) = self.transport_ctx.take() {
            let mut ctx = tcp_ctx.lock().await;
            let _ = ctx.disconnect().await;
        }
        // RTU/ASCII/RtuTcp: transport is dropped, which closes the port/stream.
        self.state = MasterState::Disconnected;
        Ok(())
    }

    /// Reconnect: disconnect then connect again.
    pub async fn reconnect(&mut self) -> Result<(), MasterError> {
        let _ = self.disconnect().await;
        self.connect().await
    }

    fn get_transport_ctx(&self) -> Result<TransportCtx, MasterError> {
        self.transport_ctx.clone().ok_or(MasterError::NotConnected)
    }

    fn get_tcp_ctx(&self) -> Result<Arc<Mutex<client::Context>>, MasterError> {
        match &self.transport_ctx {
            Some(TransportCtx::Tcp(ctx)) => Ok(ctx.clone()),
            Some(_) => Err(MasterError::Transport(
                "operation requires TCP transport".into(),
            )),
            None => Err(MasterError::NotConnected),
        }
    }

    /// Get a handle to the TCP context for external use (e.g., scanning).
    /// Returns an error for non-TCP transports.
    pub fn get_ctx_handle(&self) -> Result<Arc<Mutex<client::Context>>, MasterError> {
        self.get_tcp_ctx()
    }

    pub fn request_pacer(&self) -> RequestPacer {
        self.request_pacer.clone()
    }

    fn timeout_duration(&self) -> Duration {
        Duration::from_millis(self.config.timeout_ms)
    }

    fn to_function_code(function: ReadFunction) -> FunctionCode {
        match function {
            ReadFunction::ReadCoils => FunctionCode::ReadCoils,
            ReadFunction::ReadDiscreteInputs => FunctionCode::ReadDiscreteInputs,
            ReadFunction::ReadHoldingRegisters => FunctionCode::ReadHoldingRegisters,
            ReadFunction::ReadInputRegisters => FunctionCode::ReadInputRegisters,
        }
    }

    async fn log_tx(&self, fc: FunctionCode, detail: &str) {
        if let Some(collector) = &self.log_collector {
            let entry = LogEntry::new(Direction::Tx, fc, detail);
            collector.add(entry).await;
        }
    }

    /// Read registers using the specified function code.
    pub async fn read(
        &self,
        function: ReadFunction,
        start_address: u16,
        quantity: u16,
    ) -> Result<ReadResult, MasterError> {
        let transport_ctx = self.get_transport_ctx()?;
        execute_read_batched(
            &transport_ctx,
            &self.request_pacer,
            &self.config.requests,
            self.log_collector.as_ref(),
            self.config.slave_id,
            function,
            start_address,
            quantity,
            self.timeout_duration(),
            None,
        )
        .await
    }

    /// Write a single coil (FC05).
    pub async fn write_single_coil(&self, address: u16, value: bool) -> Result<(), MasterError> {
        let transport_ctx = self.get_transport_ctx()?;
        let timeout = self.timeout_duration();
        let _permit = self.request_pacer.acquire().await;
        self.log_tx(
            FunctionCode::WriteSingleCoil,
            &format!("W {} = {}", address, value),
        )
        .await;
        match &transport_ctx {
            TransportCtx::Tcp(ctx) => {
                let mut ctx = ctx.lock().await;
                ctx.set_slave(Slave(self.config.slave_id));
                tokio::time::timeout(timeout, ctx.write_single_coil(address, value))
                    .await
                    .map_err(|_| MasterError::Timeout("Write single coil timed out".into()))?
                    .map_err(|e| MasterError::Transport(format!("{e}")))?
                    .map_err(MasterError::Exception)?;
            }
            TransportCtx::TcpTls(tls) => {
                tls.write_single_coil(self.config.slave_id, address, value, timeout)
                    .await?;
            }
            other => {
                let coil_value: u16 = if value { 0xFF00 } else { 0x0000 };
                let mut pdu = vec![0x05];
                pdu.extend_from_slice(&address.to_be_bytes());
                pdu.extend_from_slice(&coil_value.to_be_bytes());
                let resp =
                    send_pdu_via_transport(other, self.config.slave_id, &pdu, timeout).await?;
                check_write_response(&resp, 0x05)?;
            }
        }
        Ok(())
    }

    /// Write a single holding register (FC06).
    pub async fn write_single_register(&self, address: u16, value: u16) -> Result<(), MasterError> {
        let transport_ctx = self.get_transport_ctx()?;
        let timeout = self.timeout_duration();
        let _permit = self.request_pacer.acquire().await;
        self.log_tx(
            FunctionCode::WriteSingleRegister,
            &format!("W {} = {:#06x}", address, value),
        )
        .await;
        match &transport_ctx {
            TransportCtx::Tcp(ctx) => {
                let mut ctx = ctx.lock().await;
                ctx.set_slave(Slave(self.config.slave_id));
                tokio::time::timeout(timeout, ctx.write_single_register(address, value))
                    .await
                    .map_err(|_| MasterError::Timeout("Write single register timed out".into()))?
                    .map_err(|e| MasterError::Transport(format!("{e}")))?
                    .map_err(MasterError::Exception)?;
            }
            TransportCtx::TcpTls(tls) => {
                tls.write_single_register(self.config.slave_id, address, value, timeout)
                    .await?;
            }
            other => {
                let mut pdu = vec![0x06];
                pdu.extend_from_slice(&address.to_be_bytes());
                pdu.extend_from_slice(&value.to_be_bytes());
                let resp =
                    send_pdu_via_transport(other, self.config.slave_id, &pdu, timeout).await?;
                check_write_response(&resp, 0x06)?;
            }
        }
        Ok(())
    }

    /// Write multiple coils (FC15).
    pub async fn write_multiple_coils(
        &self,
        address: u16,
        values: &[bool],
    ) -> Result<(), MasterError> {
        let transport_ctx = self.get_transport_ctx()?;
        let timeout = self.timeout_duration();
        let _permit = self.request_pacer.acquire().await;
        self.log_tx(
            FunctionCode::WriteMultipleCoils,
            &format!("W {} x{}", address, values.len()),
        )
        .await;
        match &transport_ctx {
            TransportCtx::Tcp(ctx) => {
                let mut ctx = ctx.lock().await;
                ctx.set_slave(Slave(self.config.slave_id));
                tokio::time::timeout(timeout, ctx.write_multiple_coils(address, values))
                    .await
                    .map_err(|_| MasterError::Timeout("Write multiple coils timed out".into()))?
                    .map_err(|e| MasterError::Transport(format!("{e}")))?
                    .map_err(MasterError::Exception)?;
            }
            TransportCtx::TcpTls(tls) => {
                tls.write_multiple_coils(self.config.slave_id, address, values, timeout)
                    .await?;
            }
            other => {
                let quantity = values.len() as u16;
                let byte_count = values.len().div_ceil(8);
                let mut coil_bytes = vec![0u8; byte_count];
                for (i, &v) in values.iter().enumerate() {
                    if v {
                        coil_bytes[i / 8] |= 1 << (i % 8);
                    }
                }
                let mut pdu = vec![0x0F];
                pdu.extend_from_slice(&address.to_be_bytes());
                pdu.extend_from_slice(&quantity.to_be_bytes());
                pdu.push(byte_count as u8);
                pdu.extend_from_slice(&coil_bytes);
                let resp =
                    send_pdu_via_transport(other, self.config.slave_id, &pdu, timeout).await?;
                check_write_response(&resp, 0x0F)?;
            }
        }
        Ok(())
    }

    /// Write multiple holding registers (FC16).
    pub async fn write_multiple_registers(
        &self,
        address: u16,
        values: &[u16],
    ) -> Result<(), MasterError> {
        let transport_ctx = self.get_transport_ctx()?;
        let timeout = self.timeout_duration();
        let _permit = self.request_pacer.acquire().await;
        self.log_tx(
            FunctionCode::WriteMultipleRegisters,
            &format!("W {} x{}", address, values.len()),
        )
        .await;
        match &transport_ctx {
            TransportCtx::Tcp(ctx) => {
                let mut ctx = ctx.lock().await;
                ctx.set_slave(Slave(self.config.slave_id));
                tokio::time::timeout(timeout, ctx.write_multiple_registers(address, values))
                    .await
                    .map_err(|_| MasterError::Timeout("Write multiple registers timed out".into()))?
                    .map_err(|e| MasterError::Transport(format!("{e}")))?
                    .map_err(MasterError::Exception)?;
            }
            TransportCtx::TcpTls(tls) => {
                tls.write_multiple_registers(self.config.slave_id, address, values, timeout)
                    .await?;
            }
            other => {
                let quantity = values.len() as u16;
                let byte_count = (values.len() * 2) as u8;
                let mut pdu = vec![0x10];
                pdu.extend_from_slice(&address.to_be_bytes());
                pdu.extend_from_slice(&quantity.to_be_bytes());
                pdu.push(byte_count);
                for v in values {
                    pdu.extend_from_slice(&v.to_be_bytes());
                }
                let resp =
                    send_pdu_via_transport(other, self.config.slave_id, &pdu, timeout).await?;
                check_write_response(&resp, 0x10)?;
            }
        }
        Ok(())
    }

    /// Start polling with the given configuration (legacy single-poll API).
    /// Returns a receiver for poll events.
    pub async fn start_poll(
        &mut self,
        poll_config: PollConfig,
    ) -> Result<mpsc::Receiver<PollEvent>, MasterError> {
        let group = ScanGroup {
            id: "__legacy_poll__".to_string(),
            name: "Legacy Poll".to_string(),
            function: poll_config.function,
            start_address: poll_config.start_address,
            quantity: poll_config.quantity,
            interval_ms: poll_config.interval_ms,
            enabled: true,
            slave_id: None,
        };
        self.start_scan_group(&group).await
    }

    /// Stop the legacy single polling task.
    pub async fn stop_poll(&mut self) -> Result<(), MasterError> {
        self.stop_scan_group("__legacy_poll__").await
    }

    /// Whether any polling is currently active.
    pub fn is_polling(&self) -> bool {
        !self.poll_tasks.is_empty()
    }

    /// Whether a specific scan group is actively polling.
    pub fn is_scan_active(&self, group_id: &str) -> bool {
        self.poll_tasks.contains_key(group_id)
    }

    /// Start polling for a scan group.
    /// Returns a receiver for poll events from this group.
    pub async fn start_scan_group(
        &mut self,
        group: &ScanGroup,
    ) -> Result<mpsc::Receiver<PollEvent>, MasterError> {
        group.validate().map_err(MasterError::InvalidConfig)?;
        self.config
            .requests
            .validate()
            .map_err(MasterError::InvalidConfig)?;
        // Stop existing poll for this group if any
        self.stop_scan_group(&group.id).await.ok();

        let transport_ctx = self.get_transport_ctx()?;
        let (shutdown_tx, mut shutdown_rx) = oneshot::channel::<()>();
        let (event_tx, event_rx) = mpsc::channel::<PollEvent>(100);
        let timeout = self.timeout_duration();
        let function = group.function;
        let start_address = group.start_address;
        let quantity = group.quantity;
        let interval_ms = group.interval_ms;
        let log_collector = self.log_collector.clone();
        let requests = self.config.requests.clone();
        let request_pacer = self.request_pacer.clone();
        let group_slave_id = group.slave_id;
        let default_slave_id = self.config.slave_id;
        let connection_lost_tx = self.connection_lost_tx.clone();

        let handle = tokio::spawn(async move {
            let interval = Duration::from_millis(interval_ms);
            let slave_id = group_slave_id.unwrap_or(default_slave_id);
            let mut transport_err_streak: u32 = 0;
            loop {
                // Check for shutdown
                if shutdown_rx.try_recv().is_ok() {
                    break;
                }

                let result = execute_read_batched(
                    &transport_ctx,
                    &request_pacer,
                    &requests,
                    log_collector.as_ref(),
                    slave_id,
                    function,
                    start_address,
                    quantity,
                    timeout,
                    Some(&mut shutdown_rx),
                )
                .await;
                if matches!(result, Err(MasterError::Cancelled)) {
                    break;
                }

                // Decide whether the transport looks dead. Modbus exceptions
                // (e.g. illegal address) mean the slave is alive but rejecting
                // the request, so they do NOT count toward the streak.
                let transport_died = match &result {
                    Ok(_) => {
                        transport_err_streak = 0;
                        false
                    }
                    Err(MasterError::Exception(_)) => false,
                    Err(_) => {
                        transport_err_streak += 1;
                        transport_err_streak >= TRANSPORT_LOST_STREAK
                    }
                };

                let event = match result {
                    Ok(data) => PollEvent::Data(data),
                    Err(e) => PollEvent::Error(format!("{e}")),
                };

                if event_tx.send(event).await.is_err() {
                    break; // Receiver dropped
                }

                if transport_died {
                    // Best-effort signal; if no one is listening, drop it.
                    let _ = connection_lost_tx.send(());
                    break;
                }

                tokio::select! {
                    _ = tokio::time::sleep(interval) => {}
                    _ = cancellation_signal(&mut shutdown_rx) => break,
                }
            }
        });

        self.poll_tasks.insert(
            group.id.clone(),
            PollTaskHandle {
                shutdown_tx,
                join_handle: handle,
            },
        );

        Ok(event_rx)
    }

    /// Stop a specific scan group's polling task.
    pub async fn stop_scan_group(&mut self, group_id: &str) -> Result<(), MasterError> {
        if let Some(handle) = self.poll_tasks.remove(group_id) {
            let _ = handle.shutdown_tx.send(());
            let _ = handle.join_handle.await;
        }
        Ok(())
    }

    /// Stop all active scan groups.
    pub async fn stop_all_scans(&mut self) {
        let tasks: Vec<(String, PollTaskHandle)> = self.poll_tasks.drain().collect();
        for (_, handle) in tasks {
            let _ = handle.shutdown_tx.send(());
            let _ = handle.join_handle.await;
        }
    }
}

fn map_tcp_connect_error(error: TcpConnectError) -> MasterError {
    match error {
        TcpConnectError::Timeout(message) => MasterError::Timeout(message),
        TcpConnectError::Connection(message) => MasterError::ConnectionFailed(message),
    }
}

// ---------------------------------------------------------------------------
// PDU helpers for non-TCP transports
// ---------------------------------------------------------------------------

pub(crate) fn build_read_pdu(function: ReadFunction, start_address: u16, quantity: u16) -> Vec<u8> {
    let fc: u8 = match function {
        ReadFunction::ReadCoils => 0x01,
        ReadFunction::ReadDiscreteInputs => 0x02,
        ReadFunction::ReadHoldingRegisters => 0x03,
        ReadFunction::ReadInputRegisters => 0x04,
    };
    let mut pdu = vec![fc];
    pdu.extend_from_slice(&start_address.to_be_bytes());
    pdu.extend_from_slice(&quantity.to_be_bytes());
    pdu
}

pub(crate) fn parse_read_response_pdu(
    function: ReadFunction,
    response_pdu: &[u8],
) -> Result<ReadResult, MasterError> {
    if response_pdu.is_empty() {
        return Err(MasterError::Transport("empty response".into()));
    }
    // Check for exception response (high bit set on function code)
    if response_pdu[0] & 0x80 != 0 {
        let exc = response_pdu.get(1).copied().unwrap_or(0);
        return Err(MasterError::Transport(format!(
            "Modbus exception: 0x{:02X}",
            exc
        )));
    }
    let byte_count = response_pdu.get(1).copied().unwrap_or(0) as usize;
    let data = if response_pdu.len() > 2 {
        &response_pdu[2..]
    } else {
        &[]
    };

    match function {
        ReadFunction::ReadCoils | ReadFunction::ReadDiscreteInputs => {
            let mut bits = Vec::new();
            for byte_idx in 0..byte_count {
                for bit_idx in 0..8 {
                    if byte_idx < data.len() {
                        bits.push((data[byte_idx] >> bit_idx) & 1 == 1);
                    }
                }
            }
            match function {
                ReadFunction::ReadCoils => Ok(ReadResult::Coils(bits)),
                _ => Ok(ReadResult::DiscreteInputs(bits)),
            }
        }
        ReadFunction::ReadHoldingRegisters | ReadFunction::ReadInputRegisters => {
            let mut regs = Vec::new();
            for chunk in data.chunks(2) {
                if chunk.len() == 2 {
                    regs.push(u16::from_be_bytes([chunk[0], chunk[1]]));
                }
            }
            match function {
                ReadFunction::ReadHoldingRegisters => Ok(ReadResult::HoldingRegisters(regs)),
                _ => Ok(ReadResult::InputRegisters(regs)),
            }
        }
    }
}

/// Send a raw PDU via a non-TCP transport and return the response PDU.
async fn send_pdu_via_transport(
    ctx: &TransportCtx,
    slave_id: u8,
    pdu: &[u8],
    timeout: Duration,
) -> Result<Vec<u8>, MasterError> {
    match ctx {
        TransportCtx::Rtu(t) => t
            .request(slave_id, pdu, timeout)
            .await
            .map_err(MasterError::Transport),
        TransportCtx::Ascii(t) => t
            .request(slave_id, pdu, timeout)
            .await
            .map_err(MasterError::Transport),
        TransportCtx::RtuTcp(t) => t
            .request(slave_id, pdu, timeout)
            .await
            .map_err(MasterError::Transport),
        TransportCtx::Tcp(_) => Err(MasterError::Transport(
            "send_pdu_via_transport called for TCP".into(),
        )),
        TransportCtx::TcpTls(_) => Err(MasterError::Transport(
            "send_pdu_via_transport called for TLS".into(),
        )),
    }
}

/// Check a write response PDU for exception.
pub(crate) fn check_write_response(resp: &[u8], expected_fc: u8) -> Result<(), MasterError> {
    if resp.is_empty() {
        return Err(MasterError::Transport("empty response".into()));
    }
    if resp[0] & 0x80 != 0 {
        let exc = resp.get(1).copied().unwrap_or(0);
        return Err(MasterError::Transport(format!(
            "Modbus exception: 0x{:02X}",
            exc
        )));
    }
    if resp[0] != expected_fc {
        return Err(MasterError::Transport(format!(
            "unexpected function code in response: expected 0x{:02X}, got 0x{:02X}",
            expected_fc, resp[0]
        )));
    }
    Ok(())
}

/// Read a continuous range as bounded requests, returning one complete result in address order.
#[allow(clippy::too_many_arguments)]
async fn execute_read_batched(
    ctx: &TransportCtx,
    pacer: &RequestPacer,
    settings: &RequestSettings,
    collector: Option<&Arc<LogCollector>>,
    slave_id: u8,
    function: ReadFunction,
    start_address: u16,
    quantity: u16,
    timeout: Duration,
    mut cancel: Option<&mut oneshot::Receiver<()>>,
) -> Result<ReadResult, MasterError> {
    settings.validate().map_err(MasterError::InvalidConfig)?;
    validate_read_range(start_address, quantity).map_err(MasterError::InvalidConfig)?;
    let (limit, mut combined) = match function {
        ReadFunction::ReadCoils => (settings.max_read_bits, ReadResult::Coils(Vec::new())),
        ReadFunction::ReadDiscreteInputs => (
            settings.max_read_bits,
            ReadResult::DiscreteInputs(Vec::new()),
        ),
        ReadFunction::ReadHoldingRegisters => (
            settings.max_read_registers,
            ReadResult::HoldingRegisters(Vec::new()),
        ),
        ReadFunction::ReadInputRegisters => (
            settings.max_read_registers,
            ReadResult::InputRegisters(Vec::new()),
        ),
    };
    let fc = MasterConnection::to_function_code(function);
    let mut offset = 0u32;
    while offset < u32::from(quantity) {
        let count = limit.min((u32::from(quantity) - offset) as u16);
        let address = (u32::from(start_address) + offset) as u16;
        let mut part = {
            // Cancellation can interrupt queue/gap waits, but an on-wire request is drained.
            let _permit = if let Some(cancel) = cancel.as_mut() {
                tokio::select! {
                    biased;
                    _ = cancellation_signal(cancel) => return Err(MasterError::Cancelled),
                    permit = pacer.acquire() => permit,
                }
            } else {
                pacer.acquire().await
            };
            if let Some(collector) = collector {
                collector
                    .add(LogEntry::new(
                        Direction::Tx,
                        fc,
                        format!("R {address} x{count}"),
                    ))
                    .await;
            }
            let result = execute_read_any(ctx, slave_id, function, address, count, timeout).await;
            if let Some(collector) = collector {
                let detail = match &result {
                    Ok(data) => format!("R {address} x{count}: {data:?}"),
                    Err(error) => format!("R {address} x{count}: ERR: {error}"),
                };
                collector
                    .add(LogEntry::new(Direction::Rx, fc, detail))
                    .await;
            }
            result?
        };
        match (&mut combined, &mut part) {
            (ReadResult::Coils(all), ReadResult::Coils(values))
            | (ReadResult::DiscreteInputs(all), ReadResult::DiscreteInputs(values)) => {
                if values.len() < usize::from(count) {
                    return Err(MasterError::Transport("short bit response".into()));
                }
                // Serial/TLS responses may include byte-padding bits. Discard them per packet.
                values.truncate(usize::from(count));
                all.append(values);
            }
            (ReadResult::HoldingRegisters(all), ReadResult::HoldingRegisters(values))
            | (ReadResult::InputRegisters(all), ReadResult::InputRegisters(values)) => {
                if values.len() != usize::from(count) {
                    return Err(MasterError::Transport(
                        "register response length mismatch".into(),
                    ));
                }
                all.append(values);
            }
            _ => return Err(MasterError::Transport("read response type mismatch".into())),
        }
        offset += u32::from(count);
    }
    Ok(combined)
}

/// Execute a read via TCP tokio_modbus context.
async fn execute_read_tcp(
    ctx: &Arc<Mutex<client::Context>>,
    slave_id: u8,
    function: ReadFunction,
    start_address: u16,
    quantity: u16,
    timeout: Duration,
) -> Result<ReadResult, MasterError> {
    let mut ctx = ctx.lock().await;
    ctx.set_slave(Slave(slave_id));
    match function {
        ReadFunction::ReadCoils => {
            let data = tokio::time::timeout(timeout, ctx.read_coils(start_address, quantity))
                .await
                .map_err(|_| MasterError::Timeout("Read timed out".into()))?
                .map_err(|e| MasterError::Transport(format!("{e}")))?
                .map_err(MasterError::Exception)?;
            Ok(ReadResult::Coils(data))
        }
        ReadFunction::ReadDiscreteInputs => {
            let data =
                tokio::time::timeout(timeout, ctx.read_discrete_inputs(start_address, quantity))
                    .await
                    .map_err(|_| MasterError::Timeout("Read timed out".into()))?
                    .map_err(|e| MasterError::Transport(format!("{e}")))?
                    .map_err(MasterError::Exception)?;
            Ok(ReadResult::DiscreteInputs(data))
        }
        ReadFunction::ReadHoldingRegisters => {
            let data =
                tokio::time::timeout(timeout, ctx.read_holding_registers(start_address, quantity))
                    .await
                    .map_err(|_| MasterError::Timeout("Read timed out".into()))?
                    .map_err(|e| MasterError::Transport(format!("{e}")))?
                    .map_err(MasterError::Exception)?;
            Ok(ReadResult::HoldingRegisters(data))
        }
        ReadFunction::ReadInputRegisters => {
            let data =
                tokio::time::timeout(timeout, ctx.read_input_registers(start_address, quantity))
                    .await
                    .map_err(|_| MasterError::Timeout("Read timed out".into()))?
                    .map_err(|e| MasterError::Transport(format!("{e}")))?
                    .map_err(MasterError::Exception)?;
            Ok(ReadResult::InputRegisters(data))
        }
    }
}

/// Execute a read operation on any transport type.
async fn execute_read_any(
    ctx: &TransportCtx,
    slave_id: u8,
    function: ReadFunction,
    start_address: u16,
    quantity: u16,
    timeout: Duration,
) -> Result<ReadResult, MasterError> {
    match ctx {
        TransportCtx::Tcp(tcp_ctx) => {
            execute_read_tcp(
                tcp_ctx,
                slave_id,
                function,
                start_address,
                quantity,
                timeout,
            )
            .await
        }
        TransportCtx::TcpTls(tls) => {
            tls.read(slave_id, function, start_address, quantity, timeout)
                .await
        }
        other => {
            let pdu = build_read_pdu(function, start_address, quantity);
            let resp = send_pdu_via_transport(other, slave_id, &pdu, timeout).await?;
            parse_read_response_pdu(function, &resp)
        }
    }
}

/// Format an ExceptionCode into a human-readable description.
pub fn exception_description(code: ExceptionCode) -> &'static str {
    match code {
        ExceptionCode::IllegalFunction => "Illegal Function",
        ExceptionCode::IllegalDataAddress => "Illegal Data Address",
        ExceptionCode::IllegalDataValue => "Illegal Data Value",
        ExceptionCode::ServerDeviceFailure => "Server Device Failure",
        ExceptionCode::Acknowledge => "Acknowledge",
        ExceptionCode::ServerDeviceBusy => "Server Device Busy",
        ExceptionCode::MemoryParityError => "Memory Parity Error",
        ExceptionCode::GatewayPathUnavailable => "Gateway Path Unavailable",
        ExceptionCode::GatewayTargetDevice => "Gateway Target Device Failed to Respond",
        _ => "Unknown Exception",
    }
}

#[derive(Debug, thiserror::Error)]
pub enum MasterError {
    #[error("operation cancelled")]
    Cancelled,
    #[error("invalid configuration: {0}")]
    InvalidConfig(String),
    #[error("already connected")]
    AlreadyConnected,
    #[error("not connected")]
    NotConnected,
    #[error("connection failed: {0}")]
    ConnectionFailed(String),
    #[error("timeout: {0}")]
    Timeout(String),
    #[error("transport error: {0}")]
    Transport(String),
    #[error("modbus exception: {0:?}")]
    Exception(ExceptionCode),
}

// ---------------------------------------------------------------------------
// Scanning
// ---------------------------------------------------------------------------

/// Progress report for slave ID scanning.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SlaveIdScanProgress {
    pub current_id: u8,
    pub total: u16,
    pub found_ids: Vec<u8>,
    pub done: bool,
    pub cancelled: bool,
}

/// A register found during address scanning.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FoundRegister {
    pub address: u16,
    pub value: u16,
}

/// Progress report for register address scanning.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegisterScanProgress {
    pub current_address: u16,
    pub end_address: u16,
    pub found_registers: Vec<FoundRegister>,
    pub done: bool,
    pub cancelled: bool,
}

/// Scan slave IDs 1-247 using an existing TCP context.
/// Changes slave ID via `set_slave()`, restores original when done.
pub async fn scan_slave_ids_with_ctx(
    ctx: Arc<Mutex<client::Context>>,
    original_slave_id: u8,
    start_id: u8,
    end_id: u8,
    scan_timeout: Duration,
    cancel_rx: oneshot::Receiver<()>,
    progress_tx: mpsc::Sender<SlaveIdScanProgress>,
) -> Vec<u8> {
    scan_slave_ids_paced(
        ctx,
        original_slave_id,
        start_id,
        end_id,
        scan_timeout,
        RequestPacer::new(Duration::ZERO),
        cancel_rx,
        progress_tx,
    )
    .await
}

#[allow(clippy::too_many_arguments)]
pub async fn scan_slave_ids_paced(
    ctx: Arc<Mutex<client::Context>>,
    original_slave_id: u8,
    start_id: u8,
    end_id: u8,
    scan_timeout: Duration,
    pacer: RequestPacer,
    mut cancel_rx: oneshot::Receiver<()>,
    progress_tx: mpsc::Sender<SlaveIdScanProgress>,
) -> Vec<u8> {
    let mut found_ids: Vec<u8> = Vec::new();
    let mut cancelled = false;
    let total = end_id.saturating_sub(start_id) as u16 + 1;

    for id in start_id..=end_id {
        // Check cancellation
        if cancel_rx.try_recv().is_ok() {
            cancelled = true;
            break;
        }

        // Probe this slave ID
        let found = {
            let _permit = tokio::select! {
                biased;
                _ = cancellation_signal(&mut cancel_rx) => { cancelled = true; break; },
                permit = pacer.acquire() => permit,
            };
            let mut ctx = ctx.lock().await;
            ctx.set_slave(Slave(id));
            matches!(
                tokio::time::timeout(scan_timeout, ctx.read_holding_registers(0, 1)).await,
                Ok(Ok(Ok(_)))
            )
        };

        if found {
            found_ids.push(id);
        }

        let _ = progress_tx
            .send(SlaveIdScanProgress {
                current_id: id,
                total,
                found_ids: found_ids.clone(),
                done: id == end_id,
                cancelled: false,
            })
            .await;
    }

    // Restore original slave ID
    {
        let mut ctx = ctx.lock().await;
        ctx.set_slave(Slave(original_slave_id));
    }

    // Send final done
    let _ = progress_tx
        .send(SlaveIdScanProgress {
            current_id: end_id,
            total,
            found_ids: found_ids.clone(),
            done: true,
            cancelled,
        })
        .await;

    found_ids
}

/// Scan a register address range using an existing TCP context.
#[allow(clippy::too_many_arguments)]
pub async fn scan_registers_with_ctx(
    ctx: Arc<Mutex<client::Context>>,
    function: ReadFunction,
    start_address: u16,
    end_address: u16,
    chunk_size: u16,
    scan_timeout: Duration,
    cancel_rx: oneshot::Receiver<()>,
    progress_tx: mpsc::Sender<RegisterScanProgress>,
) -> Vec<FoundRegister> {
    scan_registers_paced(
        ctx,
        None,
        function,
        start_address,
        end_address,
        chunk_size,
        scan_timeout,
        RequestPacer::new(Duration::ZERO),
        cancel_rx,
        progress_tx,
    )
    .await
}

#[allow(clippy::too_many_arguments)]
pub async fn scan_registers_paced(
    ctx: Arc<Mutex<client::Context>>,
    slave_id: Option<u8>,
    function: ReadFunction,
    start_address: u16,
    end_address: u16,
    chunk_size: u16,
    scan_timeout: Duration,
    pacer: RequestPacer,
    mut cancel_rx: oneshot::Receiver<()>,
    progress_tx: mpsc::Sender<RegisterScanProgress>,
) -> Vec<FoundRegister> {
    let mut found: Vec<FoundRegister> = Vec::new();
    let mut cancelled = false;
    let mut addr = u32::from(start_address);
    let max_chunk = match function {
        ReadFunction::ReadCoils | ReadFunction::ReadDiscreteInputs => 2000,
        _ => 125,
    };
    let chunk_size = chunk_size.clamp(1, max_chunk);

    while addr <= u32::from(end_address) {
        // Check cancellation
        if cancel_rx.try_recv().is_ok() {
            cancelled = true;
            break;
        }

        let qty = u32::from(chunk_size).min(u32::from(end_address) - addr + 1) as u16;

        let result = {
            let _permit = tokio::select! {
                biased;
                _ = cancellation_signal(&mut cancel_rx) => { cancelled = true; break; },
                permit = pacer.acquire() => permit,
            };
            let mut ctx = ctx.lock().await;
            if let Some(id) = slave_id {
                ctx.set_slave(Slave(id));
            }
            let read_fut = match function {
                ReadFunction::ReadCoils => {
                    let f = ctx.read_coils(addr as u16, qty);
                    tokio::time::timeout(scan_timeout, f)
                        .await
                        .ok()
                        .and_then(|r| r.ok())
                        .and_then(|r| r.ok())
                        .map(|vals| {
                            vals.iter()
                                .map(|&b| if b { 1u16 } else { 0u16 })
                                .collect::<Vec<_>>()
                        })
                }
                ReadFunction::ReadDiscreteInputs => {
                    let f = ctx.read_discrete_inputs(addr as u16, qty);
                    tokio::time::timeout(scan_timeout, f)
                        .await
                        .ok()
                        .and_then(|r| r.ok())
                        .and_then(|r| r.ok())
                        .map(|vals| {
                            vals.iter()
                                .map(|&b| if b { 1u16 } else { 0u16 })
                                .collect::<Vec<_>>()
                        })
                }
                ReadFunction::ReadHoldingRegisters => {
                    let f = ctx.read_holding_registers(addr as u16, qty);
                    tokio::time::timeout(scan_timeout, f)
                        .await
                        .ok()
                        .and_then(|r| r.ok())
                        .and_then(|r| r.ok())
                }
                ReadFunction::ReadInputRegisters => {
                    let f = ctx.read_input_registers(addr as u16, qty);
                    tokio::time::timeout(scan_timeout, f)
                        .await
                        .ok()
                        .and_then(|r| r.ok())
                        .and_then(|r| r.ok())
                }
            };
            read_fut
        };

        if let Some(values) = result {
            for (i, &val) in values.iter().take(usize::from(qty)).enumerate() {
                found.push(FoundRegister {
                    address: (addr + i as u32) as u16,
                    value: val,
                });
            }
        }

        let done = addr + u32::from(qty) > u32::from(end_address);
        let _ = progress_tx
            .send(RegisterScanProgress {
                current_address: (addr + u32::from(qty) - 1) as u16,
                end_address,
                found_registers: found.clone(),
                done,
                cancelled: false,
            })
            .await;

        addr += u32::from(qty);
        if addr == 0 && end_address == u16::MAX {
            break; // overflow protection
        }
    }

    if cancelled {
        let _ = progress_tx
            .send(RegisterScanProgress {
                current_address: addr.min(u32::from(end_address)) as u16,
                end_address,
                found_registers: found.clone(),
                done: false,
                cancelled: true,
            })
            .await;
    }
    found
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_master_config_default() {
        let config = MasterConfig::default();
        assert_eq!(config.target_address, "127.0.0.1");
        assert_eq!(config.port, 502);
        assert_eq!(config.slave_id, 1);
        assert_eq!(config.timeout_ms, 3000);
        assert!(!config.socks5.enabled);
    }

    #[test]
    fn test_master_connection_initial_state() {
        let config = MasterConfig::default();
        let transport = Transport::Tcp {
            host: config.target_address.clone(),
            port: config.port,
        };
        let conn = MasterConnection::new(config, transport);
        assert_eq!(conn.state(), MasterState::Disconnected);
        assert!(!conn.is_polling());
    }

    #[test]
    fn test_exception_description() {
        assert_eq!(
            exception_description(ExceptionCode::IllegalFunction),
            "Illegal Function"
        );
        assert_eq!(
            exception_description(ExceptionCode::IllegalDataAddress),
            "Illegal Data Address"
        );
    }

    #[test]
    fn test_master_error_display() {
        let err = MasterError::Exception(ExceptionCode::IllegalDataAddress);
        let msg = err.to_string();
        assert!(msg.contains("IllegalDataAddress") || msg.contains("Illegal"));
    }

    #[tokio::test]
    async fn test_disconnect_idempotent() {
        let config = MasterConfig::default();
        let transport = Transport::Tcp {
            host: "127.0.0.1".into(),
            port: 502,
        };
        let mut conn = MasterConnection::new(config, transport);
        // Never connected: disconnect should still succeed.
        assert!(conn.disconnect().await.is_ok());
        // Calling again must remain OK (idempotent).
        assert!(conn.disconnect().await.is_ok());
        assert_eq!(conn.state(), MasterState::Disconnected);
    }

    #[test]
    fn test_subscribe_connection_lost() {
        let config = MasterConfig::default();
        let transport = Transport::Tcp {
            host: "127.0.0.1".into(),
            port: 502,
        };
        let conn = MasterConnection::new(config, transport);
        // Subscribing twice yields independent receivers.
        let _rx1 = conn.subscribe_connection_lost();
        let _rx2 = conn.subscribe_connection_lost();
    }

    #[test]
    fn test_scan_group_serde() {
        let group = ScanGroup {
            id: "sg1".to_string(),
            name: "Test Group".to_string(),
            function: ReadFunction::ReadHoldingRegisters,
            start_address: 0,
            quantity: 10,
            interval_ms: 1000,
            enabled: true,
            slave_id: None,
        };
        let json = serde_json::to_string(&group).unwrap();
        let parsed: ScanGroup = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed.id, "sg1");
        assert_eq!(parsed.quantity, 10);
    }
}
