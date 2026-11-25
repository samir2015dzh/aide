package acp

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"

	"github.com/samir2015dzh/aide/internal/provider"
)

// sameModelsOnce lists each model an agent offers once (工单 #98 补充,
// #117). DSH offers its models by provider, and the same model can come by
// more than one line of the same provider (its API and its account). The
// key that makes two one model: the same provider (the provider its ID
// names, or its group's name; punctuation and a trailing "account" aside)
// and the same model ID (case, "_" and spaces aside; never its display name,
// which differs between lines). Each is listed once: as the one the session
// runs when it is that one, else the first; its lines said in Source. Models
// of different providers (a custom one too) stay apart.
//
// Naming (工单 #117, owner ruling 2026-11-24): uniform across the list,
// provider first — "<provider> · <specific model ID>" (e.g.
// "DeepSeek · deepseek-v4-flash"), never leading with a marketing display
// name; when no provider is known, the specific model ID alone.
func sameModelsOnce(xs []choice, current string) []provider.Model {
	var out []provider.Model
	var canon []string // its model ID as compared
	keys := map[string]int{}
	for _, x := range xs {
		vendor, model := modelParts(x.Value)
		var ks []string // by the provider its ID names, and by its group's
		for _, v := range []string{vendor, x.Group} {
			if v != "" && !isGenericGroup(v) {
				ks = append(ks, norm(v, true)+"\x00"+norm(model, false))
			}
		}
		if len(ks) == 0 {
			ks = append(ks, "\x00"+norm(model, false))
		}
		i, ok := -1, false
		for _, k := range ks {
			if i, ok = keys[k]; ok {
				break
			}
		}
		if !ok {
			i = len(out)
			out = append(out, provider.Model{ID: x.Value, Source: x.Group})
			canon = append(canon, norm(model, false))
		}
		for _, k := range ks {
			keys[k] = i
		}
		if !ok {
			continue
		}
		m := &out[i]
		if x.Value == current {
			m.ID = x.Value
		}
		if x.Group != "" && !slices.Contains(strings.Split(m.Source, " · "), x.Group) {
			m.Source = strings.TrimPrefix(m.Source+" · "+x.Group, " · ")
		}
	}
	for i := range out {
		if p := providerPrefix(out[i].Source, out[i].ID); p != "" {
			out[i].Name = p + " · " + canon[i]
		} else {
			out[i].Name = canon[i]
		}
	}
	return out
}

// providerPrefix is the provider label placed ahead of the specific model ID
// ("DeepSeek" for "DeepSeek · DeepSeek Account" or "DeepSeek Account", else
// the vendor prefix of the ID). Trailing "account" qualifiers are stripped so
// API and account lines of the same provider share one prefix.
func providerPrefix(source, id string) string {
	var first string
	if source != "" {
		for _, g := range strings.Split(source, " · ") {
			g = strings.TrimSpace(g)
			if g == "" || isGenericGroup(g) {
				continue
			}
			clean := stripAccountSuffix(g)
			if first == "" {
				first = clean
			}
			if clean == g {
				return clean
			}
		}
		if first != "" {
			return first
		}
	}
	if vendor, _ := modelParts(id); vendor != "" {
		return stripAccountSuffix(vendor)
	}
	return ""
}

// isGenericGroup reports container labels that are not a provider name
// (e.g. a single "Models" group in ACP config options).
func isGenericGroup(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "models", "model", "default", "all":
		return true
	default:
		return false
	}
}
func stripAccountSuffix(s string) string {
	low := strings.ToLower(strings.TrimSpace(s))
	for _, suf := range []string{" account", "-account", "_account"} {
		if strings.HasSuffix(low, suf) {
			if trimmed := strings.TrimSpace(s[:len(s)-len(suf)]); trimmed != "" {
				return trimmed
			}
		}
	}
	return strings.TrimSpace(s)
}

// NormalizeModels is models an agent's session offered, kept from an
// earlier Aide, as a session's are listed now (工单 #117): each once, named
// one way, by where from. One kept before #98 补充 is named "name (group)"
// with no source: its group is that. Their reasoning efforts stay.
func NormalizeModels(ms []provider.Model, current string) []provider.Model {
	var xs []choice
	for _, m := range ms {
		name, groups := m.Name, strings.Split(m.Source, " · ")
		if i := strings.LastIndex(name, " ("); m.Source == "" && i > 0 && strings.HasSuffix(name, ")") {
			name, groups = name[:i], []string{name[i+2 : len(name)-1]}
		}
		for _, g := range groups {
			xs = append(xs, choice{Value: m.ID, Name: name, Group: g})
		}
	}
	out := sameModelsOnce(xs, current)
	for i := range out {
		if k := slices.IndexFunc(ms, func(m provider.Model) bool { return m.ID == out[i].ID }); k >= 0 {
			out[i].Efforts, out[i].Resolved = ms[k].Efforts, ms[k].Resolved
		}
	}
	return out
}

// modelParts are the provider and the model a model ID names: DSH's
// ["provider","model"], or provider/model; else no provider.
func modelParts(id string) (vendor, model string) {
	var pair []string
	if json.Unmarshal([]byte(id), &pair) == nil && len(pair) >= 2 {
		return pair[0], strings.Join(pair[1:], "/")
	}
	if v, m, ok := strings.Cut(id, "/"); ok {
		return v, m
	}
	return "", id
}

// norm is a name as compared: lower case, "_" and spaces as "-"; a
// provider's also without punctuation and a trailing "account" (its
// account line is the same provider).
func norm(s string, vendor bool) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if !vendor {
		return strings.NewReplacer("_", "-", " ", "-").Replace(s)
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
	return strings.TrimSuffix(s, "account")
}
