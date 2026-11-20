//! The built-in tool policy (registered as the last `before_tool` hook).
//!
//! Her asks nobody before an action: a call runs unless something here refuses it. What
//! refuses one is the user's own word or a disaster that cannot be undone:
//! - a deny rule, which sees every command a line may run (see [`rules::analyze`]);
//! - a command that deletes a critical path: the file system root, a top-level directory, the
//!   home directory, or the workspace and the directories that hold it;
//! - a file tool writing another bot's memory;
//! - in the browser and on the desktop, what the configuration refuses (see [`site_policy`]);
//! - in a patrol run, anything but looking and writing its own notes (docs/her-patrol.md).
//!
//! [`site_policy`]: crate::site_policy

use async_trait::async_trait;
use her_llm::ToolCall;
use her_protocol::CommandRule;
use her_protocol::RuleDecision;
use her_tools::Authority;
use her_tools::Tool;
use her_tools::ToolKind;
use her_tools::ToolOrigin;
use std::path::Path;

use crate::hooks::Gate;
use crate::hooks::Hooks;
use crate::hooks::SessionView;
use crate::protected;
use crate::protected::canonical_lossy;
use crate::rules;
use crate::rules::Analysis;
use crate::rules::ShellContext;
use crate::rules::Target;

/// How a refusal in a patrol run starts: the run's report lists what it would have done.
pub(crate) const PATROL_REFUSED: &str = "This routine is a patrol: it only looks";

const OTHERS_MEMORY: &str = "This is a bot's memory, and only the bot itself writes it. Say in your reply what it should know.";
const OWNERS_ENTRIES: &str = "Knowledge entries are saved by the user alone: propose one with propose_memory, and they save it or decline it.";

pub struct ToolPolicy;

#[async_trait]
impl Hooks for ToolPolicy {
    async fn before_tool(&self, call: &ToolCall, tool: &dyn Tool, authorities: &[Authority], view: &SessionView) -> Gate {
        if let Some(notes) = &view.patrol
            && let Some(reason) = patrol_refusal(call, tool, notes, view)
        {
            return Gate::Deny(reason);
        }
        if tool.origin_for(&call.arguments.to_value()) == ToolOrigin::Mcp {
            return evaluate_mcp(call, view);
        }
        // Tools that say what they do on which site are judged by that (browser-design §7.1).
        if !authorities.is_empty() {
            return crate::site_policy::evaluate(authorities, view).await;
        }
        match through_call_tool(call) {
            Some(inner) => evaluate(&inner, view),
            None => evaluate(call, view),
        }
    }
}

/// Why a patrol run may not make the call: all it does is look, and write its notes. A tool from
/// outside is refused whatever it says of itself: its read-only hint is a hint (docs/her-patrol.md §2).
fn patrol_refusal(call: &ToolCall, tool: &dyn Tool, notes: &Path, view: &SessionView) -> Option<String> {
    let args = call.arguments.to_value();
    let inner = through_call_tool(call);
    let target = inner.as_ref().unwrap_or(call);
    let external = tool.origin_for(&args) == ToolOrigin::Mcp && call.name != crate::mcp::tools::SEARCH_TOOL;
    let note = matches!(target.name.as_str(), "write" | "edit")
        && target.arguments.to_value().get("path").and_then(|v| v.as_str()).is_some_and(|path| {
            canonical_lossy(&view.env.resolve(path)).starts_with(notes)
        });
    if !external && (note || tool.kind_for(&args) == ToolKind::ReadOnly) {
        return None;
    }
    Some(format!(
        "{PATROL_REFUSED}, and `{}` may change something, so it did not run. Do not try it another way: write it \
         in your notes and your reply as a suggestion for the owner, who decides in their own conversation.",
        target.name
    ))
}

/// A built-in tool run through call_tool, as the call it stands for.
fn through_call_tool(call: &ToolCall) -> Option<ToolCall> {
    if call.name != crate::mcp::tools::CALL_TOOL {
        return None;
    }
    let args = call.arguments.to_value();
    let name = args.get("name")?.as_str()?.to_owned();
    let inner = args.get("arguments").cloned().unwrap_or(serde_json::Value::Object(Default::default()));
    Some(ToolCall { id: call.id.clone(), name, arguments: inner.into(), arguments_error: None, provider_data: None })
}

/// A tool from outside runs unless a deny rule names it by its full name, as a command's first
/// word.
fn evaluate_mcp(call: &ToolCall, view: &SessionView) -> Gate {
    let args = call.arguments.to_value();
    let name = match args.get("name").and_then(|v| v.as_str()) {
        Some(name) if call.name == crate::mcp::tools::CALL_TOOL => name.to_string(),
        _ => call.name.clone(),
    };
    match rules::first_match(&view.rules, &[name], RuleDecision::Deny) {
        Some(rule) => Gate::Deny(blocked("tool", rule)),
        None => Gate::Allow,
    }
}

pub fn evaluate(call: &ToolCall, view: &SessionView) -> Gate {
    let args = call.arguments.to_value();
    let arg = |key: &str| args.get(key).and_then(|v| v.as_str()).unwrap_or("");
    match call.name.as_str() {
        "bash" => evaluate_bash(arg("command"), view),
        "write" | "edit" => evaluate_file_change(arg("path"), view),
        _ => Gate::Allow,
    }
}

fn blocked(what: &str, rule: &CommandRule) -> String {
    format!(
        "The {what} is blocked by the rule `{}`{}",
        rule.prefix.join(" "),
        rule.reason.as_deref().map(|r| format!(": {r}")).unwrap_or_default()
    )
}

fn evaluate_bash(command: &str, view: &SessionView) -> Gate {
    let home = std::env::home_dir();
    let ctx = ShellContext {
        cwd: view.env.cwd(),
        home: home.as_deref(),
        cdpath: std::env::var_os("CDPATH").is_some_and(|value| !value.is_empty()),
    };
    // The analysis reads a POSIX shell's words. PowerShell takes a backslash as part of a
    // path, where a POSIX shell takes it for an escape: as slashes, the paths stay paths.
    let powershell = her_tools::local_env::is_powershell(&view.env.os().shell);
    let analyzed = if powershell { command.replace('\\', "/") } else { command.to_string() };
    let analysis = rules::analyze(&analyzed, &ctx);
    // Deny rules see every command the line may run: behind wrappers, inside scripts, under an
    // absolute path, and past git's global options.
    let denied = analysis.found.iter().find_map(|found| {
        rules::rule_forms(found)
            .iter()
            .find_map(|form| rules::first_match(&view.rules, form, RuleDecision::Deny))
    });
    if let Some(rule) = denied {
        return Gate::Deny(blocked("command", rule));
    }
    match critical_deletion(&analysis, view, home.as_deref()) {
        Some(reason) => Gate::Deny(reason),
        None => Gate::Allow,
    }
}

/// Why the command is refused for what it deletes. Only what the words show is judged: a
/// target that cannot be read before the command runs does not stop it.
fn critical_deletion(analysis: &Analysis, view: &SessionView, home: Option<&Path>) -> Option<String> {
    for target in rules::deletion_targets(analysis) {
        let (word, judged, cwds, contents) = match target {
            Target::Unknown(_) | Target::Input => continue,
            Target::Path { word, cwds } => (word.clone(), emptied_directory(&word).unwrap_or(word), cwds, false),
            Target::Contents { word, cwds } => (word.clone(), word, cwds, true),
        };
        for cwd in &cwds {
            if let Some(what) = protected::critical(&rules::resolve_in(cwd, &judged, home), view) {
                return Some(if contents {
                    format!("Deleting everything in {} would remove {what}.", code(&word))
                } else {
                    format!("Deleting {} would remove {what}.", code(&word))
                });
            }
        }
    }
    None
}

/// `word` in backticks, unless it already has some.
fn code(word: &str) -> String {
    if word.contains('`') { word.to_string() } else { format!("`{word}`") }
}

/// The directory that `dir/*` and the like empty completely. A pattern with other
/// characters, such as `*.o`, only removes some entries and is judged as a path.
fn emptied_directory(word: &str) -> Option<String> {
    let trimmed = word.trim_end_matches('/');
    let last = trimmed.rsplit('/').next()?;
    let wildcard = last.strip_prefix('.').unwrap_or(last);
    if wildcard.is_empty() || !wildcard.chars().all(|c| c == '*' || c == '?') {
        return None;
    }
    let fixed: Vec<&str> = trimmed.split('/').take_while(|part| !has_glob(part)).collect();
    let fixed = fixed.join("/");
    Some(match fixed.as_str() {
        "" if word.starts_with('/') => "/".to_string(),
        "" => ".".to_string(),
        _ => fixed,
    })
}

fn has_glob(word: &str) -> bool {
    word.contains(['*', '?', '['])
}

/// A bot's memory is its own notes: it keeps them, and no other session writes them
/// (docs/her-memory.md §6), save its knowledge entries and proposals, which no session's file
/// tools write (§10). Every other file change runs.
fn evaluate_file_change(raw_path: &str, view: &SessionView) -> Gate {
    let path = canonical_lossy(&view.env.resolve(raw_path));
    match protected::memory(&path, view) {
        Some(false) => Gate::Deny(OTHERS_MEMORY.into()),
        Some(true) if protected::owners_entries(&path, view) => Gate::Deny(OWNERS_ENTRIES.into()),
        Some(true) | None => Gate::Allow,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use her_tools::LocalEnvironment;
    use serde_json::json;
    use std::path::PathBuf;
    use std::sync::Arc;

    fn view(dir: &Path) -> SessionView {
        SessionView {
            rules: vec![rule(&["rm", "-rf"], None), rule(&["curl"], Some("No network"))],
            env: Arc::new(LocalEnvironment::new(dir.to_path_buf())),
            data_dir: canonical_lossy(&dir.join("her-home")),
            memory_dir: None,
            patrol: None,
            browser: Arc::default(),
            desktop: Default::default(),
        }
    }

    fn rule(prefix: &[&str], reason: Option<&str>) -> CommandRule {
        CommandRule {
            prefix: prefix.iter().map(|w| w.to_string()).collect(),
            decision: RuleDecision::Deny,
            reason: reason.map(str::to_string),
        }
    }

    fn call(name: &str, args: serde_json::Value) -> ToolCall {
        ToolCall { id: "c".into(), name: name.into(), arguments: args.into(), arguments_error: None, provider_data: None }
    }

    fn gate(command: &str, view: &SessionView) -> Gate {
        evaluate(&call("bash", json!({ "command": command })), view)
    }

    fn denied(gate: Gate) -> String {
        match gate {
            Gate::Deny(reason) => reason,
            Gate::Allow => panic!("allowed"),
        }
    }

    /// A workspace `a/proj` inside a temporary directory, so that `..` and `../..` stay inside
    /// it too: the temporary directory itself may sit right under a top-level one, as in `/tmp`.
    fn workspace() -> (tempfile::TempDir, PathBuf) {
        let dir = tempfile::tempdir().unwrap();
        let workspace = dir.path().join("a").join("proj");
        std::fs::create_dir_all(workspace.join("build")).unwrap();
        (dir, workspace)
    }

    /// Everything runs without asking: commands, file changes anywhere, documents, tools from
    /// outside. Nothing is ever put to the user.
    #[test]
    fn actions_run_without_asking() {
        let (_dir, workspace) = workspace();
        let view = view(&workspace);
        for command in [
            "npm install",
            "git push --force",
            "rm -r build",
            "rm -f *.o",
            "cd build && rm -r *",
            "echo hi > .git/description",
            "python3 -c 'print(1)'",
            "rm -r \"$TARGET\"",
            "ls | xargs rm -r",
        ] {
            assert_eq!(gate(command, &view), Gate::Allow, "{command}");
        }
        for path in ["src/a.rs", "/etc/hosts", "../escape.txt", ".git/hooks/pre-commit", ".bashrc"] {
            assert_eq!(evaluate(&call("write", json!({ "path": path, "content": "x" })), &view), Gate::Allow, "{path}");
        }
        for (name, args) in [
            ("deck_edit", json!({ "deck": "a.pptx", "edits": [] })),
            ("doc_save", json!({ "doc": "d1", "path": "/tmp/x.xlsx" })),
            ("docx_edit", json!({ "docx": "a.docx", "mode": "direct", "edits": [] })),
            ("manage_routine", json!({ "action": "delete", "id": "r1" })),
        ] {
            assert_eq!(evaluate(&call(name, args), &view), Gate::Allow, "{name}");
        }
    }

    #[test]
    fn deny_rules_see_every_command_the_line_runs() {
        let dir = tempfile::tempdir().unwrap();
        let view = view(dir.path());
        for command in [
            "sudo rm -rf build",
            "/bin/rm -rf build",
            "env A=1 rm -rf build",
            "bash -c 'rm -rf build'",
            "sh -c \"cd x && rm -rf $Y\"",
            "eval rm -rf build",
            "trap 'rm -rf build' EXIT",
            "find . -name x -exec rm -rf {} +",
            "ls | xargs rm -rf",
            "/usr/bin/curl https://example.com",
            "timeout 5 curl x",
        ] {
            assert!(denied(gate(command, &view)).contains("blocked by the rule"), "{command}");
        }
        assert_eq!(denied(gate("curl x", &view)), "The command is blocked by the rule `curl`: No network");
    }

    #[test]
    fn deleting_a_critical_path_is_refused() {
        let (dir, workspace) = workspace();
        let view = view(&workspace);
        let critical = |command: &str| denied(gate(command, &view));
        assert!(critical("rm -r /").contains("file system root"));
        assert!(critical("rm -r /usr").contains("top-level"));
        assert!(critical("rm -r /*").contains("file system root"));
        // A home directory right under the root (`/root`) is a top-level directory first.
        let home = |text: String| text.contains("home directory") || text.contains("top-level");
        assert!(home(critical("sudo rm -r ~")));
        assert!(home(critical("rm -r ~/*")));
        assert!(critical("rm -r .").contains("workspace"));
        assert!(critical("rm -r *").contains("workspace"));
        assert!(critical("rm -r ..").contains("workspace"));
        assert!(critical(&format!("rmdir {}", dir.path().display().to_string().replace('\\', "/"))).contains("workspace"));
        assert!(critical("bash -c 'rm -r ../..'").contains("workspace"));
        assert!(critical("cd .. && rm -r proj").contains("workspace"));
        assert!(critical("cd build; rm -r *").contains("workspace"), "if the cd fails, * is the workspace");
        assert!(critical("env -C .. rm -r proj").contains("workspace"));
        assert!(home(critical("cd && rm -r *")));
        assert!(critical("trap 'rm -r ..' EXIT").contains("workspace"));
        assert!(critical("find . -delete").contains("everything in `.`"));
        assert!(home(critical("find ~ -type f -exec rm {} +")));
    }

    /// A bot's memory (docs/her-memory.md §6): its own session writes it, and no other session's
    /// file tools do. The rest of a bot's directory is written like any other file.
    #[test]
    fn a_bots_memory_is_written_by_the_bot_alone() {
        let dir = tempfile::tempdir().unwrap();
        let memory = canonical_lossy(&dir.path().join("her-home/bots/b1/memory"));
        let line = memory.join("MEMORY.md").display().to_string();
        let write = |path: &str| call("write", json!({ "path": path, "content": "x" }));
        let mut own = view(dir.path());
        own.memory_dir = Some(memory.clone());
        assert_eq!(evaluate(&write(&line), &own), Gate::Allow);
        assert!(matches!(evaluate(&write(&line), &view(dir.path())), Gate::Deny(_)));
        let mut another = view(dir.path());
        another.memory_dir = Some(canonical_lossy(&dir.path().join("her-home/bots/b2/memory")));
        assert!(matches!(evaluate(&write(&line), &another), Gate::Deny(_)), "another bot's");
        let settings = dir.path().join("her-home/bots/b1/bot.json").display().to_string();
        assert_eq!(evaluate(&write(&settings), &own), Gate::Allow);
    }

    #[test]
    fn knowledge_entries_and_proposals_are_not_written_with_file_tools() {
        let dir = tempfile::tempdir().unwrap();
        let memory = canonical_lossy(&dir.path().join("her-home/bots/b1/memory"));
        let mut own = view(dir.path());
        own.memory_dir = Some(memory.clone());
        for part in ["knowledge/tea.md", "proposals/p_1.json"] {
            let path = memory.join(part).display().to_string();
            assert_eq!(evaluate(&call("write", json!({ "path": path, "content": "x" })), &own), Gate::Deny(OWNERS_ENTRIES.into()), "{part}");
            assert!(matches!(evaluate(&call("edit", json!({ "path": path, "edits": [] })), &own), Gate::Deny(_)), "{part}");
        }
        let page = memory.join("knowledge.md").display().to_string();
        assert_eq!(evaluate(&call("write", json!({ "path": page, "content": "x" })), &own), Gate::Allow);
    }

    /// A tool for the patrol tests: of a given origin and kind, or saying neither.
    struct Fake {
        spec: her_llm::ToolSpec,
        origin: ToolOrigin,
        kind: Option<ToolKind>,
    }

    #[async_trait]
    impl Tool for Fake {
        fn spec(&self) -> &her_llm::ToolSpec {
            &self.spec
        }

        fn kind(&self) -> ToolKind {
            self.kind.unwrap_or_else(|| Fake::unsaid().kind())
        }

        fn origin(&self) -> ToolOrigin {
            self.origin
        }

        async fn execute(&self, _args: serde_json::Value, _ctx: her_tools::ToolCtx) -> Result<her_tools::ToolOutput, her_tools::ToolError> {
            unreachable!()
        }
    }

    impl Fake {
        fn new(name: &str, origin: ToolOrigin, kind: Option<ToolKind>) -> Fake {
            Fake { spec: her_llm::ToolSpec { name: name.into(), description: String::new(), parameters: json!({}) }, origin, kind }
        }

        /// What the trait says of a tool that says nothing.
        fn unsaid() -> impl Tool {
            struct Unsaid(her_llm::ToolSpec);
            #[async_trait]
            impl Tool for Unsaid {
                fn spec(&self) -> &her_llm::ToolSpec {
                    &self.0
                }
                async fn execute(&self, _args: serde_json::Value, _ctx: her_tools::ToolCtx) -> Result<her_tools::ToolOutput, her_tools::ToolError> {
                    unreachable!()
                }
            }
            Unsaid(her_llm::ToolSpec { name: "unsaid".into(), description: String::new(), parameters: json!({}) })
        }
    }

    async fn judged(call: &ToolCall, tool: &dyn Tool, view: &SessionView) -> Gate {
        ToolPolicy.before_tool(call, tool, &[], view).await
    }

    /// A patrol run (docs/her-patrol.md §2): it looks and writes its notes; everything else is
    /// refused, however it is reached, and says what to do instead.
    #[tokio::test]
    async fn a_patrol_only_looks_and_writes_its_notes() {
        let (_dir, workspace) = workspace();
        let notes = workspace.parent().unwrap().join("patrol").join("r_1");
        std::fs::create_dir_all(&notes).unwrap();
        let mut patrol = view(&workspace);
        patrol.patrol = Some(canonical_lossy(&notes));
        let tools: std::collections::HashMap<String, Arc<dyn Tool>> = her_tools::builtin::builtin_tools(Default::default())
            .into_iter()
            .map(|tool| (tool.spec().name.clone(), tool))
            .collect();
        let note = notes.join("notes.md").display().to_string();
        let allowed = [
            ("read", json!({ "path": "src/a.rs" })),
            ("write", json!({ "path": note })),
            ("edit", json!({ "path": note, "edits": [] })),
        ];
        for (name, args) in allowed {
            assert_eq!(judged(&call(name, args.clone()), tools[name].as_ref(), &patrol).await, Gate::Allow, "{name} {args}");
        }
        let refused = [
            ("bash", json!({ "command": "ls" })),
            ("bash", json!({ "command": format!("echo x > {note}") })),
            ("write", json!({ "path": "src/a.rs" })),
            ("write", json!({ "path": format!("{}/../../x", notes.display()) })),
            ("edit", json!({ "path": "/etc/hosts", "edits": [] })),
        ];
        for (name, args) in refused {
            let reason = denied(judged(&call(name, args.clone()), tools[name].as_ref(), &patrol).await);
            assert!(reason.starts_with(PATROL_REFUSED) && reason.contains(&format!("`{name}`")), "{name} {args}: {reason}");
        }
        // The same calls in another session run.
        let plain = view(&workspace);
        assert_eq!(judged(&call("bash", json!({ "command": "ls" })), tools["bash"].as_ref(), &plain).await, Gate::Allow);
    }

    #[tokio::test]
    async fn a_patrol_refuses_what_says_nothing_and_every_connector() {
        let (_dir, workspace) = workspace();
        let mut patrol = view(&workspace);
        patrol.patrol = Some(canonical_lossy(&workspace.join("notes")));
        assert_eq!(Fake::unsaid().kind(), ToolKind::Mutating, "a tool that says nothing changes something");
        let refused = [
            Fake::new("unsaid", ToolOrigin::Builtin, None),
            Fake::new("spawn_agent", ToolOrigin::Builtin, Some(ToolKind::Concurrent)),
            // A connector's tool that says it only reads: a hint, not a promise.
            Fake::new("mcp__notes__echo", ToolOrigin::Mcp, Some(ToolKind::ReadOnly)),
            Fake::new(crate::mcp::tools::CALL_TOOL, ToolOrigin::Mcp, Some(ToolKind::ReadOnly)),
        ];
        for tool in &refused {
            let name = &tool.spec.name;
            assert!(denied(judged(&call(name, json!({})), tool, &patrol).await).starts_with(PATROL_REFUSED), "{name}");
        }
        // Looking for tools, and a built-in tool through call_tool, are judged as themselves.
        let search = Fake::new(crate::mcp::tools::SEARCH_TOOL, ToolOrigin::Mcp, Some(ToolKind::ReadOnly));
        assert_eq!(judged(&call(crate::mcp::tools::SEARCH_TOOL, json!({})), &search, &patrol).await, Gate::Allow);
        let through = Fake::new(crate::mcp::tools::CALL_TOOL, ToolOrigin::Builtin, Some(ToolKind::Mutating));
        let write = call(crate::mcp::tools::CALL_TOOL, json!({ "name": "write", "arguments": { "path": "src/a.rs" } }));
        assert!(denied(judged(&write, &through, &patrol).await).contains("`write`"));
        let note = call(crate::mcp::tools::CALL_TOOL, json!({ "name": "write", "arguments": { "path": "notes/n.md" } }));
        assert_eq!(judged(&note, &through, &patrol).await, Gate::Allow);
    }

    #[test]
    fn emptied_directories() {
        assert_eq!(emptied_directory("*").as_deref(), Some("."));
        assert_eq!(emptied_directory("/*").as_deref(), Some("/"));
        assert_eq!(emptied_directory("/*/*").as_deref(), Some("/"));
        assert_eq!(emptied_directory("~/*").as_deref(), Some("~"));
        assert_eq!(emptied_directory("build/.*").as_deref(), Some("build"));
        assert_eq!(emptied_directory("*/").as_deref(), Some("."));
        assert_eq!(emptied_directory("*.o"), None);
        assert_eq!(emptied_directory("build"), None);
        assert_eq!(emptied_directory("/"), None);
    }
}
