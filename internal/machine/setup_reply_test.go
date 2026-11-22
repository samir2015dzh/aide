package machine

import "testing"

// A test turn whose answer says the agent is not signed in did not pass (工单 #44).
func TestNotSignedInReply(t *testing.T) {
	for _, r := range []string{"Not logged in · Please run /login", "Invalid API key · Please run /login"} {
		if !notSignedInReply.MatchString(r) {
			t.Errorf("%q passed as an answer", r)
		}
	}
	if notSignedInReply.MatchString("Hello from the test folder.") {
		t.Error("a real answer failed")
	}
}
