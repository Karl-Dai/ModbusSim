use modbussim_core::master::{MasterConfig, MasterConnection, ReadFunction, ReadResult};
use modbussim_core::slave::{SlaveConnection, SlaveDevice};
use modbussim_core::transport::{SlaveTlsConfig, TlsConfig, Transport};
use std::io::Write;
use tempfile::NamedTempFile;

// ---------------------------------------------------------------------------
// Certificate generation helpers
//
// Uses openssl exclusively (no rcgen) to avoid macOS Security.framework
// rejecting rcgen's ECDSA keys.  Certs are RSA 2048-bit, validity <= 820 days
// (macOS enforces a hard limit of 825 days for TLS leaf certs), and include
// the serverAuth EKU required by macOS.
// ---------------------------------------------------------------------------

/// Generate a CA key+cert.
/// Returns (ca_pem_bytes, ca_x509, ca_pkey).
fn gen_ca() -> (
    Vec<u8>,
    openssl::x509::X509,
    openssl::pkey::PKey<openssl::pkey::Private>,
) {
    use openssl::asn1::Asn1Time;
    use openssl::bn::{BigNum, MsbOption};
    use openssl::hash::MessageDigest;
    use openssl::pkey::PKey;
    use openssl::rsa::Rsa;
    use openssl::x509::extension::BasicConstraints;
    use openssl::x509::{X509Builder, X509NameBuilder};

    let rsa = Rsa::generate(2048).expect("RSA generate CA");
    let pkey = PKey::from_rsa(rsa).expect("PKey from RSA CA");

    let mut serial = BigNum::new().unwrap();
    serial.rand(128, MsbOption::MAYBE_ZERO, false).unwrap();
    let serial = serial.to_asn1_integer().unwrap();

    let mut name_builder = X509NameBuilder::new().unwrap();
    name_builder
        .append_entry_by_text("CN", "ModbusSim Test CA")
        .unwrap();
    let name = name_builder.build();

    let mut builder = X509Builder::new().unwrap();
    builder.set_version(2).unwrap();
    builder.set_serial_number(&serial).unwrap();
    builder.set_subject_name(&name).unwrap();
    builder.set_issuer_name(&name).unwrap();
    builder.set_pubkey(&pkey).unwrap();
    builder
        .set_not_before(&Asn1Time::days_from_now(0).unwrap())
        .unwrap();
    // macOS enforces a maximum TLS cert validity of 825 days
    builder
        .set_not_after(&Asn1Time::days_from_now(820).unwrap())
        .unwrap();

    let bc = BasicConstraints::new().critical().ca().build().unwrap();
    builder.append_extension(bc).unwrap();

    builder.sign(&pkey, MessageDigest::sha256()).unwrap();
    let cert = builder.build();
    let ca_pem = cert.to_pem().unwrap();
    (ca_pem, cert, pkey)
}

/// Generate a leaf cert signed by a CA.
///
/// Each entry in `names` is treated as an IP if it parses as `IpAddr`, otherwise
/// as a DNS SAN.  The cert includes `serverAuth`+`clientAuth` EKU required by
/// macOS Security.framework for TLS.
fn gen_leaf_cert(
    names: &[&str],
    ca_cert: &openssl::x509::X509,
    ca_key: &openssl::pkey::PKey<openssl::pkey::Private>,
) -> (
    openssl::x509::X509,
    openssl::pkey::PKey<openssl::pkey::Private>,
) {
    use openssl::asn1::Asn1Time;
    use openssl::bn::{BigNum, MsbOption};
    use openssl::hash::MessageDigest;
    use openssl::pkey::PKey;
    use openssl::rsa::Rsa;
    use openssl::x509::extension::{ExtendedKeyUsage, SubjectAlternativeName};
    use openssl::x509::{X509Builder, X509NameBuilder};

    let rsa = Rsa::generate(2048).expect("RSA generate leaf");
    let pkey = PKey::from_rsa(rsa).expect("PKey from RSA leaf");

    let mut serial = BigNum::new().unwrap();
    serial.rand(128, MsbOption::MAYBE_ZERO, false).unwrap();
    let serial = serial.to_asn1_integer().unwrap();

    let mut name_builder = X509NameBuilder::new().unwrap();
    name_builder
        .append_entry_by_text("CN", names.first().copied().unwrap_or("localhost"))
        .unwrap();
    let subject_name = name_builder.build();

    let mut builder = X509Builder::new().unwrap();
    builder.set_version(2).unwrap();
    builder.set_serial_number(&serial).unwrap();
    builder.set_subject_name(&subject_name).unwrap();
    builder.set_issuer_name(ca_cert.subject_name()).unwrap();
    builder.set_pubkey(&pkey).unwrap();
    builder
        .set_not_before(&Asn1Time::days_from_now(0).unwrap())
        .unwrap();
    // macOS enforces a maximum TLS cert validity of 825 days
    builder
        .set_not_after(&Asn1Time::days_from_now(820).unwrap())
        .unwrap();

    // SAN extension (required for modern TLS)
    let mut san = SubjectAlternativeName::new();
    for n in names {
        if n.parse::<std::net::IpAddr>().is_ok() {
            san.ip(n);
        } else {
            san.dns(n);
        }
    }
    let san_ext = {
        let context = builder.x509v3_context(Some(ca_cert), None);
        san.build(&context).unwrap()
    };
    builder.append_extension(san_ext).unwrap();

    // EKU: serverAuth + clientAuth — required by macOS Security.framework
    let eku = ExtendedKeyUsage::new()
        .server_auth()
        .client_auth()
        .build()
        .unwrap();
    builder.append_extension(eku).unwrap();

    builder.sign(ca_key, MessageDigest::sha256()).unwrap();
    let cert = builder.build();
    (cert, pkey)
}

/// Build a PKCS#12 bundle from an X509 cert, private key, and CA cert.
fn make_pkcs12(
    cert: &openssl::x509::X509,
    pkey: &openssl::pkey::PKey<openssl::pkey::Private>,
    ca_cert: &openssl::x509::X509,
) -> Vec<u8> {
    use openssl::pkcs12::Pkcs12;
    use openssl::stack::Stack;
    use openssl::x509::X509;

    let mut ca_stack = Stack::<X509>::new().expect("Stack::new");
    ca_stack.push(ca_cert.to_owned()).expect("ca_stack.push");

    let pkcs12 = Pkcs12::builder()
        .pkey(pkey)
        .cert(cert)
        .ca(ca_stack)
        .build2("password")
        .expect("Pkcs12::build2");

    pkcs12.to_der().expect("Pkcs12::to_der")
}

/// Generate test CA + server cert.
/// Returns (ca_pem_bytes, server_p12_bytes).
fn generate_test_certs() -> (Vec<u8>, Vec<u8>) {
    let (ca_pem, ca_cert, ca_key) = gen_ca();
    let (server_cert, server_key) = gen_leaf_cert(&["localhost", "127.0.0.1"], &ca_cert, &ca_key);
    let server_p12 = make_pkcs12(&server_cert, &server_key, &ca_cert);
    (ca_pem, server_p12)
}

/// Write bytes to a temp file and return the NamedTempFile (keeps file alive).
fn write_temp_file(data: &[u8]) -> NamedTempFile {
    let mut f = NamedTempFile::new().expect("NamedTempFile::new");
    f.write_all(data).expect("write_all");
    f.flush().expect("flush");
    f
}

#[test]
fn test_slave_pkcs1_pem_identity_is_supported() {
    let (_, ca, ca_key) = gen_ca();
    let (cert, key) = gen_leaf_cert(&["localhost"], &ca, &ca_key);
    let cert_file = write_temp_file(&cert.to_pem().unwrap());
    let key_file = write_temp_file(&key.rsa().unwrap().private_key_to_pem().unwrap());
    let config = SlaveTlsConfig {
        cert_file: cert_file.path().to_string_lossy().into_owned(),
        key_file: key_file.path().to_string_lossy().into_owned(),
        ..Default::default()
    };
    assert!(
        modbussim_core::tls_slave::build_tls_acceptor(&config).is_ok(),
        "A PKCS#1 RSA server key must load without format conversion or Keychain"
    );
}

#[tokio::test]
async fn test_slave_missing_certificate_never_reports_running() {
    let mut slave = SlaveConnection::new(Transport::TcpTls {
        host: "127.0.0.1".into(),
        port: 0,
    });
    let result = slave.start().await;
    let state = slave.state();
    slave.stop().await.unwrap();
    assert!(result.is_err(), "TLS setup failure must reach the caller");
    assert_eq!(state, modbussim_core::slave::ConnectionState::Stopped);
}

#[tokio::test]
async fn test_slave_tls_bind_failure_can_be_retried() {
    use modbussim_core::slave::{ConnectionState, SlaveError};
    let (_, server_p12) = generate_test_certs();
    let p12_file = write_temp_file(&server_p12);
    let occupied = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let address = occupied.local_addr().unwrap();
    let mut slave = SlaveConnection::new(Transport::TcpTls {
        host: address.ip().to_string(),
        port: address.port(),
    });
    slave.tls_config = SlaveTlsConfig {
        pkcs12_file: p12_file.path().to_string_lossy().into_owned(),
        pkcs12_password: "password".into(),
        ..Default::default()
    };
    assert!(matches!(slave.start().await, Err(SlaveError::BindError(_))));
    assert_eq!(slave.state(), ConnectionState::Stopped);
    drop(occupied);

    slave.start().await.unwrap();
    assert_eq!(slave.state(), ConnectionState::Running);
    assert!(
        std::net::TcpListener::bind(address).is_err(),
        "Running must mean the socket is already bound"
    );
    slave.stop().await.unwrap();
    assert_eq!(slave.state(), ConnectionState::Stopped);
    assert!(
        std::net::TcpListener::bind(address).is_ok(),
        "Stop must release the listening socket"
    );
}

// ---------------------------------------------------------------------------
// Test 1: TLS read/write holding registers with full cert validation
// ---------------------------------------------------------------------------

#[tokio::test]
async fn test_tls_read_holding_registers() {
    let (ca_pem, server_p12) = generate_test_certs();

    // Temp files must stay alive for the duration of the test
    let server_p12_file = write_temp_file(&server_p12);
    let ca_pem_file = write_temp_file(&ca_pem);

    // Start TLS slave on port 15802
    let transport = Transport::TcpTls {
        host: "127.0.0.1".to_string(),
        port: 15802,
    };
    let mut slave = SlaveConnection::new(transport);
    slave.tls_config = SlaveTlsConfig {
        enabled: true,
        pkcs12_file: server_p12_file.path().to_string_lossy().into_owned(),
        pkcs12_password: "password".to_string(),
        ..Default::default()
    };
    let device = SlaveDevice::with_default_registers(1, "TLS Device", 10);
    slave.add_device(device).await.unwrap();
    let mut second = SlaveDevice::with_default_registers(2, "Second TLS device", 10);
    second.register_map.write_holding_register(0, 77);
    slave.add_device(second).await.unwrap();
    slave.start().await.unwrap();
    tokio::time::sleep(std::time::Duration::from_millis(200)).await;

    // Connect master with CA cert for full validation
    let tls_config = TlsConfig {
        enabled: true,
        ca_file: ca_pem_file.path().to_string_lossy().into_owned(),
        accept_invalid_certs: false,
        ..Default::default()
    };
    let master_config = MasterConfig {
        target_address: "127.0.0.1".to_string(),
        port: 15802,
        slave_id: 1,
        timeout_ms: 5000,
        tls: tls_config,
        ..Default::default()
    };
    let master_transport = Transport::TcpTls {
        host: "127.0.0.1".to_string(),
        port: 15802,
    };
    let mut master = MasterConnection::new(master_config, master_transport);
    master.connect().await.expect("master connect");

    // Read 5 holding registers — default-initialized to 0
    let result = master
        .read(ReadFunction::ReadHoldingRegisters, 0, 5)
        .await
        .expect("read holding registers");
    match result {
        ReadResult::HoldingRegisters(values) => {
            assert_eq!(
                values,
                vec![0u16, 0, 0, 0, 0],
                "expected zero-initialized registers"
            );
        }
        _ => panic!("unexpected result type"),
    }

    // Write single register (address 0, value 42)
    master
        .write_single_register(0, 42)
        .await
        .expect("write_single_register");

    // Read back — expect [42]
    let result = master
        .read(ReadFunction::ReadHoldingRegisters, 0, 1)
        .await
        .expect("read back after write");
    match result {
        ReadResult::HoldingRegisters(values) => {
            assert_eq!(values, vec![42u16], "expected written value 42");
        }
        _ => panic!("unexpected result type"),
    }

    // Exercise the same transport handle used by the application's two scans.
    use modbussim_core::master::{scan_registers_paced, scan_slave_ids_paced};
    use std::time::Duration;
    use tokio::sync::{mpsc, oneshot};
    for function in [
        ReadFunction::ReadCoils,
        ReadFunction::ReadDiscreteInputs,
        ReadFunction::ReadHoldingRegisters,
        ReadFunction::ReadInputRegisters,
    ] {
        let (_cancel, cancel_rx) = oneshot::channel();
        let (progress_tx, mut progress_rx) = mpsc::channel(16);
        let found = scan_registers_paced(
            master.get_scan_context().unwrap(),
            Some(1),
            function,
            0,
            4,
            2,
            Duration::from_secs(1),
            master.request_pacer(),
            cancel_rx,
            progress_tx,
        )
        .await;
        assert_eq!(
            found.iter().map(|r| r.address).collect::<Vec<_>>(),
            vec![0, 1, 2, 3, 4]
        );
        if matches!(function, ReadFunction::ReadHoldingRegisters) {
            assert_eq!(found[0].value, 42);
        }
        let mut progress = Vec::new();
        while let Ok(update) = progress_rx.try_recv() {
            progress.push(update);
        }
        assert!(progress.last().unwrap().done);
        assert_eq!(progress.last().unwrap().found_registers.len(), 5);
    }
    let (_cancel, cancel_rx) = oneshot::channel();
    let (progress_tx, _progress_rx) = mpsc::channel(16);
    let ids = scan_slave_ids_paced(
        master.get_scan_context().unwrap(),
        1,
        1,
        2,
        Duration::from_secs(1),
        master.request_pacer(),
        cancel_rx,
        progress_tx,
    )
    .await;
    assert_eq!(ids, vec![1, 2]);
    // Scanning another unit must not change the original connection's unit.
    match master
        .read(ReadFunction::ReadHoldingRegisters, 0, 1)
        .await
        .unwrap()
    {
        ReadResult::HoldingRegisters(values) => assert_eq!(values, vec![42]),
        value => panic!("Unexpected result: {value:?}"),
    }
    let (cancel_tx, cancel_rx) = oneshot::channel();
    cancel_tx.send(()).unwrap();
    let (progress_tx, mut progress_rx) = mpsc::channel(16);
    let found = scan_registers_paced(
        master.get_scan_context().unwrap(),
        Some(1),
        ReadFunction::ReadHoldingRegisters,
        0,
        4,
        2,
        Duration::from_secs(1),
        master.request_pacer(),
        cancel_rx,
        progress_tx,
    )
    .await;
    assert!(found.is_empty());
    assert!(progress_rx.recv().await.unwrap().cancelled);

    // All TLS write functions must honor an explicit unit and preserve default fallback.
    for (target, value) in [(Some(2), 22), (None, 11)] {
        master
            .write_single_coil_with_slave(target, 5, true)
            .await
            .unwrap();
        master
            .write_single_register_with_slave(target, 5, value)
            .await
            .unwrap();
        master
            .write_multiple_coils_with_slave(target, 7, &[true, false, true])
            .await
            .unwrap();
        master
            .write_multiple_registers_with_slave(target, 7, &[value; 3])
            .await
            .unwrap();
        let devices = slave.devices.read().await;
        for (slave_id, expected) in [(1, if target.is_some() { 0 } else { 11 }), (2, 22)] {
            let map = &devices.get(&slave_id).unwrap().register_map;
            assert_eq!(map.holding_registers.get(&5), Some(&expected));
            assert_eq!(map.coils.get(&5), Some(&(expected != 0)));
            for address in 7..10 {
                assert_eq!(map.holding_registers.get(&address), Some(&expected));
                assert_eq!(
                    map.coils.get(&address),
                    Some(&(expected != 0 && address != 8))
                );
            }
        }
    }
    assert_eq!(master.config.slave_id, 1);

    assert_eq!(slave.clients.list().len(), 1);
    assert!(slave.clients.list()[0]
        .peer_address
        .starts_with("127.0.0.1:"));
    master.disconnect().await.unwrap();
    tokio::time::timeout(std::time::Duration::from_secs(3), async {
        while !slave.clients.list().is_empty() {
            tokio::time::sleep(std::time::Duration::from_millis(10)).await;
        }
    })
    .await
    .expect("TLS client must disappear after disconnect");
    slave.stop().await.unwrap();
    assert!(slave.clients.list().is_empty());
}

// ---------------------------------------------------------------------------
// Test 2: TLS with accept_invalid_certs (skip cert validation)
// ---------------------------------------------------------------------------

#[tokio::test]
async fn test_tls_accept_invalid_certs() {
    let (_ca_pem, server_p12) = generate_test_certs();

    let server_p12_file = write_temp_file(&server_p12);

    // Start TLS slave on port 15803
    let transport = Transport::TcpTls {
        host: "127.0.0.1".to_string(),
        port: 15803,
    };
    let mut slave = SlaveConnection::new(transport);
    slave.tls_config = SlaveTlsConfig {
        enabled: true,
        pkcs12_file: server_p12_file.path().to_string_lossy().into_owned(),
        pkcs12_password: "password".to_string(),
        ..Default::default()
    };
    let device = SlaveDevice::with_default_registers(1, "TLS Device 2", 10);
    slave.add_device(device).await.unwrap();
    slave.start().await.unwrap();
    tokio::time::sleep(std::time::Duration::from_millis(200)).await;

    // Connect WITHOUT CA cert — accept_invalid_certs skips cert validation
    let tls_config = TlsConfig {
        enabled: true,
        accept_invalid_certs: true,
        ..Default::default()
    };
    let master_config = MasterConfig {
        target_address: "127.0.0.1".to_string(),
        port: 15803,
        slave_id: 1,
        timeout_ms: 5000,
        tls: tls_config,
        ..Default::default()
    };
    let master_transport = Transport::TcpTls {
        host: "127.0.0.1".to_string(),
        port: 15803,
    };
    let mut master = MasterConnection::new(master_config, master_transport);
    master
        .connect()
        .await
        .expect("master connect with accept_invalid_certs");

    // Read 1 holding register to verify connection works
    let result = master
        .read(ReadFunction::ReadHoldingRegisters, 0, 1)
        .await
        .expect("read holding register");
    match result {
        ReadResult::HoldingRegisters(values) => {
            assert_eq!(values.len(), 1, "expected 1 register");
        }
        _ => panic!("unexpected result type"),
    }

    assert_eq!(slave.clients.list().len(), 1);
    assert!(slave.clients.list()[0]
        .peer_address
        .starts_with("127.0.0.1:"));
    master.disconnect().await.unwrap();
    tokio::time::timeout(std::time::Duration::from_secs(3), async {
        while !slave.clients.list().is_empty() {
            tokio::time::sleep(std::time::Duration::from_millis(10)).await;
        }
    })
    .await
    .expect("TLS client must disappear after disconnect");
    slave.stop().await.unwrap();
    assert!(slave.clients.list().is_empty());
}

#[test]
fn test_pem_client_identity_loads_repeatedly() {
    let (ca_pem, ca_cert, ca_key) = gen_ca();
    let (client_cert, client_key) = gen_leaf_cert(&["modbus-test-client"], &ca_cert, &ca_key);
    let mut chain_pem = client_cert.to_pem().unwrap();
    chain_pem.extend_from_slice(&ca_pem);
    let cert_file = write_temp_file(&chain_pem);
    let key_file = write_temp_file(&client_key.private_key_to_pem_pkcs8().unwrap());
    let config = TlsConfig {
        enabled: true,
        cert_file: cert_file.path().to_string_lossy().into_owned(),
        key_file: key_file.path().to_string_lossy().into_owned(),
        ..Default::default()
    };

    // Keep all connectors alive: reconnects and separate connections may share
    // certificate files, and must not double-import the private key on macOS.
    let mut connectors = Vec::new();
    for _ in 0..3 {
        connectors.push(
            modbussim_core::tls_master::build_tls_connector(&config)
                .expect("PEM certificate chain and key must import separately"),
        );
    }
}

/// Reproduce an industrial endpoint with a v1 certificate: no EKU or SAN,
/// and a CN that does not match the connection's IP address.
#[tokio::test]
async fn test_legacy_certificate_requires_explicit_compatibility() {
    use openssl::{
        asn1::Asn1Time, hash::MessageDigest, pkey::PKey, rsa::Rsa, x509::X509NameBuilder,
    };
    use std::time::Duration;

    let (ca_pem, ca_cert, ca_key) = gen_ca();
    let key = PKey::from_rsa(Rsa::generate(2048).unwrap()).unwrap();
    let mut name = X509NameBuilder::new().unwrap();
    name.append_entry_by_text("CN", "Legacy Server").unwrap();
    let mut cert = openssl::x509::X509::builder().unwrap();
    cert.set_version(0).unwrap();
    cert.set_serial_number(
        &openssl::bn::BigNum::from_u32(1)
            .unwrap()
            .to_asn1_integer()
            .unwrap(),
    )
    .unwrap();
    cert.set_subject_name(&name.build()).unwrap();
    cert.set_issuer_name(ca_cert.subject_name()).unwrap();
    cert.set_pubkey(&key).unwrap();
    cert.set_not_before(&Asn1Time::days_from_now(0).unwrap())
        .unwrap();
    cert.set_not_after(&Asn1Time::days_from_now(365).unwrap())
        .unwrap();
    cert.sign(&ca_key, MessageDigest::sha256()).unwrap();
    let cert = cert.build();
    assert!(cert.subject_alt_names().is_none());
    let identity_file = write_temp_file(&make_pkcs12(&cert, &key, &ca_cert));
    let acceptor = modbussim_core::tls_slave::build_tls_acceptor(&SlaveTlsConfig {
        pkcs12_file: identity_file.path().to_string_lossy().into_owned(),
        pkcs12_password: "password".into(),
        ..Default::default()
    })
    .unwrap();
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let port = listener.local_addr().unwrap().port();
    let server = tokio::spawn(async move {
        let mut completed = 0;
        for _ in 0..2 {
            let (stream, _) = tokio::time::timeout(Duration::from_secs(10), listener.accept())
                .await
                .unwrap()
                .unwrap();
            let acceptor = acceptor.clone();
            completed += tokio::task::spawn_blocking(move || {
                let stream = stream.into_std().unwrap();
                stream.set_nonblocking(false).unwrap();
                stream
                    .set_read_timeout(Some(Duration::from_secs(5)))
                    .unwrap();
                stream
                    .set_write_timeout(Some(Duration::from_secs(5)))
                    .unwrap();
                let Ok(mut stream) = acceptor.accept(stream) else {
                    return 0;
                };
                let Ok((header, request)) = modbussim_core::mbap::read_frame(&mut stream) else {
                    return 0;
                };
                assert_eq!(request, [3, 0, 0, 0, 1]);
                modbussim_core::mbap::write_frame(
                    &mut stream,
                    header.transaction_id,
                    header.unit_id,
                    &[3, 2, 0, 42],
                )
                .unwrap();
                1
            })
            .await
            .unwrap();
        }
        completed
    });

    let ca_file = write_temp_file(&ca_pem);
    let mut config = MasterConfig {
        target_address: "127.0.0.1".into(),
        port,
        tls: TlsConfig {
            enabled: true,
            ca_file: ca_file.path().to_string_lossy().into_owned(),
            ..Default::default()
        },
        ..Default::default()
    };
    assert!(!config.tls.accept_invalid_certs);
    let transport = Transport::TcpTls {
        host: "127.0.0.1".into(),
        port,
    };
    let mut strict = MasterConnection::new(config.clone(), transport.clone());
    assert!(
        strict.connect().await.is_err(),
        "Strict mode must reject the legacy certificate"
    );

    config.tls.accept_invalid_certs = true;
    let saved = serde_json::to_string(&config.tls).unwrap();
    assert!(
        serde_json::from_str::<TlsConfig>(&saved)
            .unwrap()
            .accept_invalid_certs
    );
    let mut compatible = MasterConnection::new(config, transport);
    compatible
        .connect()
        .await
        .expect("Explicit compatibility should allow the legacy endpoint");
    match compatible
        .read(ReadFunction::ReadHoldingRegisters, 0, 1)
        .await
        .unwrap()
    {
        ReadResult::HoldingRegisters(values) => assert_eq!(values, vec![42]),
        result => panic!("Unexpected read result: {result:?}"),
    }
    compatible.disconnect().await.unwrap();
    assert_eq!(server.await.unwrap(), 1);
}

// Exercise the production slave handshake and Modbus handler with an in-memory
// client too: these tests must not import any private key into macOS Keychain.
async fn exchange_with_tls_slave(
    config: &SlaveTlsConfig,
    client: openssl::ssl::SslConnector,
) -> Result<(), String> {
    use modbussim_core::{clients::ConnectedClients, mbap, tls_slave};
    use std::{sync::Arc, time::Duration};

    let acceptor = tls_slave::build_tls_acceptor(config)?;
    // Retain the socket throughout setup; there is no free-port/rebind race.
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let devices = Arc::new(tokio::sync::RwLock::new(std::collections::HashMap::from([
        (1, SlaveDevice::with_default_registers(1, "mTLS", 10)),
    ])));
    let clients = ConnectedClients::default();
    let (shutdown_tx, shutdown_rx) = tokio::sync::oneshot::channel();
    let server = tokio::spawn(tls_slave::run_tls_slave(
        listener,
        acceptor,
        devices,
        None,
        None,
        clients.clone(),
        shutdown_rx,
    ));
    let result = tokio::task::spawn_blocking(move || -> Result<(), String> {
        let socket = std::net::TcpStream::connect_timeout(&addr, Duration::from_secs(3))
            .map_err(|e| e.to_string())?;
        socket
            .set_read_timeout(Some(Duration::from_secs(3)))
            .unwrap();
        socket
            .set_write_timeout(Some(Duration::from_secs(3)))
            .unwrap();
        let mut stream = client
            .connect("localhost", socket)
            .map_err(|e| e.to_string())?;
        // TLS 1.3 can report certificate rejection on the first read, after the
        // client connect() returned. Require a real response to count as success.
        mbap::write_frame(&mut stream, 1, 1, &[6, 0, 0, 0, 42]).map_err(|e| e.to_string())?;
        let (header, response) = mbap::read_frame(&mut stream).map_err(|e| e.to_string())?;
        assert_eq!(header.transaction_id, 1);
        assert_eq!(response, [6, 0, 0, 0, 42]);
        mbap::write_frame(&mut stream, 2, 1, &[3, 0, 0, 0, 1]).map_err(|e| e.to_string())?;
        let (_, response) = mbap::read_frame(&mut stream).map_err(|e| e.to_string())?;
        assert_eq!(response, [3, 2, 0, 42]);
        let _ = stream.shutdown();
        Ok(())
    })
    .await
    .unwrap();
    clients.close_all();
    shutdown_tx.send(()).unwrap();
    tokio::time::timeout(Duration::from_secs(3), server)
        .await
        .unwrap()
        .unwrap()
        .unwrap();
    result
}

fn in_memory_client(
    ca: &openssl::x509::X509,
    identity: Option<(
        &openssl::x509::X509,
        &openssl::pkey::PKey<openssl::pkey::Private>,
    )>,
    version: openssl::ssl::SslVersion,
) -> openssl::ssl::SslConnector {
    use openssl::ssl::{SslConnector, SslMethod};
    let mut client = SslConnector::builder(SslMethod::tls_client()).unwrap();
    client.cert_store_mut().add_cert(ca.to_owned()).unwrap();
    client.set_min_proto_version(Some(version)).unwrap();
    client.set_max_proto_version(Some(version)).unwrap();
    if let Some((cert, key)) = identity {
        client.set_certificate(cert).unwrap();
        client.set_private_key(key).unwrap();
    }
    client.build()
}

#[tokio::test]
async fn test_slave_pem_and_pkcs12_restart_without_keychain() {
    use openssl::ssl::SslVersion;
    let (ca_pem, ca_cert, ca_key) = gen_ca();
    let (cert, key) = gen_leaf_cert(&["localhost"], &ca_cert, &ca_key);
    let mut chain = cert.to_pem().unwrap();
    chain.extend_from_slice(&ca_pem);
    let cert_file = write_temp_file(&chain);
    let key_file = write_temp_file(&key.private_key_to_pem_pkcs8().unwrap());
    let p12_file = write_temp_file(&make_pkcs12(&cert, &key, &ca_cert));
    let pkcs1_file = write_temp_file(&key.rsa().unwrap().private_key_to_pem().unwrap());
    for config in [
        SlaveTlsConfig {
            cert_file: cert_file.path().to_string_lossy().into_owned(),
            key_file: key_file.path().to_string_lossy().into_owned(),
            ..Default::default()
        },
        SlaveTlsConfig {
            cert_file: cert_file.path().to_string_lossy().into_owned(),
            key_file: pkcs1_file.path().to_string_lossy().into_owned(),
            ..Default::default()
        },
        SlaveTlsConfig {
            pkcs12_file: p12_file.path().to_string_lossy().into_owned(),
            pkcs12_password: "password".into(),
            ..Default::default()
        },
    ] {
        for _ in 0..3 {
            exchange_with_tls_slave(
                &config,
                in_memory_client(&ca_cert, None, SslVersion::TLS1_2),
            )
            .await
            .expect("Repeated slave starts must work without Keychain");
        }
    }
}

#[tokio::test]
async fn test_slave_legacy_pkcs12_without_keychain() {
    use openssl::{nid::Nid, pkcs12::Pkcs12, provider::Provider, ssl::SslVersion};
    let (_, ca, ca_key) = gen_ca();
    let (cert, key) = gen_leaf_cert(&["localhost"], &ca, &ca_key);
    let p12 = {
        let _legacy = Provider::try_load(None, "legacy", true).unwrap();
        Pkcs12::builder()
            .cert(&cert)
            .pkey(&key)
            .key_algorithm(Nid::PBE_WITHSHA1AND3_KEY_TRIPLEDES_CBC)
            .cert_algorithm(Nid::PBE_WITHSHA1AND40BITRC2_CBC)
            .build2("password")
            .unwrap()
            .to_der()
            .unwrap()
    };
    let p12_file = write_temp_file(&p12);
    let config = SlaveTlsConfig {
        pkcs12_file: p12_file.path().to_string_lossy().into_owned(),
        pkcs12_password: "password".into(),
        ..Default::default()
    };
    exchange_with_tls_slave(&config, in_memory_client(&ca, None, SslVersion::TLS1_2))
        .await
        .expect("Legacy P12 exports must still work without Keychain");
}

#[tokio::test]
async fn test_slave_mutual_tls_requires_trusted_client_certificate() {
    use openssl::ssl::SslVersion;
    let (ca_pem, ca_cert, ca_key) = gen_ca();
    let (server_cert, server_key) = gen_leaf_cert(&["localhost"], &ca_cert, &ca_key);
    let (client_cert, client_key) = gen_leaf_cert(&["client"], &ca_cert, &ca_key);
    let (_, rogue_ca, rogue_ca_key) = gen_ca();
    let (rogue_cert, rogue_key) = gen_leaf_cert(&["rogue"], &rogue_ca, &rogue_ca_key);
    let cert_file = write_temp_file(&server_cert.to_pem().unwrap());
    let key_file = write_temp_file(&server_key.private_key_to_pem_pkcs8().unwrap());
    let ca_file = write_temp_file(&ca_pem);
    let config = SlaveTlsConfig {
        cert_file: cert_file.path().to_string_lossy().into_owned(),
        key_file: key_file.path().to_string_lossy().into_owned(),
        ca_file: ca_file.path().to_string_lossy().into_owned(),
        require_client_cert: true,
        ..Default::default()
    };
    for version in [SslVersion::TLS1_2, SslVersion::TLS1_3] {
        exchange_with_tls_slave(
            &config,
            in_memory_client(&ca_cert, Some((&client_cert, &client_key)), version),
        )
        .await
        .expect("A trusted client must read and write registers");
        assert!(
            exchange_with_tls_slave(&config, in_memory_client(&ca_cert, None, version))
                .await
                .is_err(),
            "mTLS must reject clients without certificates"
        );
        assert!(
            exchange_with_tls_slave(
                &config,
                in_memory_client(&ca_cert, Some((&rogue_cert, &rogue_key)), version)
            )
            .await
            .is_err(),
            "mTLS must reject certificates signed by a different CA"
        );
    }
}

#[tokio::test]
async fn test_slave_bad_tls_configuration_fails_before_running() {
    use modbussim_core::{slave::SlaveError, tls_slave::build_tls_acceptor};
    let (_, ca, ca_key) = gen_ca();
    let (cert, key) = gen_leaf_cert(&["localhost"], &ca, &ca_key);
    let cert_file = write_temp_file(&cert.to_pem().unwrap());
    let key_file = write_temp_file(&key.private_key_to_pem_pkcs8().unwrap());
    let p12_file = write_temp_file(&make_pkcs12(&cert, &key, &ca));
    let config = SlaveTlsConfig {
        cert_file: cert_file.path().to_string_lossy().into_owned(),
        key_file: key_file.path().to_string_lossy().into_owned(),
        require_client_cert: true,
        ..Default::default()
    };
    let mut slave = SlaveConnection::new(Transport::TcpTls {
        host: "127.0.0.1".into(),
        port: 0,
    });
    slave.tls_config = config.clone();
    assert!(
        matches!(slave.start().await, Err(SlaveError::CertError(e)) if e.contains("requires a CA file"))
    );
    assert_eq!(
        slave.state(),
        modbussim_core::slave::ConnectionState::Stopped
    );
    let empty_ca = write_temp_file(b"");
    slave.tls_config.ca_file = empty_ca.path().to_string_lossy().into_owned();
    assert!(
        matches!(slave.start().await, Err(SlaveError::CertError(e)) if e.contains("no certificates"))
    );
    assert_eq!(
        slave.state(),
        modbussim_core::slave::ConnectionState::Stopped
    );

    let wrong_key_file = write_temp_file(&ca_key.private_key_to_pem_pkcs8().unwrap());
    assert!(
        build_tls_acceptor(&SlaveTlsConfig {
            key_file: wrong_key_file.path().to_string_lossy().into_owned(),
            require_client_cert: false,
            ..config.clone()
        })
        .is_err(),
        "Mismatched certificate/key must fail during setup"
    );
    assert!(
        build_tls_acceptor(&SlaveTlsConfig {
            pkcs12_file: p12_file.path().to_string_lossy().into_owned(),
            pkcs12_password: "wrong-password".into(),
            ..Default::default()
        })
        .is_err(),
        "Wrong P12 password must fail instead of prompting"
    );
    let encrypted_key = write_temp_file(
        &key.private_key_to_pem_pkcs8_passphrase(openssl::symm::Cipher::aes_256_cbc(), b"secret")
            .unwrap(),
    );
    assert!(
        build_tls_acceptor(&SlaveTlsConfig {
            key_file: encrypted_key.path().to_string_lossy().into_owned(),
            require_client_cert: false,
            ..config
        })
        .is_err(),
        "Encrypted PEM must fail without an interactive password callback"
    );
}
