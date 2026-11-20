package e2e

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestHerMemory: Her keeps what the owner tells her to remember
// (docs/her-memory.md), on an environment that is not this Mac, reached as
// the apps reach it. She notes a line without asking the owner (she runs
// actions without asking); the note is a file on her Her Server; and after
// the owner clears her conversation, as Her's settings do, the new
// conversation starts with what she kept. Her's settings read the memory and clear it.
func TestHerMemory(t *testing.T) {
	env := newHerEnvironment(t)
	const line = "- Replies are always in Chinese. (2026-11-11)"
	const recall = "[[mock:recall]] What do you remember about me?"

	askHer(t, env, recall, "什么都不记得。")
	if ran, _ := askHer(t, env, "[[mock:remember:"+line+"]] From now on, reply in Chinese.", "记下了。"); !slices.Contains(ran, "write") {
		t.Fatalf("she noted nothing: ran %v", ran)
	}
	file := filepath.Join(env.home, "bots", env.bot.ID, "memory", "MEMORY.md")
	if kept, err := env.onMachine("cat", file); err != nil || strings.TrimSpace(string(kept)) != line {
		t.Fatalf("%s on the machine: %q, %v", file, kept, err)
	}

	// The same call the apps make from Her's settings.
	herCall(t, env, "", "bots/clear", map[string]string{"bot_id": env.bot.ID})
	if _, said := askHer(t, env, recall, "记得："); said != "记得："+line {
		t.Fatalf("after clearing she said %q", said)
	}

	// Her's settings show the memory as written, and clear it; she forgets
	// at once, in the same conversation.
	var kept struct {
		Text string `json:"text"`
		Path string `json:"path"`
	}
	json.Unmarshal(herCall(t, env, "", "bots/memory", map[string]string{"bot_id": env.bot.ID}), &kept)
	if strings.TrimSpace(kept.Text) != line || filepath.Base(kept.Path) != "MEMORY.md" {
		t.Fatalf("bots/memory: %+v", kept)
	}
	herCall(t, env, "", "bots/memory_clear", map[string]string{"bot_id": env.bot.ID})
	if _, err := env.onMachine("test", "-e", file); err == nil {
		t.Fatalf("%s is still there", file)
	}
	askHer(t, env, recall, "什么都不记得。")
}

// TestHerKnowledge: knowledge entries (docs/her-memory.md §10) as the apps
// reach them, through aided, the relay and the tunnel. Her proposes two
// entries; neither is recalled before the owner answers. The owner saves one
// and declines the other: only the saved one is recalled, and only when the
// owner's words hit its trigger. The owner changes it, then deletes it.
func TestHerKnowledge(t *testing.T) {
	env := newHerEnvironment(t)
	const weekly = "周报周五下午发到团队群。"
	askHer(t, env, "[[mock:propose:weekly-report|周报; weekly report|"+weekly+"]] 周报以后发团队群", `提议了：Proposed "weekly-report"`)
	askHer(t, env, "[[mock:propose:coffee|咖啡|主人不喝咖啡。]] 我不喝咖啡", `提议了：Proposed "coffee"`)
	askHer(t, env, "[[mock:recalled]] 周报和咖啡", "没有想起什么。")

	type entry struct{ Name, Trigger, Text string }
	var kept struct {
		Entries   []entry `json:"entries"`
		Proposals []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"proposals"`
	}
	read := func() {
		kept.Entries, kept.Proposals = nil, nil
		json.Unmarshal(herCall(t, env, "", "bots/memory", map[string]string{"bot_id": env.bot.ID}), &kept)
	}
	read()
	if len(kept.Proposals) != 2 || kept.Proposals[0].Name != "weekly-report" || kept.Proposals[1].Name != "coffee" || len(kept.Entries) != 0 {
		t.Fatalf("bots/memory before the owner answered: %+v", kept)
	}
	herCall(t, env, "", "bots/memory_proposal_answer", map[string]any{"bot_id": env.bot.ID, "proposal_id": kept.Proposals[0].ID, "accept": true})
	herCall(t, env, "", "bots/memory_proposal_answer", map[string]any{"bot_id": env.bot.ID, "proposal_id": kept.Proposals[1].ID, "accept": false})
	read()
	if len(kept.Proposals) != 0 || !slices.Equal(kept.Entries, []entry{{"weekly-report", "周报; weekly report", weekly}}) {
		t.Fatalf("bots/memory after the owner answered: %+v", kept)
	}
	askHer(t, env, "[[mock:recalled]] 今天天气怎样", "没有想起什么。")
	if _, said := askHer(t, env, "[[mock:recalled]] 这周的 Weekly Report，还有咖啡", "想起："); said != "想起：weekly-report="+weekly {
		t.Fatalf("with its trigger hit she said %q", said)
	}

	herCall(t, env, "", "bots/memory_entry_set", map[string]string{"bot_id": env.bot.ID, "name": "weekly-report", "trigger": "周报", "text": "周报改成周四发。"})
	askHer(t, env, "[[mock:recalled]] 周报呢", "想起：weekly-report=周报改成周四发。")
	herCall(t, env, "", "bots/memory_entry_delete", map[string]string{"bot_id": env.bot.ID, "name": "weekly-report"})
	askHer(t, env, "[[mock:recalled]] 周报呢", "没有想起什么。")
	read()
	if len(kept.Entries) != 0 {
		t.Fatalf("bots/memory after deleting: %+v", kept)
	}
}
