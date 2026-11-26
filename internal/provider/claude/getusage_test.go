package claude

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// TestGetUsage (工单 #143, after upstream 1c862bde): asked get_usage at its
// check (Claude Code 2.1.330, recorded), Claude Code says the plan and its
// usage rows as /usage's twin has them, so the windows read as 工单 #86's
// other sources read them: the session five_hour, the week seven_day, a
// model's own week seven_day_<model>. An API key's answer has no window;
// an error answer is no reading either; another line is no answer.
func TestGetUsage(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "claude-2.1.330-get-usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, ok := usageFrom(b)
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339Nano, s); return v }
	want := []Window{
		{Key: "five_hour", Utilization: 39, ResetsAt: at("2026-11-24T20:40:00.035468+00:00")},
		{Key: "seven_day", Utilization: 64, ResetsAt: at("2026-11-29T08:00:00.035487+00:00")},
		{Key: "seven_day_fable", Utilization: 75, ResetsAt: at("2026-11-29T08:00:00.035630+00:00")},
	}
	if !ok || u.Plan != "max" || u.Unavailable != "" || !reflect.DeepEqual(u.Windows, want) {
		t.Fatalf("got %+v", u)
	}
	// Without the rows (an older answer): the endpoint's windows.
	u, ok = usageFrom([]byte(`{"type":"control_response","response":{"subtype":"success","request_id":"aide-usage","response":{"subscription_type":"pro","rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":12,"resets_at":"2026-11-24T20:40:00Z"},"seven_day":null,"extra_usage":{"utilization":null}}}}}`))
	if !ok || u.Plan != "pro" || len(u.Windows) != 1 || u.Windows[0].Key != "five_hour" || u.Windows[0].Utilization != 12 {
		t.Fatalf("endpoint's windows: %+v", u)
	}
	u, ok = usageFrom([]byte(`{"type":"control_response","response":{"subtype":"success","request_id":"aide-usage","response":{"subscription_type":null,"rate_limits_available":false,"rate_limits":null}}}`))
	if !ok || u.Unavailable != provider.UsageNone || len(u.Windows) != 0 {
		t.Fatalf("an API key: %+v", u)
	}
	u, ok = usageFrom([]byte(`{"type":"control_response","response":{"subtype":"error","request_id":"aide-usage","error":"unknown subtype"}}`))
	if !ok || u.Unavailable != provider.UsageFailed || !strings.Contains(u.Detail, "unknown subtype") {
		t.Fatalf("refused: %+v", u)
	}
	if _, ok := usageFrom([]byte(`{"type":"control_response","response":{"subtype":"success","request_id":"aide-models"}}`)); ok {
		t.Fatal("the models' answer taken for get_usage's")
	}
}

// TestDiscoverAsksUsage: a check on purpose that may ask (provider.AskUsage)
// asks a signed-in Claude Code get_usage on the process that answers its
// models, after them; a passive check, or one that may not ask, does not.
func TestDiscoverAsksUsage(t *testing.T) {
	scripted(t)
	dir := t.TempDir()
	asked := filepath.Join(dir, "asked")
	bin := filepath.Join(dir, "claude")
	models, _ := filepath.Abs(filepath.Join("testdata", "claude-2.1.330-initialize.jsonl"))
	usage, _ := filepath.Abs(filepath.Join("testdata", "claude-2.1.330-get-usage.json"))
	script := `#!/bin/sh
case "$1" in --version) echo "2.1.330 (Claude Code)"; exit;; auth) echo '{"loggedIn":true}'; exit;; esac
while read req; do
  case "$req" in
  *'"subtype":"initialize"'*) cat ` + models + `;;
  *'"subtype":"get_usage"'*) echo get_usage >> ` + asked + `; cat ` + usage + `; echo;;
  esac
done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	info := (&Provider{Binary: bin}).Discover(provider.OnPurpose(context.Background()))
	if info.PlanUsage != nil || len(info.Models) != 4 {
		t.Fatalf("not to ask: %+v %+v", info.PlanUsage, info.Models)
	}
	if _, err := os.Stat(asked); err == nil {
		t.Fatal("asked get_usage without AskUsage")
	}
	info = (&Provider{Binary: bin}).Discover(provider.AskUsage(context.Background()))
	if info.PlanUsage != nil {
		t.Fatalf("passive: %+v", info.PlanUsage)
	}
	start := time.Now()
	info = (&Provider{Binary: bin}).Discover(provider.AskUsage(provider.OnPurpose(context.Background())))
	if len(info.Models) != 4 || info.Own.Model != "claude-sonnet-5-5" {
		t.Fatalf("models: %+v", info.Models)
	}
	if u := info.PlanUsage; u == nil || u.Plan != "max" || len(u.Windows) != 3 || u.Windows[0].Utilization != 39 || u.At.Before(start) {
		t.Fatalf("usage: %+v", u)
	}
	// One that does not answer get_usage (an older version): its models
	// stand, the usage says it failed.
	os.WriteFile(bin, []byte(`#!/bin/sh
case "$1" in --version) echo "2.1.300 (Claude Code)"; exit;; auth) echo '{"loggedIn":true}'; exit;; esac
read req
cat `+models+`
`), 0o755)
	info = (&Provider{Binary: bin}).Discover(provider.AskUsage(provider.OnPurpose(context.Background())))
	if len(info.Models) != 4 || info.PlanUsage == nil || info.PlanUsage.Unavailable != provider.UsageFailed {
		t.Fatalf("an older one: %+v %+v", info.Models, info.PlanUsage)
	}
}

// TestGetUsageNotWaitedOn: one that answers its models but not get_usage,
// and keeps reading its input, is not waited on past usageWait: its models
// stand and the check goes on.
func TestGetUsageNotWaitedOn(t *testing.T) {
	scripted(t)
	defer func(d time.Duration) { usageWait = d }(usageWait)
	usageWait = 300 * time.Millisecond
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	models, _ := filepath.Abs(filepath.Join("testdata", "claude-2.1.330-initialize.jsonl"))
	os.WriteFile(bin, []byte(`#!/bin/sh
while read req; do
  case "$req" in *'"subtype":"initialize"'*) cat `+models+`;; esac
done
`), 0o755)
	start := time.Now()
	ms, _, u := listModels(context.Background(), os.Environ(), bin, true)
	if len(ms) != 4 || u == nil || u.Unavailable != provider.UsageFailed {
		t.Fatalf("got %+v %+v", ms, u)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
}
