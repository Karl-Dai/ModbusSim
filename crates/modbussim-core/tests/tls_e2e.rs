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
