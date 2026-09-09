//! 端到端联动测试：同进程内启动 core slave + master，跑完整交互流程。
//!
//! - 完全非交互：`cargo test --test e2e_flow` 直接跑通/失败。
//! - 每个关键节点打 `[E2E <ms>] [<stage>] ...` 日志，`-- --nocapture` 可直接读。
//! - assert 失败时 stderr 日志也会打出来，便于定位在哪一步坏。
//!
//! 运行：
//! ```bash
//! cargo test --test e2e_flow -- --nocapture
//! ```

use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use modbussim_core::master::{
    scan_registers_with_ctx, scan_slave_ids_with_ctx, MasterConfig, MasterConnection, MasterError,
    MasterState, PollEvent, ReadFunction, ReadResult, ScanGroup,
};
use modbussim_core::slave::{SlaveConnection, SlaveDevice};
use modbussim_core::transport::Transport;
use tokio::sync::{mpsc, oneshot};
use tokio_modbus::client::Reader;
use tokio_modbus::ExceptionCode;

/// 统一日志：`[E2E 12345ms] [stage       ] msg`。
/// 使用 stderr，test runner 不 capture stderr 时也能看到；`-- --nocapture` 两者都会打。
macro_rules! step {
    ($stage:expr, $($arg:tt)*) => {{
        let ts = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_millis())
            .unwrap_or(0);
        eprintln!("[E2E {:>12}ms] [{:<14}] {}", ts, $stage, format!($($arg)*));
    }}
}

fn now() -> Instant {
    Instant::now()
}

fn make_master(port: u16, timeout_ms: u64) -> MasterConnection {
    let config = MasterConfig {
        target_address: "127.0.0.1".to_string(),
        port,
        slave_id: 1,
        timeout_ms,
        ..Default::default()
    };
    let transport = Transport::Tcp {
        host: config.target_address.clone(),
        port: config.port,
    };
    MasterConnection::new(config, transport)
}

/// 起一个本地 slave：slave_id=1，预填 0..=50 的四种寄存器。
async fn make_slave(port: u16) -> SlaveConnection {
    let transport = Transport::Tcp {
        host: "127.0.0.1".to_string(),
        port,
    };
    let mut conn = SlaveConnection::new(transport);

    let mut device = SlaveDevice::new(1, "E2E-Device");
    for addr in 0..=50u16 {
        device.register_map.holding_registers.insert(addr, 0);
        device.register_map.input_registers.insert(addr, 0);
        device.register_map.coils.insert(addr, false);
        device.register_map.discrete_inputs.insert(addr, false);
    }
    // 固定的种子数据
    device.register_map.write_holding_register(0, 1111);
    device.register_map.write_holding_register(1, 2222);
    device.register_map.write_holding_register(2, 3333);
    device.register_map.write_coil(0, true);
    device.register_map.write_coil(1, false);
    device.register_map.write_coil(2, true);
    device.register_map.discrete_inputs.insert(0, true);
    device.register_map.input_registers.insert(5, 9999);

    conn.add_device(device).await.unwrap();
    conn.start().await.unwrap();
    // 给 server 一点点时间起来。
    tokio::time::sleep(Duration::from_millis(80)).await;
    conn
}

/// 场景 1：完整读/写/轮询链路。
///
/// 流程：启动 slave → master connect → FC03/01/02/04 全读 → FC06/05/16/15 全写
/// → 读回校验 → 启动 scan group 跑 3 轮 → stop → disconnect → slave.stop。
#[tokio::test]
async fn e2e_full_master_slave_flow() {
    let port = 17701;
    step!("SETUP", "spawn slave @127.0.0.1:{}", port);
    let mut slave = make_slave(port).await;
    step!("SETUP", "slave up, state={:?}", slave.state());

    let mut master = make_master(port, 3000);
    step!("MASTER", "pre-connect state={:?}", master.state());

    let t0 = now();
    master.connect().await.expect("connect");
    step!(
        "MASTER",
        "connected in {}ms, state={:?}",
        t0.elapsed().as_millis(),
        master.state()
    );
    assert_eq!(master.state(), MasterState::Connected);

    // ---- 读路径 --------------------------------------------------------
    let r = master
        .read(ReadFunction::ReadHoldingRegisters, 0, 3)
        .await
        .expect("read FC03");
    step!("READ FC03", "addr=0 qty=3 -> {:?}", r);
    match r {
        ReadResult::HoldingRegisters(vs) => assert_eq!(vs, vec![1111, 2222, 3333]),
        other => panic!("FC03 unexpected: {other:?}"),
    }

    let r = master
        .read(ReadFunction::ReadCoils, 0, 3)
        .await
        .expect("read FC01");
    step!("READ FC01", "addr=0 qty=3 -> {:?}", r);
    match r {
        ReadResult::Coils(vs) => assert_eq!(vs, vec![true, false, true]),
        other => panic!("FC01 unexpected: {other:?}"),
    }

    let r = master
        .read(ReadFunction::ReadDiscreteInputs, 0, 1)
        .await
        .expect("read FC02");
    step!("READ FC02", "addr=0 qty=1 -> {:?}", r);
    match r {
        ReadResult::DiscreteInputs(vs) => assert_eq!(vs, vec![true]),
        other => panic!("FC02 unexpected: {other:?}"),
    }

    let r = master
        .read(ReadFunction::ReadInputRegisters, 5, 1)
        .await
        .expect("read FC04");
    step!("READ FC04", "addr=5 qty=1 -> {:?}", r);
    match r {
        ReadResult::InputRegisters(vs) => assert_eq!(vs, vec![9999]),
        other => panic!("FC04 unexpected: {other:?}"),
    }

    // ---- 写路径 --------------------------------------------------------
    master
        .write_single_register(10, 4242)
        .await
        .expect("write FC06");
    step!("WRITE FC06", "addr=10 val=4242 ok");

    master
        .write_single_coil(10, true)
        .await
        .expect("write FC05");
    step!("WRITE FC05", "addr=10 val=true ok");

    master
        .write_multiple_registers(20, &[7, 8, 9])
        .await
        .expect("write FC16");
    step!("WRITE FC16", "addr=20..22 vals=[7,8,9] ok");

    master
        .write_multiple_coils(20, &[true, false, true])
        .await
        .expect("write FC15");
    step!("WRITE FC15", "addr=20..22 vals=[T,F,T] ok");

    // ---- 回读校验 ------------------------------------------------------
    let r = master
        .read(ReadFunction::ReadHoldingRegisters, 10, 1)
        .await
        .unwrap();
    match r {
        ReadResult::HoldingRegisters(vs) => {
            step!("VERIFY FC06", "HR[10] = {:?}", vs);
            assert_eq!(vs, vec![4242]);
        }
        _ => unreachable!(),
    }
    let r = master.read(ReadFunction::ReadCoils, 10, 1).await.unwrap();
    match r {
        ReadResult::Coils(vs) => {
            step!("VERIFY FC05", "COIL[10] = {:?}", vs);
            assert_eq!(vs, vec![true]);
        }
        _ => unreachable!(),
    }
    let r = master
        .read(ReadFunction::ReadHoldingRegisters, 20, 3)
        .await
        .unwrap();
    match r {
        ReadResult::HoldingRegisters(vs) => {
            step!("VERIFY FC16", "HR[20..22] = {:?}", vs);
            assert_eq!(vs, vec![7, 8, 9]);
        }
        _ => unreachable!(),
    }
    let r = master.read(ReadFunction::ReadCoils, 20, 3).await.unwrap();
    match r {
        ReadResult::Coils(vs) => {
            step!("VERIFY FC15", "COIL[20..22] = {:?}", vs);
            assert_eq!(vs, vec![true, false, true]);
        }
        _ => unreachable!(),
    }

    // ---- Scan group 轮询 ----------------------------------------------
    let group = ScanGroup {
        id: "g1".to_string(),
        name: "g1".to_string(),
        function: ReadFunction::ReadHoldingRegisters,
        start_address: 0,
        quantity: 3,
        interval_ms: 100,
        enabled: true,
        slave_id: None,
    };
    step!(
        "SCAN START",
        "group={} intv={}ms",
        group.id,
        group.interval_ms
    );
    let mut rx = master.start_scan_group(&group).await.expect("start_scan");
    assert!(master.is_scan_active("g1"));

    let mut data_count = 0u32;
    let deadline = Instant::now() + Duration::from_secs(2);
    while data_count < 3 && Instant::now() < deadline {
        match tokio::time::timeout(Duration::from_millis(800), rx.recv()).await {
            Ok(Some(PollEvent::Data(ReadResult::HoldingRegisters(vs)))) => {
                data_count += 1;
                step!("SCAN DATA", "#{} values={:?}", data_count, vs);
                assert_eq!(vs, vec![1111, 2222, 3333]);
            }
            Ok(Some(PollEvent::Error(e))) => panic!("scan error: {e}"),
            Ok(Some(other)) => panic!("unexpected scan event: {other:?}"),
            Ok(None) => panic!("scan channel closed early"),
            Err(_) => panic!("scan timeout waiting for PollEvent::Data"),
        }
    }
    assert!(
        data_count >= 3,
        "expected >=3 scan updates, got {data_count}"
    );

    master.stop_scan_group("g1").await.expect("stop_scan");
    step!("SCAN STOP", "g1 stopped");

    // ---- Teardown ------------------------------------------------------
    master.disconnect().await.expect("disconnect");
    step!("MASTER", "disconnected state={:?}", master.state());
    slave.stop().await.expect("slave stop");
    step!("SLAVE", "stopped state={:?}", slave.state());
    step!("DONE", "full flow OK — {} scan updates", data_count);
}

/// 场景 2：slave 端寄存器变动 → master scan group 下一轮能观察到新值。
///
/// 验证"主站轮询是真轮询",不是缓存。
#[tokio::test]
async fn e2e_scan_group_detects_slave_mutation() {
    let port = 17702;
    step!("SETUP", "spawn slave @127.0.0.1:{}", port);
    let slave = make_slave(port).await;
    let mut master = make_master(port, 3000);
    master.connect().await.expect("connect");
    step!("MASTER", "connected");

    let group = ScanGroup {
        id: "gm".to_string(),
        name: "gm".to_string(),
        function: ReadFunction::ReadHoldingRegisters,
        start_address: 0,
        quantity: 1,
        interval_ms: 80,
        enabled: true,
        slave_id: None,
    };
    let mut rx = master.start_scan_group(&group).await.unwrap();
    step!("SCAN START", "gm @addr=0 qty=1 intv=80ms");

    // 第一次:基线,应为 1111
    let first = tokio::time::timeout(Duration::from_millis(500), rx.recv())
        .await
        .expect("first poll timeout")
        .expect("rx closed");
    match first {
        PollEvent::Data(ReadResult::HoldingRegisters(vs)) => {
            step!("SCAN BASELINE", "HR[0]={:?}", vs);
            assert_eq!(vs, vec![1111]);
        }
        other => panic!("unexpected first event: {other:?}"),
    }

    // 外部直接改 slave device 的 HR[0]
    {
        let mut devices = slave.devices.write().await;
        let d = devices.get_mut(&1).unwrap();
        d.register_map.write_holding_register(0, 5555);
        step!("SLAVE MUTATE", "HR[0] 1111 -> 5555");
    }

    // 在接下来几轮里一定能看到 5555
    let deadline = Instant::now() + Duration::from_secs(2);
    let mut saw_new = false;
    let mut rounds = 0u32;
    while Instant::now() < deadline {
        match tokio::time::timeout(Duration::from_millis(500), rx.recv()).await {
            Ok(Some(PollEvent::Data(ReadResult::HoldingRegisters(vs)))) => {
                rounds += 1;
                step!("SCAN ROUND", "#{} HR[0]={:?}", rounds, vs);
                if vs == vec![5555] {
                    saw_new = true;
                    break;
                }
            }
            Ok(Some(PollEvent::Error(e))) => panic!("scan error: {e}"),
            Ok(Some(_)) => {}
            Ok(None) => panic!("rx closed"),
            Err(_) => panic!("scan round timeout"),
        }
    }
    assert!(
        saw_new,
        "mutation was not observed within deadline (rounds={rounds})"
    );
    step!("VERIFY", "mutation observed after {} rounds", rounds);

    master.stop_scan_group("gm").await.unwrap();
    master.disconnect().await.unwrap();
    // slave 离开作用域即 drop，但显式 stop 更干净
    let mut slave = slave;
    slave.stop().await.ok();
    step!("DONE", "mutation-detection flow OK");
}

/// 场景 3：读不存在的地址应触发 Modbus IllegalDataAddress 异常（错误路径）。
#[tokio::test]
async fn e2e_illegal_address_surfaces_error() {
    let port = 17703;
    let mut slave = make_slave(port).await;
    let mut master = make_master(port, 2000);
    master.connect().await.unwrap();
    step!("MASTER", "connected");

    // 地址 999 没有预填 → slave 会返回 IllegalDataAddress
    let res = master
        .read(ReadFunction::ReadHoldingRegisters, 999, 1)
        .await;
    step!("READ OOB", "addr=999 -> {:?}", res);
    assert!(
        res.is_err(),
        "expected error on illegal address, got {res:?}"
    );

    master.disconnect().await.ok();
    slave.stop().await.ok();
    step!("DONE", "illegal-address flow OK");
}

// ===========================================================================
// 场景 4：异常码全谱（IllegalDataValue / 路由不存在 → Timeout）
// ===========================================================================

/// FC03 qty=1000 触发 IllegalDataValue（spec: max=125）；未知 slave_id 由 slave
/// 静默丢弃 → master 因没有响应而 Timeout。
#[tokio::test]
async fn e2e_exception_codes_full() {
    let port = 17704;
    step!("SETUP", "spawn slave @127.0.0.1:{}", port);
    let mut slave = make_slave(port).await;

    let mut master = make_master(port, 800);
    master.connect().await.expect("connect");
    step!("MASTER", "connected");

    // Use the raw context to send deliberately oversized packets: MasterConnection::read
    // now splits logical ranges into protocol-sized requests.
    let ctx = master.get_ctx_handle().expect("raw context");
    // (a) FC03 qty=1000 → IllegalDataValue（FC03 上限 125）
    let res = ctx.lock().await.read_holding_registers(0, 1000).await;
    step!("EXC IDV", "FC03 qty=1000 -> {:?}", res);
    match res {
        Ok(Err(ExceptionCode::IllegalDataValue)) => {}
        other => panic!("expected IllegalDataValue, got {other:?}"),
    }

    // (b) FC04 qty=200 同理（FC04 上限 125）
    let res = ctx.lock().await.read_input_registers(0, 200).await;
    step!("EXC IDV", "FC04 qty=200 -> {:?}", res);
    assert!(matches!(res, Ok(Err(ExceptionCode::IllegalDataValue))));

    // (c) 未知 slave_id：reconnect 用 slave_id=99 → slave 静默丢弃 → Timeout
    master.disconnect().await.ok();
    master.config.slave_id = 99;
    master.connect().await.expect("reconnect with slave_id=99");
    let t0 = Instant::now();
    let res = master.read(ReadFunction::ReadHoldingRegisters, 0, 1).await;
    step!(
        "EXC ROUTE",
        "slave_id=99 read -> {:?} (elapsed {}ms)",
        res,
        t0.elapsed().as_millis()
    );
    match res {
        Err(MasterError::Timeout(_)) | Err(MasterError::Transport(_)) => {}
        other => panic!("expected Timeout/Transport for unknown slave_id, got {other:?}"),
    }

    master.disconnect().await.ok();
    slave.stop().await.ok();
    step!("DONE", "exception-codes flow OK");
}

// ===========================================================================
// 场景 5：连接生命周期（disconnect → reconnect → 死端口 connect_timeout）
// ===========================================================================

#[tokio::test]
async fn e2e_connection_lifecycle() {
    let port = 17705;
    let mut slave = make_slave(port).await;

    let mut master = make_master(port, 1500);
    assert_eq!(master.state(), MasterState::Disconnected);
    step!("LIFECYCLE", "initial state={:?}", master.state());

    master.connect().await.expect("connect#1");
    assert_eq!(master.state(), MasterState::Connected);
    step!("LIFECYCLE", "after connect#1 state={:?}", master.state());

    let r = master
        .read(ReadFunction::ReadHoldingRegisters, 0, 1)
        .await
        .expect("read#1");
    step!("LIFECYCLE", "read#1 -> {:?}", r);

    master.disconnect().await.expect("disconnect#1");
    assert_eq!(master.state(), MasterState::Disconnected);
    step!("LIFECYCLE", "after disconnect#1 state={:?}", master.state());

    // reconnect 后能读
    master.reconnect().await.expect("reconnect");
    assert_eq!(master.state(), MasterState::Connected);
    let r = master
        .read(ReadFunction::ReadHoldingRegisters, 0, 1)
        .await
        .expect("read#2");
    step!("LIFECYCLE", "after reconnect read#2 -> {:?}", r);

    master.disconnect().await.ok();

    // 死端口（无 listener）→ ConnectionFailed/Timeout
    let dead_port = 19999u16;
    let dead_cfg = MasterConfig {
        target_address: "127.0.0.1".to_string(),
        port: dead_port,
        slave_id: 1,
        timeout_ms: 400,
        ..Default::default()
    };
    let dead_tx = Transport::Tcp {
        host: dead_cfg.target_address.clone(),
        port: dead_cfg.port,
    };
    let mut dead = MasterConnection::new(dead_cfg, dead_tx);
    let t0 = Instant::now();
    let res = dead.connect().await;
    step!(
        "LIFECYCLE",
        "dead-port connect -> {:?} (elapsed {}ms)",
        res,
        t0.elapsed().as_millis()
    );
    assert!(res.is_err(), "expected error connecting to dead port");

    slave.stop().await.ok();
    step!("DONE", "lifecycle flow OK");
}

// ===========================================================================
// 场景 6：多设备路由（slave 挂 id=1/2/3，master 通过 reconnect 切换）
// ===========================================================================

#[tokio::test]
async fn e2e_multi_device_routing() {
    let port = 17706;
    step!("SETUP", "spawn multi-device slave @127.0.0.1:{}", port);

    let transport = Transport::Tcp {
        host: "127.0.0.1".to_string(),
        port,
    };
    let mut slave = SlaveConnection::new(transport);

    for (id, base) in [(1u8, 100u16), (2, 200), (3, 300)] {
        let mut d = SlaveDevice::new(id, format!("Dev-{}", id));
        d.register_map.holding_registers.insert(0, base);
        slave.add_device(d).await.unwrap();
        step!("SETUP", "added slave_id={} HR[0]={}", id, base);
    }
    slave.start().await.unwrap();
    tokio::time::sleep(Duration::from_millis(80)).await;

    let mut master = make_master(port, 1000);

    for (id, expected) in [(1u8, 100u16), (2, 200), (3, 300)] {
        master.config.slave_id = id;
        if master.state() == MasterState::Connected {
            master.disconnect().await.ok();
        }
        master.connect().await.expect("connect");
        let r = master
            .read(ReadFunction::ReadHoldingRegisters, 0, 1)
            .await
            .expect("read");
        step!("ROUTE", "slave_id={} HR[0] -> {:?}", id, r);
        match r {
            ReadResult::HoldingRegisters(vs) => assert_eq!(vs, vec![expected]),
            other => panic!("unexpected: {other:?}"),
        }
    }

    master.disconnect().await.ok();
    slave.stop().await.ok();
    step!("DONE", "multi-device routing flow OK");
}

// ===========================================================================
// 场景 7：多 ScanGroup 并行 + 独立停止
// ===========================================================================

#[tokio::test]
async fn e2e_multi_scan_groups() {
    let port = 17707;
    let mut slave = make_slave(port).await;
    let mut master = make_master(port, 2000);
    master.connect().await.expect("connect");

    let g1 = ScanGroup {
        id: "g_hr".to_string(),
        name: "g_hr".to_string(),
        function: ReadFunction::ReadHoldingRegisters,
        start_address: 0,
        quantity: 2,
        interval_ms: 80,
        enabled: true,
        slave_id: None,
    };
    let g2 = ScanGroup {
        id: "g_coil".to_string(),
        name: "g_coil".to_string(),
        function: ReadFunction::ReadCoils,
        start_address: 0,
        quantity: 2,
        interval_ms: 80,
        enabled: true,
        slave_id: None,
    };

    let mut rx1 = master.start_scan_group(&g1).await.expect("start g1");
    let mut rx2 = master.start_scan_group(&g2).await.expect("start g2");
    assert!(master.is_scan_active("g_hr"));
    assert!(master.is_scan_active("g_coil"));
    step!("MULTI SCAN", "both groups started");

    // 各收两轮
    let mut n1 = 0u32;
    let mut n2 = 0u32;
    let deadline = Instant::now() + Duration::from_secs(2);
    while (n1 < 2 || n2 < 2) && Instant::now() < deadline {
        tokio::select! {
            ev = rx1.recv() => match ev {
                Some(PollEvent::Data(ReadResult::HoldingRegisters(vs))) => {
                    n1 += 1;
                    step!("MULTI SCAN", "g_hr #{} -> {:?}", n1, vs);
                    assert_eq!(vs, vec![1111, 2222]);
                }
                Some(PollEvent::Error(e)) => panic!("g_hr error: {e}"),
                _ => {}
            },
            ev = rx2.recv() => match ev {
                Some(PollEvent::Data(ReadResult::Coils(vs))) => {
                    n2 += 1;
                    step!("MULTI SCAN", "g_coil #{} -> {:?}", n2, vs);
                    assert_eq!(vs, vec![true, false]);
                }
                Some(PollEvent::Error(e)) => panic!("g_coil error: {e}"),
                _ => {}
            }
        }
    }
    assert!(n1 >= 2 && n2 >= 2, "n1={n1} n2={n2}");

    // 单独停 g1，g2 仍在
    master.stop_scan_group("g_hr").await.expect("stop g1");
    assert!(!master.is_scan_active("g_hr"));
    assert!(master.is_scan_active("g_coil"));
    step!("MULTI SCAN", "stopped g_hr only, g_coil still active");

    // 验证 g_coil 还在产数据
    let extra = tokio::time::timeout(Duration::from_millis(500), rx2.recv()).await;
    assert!(matches!(extra, Ok(Some(PollEvent::Data(_)))));
    step!("MULTI SCAN", "g_coil continues after g_hr stop");

    master.stop_scan_group("g_coil").await.ok();
    master.disconnect().await.ok();
    slave.stop().await.ok();
    step!("DONE", "multi-scan-groups flow OK");
}

// ===========================================================================
// 场景 8：主站扫描器（scan_slave_ids + scan_registers）
// ===========================================================================

#[tokio::test]
async fn e2e_master_scanners() {
    let port = 17708;
    step!("SETUP", "spawn slave @127.0.0.1:{}", port);
    let transport = Transport::Tcp {
        host: "127.0.0.1".to_string(),
        port,
    };
    let mut slave = SlaveConnection::new(transport);
    for id in [1u8, 5, 7] {
        let mut d = SlaveDevice::new(id, format!("S{id}"));
        d.register_map.holding_registers.insert(0, 1000 + id as u16);
        slave.add_device(d).await.unwrap();
    }
    slave.start().await.unwrap();
    tokio::time::sleep(Duration::from_millis(80)).await;

    let mut master = make_master(port, 600);
    master.connect().await.expect("connect");

    // (a) scan_slave_ids
    let ctx = master.get_ctx_handle().expect("ctx");
    let (cancel_tx, cancel_rx) = oneshot::channel::<()>();
    let (prog_tx, mut prog_rx) = mpsc::channel(64);
    drop(cancel_tx); // 不取消
    let scan_handle = tokio::spawn(scan_slave_ids_with_ctx(
        ctx.clone(),
        master.config.slave_id,
        1,
        10,
        Duration::from_millis(150),
        cancel_rx,
        prog_tx,
    ));
    while let Some(p) = prog_rx.recv().await {
        if p.done {
            step!("SCAN IDS", "done found={:?}", p.found_ids);
            break;
        }
    }
    let found = scan_handle.await.expect("scan task");
    step!("SCAN IDS", "scan_slave_ids -> {:?}", found);
    assert_eq!(found, vec![1, 5, 7]);

    // (b) scan_registers (HR 0..=5)
    let (cancel_tx2, cancel_rx2) = oneshot::channel::<()>();
    drop(cancel_tx2);
    let (prog_tx2, mut prog_rx2) = mpsc::channel(64);
    // chunk_size=1：slave 只填了 HR[0]，更大的 chunk 会因 HR[1..] 不存在
    // 触发 IllegalDataAddress 而整块丢弃。逐地址扫描更接近真实使用。
    let regs_handle = tokio::spawn(scan_registers_with_ctx(
        ctx,
        ReadFunction::ReadHoldingRegisters,
        0,
        5,
        1,
        Duration::from_millis(200),
        cancel_rx2,
        prog_tx2,
    ));
    while let Some(p) = prog_rx2.recv().await {
        if p.done {
            step!(
                "SCAN REGS",
                "done found={} entries",
                p.found_registers.len()
            );
            break;
        }
    }
    let regs = regs_handle.await.expect("regs task");
    step!("SCAN REGS", "found = {:?}", regs);
    // 当前 slave_id=1，只能扫到 id=1 的寄存器：HR[0]=1001
    assert!(
        regs.iter().any(|f| f.address == 0 && f.value == 1001),
        "expected HR[0]=1001 from slave_id=1, got {regs:?}"
    );

    master.disconnect().await.ok();
    slave.stop().await.ok();
    step!("DONE", "master-scanners flow OK");
}

// ===========================================================================
// 场景 9：随机变异传播（mutation 后台跑 → master 轮询能看到值变化）
// ===========================================================================

#[tokio::test]
async fn e2e_random_mutation_propagation() {
    let port = 17709;
    // 用 with_default_registers 创建 device：register_defs 非空，
    // apply_random_mutation_thread 才会真正跑。
    let transport = Transport::Tcp {
        host: "127.0.0.1".to_string(),
        port,
    };
    let mut slave = SlaveConnection::new(transport);
    let device = SlaveDevice::with_default_registers(1, "Mut-Device", 10);
    slave.add_device(device).await.unwrap();
    slave.start().await.unwrap();
    tokio::time::sleep(Duration::from_millis(80)).await;

    let mut master = make_master(port, 2000);
    master.connect().await.expect("connect");
    step!("MASTER", "connected");

    // 后台周期变异 HR
    let devices = slave.devices.clone();
    let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
    let mutator = tokio::spawn(async move {
        let mut ticker = tokio::time::interval(Duration::from_millis(60));
        let mut count = 0u32;
        loop {
            tokio::select! {
                _ = &mut stop_rx => break,
                _ = ticker.tick() => {
                    let mut g = devices.write().await;
                    if let Some(d) = g.get_mut(&1) {
                        // 直接递增 HR0 制造值变化(替代已移除的设备级随机变位),验证变化经 Modbus 传播
                        d.register_map.write_holding_register(0, count as u16);
                        count += 1;
                    }
                }
            }
        }
        count
    });

    let group = ScanGroup {
        id: "g_mut".to_string(),
        name: "g_mut".to_string(),
        function: ReadFunction::ReadHoldingRegisters,
        start_address: 0,
        quantity: 5,
        interval_ms: 80,
        enabled: true,
        slave_id: None,
    };
    let mut rx = master.start_scan_group(&group).await.expect("scan start");

    let mut samples: Vec<Vec<u16>> = Vec::new();
    let deadline = Instant::now() + Duration::from_secs(3);
    while samples.len() < 8 && Instant::now() < deadline {
        match tokio::time::timeout(Duration::from_millis(500), rx.recv()).await {
            Ok(Some(PollEvent::Data(ReadResult::HoldingRegisters(vs)))) => {
                step!("MUT POLL", "#{} -> {:?}", samples.len() + 1, vs);
                samples.push(vs);
            }
            Ok(Some(PollEvent::Error(e))) => panic!("scan error: {e}"),
            Ok(_) => {}
            Err(_) => panic!("scan recv timeout"),
        }
    }

    let _ = stop_tx.send(());
    let muts = mutator.await.unwrap_or(0);
    master.stop_scan_group("g_mut").await.ok();

    let unique: std::collections::HashSet<_> = samples.iter().cloned().collect();
    step!(
        "MUT VERIFY",
        "samples={} unique={} mutations_applied={}",
        samples.len(),
        unique.len(),
        muts
    );
    assert!(unique.len() >= 2, "expected value churn, got {samples:?}");

    master.disconnect().await.ok();
    slave.stop().await.ok();
    step!("DONE", "mutation-propagation flow OK");
}

// ===========================================================================
// 场景 10：并发读写（poll + 应用层并发写，无死锁/数据竞争）
// ===========================================================================

#[tokio::test]
async fn e2e_concurrent_read_write() {
    let port = 17710;
    let mut slave = make_slave(port).await;
    let mut master = make_master(port, 2000);
    master.connect().await.expect("connect");
    step!("MASTER", "connected");

    let group = ScanGroup {
        id: "g_cc".to_string(),
        name: "g_cc".to_string(),
        function: ReadFunction::ReadHoldingRegisters,
        start_address: 0,
        quantity: 3,
        interval_ms: 50,
        enabled: true,
        slave_id: None,
    };
    let mut rx = master.start_scan_group(&group).await.expect("scan");

    // 后台收 poll 事件计数
    let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
    let collector = tokio::spawn(async move {
        let mut ok = 0u32;
        let mut err = 0u32;
        loop {
            tokio::select! {
                _ = &mut stop_rx => break,
                ev = rx.recv() => match ev {
                    Some(PollEvent::Data(_)) => ok += 1,
                    Some(PollEvent::Error(_)) => err += 1,
                    _ => break,
                }
            }
        }
        (ok, err)
    });

    // 顺序 50 次写（同一 master 实例的 ctx 是 Arc<Mutex<>>，不是真并发，
    // 但与 scan_group 后台 task 形成竞态：验证不死锁、最终值正确）
    let n = 50u16;
    for i in 0..n {
        master
            .write_single_register(10, i)
            .await
            .expect("write_single_register");
        if i % 10 == 0 {
            step!("CC WRITE", "addr=10 val={}", i);
        }
    }
    step!("CC WRITE", "all {} writes done", n);

    // 等待 poll 至少再跑一轮
    tokio::time::sleep(Duration::from_millis(150)).await;

    let r = master
        .read(ReadFunction::ReadHoldingRegisters, 10, 1)
        .await
        .expect("final read");
    step!("CC VERIFY", "HR[10] -> {:?}", r);
    match r {
        ReadResult::HoldingRegisters(vs) => assert_eq!(vs, vec![n - 1]),
        other => panic!("unexpected: {other:?}"),
    }

    let _ = stop_tx.send(());
    master.stop_scan_group("g_cc").await.ok();
    let (ok, err) = collector.await.unwrap_or((0, 0));
    step!("CC POLL", "ok={} err={}", ok, err);
    assert!(
        ok >= 3,
        "scan group should have produced ≥3 events, got {ok}"
    );
    assert_eq!(err, 0, "scan group had {err} errors");

    master.disconnect().await.ok();
    slave.stop().await.ok();
    step!("DONE", "concurrent-rw flow OK");
}
