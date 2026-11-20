//! Extension points of the agent loop. The tool policy is itself a hook.

use async_trait::async_trait;
use her_llm::AssistantMessage;
use her_llm::Context;
use her_llm::ToolCall;
use her_protocol::CommandRule;
use her_tools::Authority;
use her_tools::Environment;
use her_tools::Tool;
use her_tools::ToolOutput;
use std::path::PathBuf;
use std::sync::Arc;

/// What a hook says of a call. Nobody is asked: a call runs unless a hook refuses it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Gate {
    Allow,
    Deny(String),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TurnDecision {
    /// Normal scheduling: continue when there are tool results or queued input.
    Default,
    /// Run one more turn.
    Continue,
    /// End the task now.
    End,
}

/// Read-only snapshot of session state for hooks.
#[derive(Clone)]
pub struct SessionView {
    /// The configured deny rules.
    pub rules: Vec<CommandRule>,
    pub env: Arc<dyn Environment>,
    /// her's own data directory (canonical), where bots keep their memory.
    pub data_dir: PathBuf,
    /// A bot's main session: its memory (canonical), which it alone writes
    /// (docs/her-memory.md §6). `None` in every other session, the bot's sub-agents and its
    /// routines' runs too.
    pub memory_dir: Option<PathBuf>,
    /// The `[browser]` policy: domain lists and configured sites.
    pub browser: Arc<her_browser::policy::SitePolicy>,
    /// The `[desktop]` settings: which apps are denied.
    pub desktop: crate::config::DesktopSettings,
}

#[async_trait]
pub trait Hooks: Send + Sync + 'static {
    /// Adjust the request context (inject reminders, trim). Must not fail.
    fn transform_context(&self, _ctx: &mut Context) {}

    /// Allow or deny a tool call before it runs. The first `Deny` wins.
    /// `authorities` is what the call does on which site (`Tool::authorities`).
    async fn before_tool(&self, _call: &ToolCall, _tool: &dyn Tool, _authorities: &[Authority], _view: &SessionView) -> Gate {
        Gate::Allow
    }

    /// Adjust a tool result.
    async fn after_tool(&self, _call: &ToolCall, output: ToolOutput) -> ToolOutput {
        output
    }

    /// Decide after each turn. The first non-`Default` answer wins.
    async fn finish_turn(&self, _message: &AssistantMessage) -> TurnDecision {
        TurnDecision::Default
    }
}
