package sessionstate

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// cases is testdata/cases.json: the table every end is checked against
// (packages/core states.test.ts and the Android app's StatesTest read it too).
type cases struct {
	Limits struct {
		Lost  int `json:"lost_after_seconds"`
		Stall int `json:"stall_after_seconds"`
	} `json:"limits"`
	Cases []struct {
		Name    string `json:"name"`
		Session struct {
			Status  string `json:"status"`
			Failure string `json:"failure"`
			Last    *int   `json:"last_event_ago"`
			Updated *int   `json:"updated_ago"`
		} `json:"session"`
		Machine struct {
			Known, Local, Syncing bool
			Unreachable           string
			Heard                 *int `json:"heard_ago"`
			Since                 *int `json:"unreachable_since_ago"`
		} `json:"machine"`
		Want struct {
			State  string `json:"state"`
			Link   string `json:"link"`
			Quiet  int    `json:"quiet_seconds"`
			Silent int    `json:"silent_seconds"`
		} `json:"want"`
	} `json:"cases"`
}

func TestCases(t *testing.T) {
	b, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cs cases
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	ago := func(s *int) time.Time {
		if s == nil {
			return time.Time{}
		}
		return now.Add(-time.Duration(*s) * time.Second)
	}
	ts := func(s *int) *timestamppb.Timestamp {
		if s == nil {
			return nil
		}
		return timestamppb.New(ago(s))
	}
	l := Limits{LostAfter: time.Duration(cs.Limits.Lost) * time.Second, StallAfter: time.Duration(cs.Limits.Stall) * time.Second}
	for _, c := range cs.Cases {
		s := &sessionv1.Session{
			Status:    sessionv1.SessionStatus(sessionv1.SessionStatus_value["SESSION_STATUS_"+c.Session.Status]),
			UpdatedAt: ts(c.Session.Updated),
			Progress:  &sessionv1.SessionProgress{LastEventAt: ts(c.Session.Last)},
		}
		if c.Session.Failure != "" {
			s.Progress.LastFailure = &sessionv1.Failure{Kind: sessionv1.FailureKind(sessionv1.FailureKind_value["FAILURE_KIND_"+c.Session.Failure])}
		}
		m := Machine{Known: c.Machine.Known, Local: c.Machine.Local, Syncing: c.Machine.Syncing, Unreachable: c.Machine.Unreachable,
			HeardAt: ago(c.Machine.Heard), UnreachableSince: ago(c.Machine.Since)}
		v := Of(s, m, l, now)
		if got := StateName(v.State); got != c.Want.State {
			t.Errorf("%s: state %s, want %s", c.Name, got, c.Want.State)
		}
		if got := LinkName(v.Link); got != c.Want.Link {
			t.Errorf("%s: link %s, want %s", c.Name, got, c.Want.Link)
		}
		if got := int(v.Quiet / time.Second); got != c.Want.Quiet {
			t.Errorf("%s: quiet %d, want %d", c.Name, got, c.Want.Quiet)
		}
		if got := int(v.Silent / time.Second); got != c.Want.Silent {
			t.Errorf("%s: silent %d, want %d", c.Name, got, c.Want.Silent)
		}
	}
}

func TestText(t *testing.T) {
	now := time.Now()
	run := &sessionv1.Session{Status: sessionv1.SessionStatus_SESSION_STATUS_RUNNING, Progress: &sessionv1.SessionProgress{LastEventAt: timestamppb.New(now.Add(-12 * time.Minute))}}
	for _, c := range []struct {
		m    Machine
		want string
	}{
		{Machine{Known: true, Local: true}, "running · quiet 12m"},
		{Machine{Known: true, Unreachable: "offline", HeardAt: now.Add(-7 * time.Minute)}, "lost · last known running · 7m ago"},
		{Machine{Known: true, Unreachable: "offline", HeardAt: now.Add(-10 * time.Second)}, "running? · machine not answering"},
		{Machine{Known: true, Syncing: true}, "running · catching up"},
	} {
		if got := Of(run, c.m, Default(), now).Text(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.m, got, c.want)
		}
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("AIDE_LOST_AFTER", "90")
	t.Setenv("AIDE_STALL_AFTER", "10m")
	if l := FromEnv(); l.LostAfter != 90*time.Second || l.StallAfter != 10*time.Minute {
		t.Fatalf("%+v", l)
	}
	t.Setenv("AIDE_LOST_AFTER", "nonsense")
	t.Setenv("AIDE_STALL_AFTER", "-5")
	if l := FromEnv(); l != Default() {
		t.Fatalf("bad values keep the defaults: %+v", l)
	}
}
