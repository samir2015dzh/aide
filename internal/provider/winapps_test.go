package provider

import "testing"

// The program a DisplayIcon or UninstallString names, as installers write
// them.
func TestCommandPath(t *testing.T) {
	for in, want := range map[string]string{
		`"C:\Program Files\App\app.exe",0`:                       `C:\Program Files\App\app.exe`,
		`C:\Program Files\App\app.exe,0`:                         `C:\Program Files\App\app.exe`,
		`"C:\Users\u\AppData\Local\App\Update.exe" --uninstall`:  `C:\Users\u\AppData\Local\App\Update.exe`,
		`C:\Users\u\Programs\app\Uninstall App.exe /currentuser`: `C:\Users\u\Programs\app\Uninstall App.exe`,
		`C:\App\app.ico`: `C:\App\app.ico`,
		``:               ``,
	} {
		if got := commandPath(in); got != want {
			t.Errorf("commandPath(%q) = %q, want %q", in, got, want)
		}
	}
}
