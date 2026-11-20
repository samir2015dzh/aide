//! A bot's knowledge entries (docs/her-memory.md §10): notes that carry a trigger, the words
//! that say when to think of them. Unlike `MEMORY.md`, an entry is not always in context: the
//! system prompt lists each one by its trigger, and its text joins the conversation only when
//! the owner's words hit the trigger. Entries live in `memory/knowledge/<name>.md`; Her only
//! proposes them (`memory/proposals/<id>.json`), and the owner saves, changes and deletes them.
//!
//! An entry's file:
//!
//! ```text
//! ---
//! trigger: 周报; weekly report
//! ---
//! The weekly report goes to the team's group on Friday afternoon, as a PDF.
//! ```

use std::path::Path;
use std::path::PathBuf;
use std::sync::Arc;

use async_trait::async_trait;
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
use serde::Serialize;
use serde_json::Value;

/// Entries, in a bot's memory folder.
pub const KNOWLEDGE: &str = "knowledge";
/// Proposals that wait for the owner, in a bot's memory folder.
pub const PROPOSALS: &str = "proposals";
/// An entry's text, at most; a longer file is cut when it is recalled.
pub const TEXT_LIMIT: usize = crate::bot_memory::LIMIT;
/// A trigger's length, at most.
pub const TRIGGER_LIMIT: usize = 300;
/// A name's length in characters, at most.
const NAME_LIMIT: usize = 64;
/// Phrases shorter than this, in bytes, never trigger: a single Latin letter is in every message,
/// where a single Chinese character (3 bytes) can be a word.
const PHRASE_MIN: usize = 2;
/// How many entries one request recalls, at most.
pub const RECALL_LIMIT: usize = 5;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Entry {
    pub name: String,
    /// Empty for a file written by hand without one: listed, never recalled by itself.
    pub trigger: String,
    pub text: String,
}

/// An entry Her proposed, which takes effect only once the owner saves it.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Proposal {
    pub id: String,
    pub name: String,
    pub trigger: String,
    pub text: String,
    /// RFC 3339.
    pub created_at: String,
}

pub fn knowledge_dir(memory: &Path) -> PathBuf {
    memory.join(KNOWLEDGE)
}

pub fn proposals_dir(memory: &Path) -> PathBuf {
    memory.join(PROPOSALS)
}

/// A name is a file name of its own: letters, digits, `-` and `_`, in any script.
pub fn check_name(name: &str) -> Result<(), String> {
    let count = name.chars().count();
    if count == 0 || count > NAME_LIMIT {
        return Err(format!("name must be 1 to {NAME_LIMIT} characters"));
    }
    if !name.chars().all(|c| c.is_alphanumeric() || c == '-' || c == '_') {
        return Err("name may hold only letters, digits, - and _".into());
    }
    Ok(())
}

/// The trigger and the text as given, trimmed, or why they cannot be saved.
pub fn check(trigger: &str, text: &str) -> Result<(String, String), String> {
    let (trigger, text) = (trigger.trim(), text.trim());
    if phrases(trigger).is_empty() {
        return Err("trigger needs at least one phrase longer than a single letter".into());
    }
    if trigger.len() > TRIGGER_LIMIT || trigger.contains('\n') {
        return Err(format!("trigger must be one line of at most {TRIGGER_LIMIT} bytes"));
    }
    if text.is_empty() || text.len() > TEXT_LIMIT {
        return Err(format!("text must be 1 to {TEXT_LIMIT} bytes"));
    }
    Ok((trigger.to_string(), text.to_string()))
}

/// The phrases of a trigger, lower case: it is split at `;`, `,`, `、`, `|` and their full-width
/// forms.
fn phrases(trigger: &str) -> Vec<String> {
    trigger
        .split([';', '；', ',', '，', '、', '|'])
        .map(|phrase| phrase.trim().to_lowercase())
        .filter(|phrase| phrase.len() >= PHRASE_MIN)
        .collect()
}

/// Whether `words` (the owner's) hold one of the trigger's phrases, ignoring case.
pub fn hits(trigger: &str, words: &str) -> bool {
    let words = words.to_lowercase();
    phrases(trigger).iter().any(|phrase| words.contains(phrase.as_str()))
}

fn parse(name: &str, raw: &str) -> Entry {
    let raw = raw.replace("\r\n", "\n");
    let (front, text) = raw
        .strip_prefix("---\n")
        .and_then(|rest| rest.split_once("\n---\n").or_else(|| rest.strip_suffix("\n---").map(|front| (front, ""))))
        .unwrap_or(("", raw.as_str()));
    let trigger = front.lines().find_map(|line| line.strip_prefix("trigger:")).unwrap_or("").trim().to_string();
    Entry { name: name.to_string(), trigger, text: text.trim().to_string() }
}

fn render(trigger: &str, text: &str) -> String {
    format!("---\ntrigger: {trigger}\n---\n{text}\n")
}

/// The entries in `memory`, by name. None when there are none or the folder cannot be read.
pub fn entries(memory: &Path) -> Vec<Entry> {
    let Ok(files) = std::fs::read_dir(knowledge_dir(memory)) else {
        return Vec::new();
    };
    let mut entries: Vec<Entry> = files
        .flatten()
        .filter_map(|file| {
            let path = file.path();
            let name = path.file_stem()?.to_str()?.to_string();
            if path.extension()? != "md" || name.starts_with('.') {
                return None;
            }
            let raw = std::fs::read_to_string(&path).ok()?;
            Some(parse(&name, &raw))
        })
        .collect();
    entries.sort_by(|a, b| a.name.cmp(&b.name));
    entries
}

/// The part of a bot's system prompt that lists its entries, after `<memory>`; empty with none.
/// Each is one line: its name and its trigger, never its text.
pub(crate) fn catalog(memory: &Path) -> String {
    let entries = entries(memory);
    if entries.is_empty() {
        return String::new();
    }
    let lines: Vec<String> = entries
        .iter()
        .map(|entry| match entry.trigger.as_str() {
            "" => format!("- {}", entry.name),
            trigger => format!("- {}: {trigger}", entry.name),
        })
        .collect();
    format!("<knowledge path=\"{}\">\n{}\n</knowledge>", knowledge_dir(memory).display(), lines.join("\n"))
}

/// The entries whose trigger `words` hit, at most [`RECALL_LIMIT`], each text cut to
/// [`TEXT_LIMIT`].
pub fn recall(memory: &Path, words: &str) -> Vec<Entry> {
    if words.trim().is_empty() {
        return Vec::new();
    }
    entries(memory)
        .into_iter()
        .filter(|entry| hits(&entry.trigger, words))
        .take(RECALL_LIMIT)
        .map(|mut entry| {
            if entry.text.len() > TEXT_LIMIT {
                let mut end = TEXT_LIMIT;
                while !entry.text.is_char_boundary(end) {
                    end -= 1;
                }
                entry.text.truncate(end);
                entry.text.push_str("\n(cut: the entry is longer than what is recalled)");
            }
            entry
        })
        .collect()
}

/// Saves an entry, in place of one of that name.
pub fn save(memory: &Path, name: &str, trigger: &str, text: &str) -> Result<Entry, String> {
    check_name(name)?;
    let (trigger, text) = check(trigger, text)?;
    let dir = knowledge_dir(memory);
    std::fs::create_dir_all(&dir).map_err(|e| format!("cannot create {}: {e}", dir.display()))?;
    let path = dir.join(format!("{name}.md"));
    std::fs::write(&path, render(&trigger, &text)).map_err(|e| format!("cannot write {}: {e}", path.display()))?;
    Ok(Entry { name: name.to_string(), trigger, text })
}

/// Deletes an entry. False when there was none of that name.
pub fn delete(memory: &Path, name: &str) -> Result<bool, String> {
    check_name(name)?;
    match std::fs::remove_file(knowledge_dir(memory).join(format!("{name}.md"))) {
        Ok(()) => Ok(true),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(false),
        Err(e) => Err(e.to_string()),
    }
}

/// Keeps a proposal for the owner. Nothing of it reaches the model until they save it.
pub fn propose(memory: &Path, name: &str, trigger: &str, text: &str) -> Result<Proposal, String> {
    check_name(name)?;
    let (trigger, text) = check(trigger, text)?;
    const DIGITS: &[u8] = b"abcdefghijklmnopqrstuvwxyz0123456789";
    let mut bits = rand::random::<u128>();
    let mut id = String::from("p_");
    for _ in 0..10 {
        id.push(DIGITS[(bits % DIGITS.len() as u128) as usize] as char);
        bits /= DIGITS.len() as u128;
    }
    let proposal = Proposal {
        id,
        name: name.to_string(),
        trigger,
        text,
        created_at: chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Millis, true),
    };
    let dir = proposals_dir(memory);
    std::fs::create_dir_all(&dir).map_err(|e| format!("cannot create {}: {e}", dir.display()))?;
    let json = serde_json::to_string_pretty(&proposal).map_err(|e| e.to_string())?;
    std::fs::write(dir.join(format!("{}.json", proposal.id)), json).map_err(|e| e.to_string())?;
    Ok(proposal)
}

/// The proposals that wait, oldest first.
pub fn proposals(memory: &Path) -> Vec<Proposal> {
    let Ok(files) = std::fs::read_dir(proposals_dir(memory)) else {
        return Vec::new();
    };
    let mut proposals: Vec<Proposal> = files
        .flatten()
        .filter(|file| file.path().extension().is_some_and(|ext| ext == "json"))
        .filter_map(|file| serde_json::from_slice(&std::fs::read(file.path()).ok()?).ok())
        .collect();
    proposals.sort_by(|a, b| (&a.created_at, &a.id).cmp(&(&b.created_at, &b.id)));
    proposals
}

/// The owner's answer to a proposal: saved as an entry (in place of one of its name), or
/// dropped. Either way it waits no more. `Ok(None)` when it is dropped.
pub fn answer(memory: &Path, id: &str, accept: bool) -> Result<Option<Entry>, String> {
    // It names a file: letters, digits and `_` only, as `propose` makes them.
    check_name(id).map_err(|_| format!("no proposal {id}"))?;
    let proposal = proposals(memory).into_iter().find(|p| p.id == id).ok_or_else(|| format!("no proposal {id}"))?;
    let saved = if accept { Some(save(memory, &proposal.name, &proposal.trigger, &proposal.text)?) } else { None };
    std::fs::remove_file(proposals_dir(memory).join(format!("{id}.json"))).map_err(|e| e.to_string())?;
    Ok(saved)
}

pub const PROPOSE_MEMORY: &str = "propose_memory";

/// Where a bot's main session keeps its memory: the one session that may propose entries.
pub(crate) struct KnowledgeLink(pub(crate) PathBuf);

pub(crate) fn tool() -> Arc<dyn Tool> {
    Arc::new(Typed::new(ProposeMemory))
}

#[derive(Debug, Deserialize, JsonSchema)]
struct ProposeArgs {
    /// The entry's file name: letters, digits, - and _, such as weekly-report. An existing entry's name replaces it once saved.
    name: String,
    /// When to think of it: short phrases the user would say when it matters, separated by ";", in the languages they use, such as "周报; weekly report". The text is given to you whenever the user's words contain one of them.
    trigger: String,
    /// What to know: full sentences that still make sense in a year, in the user's language, with the date they were learned.
    text: String,
}

struct ProposeMemory;

#[async_trait]
impl TypedTool for ProposeMemory {
    type Args = ProposeArgs;
    const NAME: &'static str = PROPOSE_MEMORY;
    const DESCRIPTION: &'static str = "Propose a knowledge entry: something worth knowing only when a subject comes up, with a \
        trigger that says when. The user saves it, or declines it, in Her's Memory page; until then it is not in your memory, so \
        never act as if it were. Say in your reply what you proposed and that they can save it there.";

    fn kind_for(&self, _args: &Value) -> ToolKind {
        ToolKind::Mutating
    }

    fn describe(&self, args: &Value, _env: &dyn Environment) -> ToolDescription {
        let name = args.get("name").and_then(Value::as_str).unwrap_or("an entry");
        ToolDescription { category: ToolCategory::Other, title: format!("propose \"{name}\" for memory"), locations: Vec::new() }
    }

    async fn run(&self, args: ProposeArgs, ctx: ToolCtx) -> Result<ToolOutput, ToolError> {
        let link = ctx
            .session_state
            .get::<KnowledgeLink>()
            .ok_or_else(|| ToolError::Failed("only your own conversation can propose entries".into()))?;
        let dir = link.0.clone();
        let proposal = tokio::task::spawn_blocking(move || propose(&dir, &args.name, &args.trigger, &args.text))
            .await
            .map_err(|e| ToolError::Failed(e.to_string()))?
            .map_err(ToolError::InvalidArgs)?;
        Ok(ToolOutput::text(format!(
            "Proposed \"{}\" ({}). It waits for the user in Her's Memory page and is not in your memory until they save it.",
            proposal.name, proposal.id
        )))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_trigger_hits_on_any_of_its_phrases_ignoring_case() {
        assert!(hits("周报; Weekly Report", "这周的周报写好了吗"));
        assert!(hits("周报; Weekly Report", "is the weekly report done?"));
        assert!(hits("发布会、上线", "12 月上线的事"));
        assert!(!hits("周报; Weekly Report", "明天的会议"));
        // One letter is in every message.
        assert!(!hits("a; ;", "a banana"));
        assert!(!hits("", "anything"));
    }

    #[test]
    fn a_file_without_front_matter_has_no_trigger() {
        assert_eq!(parse("x", "just text\n"), Entry { name: "x".into(), trigger: String::new(), text: "just text".into() });
        assert_eq!(parse("x", "---\ntrigger: a b\n---\nbody\n"), Entry { name: "x".into(), trigger: "a b".into(), text: "body".into() });
        assert_eq!(parse("x", "---\r\ntrigger: ab\r\n---\r\nbody"), Entry { name: "x".into(), trigger: "ab".into(), text: "body".into() });
    }

    #[test]
    fn entries_are_saved_listed_recalled_and_deleted() {
        let dir = tempfile::tempdir().unwrap();
        let memory = dir.path();
        assert_eq!(catalog(memory), "");
        save(memory, "weekly-report", "周报; weekly report", "周报周五下午发到团队群，PDF。").unwrap();
        save(memory, "launch", "发布会", "发布会定在 12 月 8 日。").unwrap();
        std::fs::write(knowledge_dir(memory).join("by-hand.md"), "no trigger here").unwrap();
        assert_eq!(
            catalog(memory),
            format!("<knowledge path=\"{}\">\n- by-hand\n- launch: 发布会\n- weekly-report: 周报; weekly report\n</knowledge>", knowledge_dir(memory).display())
        );
        let names = |words: &str| recall(memory, words).into_iter().map(|e| e.name).collect::<Vec<_>>();
        assert_eq!(names("周报怎么写？"), ["weekly-report"]);
        assert_eq!(names("发布会和周报"), ["launch", "weekly-report"]);
        assert!(names("no trigger here").is_empty());
        assert!(names("").is_empty());
        assert!(delete(memory, "launch").unwrap());
        assert!(!delete(memory, "launch").unwrap());
        assert!(names("发布会").is_empty());
    }

    #[test]
    fn what_cannot_be_saved_says_why() {
        let dir = tempfile::tempdir().unwrap();
        assert!(save(dir.path(), "../x", "ab", "t").unwrap_err().contains("name"));
        assert!(save(dir.path(), "", "ab", "t").unwrap_err().contains("name"));
        assert!(save(dir.path(), "x", "a", "t").unwrap_err().contains("trigger"));
        assert!(save(dir.path(), "x", "ab\ncd", "t").unwrap_err().contains("trigger"));
        assert!(save(dir.path(), "x", "ab", " ").unwrap_err().contains("text"));
        assert!(save(dir.path(), "x", "ab", &"x".repeat(TEXT_LIMIT + 1)).unwrap_err().contains("text"));
        assert!(save(dir.path(), "笔记_1", "ab", "t").is_ok());
    }

    #[test]
    fn a_proposal_is_an_entry_only_once_accepted() {
        let dir = tempfile::tempdir().unwrap();
        let memory = dir.path();
        let kept = propose(memory, "tea", "茶; tea", "主人只喝绿茶。").unwrap();
        let dropped = propose(memory, "coffee", "咖啡", "主人不喝咖啡。").unwrap();
        assert_eq!(proposals(memory).len(), 2);
        assert!(entries(memory).is_empty());
        assert!(recall(memory, "泡杯茶").is_empty());
        assert_eq!(answer(memory, &kept.id, true).unwrap().map(|e| e.name).as_deref(), Some("tea"));
        assert_eq!(answer(memory, &dropped.id, false).unwrap(), None);
        assert!(proposals(memory).is_empty());
        assert_eq!(entries(memory).iter().map(|e| e.name.as_str()).collect::<Vec<_>>(), ["tea"]);
        assert_eq!(recall(memory, "泡杯茶")[0].text, "主人只喝绿茶。");
        assert!(answer(memory, &kept.id, true).unwrap_err().contains("no proposal"));
        assert!(answer(memory, "../knowledge/tea", true).unwrap_err().contains("no proposal"));
    }
}
