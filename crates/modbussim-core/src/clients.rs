//! Runtime-only registry of connected network clients.
use serde::{Deserialize, Serialize};
use std::{
    collections::BTreeMap,
    net::{Shutdown, TcpStream},
    sync::{Arc, Mutex},
};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ClientInfo {
    pub id: u64,
    pub peer_address: String,
    pub connected_at: String,
}

#[derive(Default)]
struct Registry {
    closed: bool,
    next_id: u64,
    entries: BTreeMap<u64, (ClientInfo, TcpStream)>,
}

#[derive(Clone, Default)]
pub struct ConnectedClients(Arc<Mutex<Registry>>);

impl ConnectedClients {
    pub fn list(&self) -> Vec<ClientInfo> {
        self.0
            .lock()
            .unwrap()
            .entries
            .values()
            .map(|(info, _)| info.clone())
            .collect()
    }

    pub fn track(&self, stream: &TcpStream) -> std::io::Result<ClientGuard> {
        let socket = stream.try_clone()?;
        let peer_address = stream.peer_addr()?.to_string();
        let mut registry = self.0.lock().unwrap();
        if registry.closed {
            let _ = socket.shutdown(Shutdown::Both);
            return Err(std::io::Error::new(
                std::io::ErrorKind::NotConnected,
                "listener stopped",
            ));
        }
        registry.next_id += 1;
        let id = registry.next_id;
        registry.entries.insert(
            id,
            (
                ClientInfo {
                    id,
                    peer_address,
                    connected_at: chrono::Utc::now().to_rfc3339(),
                },
                socket,
            ),
        );
        Ok(ClientGuard {
            registry: self.clone(),
            id,
        })
    }

    pub fn close_all(&self) {
        let mut registry = self.0.lock().unwrap();
        registry.closed = true;
        for (_, (_, socket)) in std::mem::take(&mut registry.entries) {
            let _ = socket.shutdown(Shutdown::Both);
        }
    }
}

pub struct ClientGuard {
    registry: ConnectedClients,
    id: u64,
}

impl Drop for ClientGuard {
    fn drop(&mut self) {
        self.registry.0.lock().unwrap().entries.remove(&self.id);
    }
}
