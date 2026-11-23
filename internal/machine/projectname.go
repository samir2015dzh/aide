package machine

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// A project's default name (工单 #88): its folder's. The owner's apps make
// a project's id of its folder's name in lowercase ASCII (signin.ts
// projectIdFromFolder), so a folder named in Chinese ("D:\报表") got a
// placeholder id, "project" or "project-2", which said nothing of the
// folder. A new project whose id was made so is named by its folder now
// (one given an id of its own keeps being shown by it), and one still under
// such a placeholder is named so as the machine opens its store.

// folderOf is a path's last folder, with either separator ("D:\报表" →
// "报表", "/home/me/app/" → "app"); "" for a drive or the root alone.
func folderOf(path string) string {
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return ""
	}
	f := strings.TrimSpace(parts[len(parts)-1])
	if strings.HasSuffix(f, ":") { // "D:"
		return ""
	}
	return f
}

// autoID: id is what the apps make of folder f — its name in lowercase
// ASCII ("project" when nothing of it is), with "-2", "-3", … when taken.
func autoID(id, f string) bool {
	s := strings.TrimLeft(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(f)), "-._")
	if s == "" {
		s = "project"
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	n, ok := strings.CutPrefix(id, s+"-")
	return id == s || ok && n != "" && strings.Trim(n, "0123456789") == ""
}

// folderName is the name a new project in path is shown by: its folder's,
// with "-2", "-3", … when another project is shown by that already; "" when
// it is shown so by its id already, its id is not one made of its folder
// (the owner chose it), or its folder has no name.
func folderName(id, path string, others []Project) string {
	f := folderOf(path)
	if f == "" || f == id || !autoID(id, f) {
		return ""
	}
	if r := []rune(f); len(r) > maxProjectName-4 {
		f = string(r[:maxProjectName-4])
	}
	taken := map[string]bool{}
	for _, o := range others {
		if o.ID != id {
			taken[o.ID] = true
			if o.Name != "" {
				taken[o.Name] = true
			}
		}
	}
	name := f
	for n := 2; taken[name]; n++ {
		name = fmt.Sprintf("%s-%d", f, n)
	}
	return name
}

var placeholderID = regexp.MustCompile(`^project(-[0-9]+)?$`)

// placeheld: the project is still shown by the placeholder id the apps
// made for its folder. All three hold: the owner never named it (renaming
// sets its name, and a name is never touched); its id is "project" or
// "project-N"; and that id is what the apps' rule makes of its folder's
// name — nothing of it is lowercase ASCII (or it is "project" itself), so
// the placeholder, not a name the owner typed, is where the id came from.
func placeheld(p Project) bool {
	f := folderOf(p.Path)
	return p.Name == "" && placeholderID.MatchString(p.ID) && f != "" && autoID(p.ID, f)
}

// nameByFolder names the projects still shown by a placeholder id by their
// folders (once: a named one is no longer placeheld).
func (s *Store) nameByFolder() error {
	projects, err := s.Projects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if !placeheld(p) {
			continue
		}
		name := folderName(p.ID, p.Path, projects)
		if name == "" || utf8.RuneCountInString(name) > maxProjectName {
			continue
		}
		if _, err := s.SetProjectName(p.ID, name); err != nil {
			return err
		}
		projects[i].Name = name
	}
	return nil
}
