package acp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The acp profile as `dsh --dump-config` prints it: its llm-pi-ai row,
// here with a provider of its own.
const acpDump = `- id: credentials
  name: '@deepseek-ai/dsh-credentials-local'
- id: llm-pi-ai
  name: '@deepseek-ai/dsh-llm-pi-ai'
  config:
    retry: 2
    providers:
      mine:
        apiKeyEnv: MINE_KEY
- id: session-persistence-jsonl
  name: '@deepseek-ai/dsh-session-persistence-jsonl'
  config:
    root: !!js dshHomePath('sessions')
`

// The desktop profile's patch layer, as DSH's settings write it.
const desktopPatch = `# the owner's
- id: permission
  config:
    defaultPreset: read-only
- id: llm-pi-ai
  config:
    providers:
      magpie:
        displayName: Magpie
        apiKeyEnv: MAGPIE_API_KEY
        api: openai-completions
        baseURL: !!js env.MAGPIE_URL
        models:
          - id: v4-flash
      mine:
        apiKeyEnv: OTHER_KEY
`

func desktopHome(t *testing.T, patch string) []string {
	t.Helper()
	home := t.TempDir()
	if patch != "" {
		dir := filepath.Join(home, "profiles", "desktop")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cordis.patch.yml"), []byte(patch), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return []string{"PATH=/bin", "DSH_HOME=" + home}
}

// The overlay is the acp profile's row with the desktop profile's
// providers added: the acp profile's own stay as they are, and so does the
// rest of its row; !!js values reach dsh as written.
func TestDSHProvidersPatch(t *testing.T) {
	desktop := dshDesktopProviders(desktopHome(t, desktopPatch))
	if desktop == nil {
		t.Fatal("the desktop profile's providers were not read")
	}
	b, added, err := dshProvidersPatch([]byte(acpDump), desktop)
	if err != nil || strings.Join(added, ",") != "magpie" {
		t.Fatalf("added %v, %v", added, err)
	}
	var got []struct {
		ID     string `yaml:"id"`
		Config struct {
			Retry     int `yaml:"retry"`
			Providers map[string]struct {
				APIKeyEnv string `yaml:"apiKeyEnv"`
				API       string `yaml:"api"`
			} `yaml:"providers"`
		} `yaml:"config"`
	}
	if err := yaml.Unmarshal(b, &got); err != nil || len(got) != 1 {
		t.Fatalf("overlay %s: %v", b, err)
	}
	c := got[0].Config
	if got[0].ID != "llm-pi-ai" || c.Retry != 2 || len(c.Providers) != 2 || c.Providers["mine"].APIKeyEnv != "MINE_KEY" ||
		c.Providers["magpie"].API != "openai-completions" || c.Providers["magpie"].APIKeyEnv != "MAGPIE_API_KEY" {
		t.Fatalf("overlay:\n%s", b)
	}
	if !strings.Contains(string(b), "baseURL: !!js env.MAGPIE_URL") {
		t.Fatalf("a !!js value changed:\n%s", b)
	}
}

// Nothing to add, nothing to overlay: no desktop profile, none of its own
// providers, all of them the acp profile's already, or no llm-pi-ai row
// to give them to (left out, or turned off).
func TestDSHProvidersNoPatch(t *testing.T) {
	if dshDesktopProviders(desktopHome(t, "")) != nil {
		t.Fatal("providers without a desktop profile")
	}
	if dshDesktopProviders(desktopHome(t, "- id: permission\n  config:\n    defaultPreset: read-only\n")) != nil {
		t.Fatal("providers from a patch layer without them")
	}
	desktop := dshDesktopProviders(desktopHome(t, desktopPatch))
	for name, dump := range map[string]string{
		"all the acp profile's": "- id: llm-pi-ai\n  name: '@deepseek-ai/dsh-llm-pi-ai'\n  config:\n    providers:\n      magpie: {}\n      mine: {}\n",
		"no row":                "- id: credentials\n  name: '@deepseek-ai/dsh-credentials-local'\n",
		"turned off":            "- id: llm-pi-ai\n  name: '@deepseek-ai/dsh-llm-pi-ai'\n  disabled: true\n",
	} {
		if b, added, err := dshProvidersPatch([]byte(dump), desktop); b != nil || added != nil || err != nil {
			t.Errorf("%s: %s %v %v", name, b, added, err)
		}
	}
	// A row without config gets one with the providers only.
	b, added, err := dshProvidersPatch([]byte("- id: llm-pi-ai\n  name: '@deepseek-ai/dsh-llm-pi-ai'\n"), desktop)
	if err != nil || strings.Join(added, ",") != "magpie,mine" || !strings.HasPrefix(string(b), "- config:\n    providers:\n") {
		t.Fatalf("a row without config: %s %v %v", b, added, err)
	}
}

// DSH's home: $DSH_HOME as the session's environment has it, else ~/.dsh.
func TestDSHHome(t *testing.T) {
	if got := dshHome([]string{"DSH_HOME=/a", "DSH_HOME=/b"}); got != "/b" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("HOME", "/home/x")
	t.Setenv("USERPROFILE", "/home/x") // the home directory on Windows
	if got := dshHome([]string{"PATH=/bin"}); got != filepath.Join("/home/x", ".dsh") {
		t.Fatalf("got %q", got)
	}
}
