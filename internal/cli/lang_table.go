package cli

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Tables in Chinese (work order #38). text/tabwriter aligns by characters,
// and a Chinese character takes two columns, so a table is translated after
// it is aligned: a header line (all capitals) word by word, and in the rows
// only a cell that is, whole, one of the words below (a state, "online", a
// reset time) — never a title, a name or anything a person wrote. Each
// keeps the column its English took: padded with spaces when narrower,
// taking from the spaces after it when wider, left in English when there is
// no room.

// headerZH are the column names, the longer phrases first.
var headerZH = []struct{ en, zh string }{
	{"DSH NATIVE SESSION (READ-ONLY)", "DSH 原生会话（只读）"},
	{"LAST ACTIVITY", "最近活动"},
	{"LAST EVENT", "最近事件"},
	{"NOW / LAST", "现在/最近"},
	{"SENT BY", "派活方"},
	{"DESCRIPTION", "说明"},
	{"WORKSPACE", "工作区"},
	{"SESSIONS", "会话数"},
	{"ADDRESS", "地址"},
	{"DEFAULT", "默认"},
	{"MACHINE", "机器"},
	{"PROJECT", "项目"},
	{"SESSION", "会话"},
	{"AGENTS", "Agent"},
	{"FAILED", "失败"},
	{"FOLDER", "目录"},
	{"MODELS", "模型"},
	{"RESETS", "重置"},
	{"WINDOW", "窗口"},
	{"AGENT", "Agent"},
	{"STATE", "状态"},
	{"TITLE", "标题"},
	{"TURNS", "回合"},
	{"DONE", "完成"},
	{"KIND", "类型"},
	{"NAME", "名字"},
	{"NOTE", "备注"},
	{"TIER", "档位"},
	{"USED", "已用"},
	{"OS", "系统"},
}

// cellZH are the words a whole cell may be.
var cellZH = map[string]string{
	"queued": "排队中", "running": "运行中", "waiting input": "等你回答", "waiting approval": "等你审批",
	"done unseen": "已完成未看", "done": "已完成", "failed": "出错", "interrupted": "已中断", "archived": "已归档",
	"unknown": "未知", "idle": "空闲", "ended": "已结束", "online": "在线", "offline": "离线", "due": "已到",
	"(not mailable)": "（不可寄信）",
}

// cellRules are the cells made of such words (docs/session-states.md's
// lines, a reset time); ${n} is translated by cellZH when it is one.
var cellRules = []struct {
	re *regexp.Regexp
	zh string
}{
	{regexp.MustCompile(`^lost · last known (.+) · (\S+) ago$`), "失联 · 最后已知 ${1} · ${2}前"},
	{regexp.MustCompile(`^lost · last known (.+)$`), "失联 · 最后已知 ${1}"},
	{regexp.MustCompile(`^(.+)\? · machine not answering$`), "${1}？· 机器未应答"},
	{regexp.MustCompile(`^(.+) · quiet (\S+)$`), "${1} · ${2} 无新事件"},
	{regexp.MustCompile(`^(.+) · catching up$`), "${1} · 正在同步"},
	{regexp.MustCompile(`^(.+) \(not mailable\)$`), "${1}（不可寄信）"},
	{regexp.MustCompile(`^in (\d+[hm][\dm]*)$`), "${1}后"},
	{regexp.MustCompile(`^(\S+) ago$`), "${1}前"},
}

// width is how many columns s takes on a terminal.
func width(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff || r >= 0xfe30 && r <= 0xfe4f || r >= 0xff00 && r <= 0xff60 || r >= 0xffe0 && r <= 0xffe6) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// fit puts zh where en was in line[i:], keeping the columns after it where
// they were; ok is false when there is no room.
func fit(line string, i int, en, zh string) (string, bool) {
	rest := line[i+len(en):]
	pad := width(en) - width(zh)
	if pad >= 0 {
		return line[:i] + zh + strings.Repeat(" ", pad) + rest, true
	}
	trimmed := strings.TrimLeft(rest, " ")
	spaces := len(rest) - len(trimmed)
	switch {
	case trimmed == "" || trimmed == "\n": // the last column: nothing after it to keep
		return line[:i] + zh + rest, true
	case spaces+pad >= 2: // still two spaces between the columns
		return line[:i] + zh + rest[-pad:], true
	}
	return line, false
}

// stateColumns are the columns whose cells may be state words.
var stateColumns = []string{"STATE", "LAST ACTIVITY", "LAST EVENT", "RESETS"}

// tableState is what a writer saw of the table it is printing: the display
// columns its state cells start at (from its header), until a blank line.
type tableState struct{ cols []int }

// see notes a header line's state columns, or forgets them at a blank one.
func (t *tableState) see(line string) {
	switch {
	case strings.TrimSpace(line) == "":
		t.cols = nil
	case headerLine.MatchString(line) && strings.Contains(line, "  "):
		t.cols = nil
		for _, c := range stateColumns {
			for from := 0; ; {
				i := strings.Index(line[from:], c)
				if i < 0 {
					break
				}
				i += from
				end := i + len(c)
				if (i == 0 || line[i-1] == ' ') && (end == len(line) || line[end] == ' ' || line[end] == '\n') {
					t.cols = append(t.cols, width(line[:i]))
				}
				from = end
			}
		}
	}
}

var headerLine = regexp.MustCompile(`^[A-Z][A-Z0-9 /()_-]*[A-Z)]\s*$`)

// translateHeader puts a table's header line in Chinese.
func translateHeader(line string) string {
	if !headerLine.MatchString(line) || !strings.Contains(line, "  ") {
		return line
	}
	for _, h := range headerZH {
		for from := 0; ; {
			i := strings.Index(line[from:], h.en)
			if i < 0 {
				break
			}
			i += from
			end := i + len(h.en)
			if (i > 0 && line[i-1] != ' ') || (end < len(line) && line[end] != ' ' && line[end] != '\n') {
				from = end // part of a longer word
				continue
			}
			next, _ := fit(line, i, h.en, h.zh)
			from = i + len(next) - len(line) + len(h.en)
			line = next
		}
	}
	return line
}

var cellGap = regexp.MustCompile(`\S(?:\s?\S)*`)

// translateCells puts a row's state words in Chinese: whole cells, in the
// state columns of the table's header only.
func translateCells(line string, cols []int) string {
	if len(cols) == 0 || !strings.Contains(line, "  ") {
		return line
	}
	idx := cellGap.FindAllStringIndex(line, -1)
	for k := len(idx) - 1; k >= 0; k-- { // from the right: the earlier positions stay
		if !slices.Contains(cols, width(line[:idx[k][0]])) {
			continue
		}
		cell := line[idx[k][0]:idx[k][1]]
		zh := translateCell(cell)
		if zh != cell && utf8.ValidString(zh) {
			line, _ = fit(line, idx[k][0], cell, zh)
		}
	}
	return line
}

func translateCell(cell string) string {
	if zh, ok := cellZH[cell]; ok {
		return zh
	}
	for _, r := range cellRules {
		m := r.re.FindStringSubmatch(cell)
		if m == nil {
			continue
		}
		out := r.zh
		for g := len(m) - 1; g >= 1; g-- {
			v := m[g]
			if zh, ok := cellZH[v]; ok {
				v = zh
			} else if zh := translateCell(v); zh != v {
				v = zh
			}
			out = strings.ReplaceAll(out, "${"+string(rune('0'+g))+"}", v)
		}
		return out
	}
	return cell
}
