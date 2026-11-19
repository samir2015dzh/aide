//! A managed browser that never answers its first command is started once more, and only once.
//! The browsers here are in memory: no Chrome is involved.

use async_trait::async_trait;
use her_browser::cdp::Conn;
use her_browser::launch::LaunchOptions;
use her_browser::launch::Launched;
use her_browser::launch::Launcher;
use her_browser::{BrowserConfig, BrowserError, BrowserPool, CdpError};
use serde_json::{Value, json};
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::Duration;
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};

/// The first `silent` starts give a browser that reads its commands and never answers; later
/// ones answer every command.
struct Starts {
    silent: usize,
    count: AtomicUsize,
}

#[async_trait]
impl Launcher for Starts {
    async fn launch(&self, _options: &LaunchOptions) -> Result<Launched, BrowserError> {
        let answers = self.count.fetch_add(1, Ordering::SeqCst) >= self.silent;
        let (client, server) = tokio::io::duplex(65536);
        let (read, mut write) = tokio::io::split(server);
        tokio::spawn(async move {
            let mut read = BufReader::new(read);
            loop {
                let mut bytes = Vec::new();
                if read.read_until(0, &mut bytes).await.unwrap_or(0) == 0 {
                    break;
                }
                if !answers {
                    continue;
                }
                bytes.pop();
                let command: Value = serde_json::from_slice(&bytes).unwrap();
                let mut reply = serde_json::to_vec(&json!({"id": command["id"], "result": {}})).unwrap();
                reply.push(0);
                if write.write_all(&reply).await.is_err() {
                    break;
                }
            }
        });
        let (reader, writer) = tokio::io::split(client);
        // A short wait for an answer stands in for COMMAND_TIMEOUT.
        let (conn, events) = Conn::start(reader, writer, Duration::from_millis(300));
        Ok(Launched { conn, events, child: None })
    }
}

fn pool(silent: usize) -> (BrowserPool, Arc<Starts>, tempfile::TempDir) {
    let dir = tempfile::tempdir().unwrap();
    let starts = Arc::new(Starts { silent, count: AtomicUsize::new(0) });
    let pool = BrowserPool::with_launcher(dir.path().join("browser"), false, starts.clone());
    (pool, starts, dir)
}

#[tokio::test]
async fn a_browser_that_never_answers_is_started_once_more() {
    let (pool, starts, _dir) = pool(1);
    pool.acquire(&BrowserConfig::default()).await.expect("the second start answers");
    assert_eq!(starts.count.load(Ordering::SeqCst), 2);
}

#[tokio::test]
async fn a_browser_that_answers_is_started_once() {
    let (pool, starts, _dir) = pool(0);
    pool.acquire(&BrowserConfig::default()).await.expect("the browser answers");
    assert_eq!(starts.count.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn two_silent_starts_are_the_last() {
    let (pool, starts, _dir) = pool(usize::MAX);
    let err = pool.acquire(&BrowserConfig::default()).await.err().expect("no browser answers");
    assert!(matches!(err, BrowserError::Cdp(CdpError::Timeout { .. })), "{err}");
    assert_eq!(starts.count.load(Ordering::SeqCst), 2);
}
