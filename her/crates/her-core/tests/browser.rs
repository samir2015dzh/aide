//! The browser tools end to end: a scripted model drives a real Chromium through the kernel,
//! without asking anyone, with the refusals of the configuration and the check that a call runs
//! only on the page it was judged for (docs/kernel/browser-design.md §11.2). Skipped where no
//! browser is installed.
//!
//! The pages are served on 127.0.0.1, but under the name `shop.test`: the browser maps it
//! there (a wrapper adds `--host-resolver-rules`), while the kernel cannot resolve it and so
//! treats it as an ordinary public site.

#![allow(clippy::print_stderr)]

use her_core::BrowserPool;
use her_core::HerHome;
use her_core::JsonlStore;
use her_core::Kernel;
use her_core::KernelDeps;
use her_core::StartOptions;
use her_core::TaskEnd;
use her_llm::Catalog;
use her_llm::Message;
use her_llm::ModelRegistry;
use her_llm::RegistryOptions;
use her_llm::ThinkingLevel;
use her_llm::UserContent;
use her_llm::api::scripted::Script;
use her_llm::api::scripted::ScriptedApi;
use her_protocol::EventMsg;
use her_tools::BashTool;
use her_tools::LocalEnvironment;
use her_tools::builtin_registry;
use serde_json::json;
use std::collections::HashMap;
use std::path::Path;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;
use tempfile::TempDir;
use tokio::io::AsyncReadExt;
use tokio::io::AsyncWriteExt;
use tokio::net::TcpListener;

/// Serves `pages` by path on 127.0.0.1; returns the port.
async fn serve(pages: HashMap<&'static str, &'static str>) -> u16 {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let port = listener.local_addr().unwrap().port();
    let pages = Arc::new(pages);
    tokio::spawn(async move {
        loop {
            let Ok((mut stream, _)) = listener.accept().await else { break };
            let pages = pages.clone();
            tokio::spawn(async move {
                let mut buf = vec![0u8; 8192];
                let n = stream.read(&mut buf).await.unwrap_or(0);
                let request = String::from_utf8_lossy(&buf[..n]).to_string();
                let target = request.split_whitespace().nth(1).unwrap_or("/");
                let (path, query) = target.split_once('?').unwrap_or((target, ""));
                if path == "/beat" {
                    *BEATS.lock().unwrap().entry((port, query.to_string())).or_default() += 1;
                }
                let (status, body) = match pages.get(path) {
                    Some(body) => ("200 OK", *body),
                    None => ("404 Not Found", "<title>missing</title>"),
                };
                let response = format!(
                    "HTTP/1.1 {status}\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                    body.len()
                );
                let _ = stream.write_all(response.as_bytes()).await;
            });
        }
    });
    port
}

/// Heartbeats of `/alive` pages, by server port and the page's query: a page beats while its
/// tab is open.
static BEATS: std::sync::LazyLock<std::sync::Mutex<HashMap<(u16, String), u64>>> = std::sync::LazyLock::new(Default::default);

fn beats(origin: &str, who: &str) -> u64 {
    let port: u16 = origin.rsplit(':').next().unwrap().parse().unwrap();
    BEATS.lock().unwrap().get(&(port, who.to_string())).copied().unwrap_or(0)
}

/// Whether the page of `who` is still beating: beats come within `window`.
async fn beating(origin: &str, who: &str, window: Duration) -> bool {
    let before = beats(origin, who);
    tokio::time::sleep(window).await;
    beats(origin, who) > before
}

fn pages() -> HashMap<&'static str, &'static str> {
    let mut pages = HashMap::new();
    pages.insert("/beat", "");
    pages.insert(
        "/alive",
        "<title>Alive</title><h1>Alive</h1><script>setInterval(() => fetch('/beat' + location.search), 150)</script>",
    );
    pages.insert(
        "/login",
        r#"<title>Sign in</title><h1>Sign in</h1>
        <form action="/welcome" method="get">
          <label>Email <input name="email" type="email"></label>
          <label>Password <input name="password" type="password"></label>
          <button type="submit">Continue</button>
        </form>"#,
    );
    pages.insert("/welcome", "<title>Welcome</title><h1>Welcome back</h1><p>Signed in.</p>");
    pages.insert("/other", "<title>Other</title><h1>Another page</h1><button>Nothing</button>");
    pages
}

/// A script that runs the installed browser with `shop.test` mapped to 127.0.0.1.
fn wrapper(dir: &Path) -> Option<PathBuf> {
    let browser = her_browser::launch::find_executable()?;
    if cfg!(windows) {
        return None;
    }
    let path = dir.join("browser.sh");
    std::fs::write(
        &path,
        format!(
            "#!/bin/sh\nexec \"{}\" --host-resolver-rules=\"MAP *.test 127.0.0.1\" --no-proxy-server \"$@\"\n",
            browser.display()
        ),
    )
    .unwrap();
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o755)).unwrap();
    }
    Some(path)
}

struct Harness {
    kernel: Kernel,
    api: Arc<ScriptedApi>,
    origin: String,
    /// The user configuration file.
    config: PathBuf,
    _dir: TempDir,
}

/// A session with the browser tools on, or `None` without a browser on this machine.
async fn harness(sites: &[(&str, &str)]) -> Option<Harness> {
    harness_with(sites, "").await
}

/// `extra`: more lines for `[browser]`.
async fn harness_with(sites: &[(&str, &str)], extra: &str) -> Option<Harness> {
    harness_config(sites, extra, true).await
}

/// `enabled`: the built-in browser is on from the start.
async fn harness_config(sites: &[(&str, &str)], extra: &str, enabled: bool) -> Option<Harness> {
    let dir = tempfile::tempdir().unwrap();
    let Some(executable) = wrapper(dir.path()) else {
        eprintln!("no Chromium-family browser installed; skipping");
        return None;
    };
    let port = serve(pages()).await;
    let origin = format!("http://shop.test:{port}");
    let work = dir.path().join("work");
    std::fs::create_dir_all(&work).unwrap();
    let home = HerHome::new(dir.path().join("home"));
    home.ensure().unwrap();
    let user = home.config_file();
    let mut toml = format!(
        "[browser]\nenabled = {enabled}\nheadless = true\nexecutable = \"{}\"\n{extra}\n[browser.sites]\n",
        executable.display()
    );
    for (site, level) in sites {
        toml.push_str(&format!("\"{}\" = \"{level}\"\n", site.replace("ORIGIN", &origin)));
    }
    std::fs::write(&user, toml).unwrap();
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
        browser_pool: Some(Arc::new(BrowserPool::new(home.browser_dir(), false))),
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
    Some(Harness {
        kernel,
        api,
        origin,
        config: user,
        _dir: dir,
    })
}

/// Runs a task to its end; fails after 60 seconds.
async fn run(kernel: &mut Kernel, text: &str) -> her_core::TaskResult {
    let input = vec![UserContent::Text { text: text.into() }];
    tokio::time::timeout(Duration::from_secs(60), kernel.run(input))
        .await
        .unwrap_or_else(|_| panic!("\"{text}\" did not finish"))
        .unwrap()
}

/// The tool results of the last request to the model.
fn last_results(api: &ScriptedApi) -> Vec<String> {
    let requests = api.requests();
    let last = requests.last().unwrap();
    let results: Vec<String> = last
        .messages
        .iter()
        .filter_map(|m| match m {
            Message::ToolResult(r) => Some(r.text_content()),
            _ => None,
        })
        .collect();
    results
}

/// The ref of the line in `text` that contains `what`.
fn ref_of(text: &str, what: &str) -> String {
    let line = text
        .lines()
        .find(|l| l.contains(what) && l.contains("[ref="))
        .unwrap_or_else(|| panic!("no ref for {what} in:\n{text}"));
    let start = line.find("[ref=").unwrap() + 5;
    line[start..].split(']').next().unwrap().to_string()
}

#[tokio::test]
async fn the_tools_act_without_asking_and_only_on_the_page_judged() {
    let Some(mut h) = harness(&[]).await else {
        return;
    };
    let login = format!("{}/login", h.origin);

    // 1. Opening a site nobody configured runs at once.
    h.api.push(Script::tool_call("c1", "browser_navigate", json!({ "url": login })));
    h.api.push(Script::text("opened"));
    run(&mut h.kernel, "sign in").await;
    let page = last_results(&h.api).pop().unwrap();
    assert!(page.contains("Sign in") && page.contains("<page-content"), "{page}");
    assert!(page.contains("[type=password]") || page.contains("Password"), "{page}");
    let email = ref_of(&page, "Email");
    let password = ref_of(&page, "Password");
    let button = ref_of(&page, "Continue");

    // 2. Reading and acting run at once too.
    h.api.push(
        Script::tool_call("c2", "browser_snapshot", json!({ "query": "continue" }))
            .then_tool_call("c3", "browser_act", json!({ "action": "fill", "ref": email, "text": "ada@example.com" })),
    );
    h.api.push(Script::text("filled"));
    run(&mut h.kernel, "fill it").await;
    let results = last_results(&h.api);
    let found = &results[results.len() - 2];
    assert!(found.contains("Continue") && !found.contains("Email"), "query: {found}");
    assert!(results.last().unwrap().contains("ada@example.com"), "{}", results.last().unwrap());

    // 3. Typing a password and submitting: the password does not show, and the click lands on
    // the next page.
    h.api.push(
        Script::tool_call("c4", "browser_act", json!({ "action": "fill", "ref": password, "text": "hunter2" }))
            .then_tool_call("c5", "browser_act", json!({ "action": "click", "ref": button })),
    );
    h.api.push(Script::text("signed in"));
    let result = run(&mut h.kernel, "submit").await;
    assert!(matches!(result.end, TaskEnd::Completed { .. }));
    let results = last_results(&h.api);
    let filled = &results[results.len() - 2];
    assert!(!filled.contains("hunter2"), "{filled}");
    let clicked = results.last().unwrap();
    assert!(clicked.contains("Welcome back"), "{clicked}");

    // 4. An old ref after the navigation is refused before anything is sent.
    h.api.push(Script::tool_call("c6", "browser_act", json!({ "action": "click", "ref": button })));
    h.api.push(Script::text("stale"));
    run(&mut h.kernel, "click again").await;
    let stale = last_results(&h.api).pop().unwrap();
    assert!(stale.contains("navigated"), "{stale}");

    // 5. Navigate and act in one reply: the act was judged for the page before the
    // navigation, so it sends nothing.
    let other = format!("{}/other", h.origin);
    let current = {
        h.api.push(Script::tool_call("c8", "browser_navigate", json!({ "url": login })));
        h.api.push(Script::text("back at login"));
        run(&mut h.kernel, "login again").await;
        last_results(&h.api).pop().unwrap()
    };
    let continue_now = ref_of(&current, "Continue");
    h.api.push(
        Script::tool_call("c9", "browser_navigate", json!({ "url": other }))
            .then_tool_call("c10", "browser_act", json!({ "action": "click", "ref": continue_now })),
    );
    h.api.push(Script::text("raced"));
    let result = run(&mut h.kernel, "race").await;
    let results = last_results(&h.api);
    assert!(results[results.len() - 2].contains("Another page"), "{}", results[results.len() - 2]);
    let raced = results.last().unwrap();
    assert!(raced.contains("nothing was sent") || raced.contains("navigated"), "{raced}");

    // 6. Tabs: listed, and a details payload for user interfaces.
    let details = result
        .events
        .iter()
        .find_map(|e| match &e.msg {
            EventMsg::ToolCompleted { result, .. } if result.tool_name == "browser_navigate" => result.details.clone(),
            _ => None,
        })
        .expect("details");
    let details: serde_json::Value = serde_json::from_str(details.as_str()).unwrap();
    assert_eq!(details["url"], json!(other), "{details}");
    assert_eq!(details["title"], json!("Other"));
    h.api.push(Script::tool_call("c11", "browser_tabs", json!({})));
    h.api.push(Script::text("listed"));
    run(&mut h.kernel, "tabs").await;
    let listing = last_results(&h.api).pop().unwrap();
    assert!(listing.contains("(current)") && listing.contains("/other"), "{listing}");
    h.kernel.shutdown().await;
}

#[tokio::test]
async fn denied_sites_and_metadata_addresses_are_refused() {
    let Some(mut h) = harness(&[("ORIGIN", "deny")]).await else {
        return;
    };
    let login = format!("{}/login", h.origin);
    h.api.push(
        Script::tool_call("c1", "browser_navigate", json!({ "url": login }))
            .then_tool_call("c2", "browser_navigate", json!({ "url": "http://169.254.169.254/latest/meta-data" })),
    );
    h.api.push(Script::text("refused"));
    run(&mut h.kernel, "open").await;
    let results = last_results(&h.api);
    assert!(results[0].contains("denied by browser.sites"), "{}", results[0]);
    assert!(results[1].contains("cloud metadata"), "{}", results[1]);
    // Reading before anything is open says so.
    h.api.push(Script::tool_call("c3", "browser_snapshot", json!({})));
    h.api.push(Script::text("nothing"));
    run(&mut h.kernel, "read").await;
    assert!(last_results(&h.api).pop().unwrap().contains("No page is open"));
    h.kernel.shutdown().await;
}

#[tokio::test]
async fn a_batch_runs_its_steps_and_stops_where_one_is_refused() {
    // A configured site: every step runs, the form is filled and sent in one call.
    let Some(mut h) = harness(&[("ORIGIN", "act")]).await else {
        return;
    };
    let login = format!("{}/login", h.origin);
    h.api.push(Script::tool_call("c1", "browser_navigate", json!({ "url": login })));
    h.api.push(Script::text("opened"));
    run(&mut h.kernel, "open").await;
    let page = last_results(&h.api).pop().unwrap();
    let steps = json!([
        { "tool": "browser_act", "args": { "action": "fill", "ref": ref_of(&page, "Email"), "text": "ada@example.com" } },
        { "tool": "browser_act", "args": { "action": "fill", "ref": ref_of(&page, "Password"), "text": "hunter2" } },
        { "tool": "browser_act", "args": { "action": "click", "ref": ref_of(&page, "Continue") } },
        { "tool": "browser_snapshot", "args": { "query": "welcome" } },
    ]);
    h.api.push(Script::tool_call("c2", "browser_batch", json!({ "steps": steps })));
    h.api.push(Script::text("sent"));
    run(&mut h.kernel, "sign in").await;
    let text = last_results(&h.api).pop().unwrap();
    assert!(text.contains("Step 4 of 4 (browser_snapshot)") && text.contains("Welcome back"), "{text}");
    assert!(!text.contains("value=\"hunter2\""), "{text}");
    h.kernel.shutdown().await;

    // A site nobody configured: every step runs; a later step that is refused stops the batch.
    let Some(mut h) = harness(&[]).await else {
        return;
    };
    let other = format!("{}/other", h.origin);
    let steps = json!([
        { "tool": "browser_navigate", "args": { "url": other } },
        { "tool": "browser_navigate", "args": { "url": "http://169.254.169.254/latest" } },
        { "tool": "browser_snapshot" },
    ]);
    h.api.push(Script::tool_call("c1", "browser_batch", json!({ "steps": steps })));
    h.api.push(Script::text("stopped"));
    run(&mut h.kernel, "go").await;
    let text = last_results(&h.api).pop().unwrap();
    // Steps before the last report briefly: where the page is, not its snapshot.
    assert!(text.contains("Page: \"Other\"") && !text.contains("Another page"), "the first step ran: {text}");
    assert!(text.contains("Take a snapshot to see the page."), "{text}");
    assert!(text.contains("Step 2 (browser_navigate) was refused") && text.contains("1 of 3 steps ran"), "{text}");

    // Batches do not nest, and an unknown tool fails before anything runs.
    h.api.push(Script::tool_call("c2", "browser_batch", json!({ "steps": [{ "tool": "browser_batch" }] })));
    h.api.push(Script::text("no"));
    run(&mut h.kernel, "nest").await;
    assert!(last_results(&h.api).pop().unwrap().contains("cannot be batched"));
    h.kernel.shutdown().await;
}

#[tokio::test]
async fn scripts_run_only_when_enabled() {
    let Some(mut h) = harness_with(&[], "script_enabled = false").await else {
        return;
    };
    let other = format!("{}/other", h.origin);
    h.api.push(Script::tool_call("c1", "browser_navigate", json!({ "url": other })));
    h.api.push(Script::text("opened"));
    run(&mut h.kernel, "open").await;
    h.api.push(Script::tool_call("c2", "browser_eval", json!({ "expression": "1 + 1" })));
    h.api.push(Script::text("no tool"));
    run(&mut h.kernel, "eval").await;
    assert!(last_results(&h.api).pop().unwrap().contains("Unknown tool"), "turned off: not even listed");
    h.kernel.shutdown().await;

    // On by default (browser-design §10.1): scripts run without asking.
    let Some(mut h) = harness(&[]).await else {
        return;
    };
    let other = format!("{}/other", h.origin);
    h.api.push(Script::tool_call("c1", "browser_navigate", json!({ "url": other })));
    h.api.push(Script::text("opened"));
    run(&mut h.kernel, "open").await;
    h.api.push(Script::tool_call(
        "c2",
        "browser_eval",
        json!({ "expression": "document.querySelector('h1').textContent = 'Changed'; ({ title: document.title, n: 6 * 7 })" }),
    ));
    h.api.push(Script::text("ran"));
    run(&mut h.kernel, "eval").await;
    let text = last_results(&h.api).pop().unwrap();
    assert!(text.contains("\"n\": 42") && text.contains("\"title\": \"Other\""), "{text}");
    assert!(text.contains("Changed"), "the change shows: {text}");
    h.kernel.shutdown().await;

}

#[tokio::test]
async fn tabs_close_when_the_session_ends() {
    let Some(mut h) = harness(&[("ORIGIN", "act")]).await else {
        return;
    };
    let alive = format!("{}/alive?end", h.origin);
    h.api.push(Script::tool_call("c1", "browser_navigate", json!({ "url": alive })));
    h.api.push(Script::text("open"));
    run(&mut h.kernel, "open").await;
    assert!(beating(&h.origin, "end", Duration::from_secs(1)).await, "the page runs");
    h.kernel.shutdown().await;
    // Let a beat already on its way arrive, then nothing more.
    tokio::time::sleep(Duration::from_millis(500)).await;
    assert!(!beating(&h.origin, "end", Duration::from_millis(1500)).await, "the tab closed with the session");
}

#[tokio::test]
async fn a_sub_agents_tabs_close_when_it_reports_and_the_main_sessions_stay() {
    let Some(mut h) = harness(&[("ORIGIN", "act")]).await else {
        return;
    };
    let main = format!("{}/alive?main", h.origin);
    let child = format!("{}/alive?child", h.origin);
    // The main session opens a page, then waits for an agent that opens one of its own.
    h.api.push(Script::tool_call("c1", "browser_navigate", json!({ "url": main })));
    h.api.push(Script::tool_call("c2", "spawn_agent", json!({ "description": "open the page", "task": "open the page", "name": "web", "wait": true })));
    h.api.push(Script::tool_call("k1", "browser_navigate", json!({ "url": child })));
    // While the agent still works, its page runs beside the main one.
    h.api.push(Script::tool_call("k2", "browser_wait", json!({ "seconds": 1 })));
    h.api.push(Script::text("opened it"));
    h.api.push(Script::text("the agent is done"));
    let result = run(&mut h.kernel, "go").await;
    assert!(matches!(result.end, TaskEnd::Completed { .. }), "{:?}", result.end);
    let all: Vec<String> = h
        .api
        .requests()
        .iter()
        .flat_map(|r| r.messages.iter().filter_map(|m| match m {
            Message::ToolResult(r) => Some(format!("{}: {}", r.tool_name, r.text_content())),
            _ => None,
        }))
        .collect();
    assert!(beats(&h.origin, "child") > 0, "the agent's page ran: {all:#?}");
    tokio::time::sleep(Duration::from_millis(500)).await;
    assert!(!beating(&h.origin, "child", Duration::from_millis(1500)).await, "the agent's tab closed when it reported");
    assert!(beating(&h.origin, "main", Duration::from_millis(1500)).await, "the main session's tab is still open");
    // The main session still sees only its own tab.
    h.api.push(Script::tool_call("c3", "browser_tabs", json!({})));
    h.api.push(Script::text("listed"));
    run(&mut h.kernel, "tabs").await;
    let listing = last_results(&h.api).pop().unwrap();
    assert!(listing.contains("1 tab:") && listing.contains("alive?main"), "{listing}");
    h.kernel.shutdown().await;
}

#[tokio::test]
async fn a_sub_agent_opens_a_site_without_asking() {
    let Some(mut h) = harness(&[]).await else {
        return;
    };
    let login = format!("{}/login", h.origin);
    h.api.push(Script::tool_call("c1", "spawn_agent", json!({ "description": "open the page", "task": "open the page", "name": "web", "wait": true })));
    h.api.push(Script::tool_call("k1", "browser_navigate", json!({ "url": login })));
    h.api.push(Script::text("opened it"));
    h.api.push(Script::tool_call("c2", "browser_navigate", json!({ "url": login })));
    h.api.push(Script::text("done"));
    let result = run(&mut h.kernel, "go").await;
    assert!(matches!(result.end, TaskEnd::Completed { .. }), "{:?}", result.end);
    let page = last_results(&h.api).pop().unwrap();
    assert!(page.contains("Sign in"), "{page}");
    h.kernel.shutdown().await;
}

/// The browser settings as the user configuration file has them now, as the service hands them
/// to an open session when the file changes (connectors-entry-design §9.4).
fn settings_from(file: &Path) -> her_core::SystemSettings {
    let files = her_core::config::LayerFiles {
        user: file,
        project: None,
        user_project: None,
        project_root: None,
    };
    her_core::SystemSettings::of(&her_core::config::load_layers(&files, true, &[]).unwrap().config)
}

fn rewrite(file: &Path, from: &str, to: &str) {
    let text = std::fs::read_to_string(file).unwrap();
    assert!(text.contains(from), "{text}");
    std::fs::write(file, text.replace(from, to)).unwrap();
}

fn tool_names(api: &ScriptedApi) -> Vec<String> {
    api.requests().last().unwrap().tools.iter().map(|t| t.name.clone()).collect()
}

/// The browser turned on while a session is open: from its next request the catalog lists the
/// browser group, tool_search gives its definitions and guidelines, and call_tool runs them; the
/// tool list itself only gains the two search tools, and the frozen part of the prompt stays.
/// Turned off again, the group goes and the session's tabs close (connectors-entry-design §9.4,
/// computer-use-design §5).
#[tokio::test]
async fn the_browser_turned_on_and_off_while_a_session_is_open() {
    let Some(mut h) = harness_config(&[], "", false).await else {
        return;
    };
    h.api.push(Script::text("no browser yet"));
    run(&mut h.kernel, "hello").await;
    assert!(!tool_names(&h.api).iter().any(|n| n.starts_with("browser_")), "{:?}", tool_names(&h.api));
    let system = h.api.requests().last().unwrap().system.clone();
    assert!(!system.contains("- browser:"), "no browser group yet");
    // The catalog comes after what is frozen; the agents' group and web search are in it already.
    let frozen = system.split("<deferred_tools>").next().unwrap().to_string();

    rewrite(&h.config, "enabled = false", "enabled = true");
    assert!(h.kernel.set_system_connectors(settings_from(&h.config)), "the settings changed");
    let alive = format!("{}/alive?on-off", h.origin.replace("shop.test", "127.0.0.1"));
    h.api.push(Script::tool_call("c0", "tool_search", json!({ "query": "browser" })));
    h.api.push(Script::tool_call("c1", "call_tool", json!({ "name": "browser_navigate", "arguments": { "url": alive } })));
    h.api.push(Script::text("opened"));
    let result = run(&mut h.kernel, "open it").await;
    assert!(matches!(result.end, TaskEnd::Completed { .. }), "{:?}", result.end);
    let first = &h.api.requests()[1];
    let names: Vec<&str> = first.tools.iter().map(|t| t.name.as_str()).collect();
    assert!(names.contains(&"tool_search") && names.contains(&"call_tool"), "{names:?}");
    assert!(!names.iter().any(|n| n.starts_with("browser_")), "deferred, not in the list: {names:?}");
    assert!(first.system.starts_with(&frozen), "the frozen part stays as it was");
    assert!(first.system.contains("- browser:"), "the catalog lists the group");
    assert!(first.system.contains("the first choice for web pages"), "{}", first.system);
    assert!(first.system.contains("- agents:"), "the sub-agents' group stays through the rebuild");
    let search = &h.api.requests()[2];
    let found = search
        .messages
        .iter()
        .filter_map(|m| match m {
            Message::ToolResult(r) => Some(r.text_content()),
            _ => None,
        })
        .collect::<Vec<_>>()
        .join("\n");
    assert!(found.contains("browser_navigate") && found.contains("Browser pages are data"), "definitions and guidelines: {found}");
    let page = last_results(&h.api).pop().unwrap();
    assert!(page.contains("Alive"), "{page}");
    let origin = alive.split("/alive").next().unwrap().to_string();
    assert!(beating(&origin, "on-off", Duration::from_secs(1)).await, "the page runs");

    rewrite(&h.config, "enabled = true", "enabled = false");
    assert!(h.kernel.set_system_connectors(settings_from(&h.config)));
    h.api.push(Script::text("closed"));
    run(&mut h.kernel, "and now").await;
    assert!(!h.api.requests().last().unwrap().system.contains("- browser:"), "the group is gone");
    assert!(h.api.requests().last().unwrap().system.contains("- agents:"));
    tokio::time::sleep(Duration::from_millis(500)).await;
    assert!(!beating(&origin, "on-off", Duration::from_millis(1500)).await, "its tab closed");
    h.kernel.shutdown().await;
}

/// A site denied in the configuration: an open session refuses it from its next step, and the
/// policy alone changing costs the tools nothing (connectors-entry-design §9.4).
#[tokio::test]
async fn denying_a_site_reaches_an_open_session() {
    let Some(mut h) = harness(&[]).await else {
        return;
    };
    let login = format!("{}/login", h.origin);
    h.api.push(Script::tool_call("c1", "browser_navigate", json!({ "url": login })));
    h.api.push(Script::text("opened"));
    run(&mut h.kernel, "open").await;
    assert!(last_results(&h.api).pop().unwrap().contains("Sign in"));
    let tools_before = tool_names(&h.api);

    rewrite(&h.config, "[browser.sites]\n", &format!("[browser.sites]\n\"{}\" = \"deny\"\n", h.origin));
    assert!(h.kernel.set_system_connectors(settings_from(&h.config)));
    let other = format!("{}/other", h.origin);
    h.api.push(Script::tool_call("c2", "browser_navigate", json!({ "url": other })));
    h.api.push(Script::text("refused"));
    run(&mut h.kernel, "again").await;
    assert!(last_results(&h.api).pop().unwrap().contains("denied by browser.sites"));
    assert_eq!(tool_names(&h.api), tools_before, "the policy alone changes no tool");
    h.kernel.shutdown().await;
}

/// A browser tool run through call_tool is judged as itself, and the tool started says what it
/// does (computer-use-design §5).
#[tokio::test]
async fn a_browser_tool_through_call_tool_is_judged_as_itself() {
    let Some(mut h) = harness(&[]).await else {
        return;
    };
    let login = format!("{}/login", h.origin);
    h.api.push(Script::tool_call("c1", "call_tool", json!({ "name": "browser_navigate", "arguments": { "url": login } })));
    h.api.push(Script::text("opened"));
    let result = run(&mut h.kernel, "open the login page").await;
    let started = result.events.iter().find_map(|e| match &e.msg {
        EventMsg::ToolStarted { title, .. } => Some(title.clone()),
        _ => None,
    });
    assert_eq!(started.as_deref(), Some("open shop.test"), "the browser tool's own title");
    assert!(last_results(&h.api).pop().unwrap().contains("Sign in"), "the page opened");
    h.kernel.shutdown().await;
}
