package acp

import (
	"encoding/json"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// A DSH process's first turn, failed for a key DSH had not loaded yet
// (工单 #172). DSH keeps the keys its owner types in in its credentials
// store, which it loads as its process starts, alongside answering ACP: a
// session it answers before the store has loaded lists no models of a
// provider whose key is there (工单 #129 返修, dshmodels.go), and a turn
// it runs then fails "no API key for provider route …" — on an owner's machine, a
// resumed session's first turn did. One that failed so is sent again once,
// only when DSH now says it has that key: the provider of the model the
// session runs offers more than that model (with no key, DSH lists that
// model alone; verified on dsh 0.2.2-rc.5). A key that is missing indeed
// fails as before, at once. DSH's log keeps the failed turn: the owner's
// message shows twice there.

// keyNotLoaded is the first turn's request when f, its failure, is a
// missing key, none has been sent again yet and the owner has not stopped
// the turn; nil otherwise.
func (s *session) keyNotLoaded(f *provider.Failure) map[string]any {
	if !s.dsh || f == nil || f.Kind != sessionv1.FailureKind_FAILURE_KIND_MISSING_CREDENTIALS {
		return nil
	}
	s.stopMu.Lock()
	stopped := s.interrupted
	s.stopMu.Unlock()
	if stopped {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.firstPrompt
	if s.sent != 1 || p == nil {
		return nil
	}
	s.firstPrompt = nil
	return p
}

// retryForKey sends the first turn's request again if DSH now has the key
// its model needs; else it ends the turn as it failed (failed), or as
// interrupted when the owner stopped it meanwhile.
func (s *session) retryForKey(params map[string]any, failed provider.Event) {
	loaded := s.keyLoaded()
	s.stopMu.Lock()
	stopped := s.interrupted
	sent := loaded && !stopped && s.request("session/prompt", params, s.turnDone) == nil
	s.stopMu.Unlock()
	if sent {
		return
	}
	if stopped {
		failed.Text, failed.Failure = "interrupted", nil
	}
	s.prompting.Store(false)
	s.emit(failed)
}

// keyLoaded: the provider of the model the session runs offers models
// besides it, as DSH says now (its model set to itself: the answer is
// every option as it now is).
func (s *session) keyLoaded() bool {
	s.cfgMu.Lock()
	var configID, value string
	if s.cfg != nil {
		if m := s.cfg.option("model"); m != nil {
			configID, value = m.ID, m.current()
		}
	}
	s.cfgMu.Unlock()
	provider := routeOf(value)
	if provider == "" {
		return false
	}
	res, err := s.call(s.ctx, "session/set_config_option", map[string]any{"sessionId": s.sessionID, "configId": configID, "value": value})
	var now sessionConfig
	if err != nil || json.Unmarshal(res, &now) != nil {
		return false
	}
	n := 0
	for _, m := range now.modelOptions() {
		for _, x := range m.choices() {
			if routeOf(x.Value) == provider {
				n++
			}
		}
	}
	return n > 1
}

// routeOf is the provider route of a DSH model value (["provider","model"]).
func routeOf(value string) string {
	var v []string
	if json.Unmarshal([]byte(value), &v) != nil || len(v) != 2 {
		return ""
	}
	return v[0]
}
