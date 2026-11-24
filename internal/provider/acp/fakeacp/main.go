// Command fakeacp is a scripted coding agent that speaks the Agent Client
// Protocol on stdio, for tests of the ACP adapter. Each prompt line is a
// command:
//
//	say <text>          agent message (streamed in chunks); \n in text is a newline
//	stream <text>       agent message written slowly: a few characters every
//	                    50 ms, as a reply grows (\n as in say)
//	write <path> <text> edit tool call, asks permission, then writes the file
//	shell <command>     execute tool call, asks permission, then runs it
//	read <path>         read tool call without a permission request
//	sleep <duration>    pause (session/cancel ends the turn as cancelled)
//	tool-sleep <dur>    execute tool call "sleep <dur>" that pauses in this
//	                    process, no program run (a long command on any OS);
//	                    session/cancel ends it and the turn as cancelled
//	history             agent message "history: <n> turns"
//	config              agent message "config: <model> <effort> <mode>"
//	                    (what the session runs with)
//	crash               exit in the middle of the turn
//	fail <text>         answer the prompt with a JSON-RPC error
//	fail-code <n> <t>   same, with error code n
//	images              agent message "images: <media types>" (the image
//	                    blocks of the prompt)
//	files               agent message "files: <block>; …", each non-text
//	                    block of the prompt: "image|audio <type> <sha>" or
//	                    "link <name> <type> <sha>" (of the linked file it
//	                    read; sha: the first 12 hex digits of its SHA-256)
//	env <name>          agent message "env: <name>=<value>" (its environment)
//	Reply …             agent message "ready" (machine.TestAgent's prompt)
//	cwd                 agent message "cwd: <session cwd> <process folder>"
//	ask <title>|<a,b>   elicitation/create (a form: a choice of a or b, and
//	                    a note), then agent message "answer: <choice>
//	                    <note>" or "declined: <action>"; "no elicitation"
//	                    when the client does not take forms
//
// Sessions are kept under $FAKE_ACP_STATE (default $TMPDIR/fakeacp) so a
// new process can load them. Environment:
//
//	FAKE_ACP_RESUME=load|resume|none  how sessions resume (default load)
//	FAKE_ACP_AUTH=<id>                require `authenticate` with this method
//	FAKE_ACP_MODELS=a,b               models session/set_model accepts
//	FAKE_ACP_CONFIG=1                 offer the models as a "model" config
//	                                  option (session/set_config_option),
//	                                  in a group, the first one current,
//	                                  and drop session/set_model
//	FAKE_ACP_EFFORTS=m=a|b;n=c        with FAKE_ACP_CONFIG, a "thought_level"
//	                                  option: the reasoning efforts of the
//	                                  current model, and "" (its default)
//	FAKE_ACP_MODES=a,b                session modes, the first one current:
//	                                  a "mode" config option with
//	                                  FAKE_ACP_CONFIG, else the older
//	                                  modes (session/set_mode)
//	FAKE_ACP_ALLOW_ALWAYS_ONLY=1      offer only allow_always/reject_once
//	FAKE_ACP_IMAGES=1                 take image prompt blocks
//	FAKE_ACP_AUDIO=1                  take audio prompt blocks
//	FAKE_ACP_CLOSE=1                  take session/close: it ends the turn
//	                                  in progress (cancelled) and writes
//	                                  "<session id>" to closed.log in
//	                                  $FAKE_ACP_STATE
//	FAKE_ACP_OPTIONS=1                advertise session/options (never creates a session)
//	FAKE_ACP_OPTIONS_ERROR=<code>     fail session/options; "wait" waits for cancellation
//	FAKE_ACP_CONFIG_FILE=<path>       configOptions JSON used by new/options
//	FAKE_ACP_PROVIDERS=<json>         with FAKE_ACP_CONFIG, models as DSH
//	                                  offers them by provider: a list of
//	                                  model options, each {"id", "groups":
//	                                  [{"name", "models": [{"value",
//	                                  "name"}]}]}; the first model current
//	FAKE_ACP_LOG=<path>               append method names only, for no-prompt assertions
//	FAKE_ACP_PRESETS=<default>        DSH's permission presets (read-only,
//	                                  workspace-write, danger-full-access;
//	                                  <default> is "" or one of them): its
//	                                  --dump-config prints them, a new
//	                                  session takes the defaultPreset of a
//	                                  --patch overlay, else <default>, else
//	                                  workspace-write, as its mode (kept
//	                                  when resumed; not an ACP mode), and
//	                                  write fails in a read-only session
package main

import (
	"bufio"
	"cmp"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

type msg struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

var (
	outMu     sync.Mutex
	out       = bufio.NewWriter(os.Stdout)
	stateDir  = os.Getenv("FAKE_ACP_STATE")
	resume    = os.Getenv("FAKE_ACP_RESUME")
	authID    = os.Getenv("FAKE_ACP_AUTH")
	authed    = false
	mode      string
	nextID    = 1000
	waiting   = map[string]chan msg{}
	waitMu    sync.Mutex
	cancelled = make(chan struct{}, 1)
	cwd       string
)

func send(v any) {
	b, _ := json.Marshal(v)
	outMu.Lock()
	out.Write(append(b, '\n'))
	out.Flush()
	outMu.Unlock()
}

func reply(id json.RawMessage, result any) {
	send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func replyErr(id json.RawMessage, code int, text string) {
	send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": text}})
}

func update(sid string, u map[string]any) {
	send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": sid, "update": u}})
}

// ask sends a request to the client and waits for its response.
func ask(method string, params any) msg {
	waitMu.Lock()
	nextID++
	n := nextID
	ch := make(chan msg, 1)
	waiting[fmt.Sprint(n)] = ch
	waitMu.Unlock()
	send(map[string]any{"jsonrpc": "2.0", "id": n, "method": method, "params": params})
	return <-ch
}

type history struct {
	Turns  []string `json:"turns"`
	Preset string   `json:"preset,omitempty"`
}

// presets: DSH's permission presets (FAKE_ACP_PRESETS); preset, what this
// process's new sessions get.
var preset, presets = os.LookupEnv("FAKE_ACP_PRESETS")

// dumpConfig prints a profile's configuration as `dsh --dump-config` does.
func dumpConfig() {
	fmt.Print("# == @deepseek-ai/dsh-base\n- id: sandbox-policy\n  name: '@deepseek-ai/dsh-sandbox-policy'\n  config:\n" +
		"    mode: !!js process.env.DSH_PERMISSION_MODE ?? 'workspace-write'\n")
	if !presets {
		return
	}
	fmt.Print("- id: permission\n  name: '@deepseek-ai/dsh-permission-presets'\n  config:\n")
	if preset != "" {
		fmt.Printf("    defaultPreset: %s\n", preset)
	}
	fmt.Print("    presets:\n      read-only:\n        sandbox: read-only\n        approval: ask\n" +
		"      workspace-write:\n        sandbox: workspace-write\n        approval: ask\n" +
		"      danger-full-access:\n        sandbox: danger-full-access\n        approval: never\n")
}

// patched reads the defaultPreset of --patch overlays.
func patched(args []string) {
	for i, a := range args {
		if a != "--patch" || i+1 == len(args) {
			continue
		}
		b, err := os.ReadFile(args[i+1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "patch:", err)
			os.Exit(1)
		}
		var rows []struct {
			ID     string
			Config struct {
				DefaultPreset string `yaml:"defaultPreset"`
				Presets       map[string]any
			}
		}
		if err := yaml.Unmarshal(b, &rows); err != nil || len(rows) != 1 || rows[0].ID != "permission" || len(rows[0].Config.Presets) != 3 {
			fmt.Fprintf(os.Stderr, "patch: %v\n%s", err, b)
			os.Exit(1)
		}
		preset = rows[0].Config.DefaultPreset
	}
}

// load reads a session's history. Another process of the session may be
// writing it, and Windows lets a reader see it half written: a moment
// later it's whole.
func load(sid string) (h *history, err error) {
	for range 20 {
		var b []byte
		if b, err = os.ReadFile(filepath.Join(stateDir, sid+".json")); err == nil {
			h = &history{}
			if err = json.Unmarshal(b, h); err == nil {
				return h, nil
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, err
}

func save(sid string, h *history) {
	b, _ := json.Marshal(h)
	os.WriteFile(filepath.Join(stateDir, sid+".json"), b, 0o600)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("fakeacp 1.2.3")
		return
	}
	if slices.Contains(os.Args, "--dump-config") {
		dumpConfig()
		return
	}
	patched(os.Args)
	if stateDir == "" {
		stateDir = filepath.Join(os.TempDir(), "fakeacp")
	}
	os.MkdirAll(stateDir, 0o700)
	if resume == "" {
		resume = "load"
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 32<<20)
	for in.Scan() {
		var m msg
		if json.Unmarshal(in.Bytes(), &m) != nil {
			continue
		}
		if m.Method == "" { // response to one of our requests
			waitMu.Lock()
			ch := waiting[string(m.ID)]
			delete(waiting, string(m.ID))
			waitMu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		handle(m)
	}
}

func handle(m msg) {
	if path := os.Getenv("FAKE_ACP_LOG"); path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			fmt.Fprintln(f, m.Method)
			f.Close()
		}
	}
	var p struct {
		SessionID string `json:"sessionId"`
		Cwd       string `json:"cwd"`
		MethodID  string `json:"methodId"`
		ModelID   string `json:"modelId"`
		ConfigID  string `json:"configId"`
		Value     string `json:"value"`
		Prompt    []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			MimeType string `json:"mimeType"`
			Data     string `json:"data"`
			URI      string `json:"uri"`
			Name     string `json:"name"`
		} `json:"prompt"`
	}
	json.Unmarshal(m.Params, &p)
	needAuth := authID != "" && !authed
	switch m.Method {
	case "initialize":
		var ip struct {
			ClientCapabilities struct {
				Elicitation struct {
					Form *struct{} `json:"form"`
				} `json:"elicitation"`
			} `json:"clientCapabilities"`
		}
		json.Unmarshal(m.Params, &ip)
		canElicit = ip.ClientCapabilities.Elicitation.Form != nil
		caps := map[string]any{"loadSession": resume == "load"}
		sessionCaps := map[string]any{}
		if resume == "resume" {
			sessionCaps["resume"] = map[string]any{}
		}
		if os.Getenv("FAKE_ACP_CLOSE") == "1" {
			sessionCaps["close"] = map[string]any{}
		}
		if os.Getenv("FAKE_ACP_OPTIONS") == "1" {
			sessionCaps["options"] = map[string]any{}
		}
		caps["sessionCapabilities"] = sessionCaps
		caps["promptCapabilities"] = map[string]any{"image": os.Getenv("FAKE_ACP_IMAGES") == "1", "audio": os.Getenv("FAKE_ACP_AUDIO") == "1"}
		var auth []any
		if authID != "" {
			auth = []any{map[string]any{"id": "other", "name": "Other"}, map[string]any{"id": authID, "name": "Login"}}
		}
		reply(m.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": caps, "authMethods": auth,
			"agentInfo": map[string]any{"name": "fakeacp", "title": "Fake ACP", "version": "1.2.3"}})
	case "authenticate":
		if p.MethodID != authID {
			replyErr(m.ID, -32000, "unknown auth method "+p.MethodID)
			return
		}
		if os.Getenv("FAKE_ACP_AUTH_WAIT") == "1" {
			fmt.Fprintln(os.Stderr, "Open https://example.test/login to sign in")
			time.Sleep(30 * time.Second)
		}
		authed = true
		reply(m.ID, map[string]any{})
	case "session/set_mode":
		var params struct {
			ModeID string `json:"modeId"`
		}
		json.Unmarshal(m.Params, &params)
		if modes := os.Getenv("FAKE_ACP_MODES"); modes != "" && !slices.Contains(strings.Split(modes, ","), params.ModeID) {
			replyErr(m.ID, -32602, "unknown mode "+params.ModeID)
			return
		}
		mode = params.ModeID
		reply(m.ID, map[string]any{})
	case "session/options":
		if os.Getenv("FAKE_ACP_OPTIONS") != "1" {
			replyErr(m.ID, -32601, "not advertised")
			return
		}
		if code := os.Getenv("FAKE_ACP_OPTIONS_ERROR"); code != "" {
			if code == "wait" {
				time.Sleep(30 * time.Second)
				return
			}
			n, _ := strconv.Atoi(code)
			replyErr(m.ID, n, "sensitive server error must not be exposed")
			return
		}
		reply(m.ID, map[string]any{"configOptions": modelConfig("")})
	case "session/new":
		if needAuth {
			replyErr(m.ID, -32000, "Authentication required")
			return
		}
		cwd = p.Cwd
		// The pid too: on Windows the clock's steps are coarse enough that two
		// sessions started at once would share an id (and overwrite each other).
		sid := fmt.Sprintf("fake-%d-%d", os.Getpid(), time.Now().UnixNano())
		res := map[string]any{"sessionId": sid}
		curModel, effort = strings.Split(os.Getenv("FAKE_ACP_MODELS"), ",")[0], ""
		if ps := providers(); len(ps) > 0 {
			curModel = ps[0].Groups[0].Models[0].Value
		}
		mode = strings.Split(os.Getenv("FAKE_ACP_MODES"), ",")[0]
		if presets {
			mode = cmp.Or(preset, "workspace-write")
		}
		save(sid, &history{Preset: mode})
		configState(res)
		reply(m.ID, res)
	case "session/load", "session/resume":
		if needAuth {
			replyErr(m.ID, -32000, "Authentication required")
			return
		}
		h, err := load(p.SessionID)
		if err != nil {
			replyErr(m.ID, -32002, "Resource not found: session "+p.SessionID)
			return
		}
		cwd = p.Cwd
		if presets {
			mode = h.Preset
		}
		if m.Method == "session/load" { // replay the conversation
			for _, t := range h.Turns {
				update(p.SessionID, map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]any{"type": "text", "text": t}})
				update(p.SessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "replayed " + t}})
				update(p.SessionID, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "old", "title": "old", "kind": "edit", "status": "completed"})
			}
		}
		res := map[string]any{}
		if curModel == "" && os.Getenv("FAKE_ACP_CONFIG") == "1" { // a new process: its first model, as a new session
			curModel = strings.Split(os.Getenv("FAKE_ACP_MODELS"), ",")[0]
			if ps := providers(); len(ps) > 0 {
				curModel = ps[0].Groups[0].Models[0].Value
			}
		}
		configState(res)
		reply(m.ID, res)
	case "session/set_config_option":
		if os.Getenv("FAKE_ACP_CONFIG") == "1" {
			switch {
			case providerOffers(p.ConfigID, p.Value):
				curModel, effort = p.Value, ""
			case p.ConfigID == "llm" && slices.Contains(strings.Split(os.Getenv("FAKE_ACP_MODELS"), ","), p.Value):
				curModel, effort = p.Value, "" // the model's efforts differ
			case p.ConfigID == "reasoning_effort" && slices.Contains(efforts(), p.Value):
				effort = p.Value
			case p.ConfigID == "mode" && slices.Contains(strings.Split(os.Getenv("FAKE_ACP_MODES"), ","), p.Value):
				mode = p.Value
			default:
				replyErr(m.ID, -32602, "unknown config option or value "+p.ConfigID+"="+p.Value)
				return
			}
			res := map[string]any{}
			configState(res)
			reply(m.ID, res)
			if options := os.Getenv("FAKE_ACP_EAGER_UPDATE"); options != "" && p.ConfigID == "llm" {
				update(p.SessionID, map[string]any{"sessionUpdate": "config_option_update", "configOptions": json.RawMessage(options)})
			}
			return
		}
		replyErr(m.ID, -32602, "unknown config option "+p.ConfigID)
	case "session/set_model":
		if os.Getenv("FAKE_ACP_CONFIG") == "1" {
			replyErr(m.ID, -32601, "Method not found")
			return
		}
		for _, x := range strings.Split(os.Getenv("FAKE_ACP_MODELS"), ",") {
			if x != "" && x == p.ModelID {
				curModel = x
				reply(m.ID, map[string]any{})
				return
			}
		}
		replyErr(m.ID, -32602, "unknown model "+p.ModelID)
	case "session/prompt":
		if os.Getenv("FAKE_ACP_REQUIRE_MODE") == "1" && mode != "default" {
			replyErr(m.ID, -32602, "permission mode was not reset")
			return
		}
		select { // drop a stale cancel
		case <-cancelled:
		default:
		}
		var images, files []string
		for _, b := range p.Prompt {
			if b.Type == "image" && b.Data != "" {
				images = append(images, b.MimeType)
			}
			switch b.Type {
			case "image", "audio":
				data, _ := base64.StdEncoding.DecodeString(b.Data)
				files = append(files, b.Type+" "+b.MimeType+" "+shaOf(data))
			case "resource_link":
				files = append(files, "link "+b.Name+" "+b.MimeType+" "+linked(b.URI))
			}
		}
		go prompt(m.ID, p.SessionID, p.Prompt[0].Text, images, files)
	case "session/close":
		select {
		case cancelled <- struct{}{}:
		default:
		}
		f, err := os.OpenFile(filepath.Join(stateDir, "closed.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintln(f, p.SessionID)
			f.Close()
		}
		reply(m.ID, map[string]any{})
	case "session/cancel":
		select {
		case cancelled <- struct{}{}:
		default:
		}
	default:
		if m.ID != nil {
			replyErr(m.ID, -32601, "method not found")
		}
	}
}

var callN int

// What the session runs with (one session per process).
var curModel, effort string

// efforts are the current model's reasoning efforts (FAKE_ACP_EFFORTS).
func efforts() []string {
	for _, e := range strings.Split(os.Getenv("FAKE_ACP_EFFORTS"), ";") {
		if m, list, _ := strings.Cut(e, "="); m == curModel {
			return strings.Split(list, "|")
		}
	}
	return nil
}

// configState adds the session's configuration to a session/new, /load,
// /resume or /set_config_option result: config options with
// FAKE_ACP_CONFIG, else the older modes (FAKE_ACP_MODES).
func configState(res map[string]any) {
	modes := os.Getenv("FAKE_ACP_MODES")
	if os.Getenv("FAKE_ACP_CONFIG") != "1" {
		if modes != "" {
			var available []any
			for _, x := range strings.Split(modes, ",") {
				available = append(available, map[string]any{"id": x, "name": "Mode " + x})
			}
			res["modes"] = map[string]any{"currentModeId": mode, "availableModes": available}
		}
		return
	}
	if os.Getenv("FAKE_ACP_CONFIG_FILE") != "" {
		res["configOptions"] = modelConfig("")
		return
	}
	if ps := providers(); len(ps) > 0 { // DSH's: no modes over ACP
		var opts []any
		for _, o := range ps {
			current, groups := "", []any{}
			for _, g := range o.Groups {
				var models []any
				for _, m := range g.Models {
					models = append(models, map[string]any{"value": m.Value, "name": m.Name})
					if m.Value == curModel {
						current = curModel
					}
				}
				groups = append(groups, map[string]any{"group": strings.ToLower(g.Name), "name": g.Name, "options": models})
			}
			opts = append(opts, map[string]any{"id": o.ID, "category": "model", "type": "select", "currentValue": current, "options": groups})
		}
		res["configOptions"] = opts
		return
	}
	var models, modeOpts []any
	for _, x := range strings.Split(os.Getenv("FAKE_ACP_MODELS"), ",") {
		models = append(models, map[string]any{"value": x, "name": strings.ToUpper(x)})
	}
	for _, x := range strings.Split(cmp.Or(modes, "ask"), ",") {
		modeOpts = append(modeOpts, map[string]any{"value": x, "name": "Mode " + x})
	}
	// Mode first: the model is found by its category, not its place.
	opts := []any{
		map[string]any{"id": "mode", "category": "mode", "type": "select", "currentValue": cmp.Or(mode, "ask"), "options": modeOpts},
		map[string]any{"id": "llm", "category": "model", "type": "select", "currentValue": curModel,
			"options": []any{map[string]any{"group": "g", "name": "Models", "options": models}}},
	}
	if presets { // DSH offers no modes over ACP
		opts = opts[1:]
	}
	if e := efforts(); e != nil {
		levels := []any{map[string]any{"value": "", "name": "Provider default"}}
		for _, x := range e {
			levels = append(levels, map[string]any{"value": x, "name": "Effort " + x})
		}
		opts = append(opts, map[string]any{"id": "reasoning_effort", "category": "thought_level", "type": "select", "currentValue": effort, "options": levels})
	}
	res["configOptions"] = opts
}

// canElicit: the client takes form elicitations.
var canElicit bool

// shaOf is the first 12 hex digits of data's SHA-256.
func shaOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}

// linked is shaOf the file a file:// URI names, read as an agent would.
func linked(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return "not-a-file"
	}
	path := u.Path
	if runtime.GOOS == "windows" {
		path = filepath.FromSlash(strings.TrimPrefix(path, "/"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "unreadable"
	}
	return shaOf(data)
}

func prompt(id json.RawMessage, sid, text string, images, files []string) {
	h, err := load(sid)
	if err != nil {
		replyErr(id, -32002, "unknown session")
		return
	}
	h.Turns = append(h.Turns, text)
	save(sid, h)
	for _, line := range strings.Split(text, "\n") {
		cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch cmd {
		case "model-options":
			update(sid, map[string]any{"sessionUpdate": "config_option_update", "configOptions": json.RawMessage(arg)})
		case "say":
			r := []rune(strings.ReplaceAll(arg, `\n`, "\n")) // split between characters, as agents stream
			half := len(r) / 2
			update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": string(r[:half])}})
			update(sid, map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": "thinking"}})
			update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": string(r[half:])}})
		case "stream":
			r := []rune(strings.ReplaceAll(arg, `\n`, "\n"))
			for i := 0; i < len(r); i += 4 {
				update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": string(r[i:min(i+4, len(r))])}})
				select {
				case <-time.After(50 * time.Millisecond):
				case <-cancelled:
					reply(id, map[string]any{"stopReason": "cancelled"})
					return
				}
			}
		case "images":
			update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "images: " + strings.Join(images, ",")}})
		case "files":
			update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "files: " + strings.Join(files, "; ")}})
		case "ask":
			title, opts, _ := strings.Cut(arg, "|")
			if !canElicit {
				update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "no elicitation"}})
				break
			}
			var oneOf []any
			for _, o := range strings.Split(opts, ",") {
				oneOf = append(oneOf, map[string]any{"const": "v-" + strings.ToLower(o), "title": o})
			}
			r := ask("elicitation/create", map[string]any{"sessionId": sid, "mode": "form", "message": title, "requestedSchema": map[string]any{
				"type": "object", "required": []string{"choice"}, "properties": map[string]any{
					"choice": map[string]any{"type": "string", "title": "Choice", "oneOf": oneOf},
					"note":   map[string]any{"type": "string", "title": "Note", "description": "Anything else"}}}})
			var res struct {
				Action  string         `json:"action"`
				Content map[string]any `json:"content"`
			}
			json.Unmarshal(r.Result, &res)
			text := "declined: " + res.Action
			if res.Action == "accept" {
				text = fmt.Sprintf("answer: %v %v", res.Content["choice"], res.Content["note"])
			}
			update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}})
		case "config":
			update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": fmt.Sprintf("config: %s %s %s", curModel, effort, mode)}})
		case "Reply": // Test's fixed prompt (machine.TestAgent): answered as a model would
			update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "ready"}})
		case "history":
			update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": fmt.Sprintf("history: %d turns", len(h.Turns))}})
		case "env":
			update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "env: " + arg + "=" + os.Getenv(arg)}})
		case "cwd":
			wd, _ := os.Getwd()
			update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "cwd: " + cwd + " " + wd}})
		case "write":
			path, body, _ := strings.Cut(arg, " ")
			abs := path
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(cwd, path)
			}
			if presets && mode == "read-only" { // DSH's sandbox: no permission asked
				callN++
				id := fmt.Sprintf("call-%d", callN)
				update(sid, map[string]any{"sessionUpdate": "tool_call", "toolCallId": id, "title": "write", "kind": "other", "status": "in_progress", "rawInput": map[string]any{"file_path": abs}})
				update(sid, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "status": "failed",
					"content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "Error: [sandbox: file access denied under read-only mode]"}}}})
				break
			}
			tool(sid, "edit", "Write "+path, map[string]any{"path": abs, "content": body}, abs, func() (string, error) {
				os.MkdirAll(filepath.Dir(abs), 0o755)
				return "wrote " + path, os.WriteFile(abs, []byte(body+"\n"), 0o644)
			})
		case "read":
			callN++
			id := fmt.Sprintf("call-%d", callN)
			abs := filepath.Join(cwd, arg)
			update(sid, map[string]any{"sessionUpdate": "tool_call", "toolCallId": id, "title": "Read File", "kind": "read", "status": "pending", "rawInput": map[string]any{}})
			update(sid, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "title": "Read " + arg, "locations": []any{map[string]any{"path": abs}}})
			b, _ := os.ReadFile(abs)
			update(sid, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "status": "completed", "rawOutput": string(b)})
		case "shell":
			tool(sid, "execute", arg, map[string]any{"command": arg}, "", func() (string, error) {
				c := shellCommand(arg)
				c.Dir = cwd
				b, err := c.CombinedOutput()
				return string(b), err
			})
		case "sleep":
			d, _ := time.ParseDuration(arg)
			select {
			case <-time.After(d):
			case <-cancelled:
				reply(id, map[string]any{"stopReason": "cancelled"})
				return
			}
		case "tool-sleep":
			stopped := false
			tool(sid, "execute", "sleep "+arg, map[string]any{"command": "sleep " + arg}, "", func() (string, error) {
				d, _ := time.ParseDuration(arg)
				select {
				case <-time.After(d):
					return "", nil
				case <-cancelled:
					stopped = true
					return "", fmt.Errorf("cancelled")
				}
			})
			if stopped {
				reply(id, map[string]any{"stopReason": "cancelled"})
				return
			}
		case "crash":
			os.Exit(1)
		case "fail":
			replyErr(id, -32603, arg)
			return
		case "fail-code":
			code, text, _ := strings.Cut(arg, " ")
			n, _ := strconv.Atoi(code)
			replyErr(id, n, text)
			return
		}
	}
	reply(id, map[string]any{"stopReason": "end_turn", "usage": map[string]any{"inputTokens": 100, "outputTokens": 20, "cachedReadTokens": 5}})
}

// fakeProvider is a model option of FAKE_ACP_PROVIDERS.
type fakeProvider struct {
	ID     string `json:"id"`
	Groups []struct {
		Name   string `json:"name"`
		Models []struct {
			Value string `json:"value"`
			Name  string `json:"name"`
		} `json:"models"`
	} `json:"groups"`
}

func providers() []fakeProvider {
	var ps []fakeProvider
	json.Unmarshal([]byte(os.Getenv("FAKE_ACP_PROVIDERS")), &ps)
	return ps
}

// providerOffers: model option id of FAKE_ACP_PROVIDERS has value.
func providerOffers(id, value string) bool {
	for _, o := range providers() {
		for _, g := range o.Groups {
			for _, m := range g.Models {
				if o.ID == id && m.Value == value {
					return true
				}
			}
		}
	}
	return false
}

func modelConfig(current string) any {
	if file := os.Getenv("FAKE_ACP_CONFIG_FILE"); file != "" {
		b, _ := os.ReadFile(file)
		return json.RawMessage(b)
	}
	opts := []any{}
	for _, x := range strings.Split(os.Getenv("FAKE_ACP_MODELS"), ",") {
		if x == "" {
			continue
		}
		opts = append(opts, map[string]any{"value": x, "name": x})
		if current == "" {
			current = x
		}
	}
	return []any{
		map[string]any{"id": "mode", "category": "mode", "type": "select", "currentValue": "ask", "options": []any{map[string]any{"value": "ask"}}},
		map[string]any{"id": "llm", "category": "model", "type": "select", "currentValue": current,
			"options": []any{map[string]any{"group": "g", "name": "Models", "options": opts}}},
	}
}

func tool(sid, kind, title string, input map[string]any, path string, run func() (string, error)) {
	callN++
	id := fmt.Sprintf("call-%d", callN)
	// Announce the call first, then fill in its input, as real agents do.
	update(sid, map[string]any{"sessionUpdate": "tool_call", "toolCallId": id, "title": "Preparing…", "kind": kind, "status": "pending", "rawInput": map[string]any{}})
	tc := map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "title": title, "rawInput": input}
	if path != "" {
		tc["locations"] = []any{map[string]any{"path": path}}
	}
	update(sid, tc)
	options := []any{
		map[string]any{"optionId": "allow-once", "name": "Allow", "kind": "allow_once"},
		map[string]any{"optionId": "allow-always", "name": "Always allow", "kind": "allow_always"},
		map[string]any{"optionId": "reject-once", "name": "Reject", "kind": "reject_once"},
	}
	if os.Getenv("FAKE_ACP_ALLOW_ALWAYS_ONLY") == "1" {
		options = []any{options[1], options[2]}
	}
	r := ask("session/request_permission", map[string]any{"sessionId": sid, "toolCall": map[string]any{"toolCallId": id, "title": title, "kind": kind}, "options": options})
	var res struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	json.Unmarshal(r.Result, &res)
	if r.Error != nil || res.Outcome.OptionID != "allow-once" {
		update(sid, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "status": "failed",
			"content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "rejected"}}}})
		return
	}
	update(sid, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "status": "in_progress"})
	text, err := run()
	status := "completed"
	if err != nil {
		status, text = "failed", text+err.Error()
	}
	update(sid, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "status": status,
		"content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": text}}}})
}
