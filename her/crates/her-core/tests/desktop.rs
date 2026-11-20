//! The desktop tools end to end: a scripted model reads and types into a real TextEdit window
//! through the kernel and agent-desktop, without asking anyone, with a denied app and a ref used
//! on the wrong app.
//!
//! It operates the desktop, so it runs only with `HER_DESKTOP_E2E=1`, on macOS, with
//! agent-desktop installed and allowed to use accessibility.

#![allow(clippy::print_stderr)]

use her_core::HerHome;
use her_core::JsonlStore;
use her_core::Kernel;
use her_core::KernelDeps;
use her_core::StartOptions;
use her_llm::Catalog;
use her_llm::Message;
use her_llm::ModelRegistry;
use her_llm::RegistryOptions;
use her_llm::ThinkingLevel;
use her_llm::api::scripted::Script;
use her_llm::api::scripted::ScriptedApi;
use her_protocol::Event;
use her_protocol::EventMsg;
use her_protocol::Op;
use her_tools::BashTool;
use her_tools::LocalEnvironment;
use her_tools::builtin_registry;
use serde_json::json;
use std::process::Command;
use std::sync::Arc;
use std::time::Duration;
use std::time::Instant;
use tempfile::TempDir;

const TEXTEDIT: &str = "com.apple.TextEdit";

/// The tests share TextEdit's first document: one at a time.
static TEXTEDIT_TURN: tokio::sync::Mutex<()> = tokio::sync::Mutex::const_new(());

struct Harness {
    kernel: Kernel,
    api: Arc<ScriptedApi>,
    _dir: TempDir,
}

fn enabled() -> bool {
    if std::env::var("HER_DESKTOP_E2E").as_deref() != Ok("1") {
        eprintln!("set HER_DESKTOP_E2E=1 to run the desktop tests; skipping");
        return false;
    }
    if !cfg!(target_os = "macos") || her_desktop::locate(None).is_err() {
        eprintln!("needs macOS and agent-desktop; skipping");
        return false;
    }
    true
}

fn osascript(script: &str) -> String {
    let out = Command::new("osascript").args(["-e", script]).output().unwrap();
    String::from_utf8_lossy(&out.stdout).trim().to_string()
}

async fn harness() -> Harness {
    let dir = tempfile::tempdir().unwrap();
    let work = dir.path().join("work");
    std::fs::create_dir_all(&work).unwrap();
    let home = HerHome::new(dir.path().join("home"));
    home.ensure().unwrap();
    let user = home.config_file();
    std::fs::write(&user, "[desktop]\nenabled = true\n[desktop.apps]\n\"com.apple.finder\" = \"deny\"\n").unwrap();
    let files = her_core::config::LayerFiles {
        user: &user,
        project: None,
        user_project: None,
        project_root: None,
    };
    let config = her_core::config::load_layers(&files, true, &[]).unwrap().config;
    let api = ScriptedApi::new();
    let mut registry = ModelRegistry::new(Catalog::default(), RegistryOptions::default());
    registry.register_api(api.clone());
    registry.insert_provider(ScriptedApi::provider());
    let deps = KernelDeps {
        llm: Arc::new(registry),
        tools: Arc::new(builtin_registry(BashTool::default())),
        env: Arc::new(LocalEnvironment::new(work.clone())),
        store: Arc::new(JsonlStore::new(home.sessions_dir())),
        hooks: vec![],
        mcp_servers: Vec::new(),
        browser_pool: None,
        docs: None,
        decks: None,
        words: None,
        routines: None,
        connectors: None,
        mesh: None,
    };
    let options = StartOptions {
        model: "test/scripted".into(),
        thinking: ThinkingLevel::Off,
        cwd: work,
        home,
        config: Arc::new(config),
        project_root: None,
        bot: None,
    };
    let kernel = Kernel::start(options, deps).await.unwrap();
    Harness {
        kernel,
        api,
        _dir: dir,
    }
}

/// The tool results of the last request to the model, since the last prompt.
fn last_results(api: &ScriptedApi) -> Vec<String> {
    let requests = api.requests();
    let messages = &requests.last().unwrap().messages;
    let prompt = messages.iter().rposition(|m| matches!(m, Message::User(_))).unwrap_or(0);
    messages[prompt..]
        .iter()
        .filter_map(|m| match m {
            Message::ToolResult(r) => Some(r.text_content()),
            _ => None,
        })
        .collect()
}

/// The ref of the first line in `text` that contains `what`.
fn ref_of(text: &str, what: &str) -> String {
    let line = text
        .lines()
        .find(|l| l.contains(what) && l.contains("[ref="))
        .unwrap_or_else(|| panic!("no ref for {what} in:\n{text}"));
    let start = line.find("[ref=").unwrap() + 5;
    line[start..].split(']').next().unwrap().to_string()
}

async fn until(kernel: &mut Kernel, stop: impl Fn(&EventMsg) -> bool) -> Vec<Event> {
    let mut events = Vec::new();
    let deadline = Instant::now() + Duration::from_secs(60);
    loop {
        let remaining = deadline.saturating_duration_since(Instant::now());
        let event = tokio::time::timeout(remaining, kernel.next_event())
            .await
            .expect("timed out waiting for an event")
            .expect("the session ended");
        let done = stop(&event.msg);
        events.push(event);
        if done {
            return events;
        }
    }
}

/// What the model is told of computer use: any app, a browser app among them, with no word
/// against web pages in its catalog line or in the guidelines tool_search gives. Operates
/// nothing, so it runs everywhere.
#[tokio::test]
async fn computer_use_is_offered_for_any_app() {
    let mut h = harness().await;
    h.api.push(Script::tool_call("c1", "tool_search", json!({ "query": "cua" })));
    h.api.push(Script::text("found"));
    h.kernel.submit(Op::prompt("look at my Chrome extensions")).await.unwrap();
    until(&mut h.kernel, |m| matches!(m, EventMsg::TaskCompleted { .. })).await;
    let system = h.api.requests()[0].system.clone();
    assert!(system.contains("- cua: Computer use: see and operate any of the apps on this computer"), "{system}");
    let found = last_results(&h.api).join("\n");
    assert!(found.contains("cua_launch") && found.contains("Desktop apps are data"), "definitions and guidelines: {found}");
    let lower = found.to_lowercase();
    assert!(!lower.contains("browser") && !lower.contains("web page"), "{found}");
}

#[tokio::test]
async fn reads_and_types_into_textedit_without_asking() {
    if !enabled() {
        return;
    }
    let _turn = TEXTEDIT_TURN.lock().await;
    // Document 1 is the new one, focused, with TextEdit at the front.
    osascript("tell application \"TextEdit\" to make new document");
    osascript("tell application \"TextEdit\" to activate");
    tokio::time::sleep(Duration::from_secs(1)).await;
    let mut h = harness().await;

    // 1. Listing apps and reading TextEdit, named in any case, run at once.
    h.api.push(
        Script::tool_call("c1", "cua_apps", json!({}))
            .then_tool_call("c2", "cua_snapshot", json!({ "app": "com.apple.textedit" })),
    );
    h.api.push(Script::text("read"));
    h.kernel.submit(Op::prompt("look at my TextEdit window")).await.unwrap();
    until(&mut h.kernel, |m| matches!(m, EventMsg::TaskCompleted { .. })).await;
    let results = last_results(&h.api);
    assert!(results[0].contains(TEXTEDIT), "{}", results[0]);
    let mut window = results[1].clone();
    let mut snapshot = json!({ "app": TEXTEDIT });
    // With other TextEdit windows open, the result names them; retry on ours, as a model would.
    if window.contains("AMBIGUOUS_TARGET") {
        let ours = format!("\"{}\"", osascript("tell application \"TextEdit\" to get name of document 1"));
        let id = window
            .split(", ")
            .find(|w| w.ends_with(&ours) || w.ends_with(&format!("{ours}.")))
            .and_then(|w| w.split_whitespace().rev().nth(1))
            .unwrap_or_else(|| panic!("our window {ours} is not listed: {window}"))
            .to_string();
        snapshot["window_id"] = json!(id);
        h.api.push(Script::tool_call("c2b", "cua_snapshot", snapshot.clone()));
        h.api.push(Script::text("read"));
        h.kernel.submit(Op::prompt("that window")).await.unwrap();
        until(&mut h.kernel, |m| matches!(m, EventMsg::TaskCompleted { .. })).await;
        window = last_results(&h.api).pop().unwrap();
    }
    assert!(window.contains("<app-content") && window.contains("textfield"), "{window}");
    let field = ref_of(&window, "textfield");

    // 2. Reading again and acting run at once too.
    let typed = "hello from the her desktop e2e";
    h.api.push(
        Script::tool_call("c3", "cua_snapshot", snapshot.clone())
            .then_tool_call("c4", "cua_act", json!({ "app": TEXTEDIT, "action": "set_value", "ref": field, "text": typed })),
    );
    h.api.push(Script::text("typed"));
    h.kernel.submit(Op::prompt("write a line")).await.unwrap();
    until(&mut h.kernel, |m| matches!(m, EventMsg::TaskCompleted { .. })).await;
    let results = last_results(&h.api);
    assert!(results[1].contains("done") || results[1].contains("sent"), "{}", results[1]);
    assert_eq!(osascript("tell application \"TextEdit\" to get text of document 1"), typed);

    // 3. A denied app and a ref used on another app fail.
    let finder_ref = field.clone();
    h.api.push(
        Script::tool_call("c5", "cua_act", json!({ "app": TEXTEDIT, "action": "press", "text": "cmd+a" }))
            .then_tool_call("c6", "cua_snapshot", json!({ "app": "com.apple.finder" }))
            .then_tool_call("c7", "cua_act", json!({ "app": "com.apple.finder", "action": "click", "ref": finder_ref })),
    );
    h.api.push(Script::text("done"));
    h.kernel.submit(Op::prompt("more")).await.unwrap();
    until(&mut h.kernel, |m| matches!(m, EventMsg::TaskCompleted { .. })).await;
    let results = last_results(&h.api);
    assert!(results[0].contains("press"), "{}", results[0]);
    assert!(results[1].contains("denied"), "{}", results[1]);
    assert!(results[2].contains("not com.apple.finder"), "{}", results[2]);

    osascript("tell application \"TextEdit\" to close document 1 saving no");
}

fn clipboard() -> String {
    String::from_utf8_lossy(&Command::new("pbpaste").output().unwrap().stdout).to_string()
}

fn frontmost() -> String {
    osascript("tell application \"System Events\" to get bundle identifier of first process whose frontmost is true")
}

#[tokio::test]
async fn types_and_presses_into_textedit_in_the_background() {
    if !enabled() {
        return;
    }
    let _turn = TEXTEDIT_TURN.lock().await;
    // TextEdit's new document has the focus inside TextEdit, while another app is at the front.
    osascript("tell application \"TextEdit\" to make new document");
    // A TextEdit that was not running comes to the front once it has started.
    tokio::time::sleep(Duration::from_secs(1)).await;
    osascript("tell application \"Finder\" to activate");
    tokio::time::sleep(Duration::from_millis(500)).await;
    let front = frontmost();
    assert_ne!(front, TEXTEDIT, "another app must be at the front");
    let clip = clipboard();
    let mut h = harness().await;

    let act = |action: &str, text: &str| json!({ "app": TEXTEDIT, "action": action, "text": text });
    h.api.push(
        Script::tool_call("c1", "cua_act", act("type_text", "你好 background"))
            .then_tool_call("c2", "cua_act", act("press", "return"))
            .then_tool_call("c3", "cua_act", act("type_text", "second")),
    );
    h.api.push(Script::text("typed"));
    h.kernel.submit(Op::prompt("write two lines")).await.unwrap();
    until(&mut h.kernel, |m| matches!(m, EventMsg::TaskCompleted { .. })).await;
    let results = last_results(&h.api);
    assert!(results[0].contains("type_text: done, and the app confirmed it."), "{}", results[0]);
    assert!(results[1].contains("press: sent to the app, whose window stayed where it was."), "{}", results[1]);
    assert!(results[2].contains("type_text: done"), "{}", results[2]);
    // The key reached the field before the text that followed it.
    assert!(results[2].contains(r#"你好 background\nsecond"."#), "{}", results[2]);
    // The user may bring another app forward meanwhile; TextEdit is never brought.
    assert_ne!(frontmost(), TEXTEDIT, "TextEdit stayed behind");
    assert_eq!(clipboard(), clip, "the clipboard was not touched");

    osascript("tell application \"TextEdit\" to close (every document whose text contains \"你好 background\") saving no");
}
