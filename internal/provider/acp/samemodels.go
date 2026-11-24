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
// Named one way in a list (工单 #117): as they are, when none is named by
// its ID as written or all are; a list mixing names ("DeepSeek-V4.1-Flash",
// "Gemini 3 Pro") with IDs as written ("deepseek-v4-pro"), each by its
// specific model ID as compared ("deepseek-v4-flash").
func sameModelsOnce(xs []choice, current string) []provider.Model {
	var out []provider.Model
	var named []bool   // its name is a display name, not its ID again
	var canon []string // its model ID as compared
	var raw []string   // its model ID as written
	keys := map[string]int{}
	for _, x := range xs {
		vendor, model := modelParts(x.Value)
		var ks []string // by the provider its ID names, and by its group's
		for _, v := range []string{vendor, x.Group} {
			if v != "" {
				ks = append(ks, norm(v, true)+"\x00"+norm(model, false))
			}
		}
		i, ok := -1, false
		for _, k := range ks {
			if i, ok = keys[k]; ok {
				break
			}
		}
		display := norm(x.Name, false) != norm(model, false) && x.Name != x.Value
		if !ok {
			i = len(out)
			out = append(out, provider.Model{ID: x.Value, Name: x.Name, Source: x.Group})
			named = append(named, display)
			canon = append(canon, norm(model, false))
			raw = append(raw, model)
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
		if display && !named[i] {
			m.Name, named[i] = x.Name, true
		}
		if x.Group != "" && !slices.Contains(strings.Split(m.Source, " · "), x.Group) {
			m.Source = strings.TrimPrefix(m.Source+" · "+x.Group, " · ")
		}
	}
	// Named by its ID as written: some, not all, is a list in two ways.
	asID := 0
	for i, m := range out {
		if m.Name == m.ID || m.Name == raw[i] || m.Name == canon[i] {
			asID++
		}
	}
	if asID > 0 && asID < len(out) {
		for i := range out {
			out[i].Name = canon[i]
		}
	}
	return out
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
