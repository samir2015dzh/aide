//! Paths that need extra care: deleting a critical path is refused, and a bot's memory is
//! written by that bot alone. Nothing here asks anyone.

use std::path::Component;
use std::path::Path;
use std::path::PathBuf;

use crate::hooks::SessionView;

/// Whether `path` (absolute and normalized) is in a bot's memory, `bots/<id>/memory` in the data
/// directory, and whether that memory is this session's own.
pub(crate) fn memory(path: &Path, view: &SessionView) -> Option<bool> {
    let path = canonical_lossy(path);
    if view.memory_dir.as_ref().is_some_and(|own| path.starts_with(own)) {
        return Some(true);
    }
    let mut below = path.strip_prefix(view.data_dir.join("bots")).ok()?.components();
    below.next()?;
    (below.next()?.as_os_str() == crate::bot_memory::DIR).then_some(false)
}

/// Why deleting `path` (absolute and normalized) would be a disaster: it is the file system
/// root, a top-level directory, the home directory, or the workspace, the working directory
/// or one of their ancestors.
pub(crate) fn critical(path: &Path, view: &SessionView) -> Option<&'static str> {
    let forms = [path.to_path_buf(), canonical_lossy(path)];
    let home = std::env::home_dir();
    let anchors: Vec<PathBuf> = view
        .env
        .workspace_roots()
        .iter()
        .map(PathBuf::as_path)
        .chain([view.env.cwd()])
        .flat_map(|dir| [dir.to_path_buf(), canonical_lossy(dir)])
        .collect();
    for form in &forms {
        let depth = form.components().filter(|c| matches!(c, Component::Normal(_))).count();
        if depth == 0 {
            return Some("the file system root");
        }
        if depth == 1 {
            return Some("a top-level directory");
        }
        if home.as_ref().is_some_and(|home| form == home || canonical_lossy(home) == *form) {
            return Some("the home directory");
        }
        if anchors.iter().any(|anchor| anchor.starts_with(form)) {
            return Some("the workspace or a directory that contains it");
        }
    }
    None
}

/// [`canonical_lossy`] of her's data directory, worked out once per directory.
pub(crate) fn canonical_data_dir(root: &Path) -> PathBuf {
    static CACHE: std::sync::OnceLock<parking_lot::Mutex<std::collections::HashMap<PathBuf, PathBuf>>> =
        std::sync::OnceLock::new();
    let cache = CACHE.get_or_init(Default::default);
    if let Some(dir) = cache.lock().get(root) {
        return dir.clone();
    }
    let dir = canonical_lossy(root);
    cache.lock().insert(root.to_path_buf(), dir.clone());
    dir
}

/// Links followed through dangling targets; a longer chain is treated as a loop.
const MAX_LINK_HOPS: usize = 40;

/// Canonical form of the deepest existing ancestor, with the remaining components appended. A
/// dangling symbolic link on the way is followed: writing through it creates its target, so the
/// target is the path that gets changed.
pub(crate) fn canonical_lossy(path: &Path) -> PathBuf {
    canonical_lossy_from(path, 0)
}

fn canonical_lossy_from(path: &Path, hops: usize) -> PathBuf {
    let mut existing = path;
    let mut rest = Vec::new();
    loop {
        if let Ok(canonical) = dunce::canonicalize(existing) {
            let mut out = canonical;
            for part in rest.iter().rev() {
                out.push(part);
            }
            return out;
        }
        if hops < MAX_LINK_HOPS
            && std::fs::symlink_metadata(existing).is_ok_and(|meta| meta.file_type().is_symlink())
            && let Ok(target) = std::fs::read_link(existing)
        {
            // Relative to the directory holding the link; `..` is left to `canonicalize`.
            let mut next = existing.parent().map_or_else(|| target.clone(), |dir| dir.join(&target));
            for part in rest.iter().rev() {
                next.push(part);
            }
            return canonical_lossy_from(&next, hops + 1);
        }
        match (existing.parent(), existing.file_name()) {
            (Some(parent), Some(name)) => {
                rest.push(name.to_os_string());
                existing = parent;
            }
            _ => return path.to_path_buf(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use her_tools::LocalEnvironment;
    use std::sync::Arc;

    fn view_for(workspace: &Path, data_dir: &Path) -> SessionView {
        SessionView {
            rules: Vec::new(),
            env: Arc::new(LocalEnvironment::new(workspace.to_path_buf())),
            data_dir: canonical_lossy(data_dir),
            memory_dir: None,
            browser: Arc::default(),
            desktop: Default::default(),
        }
    }

    #[test]
    fn critical_paths() {
        let dir = tempfile::tempdir().unwrap();
        let workspace = dir.path().join("a").join("proj");
        std::fs::create_dir_all(workspace.join("build")).unwrap();
        let view = view_for(&workspace, &dir.path().join("her-home"));
        assert_eq!(critical(Path::new("/"), &view), Some("the file system root"));
        assert_eq!(critical(Path::new("/usr"), &view), Some("a top-level directory"));
        if let Some(home) = std::env::home_dir() {
            assert_eq!(critical(&home, &view), Some("the home directory"));
        }
        assert!(critical(&workspace, &view).is_some());
        assert!(critical(&dir.path().join("a"), &view).is_some());
        assert!(critical(dir.path(), &view).is_some());
        assert_eq!(critical(&workspace.join("build"), &view), None);
        assert_eq!(critical(&workspace.join("missing/x"), &view), None);
        assert_eq!(critical(Path::new("/usr/local/lib/x"), &view), None);
    }
}
