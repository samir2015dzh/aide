//! `her exec`: run a task headless, in a new session or one it continues. The output is the
//! reply as text, one JSON result object (`--json`), or the event stream as JSONL
//! (`--output stream-json`).

use anyhow::Context as _;
use her_core::JsonlStore;
use her_core::Kernel;
use her_core::ResumeOptions;
use her_core::SessionRef;
use her_core::SessionStore;
use her_core::StartOptions;
use her_llm::UsageTotals;
use her_llm::UserContent;
use her_protocol::AgentStatus;
use her_protocol::ErrorInfo;
use her_protocol::EventMsg;
use her_protocol::Op;
use serde_json::Value;
use serde_json::json;
use std::io::BufRead;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;
use std::time::Instant;
use tokio::sync::mpsc;

use crate::OutputFormat;
use crate::ExecArgs;
use crate::RunArgs;
use crate::render::Renderer;
use crate::setup;

pub async fn exec(
    overrides: &[String],
    prompt: &str,
    cwd: Option<PathBuf>,
    save: bool,
    args: &ExecArgs,
) -> anyhow::Result<u8> {
    let run = &args.run;
    let cwd = match cwd {
        Some(cwd) => cwd,
        None => std::env::current_dir()?,
    };
    let mut rt = setup::init(cwd, &with_run_overrides(overrides, run))?;
    rt.warnings.extend(run.warnings());
    let model = setup::session_model(&rt, run.model.clone())?;
    setup::check_credentials(&rt, &model)?;
    let prompt = input_of(setup::read_prompt(prompt)?, &args.image)?;
    let deps = rt.kernel_deps(save, &run.add_dir)?;
    let options = StartOptions {
        model,
        thinking: run.thinking.unwrap_or(rt.config.thinking),
        cwd: rt.cwd.clone(),
        home: rt.home.clone(),
        config: Arc::new(rt.config.clone()),
        project_root: rt.project_root.clone(),
        bot: None,
    };
    let kernel = Kernel::start(options, deps).await?;
    drive(kernel, prompt, args, save, &rt.warnings).await
}

pub async fn resume(
    overrides: &[String],
    session: &str,
    prompt: &str,
    args: &ExecArgs,
) -> anyhow::Result<u8> {
    let run = &args.run;
    let home = setup::home()?;
    let store = JsonlStore::new(home.sessions_dir());
    let (header, path) = match session {
        "last" => {
            let workspace = setup::workspace(&std::env::current_dir()?);
            store
                .header(&SessionRef::LastIn(workspace))
                .context("nothing to continue in this project (`her sessions --all` lists every project)")?
        }
        id => store.header(&SessionRef::Id(id.to_string()))?,
    };
    let prompt = input_of(setup::read_prompt(prompt)?, &args.image)?;
    let mut rt = setup::init(header.cwd.clone(), &with_run_overrides(overrides, run))?;
    rt.warnings.extend(run.warnings());
    let deps = rt.kernel_deps(true, &run.add_dir)?;
    let options = ResumeOptions {
        session: SessionRef::Path(path),
        model: run.model.clone(),
        thinking: run.thinking,
        home: rt.home.clone(),
        config: Arc::new(rt.config.clone()),
        project_root: rt.project_root.clone(),
        bot_name: None,
    };
    let kernel = Kernel::resume(options, deps).await?;
    setup::check_credentials(&rt, &kernel.info().model)?;
    drive(kernel, prompt, args, true, &rt.warnings).await
}

pub(crate) fn with_run_overrides(overrides: &[String], run: &RunArgs) -> Vec<String> {
    let mut all = overrides.to_vec();
    if let Some(max_turns) = run.max_turns {
        all.push(format!("max_turns={max_turns}"));
    }
    if let Some(tools) = &run.tools {
        all.push(format!("tools.enabled={}", toml_list(tools)));
    }
    all
}

fn toml_list(items: &[String]) -> String {
    let items: Vec<String> = items.iter().map(|item| format!("{item:?}")).collect();
    format!("[{}]", items.join(", "))
}

/// The task's text and the images given with `--image`.
fn input_of(text: String, images: &[PathBuf]) -> anyhow::Result<Vec<UserContent>> {
    use base64::Engine as _;
    let mut input = vec![UserContent::Text { text }];
    for path in images {
        let mime_type = match path.extension().and_then(|e| e.to_str()).map(str::to_ascii_lowercase).as_deref() {
            Some("png") => "image/png",
            Some("jpg" | "jpeg") => "image/jpeg",
            Some("gif") => "image/gif",
            Some("webp") => "image/webp",
            _ => anyhow::bail!("--image {}: expected a png, jpeg, gif or webp file", path.display()),
        };
        let bytes = std::fs::read(path).with_context(|| format!("cannot read --image {}", path.display()))?;
        input.push(UserContent::Image {
            mime_type: mime_type.to_string(),
            data: base64::engine::general_purpose::STANDARD.encode(bytes),
        });
    }
    Ok(input)
}

/// Runs one task to its end. Exit codes: 0 completed, 1 failed, 130 interrupted. Nobody is asked
/// about a tool call: what the policy does not refuse runs.
async fn drive(
    mut kernel: Kernel,
    input: Vec<UserContent>,
    run: &ExecArgs,
    saved: bool,
    warnings: &[String],
) -> anyhow::Result<u8> {
    let format = run.format();
    let started = Instant::now();
    let mut renderer = Renderer::new(format == OutputFormat::StreamJson);
    if format != OutputFormat::StreamJson {
        for warning in warnings {
            eprintln!("{}", renderer.style().yellow(&format!("warning: {warning}")));
        }
    }
    let session_id = kernel.session_id().to_string();
    let session_file = kernel.session_file().to_path_buf();
    let ops = kernel.op_sender();
    spawn_interrupt_handler(ops.clone(), format != OutputFormat::Text);
    if run.ops_stdin {
        spawn_stdin_ops(ops.clone());
    }
    ops.send(Op::Prompt { input, mode: Default::default(), commands: true, id: None })
        .await
        .context("the session closed unexpectedly")?;

    let mut outcome = Outcome::default();
    let mut exit_code = 1;
    while let Some(event) = kernel.next_event().await {
        if format != OutputFormat::Json {
            renderer.render(&event);
        } else if let EventMsg::Warning { message, .. } = &event.msg {
            eprintln!("{}", renderer.style().yellow(&format!("warning: {message}")));
        }
        outcome.observe(&event.msg);
        match &event.msg {
            // The exit code is that of the last task; the run ends once nothing is left to do,
            // so that sub-agents still working are not cut off.
            EventMsg::TaskCompleted { .. } => exit_code = 0,
            EventMsg::TaskFailed { .. } => exit_code = 1,
            EventMsg::Idle => break,
            EventMsg::TaskInterrupted { .. } => {
                exit_code = 130;
                break;
            }
            _ => {}
        }
    }
    for event in kernel.shutdown().await {
        if format != OutputFormat::Json {
            renderer.render(&event);
        }
    }
    if let Some(path) = &run.output_last_message {
        std::fs::write(path, outcome.final_output.clone().unwrap_or_default())
            .with_context(|| format!("cannot write {}", path.display()))?;
    }
    match format {
        OutputFormat::Json => {
            let session = saved.then_some((session_id.as_str(), session_file.as_path()));
            println!("{}", outcome.to_json(exit_code, session, started.elapsed()));
        }
        OutputFormat::Text if saved => {
            let short = her_core::short_id(&session_id);
            let hint = format!("session {short} · continue with: her exec --resume {short} \"…\"");
            eprintln!("{}", renderer.style().dim(&hint));
        }
        _ => {}
    }
    Ok(exit_code)
}

/// What `--output json` reports, gathered from the events.
#[derive(Default)]
struct Outcome {
    model: String,
    turns: u32,
    compactions: u32,
    tool_calls: Vec<Value>,
    usage: UsageTotals,
    final_output: Option<String>,
    error: Option<ErrorInfo>,
    agents: Vec<AgentOutcome>,
    /// The last `/context` report.
    context: Option<her_protocol::ContextReport>,
}

impl Outcome {
    fn observe(&mut self, msg: &EventMsg) {
        match msg {
            EventMsg::SessionStarted { model, .. } | EventMsg::SettingsChanged { model, .. } => {
                self.model = model.clone();
            }
            EventMsg::TurnCompleted { .. } => self.turns += 1,
            EventMsg::CompactionCompleted { .. } => self.compactions += 1,
            EventMsg::ContextReported { report } => self.context = Some(report.clone()),
            EventMsg::ToolCompleted { result, .. } => self.tool_calls.push(json!({
                "name": result.tool_name,
                "status": if result.is_error { "error" } else { "ok" },
                "duration_ms": result.duration_ms,
            })),
            // A run can take several tasks: reports from sub-agents start new ones.
            EventMsg::TaskCompleted { usage, last_message, .. } => {
                self.usage.merge(usage);
                self.final_output = last_message.clone();
                self.error = None;
            }
            EventMsg::TaskFailed { error, usage, .. } => {
                self.usage.merge(usage);
                self.error = Some(error.clone());
            }
            EventMsg::TaskInterrupted { usage, .. } => {
                self.usage.merge(usage);
                self.error = Some(ErrorInfo::new("interrupted", "the task was interrupted"));
            }
            EventMsg::AgentStarted {
                agent_id,
                name,
                description,
                session_file,
                ..
            } => self.agents.push(AgentOutcome {
                id: agent_id.clone(),
                name: name.clone(),
                description: description.clone(),
                session_file: session_file.clone(),
                status: AgentStatus::Running,
                usage: UsageTotals::default(),
            }),
            EventMsg::AgentStatus {
                agent_id, status, usage, ..
            } => {
                if let Some(agent) = self.agents.iter_mut().find(|a| &a.id == agent_id) {
                    agent.status = *status;
                    if let Some(usage) = usage {
                        agent.usage.merge(usage);
                    }
                }
            }
            _ => {}
        }
    }

    fn to_json(&self, exit_code: u8, session: Option<(&str, &std::path::Path)>, elapsed: Duration) -> Value {
        let tokens = self.usage.tokens();
        json!({
            "final_output": self.final_output.clone().unwrap_or_default(),
            "exit_code": exit_code,
            "error": self.error,
            "session_id": session.map(|(id, _)| id),
            "session_file": session.map(|(_, file)| file),
            "model": self.model,
            "turns": self.turns,
            "compactions": self.compactions,
            "duration_ms": elapsed.as_millis() as u64,
            "usage": {
                "input_tokens": tokens.input,
                "cache_read_tokens": tokens.cache_read,
                "cache_write_tokens": tokens.cache_write,
                "output_tokens": tokens.output,
                "cost": self.usage.cost_by_currency(),
            },
            "tool_calls": self.tool_calls,
            "context": self.context,
            "agents": self.agents.iter().map(|a| {
                let tokens = a.usage.tokens();
                json!({
                    "name": a.name,
                    "description": a.description,
                    "status": a.status,
                    "session_file": a.session_file,
                    "usage": {
                        "input_tokens": tokens.input,
                        "cache_read_tokens": tokens.cache_read,
                        "cache_write_tokens": tokens.cache_write,
                        "output_tokens": tokens.output,
                        "cost": a.usage.cost_by_currency(),
                    },
                })
            }).collect::<Vec<_>>(),
        })
    }
}

/// A sub-agent as `--output json` reports it.
struct AgentOutcome {
    id: String,
    name: String,
    description: String,
    session_file: PathBuf,
    status: AgentStatus,
    usage: UsageTotals,
}

/// First Ctrl-C interrupts the task; a second one within two seconds exits.
fn spawn_interrupt_handler(ops: mpsc::Sender<Op>, quiet: bool) {
    tokio::spawn(async move {
        let mut last: Option<Instant> = None;
        while tokio::signal::ctrl_c().await.is_ok() {
            if last.is_some_and(|t| t.elapsed() < Duration::from_secs(2)) {
                std::process::exit(130);
            }
            last = Some(Instant::now());
            if !quiet {
                eprintln!("\ninterrupting… (press Ctrl-C again to quit)");
            }
            let _ = ops.send(Op::Interrupt).await;
        }
    });
}

/// `--ops-stdin`: one JSON operation per line, e.g. `{"type":"approval",...}`.
fn spawn_stdin_ops(ops: mpsc::Sender<Op>) {
    std::thread::spawn(move || {
        let stdin = std::io::stdin();
        for line in stdin.lock().lines() {
            let Ok(line) = line else { break };
            if line.trim().is_empty() {
                continue;
            }
            match serde_json::from_str::<Op>(&line) {
                Ok(op) => {
                    if ops.blocking_send(op).is_err() {
                        break;
                    }
                }
                Err(err) => eprintln!("ignored invalid operation on stdin: {err}"),
            }
        }
    });
}
