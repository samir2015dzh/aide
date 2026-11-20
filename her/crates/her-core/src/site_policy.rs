//! What browser and desktop calls may not do, by what they do on which site (docs/kernel/
//! browser-design.md §7.2): the URL, the address class, the domain lists, the sites and apps
//! the configuration denies. Nobody is asked: a call that nothing here refuses runs. Reads only
//! the `SessionView`.

use her_browser::policy::AddressClass;
use her_browser::policy::UrlKind;
use her_browser::policy::classify_url;
use her_browser::policy::url_kind;
use her_tools::Authority;
use url::Url;

use crate::config::AppLevel;
use crate::hooks::Gate;
use crate::hooks::SessionView;

pub(crate) async fn evaluate(authorities: &[Authority], view: &SessionView) -> Gate {
    for authority in authorities {
        if let Some(reason) = refusal(authority, view).await {
            return Gate::Deny(reason);
        }
    }
    Gate::Allow
}

async fn refusal(authority: &Authority, view: &SessionView) -> Option<String> {
    match authority {
        Authority::Navigate { url, .. } => navigate(url, view).await,
        Authority::Observe { .. } => None,
        Authority::Act { origin, .. } | Authority::Upload { origin, .. } => origin_refusal(origin, view).await,
        Authority::Script { origin, .. } => script(origin, view).await,
        Authority::AppObserve { app } | Authority::AppAct { app, .. } => {
            (view.desktop.app(app) == Some(AppLevel::Deny)).then(|| format!("{app} is denied under [desktop.apps]"))
        }
    }
}

/// Scripts: only with `browser.script_enabled`, never on a refused site.
async fn script(origin: &str, view: &SessionView) -> Option<String> {
    if !view.browser.script_enabled {
        return Some("running scripts is off (browser.script_enabled)".into());
    }
    origin_refusal(origin, view).await
}

async fn navigate(raw: &str, view: &SessionView) -> Option<String> {
    let Ok(url) = Url::parse(raw) else {
        return Some(format!("\"{raw}\" is not a valid URL"));
    };
    match url_kind(&url) {
        UrlKind::Refused(reason) => Some(reason),
        UrlKind::Blank | UrlKind::File(_) => None,
        UrlKind::Web => web_refusal(&url, view).await,
    }
}

/// Acting on a page: blank pages and local files send nothing anywhere.
async fn origin_refusal(origin: &str, view: &SessionView) -> Option<String> {
    if origin == "null" || origin == "file://" {
        return None;
    }
    let Ok(url) = Url::parse(origin) else {
        return Some(format!("cannot act on a page of origin {origin}"));
    };
    web_refusal(&url, view).await
}

/// A site the configuration denies, a denied domain, or a cloud metadata address.
async fn web_refusal(url: &Url, view: &SessionView) -> Option<String> {
    if let Some(reason) = view.browser.refusal(url) {
        return Some(reason);
    }
    (classify_url(url).await == AddressClass::Metadata)
        .then(|| format!("{} is a cloud metadata address", her_browser::policy::origin_of(url)))
}

#[cfg(test)]
mod tests {
    use super::*;
    use her_browser::policy::SiteLevel;
    use her_browser::policy::SitePolicy;
    use her_tools::LocalEnvironment;
    use her_tools::PageTarget;
    use std::path::PathBuf;
    use std::sync::Arc;

    fn view(policy: SitePolicy, root: &std::path::Path) -> SessionView {
        SessionView {
            rules: Vec::new(),
            env: Arc::new(LocalEnvironment::new(root.to_path_buf())),
            data_dir: root.join(".her-data"),
            memory_dir: None,
            browser: Arc::new(policy),
            desktop: Default::default(),
        }
    }

    fn page() -> PageTarget {
        PageTarget {
            tab: "1".into(),
            frame: "F".into(),
            loader: "L".into(),
        }
    }

    fn nav(url: &str, own: bool) -> Authority {
        Authority::Navigate { url: url.into(), own }
    }

    fn act_on(origin: &str, own: bool) -> Authority {
        Authority::Act {
            origin: origin.into(),
            target: page(),
            action: "click button \"Go\"".into(),
            own,
        }
    }

    /// Public, private and loopback sites, files and uploads: every one runs without a
    /// question, in either browser.
    #[tokio::test]
    async fn browser_calls_run_without_asking() {
        let dir = tempfile::tempdir().unwrap();
        let root = dunce::canonicalize(dir.path()).unwrap();
        let view = view(SitePolicy::default(), &root);
        for own in [false, true] {
            for url in ["https://93.184.215.14/docs", "http://192.168.0.7/", "http://localhost:5173/", "about:blank", "file:///etc/hosts"] {
                assert_eq!(evaluate(&[nav(url, own)], &view).await, Gate::Allow, "{url}");
            }
            for origin in ["https://93.184.215.14", "http://192.168.0.7", "http://127.0.0.1:3000"] {
                assert_eq!(evaluate(&[act_on(origin, own)], &view).await, Gate::Allow, "{origin}");
            }
        }
        let upload = Authority::Upload {
            origin: "https://93.184.215.14".into(),
            paths: vec![root.join("index.html"), PathBuf::from("/etc/hosts")],
            target: page(),
            own: true,
        };
        assert_eq!(evaluate(&[upload], &view).await, Gate::Allow);
        assert_eq!(evaluate(&[Authority::Observe { origin: "https://93.184.215.14".into() }], &view).await, Gate::Allow);
    }

    #[tokio::test]
    async fn denied_sites_metadata_addresses_and_bad_urls_are_refused() {
        let dir = tempfile::tempdir().unwrap();
        let mut policy = SitePolicy::default();
        policy.denied_domains.push("evil.test".into());
        policy.sites.insert("https://93.184.215.15".into(), SiteLevel::Deny);
        let view = view(policy, dir.path());
        assert!(matches!(evaluate(&[nav("https://93.184.215.15/", false)], &view).await, Gate::Deny(_)), "a deny holds");
        assert!(matches!(evaluate(&[nav("http://169.254.169.254/latest", false)], &view).await, Gate::Deny(_)));
        assert!(matches!(evaluate(&[nav("https://www.evil.test/", true)], &view).await, Gate::Deny(_)));
        assert!(matches!(evaluate(&[nav("javascript:alert(1)", false)], &view).await, Gate::Deny(_)));
        assert!(matches!(evaluate(&[act_on("https://evil.test", false)], &view).await, Gate::Deny(_)));
        // One refused authority refuses the call.
        assert!(matches!(
            evaluate(&[nav("https://93.184.215.14/", false), act_on("https://evil.test", false)], &view).await,
            Gate::Deny(_)
        ));
    }

    #[tokio::test]
    async fn scripts_run_only_when_enabled() {
        let dir = tempfile::tempdir().unwrap();
        let script = |origin: &str| Authority::Script { origin: origin.into(), target: page(), own: true };
        let off = view(SitePolicy::default(), dir.path());
        assert!(matches!(evaluate(&[script("http://localhost:3000")], &off).await, Gate::Deny(r) if r.contains("script_enabled")));
        let on = view(SitePolicy { script_enabled: true, ..SitePolicy::default() }, dir.path());
        assert_eq!(evaluate(&[script("http://localhost:3000")], &on).await, Gate::Allow);
        assert_eq!(evaluate(&[script("https://93.184.215.14")], &on).await, Gate::Allow);
        assert!(matches!(evaluate(&[script("http://169.254.169.254")], &on).await, Gate::Deny(_)));
    }

    #[tokio::test]
    async fn desktop_apps_run_unless_denied() {
        let dir = tempfile::tempdir().unwrap();
        let read = Authority::AppObserve { app: "com.apple.TextEdit".into() };
        let act = Authority::AppAct { app: "com.apple.TextEdit".into(), action: "type @s1:e2".into() };
        let mut view = view(SitePolicy::default(), dir.path());
        assert_eq!(evaluate(&[read.clone(), act.clone()], &view).await, Gate::Allow);
        let toml = "[desktop.apps]\n\"com.apple.TextEdit\" = \"deny\"\n";
        view.desktop = crate::config::Config::from_file(toml::from_str(toml).unwrap()).unwrap().desktop;
        assert!(matches!(evaluate(&[read], &view).await, Gate::Deny(_)));
        assert!(matches!(evaluate(&[act], &view).await, Gate::Deny(_)));
    }
}
