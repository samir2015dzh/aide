//! End-to-end: `her exec` in text, JSON-events and ops-on-stdin modes, against the mock model.

mod common;

use common::SchemaCheck;
use common::TestEnv;
use common::assert_increasing;
use serde_json::Value;
use serde_json::json;
use std::process::Stdio;
use tokio::io::AsyncBufReadExt;
use tokio::io::AsyncWriteExt;
use tokio::io::BufReader;

/// Runs her and returns its exit code and JSON lines, each checked against the `Event` schema.
async fn json_lines(env: &TestEnv, args: &[&str]) -> (i32, Vec<Value>) {
    let output = env.her(args).output().await.expect("run her");
    let events: Vec<Value> = String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(|l| serde_json::from_str(l).unwrap_or_else(|e| panic!("not JSON ({e}): {l}")))
        .collect();
    let schema = SchemaCheck::new(&["Event"]);
    for event in &events {
        schema.check("Event", event);
    }
    (output.status.code().unwrap_or(-1), events)
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn protocol_schema_prints_the_document() {
    let env = TestEnv::new("").await;
    let output = env.her(&["protocol", "schema"]).output().await.unwrap();
    assert_eq!(output.status.code(), Some(0));
    let printed: Value = serde_json::from_slice(&output.stdout).expect("JSON on stdout");
    assert!(printed == her_protocol::schema::document());
    assert_eq!(printed["x-methods"]["session/op"]["params"], "SessionOpParams");
    // An operation in the shape `--ops-stdin` and `session/op` take is valid.
    let ops = SchemaCheck::new(&["Op"]);
    ops.check("Op", &json!({"type": "steer_now"}));
    ops.check("Op", &json!({"type": "prompt", "input": [{"type": "text", "text": "hi"}], "mode": "follow_up"}));
}

fn of_type<'a>(events: &'a [Value], kind: &str) -> Vec<&'a Value> {
    events.iter().filter(|e| e["type"] == kind).collect()
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn exec_text_prints_the_streamed_reply() {
    let env = TestEnv::new("").await;
    let output = env.her(&["exec", "say hi"]).output().await.unwrap();
    assert_eq!(output.status.code(), Some(0), "stderr: {}", String::from_utf8_lossy(&output.stderr));
    assert!(String::from_utf8_lossy(&output.stdout).contains("Hello from mock."));
}

#[tokio::test]
async fn foreground_runtime_drives_parallel_agents_and_their_tools() {
    let env = TestEnv::new("").await;
    std::fs::create_dir(env.work().join("src")).unwrap();
    std::fs::write(env.work().join("src/lib.rs"), "pub fn value() -> u32 { 42 }\n").unwrap();
    std::fs::write(env.work().join("README.md"), "A small library.\n").unwrap();
    let (code, events) = tokio::time::timeout(
        std::time::Duration::from_secs(20),
        json_lines(&env, &["exec", "--output", "stream-json", "[[mock:fanout:3:foreground]] inspect modules"]),
    ).await.expect("foreground agents must not deadlock");
    assert_eq!(code, 0);
    let tools = of_type(&events, "tool_completed");
    let started = of_type(&events, "tool_started");
    let agents: Vec<_> = started.iter().filter(|e| e["name"] == "spawn_agent").collect();
    assert_eq!(agents.len(), 3);
    assert!(agents.iter().all(|a| tools.iter().any(|t| t["call_id"] == a["call_id"])));
    assert!(tools.iter().all(|e| e["result"]["is_error"] != true));
    assert_eq!(of_type(&events, "task_completed")[0]["reason"], "done");
    for i in 0..3 {
        let report = std::fs::read_to_string(env.work().join(format!("notes/foreground-a{i}.md"))).unwrap();
        assert!(report.contains("状态：DONE"), "agent {i} did not finish its edit");
    }
}

#[tokio::test]
async fn foreground_runtime_accepts_interrupt_while_streaming() {
    let env = TestEnv::new("").await;
    let mut child = env.her(&["exec", "--output", "stream-json", "--ops-stdin", "[[mock:slow:200]] reply slowly"])
        .stdin(Stdio::piped()).stdout(Stdio::piped()).kill_on_drop(true).spawn().unwrap();
    let mut stdin = child.stdin.take().unwrap();
    let mut lines = BufReader::new(child.stdout.take().unwrap()).lines();
    let mut interrupted = false;
    let mut sent = false;
    tokio::time::timeout(std::time::Duration::from_secs(10), async {
        while let Some(line) = lines.next_line().await.unwrap() {
            let event: Value = serde_json::from_str(&line).unwrap();
            if event["type"] == "message_delta" && !sent {
                stdin.write_all(b"{\"type\":\"interrupt\"}\n").await.unwrap();
                stdin.flush().await.unwrap();
                sent = true;
            }
            interrupted |= event["type"] == "task_interrupted";
        }
    }).await.expect("stdin interruption must progress on the foreground runtime");
    assert!(sent && interrupted);
    assert_eq!(common::exit_code(&mut child).await, 130);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn exec_json_emits_enriched_tool_events() {
    let env = TestEnv::new("").await;
    let (code, events) = json_lines(&env, &["exec", "--output", "stream-json", "[[mock:bash:echo e2e-marker]] run it"]).await;
    assert_eq!(code, 0);
    assert_increasing(&events);
    let started = of_type(&events, "session_started");
    assert_eq!(started[0]["model"], "mock/mock-1");
    assert_eq!(started[0]["context_window"], 200000);
    assert!(started[0].get("permission_mode").is_none(), "no permission modes");
    let tool = of_type(&events, "tool_started")[0];
    assert_eq!(tool["category"], "execute");
    assert_eq!(tool["title"], "echo e2e-marker");
    assert_eq!(of_type(&events, "tool_running").len(), 1);
    let done = of_type(&events, "tool_completed")[0];
    assert!(done["result"]["content"][0]["text"].as_str().unwrap().contains("e2e-marker"));
    let end = events.last().unwrap();
    assert_eq!(end["type"], "session_closed");
    let completed = of_type(&events, "task_completed")[0];
    assert_eq!(completed["reason"], "done");
    assert_eq!(completed["last_message"], "bash done");
}

/// Nobody is asked: a script's run changes files and its sub-agents' commands run.
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn exec_runs_every_action_without_asking() {
    let env = TestEnv::new("").await;
    let (code, events) = json_lines(&env, &["exec", "--output", "stream-json", "[[mock:files]] edit notes"]).await;
    assert_eq!(code, 0);
    assert!(events.iter().all(|e| !e["type"].as_str().unwrap_or("").starts_with("approval")));
    assert_eq!(of_type(&events, "tool_completed").len(), 3);
    assert_eq!(std::fs::read_to_string(env.work().join("notes.txt")).unwrap(), "gamma\nbeta\n");

    let (code, _) = json_lines(&env, &["exec", "--output", "stream-json", "[[mock:delegate:touch made.txt]] run it"]).await;
    assert_eq!(code, 0);
    assert!(env.work().join("made.txt").exists(), "the sub-agent's command ran");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn exec_reports_retries_failures_and_cutoffs() {
    let env = TestEnv::new("").await;
    let (code, events) = json_lines(&env, &["exec", "--output", "stream-json", "[[mock:retry]] once"]).await;
    assert_eq!(code, 0);
    assert_eq!(of_type(&events, "stream_retry").len(), 1);
    assert_eq!(of_type(&events, "task_completed")[0]["last_message"], "recovered after a retry");

    let (code, events) = json_lines(&env, &["exec", "--output", "stream-json", "[[mock:fail]] please"]).await;
    assert_eq!(code, 1);
    let failed = of_type(&events, "task_failed");
    assert_eq!(failed.len(), 1);
    assert!(failed[0]["error"]["message"].as_str().unwrap().contains("mock rejects"));

    let (code, events) = json_lines(&env, &["exec", "--output", "stream-json", "[[mock:cut]] long answer"]).await;
    assert_eq!(code, 0);
    assert_eq!(of_type(&events, "task_completed")[0]["reason"], "output_limit");

    let (code, events) = json_lines(&env, &["exec", "--output", "stream-json", "--max-turns", "2", "[[mock:loop:5]] spin"]).await;
    assert_eq!(code, 0);
    assert_eq!(of_type(&events, "task_completed")[0]["reason"], "max_turns");
}
