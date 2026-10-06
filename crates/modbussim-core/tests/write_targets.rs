use modbussim_core::log_collector::LogCollector;
use modbussim_core::log_entry::FunctionCode;
use modbussim_core::master::{
    MasterConfig, MasterConnection, MasterError, PollEvent, ReadFunction, ReadResult, ScanGroup,
};
use modbussim_core::slave::{SlaveConnection, SlaveDevice};
use modbussim_core::transport::Transport;
use std::sync::Arc;
use std::time::Duration;

async fn assert_written(slave: &SlaveConnection, slave_id: u8, coil: bool, value: u16) {
    let devices = slave.devices.read().await;
    let map = &devices.get(&slave_id).unwrap().register_map;
    assert_eq!(map.coils.get(&10), Some(&coil));
    assert_eq!(map.holding_registers.get(&10), Some(&value));
    for address in 20..23 {
        assert_eq!(map.coils.get(&address), Some(&coil));
        assert_eq!(map.holding_registers.get(&address), Some(&value));
    }
}

async fn exercise_write_targets(rtu: bool) {
    let reservation = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let port = reservation.local_addr().unwrap().port();
    drop(reservation);
    let transport = if rtu {
        Transport::RtuOverTcp {
            host: "127.0.0.1".into(),
            port,
        }
    } else {
        Transport::Tcp {
            host: "127.0.0.1".into(),
            port,
        }
    };
    let mut slave = SlaveConnection::new(transport.clone());
    for slave_id in [1, 2] {
        let mut device = SlaveDevice::with_default_registers(slave_id, "Write target", 30);
        device
            .register_map
            .write_holding_register(0, u16::from(slave_id) * 1000);
        slave.add_device(device).await.unwrap();
    }
    slave.start().await.unwrap();
    let collector = Arc::new(LogCollector::new());
    let mut master = MasterConnection::new(
        MasterConfig {
            port,
            slave_id: 1,
            ..Default::default()
        },
        transport,
    )
    .with_log_collector(collector.clone());
    tokio::time::timeout(Duration::from_secs(3), async {
        loop {
            if master.connect().await.is_ok() {
                break;
            }
            tokio::time::sleep(Duration::from_millis(10)).await;
        }
    })
    .await
    .expect("slave must become ready");
    let group = ScanGroup {
        id: "device-2".into(),
        name: "Device 2".into(),
        function: ReadFunction::ReadHoldingRegisters,
        start_address: 0,
        quantity: 1,
        interval_ms: 1,
        enabled: true,
        slave_id: Some(2),
    };
    let mut polls = master.start_scan_group(&group).await.unwrap();
    let event = tokio::time::timeout(Duration::from_secs(3), polls.recv())
        .await
        .unwrap()
        .unwrap();
    assert!(matches!(
        event,
        PollEvent::Data(ReadResult::HoldingRegisters(values)) if values == vec![2000]
    ));

    // The selected group is device 2 while the connection still defaults to device 1.
    master
        .write_single_coil_with_slave(group.slave_id, 10, true)
        .await
        .unwrap();
    master
        .write_single_register_with_slave(group.slave_id, 10, 42)
        .await
        .unwrap();
    master
        .write_multiple_coils_with_slave(group.slave_id, 20, &[true; 3])
        .await
        .unwrap();
    master
        .write_multiple_registers_with_slave(group.slave_id, 20, &[42; 3])
        .await
        .unwrap();
    assert_written(&slave, 2, true, 42).await;
    assert_written(&slave, 1, false, 0).await;
    assert_eq!(master.config.slave_id, 1);

    // Missing/null command targets must reset the shared TCP context to the default.
    master
        .write_single_coil_with_slave(None, 10, true)
        .await
        .unwrap();
    master
        .write_single_register_with_slave(None, 10, 99)
        .await
        .unwrap();
    master
        .write_multiple_coils_with_slave(None, 20, &[true; 3])
        .await
        .unwrap();
    master
        .write_multiple_registers_with_slave(None, 20, &[99; 3])
        .await
        .unwrap();
    assert_written(&slave, 1, true, 99).await;
    assert_written(&slave, 2, true, 42).await;

    // The original public methods remain source-compatible and retain their default target.
    master.write_single_coil(10, false).await.unwrap();
    master.write_single_register(10, 77).await.unwrap();
    master.write_multiple_coils(20, &[false; 3]).await.unwrap();
    master.write_multiple_registers(20, &[77; 3]).await.unwrap();
    assert_written(&slave, 1, false, 77).await;
    assert_written(&slave, 2, true, 42).await;

    // Reject broadcast/reserved overrides before sending anything, for every write function.
    for slave_id in [0, 248, 255] {
        let results = [
            master
                .write_single_coil_with_slave(Some(slave_id), 10, true)
                .await,
            master
                .write_single_register_with_slave(Some(slave_id), 10, 999)
                .await,
            master
                .write_multiple_coils_with_slave(Some(slave_id), 20, &[true; 3])
                .await,
            master
                .write_multiple_registers_with_slave(Some(slave_id), 20, &[999; 3])
                .await,
        ];
        for result in results {
            assert!(matches!(result, Err(MasterError::InvalidConfig(_))));
        }
    }
    assert_written(&slave, 1, false, 77).await;
    assert_written(&slave, 2, true, 42).await;
    master.stop_all_scans().await;
    while let Some(event) = polls.recv().await {
        assert!(matches!(
            event,
            PollEvent::Data(ReadResult::HoldingRegisters(values)) if values == vec![2000]
        ));
    }

    let writes: Vec<_> = collector
        .get_all()
        .await
        .into_iter()
        .filter(|entry| {
            matches!(
                entry.function_code,
                FunctionCode::WriteSingleCoil
                    | FunctionCode::WriteSingleRegister
                    | FunctionCode::WriteMultipleCoils
                    | FunctionCode::WriteMultipleRegisters
            )
        })
        .collect();
    assert_eq!(writes.len(), 12);
    for (index, entry) in writes.iter().enumerate() {
        let slave_id = if index < 4 { 2 } else { 1 };
        assert!(entry.detail.starts_with(&format!("Slave {slave_id}: W ")));
    }
    master.disconnect().await.unwrap();
    slave.stop().await.unwrap();
}

#[tokio::test]
async fn tcp_writes_preserve_scan_group_target_and_default() {
    exercise_write_targets(false).await;
}

#[tokio::test]
async fn rtu_tcp_writes_preserve_scan_group_target_and_default() {
    exercise_write_targets(true).await;
}

#[tokio::test]
async fn write_target_validation_preserves_legacy_default_ids() {
    let master = MasterConnection::new(
        MasterConfig {
            slave_id: 255,
            ..Default::default()
        },
        Transport::Tcp {
            host: "127.0.0.1".into(),
            port: 502,
        },
    );
    // Validation accepts both override boundaries and the legacy configured default.
    for slave_id in [None, Some(1), Some(247)] {
        let results = [
            master.write_single_coil_with_slave(slave_id, 0, true).await,
            master
                .write_single_register_with_slave(slave_id, 0, 1)
                .await,
            master
                .write_multiple_coils_with_slave(slave_id, 0, &[true])
                .await,
            master
                .write_multiple_registers_with_slave(slave_id, 0, &[1])
                .await,
        ];
        for result in results {
            assert!(matches!(result, Err(MasterError::NotConnected)));
        }
    }
}
