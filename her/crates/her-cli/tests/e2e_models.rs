//! End-to-end: model lists and the public catalog through the real `her serve` and `her models`
//! (docs/kernel/model-catalog-design.md). The mock stands in for DeepSeek's model list and for
//! the public catalog; nothing leaves the machine.

mod common;

use common::Demux;
use common::SchemaCheck;
use common::TestEnv;
use common::native_init;
use common::native_prompt;
use serde_json::Value;
use serde_json::json;
use std::sync::atomic::Ordering;
use std::time::Duration;
use std::time::Instant;

fn provider(models: &Value, id: &str) -> Value {
    models["providers"].as_array().unwrap().iter().find(|p| p["id"] == id).cloned().unwrap_or(Value::Null)
}

fn model(models: &Value, id: &str) -> Value {
    models["models"].as_array().unwrap().iter().find(|m| m["id"] == id).cloned().unwrap_or(Value::Null)
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_new_key_brings_the_providers_models_and_the_menu_follows_the_user() {
    let env = TestEnv::new("").await;
    let schema = SchemaCheck::new(&["ModelsListResult", "AuthSetResult", "ModelsRefreshResult"]);
    let (_child, client, incoming) = env.spawn_rpc(&["serve", "--stdio"]);
    native_init(&client).await;
    let demux = Demux::spawn(incoming);

    // The service refreshes the public catalog in the background when it starts.
    let started = Instant::now();
    while env.mock.stats().catalog_fetches.load(Ordering::Relaxed) == 0 {
        assert!(started.elapsed() < Duration::from_secs(20), "the catalog was never fetched");
        tokio::time::sleep(Duration::from_millis(50)).await;
    }

    // No DeepSeek key yet: its models are not listed, the default model is.
    let models = client.request("models/list", json!({})).await.unwrap();
    schema.check("ModelsListResult", &models);
    assert_eq!(provider(&models, "deepseek")["credential"], "missing");
    assert!(provider(&models, "deepseek").get("listing").is_none());
    assert!(models["models"].as_array().unwrap().iter().all(|m| m["provider"] != "deepseek"));
    assert_eq!(model(&models, "mock/mock-1")["shown"], true);

    // A refused key is not saved.
    let err = client
        .request("auth/set", json!({"provider": "deepseek", "api_key": her_mock::REJECTED_KEY}))
        .await
        .unwrap_err();
    assert_eq!(err.code, -32021);
    assert!(err.message.contains("invalid"), "{}", err.message);
    let models = client.request("models/list", json!({})).await.unwrap();
    assert_eq!(provider(&models, "deepseek")["credential"], "missing");
    assert!(!env.home.path().join("credentials.toml").exists());

    // A good key is checked against the provider's list, saved, and brings its models.
    let lists_before = env.mock.stats().model_lists.load(Ordering::Relaxed);
    let set = client
        .request("auth/set", json!({"provider": "deepseek", "api_key": "sk-good-key"}))
        .await
        .unwrap();
    schema.check("AuthSetResult", &set);
    assert_eq!(set, json!({"verified": true, "models": 3}));
    assert!(env.mock.stats().model_lists.load(Ordering::Relaxed) > lists_before);
    let models = client.request("models/list", json!({})).await.unwrap();
    schema.check("ModelsListResult", &models);
    let deepseek = provider(&models, "deepseek");
    assert_eq!(deepseek["credential"], "stored");
    assert_eq!(deepseek["listing"]["source"], "provider");
    assert!(deepseek["listing"]["fetched_at"].is_string());
    // Listed and described by the catalog: shown, with the catalog's window.
    let flash = model(&models, "deepseek/deepseek-flash");
    assert_eq!((&flash["shown"], &flash["known"], &flash["available"]), (&json!(true), &json!(true), &json!(true)));
    assert_eq!(flash["name"], "DeepSeek-V4.1-Flash", "the built-in correction names it");
    assert_eq!(flash["thinking_levels"], json!(["off", "low", "high", "max"]));
    // Listed but unknown to the catalog: there, hidden, default limits.
    let unknown = model(&models, "deepseek/mock-2");
    assert_eq!((&unknown["shown"], &unknown["known"]), (&json!(false), &json!(false)));
    // Written in builtin.toml though not in the list: still there.
    assert!(model(&models, "deepseek/deepseek-v4-pro").is_object());
    // The Anthropic endpoint is a provider of its own: its key is set separately.
    assert_eq!(provider(&models, "deepseek-anthropic")["credential"], "missing");
    assert!(models["models"].as_array().unwrap().iter().all(|m| m["provider"] != "deepseek-anthropic"));
    assert!(!models.to_string().contains("sk-good-key"), "keys never leave the service");

    // The user's menu: show the unknown model, hide the default one's sibling.
    client.request("models/show", json!({"model": "deepseek/mock-2", "shown": true})).await.unwrap();
    client.request("models/show", json!({"model": "deepseek/deepseek-flash", "shown": false})).await.unwrap();
    let models = client.request("models/list", json!({})).await.unwrap();
    assert_eq!(model(&models, "deepseek/mock-2")["shown"], true);
    assert_eq!(model(&models, "deepseek/deepseek-flash")["shown"], false);
    assert_eq!(provider(&models, "deepseek")["listing"]["shown"], 2);
    let selection: Value =
        serde_json::from_slice(&std::fs::read(env.home.path().join("models/selection.json")).unwrap()).unwrap();
    assert_eq!(selection["show"], json!(["deepseek/mock-2"]));
    assert_eq!(selection["hide"], json!(["deepseek/deepseek-flash"]));
    let err = client.request("models/show", json!({"model": "nope/x", "shown": true})).await.unwrap_err();
    assert_eq!(err.code, her_rpc::code::INVALID_PARAMS);

    // A manual refresh asks again and takes the catalog's newer details.
    let refreshed = client.request("models/refresh", json!({"provider": "deepseek", "catalog": true})).await.unwrap();
    schema.check("ModelsRefreshResult", &refreshed);
    assert_eq!(refreshed["providers"].as_array().unwrap().iter().find(|p| p["id"] == "deepseek").unwrap()["listing"]["refreshing"], false);
    let models = client.request("models/list", json!({})).await.unwrap();
    assert_eq!(model(&models, "deepseek/deepseek-flash")["context_window"], 777_777, "the mock catalog's window");
    let err = client.request("models/refresh", json!({"provider": "nope"})).await.unwrap_err();
    assert_eq!(err.code, her_rpc::code::INVALID_PARAMS);

    // A model only the provider's list knows works in a real session.
    let cwd = env.workspace("discovered");
    let created = client.request("session/new", json!({"cwd": cwd, "model": "deepseek/mock-2"})).await.unwrap();
    let session = created["session_id"].as_str().unwrap().to_string();
    assert_eq!(created["snapshot"]["session"]["model"], "deepseek/mock-2");
    let mut events = demux.subscribe(&session);
    let turn = native_prompt(&client, &mut events, &session, "hello").await;
    assert_eq!(turn.end["type"], "task_completed", "{:?}", turn.end);

    // The command line sees the same models and changes the same menu.
    let out = env.her(&["models", "--provider", "deepseek", "--json"]).output().await.unwrap();
    assert!(out.status.success());
    let cli: Value = serde_json::from_slice(&out.stdout).unwrap();
    assert_eq!(model(&cli, "deepseek/mock-2")["shown"], true);
    assert!(cli["providers"].as_array().unwrap().iter().all(|p| p["id"] == "deepseek"));
    let out = env.her(&["models", "hide", "deepseek/mock-2"]).output().await.unwrap();
    assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
    let text = env.her(&["models", "--provider", "deepseek"]).output().await.unwrap();
    let text = String::from_utf8(text.stdout).unwrap();
    assert!(!text.contains("deepseek/mock-2"), "{text}");
    assert!(text.contains("from the provider's list"), "{text}");
    let all = env.her(&["models", "--provider", "deepseek", "--all"]).output().await.unwrap();
    assert!(String::from_utf8(all.stdout).unwrap().contains("deepseek/mock-2  mock-2  context 128000  details unknown, hidden"));

    // Removing the key takes the models away again.
    client.request("auth/delete", json!({"provider": "deepseek"})).await.unwrap();
    let models = client.request("models/list", json!({})).await.unwrap();
    assert!(models["models"].as_array().unwrap().iter().all(|m| m["provider"] != "deepseek"));
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn without_network_nothing_is_asked_in_the_background() {
    let env = TestEnv::new("[models]\nnetwork = false\n").await;
    let (_child, client, _incoming) = env.spawn_rpc(&["serve", "--stdio"]);
    native_init(&client).await;
    client.request("models/list", json!({})).await.unwrap();
    tokio::time::sleep(Duration::from_millis(500)).await;
    assert_eq!(env.mock.stats().catalog_fetches.load(Ordering::Relaxed), 0);
    // An explicit key check still asks.
    let set = client.request("auth/set", json!({"provider": "deepseek", "api_key": "sk-good-key"})).await.unwrap();
    assert_eq!(set["verified"], true);
    // A project cannot turn it back on.
    let project = env.workspace("p");
    std::fs::create_dir_all(project.join(".her")).unwrap();
    std::fs::write(project.join(".her/config.toml"), "[models]\nnetwork = true\n").unwrap();
    let out = env.her_in(&project, &["status"]).output().await.unwrap();
    assert!(String::from_utf8_lossy(&out.stdout).contains("ignored [models]"), "{}", String::from_utf8_lossy(&out.stdout));
}

/// Waits for the session's next `settings_changed` and returns its thinking level.
async fn next_thinking(events: &mut tokio::sync::mpsc::UnboundedReceiver<her_rpc::Incoming>) -> String {
    loop {
        let incoming = common::recv(events).await;
        let event = &incoming.params()["event"];
        if event["type"] == "settings_changed" {
            return event["thinking"].as_str().unwrap().to_string();
        }
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn the_thinking_level_follows_the_model_and_is_remembered() {
    let env = TestEnv::new("").await;
    let (_child, client, incoming) = env.spawn_rpc(&["serve", "--stdio"]);
    native_init(&client).await;
    let demux = Demux::spawn(incoming);
    client.request("auth/set", json!({"provider": "deepseek", "api_key": "sk-good-key"})).await.unwrap();
    let models = client.request("models/list", json!({})).await.unwrap();
    let flash = model(&models, "deepseek/deepseek-flash");
    assert_eq!(flash["thinking_levels"], json!(["off", "low", "high", "max"]), "DeepSeek's own levels");
    assert_eq!(flash["default_thinking"], "default");
    assert_eq!(model(&models, "mock/mock-1")["thinking_levels"], json!([]), "no thinking control without a format");

    // A level the model does not offer is not kept: the provider decides.
    let cwd = env.workspace("t");
    let created = client
        .request("session/new", json!({"cwd": cwd, "model": "deepseek/deepseek-flash", "thinking": "medium"}))
        .await
        .unwrap();
    assert_eq!(created["snapshot"]["session"]["thinking"], "default");
    let session = created["session_id"].as_str().unwrap().to_string();
    let mut events = demux.subscribe(&session);
    let set = |model: &str, thinking: Option<&str>| {
        let mut op = json!({"type": "set_model", "model": model});
        if let Some(level) = thinking {
            op["thinking"] = json!(level);
        }
        client.request("session/op", json!({"session_id": session, "op": op}))
    };

    // Chosen by hand: kept, and remembered for the model.
    set("deepseek/deepseek-flash", Some("max")).await.unwrap();
    assert_eq!(next_thinking(&mut events).await, "max");
    // A model without it: the provider decides.
    set("mock/mock-1", None).await.unwrap();
    assert_eq!(next_thinking(&mut events).await, "default");
    // Back: the level chosen on this model returns.
    set("deepseek/deepseek-flash", None).await.unwrap();
    assert_eq!(next_thinking(&mut events).await, "max");
    // A level both models offer is kept across the switch.
    set("deepseek/deepseek-flash", Some("low")).await.unwrap();
    assert_eq!(next_thinking(&mut events).await, "low");
    set("deepseek-anthropic/deepseek-flash", None).await.unwrap();
    assert_eq!(next_thinking(&mut events).await, "low");

    let models = client.request("models/list", json!({})).await.unwrap();
    assert_eq!(model(&models, "deepseek/deepseek-flash")["default_thinking"], "low");
    let selection: Value =
        serde_json::from_slice(&std::fs::read(env.home.path().join("models/selection.json")).unwrap()).unwrap();
    assert_eq!(selection["thinking"]["deepseek/deepseek-flash"], "low");
    // Choosing "default" forgets it.
    set("deepseek/deepseek-flash", Some("default")).await.unwrap();
    assert_eq!(next_thinking(&mut events).await, "default");
    let models = client.request("models/list", json!({})).await.unwrap();
    assert_eq!(model(&models, "deepseek/deepseek-flash")["default_thinking"], "default");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn token_plans_save_independently_and_do_not_change_session_defaults() {
    let env = TestEnv::new("").await;
    let path = env.home.path().join("models.toml");
    let mut config = std::fs::read_to_string(&path).unwrap();
    // All endpoints used by this test are local; the regular API provider stays unconnected.
    config.push_str(&format!(
        "\n[providers.bailian-token-plan]\nbase_url = \"{}\"\n\n[providers.zhipu-coding-plan]\nbase_url = \"{}/coding/v4\"\n",
        env.mock.base_url(), env.mock.base_url(),
    ));
    std::fs::write(path, config).unwrap();
    let (_child, client, incoming) = env.spawn_rpc(&["serve", "--stdio"]);
    native_init(&client).await;
    let demux = Demux::spawn(incoming);
    let before = client.request("models/list", json!({})).await.unwrap();
    let defaults = before["defaults"].clone();
    assert_eq!(before["providers"].as_array().unwrap().iter().filter(|p| p.get("plan").is_some()).count(), 6);
    assert_eq!(provider(&before, "bailian-token-plan")["plan"]["dynamic_models"], true);
    assert_eq!(provider(&before, "zhipu-coding-plan")["plan"]["dynamic_models"], false);

    let rejected = client.request("auth/set", json!({"provider": "bailian-token-plan", "api_key": her_mock::REJECTED_KEY})).await.unwrap_err();
    assert_eq!(rejected.code, -32021);
    let saved = client.request("auth/set", json!({"provider": "bailian-token-plan", "api_key": "sk-plan-test"})).await.unwrap();
    assert_eq!(saved["verified"], true);
    let saved = client.request("auth/set", json!({"provider": "zhipu-coding-plan", "api_key": "sk-glm-plan-test"})).await.unwrap();
    assert_eq!(saved["verified"], false, "saving a static plan cannot claim validation");
    let after = client.request("models/list", json!({})).await.unwrap();
    assert_eq!(after["defaults"], defaults);
    assert_eq!(provider(&after, "qwen")["credential"], "missing");
    assert_eq!(provider(&after, "glm")["credential"], "missing");
    assert_eq!(provider(&after, "zai-coding-plan")["credential"], "missing");
    let glm: Vec<_> = after["models"].as_array().unwrap().iter().filter(|m| m["provider"] == "zhipu-coding-plan").collect();
    assert_eq!(glm.len(), 2);
    assert!(glm.iter().all(|m| m.get("cost").is_none()));
    assert!(model(&after, "bailian-token-plan/qwen3.8-max").is_null(), "a successful list replaces fallback seeds");
    assert!(!after.to_string().contains("sk-plan-test"));

    // The subscription model is a regular harness model through its dedicated provider route.
    let cwd = env.workspace("plan-session");
    let created = client.request("session/new", json!({"cwd": cwd, "model": "bailian-token-plan/mock-2"})).await.unwrap();
    let session = created["session_id"].as_str().unwrap().to_string();
    let mut events = demux.subscribe(&session);
    let turn = native_prompt(&client, &mut events, &session, "hello").await;
    assert_eq!(turn.end["type"], "task_completed", "{:?}", turn.end);

    client.request("auth/set", json!({"provider": "zhipu-coding-plan", "api_key": "sk-glm-replacement-test"})).await.unwrap();
    client.request("auth/delete", json!({"provider": "zhipu-coding-plan"})).await.unwrap();
    let removed = client.request("models/list", json!({})).await.unwrap();
    assert_eq!(provider(&removed, "zhipu-coding-plan")["credential"], "missing");
    assert_eq!(provider(&removed, "bailian-token-plan")["credential"], "stored");
    assert!(removed["models"].as_array().unwrap().iter().all(|m| m["provider"] != "zhipu-coding-plan"));
    assert_eq!(removed["defaults"], defaults);
}

/// One session on `model` at `thinking`, one prompt: what reached the provider.
async fn request_of(env: &TestEnv, client: &her_rpc::Client, demux: &Demux, model: &str, thinking: &str) -> Value {
    let cwd = env.workspace(&format!("w{}", env.mock.stats().requests.load(Ordering::Relaxed)));
    let created = client.request("session/new", json!({"cwd": cwd, "model": model, "thinking": thinking})).await.unwrap();
    assert_eq!(created["snapshot"]["session"]["thinking"], thinking, "{model} offers it");
    let session = created["session_id"].as_str().unwrap().to_string();
    let mut events = demux.subscribe(&session);
    let turn = native_prompt(client, &mut events, &session, "hello").await;
    assert_eq!(turn.end["type"], "task_completed", "{model}: {:?}", turn.end);
    env.mock.stats().chat_controls().last().cloned().unwrap()
}

/// The fields of a request that say how much to think and write, as sent.
fn sent(request: &Value) -> Value {
    let fields = ["model", "max_tokens", "enable_thinking", "reasoning_effort", "thinking_budget", "thinking"];
    Value::Object(fields.iter().filter_map(|f| Some((f.to_string(), request.get(*f)?.clone()))).collect())
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn each_channel_describes_its_models_and_sends_the_levels_it_offers() {
    // The compiled-in catalog: the mock's would empty what these channels borrow from.
    let env = TestEnv::new("[models]\nnetwork = false\n").await;
    let path = env.home.path().join("models.toml");
    let mut config = std::fs::read_to_string(&path).unwrap();
    for (provider, route) in [("bailian-token-plan", "bailian"), ("tencent-token-plan", "tencent"), ("zhipu-coding-plan", "zhipu")] {
        config.push_str(&format!("\n[providers.{provider}]\nbase_url = \"{}/{route}/v1\"\n", env.mock.base_url()));
    }
    std::fs::write(path, config).unwrap();
    let (_child, client, incoming) = env.spawn_rpc(&["serve", "--stdio"]);
    native_init(&client).await;
    let demux = Demux::spawn(incoming);
    for provider in ["bailian-token-plan", "tencent-token-plan", "zhipu-coding-plan"] {
        client.request("auth/set", json!({"provider": provider, "api_key": "sk-plan-test"})).await.unwrap();
    }
    let models = client.request("models/list", json!({})).await.unwrap();

    // The plan's list decides what exists; the plan's row in the catalog says what each can do.
    let bailian = |id: &str| model(&models, &format!("bailian-token-plan/{id}"));
    assert_eq!(bailian("qwen3.8-max")["thinking_levels"], json!(["off", "low", "medium", "xhigh"]));
    assert_eq!(bailian("qwen3.8-max")["context_window"], 1_000_000);
    assert_eq!(bailian("qwen3.8-max")["images"], true);
    assert!(bailian("qwen3.8-max").get("cost").is_none(), "a plan is not billed per token");
    assert_eq!(bailian("qwen3.7-max")["thinking_levels"], json!(["off", "low", "medium", "high", "max"]));
    assert_eq!(bailian("glm-5.3")["thinking_levels"], json!(["low", "high", "max"]), "it always thinks");
    assert_eq!(bailian("glm-5.3")["images"], false);
    let dated = bailian("deepseek-v4.1-flash-1017");
    assert_eq!((&dated["known"], &dated["shown"]), (&json!(true), &json!(true)), "a dated build has its row");
    assert_eq!(dated["thinking_levels"], json!(["off", "low", "high", "max"]));
    assert!(bailian("wan2.7-image").is_null(), "it does not chat");
    assert_eq!((&bailian("auto")["known"], &bailian("auto")["shown"]), (&json!(false), &json!(false)), "a name only");

    // A plan the catalog barely covers: the maker's row, as this channel calls the model.
    let tencent = |id: &str| model(&models, &format!("tencent-token-plan/{id}"));
    assert_eq!(tencent("glm-5.3")["context_window"], 1_000_000);
    assert_eq!(tencent("glm-5.3")["known"], true);
    assert_eq!(tencent("glm-5.3")["thinking_levels"], json!(["low", "high", "max"]));
    assert_eq!(tencent("kimi-k3")["images"], true);
    assert_eq!(tencent("deepseek/deepseek-flash")["thinking_levels"], json!(["low", "high", "max"]), "its switch is not known here");
    assert_eq!(tencent("hy3")["thinking_levels"], json!(["off", "high"]), "`none` is one of its efforts");
    assert!(tencent("glm-5.3").get("cost").is_none(), "nor is a maker's price this plan's");
    // A routing name: in the menu, marked unknown, on no invented limits.
    let auto = tencent("tc-code-latest");
    assert_eq!((&auto["known"], &auto["shown"]), (&json!(false), &json!(true)));
    assert_eq!(auto["thinking_levels"], json!([]));
    // What a maker says it cannot do is not offered: this one cannot be switched off.
    assert_eq!(model(&models, "zhipu-coding-plan/glm-5.3")["thinking_levels"], json!(["low", "high", "max"]));

    // What is chosen reaches the provider, the way each model takes it.
    let cases = [
        ("bailian-token-plan/qwen3.8-max", "xhigh", json!({"model": "qwen3.8-max", "max_tokens": 32000, "enable_thinking": true, "reasoning_effort": "xhigh"})),
        ("bailian-token-plan/qwen3.8-max", "off", json!({"model": "qwen3.8-max", "max_tokens": 32000, "enable_thinking": false})),
        ("bailian-token-plan/qwen3.8-max", "default", json!({"model": "qwen3.8-max", "max_tokens": 32000})),
        ("bailian-token-plan/qwen3.7-max", "low", json!({"model": "qwen3.7-max", "max_tokens": 32000, "enable_thinking": true, "thinking_budget": 4096})),
        ("bailian-token-plan/glm-5.3", "high", json!({"model": "glm-5.3", "max_tokens": 32000, "reasoning_effort": "high"})),
        ("bailian-token-plan/deepseek-v4.1-flash-1017", "max", json!({"model": "deepseek-v4.1-flash-1017", "max_tokens": 32000, "enable_thinking": true, "reasoning_effort": "max"})),
        ("tencent-token-plan/glm-5.3", "high", json!({"model": "glm-5.3", "max_tokens": 32000, "reasoning_effort": "high"})),
        ("tencent-token-plan/hy3", "off", json!({"model": "hy3", "max_tokens": 32000, "reasoning_effort": "none"})),
        // The channel's name for the model, not the catalog's.
        ("tencent-token-plan/deepseek/deepseek-flash", "low", json!({"model": "deepseek/deepseek-flash", "max_tokens": 32000, "reasoning_effort": "low"})),
        ("tencent-token-plan/minimax-m3", "default", json!({"model": "minimax-m3", "max_tokens": 32000})),
        // No limit nobody stated: a guess would cut replies short.
        ("tencent-token-plan/tc-code-latest", "default", json!({"model": "tc-code-latest"})),
        ("zhipu-coding-plan/glm-5.3", "low", json!({"model": "glm-5.3", "max_tokens": 32000, "reasoning_effort": "low"})),
    ];
    for (reference, thinking, expected) in cases {
        let request = request_of(&env, &client, &demux, reference, thinking).await;
        assert_eq!(sent(&request), expected, "{reference} at {thinking}");
    }
}

/// What `models/list` says now.
async fn listing(client: &her_rpc::Client) -> Value {
    client.request("models/list", json!({})).await.unwrap()
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_service_of_ones_own_is_added_used_changed_and_removed() {
    let env = TestEnv::new("").await;
    let schema = SchemaCheck::new(&["ModelsListResult", "ProvidersModelsResult", "ProvidersSaveResult"]);
    let models_file = env.home.path().join("models.toml");
    let written = std::fs::read_to_string(&models_file).unwrap();
    let (child, client, incoming) = env.spawn_rpc(&["serve", "--stdio"]);
    native_init(&client).await;
    let demux = Demux::spawn(incoming);
    let before = listing(&client).await;
    // A provider written by hand in models.toml is one of one's own too; a built-in one changed there is not.
    assert_eq!(provider(&before, "mock")["custom"]["models"], json!([{"id": "mock-1", "context_window": 200000, "max_output_tokens": 32000}]));
    assert!(provider(&before, "deepseek").get("custom").is_none());

    // The service's list, to choose from: with the key typed, a refused key said so, an unreachable service tolerated.
    let service = |key: &str| json!({"base_url": env.mock.base_url(), "api": "openai-chat", "api_key": key});
    let err = client.request("providers/models", service(her_mock::REJECTED_KEY)).await.unwrap_err();
    assert_eq!(err.code, -32021);
    let listed = client.request("providers/models", service("sk-gateway")).await.unwrap();
    schema.check("ProvidersModelsResult", &listed);
    assert_eq!(listed["listed"], true);
    let ids: Vec<&str> = listed["models"].as_array().unwrap().iter().map(|m| m["id"].as_str().unwrap()).collect();
    assert_eq!(ids, ["mock-1", "mock-2", "deepseek-flash"]);
    let away = client.request("providers/models", json!({"base_url": "http://127.0.0.1:1/v1", "api": "openai-chat"})).await.unwrap();
    assert_eq!((&away["listed"], away["error"].is_string()), (&json!(false), true), "{away}");

    // Saved: a model from the list with its window, and one named by hand.
    let saved = client
        .request(
            "providers/save",
            json!({"name": "Magpie Gateway", "base_url": format!("{}/", env.mock.base_url()), "api": "openai-chat", "api_key": "sk-gateway",
                   "models": [{"id": "mock-2", "context_window": 64000}, {"id": "typed-by-hand"}]}),
        )
        .await
        .unwrap();
    schema.check("ProvidersSaveResult", &saved);
    assert_eq!(saved["id"], "magpie-gateway");
    let after = listing(&client).await;
    schema.check("ModelsListResult", &after);
    let magpie = provider(&after, "magpie-gateway");
    assert_eq!((&magpie["name"], &magpie["credential"], &magpie["auth"]), (&json!("Magpie Gateway"), &json!("stored"), &json!("api_key")));
    assert_eq!(magpie["custom"]["base_url"], env.mock.base_url());
    assert_eq!(magpie["custom"]["api"], "openai-chat");
    let mock_2 = model(&after, "magpie-gateway/mock-2");
    assert_eq!((&mock_2["context_window"], &mock_2["available"], &mock_2["shown"]), (&json!(64000), &json!(true), &json!(true)));
    assert_eq!(model(&after, "magpie-gateway/typed-by-hand")["context_window"], 128_000, "her's default window");
    assert_eq!(after["defaults"], before["defaults"]);
    assert_eq!(provider(&after, "deepseek"), provider(&before, "deepseek"), "built-in providers are as they were");
    // The key is in the credential store, nowhere else.
    let file = std::fs::read_to_string(&models_file).unwrap();
    assert!(file.starts_with(&written), "what was written stays:\n{file}");
    assert!(!file.contains("sk-gateway") && !after.to_string().contains("sk-gateway"));
    assert!(std::fs::read_to_string(env.home.path().join("credentials.toml")).unwrap().contains("magpie-gateway"));

    // A session runs on it, with its key.
    let cwd = env.workspace("own-service");
    let created = client.request("session/new", json!({"cwd": cwd, "model": "magpie-gateway/mock-2"})).await.unwrap();
    let session = created["session_id"].as_str().unwrap().to_string();
    let mut events = demux.subscribe(&session);
    let turn = native_prompt(&client, &mut events, &session, "hello").await;
    assert_eq!(turn.end["type"], "task_completed", "{:?}", turn.end);
    assert_eq!(env.mock.stats().chat_authorization().as_deref(), Some("Bearer sk-gateway"));
    assert_eq!(env.mock.stats().chat_controls().last().unwrap()["model"], "mock-2");

    // It is there after her starts again, and can be changed: the saved key stays for the same service only.
    drop((child, client, demux));
    let (_child, client, incoming) = env.spawn_rpc(&["serve", "--stdio"]);
    native_init(&client).await;
    let _demux = Demux::spawn(incoming);
    let again = listing(&client).await;
    assert_eq!(provider(&again, "magpie-gateway")["custom"], magpie["custom"]);
    assert_eq!(provider(&again, "magpie-gateway")["credential"], "stored");
    let mine = client.request("providers/models", json!({"base_url": env.mock.base_url(), "api": "openai-chat", "provider": "magpie-gateway"})).await.unwrap();
    assert_eq!(mine["listed"], true, "the saved key, at its own service");
    let elsewhere = json!({"base_url": "http://127.0.0.2:9/v1", "api": "openai-chat", "provider": "magpie-gateway"});
    assert_eq!(client.request("providers/models", elsewhere).await.unwrap_err().code, her_rpc::code::INVALID_PARAMS);
    let change = |base_url: String| json!({"id": "magpie-gateway", "name": "Magpie", "base_url": base_url, "api": "openai-chat", "models": [{"id": "mock-1"}]});
    let err = client.request("providers/save", change("http://127.0.0.2:9/v1".into())).await.unwrap_err();
    assert_eq!(err.code, her_rpc::code::INVALID_PARAMS, "{}", err.message);
    client.request("providers/save", change(env.mock.base_url())).await.unwrap();
    let changed = listing(&client).await;
    assert_eq!(provider(&changed, "magpie-gateway")["name"], "Magpie");
    assert_eq!(provider(&changed, "magpie-gateway")["credential"], "stored");
    let mine: Vec<&str> = changed["models"].as_array().unwrap().iter().filter(|m| m["provider"] == "magpie-gateway").map(|m| m["id"].as_str().unwrap()).collect();
    assert_eq!(mine, ["magpie-gateway/mock-1"]);

    // Built-in providers are not changed or removed this way.
    let builtin = json!({"id": "deepseek", "name": "x", "base_url": env.mock.base_url(), "api": "openai-chat", "models": [{"id": "m"}]});
    assert_eq!(client.request("providers/save", builtin).await.unwrap_err().code, her_rpc::code::INVALID_PARAMS);
    assert_eq!(client.request("providers/remove", json!({"id": "deepseek"})).await.unwrap_err().code, her_rpc::code::INVALID_PARAMS);

    // Removed: the file is as it was written, the key gone.
    client.request("providers/remove", json!({"id": "magpie-gateway"})).await.unwrap();
    let removed = listing(&client).await;
    assert!(provider(&removed, "magpie-gateway").is_null());
    assert!(removed["models"].as_array().unwrap().iter().all(|m| m["provider"] != "magpie-gateway"));
    assert_eq!(std::fs::read_to_string(&models_file).unwrap(), written);
    assert!(!std::fs::read_to_string(env.home.path().join("credentials.toml")).unwrap_or_default().contains("magpie-gateway"));
    assert_eq!(provider(&removed, "deepseek"), provider(&before, "deepseek"));
}
