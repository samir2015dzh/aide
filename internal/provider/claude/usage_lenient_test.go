package claude

import (
	"context"
	"testing"
	"time"
)

// The endpoint's answer as it came once it grew keys that are no windows
// (synthetic: the shape seen on a real account in 2026-11, figures made
// up). Decoding every key as a window failed the whole reading, and the
// app showed the usage as unreadable.
const usageBodyGrown = `{
  "five_hour":  {"utilization": 16.0, "resets_at": "2026-11-21T11:39:59.735515+00:00"},
  "seven_day":  {"utilization": 75.0, "resets_at": "2026-11-26T06:59:59.735549+00:00"},
  "seven_day_opus": null,
  "seven_day_oauth_apps": {"utilization": 0.0, "resets_at": null},
  "limits": [{"kind": "five_hour", "value": 1}],
  "weekly_scoped_shares": [],
  "member_dashboard_available": false,
  "extra_usage": {"is_enabled": false, "monthly_limit": null},
  "future_window": {"utilization": 3.5, "resets_at": "not a time"}
}`

func TestUsageReader_LenientToNewKeys(t *testing.T) {
	access := "sk-ant-oat-CLI-lenient0123456"
	home := credentialsLayout(t, access, "refresh-cli", time.Now().Add(24*time.Hour))
	srv := usageServer(t, access, usageBodyGrown)
	defer srv.Close()
	r := &UsageReader{Home: home, AppData: t.TempDir(), LocalAppData: t.TempDir(), UsageURL: srv.URL, keyFn: testKeyFn(testKey)}
	u := r.Read(context.Background())
	if u.Unavailable != "" {
		t.Fatalf("unavailable = %q, want a reading", u.Unavailable)
	}
	// The windows, five_hour and seven_day first; the arrays, the bool, the
	// object with no utilization and the null are no windows.
	var keys []string
	for _, w := range u.Windows {
		keys = append(keys, w.Key)
	}
	want := []string{"five_hour", "seven_day", "future_window", "seven_day_oauth_apps"}
	if len(keys) != len(want) {
		t.Fatalf("windows %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("windows %v, want %v", keys, want)
		}
	}
	if u.Windows[0].Utilization != 16 || u.Windows[1].Utilization != 75 {
		t.Fatalf("figures %+v", u.Windows[:2])
	}
	if u.Windows[0].ResetsAt.IsZero() || u.Windows[0].ResetsAt.UTC().Hour() != 11 {
		t.Fatalf("five_hour resets at %v", u.Windows[0].ResetsAt)
	}
	// A reset time that is null or no time: the window stays, its time unknown.
	for _, w := range u.Windows[2:] {
		if !w.ResetsAt.IsZero() {
			t.Fatalf("%s resets at %v, want unknown", w.Key, w.ResetsAt)
		}
	}
}

// An answer that is no JSON object is told apart from the endpoint failing:
// parse_error, not api_error, so the CLI and the log say which.
func TestUsageReader_ParseError(t *testing.T) {
	for _, body := range []string{`not json`, `[]`, `null`, `"five_hour"`} {
		access := "sk-ant-oat-CLI-parse01234567"
		home := credentialsLayout(t, access, "refresh-cli", time.Now().Add(24*time.Hour))
		srv := usageServer(t, access, body)
		r := &UsageReader{Home: home, AppData: t.TempDir(), LocalAppData: t.TempDir(), UsageURL: srv.URL, keyFn: testKeyFn(testKey)}
		u := r.Read(context.Background())
		srv.Close()
		if u.Unavailable != "parse_error" {
			t.Fatalf("body %s: unavailable = %q, want parse_error", body, u.Unavailable)
		}
	}
	// An object with no window at all is still an answer, read as none.
	if w, err := windowsFromBody([]byte(`{"limits": [], "member_dashboard_available": true}`)); err != nil || len(w) != 0 {
		t.Fatalf("no windows: %v %v", w, err)
	}
}
