package dshnative

import (
	"encoding/json"
	"github.com/hermeshai/hermesh/internal/legacy"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
	"github.com/hermeshai/hermesh/internal/provider"
)

// inlineOutput is how much of a tool's output an event carries, as for
// Hermesh's own sessions (EVENT_VIEW_FULL).
const inlineOutput = 16 << 10

// message is the part of a DSH message (@deepseek-ai/dsh-llm) shown.
type message struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Source struct {
		Kind  string `json:"kind"`
		RPCID string `json:"rpcId"`
	} `json:"source"`
	ToolCallID string `json:"toolCallId"`
	IsError    bool   `json:"isError"`
}

// text is the message's text blocks; with files, those it carries are
// named too.
func (m message) text(files bool) string {
	var parts []string
	for _, c := range m.Content {
		switch {
		case c.Type == "text":
			parts = append(parts, c.Text)
		case files && (c.Type == "image" || c.Type == "file"):
			parts = append(parts, "["+c.Type+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// appended: the event entered the conversation where it stands. DSH also
// logs copies that replace a stretch of it for the model (a compaction's
// summary): those are not what was said.
func appended(e event) bool { return string(e.SurfaceOp) == `"append"` }

// RequestPrefix starts the request id of every message Aide sends into a
// DSH session (through the bridge); DSH keeps it as the message's rpcId.
// The messages sent before the rename have the old prefix (internal/legacy).
const RequestPrefix = "hermesh-"

// userText is what the owner wrote, when e is a prompt of theirs (DSH
// also logs as user messages the context it adds itself); viaHermesh: the
// owner sent it from Hermesh (its rpcId is one Hermesh made).
func userText(e event) (text string, viaHermesh, ok bool) {
	var m message
	if e.Type != "user/message" || !appended(e) || json.Unmarshal(e.Data, &m) != nil || m.Source.Kind != "user" {
		return "", false, false
	}
	return m.text(true), strings.HasPrefix(m.Source.RPCID, RequestPrefix) || strings.HasPrefix(m.Source.RPCID, legacy.DSHRequest), true
}

// transcript is the conversation as Hermesh's session events: turns
// started and completed, the owner's prompts, DSH's replies, tool calls
// and results, and its compactions (a StatusChanged, reason "compacted").
func (p *parsed) transcript() []*sessionv1.SessionEvent {
	var out []*sessionv1.SessionEvent
	for _, e := range p.events {
		ev := &sessionv1.SessionEvent{Seq: e.Seq + 1, Ts: timestamppb.New(time.UnixMilli(e.Time))}
		switch e.Type {
		case "turn/start":
			var d struct{ Turn uint32 }
			json.Unmarshal(e.Data, &d)
			ev.Kind = &sessionv1.SessionEvent_TurnStarted{TurnStarted: &sessionv1.TurnStarted{Turn: d.Turn}}
		case "user/message":
			text, viaHermesh, ok := userText(e)
			if !ok {
				continue
			}
			um := &sessionv1.UserMessage{Text: text}
			if viaHermesh { // only the owner sends through the bridge
				um.From = "owner"
			}
			ev.Kind = &sessionv1.SessionEvent_UserMessage{UserMessage: um}
		case "assistant/message":
			var d struct{ Message message }
			if !appended(e) || json.Unmarshal(e.Data, &d) != nil || d.Message.text(false) == "" {
				continue
			}
			ev.Kind = &sessionv1.SessionEvent_AgentMessage{AgentMessage: &sessionv1.AgentMessage{Text: d.Message.text(false)}}
		case "tool/call":
			var d struct{ CallID, Name, Arguments string }
			json.Unmarshal(e.Data, &d)
			args, _ := provider.Truncate(d.Arguments, 500)
			ev.Kind = &sessionv1.SessionEvent_ToolCall{ToolCall: &sessionv1.ToolCall{CallId: d.CallID, Kind: string(provider.ToolOther), Name: d.Name, Summary: args}}
		case "tool/result":
			var d struct{ Message message }
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			text, cut := provider.Truncate(d.Message.text(false), inlineOutput)
			ev.Kind = &sessionv1.SessionEvent_ToolResult{ToolResult: &sessionv1.ToolResult{CallId: d.Message.ToolCallID, Summary: text, IsError: d.Message.IsError, Truncated: cut}}
		case "turn/end":
			var d struct {
				Turn   uint32
				Reason struct {
					Kind  string
					Error struct{ Message string }
				}
			}
			json.Unmarshal(e.Data, &d)
			ev.Kind = &sessionv1.SessionEvent_TurnCompleted{TurnCompleted: turnEnd(d.Turn, d.Reason.Kind, d.Reason.Error.Message)}
		case "compaction/end":
			var d struct{ Error string }
			if json.Unmarshal(e.Data, &d) != nil || d.Error != "" {
				continue
			}
			ev.Kind = &sessionv1.SessionEvent_Status{Status: &sessionv1.StatusChanged{Reason: "compacted"}}
		default:
			continue
		}
		out = append(out, ev)
	}
	return out
}

// turnEnd is a turn ended for reason (DSH's TurnEndReason kind).
func turnEnd(turn uint32, reason, message string) *sessionv1.TurnCompleted {
	t := &sessionv1.TurnCompleted{Turn: turn}
	fail := func(kind sessionv1.FailureKind, msg string) {
		t.IsError, t.Failure = true, &sessionv1.Failure{Kind: kind, Message: msg}
	}
	switch reason {
	case "completed", "forked":
	case "aborted":
		fail(sessionv1.FailureKind_FAILURE_KIND_INTERRUPTED, "interrupted")
	case "interrupted": // DSH stopped mid-turn, and closed it when it opened the session again
		fail(sessionv1.FailureKind_FAILURE_KIND_AGENT_CRASHED, "DSH stopped before the turn ended")
	case "max-tokens":
		fail(sessionv1.FailureKind_FAILURE_KIND_LIMIT_REACHED, "the model reached its output limit")
	case "blocked":
		fail(sessionv1.FailureKind_FAILURE_KIND_REFUSED, "blocked")
	case "error":
		fail(sessionv1.FailureKind_FAILURE_KIND_UNKNOWN, message)
	default:
		fail(sessionv1.FailureKind_FAILURE_KIND_UNKNOWN, reason)
	}
	return t
}
