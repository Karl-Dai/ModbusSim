use crate::clients::{ClientGuard, ConnectedClients};
use crate::log_collector::LogCollector;
use crate::log_entry::{Direction, FunctionCode, LogEntry};
use crate::register::{RegisterDef, RegisterMap, RegisterType};
use crate::transport::{SlaveTlsConfig, Transport};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::future::Future;
use std::net::SocketAddr;
use std::pin::Pin;
use std::sync::Arc;
use tokio::net::TcpListener;
use tokio::sync::{oneshot, RwLock};
use tokio_modbus::server::tcp::Server;
use tokio_modbus::server::Service;
use tokio_modbus::{ExceptionCode, Request, Response, SlaveRequest};

/// A single Modbus slave device with its own register map and definitions.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SlaveDevice {
    pub slave_id: u8,
    pub name: String,
    pub register_map: RegisterMap,
    pub register_defs: Vec<RegisterDef>,
}

impl SlaveDevice {
    pub fn new(slave_id: u8, name: impl Into<String>) -> Self {
        Self {
            slave_id,
            name: name.into(),
            register_map: RegisterMap::new(),
            register_defs: Vec::new(),
        }
    }

    /// Create a device with default registers pre-filled.
    /// Adds FC1/FC2/FC3/FC4 registers for addresses 0..=max_address,
    /// and initializes the corresponding RegisterMap values.
    pub fn with_default_registers(slave_id: u8, name: impl Into<String>, max_address: u16) -> Self {
        use crate::register::{DataType, Endian, RegisterDef, RegisterType};

        let mut device = Self::new(slave_id, name);
        let mut defs = Vec::with_capacity((max_address as usize + 1) * 4);

        for addr in 0..=max_address {
            // FC1 Coil
            defs.push(RegisterDef {
                address: addr,
                register_type: RegisterType::Coil,
                data_type: DataType::Bool,
                endian: Endian::Big,
                name: String::new(),
                comment: String::new(),
                mutation: None,
                data_source: None,
            });
            device.register_map.write_coil(addr, false);

            // FC2 Discrete Input
            defs.push(RegisterDef {
                address: addr,
                register_type: RegisterType::DiscreteInput,
                data_type: DataType::Bool,
                endian: Endian::Big,
                name: String::new(),
                comment: String::new(),
                mutation: None,
                data_source: None,
            });
            device.register_map.discrete_inputs.insert(addr, false);

            // FC3 Holding Register
            defs.push(RegisterDef {
                address: addr,
                register_type: RegisterType::HoldingRegister,
                data_type: DataType::UInt16,
                endian: Endian::Big,
                name: String::new(),
                comment: String::new(),
                mutation: None,
                data_source: None,
            });
            device.register_map.write_holding_register(addr, 0);

            // FC4 Input Register
            defs.push(RegisterDef {
                address: addr,
                register_type: RegisterType::InputRegister,
                data_type: DataType::UInt16,
                endian: Endian::Big,
                name: String::new(),
                comment: String::new(),
                mutation: None,
                data_source: None,
            });
            device.register_map.input_registers.insert(addr, 0);
        }

        device.register_defs = defs;
        device
    }

    /// Create a device with random register values pre-filled.
    /// Same structure as `with_default_registers` but values are randomized:
    /// - Coil/DiscreteInput: random bool
    /// - HoldingRegister/InputRegister: random u16
    pub fn with_random_registers(slave_id: u8, name: impl Into<String>, max_address: u16) -> Self {
        use crate::register::{DataType, Endian, RegisterDef, RegisterType};
        use rand::Rng;

        let mut device = Self::new(slave_id, name);
        let mut defs = Vec::with_capacity((max_address as usize + 1) * 4);
        let mut rng = rand::thread_rng();

        for addr in 0..=max_address {
            // FC1 Coil
            defs.push(RegisterDef {
                address: addr,
                register_type: RegisterType::Coil,
                data_type: DataType::Bool,
                endian: Endian::Big,
                name: String::new(),
                comment: String::new(),
                mutation: None,
                data_source: None,
            });
            device.register_map.write_coil(addr, rng.gen::<bool>());

            // FC2 Discrete Input
            defs.push(RegisterDef {
                address: addr,
                register_type: RegisterType::DiscreteInput,
                data_type: DataType::Bool,
                endian: Endian::Big,
                name: String::new(),
                comment: String::new(),
                mutation: None,
                data_source: None,
            });
            device
                .register_map
                .discrete_inputs
                .insert(addr, rng.gen::<bool>());

            // FC3 Holding Register
            defs.push(RegisterDef {
                address: addr,
                register_type: RegisterType::HoldingRegister,
                data_type: DataType::UInt16,
                endian: Endian::Big,
                name: String::new(),
                comment: String::new(),
                mutation: None,
                data_source: None,
            });
            device
                .register_map
                .write_holding_register(addr, rng.gen::<u16>());

            // FC4 Input Register
            defs.push(RegisterDef {
                address: addr,
                register_type: RegisterType::InputRegister,
                data_type: DataType::UInt16,
                endian: Endian::Big,
                name: String::new(),
                comment: String::new(),
                mutation: None,
                data_source: None,
            });
            device
                .register_map
                .input_registers
                .insert(addr, rng.gen::<u16>());
        }

        device.register_defs = defs;
        device
    }
}

/// Running state of a slave connection.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ConnectionState {
    Stopped,
    Running,
}

/// Shared state accessible by all connections on a SlaveConnection.
pub type SharedDevices = Arc<RwLock<HashMap<u8, SlaveDevice>>>;

/// Shared log collector for all connections on a SlaveConnection.
pub type SharedLogCollector = Option<Arc<LogCollector>>;

/// One register write triggered by an incoming Modbus request.
#[derive(Debug, Clone)]
pub struct RegisterChange {
    pub slave_id: u8,
    pub register_type: RegisterType,
    pub address: u16,
    pub value: u16,
}

/// Callback fired (synchronously, off the request task) once per inbound
/// write request from a remote master, with the full list of resulting
/// register mutations. Slave-side internal writes (e.g. write_register
/// Tauri command, random_mutate) do NOT route through this callback —
/// those code paths emit notifications themselves.
pub type RegisterChangeCallback = Arc<dyn Fn(&[RegisterChange]) + Send + Sync>;
pub type SharedChangeCallback = Option<RegisterChangeCallback>;

/// A slave connection manages multiple SlaveDevices on a single transport.
pub struct SlaveConnection {
    pub transport: Transport,
    pub tls_config: SlaveTlsConfig,
    pub clients: ConnectedClients,
    pub devices: SharedDevices,
    pub log_collector: SharedLogCollector,
    pub change_callback: SharedChangeCallback,
    state: ConnectionState,
    shutdown_tx: Option<oneshot::Sender<()>>,
    server_handle: Option<tokio::task::JoinHandle<()>>,
}

impl SlaveConnection {
    pub fn new(transport: Transport) -> Self {
        Self {
            transport,
            tls_config: SlaveTlsConfig::default(),
            clients: ConnectedClients::default(),
            devices: Arc::new(RwLock::new(HashMap::new())),
            log_collector: None,
            change_callback: None,
            state: ConnectionState::Stopped,
            shutdown_tx: None,
            server_handle: None,
        }
    }

    /// Set the log collector for this connection.
    pub fn with_log_collector(mut self, collector: Arc<LogCollector>) -> Self {
        self.log_collector = Some(collector);
        self
    }

    /// Set the TLS configuration for this connection.
    pub fn with_tls_config(mut self, config: SlaveTlsConfig) -> Self {
        self.tls_config = config;
        self
    }

    /// Install (or replace) the callback fired when a remote master writes
    /// a register on this connection. Safe to call any time, including while
    /// the server is running.
    pub fn set_change_callback(&mut self, cb: RegisterChangeCallback) {
        self.change_callback = Some(cb);
    }

    pub fn state(&self) -> ConnectionState {
        self.state
    }

    /// Add a slave device. Returns error if the slave_id already exists.
    pub async fn add_device(&self, device: SlaveDevice) -> Result<(), SlaveError> {
        let mut devices = self.devices.write().await;
        if devices.contains_key(&device.slave_id) {
            return Err(SlaveError::DuplicateSlaveId(device.slave_id));
        }
        devices.insert(device.slave_id, device);
        Ok(())
    }

    /// Remove a slave device by ID. Returns error if not found.
    pub async fn remove_device(&self, slave_id: u8) -> Result<SlaveDevice, SlaveError> {
        let mut devices = self.devices.write().await;
        devices
            .remove(&slave_id)
            .ok_or(SlaveError::SlaveNotFound(slave_id))
    }

    /// Start the server. Idempotent: returns Ok if already running. Errors
    /// only on bind/transport failure.
    pub async fn start(&mut self) -> Result<(), SlaveError> {
        if self.state == ConnectionState::Running {
            return Ok(());
        }

        let (shutdown_tx, shutdown_rx) = oneshot::channel::<()>();
        let devices = self.devices.clone();
        let log_collector = self.log_collector.clone();
        let change_callback = self.change_callback.clone();

        self.clients = ConnectedClients::default();
        let clients = self.clients.clone();
        let handle = match &self.transport {
            Transport::Tcp { host, port } => {
                let addr = SocketAddr::new(
                    host.parse()
                        .map_err(|e| SlaveError::BindError(format!("Invalid address: {e}")))?,
                    *port,
                );

                let listener = TcpListener::bind(addr)
                    .await
                    .map_err(|e| SlaveError::BindError(format!("Failed to bind {addr}: {e}")))?;

                tokio::spawn(async move {
                    let server = Server::new(listener);
                    let on_connected = {
                        let devices = devices.clone();
                        let log_collector = log_collector.clone();
                        let change_callback = change_callback.clone();
                        move |stream: tokio::net::TcpStream, _socket_addr| {
                            let devices = devices.clone();
                            let log_collector = log_collector.clone();
                            let change_callback = change_callback.clone();
                            let clients = clients.clone();
                            async move {
                                let stream = stream.into_std()?;
                                let guard = clients.track(&stream)?;
                                let stream = tokio::net::TcpStream::from_std(stream)?;
                                let mut service =
                                    SlaveService::new(devices, log_collector, change_callback);
                                service._client_guard = Some(guard);
                                Ok::<_, std::io::Error>(Some((service, stream)))
                            }
                        }
                    };
                    let on_process_error = |err| {
                        log::error!("Slave server process error: {err}");
                    };
                    let abort_signal = Box::pin(async {
                        let _ = shutdown_rx.await;
                    });
                    let _ = server
                        .serve_until(&on_connected, on_process_error, abort_signal)
                        .await;
                })
            }
            Transport::Rtu(serial_config) => {
                let config = serial_config.clone();
                tokio::spawn(async move {
                    if let Err(e) = crate::rtu_slave::run_rtu_slave(
                        config,
                        devices,
                        log_collector,
                        change_callback,
                        shutdown_rx,
                    )
                    .await
                    {
                        log::error!("RTU slave error: {}", e);
                    }
                })
            }
            Transport::Ascii(serial_config) => {
                let config = serial_config.clone();
                tokio::spawn(async move {
                    if let Err(e) = crate::ascii_slave::run_ascii_slave(
                        config,
                        devices,
                        log_collector,
                        change_callback,
                        shutdown_rx,
                    )
                    .await
                    {
                        log::error!("ASCII slave error: {}", e);
                    }
                })
            }
            Transport::RtuOverTcp { host, port } => {
                let host = host.clone();
                let port = *port;
                tokio::spawn(async move {
                    if let Err(e) = crate::rtu_tcp_slave::run_rtu_tcp_slave(
                        host,
                        port,
                        devices,
                        log_collector,
                        change_callback,
                        clients,
                        shutdown_rx,
                    )
                    .await
                    {
                        log::error!("RTU-over-TCP slave error: {}", e);
                    }
                })
            }
            Transport::TcpTls { host, port } => {
                let addr = SocketAddr::new(
                    host.parse()
                        .map_err(|e| SlaveError::BindError(format!("Invalid address: {e}")))?,
                    *port,
                );
                let tls_config = self.tls_config.clone();
                tokio::spawn(async move {
                    if let Err(e) = crate::tls_slave::run_tls_slave(
                        addr,
                        tls_config,
                        devices,
                        log_collector,
                        change_callback,
                        clients,
                        shutdown_rx,
                    )
                    .await
                    {
                        log::error!("TLS slave error: {}", e);
                    }
                })
            }
        };

        self.shutdown_tx = Some(shutdown_tx);
        self.server_handle = Some(handle);
        self.state = ConnectionState::Running;
        Ok(())
    }

    /// Stop the server gracefully. Idempotent: returns Ok if already stopped.
    pub async fn stop(&mut self) -> Result<(), SlaveError> {
        if self.state == ConnectionState::Stopped {
            return Ok(());
        }

        if let Some(tx) = self.shutdown_tx.take() {
            let _ = tx.send(());
        }
        if let Some(handle) = self.server_handle.take() {
            let _ = handle.await;
        }
        self.clients.close_all();
        self.state = ConnectionState::Stopped;
        Ok(())
    }
}

/// The Modbus service that handles requests for a slave connection.
/// Shared across all client connections via the `new_service` closure.
/// Build the list of register mutations implied by a successful write
/// request. Modbus write function codes only mutate writable data areas:
/// coils (FC05/FC15) and holding registers (FC06/FC16).
pub fn changes_from_tokio_request(slave_id: u8, req: &Request<'_>) -> Vec<RegisterChange> {
    match req {
        Request::WriteSingleCoil(addr, value) => {
            let v = if *value { 1 } else { 0 };
            vec![RegisterChange {
                slave_id,
                register_type: RegisterType::Coil,
                address: *addr,
                value: v,
            }]
        }
        Request::WriteSingleRegister(addr, value) => {
            vec![RegisterChange {
                slave_id,
                register_type: RegisterType::HoldingRegister,
                address: *addr,
                value: *value,
            }]
        }
        Request::WriteMultipleCoils(addr, values) => {
            let mut out = Vec::with_capacity(values.len());
            for (i, &v) in values.iter().enumerate() {
                let a = addr.wrapping_add(i as u16);
                let val = if v { 1 } else { 0 };
                out.push(RegisterChange {
                    slave_id,
                    register_type: RegisterType::Coil,
                    address: a,
                    value: val,
                });
            }
            out
        }
        Request::WriteMultipleRegisters(addr, values) => {
            let mut out = Vec::with_capacity(values.len());
            for (i, &v) in values.iter().enumerate() {
                let a = addr.wrapping_add(i as u16);
                out.push(RegisterChange {
                    slave_id,
                    register_type: RegisterType::HoldingRegister,
                    address: a,
                    value: v,
                });
            }
            out
        }
        _ => Vec::new(),
    }
}

struct SlaveService {
    _client_guard: Option<ClientGuard>,
    devices: SharedDevices,
    log_collector: SharedLogCollector,
    change_callback: SharedChangeCallback,
}

impl SlaveService {
    fn new(
        devices: SharedDevices,
        log_collector: SharedLogCollector,
        change_callback: SharedChangeCallback,
    ) -> Self {
        Self {
            _client_guard: None,
            devices,
            log_collector,
            change_callback,
        }
    }

    fn get_function_code(request: &Request<'_>) -> Option<FunctionCode> {
        match request {
            Request::ReadCoils(..) => Some(FunctionCode::ReadCoils),
            Request::ReadDiscreteInputs(..) => Some(FunctionCode::ReadDiscreteInputs),
            Request::ReadHoldingRegisters(..) => Some(FunctionCode::ReadHoldingRegisters),
            Request::ReadInputRegisters(..) => Some(FunctionCode::ReadInputRegisters),
            Request::WriteSingleCoil(..) => Some(FunctionCode::WriteSingleCoil),
            Request::WriteSingleRegister(..) => Some(FunctionCode::WriteSingleRegister),
            Request::WriteMultipleCoils(..) => Some(FunctionCode::WriteMultipleCoils),
            Request::WriteMultipleRegisters(..) => Some(FunctionCode::WriteMultipleRegisters),
            _ => None,
        }
    }

    fn format_request_detail(request: &Request<'_>) -> String {
        match request {
            Request::ReadCoils(addr, qty) => format!("R {} x{}", addr, qty),
            Request::ReadDiscreteInputs(addr, qty) => format!("R {} x{}", addr, qty),
            Request::ReadHoldingRegisters(addr, qty) => format!("R {} x{}", addr, qty),
            Request::ReadInputRegisters(addr, qty) => format!("R {} x{}", addr, qty),
            Request::WriteSingleCoil(addr, val) => format!("W {} = {}", addr, val),
            Request::WriteSingleRegister(addr, val) => format!("W {} = {:#06x}", addr, val),
            Request::WriteMultipleCoils(addr, vals) => format!("W {} x{}", addr, vals.len()),
            Request::WriteMultipleRegisters(addr, vals) => format!("W {} x{}", addr, vals.len()),
            _ => "?".to_string(),
        }
    }

    fn log_if_enabled(&self, direction: Direction, fc: FunctionCode, detail: &str) {
        if let Some(collector) = &self.log_collector {
            let entry = LogEntry::new(direction, fc, detail);
            collector.try_add(entry);
        }
    }
}

impl Service for SlaveService {
    type Request = SlaveRequest<'static>;
    type Response = Option<Response>;
    type Exception = ExceptionCode;
    type Future = Pin<Box<dyn Future<Output = Result<Option<Response>, ExceptionCode>> + Send>>;

    fn call(&self, req: Self::Request) -> Self::Future {
        let SlaveRequest { slave, request } = req;
        let devices = self.devices.clone();
        let log_collector = self.log_collector.clone();
        let change_callback = self.change_callback.clone();

        // Log inbound request & save fc for response log
        let fc = Self::get_function_code(&request);
        if let Some(fc) = fc {
            let detail = Self::format_request_detail(&request);
            self.log_if_enabled(Direction::Rx, fc, &detail);
        }

        let is_write = matches!(
            request,
            Request::WriteSingleCoil(..)
                | Request::WriteSingleRegister(..)
                | Request::WriteMultipleCoils(..)
                | Request::WriteMultipleRegisters(..)
        );

        Box::pin(async move {
            // Snapshot any change list before moving `request` into handle_write.
            let pending_changes: Vec<RegisterChange> = if is_write && change_callback.is_some() {
                changes_from_tokio_request(slave, &request)
            } else {
                Vec::new()
            };

            let result = if is_write {
                let mut devices = devices.write().await;
                match devices.get_mut(&slave) {
                    Some(device) => Some(handle_write(&mut device.register_map, request)),
                    None => None,
                }
            } else {
                let devices = devices.read().await;
                devices
                    .get(&slave)
                    .map(|device| handle_read(&device.register_map, request))
            };

            // Fire callback only on successful writes.
            if matches!(&result, Some(Ok(_))) {
                if let Some(cb) = &change_callback {
                    if !pending_changes.is_empty() {
                        cb(&pending_changes);
                    }
                }
            }

            // Log outbound response
            if let (Some(fc), Some(collector)) = (fc, &log_collector) {
                match &result {
                    Some(Ok(_)) => {
                        collector.try_add(LogEntry::new(Direction::Tx, fc, "OK"));
                    }
                    Some(Err(exc)) => {
                        collector.try_add(LogEntry::new(
                            Direction::Tx,
                            fc,
                            format!("ERR: {:?}", exc),
                        ));
                    }
                    None => {}
                }
            }

            match result {
                Some(Ok(response)) => Ok(Some(response)),
                Some(Err(exception)) => Err(exception),
                None => Ok(None), // Unknown slave ID: silent drop
            }
        })
    }
}

/// Validate quantity and address overflow for read/write requests.
fn validate_quantity(addr: u16, quantity: u16, max_quantity: u16) -> Result<(), ExceptionCode> {
    if quantity == 0 || quantity > max_quantity {
        return Err(ExceptionCode::IllegalDataValue);
    }
    if (addr as u32) + (quantity as u32) > 65536 {
        return Err(ExceptionCode::IllegalDataAddress);
    }
    Ok(())
}

/// Handle read-only Modbus requests.
fn handle_read(
    register_map: &RegisterMap,
    request: Request<'static>,
) -> Result<Response, ExceptionCode> {
    match request {
        // FC01: Read Coils (max 2000)
        Request::ReadCoils(addr, quantity) => {
            validate_quantity(addr, quantity, 2000)?;
            if !register_map.has_all_coils(addr, quantity) {
                return Err(ExceptionCode::IllegalDataAddress);
            }
            Ok(Response::ReadCoils(register_map.read_coils(addr, quantity)))
        }
        // FC02: Read Discrete Inputs (max 2000)
        Request::ReadDiscreteInputs(addr, quantity) => {
            validate_quantity(addr, quantity, 2000)?;
            if !register_map.has_all_discrete_inputs(addr, quantity) {
                return Err(ExceptionCode::IllegalDataAddress);
            }
            Ok(Response::ReadDiscreteInputs(
                register_map.read_discrete_inputs(addr, quantity),
            ))
        }
        // FC03: Read Holding Registers (max 125)
        Request::ReadHoldingRegisters(addr, quantity) => {
            validate_quantity(addr, quantity, 125)?;
            if !register_map.has_all_holding_registers(addr, quantity) {
                return Err(ExceptionCode::IllegalDataAddress);
            }
            Ok(Response::ReadHoldingRegisters(
                register_map.read_holding_registers(addr, quantity),
            ))
        }
        // FC04: Read Input Registers (max 125)
        Request::ReadInputRegisters(addr, quantity) => {
            validate_quantity(addr, quantity, 125)?;
            if !register_map.has_all_input_registers(addr, quantity) {
                return Err(ExceptionCode::IllegalDataAddress);
            }
            Ok(Response::ReadInputRegisters(
                register_map.read_input_registers(addr, quantity),
            ))
        }
        // Unsupported function codes
        _ => Err(ExceptionCode::IllegalFunction),
    }
}

/// Handle write Modbus requests (requires mutable access to register map).
fn handle_write(
    register_map: &mut RegisterMap,
    request: Request<'static>,
) -> Result<Response, ExceptionCode> {
    match request {
        // FC05: Write Single Coil
        Request::WriteSingleCoil(addr, value) => {
            if !register_map.has_coil(addr) {
                return Err(ExceptionCode::IllegalDataAddress);
            }
            register_map.write_coil(addr, value);
            Ok(Response::WriteSingleCoil(addr, value))
        }
        // FC06: Write Single Register
        Request::WriteSingleRegister(addr, value) => {
            if !register_map.has_holding_register(addr) {
                return Err(ExceptionCode::IllegalDataAddress);
            }
            register_map.write_holding_register(addr, value);
            Ok(Response::WriteSingleRegister(addr, value))
        }
        // FC15: Write Multiple Coils (max 1968)
        Request::WriteMultipleCoils(addr, values) => {
            let quantity = values.len() as u16;
            validate_quantity(addr, quantity, 1968)?;
            if !register_map.has_all_coils(addr, quantity) {
                return Err(ExceptionCode::IllegalDataAddress);
            }
            register_map.write_coils(addr, &values);
            Ok(Response::WriteMultipleCoils(addr, quantity))
        }
        // FC16: Write Multiple Registers (max 123)
        Request::WriteMultipleRegisters(addr, values) => {
            let quantity = values.len() as u16;
            validate_quantity(addr, quantity, 123)?;
            if !register_map.has_all_holding_registers(addr, quantity) {
                return Err(ExceptionCode::IllegalDataAddress);
            }
            register_map.write_holding_registers(addr, &values);
            Ok(Response::WriteMultipleRegisters(addr, quantity))
        }
        _ => Err(ExceptionCode::IllegalFunction),
    }
}

#[derive(Debug, thiserror::Error)]
pub enum SlaveError {
    #[error("slave ID {0} already exists")]
    DuplicateSlaveId(u8),
    #[error("slave ID {0} not found")]
    SlaveNotFound(u8),
    #[error("server is already running")]
    AlreadyRunning,
    #[error("server is not running")]
    NotRunning,
    #[error("bind error: {0}")]
    BindError(String),
    #[error("TLS error: {0}")]
    TlsError(String),
    #[error("certificate error: {0}")]
    CertError(String),
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_slave_device_creation() {
        let device = SlaveDevice::new(1, "Test Device");
        assert_eq!(device.slave_id, 1);
        assert_eq!(device.name, "Test Device");
        assert!(device.register_map.holding_registers.is_empty());
        assert!(device.register_defs.is_empty());
    }

    #[test]
    fn test_handle_read_holding_registers() {
        let mut map = RegisterMap::new();
        map.write_holding_register(0, 1234);
        map.write_holding_register(1, 5678);

        let response = handle_read(&map, Request::ReadHoldingRegisters(0, 2)).unwrap();
        match response {
            Response::ReadHoldingRegisters(values) => {
                assert_eq!(values, vec![1234, 5678]);
            }
            _ => panic!("unexpected response"),
        }
    }

    #[test]
    fn test_handle_read_coils() {
        let mut map = RegisterMap::new();
        map.write_coil(0, true);
        map.write_coil(1, false);
        map.write_coil(2, true);

        let response = handle_read(&map, Request::ReadCoils(0, 3)).unwrap();
        match response {
            Response::ReadCoils(values) => {
                assert_eq!(values, vec![true, false, true]);
            }
            _ => panic!("unexpected response"),
        }
    }

    #[test]
    fn test_handle_read_discrete_inputs() {
        let mut map = RegisterMap::new();
        map.discrete_inputs.insert(0, true);
        map.discrete_inputs.insert(1, false);

        let response = handle_read(&map, Request::ReadDiscreteInputs(0, 2)).unwrap();
        match response {
            Response::ReadDiscreteInputs(values) => {
                assert_eq!(values, vec![true, false]);
            }
            _ => panic!("unexpected response"),
        }
    }

    #[test]
    fn test_handle_read_input_registers() {
        let mut map = RegisterMap::new();
        map.input_registers.insert(0, 100);
        map.input_registers.insert(1, 200);

        let response = handle_read(&map, Request::ReadInputRegisters(0, 2)).unwrap();
        match response {
            Response::ReadInputRegisters(values) => {
                assert_eq!(values, vec![100, 200]);
            }
            _ => panic!("unexpected response"),
        }
    }

    #[test]
    fn test_handle_write_single_coil() {
        let mut map = RegisterMap::new();
        map.coils.insert(5, false);
        let response = handle_write(&mut map, Request::WriteSingleCoil(5, true)).unwrap();
        assert!(matches!(response, Response::WriteSingleCoil(5, true)));
        assert_eq!(map.read_coils(5, 1), vec![true]);
    }

    #[test]
    fn test_handle_write_single_register() {
        let mut map = RegisterMap::new();
        map.holding_registers.insert(10, 0);
        let response = handle_write(&mut map, Request::WriteSingleRegister(10, 0xABCD)).unwrap();
        assert!(matches!(
            response,
            Response::WriteSingleRegister(10, 0xABCD)
        ));
        assert_eq!(map.read_holding_registers(10, 1), vec![0xABCD]);
    }

    #[test]
    fn test_handle_write_multiple_coils() {
        let mut map = RegisterMap::new();
        for a in 0..3 {
            map.coils.insert(a, false);
        }
        let values = vec![true, false, true];
        let response = handle_write(
            &mut map,
            Request::WriteMultipleCoils(0, std::borrow::Cow::Owned(values)),
        )
        .unwrap();
        assert!(matches!(response, Response::WriteMultipleCoils(0, 3)));
        assert_eq!(map.read_coils(0, 3), vec![true, false, true]);
    }

    #[test]
    fn test_handle_write_multiple_registers() {
        let mut map = RegisterMap::new();
        for a in 0..3 {
            map.holding_registers.insert(a, 0);
        }
        let values = vec![100, 200, 300];
        let response = handle_write(
            &mut map,
            Request::WriteMultipleRegisters(0, std::borrow::Cow::Owned(values)),
        )
        .unwrap();
        assert!(matches!(response, Response::WriteMultipleRegisters(0, 3)));
        assert_eq!(map.read_holding_registers(0, 3), vec![100, 200, 300]);
    }

    #[test]
    fn test_handle_unsupported_function() {
        let map = RegisterMap::new();
        let result = handle_read(&map, Request::ReportServerId);
        assert_eq!(result.unwrap_err(), ExceptionCode::IllegalFunction);
    }

    #[tokio::test]
    async fn test_slave_connection_add_remove_device() {
        let conn = SlaveConnection::new(Transport::Tcp {
            host: "0.0.0.0".to_string(),
            port: 502,
        });
        let device = SlaveDevice::new(1, "Test");
        conn.add_device(device).await.unwrap();

        // Duplicate should fail
        let dup = SlaveDevice::new(1, "Dup");
        assert!(conn.add_device(dup).await.is_err());

        // Remove should work
        let removed = conn.remove_device(1).await.unwrap();
        assert_eq!(removed.slave_id, 1);

        // Remove again should fail
        assert!(conn.remove_device(1).await.is_err());
    }

    #[test]
    fn test_with_default_registers() {
        let device = SlaveDevice::with_default_registers(1, "从站 1", 100);
        assert_eq!(device.slave_id, 1);
        assert_eq!(device.name, "从站 1");

        // 4 types x 101 addresses = 404 register defs
        assert_eq!(device.register_defs.len(), 404);

        // Verify register map values initialized
        assert_eq!(device.register_map.coils.len(), 101);
        assert_eq!(device.register_map.discrete_inputs.len(), 101);
        assert_eq!(device.register_map.holding_registers.len(), 101);
        assert_eq!(device.register_map.input_registers.len(), 101);

        // All coils should be false
        for addr in 0..=100u16 {
            assert_eq!(device.register_map.coils.get(&addr), Some(&false));
        }

        // All holding registers should be 0
        for addr in 0..=100u16 {
            assert_eq!(device.register_map.holding_registers.get(&addr), Some(&0));
        }
    }

    #[tokio::test]
    async fn test_connection_with_default_device() {
        let conn = SlaveConnection::new(Transport::Tcp {
            host: "0.0.0.0".to_string(),
            port: 502,
        });
        let device = SlaveDevice::with_default_registers(1, "从站 1", 100);
        conn.add_device(device).await.unwrap();

        let devices = conn.devices.read().await;
        assert_eq!(devices.len(), 1);

        let dev = devices.get(&1).unwrap();
        assert_eq!(dev.register_defs.len(), 404);

        // Verify FC03 ReadHoldingRegisters works with default values
        let values = dev.register_map.read_holding_registers(0, 10);
        assert_eq!(values, vec![0; 10]);
    }

    #[test]
    fn test_with_random_registers() {
        let device = SlaveDevice::with_random_registers(1, "随机从站", 100);
        assert_eq!(device.slave_id, 1);
        assert_eq!(device.name, "随机从站");

        // 4 types x 101 addresses = 404 register defs
        assert_eq!(device.register_defs.len(), 404);

        // Verify register map values initialized
        assert_eq!(device.register_map.coils.len(), 101);
        assert_eq!(device.register_map.discrete_inputs.len(), 101);
        assert_eq!(device.register_map.holding_registers.len(), 101);
        assert_eq!(device.register_map.input_registers.len(), 101);

        // At least some values should be non-zero/true (statistically near-certain with 101 entries)
        let has_true_coil = (0..=100u16).any(|addr| *device.register_map.coils.get(&addr).unwrap());
        let has_nonzero_hr = (0..=100u16)
            .any(|addr| *device.register_map.holding_registers.get(&addr).unwrap() != 0);
        assert!(
            has_true_coil,
            "expected at least one true coil with random init"
        );
        assert!(
            has_nonzero_hr,
            "expected at least one non-zero holding register with random init"
        );
    }

    #[test]
    fn slave_device_deserializes_legacy_json_without_jitter() {
        // Older .modbusproj files wrote SlaveDevice without a `jitter` field.
        let legacy = r#"{
            "slave_id": 1,
            "name": "legacy",
            "register_map": {
                "coils": {},
                "discrete_inputs": {},
                "holding_registers": {},
                "input_registers": {}
            },
            "register_defs": []
        }"#;
        let d: SlaveDevice = serde_json::from_str(legacy).expect("legacy parse");
        assert_eq!(d.slave_id, 1);
        assert_eq!(d.name, "legacy");
    }
}
