//! Providers of one's own: an OpenAI- or Anthropic-compatible service at an address the person
//! gives, such as a company gateway. They live in models.toml, the file that already holds such
//! providers when written by hand: it is edited in place, so everything else in it stays as written,
//! and the key goes to the credential store, never into the file. A provider the built-in catalog
//! does not define, and that is no account connection, is one of these.

use her_llm::AuthConfig;
use her_llm::Catalog;
use her_llm::ProviderConfig;
use her_protocol::api::CustomModel;
use her_protocol::api::CustomProvider;
use toml_edit::DocumentMut;
use toml_edit::Item;
use toml_edit::Table;

use crate::paths::HerHome;

/// The protocols a provider of one's own can speak.
pub const APIS: [&str; 3] = ["openai-chat", "anthropic-messages", "openai-responses"];

/// What models.toml says of a provider the person added; none for the built-in ones (even
/// changed there) and account connections.
pub fn summary(provider: &ProviderConfig) -> Option<CustomProvider> {
    if her_llm::catalog::is_builtin(&provider.id) || matches!(provider.auth, AuthConfig::Managed) {
        return None;
    }
    Some(CustomProvider {
        base_url: provider.base_url.clone(),
        api: provider.api.clone(),
        models: provider
            .listing
            .entries
            .iter()
            .map(|e| CustomModel { id: e.id.clone(), context_window: e.context_window, max_output_tokens: e.max_output_tokens })
            .collect(),
    })
}

/// The id of a new provider named `name`: its ASCII letters and digits in lower case, the words
/// joined by `-`, else `custom`; then `-2`, `-3`… while `taken`.
pub fn new_id(name: &str, taken: impl Fn(&str) -> bool) -> String {
    let words: Vec<String> = name
        .split(|c: char| !c.is_ascii_alphanumeric())
        .filter(|w| !w.is_empty())
        .map(str::to_ascii_lowercase)
        .collect();
    let mut base = words.join("-");
    base.truncate(40);
    let base = base.trim_end_matches('-');
    let base = if base.is_empty() { "custom" } else { base };
    let mut id = base.to_string();
    let mut n = 2;
    while taken(&id) {
        id = format!("{base}-{n}");
        n += 1;
    }
    id
}

/// The address as saved: http or https, with a host, without a trailing `/`. Otherwise why not.
pub fn base_url(given: &str) -> Result<String, String> {
    let trimmed = given.trim().trim_end_matches('/');
    match url::Url::parse(trimmed) {
        Ok(url) if matches!(url.scheme(), "http" | "https") && url.host().is_some() && url.query().is_none() => Ok(trimmed.to_string()),
        _ => Err(format!("`{}` is not an http or https address", given.trim())),
    }
}

/// Whether two addresses are the same service (scheme, host and port): a saved key goes to no other.
pub fn same_service(a: &str, b: &str) -> bool {
    match (url::Url::parse(a), url::Url::parse(b)) {
        (Ok(a), Ok(b)) => a.origin() == b.origin(),
        _ => false,
    }
}

/// Checks what is saved for a provider: a name, a protocol her speaks, models with usable ids,
/// none twice.
pub fn check(name: &str, api: &str, models: &[CustomModel]) -> Result<(), String> {
    if name.trim().is_empty() {
        return Err("the provider needs a name".into());
    }
    if !APIS.contains(&api) {
        return Err(format!("unknown protocol `{api}`: expected {}", APIS.join(", ")));
    }
    if models.is_empty() {
        return Err("choose or name at least one model".into());
    }
    for (i, model) in models.iter().enumerate() {
        if !her_llm::discovery::valid_id(&model.id) {
            return Err(format!("`{}` cannot be a model id", model.id));
        }
        if models[..i].iter().any(|m| m.id == model.id) {
            return Err(format!("the model `{}` is named twice", model.id));
        }
        if model.context_window == Some(0) || model.max_output_tokens == Some(0) {
            return Err(format!("the limits of `{}` must be more than 0", model.id));
        }
    }
    Ok(())
}

/// Writes provider `id` into models.toml: its name, protocol, address and models. The rest of the
/// file, and of the provider's own table, stays; so do the other settings of a model kept. A new
/// provider lists no models of its service: its models are the ones chosen. Returns the catalog
/// read from the file written; a file her cannot read afterwards is put back as it was.
pub fn save(home: &HerHome, id: &str, name: &str, base_url: &str, api: &str, models: &[CustomModel]) -> Result<Catalog, String> {
    edit(home, |providers| {
        let mut table = match providers.remove(id) {
            Some(item) => item.into_table().map_err(|_| format!("`providers.{id}` in models.toml is not a table"))?,
            None => {
                let mut table = Table::new();
                table["list_models"] = toml_edit::value(false);
                table
            }
        };
        table["name"] = toml_edit::value(name.trim());
        table["api"] = toml_edit::value(api);
        table["base_url"] = toml_edit::value(base_url);
        let mut before = written_models(table.remove("models"));
        let mut list = toml_edit::ArrayOfTables::new();
        for model in models {
            let mut entry = match before.iter().position(|t| t.get("id").and_then(Item::as_str) == Some(model.id.as_str())) {
                Some(at) => before.remove(at),
                None => {
                    let mut entry = Table::new();
                    entry["id"] = toml_edit::value(model.id.as_str());
                    entry
                }
            };
            for (key, limit) in [("context_window", model.context_window), ("max_output_tokens", model.max_output_tokens)] {
                match limit {
                    Some(n) => entry[key] = toml_edit::value(i64::from(n)),
                    None => {
                        entry.remove(key);
                    }
                }
            }
            list.push(entry);
        }
        table["models"] = Item::ArrayOfTables(list);
        providers.insert(id, Item::Table(table));
        Ok(())
    })
}

/// Takes provider `id` out of models.toml. Returns the catalog read from the file written.
pub fn remove(home: &HerHome, id: &str) -> Result<Catalog, String> {
    edit(home, |providers| {
        providers.remove(id);
        Ok(())
    })
}

/// A provider's models as written, either as `[[…models]]` or as an inline array.
fn written_models(item: Option<Item>) -> Vec<Table> {
    match item {
        Some(Item::ArrayOfTables(list)) => list.into_iter().collect(),
        Some(Item::Value(toml_edit::Value::Array(list))) => {
            list.into_iter().filter_map(|v| v.as_inline_table().cloned()).map(|t| t.into_table()).collect()
        }
        _ => Vec::new(),
    }
}

/// Changes the `providers` table of models.toml under its lock, and checks that the catalog still
/// reads; if not, the file is put back.
fn edit(home: &HerHome, change: impl FnOnce(&mut Table) -> Result<(), String>) -> Result<Catalog, String> {
    let path = home.models_file();
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| format!("cannot write {}: {e}", path.display()))?;
    }
    let _lock = crate::config_edit::lock(&path, home.root()).map_err(|e| e.to_string())?;
    let before = match std::fs::read(&path) {
        Ok(bytes) => Some(bytes),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => None,
        Err(e) => return Err(format!("cannot read {}: {e}", path.display())),
    };
    let text = String::from_utf8_lossy(before.as_deref().unwrap_or_default()).into_owned();
    let mut document: DocumentMut = text.parse().map_err(|e| format!("cannot read {}: {e}", path.display()))?;
    let providers = document.entry("providers").or_insert(toml_edit::table());
    let providers = providers.as_table_mut().ok_or_else(|| format!("`providers` in {} is not a table", path.display()))?;
    providers.set_implicit(true);
    change(providers)?;
    crate::config_edit::replace_file(&path, document.to_string().as_bytes()).map_err(|e| format!("cannot write {}: {e}", path.display()))?;
    match Catalog::load(Some(&path)) {
        Ok(catalog) => Ok(catalog),
        Err(error) => {
            let restored = match &before {
                Some(bytes) => crate::config_edit::replace_file(&path, bytes),
                None => std::fs::remove_file(&path),
            };
            if let Err(e) = restored {
                tracing::warn!("cannot put {} back: {e}", path.display());
            }
            Err(error.to_string())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn model(id: &str, window: Option<u32>, output: Option<u32>) -> CustomModel {
        CustomModel { id: id.into(), context_window: window, max_output_tokens: output }
    }

    fn home() -> (tempfile::TempDir, HerHome) {
        let dir = tempfile::tempdir().unwrap();
        let home = HerHome::new(dir.path().to_path_buf());
        (dir, home)
    }

    #[test]
    fn ids_come_from_names() {
        let none = |_: &str| false;
        assert_eq!(new_id("Magpie Gateway", none), "magpie-gateway");
        assert_eq!(new_id("  我的网关 ", none), "custom");
        assert_eq!(new_id("Magpie 网关 (prod)", none), "magpie-prod");
        let taken = |id: &str| ["deepseek", "deepseek-2"].contains(&id);
        assert_eq!(new_id("DeepSeek", taken), "deepseek-3");
    }

    #[test]
    fn addresses_are_checked() {
        assert_eq!(base_url(" https://gw.example:3443/v1/ ").unwrap(), "https://gw.example:3443/v1");
        assert!(base_url("gw.example/v1").is_err());
        assert!(base_url("ftp://gw.example").is_err());
        assert!(same_service("https://gw.example:3443/v1", "https://gw.example:3443/v2"));
        assert!(!same_service("https://gw.example:3443/v1", "https://other.example:3443/v1"));
        assert!(!same_service("https://gw.example:3443/v1", "http://gw.example:3443/v1"));
    }

    #[test]
    fn what_is_saved_is_checked() {
        assert!(check("Magpie", "openai-chat", &[model("a", None, None)]).is_ok());
        assert!(check(" ", "openai-chat", &[model("a", None, None)]).is_err());
        assert!(check("Magpie", "gemini", &[model("a", None, None)]).is_err());
        assert!(check("Magpie", "openai-chat", &[]).is_err());
        assert!(check("Magpie", "openai-chat", &[model("has space", None, None)]).is_err());
        assert!(check("Magpie", "openai-chat", &[model("a", None, None), model("a", None, None)]).is_err());
        assert!(check("Magpie", "openai-chat", &[model("a", Some(0), None)]).is_err());
    }

    #[test]
    fn a_new_provider_is_added_beside_what_was_written() {
        let (_dir, home) = home();
        let written = "# my own\n[providers.deepseek]\nbase_url = \"http://proxy.local/v1\" # the proxy\n\n[providers.vllm]\napi = \"openai-chat\"\nbase_url = \"http://10.0.0.5:8000/v1\"\nmodels = [{ id = \"qwen3-coder\" }]\n";
        std::fs::write(home.models_file(), written).unwrap();
        let models = [model("gpt-5", Some(400_000), Some(128_000)), model("glm-5", None, None)];
        let catalog = save(&home, "magpie", "Magpie 网关", "https://gw.example:3443/v1", "openai-chat", &models).unwrap();
        let text = std::fs::read_to_string(home.models_file()).unwrap();
        assert!(text.starts_with(written), "{text}");
        let magpie = &catalog.providers["magpie"];
        assert_eq!((magpie.name.as_str(), magpie.base_url.as_str(), magpie.api.as_str()), ("Magpie 网关", "https://gw.example:3443/v1", "openai-chat"));
        assert_eq!(magpie.listing.path, None, "its models are the ones chosen");
        assert_eq!(magpie.auth, AuthConfig::ApiKey { env: Vec::new(), value: None }, "the key is in the credential store");
        assert_eq!(summary(magpie).unwrap().models, models);
        // What was there reads as before; built-in providers are not providers of one's own.
        assert_eq!(catalog.providers["deepseek"].base_url, "http://proxy.local/v1");
        assert!(summary(&catalog.providers["deepseek"]).is_none());
        assert_eq!(summary(&catalog.providers["vllm"]).unwrap().models, [model("qwen3-coder", None, None)]);
    }

    #[test]
    fn a_change_keeps_what_her_does_not_write() {
        let (_dir, home) = home();
        std::fs::write(
            home.models_file(),
            "[providers.vllm]\napi = \"openai-chat\"\nbase_url = \"http://10.0.0.5:8000/v1\"\nheaders = { X-Team = \"a\" }\n\n[[providers.vllm.models]]\nid = \"qwen3-coder\"\nreasoning = true\ncontext_window = 1000\n\n[[providers.vllm.models]]\nid = \"old\"\n",
        )
        .unwrap();
        let catalog = save(&home, "vllm", "vLLM", "http://10.0.0.6:8000/v1", "openai-chat", &[model("new", None, Some(8_000)), model("qwen3-coder", None, None)]).unwrap();
        let vllm = &catalog.providers["vllm"];
        assert_eq!(vllm.base_url, "http://10.0.0.6:8000/v1");
        assert_eq!(vllm.headers["X-Team"], "a");
        assert_eq!(vllm.listing.path.as_deref(), Some("/models"), "a hand-written provider keeps listing");
        assert_eq!(summary(vllm).unwrap().models, [model("new", None, Some(8_000)), model("qwen3-coder", None, None)]);
        assert_eq!(vllm.entry("qwen3-coder").unwrap().reasoning, Some(true), "a kept model's other settings stay");
    }

    #[test]
    fn removing_takes_only_that_provider() {
        let (_dir, home) = home();
        std::fs::write(home.models_file(), "[providers.deepseek]\nbase_url = \"http://proxy.local/v1\"\n").unwrap();
        save(&home, "magpie", "Magpie", "https://gw.example/v1", "anthropic-messages", &[model("m", None, None)]).unwrap();
        let catalog = remove(&home, "magpie").unwrap();
        assert!(!catalog.providers.contains_key("magpie"));
        assert_eq!(std::fs::read_to_string(home.models_file()).unwrap(), "[providers.deepseek]\nbase_url = \"http://proxy.local/v1\"\n");
    }

    #[test]
    fn a_file_her_cannot_read_is_left_alone() {
        let (_dir, home) = home();
        std::fs::write(home.models_file(), "[providers\n").unwrap();
        assert!(save(&home, "magpie", "Magpie", "https://gw.example/v1", "openai-chat", &[model("m", None, None)]).is_err());
        assert_eq!(std::fs::read_to_string(home.models_file()).unwrap(), "[providers\n");
        // Readable TOML that is no catalog: put back as it was.
        std::fs::write(home.models_file(), "[providers.broken]\nname = \"no address\"\n").unwrap();
        assert!(save(&home, "magpie", "Magpie", "https://gw.example/v1", "openai-chat", &[model("m", None, None)]).is_err());
        assert_eq!(std::fs::read_to_string(home.models_file()).unwrap(), "[providers.broken]\nname = \"no address\"\n");
    }
}
