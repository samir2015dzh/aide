package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// TestRateLimitsRead (工单 #143, after upstream 1c862bde): asked
// account/rateLimits/read at its check (Codex 0.171, recorded), Codex says
// its account's windows and plan; they read as 工单 #86's windows are named
// (a week seven_day). An answer with no window, or a refusal, is no reading.
func TestRateLimitsRead(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "codex-0.171-rate-limits-read.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	answer := func(result json.RawMessage, fail error) func(string, any, any) error {
		return func(method string, params, v any) error {
			if method != "account/rateLimits/read" || params != nil {
				t.Fatalf("asked %s %v", method, params)
			}
			if fail != nil {
				return fail
			}
			return json.Unmarshal(result, v)
		}
	}
	got := usageFrom(answer(m.Result, nil))
	want := []provider.UsageWindow{{Key: "seven_day", Utilization: 61, ResetsAt: time.Unix(1795678770, 0)}}
	if got.Plan != "pro" || got.Unavailable != "" || !reflect.DeepEqual(got.Windows, want) {
		t.Fatalf("got %+v", got)
	}
	both := usageFrom(answer(json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":7,"windowDurationMins":10080,"resetsAt":1795678770},"secondary":{"usedPercent":30,"windowDurationMins":300,"resetsAt":1795600000},"planType":"plus"}}`), nil))
	if len(both.Windows) != 2 || both.Windows[0].Key != "five_hour" || both.Windows[1].Key != "seven_day" {
		t.Fatalf("the shortest first: %+v", both.Windows)
	}
	if u := usageFrom(answer(json.RawMessage(`{"rateLimits":{"primary":null,"secondary":null,"planType":"pro"}}`), nil)); u.Unavailable != provider.UsageNone || len(u.Windows) != 0 {
		t.Fatalf("no window: %+v", u)
	}
	if u := usageFrom(answer(nil, errors.New("not logged in"))); u.Unavailable != provider.UsageFailed {
		t.Fatalf("refused: %+v", u)
	}
	if k := windowKey(60); k != "window_60m" || windowMinutes(k) != 60 {
		t.Fatalf("another window: %s", k)
	}
}

// Its --version failing is no verdict (工单 #176): found, its version unknown,
// signed in as `codex login status` says.
func TestVersionProbeIsNoVerdict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for codex")
	}
	bin := filepath.Join(t.TempDir(), "codex")
	os.WriteFile(bin, []byte("#!/bin/sh\ncase \"$1\" in --version) echo 'error: unexpected argument' >&2; exit 2;; login) echo 'Logged in using ChatGPT';; esac\n"), 0o755)
	info := (&Provider{Binary: bin}).Discover(context.Background())
	if !info.Available || info.Version != "" || info.Problem != provider.ProblemNone || !info.LoggedIn {
		t.Fatalf("version unknown, found: %+v", info)
	}
}

// TestDiscoverAsksRateLimits: a check on purpose that may ask asks a
// signed-in Codex its limits on the app-server that lists its models; one
// that may not ask does not.
func TestDiscoverAsksRateLimits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for codex")
	}
	dir := t.TempDir()
	asked := filepath.Join(dir, "asked")
	bin := filepath.Join(dir, "codex")
	script := `#!/bin/sh
case "$1" in --version) echo "codex-cli 0.171.0"; exit;; login) echo "Logged in using ChatGPT"; exit;; esac
while read req; do
  id=$(echo "$req" | sed 's/.*"id":\([0-9]*\).*/\1/')
  case "$req" in
  *'"model/list"'*) echo '{"id":'$id',"result":{"data":[{"id":"gpt-fake","displayName":"Fake","isDefault":true}]}}';;
  *'"account/rateLimits/read"'*) echo x >> ` + asked + `; echo '{"id":'$id',"result":{"rateLimits":{"primary":{"usedPercent":61,"windowDurationMins":10080,"resetsAt":1795678770},"secondary":null,"planType":"pro"}}}';;
  *) echo '{"id":'$id',"result":{}}';;
  esac
done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	info := (&Provider{Binary: bin}).Discover(provider.OnPurpose(context.Background()))
	if len(info.Models) != 1 || info.PlanUsage != nil {
		t.Fatalf("not to ask: %+v %+v", info.Models, info.PlanUsage)
	}
	if _, err := os.Stat(asked); err == nil {
		t.Fatal("asked its limits without AskUsage")
	}
	info = (&Provider{Binary: bin}).Discover(provider.AskUsage(provider.OnPurpose(context.Background())))
	if u := info.PlanUsage; len(info.Models) != 1 || u == nil || u.Plan != "pro" || len(u.Windows) != 1 || u.Windows[0].Utilization != 61 {
		t.Fatalf("asked: %+v %+v", info.Models, u)
	}
}
