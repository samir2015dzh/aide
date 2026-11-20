use her_llm::ThinkingLevel;
use her_llm::UserContent;
use schemars::JsonSchema;
use serde::Deserialize;
use serde::Serialize;

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize, JsonSchema)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum Op {
    /// User input. Starts a task when the session is idle; queued by `mode` otherwise.
    Prompt {
        input: Vec<UserContent>,
        #[serde(default)]
        mode: PromptMode,
        /// Whether a leading `/name` is a command: a built-in one such as `/compact`, or a
        /// skill, whose instructions replace it. Front ends leave it out; messages between
        /// agents set false.
        #[serde(default = "yes", skip_serializing_if = "is_yes")]
        commands: bool,
        /// The input's id, given by the front end: the `message_id` it has once it joins the
        /// history. A prompt whose id the session knows already, in its history or its queues,
        /// is taken once: sent again, it changes nothing. At most 64 of
        /// `A-Z a-z 0-9 _ -`; left out, the kernel gives one.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        id: Option<String>,
    },
    /// Starts again from a message the user sent earlier: the running task stops, the
    /// conversation goes back to just before that message, and `input` takes its place. Nothing
    /// changes until this op: `session_rewound` says it happened, a `warning` with the code
    /// `resend_rejected` that it did not (docs/kernel/message-resend-design.md).
    Resend {
        /// The `message_id` of a user message on the current branch.
        message_id: String,
        input: Vec<UserContent>,
        /// As for `Prompt`.
        #[serde(default = "yes", skip_serializing_if = "is_yes")]
        commands: bool,
    },
    /// Continue from the current history (retry after a failure).
    Continue,
    Interrupt,
    /// Takes input that waits for the running task out of the queue: `input_withdrawn` says it
    /// went. Nothing happens once it has joined the history.
    Withdraw {
        id: String,
    },
    /// Moves waiting input to the other mode, at the end of it.
    Requeue {
        id: String,
        mode: PromptMode,
    },
    /// Ends the running step, the model's reply or its tool calls, so that waiting steering input
    /// joins the history now; the task goes on. Nothing happens when no steering input waits.
    SteerNow,
    /// Stops one sub-agent's current task. It keeps its session and takes messages again; its
    /// report arrives as an interrupted one and does not wake the session.
    AgentInterrupt {
        /// The agent's id or name.
        agent: String,
    },
    SetModel {
        model: String,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        thinking: Option<ThinkingLevel>,
    },
    /// Names the session; recorded in its log, at any time.
    SetTitle {
        title: String,
    },
    /// Archives the session or brings it back; recorded in its log, at any time.
    SetArchived {
        archived: bool,
    },
    Compact,
    Shutdown,
}

impl Op {
    pub fn prompt(text: impl Into<String>) -> Self {
        Op::Prompt {
            input: vec![UserContent::Text { text: text.into() }],
            mode: PromptMode::Steer,
            commands: true,
            id: None,
        }
    }
}

/// Whether a front end's id for a prompt can be one: 1 to 64 of `A-Z a-z 0-9 _ -`.
pub fn valid_input_id(id: &str) -> bool {
    (1..=64).contains(&id.len()) && id.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

fn yes() -> bool {
    true
}

fn is_yes(value: &bool) -> bool {
    *value
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum PromptMode {
    /// Injected at the next turn boundary of the running task.
    #[default]
    Steer,
    /// Processed when the running task would otherwise finish.
    FollowUp,
}

/// Command prefix rule for the bash tool: what matches it is refused.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize, JsonSchema)]
#[serde(deny_unknown_fields)]
pub struct CommandRule {
    pub prefix: Vec<String>,
    pub decision: RuleDecision,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum RuleDecision {
    /// The command never runs. Her asks nobody before an action: a rule can only refuse one.
    Deny,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ops_round_trip_as_tagged_json() {
        let op = Op::prompt("hi");
        let json = serde_json::to_string(&op).unwrap();
        assert_eq!(json, r#"{"type":"prompt","input":[{"type":"text","text":"hi"}],"mode":"steer"}"#);
        let parsed: Op = serde_json::from_str(r#"{"type":"prompt","input":[{"type":"text","text":"x"}]}"#).unwrap();
        assert!(matches!(parsed, Op::Prompt { mode: PromptMode::Steer, commands: true, .. }));
        let plain: Op =
            serde_json::from_str(r#"{"type":"prompt","input":[{"type":"text","text":"/x"}],"commands":false}"#).unwrap();
        assert!(matches!(plain, Op::Prompt { commands: false, .. }));
        assert!(serde_json::to_string(&plain).unwrap().ends_with(r#""commands":false}"#));
        let named: Op = serde_json::from_str(r#"{"type":"prompt","input":[],"id":"hw_01"}"#).unwrap();
        assert!(matches!(&named, Op::Prompt { id: Some(id), .. } if id == "hw_01"));
        assert!(serde_json::to_string(&named).unwrap().ends_with(r#""id":"hw_01"}"#));
        assert!(valid_input_id("hw_01jcx-Z9") && !valid_input_id("") && !valid_input_id("a b") && !valid_input_id(&"x".repeat(65)));
        // Approvals and permission modes are gone: those ops are no longer ops.
        assert!(serde_json::from_str::<Op>(r#"{"type":"approval","call_id":"c1","decision":{"type":"approve"}}"#).is_err());
        assert!(serde_json::from_str::<Op>(r#"{"type":"set_permission_mode","mode":"manual"}"#).is_err());
        assert_eq!(serde_json::to_string(&Op::Interrupt).unwrap(), r#"{"type":"interrupt"}"#);
    }
}
