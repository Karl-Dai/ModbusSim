use modbussim_core::master::{
    scan_registers_paced, MasterConfig, MasterConnection, MasterError, PollEvent, ReadFunction,
    ReadResult, ScanGroup,
};
use modbussim_core::project::ProjectFile;
use modbussim_core::request::RequestSettings;
use modbussim_core::transport::Transport;
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;
use tokio::sync::{mpsc, oneshot};
use tokio::time::{timeout, Instant};

#[derive(Debug, Clone)]
struct Packet {
    unit: u8,
    function: u8,
    address: u16,
    quantity: u16,
    received: Instant,
    response_sent: Instant,
}

struct Server {
    port: u16,
    packets: Arc<Mutex<Vec<Packet>>>,
    task: tokio::task::JoinHandle<()>,
}

impl Drop for Server {
    fn drop(&mut self) {
        self.task.abort();
    }
}

impl Server {
    async fn start(fail_at: Option<u16>, rtu: bool) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();
        let packets = Arc::new(Mutex::new(Vec::new()));
        let recorded = packets.clone();
        let task = tokio::spawn(async move {
            let (mut stream, _) = listener.accept().await.unwrap();
            loop {
                let (unit, pdu, header) = if rtu {
                    let mut frame = [0u8; 8];
                    if stream.read_exact(&mut frame).await.is_err() {
                        break;
                    }
                    assert_eq!(
                        modbussim_core::tools::crc16(&frame[..6]),
                        u16::from_le_bytes([frame[6], frame[7]])
                    );
                    (frame[0], frame[1..6].to_vec(), [0u8; 7])
                } else {
                    let mut header = [0u8; 7];
                    if stream.read_exact(&mut header).await.is_err() {
                        break;
                    }
                    let length = u16::from_be_bytes([header[4], header[5]]) as usize;
                    let mut pdu = vec![0; length - 1];
                    stream.read_exact(&mut pdu).await.unwrap();
                    (header[6], pdu, header)
                };
                let received = Instant::now();
                let function = pdu[0];
                let address = u16::from_be_bytes([pdu[1], pdu[2]]);
                let quantity = u16::from_be_bytes([pdu[3], pdu[4]]);
                let response = if fail_at.is_some_and(|limit| address >= limit) {
                    vec![function | 0x80, 2]
                } else if function == 1 || function == 2 {
                    let mut response = vec![function, quantity.div_ceil(8) as u8];
                    response.resize(2 + usize::from(quantity.div_ceil(8)), 0);
                    for i in 0..quantity {
                        if (address + i) % 3 == 0 {
                            response[2 + usize::from(i / 8)] |= 1 << (i % 8);
                        }
                    }
                    response
                } else if function == 3 || function == 4 {
                    let mut response = vec![function, (quantity * 2) as u8];
                    for i in 0..quantity {
                        response.extend_from_slice(
                            &(address + i)
                                .wrapping_add(u16::from(unit) * 1000)
                                .to_be_bytes(),
                        );
                    }
                    response
                } else {
                    pdu[..5].to_vec()
                };
                // Include server processing time to distinguish response-to-request gaps from start-to-start spacing.
                tokio::time::sleep(Duration::from_millis(10)).await;
                let response_sent = Instant::now();
                if rtu {
                    let mut frame = vec![unit];
                    frame.extend_from_slice(&response);
                    frame.extend_from_slice(&modbussim_core::tools::crc16(&frame).to_le_bytes());
                    stream.write_all(&frame).await.unwrap();
                } else {
                    let mut header = header;
                    header[4..6].copy_from_slice(&((response.len() + 1) as u16).to_be_bytes());
                    let mut frame = header.to_vec();
                    frame.extend_from_slice(&response);
                    stream.write_all(&frame).await.unwrap();
                }
                recorded.lock().unwrap().push(Packet {
                    unit,
                    function,
                    address,
                    quantity,
                    received,
                    response_sent,
                });
            }
        });
        Self {
            port,
            packets,
            task,
        }
    }

    async fn master(&self, requests: RequestSettings, rtu: bool) -> MasterConnection {
        let config = MasterConfig {
            port: self.port,
            requests,
            ..Default::default()
        };
        let transport = if rtu {
            Transport::RtuOverTcp {
                host: "127.0.0.1".into(),
                port: self.port,
            }
        } else {
            Transport::Tcp {
                host: "127.0.0.1".into(),
                port: self.port,
            }
        };
        let mut master = MasterConnection::new(config, transport);
        master.connect().await.unwrap();
        master
    }
}

fn values(result: ReadResult) -> Vec<u16> {
    match result {
        ReadResult::HoldingRegisters(v) | ReadResult::InputRegisters(v) => v,
        _ => panic!("expected registers"),
    }
}

fn group(id: &str, unit: Option<u8>, quantity: u16) -> ScanGroup {
    ScanGroup {
        id: id.into(),
        name: id.into(),
        function: ReadFunction::ReadHoldingRegisters,
        start_address: 0,
        quantity,
        interval_ms: 60_000,
        enabled: true,
        slave_id: unit,
    }
}

#[tokio::test]
async fn continuous_registers_split_on_wire_and_reassemble_in_order() {
    let server = Server::start(None, false).await;
    let mut master = server
        .master(
            RequestSettings {
                max_read_registers: 40,
                ..Default::default()
            },
            false,
        )
        .await;
    assert_eq!(
        values(
            master
                .read(ReadFunction::ReadHoldingRegisters, 10, 100)
                .await
                .unwrap()
        ),
        (1010..1110).collect::<Vec<_>>()
    );
    let packets = server.packets.lock().unwrap().clone();
    assert_eq!(
        packets
            .iter()
            .map(|p| (p.function, p.address, p.quantity))
            .collect::<Vec<_>>(),
        vec![(3, 10, 40), (3, 50, 40), (3, 90, 20)]
    );
    master.disconnect().await.unwrap();
}

#[tokio::test]
async fn bit_batches_discard_each_packets_padding_on_tcp_and_rtu_over_tcp() {
    for rtu in [false, true] {
        let server = Server::start(None, rtu).await;
        let mut master = server
            .master(
                RequestSettings {
                    max_read_bits: 9,
                    ..Default::default()
                },
                rtu,
            )
            .await;
        for function in [ReadFunction::ReadCoils, ReadFunction::ReadDiscreteInputs] {
            let result = master.read(function, 3, 23).await.unwrap();
            let bits = match result {
                ReadResult::Coils(v) | ReadResult::DiscreteInputs(v) => v,
                _ => panic!(),
            };
            assert_eq!(
                bits,
                (3..26).map(|address| address % 3 == 0).collect::<Vec<_>>()
            );
        }
        let packets = server.packets.lock().unwrap().clone();
        assert_eq!(
            packets.iter().map(|p| p.quantity).collect::<Vec<_>>(),
            vec![9, 9, 5, 9, 9, 5]
        );
        master.disconnect().await.unwrap();
    }
}

#[tokio::test]
async fn one_connection_paces_polling_manual_writes_and_discovery_and_keeps_unit_ids() {
    let server = Server::start(None, false).await;
    let mut master = server
        .master(
            RequestSettings {
                interval_ms: 35,
                max_read_registers: 2,
                ..Default::default()
            },
            false,
        )
        .await;
    let mut first = master
        .start_scan_group(&group("override", Some(2), 4))
        .await
        .unwrap();
    let mut second = master
        .start_scan_group(&group("default", None, 4))
        .await
        .unwrap();
    let (_cancel, cancel_rx) = oneshot::channel();
    let (progress_tx, _progress_rx) = mpsc::channel(8);
    let scan = scan_registers_paced(
        master.get_ctx_handle().unwrap(),
        Some(3),
        ReadFunction::ReadHoldingRegisters,
        20,
        21,
        1,
        Duration::from_secs(1),
        master.request_pacer(),
        cancel_rx,
        progress_tx,
    );
    let (write, found) = tokio::join!(master.write_single_register(30, 7), scan);
    write.unwrap();
    assert_eq!(
        found.iter().map(|r| r.value).collect::<Vec<_>>(),
        vec![3020, 3021]
    );
    for (receiver, expected) in [(&mut first, 2000), (&mut second, 1000)] {
        match timeout(Duration::from_secs(3), receiver.recv())
            .await
            .unwrap()
            .unwrap()
        {
            PollEvent::Data(data) => {
                assert_eq!(values(data), (expected..expected + 4).collect::<Vec<_>>())
            }
            other => panic!("{other:?}"),
        }
    }
    assert_eq!(
        values(
            master
                .read(ReadFunction::ReadHoldingRegisters, 0, 1)
                .await
                .unwrap()
        ),
        vec![1000]
    );
    let packets = server.packets.lock().unwrap().clone();
    assert_eq!(packets.len(), 8);
    assert_eq!(packets.iter().find(|p| p.function == 6).unwrap().unit, 1);
    for pair in packets.windows(2) {
        assert!(
            pair[1].received.duration_since(pair[0].response_sent) >= Duration::from_millis(35),
            "gap violated: {pair:?}"
        );
    }
    master.stop_all_scans().await;
    master.disconnect().await.unwrap();
}

#[tokio::test]
async fn cancellation_interrupts_long_gap_between_batch_requests() {
    let server = Server::start(None, false).await;
    let mut master = server
        .master(
            RequestSettings {
                interval_ms: 60_000,
                max_read_registers: 1,
                ..Default::default()
            },
            false,
        )
        .await;
    let _events = master
        .start_scan_group(&group("large", None, 100))
        .await
        .unwrap();
    timeout(Duration::from_secs(2), async {
        while server.packets.lock().unwrap().is_empty() {
            tokio::time::sleep(Duration::from_millis(5)).await;
        }
    })
    .await
    .unwrap();
    timeout(Duration::from_millis(500), master.stop_scan_group("large"))
        .await
        .unwrap()
        .unwrap();
    assert_eq!(server.packets.lock().unwrap().len(), 1);
    master.disconnect().await.unwrap();
}

#[tokio::test]
async fn invalid_ranges_never_send_and_partial_batch_failure_never_returns_data() {
    let server = Server::start(Some(4), false).await;
    let mut master = server
        .master(
            RequestSettings {
                max_read_registers: 2,
                ..Default::default()
            },
            false,
        )
        .await;
    assert!(matches!(
        master
            .read(ReadFunction::ReadHoldingRegisters, 65535, 2)
            .await,
        Err(MasterError::InvalidConfig(_))
    ));
    assert!(matches!(
        master.read(ReadFunction::ReadHoldingRegisters, 0, 0).await,
        Err(MasterError::InvalidConfig(_))
    ));
    assert!(server.packets.lock().unwrap().is_empty());
    assert!(matches!(
        master.read(ReadFunction::ReadHoldingRegisters, 0, 10).await,
        Err(MasterError::Exception(_))
    ));
    assert_eq!(
        server
            .packets
            .lock()
            .unwrap()
            .iter()
            .map(|p| p.address)
            .collect::<Vec<_>>(),
        vec![0, 2, 4]
    );
    master.disconnect().await.unwrap();
}

#[tokio::test]
async fn last_modbus_address_can_be_read_and_discovered_without_overflow() {
    let server = Server::start(None, false).await;
    let mut master = server
        .master(
            RequestSettings {
                max_read_registers: 1,
                ..Default::default()
            },
            false,
        )
        .await;
    assert_eq!(
        values(
            master
                .read(ReadFunction::ReadHoldingRegisters, 65534, 2)
                .await
                .unwrap()
        ),
        vec![998, 999]
    );
    let (_cancel, cancel_rx) = oneshot::channel();
    let (progress_tx, _progress_rx) = mpsc::channel(8);
    let found = scan_registers_paced(
        master.get_ctx_handle().unwrap(),
        Some(1),
        ReadFunction::ReadHoldingRegisters,
        65535,
        65535,
        1,
        Duration::from_secs(1),
        master.request_pacer(),
        cancel_rx,
        progress_tx,
    )
    .await;
    assert_eq!(found.len(), 1);
    assert_eq!(found[0].address, 65535);
    master.disconnect().await.unwrap();
}

#[test]
fn old_projects_use_existing_defaults_and_new_projects_preserve_limits() {
    let old = r#"{"version":1,"type":"master","connections":[{"id":"m1","name":"PLC","transport":{"type":"tcp","host":"localhost","port":502}}]}"#;
    let mut project: ProjectFile = serde_json::from_str(old).unwrap();
    assert_eq!(project.connections[0].requests, RequestSettings::default());
    project.connections[0].requests = RequestSettings {
        interval_ms: 80,
        max_read_registers: 32,
        max_read_bits: 128,
    };
    project.connections[0].reconnect_policy.max_attempts = Some(5);
    let encoded = serde_json::to_string(&project).unwrap();
    let loaded: ProjectFile = serde_json::from_str(&encoded).unwrap();
    assert_eq!(
        loaded.connections[0].requests,
        project.connections[0].requests
    );
    assert_eq!(loaded.connections[0].reconnect_policy.max_attempts, Some(5));
}

#[tokio::test]
async fn cancelling_discovery_while_waiting_for_gap_emits_terminal_progress() {
    let server = Server::start(None, false).await;
    let mut master = server
        .master(
            RequestSettings {
                interval_ms: 60_000,
                ..Default::default()
            },
            false,
        )
        .await;
    master
        .read(ReadFunction::ReadHoldingRegisters, 0, 1)
        .await
        .unwrap();
    let (cancel_tx, cancel_rx) = oneshot::channel();
    let (progress_tx, mut progress_rx) = mpsc::channel(8);
    let scan = tokio::spawn(scan_registers_paced(
        master.get_ctx_handle().unwrap(),
        Some(1),
        ReadFunction::ReadHoldingRegisters,
        0,
        9,
        1,
        Duration::from_secs(1),
        master.request_pacer(),
        cancel_rx,
        progress_tx,
    ));
    tokio::task::yield_now().await;
    cancel_tx.send(()).unwrap();
    timeout(Duration::from_millis(500), scan)
        .await
        .unwrap()
        .unwrap();
    assert!(progress_rx.recv().await.unwrap().cancelled);
    assert_eq!(server.packets.lock().unwrap().len(), 1);
    master.disconnect().await.unwrap();
}
