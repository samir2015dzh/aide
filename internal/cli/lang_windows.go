package cli

import "golang.org/x/sys/windows"

// systemLanguage is the user's display language ("zh-CN"), "" when unknown.
func systemLanguage() string {
	if l, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil && len(l) > 0 {
		return l[0]
	}
	return ""
}
