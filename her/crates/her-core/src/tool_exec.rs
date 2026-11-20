//! One batch of tool calls: sequential preflight (argument repair, lookup, hooks), concurrent
//! execution, results in call order. Nobody is asked before a call: what no hook refuses runs.

use her_llm::JsonText;
use her_llm::ToolCall;
use her_llm::ToolResultMessage;
use her_llm::UserContent;
use her_llm::now_ms;
use her_protocol::EventMsg;
use her_protocol::Location;
use her_protocol::ToolCategory;
use her_tools::Authority;
use her_tools::ProgressSink;
use her_tools::Tool;
use her_tools::ToolCtx;
use her_tools::ToolDescription;
use her_tools::ToolError;
use her_tools::ToolKind;
use her_tools::ToolOutput;
use her_tools::cap_result;
use std::sync::Arc;
use std::time::Duration;
use std::time::Instant;

use crate::context::NOT_EXECUTED;
use crate::hooks::Gate;
use crate::session::Session;
use crate::task::TaskCtx;

pub(crate) const TRUNCATED_CALL: &str = "The response hit the output token limit, so the arguments may be \
    incomplete. Re-issue the tool call.";
/// How long a cancelled tool may take to stop on its own.
const CANCEL_GRACE: Duration = Duration::from_secs(3);

enum Prepared {
    Done(Arc<ToolResultMessage>),
    /// With what the call was judged to do, for the tool to hold execution to.
    Run(Arc<dyn Tool>, ToolCall, Arc<[Authority]>),
}

pub(crate) async fn execute_batch(
    session: &Arc<Session>,
    task: &TaskCtx,
    calls: Vec<ToolCall>,
) -> Vec<Arc<ToolResultMessage>> {
    let mut prepared = Vec::with_capacity(calls.len());
    // Every call of a batch is looked up in the same registry.
    let tools = session.tools();
    for mut call in calls {
        let tool = session.tool(&tools, &call.name);
        if let Some(tool) = &tool
            && call.arguments_error.is_none()
        {
            call.arguments = tool.prepare_arguments(call.arguments.to_value()).into();
        }
        session.emit(started(session, &call, tool.as_deref()));
        if task.turn().is_cancelled() {
            prepared.push(Prepared::Done(immediate(session, &call, NOT_EXECUTED)));
            continue;
        }
        let Some(tool) = tool else {
            let text = format!(
                "Unknown tool '{}'. Available tools: {}.",
                call.name,
                tools.names().join(", ")
            );
            prepared.push(Prepared::Done(immediate(session, &call, &text)));
            continue;
        };
        if let Some(problem) = &call.arguments_error {
            let text = format!("Invalid arguments: {problem}");
            prepared.push(Prepared::Done(immediate(session, &call, &text)));
            continue;
        }
        // What the call does on which site, judged from the session's state before the batch
        // runs (browser-design §7.1); execution is held to it.
        let authorities: Arc<[Authority]> = match authorities(session, task, tool.as_ref(), &call).await {
            Ok(authorities) => authorities.into(),
            Err(err) => {
                let text = match err {
                    ToolError::Cancelled => NOT_EXECUTED.to_string(),
                    err => err.to_string(),
                };
                prepared.push(Prepared::Done(immediate(session, &call, &text)));
                continue;
            }
        };
        // Judging may look up a site's address: a stop does not wait for it.
        let turn = task.turn();
        let verdict = tokio::select! {
            biased;
            _ = turn.cancelled() => {
                prepared.push(Prepared::Done(immediate(session, &call, NOT_EXECUTED)));
                continue;
            }
            verdict = gate(session, &call, tool.as_ref(), &authorities) => verdict,
        };
        match verdict {
            Gate::Allow => prepared.push(Prepared::Run(tool, call, authorities)),
            Gate::Deny(reason) => {
                let text = format!("Tool call denied: {reason}");
                prepared.push(Prepared::Done(immediate(session, &call, &text)));
            }
        }
    }
    let runs = prepared.into_iter().map(|item| async move {
        match item {
            Prepared::Done(result) => result,
            Prepared::Run(tool, call, authorities) => run_tool(session, task, tool, call, authorities).await,
        }
    });
    futures::future::join_all(runs).await
}

/// Error results for every call of a truncated response, with the usual events.
pub(crate) fn reject_all(session: &Session, calls: &[ToolCall], text: &str) -> Vec<Arc<ToolResultMessage>> {
    calls
        .iter()
        .map(|call| {
            let tool = session.tool(&session.tools(), &call.name);
            session.emit(started(session, call, tool.as_deref()));
            immediate(session, call, text)
        })
        .collect()
}

/// The tool_started event, with the tool's own description of the call.
fn started(session: &Session, call: &ToolCall, tool: Option<&dyn Tool>) -> EventMsg {
    let description = match tool {
        Some(tool) => tool.describe(&call.arguments.to_value(), session.env.as_ref()),
        None => ToolDescription::named(&call.name),
    };
    EventMsg::ToolStarted {
        call_id: call.id.clone(),
        name: call.name.clone(),
        args: call.arguments.to_value(),
        category: match description.category {
            her_tools::ToolCategory::Read => ToolCategory::Read,
            her_tools::ToolCategory::Edit => ToolCategory::Edit,
            her_tools::ToolCategory::Delete => ToolCategory::Delete,
            her_tools::ToolCategory::Move => ToolCategory::Move,
            her_tools::ToolCategory::Search => ToolCategory::Search,
            her_tools::ToolCategory::Execute => ToolCategory::Execute,
            her_tools::ToolCategory::Fetch => ToolCategory::Fetch,
            her_tools::ToolCategory::Other => ToolCategory::Other,
        },
        title: description.title,
        locations: description
            .locations
            .into_iter()
            .map(|(path, line)| Location { path, line })
            .collect(),
    }
}

async fn authorities(session: &Session, task: &TaskCtx, tool: &dyn Tool, call: &ToolCall) -> Result<Vec<Authority>, ToolError> {
    let ctx = tool_ctx(session, task, call, Arc::from([]));
    until_cancelled(task, tool.authorities(&call.arguments.to_value(), &ctx)).await
}

async fn gate(session: &Session, call: &ToolCall, tool: &dyn Tool, authorities: &[Authority]) -> Gate {
    let view = session.view();
    for hook in &session.hooks {
        match hook.before_tool(call, tool, authorities, &view).await {
            Gate::Allow => continue,
            other => return other,
        }
    }
    Gate::Allow
}

fn immediate(session: &Session, call: &ToolCall, text: &str) -> Arc<ToolResultMessage> {
    let result = Arc::new(ToolResultMessage {
        call_id: call.id.clone(),
        tool_name: call.name.clone(),
        content: vec![UserContent::Text { text: text.to_string() }],
        is_error: true,
        details: None,
        ts: now_ms(),
        duration_ms: None,
    });
    session.emit(EventMsg::ToolCompleted {
        call_id: call.id.clone(),
        result: result.clone(),
    });
    result
}

async fn run_tool(
    session: &Arc<Session>,
    task: &TaskCtx,
    tool: Arc<dyn Tool>,
    call: ToolCall,
    authorities: Arc<[Authority]>,
) -> Arc<ToolResultMessage> {
    let started = Instant::now();
    let _slot = session.tool_slots.acquire().await.ok();
    let ctx = tool_ctx(session, task, &call, authorities);
    let limits = ctx.limits.clone();
    let running = || session.emit(EventMsg::ToolRunning { call_id: call.id.clone() });
    let result = match tool.kind_for(&call.arguments.to_value()) {
        ToolKind::ReadOnly | ToolKind::Concurrent => {
            let _guard = session.tool_gate.read().await;
            running();
            execute(tool.as_ref(), &call, ctx, task).await
        }
        ToolKind::Mutating => {
            let _guard = session.tool_gate.write().await;
            running();
            execute(tool.as_ref(), &call, ctx, task).await
        }
    };
    let mut output = match result {
        Ok(output) => output,
        Err(ToolError::Cancelled) => ToolOutput::error("Cancelled by user."),
        Err(err) => ToolOutput::error(err.to_string()),
    };
    for hook in &session.hooks {
        output = hook.after_tool(&call, output).await;
    }
    if output.ends_task && !output.is_error {
        task.request_end();
    }
    // Safety net on top of each tool's own limits (a fifth over them, room for their notes). The
    // result is cut once, here, and never changes afterwards; the full text stays readable in a
    // file.
    cap_result(
        &mut output.content,
        limits.safety_net_tokens(),
        Some(&session.output_dir),
        &call.id,
        limits.max_saved_bytes,
    );
    // The result stays in the history for the whole session; drop the slack left from
    // building the text, which can be as large as the text itself.
    for content in &mut output.content {
        if let UserContent::Text { text } = content {
            text.shrink_to_fit();
        }
    }
    let result = Arc::new(ToolResultMessage {
        call_id: call.id.clone(),
        tool_name: call.name.clone(),
        content: output.content,
        is_error: output.is_error,
        details: output.details.map(JsonText::from),
        ts: now_ms(),
        duration_ms: Some(started.elapsed().as_millis() as u64),
    });
    session.emit(EventMsg::ToolCompleted {
        call_id: call.id.clone(),
        result: result.clone(),
    });
    result
}

fn tool_ctx(session: &Session, task: &TaskCtx, call: &ToolCall, authorities: Arc<[Authority]>) -> ToolCtx {
    let (model, _) = session.current_model();
    let emitter = session.emitter.clone();
    let progress_id = call.id.clone();
    ToolCtx {
        call_id: call.id.clone(),
        env: session.env.clone(),
        files: session.files.clone(),
        locks: session.locks.clone(),
        // A small window gets smaller results, so one call cannot fill it.
        limits: session.tool_limits(),
        progress: ProgressSink::new(move |text| {
            emitter.emit(EventMsg::ToolProgress {
                call_id: progress_id.clone(),
                text: text.to_string(),
            })
        }),
        cancel: task.turn().child_token(),
        model_accepts_images: model.accepts_images(),
        output_dir: session.output_dir.clone(),
        session_state: session.tool_state.clone(),
        authorities,
    }
}

async fn execute(tool: &dyn Tool, call: &ToolCall, ctx: ToolCtx, task: &TaskCtx) -> Result<ToolOutput, ToolError> {
    if task.turn().is_cancelled() {
        return Err(ToolError::Cancelled);
    }
    until_cancelled(task, tool.execute(call.arguments.to_value(), ctx)).await
}

/// Waits for a tool, but not forever once the turn is cancelled (the task interrupted, or
/// `steer_now`): the tool gets a grace period to
/// stop on its own (a command is killed within about 2.5 s), then the kernel stops waiting and
/// drops it, so a stuck call cannot hold the session.
async fn until_cancelled<T>(
    task: &TaskCtx,
    run: impl Future<Output = Result<T, ToolError>>,
) -> Result<T, ToolError> {
    let mut run = std::pin::pin!(run);
    let turn = task.turn();
    tokio::select! {
        result = &mut run => result,
        _ = turn.cancelled() => {
            tokio::time::timeout(CANCEL_GRACE, run).await.unwrap_or(Err(ToolError::Cancelled))
        }
    }
}
