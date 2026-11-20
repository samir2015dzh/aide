package e2e

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestHerPatrol: a routine in patrol mode (docs/her-patrol.md) looks for
// problems and does not fix them. The owner asks Her for one; the switch
// the desktop's routine page shows turns it off and on; a run tries every
// way to act — the shell, a file outside its notes, a file through
// call_tool, a connector's tool that says it only reads, directly and
// through call_tool — and each is refused in the code, while reading and
// writing its own notes work. What it was refused comes back in its report,
// as suggestions for the owner, and nothing on the machine changed.
func TestHerPatrol(t *testing.T) {
	env := newHerEnvironment(t)
	mcpServer := env.mcpServerThere(t)
	page := openConsolePage(t, env.console)
	if code, text := httpDo(t, http.MethodPost, env.console+"command-connector",
		`{"name":"notes","command":"`+mcpServer+`","args":["--name","notes"]}`); code != http.StatusOK {
		t.Fatalf("command connector: %d %s", code, text)
	}
	var checked mcpChecked
	if page.call("mcp/check", map[string]string{"name": "notes"}, &checked); checked.Server.Status.State != "ok" || !checked.has("echo") {
		t.Fatalf("the connector: %+v", checked)
	}

	_, said := askHer(t, env, "[[mock:patrol:巡查|in 1h|patrol_run:notes]] 每小时帮我查一下，但什么都别动。", `Saved routine "巡查"`)
	m := regexp.MustCompile(`\(id (r_\w+)\)`).FindStringSubmatch(said)
	if m == nil {
		t.Fatalf("no routine id in %q", said)
	}
	routine := m[1]
	ask := map[string]any{"bot_id": env.bot.ID, "routine_id": routine}
	patrol := func() bool {
		var r struct {
			Routine struct {
				Summary struct {
					Patrol bool `json:"patrol"`
				} `json:"summary"`
			} `json:"routine"`
		}
		json.Unmarshal(herCall(t, env, "", "bots/routine", ask), &r)
		return r.Routine.Summary.Patrol
	}
	if !patrol() {
		t.Fatal("made as a patrol, but bots/routine does not say so")
	}
	// The desktop's switch, off and on again.
	set := func(on bool) {
		herCall(t, env, "", "bots/routine_set_patrol", map[string]any{"bot_id": env.bot.ID, "routine_id": routine, "patrol": on})
	}
	if set(false); patrol() {
		t.Fatal("still a patrol after the switch went off")
	}
	if set(true); !patrol() {
		t.Fatal("not a patrol after the switch went on")
	}

	herCall(t, env, "", "bots/routine_run_now", ask)
	var run struct {
		ID      string `json:"id"`
		Outcome string `json:"outcome"`
		Report  string `json:"report"`
	}
	eventually(t, 90*time.Second, "the patrol's run to end", func() bool {
		var runs struct {
			Runs []json.RawMessage `json:"runs"`
		}
		json.Unmarshal(herCall(t, env, "", "bots/routine_runs", ask), &runs)
		if len(runs.Runs) == 0 {
			return false
		}
		json.Unmarshal(runs.Runs[0], &run)
		return run.Outcome != ""
	})
	if run.Outcome != "reported" || !strings.HasPrefix(run.Report, "巡查完毕。\n\nNot done, as this routine only looks.") {
		t.Fatalf("the run: %+v", run)
	}
	for _, refused := range []string{"\n- touch patrol-bash.txt", "\n- write patrol-write.txt", "\n- call write", "\n- notes: Echo"} {
		if !strings.Contains(run.Report, refused) {
			t.Errorf("the report does not suggest %q: %s", refused, run.Report)
		}
	}

	// Its steps, as the routine page shows them: what was refused, and why.
	var steps struct {
		Transcript []struct {
			Type   string `json:"type"`
			Result struct {
				ToolName string `json:"tool_name"`
				IsError  bool   `json:"is_error"`
				Content  []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		} `json:"transcript"`
	}
	json.Unmarshal(herCall(t, env, "", "bots/routine_run", map[string]any{"bot_id": env.bot.ID, "routine_id": routine, "run_id": run.ID}), &steps)
	var results []string
	for _, item := range steps.Transcript {
		if item.Type != "tool_result" {
			continue
		}
		text := ""
		if len(item.Result.Content) > 0 {
			text = item.Result.Content[0].Text
		}
		how := "ran"
		switch {
		case strings.HasPrefix(text, "Tool call denied: This routine is a patrol: it only looks"):
			how = "refused"
		// A run reaches connectors through call_tool only: by its own name,
		// a connector's tool is not even there.
		case strings.HasPrefix(text, "Unknown tool") && item.Result.ToolName == "mcp__notes__echo":
			how = "refused"
		case item.Result.IsError:
			t.Errorf("%s failed otherwise: %s", item.Result.ToolName, text)
		}
		results = append(results, how+" "+item.Result.ToolName)
	}
	want := []string{"ran read", "refused bash", "refused write", "refused call_tool", "ran tool_search", "refused mcp__notes__echo", "refused call_tool", "ran write"}
	if strings.Join(results, ", ") != strings.Join(want, ", ") {
		t.Fatalf("the run's calls: %v, want %v", results, want)
	}

	// Nothing changed in the bot's folder; the note is where the brief said.
	files := filepath.Join(env.home, "bots", env.bot.ID, "files")
	for _, name := range []string{"patrol-bash.txt", "patrol-write.txt", "patrol-call.txt"} {
		if _, err := env.onMachine("test", "-e", filepath.Join(files, name)); err == nil {
			t.Errorf("%s was made", name)
		}
	}
	note := filepath.Join(env.home, "bots", env.bot.ID, "patrol", routine, "notes.md")
	if b, err := env.onMachine("cat", note); err != nil || !strings.Contains(string(b), "建议") {
		t.Fatalf("%s: %q, %v", note, b, err)
	}

	// Her passes the suggestions on to the owner.
	eventually(t, 60*time.Second, "the suggestions as Her's latest message", func() bool {
		l := listedMachine(t, env.c, env.machine).GetHer().GetLatest()
		return l.GetBotId() == env.bot.ID && strings.Contains(l.GetText(), "Not done, as this routine only looks")
	})
}
