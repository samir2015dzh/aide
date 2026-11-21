//! Confirmation rules (docs/her-confirm-rules.md): what the owner said about asking first before
//! an action of a kind, kept per assistant in `confirm-rules.json` in its directory. Asking first
//! is a courtesy in the conversation, not an approval mechanism: nothing here stops a call. The
//! rules decide one thing, whether the assistant asks in the chat before it acts, and the code
//! holds two lines whatever is written:
//! - the hard floor (deleting data, installing software, giving a new client or permission
//!   access) is asked every time: no rule that skips the question is saved for it, and
//!   [`decide`] asks for it even if the file says otherwise;
//! - where rules disagree, asking wins.

use async_trait::async_trait;
use her_protocol::bot::ConfirmAction;
use her_protocol::bot::ConfirmRule;
use her_protocol::bot::ConfirmWant;
use her_tools::Environment;
use her_tools::Tool;
use her_tools::ToolCategory;
use her_tools::ToolCtx;
use her_tools::ToolDescription;
use her_tools::ToolError;
use her_tools::ToolKind;
use her_tools::ToolOutput;
use her_tools::Typed;
use her_tools::TypedTool;
use schemars::JsonSchema;
use serde::Deserialize;
use serde_json::Value;
use std::path::Path;
use std::path::PathBuf;
use std::sync::Arc;

pub const TOOL: &str = "confirm_rules";
/// The file's name in a bot's directory.
pub const FILE: &str = "confirm-rules.json";

/// One writer at a time in this process: the bot's tool and the service's delete.
static WRITING: parking_lot::Mutex<()> = parking_lot::Mutex::new(());

/// What to do before an action, and why.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Decision {
    pub ask: bool,
    pub why: String,
}

/// Whether to ask first before `action` on `target` (docs/her-confirm-rules.md §4).
pub fn decide(rules: &[ConfirmRule], action: ConfirmAction, target: &str) -> Decision {
    if action.is_hard_floor() {
        return Decision {
            ask: true,
            why: format!("{} is a hard floor: ask in the conversation every time, whatever any rule says", word(action)),
        };
    }
    let matching: Vec<&ConfirmRule> = rules.iter().filter(|rule| rule.action == action && matches(&rule.target, target)).collect();
    let ids = |want| matching.iter().filter(|rule| rule.want == want).map(|rule| rule.id.as_str()).collect::<Vec<_>>().join(", ");
    if matching.iter().any(|rule| rule.want == ConfirmWant::Ask) {
        return Decision { ask: true, why: format!("rule {} says ask first; asking wins over any rule that says not to", ids(ConfirmWant::Ask)) };
    }
    if !matching.is_empty() {
        return Decision { ask: false, why: format!("rule {} says the owner does not need to be asked", ids(ConfirmWant::Skip)) };
    }
    Decision { ask: true, why: "no rule covers it: ask first".into() }
}

/// `pattern` against `text`, whole, ignoring case; `*` is any run of characters.
pub fn matches(pattern: &str, text: &str) -> bool {
    let pattern: Vec<char> = pattern.trim().to_lowercase().chars().collect();
    let text: Vec<char> = text.trim().to_lowercase().chars().collect();
    // Glob matching with one star to fall back to.
    let (mut p, mut t, mut star, mut from) = (0, 0, None, 0);
    while t < text.len() {
        if p < pattern.len() && pattern[p] == '*' {
            star = Some(p);
            from = t;
            p += 1;
        } else if p < pattern.len() && pattern[p] == text[t] {
            p += 1;
            t += 1;
        } else if let Some(s) = star {
            p = s + 1;
            from += 1;
            t = from;
        } else {
            return false;
        }
    }
    pattern[p..].iter().all(|c| *c == '*')
}

/// The rules in `path`, oldest first; none when there is no file.
pub fn load(path: &Path) -> std::io::Result<Vec<ConfirmRule>> {
    match std::fs::read(path) {
        Ok(bytes) => serde_json::from_slice(&bytes).map_err(std::io::Error::other),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(Vec::new()),
        Err(err) => Err(err),
    }
}

fn save(path: &Path, rules: &[ConfirmRule]) -> std::io::Result<()> {
    let dir = path.parent().unwrap_or(Path::new("."));
    std::fs::create_dir_all(dir)?;
    let mut file = tempfile::NamedTempFile::new_in(dir)?;
    serde_json::to_writer_pretty(&mut file, rules).map_err(std::io::Error::other)?;
    file.persist(path).map_err(|err| err.error)?;
    Ok(())
}

/// Keeps a new rule. A rule that would skip the question on the hard floor is refused.
pub fn add(path: &Path, action: ConfirmAction, target: &str, want: ConfirmWant, note: &str) -> Result<ConfirmRule, String> {
    if action.is_hard_floor() && want == ConfirmWant::Skip {
        return Err(format!(
            "{} is a hard floor: it is asked in the conversation every time and no rule can change that. Tell the owner so.",
            word(action)
        ));
    }
    let target = target.trim();
    if target.is_empty() {
        return Err("target is empty: give the targets it covers, or * for all".into());
    }
    let _writing = WRITING.lock();
    let mut rules = load(path).map_err(|err| format!("cannot read the rules: {err}"))?;
    let rule = ConfirmRule {
        id: new_id(),
        action,
        target: target.to_string(),
        want,
        note: note.trim().to_string(),
        created_at: chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Secs, true),
    };
    rules.push(rule.clone());
    save(path, &rules).map_err(|err| format!("cannot save the rules: {err}"))?;
    Ok(rule)
}

/// Removes a rule; `None` when there is none with that id.
pub fn remove(path: &Path, id: &str) -> std::io::Result<Option<ConfirmRule>> {
    let _writing = WRITING.lock();
    let mut rules = load(path)?;
    let Some(at) = rules.iter().position(|rule| rule.id == id) else {
        return Ok(None);
    };
    let rule = rules.remove(at);
    save(path, &rules)?;
    Ok(Some(rule))
}

/// `c_` and six lowercase letters and digits, from the random end of a UUID.
fn new_id() -> String {
    const DIGITS: &[u8] = b"abcdefghijklmnopqrstuvwxyz0123456789";
    let mut bits = uuid::Uuid::now_v7().as_u128();
    let mut id = String::from("c_");
    for _ in 0..6 {
        id.push(DIGITS[(bits % DIGITS.len() as u128) as usize] as char);
        bits /= DIGITS.len() as u128;
    }
    id
}

fn word(action: ConfirmAction) -> &'static str {
    match action {
        ConfirmAction::Send => "send",
        ConfirmAction::Publish => "publish",
        ConfirmAction::Pay => "pay",
        ConfirmAction::Delete => "delete",
        ConfirmAction::Install => "install",
        ConfirmAction::Grant => "grant",
    }
}

fn want_word(want: ConfirmWant) -> &'static str {
    match want {
        ConfirmWant::Skip => "no need to ask",
        ConfirmWant::Ask => "ask first",
    }
}

fn listing(rules: &[ConfirmRule]) -> String {
    if rules.is_empty() {
        return "No confirmation rules: ask first before what is hard to undo or reaches other people.".into();
    }
    let mut text = String::from("Confirmation rules:");
    for rule in rules {
        text.push_str(&format!("\n- {} {} \"{}\": {}", rule.id, word(rule.action), rule.target, want_word(rule.want)));
        if !rule.note.is_empty() {
            text.push_str(&format!(" (owner: {})", rule.note));
        }
    }
    text.push_str("\nDelete, install and grant are asked every time whatever the rules say.");
    text
}

/// Where a bot's session keeps its rules.
pub(crate) struct RulesFile(pub(crate) PathBuf);

pub(crate) fn tool() -> Arc<dyn Tool> {
    Arc::new(Typed::new(ConfirmRulesTool))
}

#[derive(Clone, Copy, Debug, Deserialize, JsonSchema, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
enum Op {
    Check,
    List,
    Add,
    Remove,
}

#[derive(Debug, Deserialize, JsonSchema)]
struct Args {
    /// What to do.
    op: Op,
    /// The kind of action. Required on check and add.
    #[serde(default)]
    action: Option<ConfirmAction>,
    /// check: the concrete target, such as "email to alice@example.com" or "the team channel". add: which targets the rule covers, with * for any run of characters ("*@example.com", "the team channel"), or * for all.
    #[serde(default)]
    target: Option<String>,
    /// add: skip when the owner said not to ask about this again, ask when they said always to ask first.
    #[serde(default)]
    want: Option<ConfirmWant>,
    /// add: what the owner said, in their words.
    #[serde(default)]
    note: Option<String>,
    /// remove: the rule's id.
    #[serde(default)]
    id: Option<String>,
}

struct ConfirmRulesTool;

#[async_trait]
impl TypedTool for ConfirmRulesTool {
    type Args = Args;
    const NAME: &'static str = TOOL;
    const DESCRIPTION: &'static str = "Your owner's confirmation rules: whether to ask first in the conversation before an action \
        of a kind. Asking first is a courtesy in the chat, not an approval: the rules only decide whether you ask.\n\n\
        - check: action and target. Before you send, publish or pay, check, and do what the result says: ask first, or go \
        ahead without asking.\n\
        - add: action, target, want, note. When the owner says not to ask again about a kind of action (\"以后这类不用问我\"), add a \
        rule with want skip for the targets they meant, no wider; when they say always to ask first, want ask. Tell them the \
        rule is kept and that they can see and delete it in your settings.\n\
        - list. remove: id, when the owner takes a preference back.\n\n\
        Delete, install and grant (a new client or a new permission) are a hard floor: they are asked in the conversation every \
        time, and no rule can change that.";

    fn kind_for(&self, args: &Value) -> ToolKind {
        match args.get("op").and_then(Value::as_str) {
            Some("check" | "list") => ToolKind::ReadOnly,
            _ => ToolKind::Mutating,
        }
    }

    fn describe(&self, args: &Value, _env: &dyn Environment) -> ToolDescription {
        let text = |key: &str| args.get(key).and_then(Value::as_str).unwrap_or("");
        ToolDescription {
            category: ToolCategory::Other,
            title: format!("{} confirmation rules {} {}", text("op"), text("action"), text("target")).trim().to_string(),
            locations: Vec::new(),
        }
    }

    async fn run(&self, args: Args, ctx: ToolCtx) -> Result<ToolOutput, ToolError> {
        let file = ctx
            .session_state
            .get::<RulesFile>()
            .ok_or_else(|| ToolError::Failed("confirmation rules are not available in this session".into()))?;
        let path = file.0.clone();
        let action = || args.action.ok_or_else(|| ToolError::InvalidArgs("action is required".into()));
        let target = || args.target.clone().filter(|t| !t.trim().is_empty()).ok_or_else(|| ToolError::InvalidArgs("target is required".into()));
        let read = |path: &Path| load(path).map_err(|err| ToolError::Failed(format!("cannot read the rules: {err}")));
        match args.op {
            Op::Check => {
                let (action, target) = (action()?, target()?);
                // An unreadable file covers nothing: ask.
                let rules = load(&path).unwrap_or_default();
                let decision = decide(&rules, action, &target);
                let head = if decision.ask { "Ask first" } else { "No need to ask" };
                Ok(ToolOutput::text(format!("{head}: {}.", decision.why)).with_details(serde_json::json!({ "ask": decision.ask })))
            }
            Op::List => Ok(ToolOutput::text(listing(&read(&path)?))),
            Op::Add => {
                let want = args.want.ok_or_else(|| ToolError::InvalidArgs("want is required on add".into()))?;
                let (action, target) = (action()?, target()?);
                let note = args.note.unwrap_or_default();
                let rule = tokio::task::spawn_blocking(move || add(&path, action, &target, want, &note))
                    .await
                    .map_err(|err| ToolError::Failed(err.to_string()))?
                    .map_err(ToolError::Failed)?;
                Ok(ToolOutput::text(format!(
                    "Kept rule {}: {} \"{}\": {}.",
                    rule.id,
                    word(rule.action),
                    rule.target,
                    want_word(rule.want)
                )))
            }
            Op::Remove => {
                let id = args.id.ok_or_else(|| ToolError::InvalidArgs("id is required on remove".into()))?;
                match remove(&path, id.trim()).map_err(|err| ToolError::Failed(format!("cannot change the rules: {err}")))? {
                    Some(rule) => Ok(ToolOutput::text(format!("Removed rule {}.", rule.id))),
                    None => Err(ToolError::Failed(format!("there is no rule {id}"))),
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn rule(id: &str, action: ConfirmAction, target: &str, want: ConfirmWant) -> ConfirmRule {
        ConfirmRule { id: id.into(), action, target: target.into(), want, note: String::new(), created_at: String::new() }
    }

    #[test]
    fn patterns_match_whole_and_ignore_case() {
        assert!(matches("*", "anything"));
        assert!(matches("*@example.com", "Email to ALICE@example.com"));
        assert!(!matches("*@example.com", "alice@example.com.cn"));
        assert!(matches("团队群", "团队群"));
        assert!(!matches("团队群", "团队群外"));
        assert!(matches("a*b*c", "axxbyyc"));
        assert!(!matches("a*b*c", "axxbyy"));
    }

    #[test]
    fn without_rules_she_asks() {
        assert!(decide(&[], ConfirmAction::Send, "x").ask);
    }

    #[test]
    fn a_skip_rule_covers_its_targets_only() {
        let rules = [rule("c_1", ConfirmAction::Send, "*@example.com", ConfirmWant::Skip)];
        assert!(!decide(&rules, ConfirmAction::Send, "bob@example.com").ask);
        assert!(decide(&rules, ConfirmAction::Send, "bob@other.com").ask);
        assert!(decide(&rules, ConfirmAction::Pay, "bob@example.com").ask);
    }

    #[test]
    fn asking_wins_a_conflict() {
        let rules = [
            rule("c_1", ConfirmAction::Send, "*", ConfirmWant::Skip),
            rule("c_2", ConfirmAction::Send, "*boss*", ConfirmWant::Ask),
        ];
        assert!(!decide(&rules, ConfirmAction::Send, "team channel").ask);
        let d = decide(&rules, ConfirmAction::Send, "email to my boss");
        assert!(d.ask && d.why.contains("c_2"), "{d:?}");
    }

    #[test]
    fn the_hard_floor_is_asked_whatever_the_file_says() {
        // Even a rule written into the file by hand does not take the question away.
        for action in [ConfirmAction::Delete, ConfirmAction::Install, ConfirmAction::Grant] {
            let rules = [rule("c_1", action, "*", ConfirmWant::Skip)];
            assert!(decide(&rules, action, "x").ask);
        }
    }

    #[test]
    fn the_store_refuses_to_loosen_the_hard_floor() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join(FILE);
        for action in [ConfirmAction::Delete, ConfirmAction::Install, ConfirmAction::Grant] {
            assert!(add(&path, action, "*", ConfirmWant::Skip, "").unwrap_err().contains("hard floor"));
        }
        // Asking more is always fine.
        add(&path, ConfirmAction::Delete, "*", ConfirmWant::Ask, "").unwrap();
        assert_eq!(load(&path).unwrap().len(), 1);
    }

    #[test]
    fn rules_are_kept_listed_and_removed() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("bot").join(FILE);
        assert!(load(&path).unwrap().is_empty());
        let kept = add(&path, ConfirmAction::Send, " 团队群 ", ConfirmWant::Skip, "以后发团队群不用问我").unwrap();
        assert!(kept.id.starts_with("c_") && kept.id.len() == 8, "{}", kept.id);
        assert_eq!(kept.target, "团队群");
        let rules = load(&path).unwrap();
        assert_eq!(rules, vec![kept.clone()]);
        assert!(!decide(&rules, ConfirmAction::Send, "团队群").ask);
        assert!(listing(&rules).contains(&kept.id));
        assert_eq!(remove(&path, &kept.id).unwrap(), Some(kept.clone()));
        assert_eq!(remove(&path, &kept.id).unwrap(), None);
        assert!(decide(&load(&path).unwrap(), ConfirmAction::Send, "团队群").ask);
    }
}
