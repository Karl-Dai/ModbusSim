use modbussim_core::{slave::SlaveConnection, transport::Transport};
use tokio::{
    io::AsyncReadExt,
    net::TcpStream,
    time::{sleep, timeout, Duration},
};

async fn wait_count(server: &SlaveConnection, expected: usize) {
    timeout(Duration::from_secs(3), async {
        while server.clients.list().len() != expected {
            sleep(Duration::from_millis(10)).await;
        }
    })
    .await
    .expect("client registry did not converge");
}

async fn exercise(rtu: bool) {
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
    let mut server = SlaveConnection::new(transport);
    server.start().await.unwrap();
    let connect = || async {
        timeout(Duration::from_secs(3), async {
            loop {
                if let Ok(stream) = TcpStream::connect(("127.0.0.1", port)).await {
                    break stream;
                }
                sleep(Duration::from_millis(10)).await;
            }
        })
        .await
        .unwrap()
    };
    let first = connect().await;
    let mut second = connect().await;
    wait_count(&server, 2).await;
    let rows = server.clients.list();
    assert!(rows
        .iter()
        .any(|row| row.peer_address == first.local_addr().unwrap().to_string()));
    assert!(rows
        .iter()
        .all(|row| chrono::DateTime::parse_from_rfc3339(&row.connected_at).is_ok()));
    drop(first);
    wait_count(&server, 1).await;
    server.stop().await.unwrap();
    wait_count(&server, 0).await;
    let mut buf = [0];
    assert_eq!(
        timeout(Duration::from_secs(2), second.read(&mut buf))
            .await
            .unwrap()
            .unwrap(),
        0
    );
    server.start().await.unwrap();
    let third = connect().await;
    wait_count(&server, 1).await;
    assert_eq!(
        server.clients.list()[0].peer_address,
        third.local_addr().unwrap().to_string()
    );
    drop(third);
    wait_count(&server, 0).await;
    server.stop().await.unwrap();
}

#[tokio::test]
async fn tcp_clients_follow_socket_lifecycle() {
    exercise(false).await;
}
#[tokio::test]
async fn rtu_tcp_clients_follow_socket_lifecycle() {
    exercise(true).await;
}
