//! The built-in tool policy (registered as the last `before_tool` hook).
//!
//! Her asks nobody before an action: a call runs unless something here refuses it. What
//! refuses one is the user's own word or a disaster that cannot be undone:
//! - a deny rule, which sees every command a line may run (see [`rules::analyze`]);
//! - a command that deletes a critical path: the file system root, a top-level directory, the
//!   home directory, or the workspace and the directories that hold it;
//! - a file tool writing another bot's memory;
//! - in the browser and on the desktop, what the configuration refuses (see [`site_policy`]).
//!
//! [`site_policy`]: crate::site_policy

use async_trait::async_trait;
use her_llm::ToolCall;
use her_protocol::CommandRule;
use her_protocol::RuleDecision;
use her_tools::Authority;
use her_tools::Tool;
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

const OTHERS_MEMORY: &str = "This is a bot's memory, and only the bot itself writes it. Say in your reply what it should know.";

pub struct ToolPolicy;

#[async_trait]
impl Hooks for ToolPolicy {
    async fn before_tool(&self, call: &ToolCall, tool: &dyn Tool, authorities: &[Authority], view: &SessionView) -> Gate {
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
/// (docs/her-memory.md §6). Every other file change runs.
fn evaluate_file_change(raw_path: &str, view: &SessionView) -> Gate {
    match protected::memory(&canonical_lossy(&view.env.resolve(raw_path)), view) {
        Some(false) => Gate::Deny(OTHERS_MEMORY.into()),
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
