use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::Path;

use crate::config::RegisterValues;
use crate::reconnect::ReconnectPolicy;
use crate::register::RegisterDef;
use crate::request::RequestSettings;
use crate::socks5::Socks5Config;
use crate::transport::{SlaveTlsConfig, TlsConfig};

/// Project type: slave or master.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ProjectType {
    Slave,
    Master,
}

/// Transport configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum TransportConfig {
    Tcp {
        host: String,
        port: u16,
    },
    TcpTls {
        host: String,
        port: u16,
        #[serde(default)]
        client_tls: Box<TlsConfig>,
        #[serde(default)]
        server_tls: Box<SlaveTlsConfig>,
    },
    Rtu {
        port: String,
        baud_rate: u32,
        data_bits: u8,
        stop_bits: u8,
        parity: String,
    },
    Ascii {
        port: String,
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

/// A register block definition in a project file.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegisterBlockConfig {
    pub address: u16,
    pub count: u16,
    #[serde(default)]
    pub data_type: Option<String>,
    #[serde(default)]
    pub endian: Option<String>,
    #[serde(default)]
    pub values: Vec<serde_json::Value>,
    #[serde(default)]
    pub names: HashMap<String, String>,
}

/// A slave device definition in a project file.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DeviceConfig {
    pub slave_id: u8,
    #[serde(default)]
    pub name: String,
    /// Current point-oriented representation. Optional for compatibility with
    /// early v1 project files that only contained register blocks.
    #[serde(default)]
    pub register_defs: Vec<RegisterDef>,
    #[serde(default)]
    pub registers: RegistersConfig,
    /// Current raw values for all four Modbus areas.
    #[serde(default)]
    pub values: RegisterValues,
}

/// Register configuration grouped by type.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct RegistersConfig {
    #[serde(default)]
    pub coils: Vec<RegisterBlockConfig>,
    #[serde(default)]
    pub discrete_inputs: Vec<RegisterBlockConfig>,
    #[serde(default)]
    pub holding: Vec<RegisterBlockConfig>,
    #[serde(default)]
    pub input: Vec<RegisterBlockConfig>,
}

/// A scan group definition (master project only).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScanGroupConfig {
    #[serde(default)]
    pub id: String,
    pub name: String,
    pub slave_id: u8,
    pub function_code: u8,
    pub start_address: u16,
    pub count: u16,
    pub interval_ms: u64,
    #[serde(default = "default_enabled")]
    pub enabled: bool,
}

fn default_enabled() -> bool {
    true
}

/// A connection definition in a project file.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectionConfig {
    pub id: String,
    pub name: String,
    pub transport: TransportConfig,
    #[serde(default)]
    pub devices: Vec<DeviceConfig>,
    #[serde(default)]
    pub scan_groups: Vec<ScanGroupConfig>,
    #[serde(default = "default_slave_id")]
    pub default_slave_id: u8,
    #[serde(default = "default_timeout_ms")]
    pub timeout_ms: u64,
    #[serde(default)]
    pub requests: RequestSettings,
    #[serde(default)]
    pub reconnect_policy: ReconnectPolicy,
    #[serde(default, skip_serializing_if = "Socks5Config::is_disabled")]
    pub socks5: Socks5Config,
}

fn default_slave_id() -> u8 {
    1
}

fn default_timeout_ms() -> u64 {
    3000
}

/// The top-level project file structure.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProjectFile {
    pub version: u32,
    #[serde(rename = "type")]
    pub project_type: ProjectType,
    pub connections: Vec<ConnectionConfig>,
}

impl ProjectFile {
    pub fn new_slave() -> Self {
        Self {
            version: 1,
            project_type: ProjectType::Slave,
            connections: Vec::new(),
        }
    }

    pub fn new_master() -> Self {
        Self {
            version: 1,
            project_type: ProjectType::Master,
            connections: Vec::new(),
        }
    }
}

/// Save a project file to the given path as pretty-printed JSON.
pub fn save_project(project: &ProjectFile, path: &Path) -> Result<(), String> {
    let json = serde_json::to_string_pretty(project)
        .map_err(|e| format!("failed to serialize project: {}", e))?;
    std::fs::write(path, json).map_err(|e| format!("failed to write project file: {}", e))
}

/// Load a project file from the given path.
pub fn load_project(path: &Path) -> Result<ProjectFile, String> {
    let data =
        std::fs::read_to_string(path).map_err(|e| format!("failed to read project file: {}", e))?;
    migrate_project(&data)
}

/// Migrate an older project file format to the current version.
/// Currently only version 1 is supported.
pub fn migrate_project(data: &str) -> Result<ProjectFile, String> {
    let value: serde_json::Value =
        serde_json::from_str(data).map_err(|e| format!("invalid JSON: {}", e))?;

    let version = value
        .get("version")
        .and_then(|v| v.as_u64())
        .ok_or("missing or invalid version field")?;

    match version {
        1 => {
            serde_json::from_value(value).map_err(|e| format!("failed to parse project v1: {}", e))
        }
        v => Err(format!("unsupported project version: {}", v)),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::data_source::{DataSource, DataSourceConfig};
    use crate::mutation::{MutationConfig, MutationMode};
    use crate::register::{DataType, Endian, RegisterType};
    use tempfile::TempDir;

    #[test]
    fn test_new_slave_project() {
        let p = ProjectFile::new_slave();
        assert_eq!(p.version, 1);
        assert_eq!(p.project_type, ProjectType::Slave);
        assert!(p.connections.is_empty());
    }

    #[test]
    fn test_new_master_project() {
        let p = ProjectFile::new_master();
        assert_eq!(p.version, 1);
        assert_eq!(p.project_type, ProjectType::Master);
        assert!(p.connections.is_empty());
    }

    #[test]
    fn test_save_and_load_slave_project() {
        let dir = TempDir::new().unwrap();
        let path = dir.path().join("test.modbusproj");

        let mut project = ProjectFile::new_slave();
        project.connections.push(ConnectionConfig {
            id: "conn-1".into(),
            name: "Local TCP".into(),
            transport: TransportConfig::Tcp {
                host: "127.0.0.1".into(),
                port: 502,
            },
            devices: vec![DeviceConfig {
                slave_id: 1,
                name: "Slave 1".into(),
                register_defs: vec![],
                registers: RegistersConfig {
                    holding: vec![RegisterBlockConfig {
                        address: 0,
                        count: 10,
                        data_type: Some("uint16".into()),
                        endian: None,
                        values: vec![serde_json::json!(0), serde_json::json!(100)],
                        names: HashMap::from([("0".into(), "Temperature".into())]),
                    }],
                    ..Default::default()
                },
                values: RegisterValues::default(),
            }],
            scan_groups: vec![],
            default_slave_id: 1,
            timeout_ms: 3000,
            requests: RequestSettings::default(),
            reconnect_policy: ReconnectPolicy::default(),
            socks5: Socks5Config::default(),
        });

        save_project(&project, &path).unwrap();
        let loaded = load_project(&path).unwrap();

        assert_eq!(loaded.version, 1);
        assert_eq!(loaded.project_type, ProjectType::Slave);
        assert_eq!(loaded.connections.len(), 1);

        let conn = &loaded.connections[0];
        assert_eq!(conn.id, "conn-1");
        assert_eq!(conn.name, "Local TCP");
        match &conn.transport {
            TransportConfig::Tcp { host, port } => {
                assert_eq!(host, "127.0.0.1");
                assert_eq!(*port, 502);
            }
            _ => panic!("wrong transport variant"),
        }

        assert_eq!(conn.devices.len(), 1);
        let dev = &conn.devices[0];
        assert_eq!(dev.slave_id, 1);
        assert_eq!(dev.registers.holding.len(), 1);
        assert_eq!(dev.registers.holding[0].address, 0);
        assert_eq!(dev.registers.holding[0].count, 10);
        assert_eq!(
            dev.registers.holding[0].data_type,
            Some("uint16".to_string())
        );
        assert_eq!(dev.registers.holding[0].values.len(), 2);
        assert_eq!(
            dev.registers.holding[0].names.get("0"),
            Some(&"Temperature".to_string())
        );
    }

    #[test]
    fn test_save_and_load_master_project() {
        let dir = TempDir::new().unwrap();
        let path = dir.path().join("master.modbusproj");

        let mut project = ProjectFile::new_master();
        project.connections.push(ConnectionConfig {
            id: "conn-m1".into(),
            name: "Remote PLC".into(),
            transport: TransportConfig::Tcp {
                host: "192.168.1.10".into(),
                port: 502,
            },
            devices: vec![],
            scan_groups: vec![ScanGroupConfig {
                id: "fast-poll".into(),
                name: "Fast Poll".into(),
                slave_id: 1,
                function_code: 3,
                start_address: 0,
                count: 10,
                interval_ms: 1000,
                enabled: true,
            }],
            default_slave_id: 1,
            timeout_ms: 3000,
            requests: RequestSettings::default(),
            reconnect_policy: ReconnectPolicy::default(),
            socks5: Socks5Config {
                enabled: true,
                host: "proxy.example.com".to_string(),
                port: 1080,
                username: "operator".to_string(),
                password: "secret".to_string(),
            },
        });

        save_project(&project, &path).unwrap();
        let loaded = load_project(&path).unwrap();

        assert_eq!(loaded.project_type, ProjectType::Master);
        assert_eq!(loaded.connections.len(), 1);

        let conn = &loaded.connections[0];
        assert_eq!(conn.id, "conn-m1");
        assert_eq!(conn.scan_groups.len(), 1);
        assert!(conn.socks5.enabled);
        assert_eq!(conn.socks5.host, "proxy.example.com");
        assert_eq!(conn.socks5.username, "operator");
        assert_eq!(conn.socks5.password, "secret");

        let sg = &conn.scan_groups[0];
        assert_eq!(sg.name, "Fast Poll");
        assert_eq!(sg.slave_id, 1);
        assert_eq!(sg.function_code, 3);
        assert_eq!(sg.start_address, 0);
        assert_eq!(sg.count, 10);
        assert_eq!(sg.interval_ms, 1000);
    }

    #[test]
    fn point_mutation_config_survives_project_roundtrip() {
        let dir = TempDir::new().unwrap();
        let path = dir.path().join("mutation.modbusproj");
        let mut project = ProjectFile::new_slave();
        project.connections.push(ConnectionConfig {
            id: "slave_1".into(),
            name: "Local".into(),
            transport: TransportConfig::Tcp {
                host: "0.0.0.0".into(),
                port: 5020,
            },
            devices: vec![DeviceConfig {
                slave_id: 7,
                name: "Pump".into(),
                register_defs: vec![RegisterDef {
                    address: 10,
                    register_type: RegisterType::HoldingRegister,
                    data_type: DataType::Float32,
                    endian: Endian::Big,
                    name: "speed".into(),
                    comment: String::new(),
                    mutation: Some(MutationConfig {
                        enabled: true,
                        mode: MutationMode::Increment,
                        period_ms: 750,
                        step: 0.5,
                        min: 0.0,
                        max: 10.0,
                    }),
                    data_source: None,
                }],
                registers: RegistersConfig::default(),
                values: RegisterValues::default(),
            }],
            scan_groups: vec![],
            default_slave_id: 1,
            timeout_ms: 3000,
            requests: RequestSettings::default(),
            reconnect_policy: ReconnectPolicy::default(),
            socks5: Socks5Config::default(),
        });

        save_project(&project, &path).unwrap();
        let loaded = load_project(&path).unwrap();
        let mutation = loaded.connections[0].devices[0].register_defs[0]
            .mutation
            .as_ref()
            .unwrap();
        assert_eq!(mutation.mode, MutationMode::Increment);
        assert_eq!(mutation.period_ms, 750);
        assert_eq!(loaded.connections[0].devices[0].name, "Pump");
    }

    #[test]
    fn old_v1_device_without_new_fields_still_loads() {
        let json = r#"{
            "version": 1,
            "type": "slave",
            "connections": [{
                "id": "legacy",
                "name": "Legacy",
                "transport": { "type": "tcp", "host": "0.0.0.0", "port": 502 },
                "devices": [{ "slave_id": 1, "registers": {} }]
            }]
        }"#;
        let loaded = migrate_project(json).unwrap();
        let device = &loaded.connections[0].devices[0];
        assert!(device.name.is_empty());
        assert!(device.register_defs.is_empty());
        assert!(!loaded.connections[0].socks5.enabled);
    }

    #[test]
    fn test_load_nonexistent_file() {
        let result = load_project(Path::new("/tmp/does_not_exist_12345.modbusproj"));
        assert!(result.is_err());
    }

    #[test]
    fn test_load_invalid_json() {
        let dir = TempDir::new().unwrap();
        let path = dir.path().join("bad.modbusproj");
        std::fs::write(&path, "not json").unwrap();
        let result = load_project(&path);
        assert!(result.is_err());
    }

    #[test]
    fn test_migrate_current_version() {
        let data = r#"{"version":1,"type":"slave","connections":[]}"#;
        let project = migrate_project(data).unwrap();
        assert_eq!(project.version, 1);
        assert_eq!(project.project_type, ProjectType::Slave);
        assert!(project.connections.is_empty());
    }

    #[test]
    fn test_migrate_unknown_version() {
        let data = r#"{"version":99,"type":"slave","connections":[]}"#;
        let result = migrate_project(data);
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("unsupported project version"));
    }

    #[test]
    fn test_json_roundtrip_preserves_transport_tag() {
        let mut project = ProjectFile::new_slave();
        project.connections.push(ConnectionConfig {
            id: "c1".into(),
            name: "Test".into(),
            transport: TransportConfig::Tcp {
                host: "localhost".into(),
                port: 5020,
            },
            devices: vec![],
            scan_groups: vec![],
            default_slave_id: 1,
            timeout_ms: 3000,
            requests: RequestSettings::default(),
            reconnect_policy: ReconnectPolicy::default(),
            socks5: Socks5Config::default(),
        });

        let json = serde_json::to_string(&project).unwrap();
        assert!(json.contains(r#""type":"tcp""#));
        assert!(!json.contains("socks5"));

        let loaded: ProjectFile = serde_json::from_str(&json).unwrap();
        match &loaded.connections[0].transport {
            TransportConfig::Tcp { host, port } => {
                assert_eq!(host, "localhost");
                assert_eq!(*port, 5020);
            }
            _ => panic!("wrong transport variant"),
        }
    }

    #[test]
    fn test_transport_rtu_serde() {
        let proj = ProjectFile {
            version: 1,
            project_type: ProjectType::Slave,
            connections: vec![ConnectionConfig {
                id: "c1".to_string(),
                name: "serial".to_string(),
                transport: TransportConfig::Rtu {
                    port: "/dev/ttyUSB0".to_string(),
                    baud_rate: 9600,
                    data_bits: 8,
                    stop_bits: 1,
                    parity: "none".to_string(),
                },
                devices: vec![],
                scan_groups: vec![],
                default_slave_id: 1,
                timeout_ms: 3000,
                requests: RequestSettings::default(),
                reconnect_policy: ReconnectPolicy::default(),
                socks5: Socks5Config::default(),
            }],
        };
        let json = serde_json::to_string(&proj).unwrap();
        assert!(json.contains("\"type\":\"rtu\""));
        assert!(json.contains("ttyUSB0"));
        let loaded: ProjectFile = serde_json::from_str(&json).unwrap();
        match &loaded.connections[0].transport {
            TransportConfig::Rtu {
                port, baud_rate, ..
            } => {
                assert_eq!(port, "/dev/ttyUSB0");
                assert_eq!(*baud_rate, 9600);
            }
            _ => panic!("wrong transport variant"),
        }
    }

    #[test]
    fn test_transport_rtu_over_tcp_serde() {
        let config = TransportConfig::RtuOverTcp {
            host: "10.0.0.1".to_string(),
            port: 502,
        };
        let json = serde_json::to_string(&config).unwrap();
        assert!(json.contains("\"type\":\"rtu_over_tcp\""));
        let loaded: TransportConfig = serde_json::from_str(&json).unwrap();
        match loaded {
            TransportConfig::RtuOverTcp { host, port } => {
                assert_eq!(host, "10.0.0.1");
                assert_eq!(port, 502);
            }
            _ => panic!("wrong variant"),
        }
    }

    #[test]
    fn tls_values_and_data_source_survive_roundtrip() {
        let server_tls = SlaveTlsConfig {
            enabled: true,
            cert_file: "server.pem".to_string(),
            ..Default::default()
        };
        let client_tls = TlsConfig {
            enabled: true,
            ca_file: "ca.pem".to_string(),
            ..Default::default()
        };
        let definition = RegisterDef {
            address: 7,
            register_type: RegisterType::HoldingRegister,
            data_type: DataType::UInt16,
            endian: Endian::Big,
            name: "generated".to_string(),
            comment: String::new(),
            mutation: None,
            data_source: Some(DataSourceConfig {
                source: DataSource::Counter {
                    start: 12,
                    step: 2,
                    wrap: true,
                },
                update_interval_ms: 250,
            }),
        };
        let project = ProjectFile {
            version: 1,
            project_type: ProjectType::Slave,
            connections: vec![ConnectionConfig {
                id: "tls".to_string(),
                name: "TLS".to_string(),
                transport: TransportConfig::TcpTls {
                    host: "127.0.0.1".to_string(),
                    port: 802,
                    client_tls: Box::new(client_tls.clone()),
                    server_tls: Box::new(server_tls.clone()),
                },
                devices: vec![DeviceConfig {
                    slave_id: 1,
                    name: "device".to_string(),
                    register_defs: vec![definition],
                    registers: RegistersConfig::default(),
                    values: RegisterValues {
                        holding_registers: vec![(7, 42)],
                        ..Default::default()
                    },
                }],
                scan_groups: vec![],
                default_slave_id: 1,
                timeout_ms: 3000,
                requests: RequestSettings::default(),
                reconnect_policy: ReconnectPolicy::default(),
                socks5: Socks5Config::default(),
            }],
        };

        let json = serde_json::to_string(&project).unwrap();
        let loaded: ProjectFile = serde_json::from_str(&json).unwrap();
        match &loaded.connections[0].transport {
            TransportConfig::TcpTls {
                client_tls: loaded_client,
                server_tls: loaded_server,
                ..
            } => {
                assert_eq!(loaded_client.as_ref(), &client_tls);
                assert_eq!(loaded_server.as_ref(), &server_tls);
            }
            _ => panic!("wrong transport variant"),
        }
        let device = &loaded.connections[0].devices[0];
        assert_eq!(device.values.holding_registers, vec![(7, 42)]);
        assert!(device.register_defs[0].data_source.is_some());
    }
}
