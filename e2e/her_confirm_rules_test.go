package e2e

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestHerConfirmRules: the owner's confirmation rules (docs/her-confirm-rules.md),
// on an environment that is not this Mac, reached as the apps reach it. Her
// asks first before a send; once the owner says not to ask about that kind
// again, the rule is kept and she goes ahead; Her's settings list it and
// delete it, and she asks again. A rule cannot take the question away from
// the hard floor (deleting data here), and where rules disagree she asks.
func TestHerConfirmRules(t *testing.T) {
	env := newHerEnvironment(t)
	const send = "[[mock:act:send|团队群]] 把周报发到团队群。"
	asks := func(text, why string) {
		t.Helper()
		if _, said := askHer(t, env, text, "先问一下："); !strings.Contains(said, why) {
			t.Fatalf("she asked, but said %q; want %q", said, why)
		}
	}

	asks(send, "no rule covers it")

	// The owner's preference, from the chat.
	if _, said := askHer(t, env, "[[mock:prefer:send|团队群|skip]] 以后发团队群不用问我。", "规则："); !strings.Contains(said, "Kept rule c_") {
		t.Fatalf("no rule kept: %q", said)
	}
	askHer(t, env, send, "直接办了：团队群")
	// Only what it covers.
	asks("[[mock:act:send|老板的邮箱]] 把周报发给老板。", "no rule covers it")
	file := filepath.Join(env.home, "bots", env.bot.ID, "confirm-rules.json")
	if kept, err := env.onMachine("cat", file); err != nil || !strings.Contains(string(kept), "团队群") {
		t.Fatalf("%s on the machine: %q, %v", file, kept, err)
	}

	// The hard floor: no rule is kept for it, and she asks whatever the file says.
	if _, said := askHer(t, env, "[[mock:prefer:delete|*|skip]] 以后删东西不用问我。", "规则："); !strings.Contains(said, "hard floor") {
		t.Fatalf("a rule loosened the hard floor: %q", said)
	}
	asks("[[mock:act:delete|旧照片]] 把旧照片删了。", "hard floor")

	// Rules that disagree: asking wins.
	askHer(t, env, "[[mock:prefer:send|团队*|ask]] 发到团队的都先问我。", "规则：")
	asks(send, "asking wins")

	// Her's settings list the rules and delete them, as the apps do.
	type rule struct {
		ID     string `json:"id"`
		Action string `json:"action"`
		Target string `json:"target"`
		Want   string `json:"want"`
	}
	var listed struct {
		Rules []rule `json:"rules"`
	}
	json.Unmarshal(herCall(t, env, "", "bots/confirm_rules", map[string]string{"bot_id": env.bot.ID}), &listed)
	if len(listed.Rules) != 2 || listed.Rules[0].Target != "团队群" || listed.Rules[0].Want != "skip" || listed.Rules[1].Want != "ask" {
		t.Fatalf("bots/confirm_rules: %+v", listed.Rules)
	}
	// Without the rule that says ask, the one that skips holds again.
	herCall(t, env, "", "bots/confirm_rule_delete", map[string]string{"bot_id": env.bot.ID, "rule_id": listed.Rules[1].ID})
	askHer(t, env, send, "直接办了：团队群")
	// Without any, she asks first again.
	herCall(t, env, "", "bots/confirm_rule_delete", map[string]string{"bot_id": env.bot.ID, "rule_id": listed.Rules[0].ID})
	asks(send, "no rule covers it")
}
