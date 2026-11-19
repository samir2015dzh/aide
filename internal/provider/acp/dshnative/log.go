package dshnative

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/klauspost/compress/zstd"
)

// A session's log, as @deepseek-ai/dsh-session-persistence-jsonl writes it
// (0.2.2): JSON lines, the session's header first, then one event per line.
// Its default encoding is a series of independent, checksummed zstd frames:
// the header alone in the first, then one per batch of events DSH flushed.
// DSH never rewrites what it committed; a frame or line cut short (a crash,
// or a write under way as we read) is the log's torn end.

// formatVersion is the log format this reads (DSH's SESSION_FORMAT_VERSION).
const formatVersion = 4

// logName is a log file's name: session.jsonl is format 0, session.vN.jsonl
// format N, with .zstd when compressed. DSH keeps the older formats it
// migrated from beside the newest, which is the one it reads.
var logName = regexp.MustCompile(`^session(?:\.v([1-9][0-9]*))?\.jsonl(\.zstd)?$`)

// logFile is the log DSH reads in a session's directory ("" when it has
// none).
func logFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	path, version := "", -1
	for _, e := range entries {
		m := logName.FindStringSubmatch(e.Name())
		if m == nil || e.IsDir() {
			continue
		}
		v := 0
		if m[1] != "" {
			if v, err = strconv.Atoi(m[1]); err != nil {
				continue
			}
		}
		if v > version || v == version && m[2] != "" {
			path, version = filepath.Join(dir, e.Name()), v
		}
	}
	return path, nil
}

// header is the first line of a log (SessionHeader).
type header struct {
	Type        string `json:"type"`
	Version     int    `json:"version"`
	ID          string `json:"id"`
	CreatedAt   int64  `json:"createdAt"` // Unix ms
	Cwd         string `json:"cwd"`
	Origin      string `json:"origin"` // "subagent" for a subagent's session
	AgentPreset string `json:"agentPreset"`
}

// event is one event line: data and surfaceOp are read by those that need
// them.
type event struct {
	Type      string          `json:"type"`
	Seq       uint64          `json:"seq"`
	Time      int64           `json:"time"` // Unix ms
	Ignorable bool            `json:"ignorable"`
	SurfaceOp json.RawMessage `json:"surfaceOp"`
	Data      json.RawMessage `json:"data"`
}

// parsed is a log as read: its header, its events and, when they could not
// all be read, why.
type parsed struct {
	header  header
	events  []event
	problem string
}

// parse reads the log at path. An error means it is not a session's log
// at all (no header); a log of another format, or with an event of a kind
// this does not know that DSH requires readers to know, has problem set
// and the events before it.
func parse(path string) (*parsed, error) {
	lines, err := records(path)
	if err != nil {
		return nil, err
	}
	p := &parsed{}
	if len(lines) == 0 || json.Unmarshal(lines[0], &p.header) != nil || p.header.Type != "session" || p.header.ID == "" {
		return nil, fmt.Errorf("%s has no DSH session header", path)
	}
	if p.header.Version != formatVersion {
		p.problem = fmt.Sprintf("unsupported DSH log format version %d", p.header.Version)
		return p, nil
	}
	for _, l := range lines[1:] {
		var e event
		if json.Unmarshal(l, &e) != nil {
			p.problem = "unreadable DSH log line"
			break
		}
		if !knownEvents[e.Type] && !e.Ignorable {
			p.problem = fmt.Sprintf("unsupported DSH log format: event %q", e.Type)
			break
		}
		p.events = append(p.events, e)
	}
	return p, nil
}

// records is the complete lines of the log at path.
func records(path string) ([][]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".zstd") {
		frames, err := zstdFrames(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		var plain []byte
		for _, f := range frames {
			if plain, err = decoder().DecodeAll(b[f[0]:f[1]], plain); err != nil {
				return nil, fmt.Errorf("%s: corrupt zstd frame at byte %d: %w", path, f[0], err)
			}
		}
		b = plain
	}
	lines := bytes.Split(b, []byte{'\n'})
	return lines[:len(lines)-1], nil // the last is empty, or a line not yet finished
}

var decoder = sync.OnceValue(func() *zstd.Decoder {
	d, err := zstd.NewReader(nil)
	if err != nil {
		panic(err)
	}
	return d
})

const zstdMagic = 0xFD2FB528

// zstdFrames is where each complete frame of b starts and ends, found from
// the frames' and blocks' headers without decompressing them (DSH's
// scanZstdFrames). A last frame cut short is left out.
func zstdFrames(b []byte) ([][2]int, error) {
	var frames [][2]int
	for off := 0; off < len(b); {
		start := off
		if len(b)-off < 5 {
			return frames, nil
		}
		if binary.LittleEndian.Uint32(b[off:]) != zstdMagic {
			return nil, fmt.Errorf("not a zstd frame at byte %d", off)
		}
		desc := b[off+4]
		off += 5
		if desc&0x18 != 0 {
			return nil, fmt.Errorf("reserved zstd frame header bit at byte %d", off-1)
		}
		single := desc&0x20 != 0
		size := 0 // the content size field's
		if fcs := desc >> 6; fcs != 0 {
			size = 1 << fcs
		} else if single {
			size = 1
		}
		dict := int(desc & 3)
		if dict == 3 {
			dict = 4
		}
		if !single {
			off++ // the window descriptor
		}
		off += dict + size
		for last := false; !last; {
			if len(b)-off < 3 {
				return frames, nil
			}
			h := int(b[off]) | int(b[off+1])<<8 | int(b[off+2])<<16
			off += 3
			last = h&1 != 0
			n := h >> 3
			switch h >> 1 & 3 {
			case 1: // RLE: one byte repeated
				n = 1
			case 3:
				return nil, fmt.Errorf("reserved zstd block type at byte %d", off-3)
			}
			off += n
		}
		if desc&4 != 0 {
			off += 4 // the checksum
		}
		if off > len(b) {
			return frames, nil
		}
		frames = append(frames, [2]int{start, off})
	}
	return frames, nil
}

// knownEvents is the event vocabulary of DSH 0.2.2 (KNOWN_SESSION_EVENT_TYPES
// of @deepseek-ai/dsh-session 0.2.2-rc.1 and rc.4). DSH marks an event a
// reader may skip without knowing it ignorable; one it does not mark could
// change how the rest of the log reads, so a log with one this does not
// know is not read.
var knownEvents = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`agent-preset/selected agent/inbox/spliced approval/asked approval/decided approval/policy
		assistant/attempt assistant/message command/done command/run compaction/end compaction/prune compaction/start
		compaction/summary deliverables/presented developer/message feedback/message-delete feedback/message-put
		feedback/record goal/change hook/invoked hook/result image/offload job/registered job/settled llm/retry
		llm/retry-started model/selection permission/preset plan/mode request/context request/header sandbox/mode
		schedule/change session-log-deepseek/delivery-accepted session/end-seed session/title
		session/title-llm-request step/end step/start subagent/catalog subagent/descriptor
		subagent/model-selection-policy system/message team/member team/message/delivered team/message/queued
		team/mode team/task todo/write tool-workflow/agent-end tool-workflow/agent-start tool-workflow/run-end
		tool-workflow/run-start tool/call tool/ptc-dispatch tool/ptc-dispatch-start tool/result turn/end turn/start
		user/message web/deepseek-search-llm-request working-directory/change workspace/changes`) {
		knownEvents[t] = true
	}
}

// encodeSegment is DSH's name for a session's directory: its id with every
// UTF-16 unit but [A-Za-z0-9._-] written ~XXXX (and "." and ".." in full),
// so no id leads out of its project's directory.
func encodeSegment(id string) string {
	switch id {
	case ".":
		return "~002E"
	case "..":
		return "~002E~002E"
	}
	var s strings.Builder
	for _, u := range utf16.Encode([]rune(id)) {
		if u < 0x80 && (u >= 'a' && u <= 'z' || u >= 'A' && u <= 'Z' || u >= '0' && u <= '9' || u == '.' || u == '_' || u == '-') {
			s.WriteByte(byte(u))
		} else {
			fmt.Fprintf(&s, "~%04X", u)
		}
	}
	return s.String()
}
