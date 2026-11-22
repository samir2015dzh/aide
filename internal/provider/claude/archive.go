package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Archiving in the Claude desktop app (工单 #67): a session Aide runs is
// listed there by the record Aide wrote for it (desktop.go); archived on one
// side, it is archived on the other. The app keeps it in the record
// (isArchived) and in its folder's archived-sessions.idx; both are set, the
// rest of the record kept as it is. Like the record itself, the app reads
// them when it starts.

// Archive archives (or unarchives) the session ref in the desktop app's
// index, where it has a record; none (no app, or not listed): nothing to do.
func (p *Provider) Archive(_ context.Context, ref string, archived bool) error {
	home, _ := os.UserHomeDir()
	for _, idx := range desktopIndexes(home, os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA")) {
		files, _ := filepath.Glob(filepath.Join(idx, "*", "*", "local_"+ref+".json"))
		for _, f := range files {
			if err := setRecordArchived(f, archived); err != nil {
				return err
			}
			if err := setIndexArchived(filepath.Dir(f), "local_"+ref, archived); err != nil {
				return err
			}
		}
	}
	return nil
}

// Archived is whether the desktop app has each of these sessions archived,
// for those it lists.
func (p *Provider) Archived(refs []string) map[string]bool {
	home, _ := os.UserHomeDir()
	recs := records(desktopIndexes(home, os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA")))
	if len(recs) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, r := range recs {
		id := r.CLISessionID
		if id == "" {
			id = strings.TrimPrefix(r.SessionID, "local_")
		}
		if slices.Contains(refs, id) {
			out[id] = r.IsArchived
		}
	}
	return out
}

func setRecordArchived(path string, archived bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var r map[string]any
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	if v, _ := r["isArchived"].(bool); v == archived {
		return nil
	}
	r["isArchived"] = archived
	return writeWhole(path, r)
}

// setIndexArchived puts id in the folder's archived-sessions.idx, or takes it out.
func setIndexArchived(dir, id string, archived bool) error {
	path := filepath.Join(dir, "archived-sessions.idx")
	idx := map[string]any{"v": 1, "archived": []any{}}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &idx)
	} else if !archived {
		return nil
	}
	var list []any
	present := false
	if l, ok := idx["archived"].([]any); ok {
		for _, x := range l {
			if x == id {
				present = true
				if !archived {
					continue
				}
			}
			list = append(list, x)
		}
	}
	if present == archived {
		return nil
	}
	if archived {
		list = append(list, id)
	}
	if list == nil {
		list = []any{}
	}
	idx["archived"] = list
	return writeWhole(path, idx)
}

// writeWhole writes v as JSON under another name first, then puts it in place.
func writeWhole(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".aide.tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
