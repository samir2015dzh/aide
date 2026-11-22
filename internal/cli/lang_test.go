package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"text/tabwriter"
)

func TestLanguage(t *testing.T) {
	for _, c := range []struct{ flag, env, lcAll, lang, want string }{
		{"zh-CN", "", "", "", "zh-CN"},
		{"en", "zh-CN", "", "zh_CN.UTF-8", "en"},
		{"", "zh", "", "", "zh-CN"},
		{"auto", "", "", "zh_CN.UTF-8", "zh-CN"},
		{"", "", "en_US.UTF-8", "zh_CN.UTF-8", "en"},
		{"", "", "", "en_GB.UTF-8", "en"},
		{"", "auto", "zh_TW.UTF-8", "", "zh-CN"},
	} {
		t.Setenv("AIDE_LANG", c.env)
		t.Setenv("HERMESH_LANG", "")
		t.Setenv("LC_ALL", c.lcAll)
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", c.lang)
		if got := language(c.flag); got != c.want {
			t.Errorf("%+v: %s, want %s", c, got, c.want)
		}
	}
	if got := langArg([]string{"auth", "--lang", "zh-CN", "status"}); got != "zh-CN" {
		t.Fatal(got)
	}
	if got := langArg([]string{"--lang=en"}); got != "en" {
		t.Fatal(got)
	}
}

func TestTranslateZH(t *testing.T) {
	for in, want := range map[string]string{
		"Logged in to owner as \"client@box\". Run `aide project list` to see where it can work.\n":                                                  "已登录到 owner，名字为 \"client@box\"。运行 `aide project list` 查看它可以在哪里工作。\n",
		"error: which server? give your server's address (relay.example.com) or a connection address (relay.example.com/o_…), or set $AIDE_SERVER\n": "错误：哪台服务器？请给出服务器地址（relay.example.com）或连接地址（relay.example.com/o_…），或设置 $AIDE_SERVER\n",
		"3 message(s) wait in your inbox: aide message list\n":                                                                                       "收件箱里有 3 条消息等你处理：aide message list\n",
		"✓ grant       v1 · all machines, all projects · all agents · sees own sessions · doesn't expire\n":                                          "✓ 授权        v1 · 全部机器、全部项目 · 全部 agent · 只看自己的会话 · 不过期\n",
		"something nobody translated\n": "something nobody translated\n",
	} {
		if got := translateZH(in); got != want {
			t.Errorf("%q\n got %q\nwant %q", in, got, want)
		}
	}
	// Every catalog entry compiles and is used for something.
	for _, e := range catalogZH {
		if e.en == "" || e.zh == "" {
			t.Errorf("empty entry %v", e)
		}
	}
}

// -o json is never translated: the same bytes in any language.
func TestJSONNotTranslated(t *testing.T) {
	home := t.TempDir()
	run := func(lang string) string {
		t.Setenv("AIDE_HOME", home)
		t.Setenv("AIDE_LANG", lang)
		var out, errb bytes.Buffer
		Main([]string{"auth", "status", "-o", "json"}, &out, &errb)
		return out.String()
	}
	en, zh := run("en"), run("zh-CN")
	if en != zh || !strings.Contains(en, "not logged in") {
		t.Fatalf("json differs:\n%s\n%s", en, zh)
	}
	var out bytes.Buffer
	t.Setenv("AIDE_LANG", "zh-CN")
	Main([]string{"auth", "status"}, &out, &out)
	if !strings.Contains(out.String(), "未登录") || strings.Contains(out.String(), "not logged in") {
		t.Fatalf("text in Chinese:\n%s", out.String())
	}
}

// Tables keep their columns in Chinese: the header and the state cells are
// translated where they stand; a title that happens to say "running" is not.
func TestTranslateTable(t *testing.T) {
	var b bytes.Buffer
	tw := tabwriter.NewWriter(&b, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "SESSION\tTITLE\tSTATE\tPROJECT\tAGENT\tTURNS\tNOW / LAST")
	fmt.Fprintln(tw, "s_1\trunning\trunning · quiet 12m\tweb\tclaude\t2\tBash")
	fmt.Fprintln(tw, "s_22\tfix the page\tlost · last known running · 7m ago\tweb\tcodex\t1\t-")
	fmt.Fprintln(tw, "s_3\tdone\tdone\tapi\tgemini\t1\tok")
	tw.Flush()
	got := translateZH(b.String())
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if !strings.HasPrefix(lines[0], "会话") || !strings.Contains(lines[0], "状态") || !strings.Contains(lines[0], "现在/最近") {
		t.Fatalf("header: %q", lines[0])
	}
	// Every column starts where the English one did.
	en := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	col := func(l string, n int) int { // display column of the n-th cell
		idx := cellGap.FindAllStringIndex(l, -1)
		return width(l[:idx[n][0]])
	}
	for i := range lines {
		for n := 1; n < 7; n++ {
			if col(lines[i], n) != col(en[i], n) {
				t.Errorf("line %d cell %d at %d, was %d:\n%s\n%s", i, n, col(lines[i], n), col(en[i], n), en[i], lines[i])
			}
		}
	}
	if !strings.Contains(lines[1], "s_1       running ") || !strings.Contains(lines[1], "运行中 · 12m 无新事件") {
		t.Errorf("row 1: %q", lines[1])
	}
	if !strings.Contains(lines[2], "失联 · 最后已知 运行中 · 7m前") {
		t.Errorf("row 2: %q", lines[2])
	}
	if !strings.Contains(lines[3], "s_3       done           已完成") {
		t.Errorf("row 3 (its title stays): %q", lines[3])
	}
}

// A table printed a cell at a time (text/tabwriter writes so) through the
// writer: its state cells still in Chinese, in their columns.
func TestTranslatorTableInPieces(t *testing.T) {
	var out bytes.Buffer
	tr := &translator{w: &out, on: func() bool { return true }}
	tw := tabwriter.NewWriter(tr, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "SESSION\tTITLE\tSTATE\tNOW / LAST")
	fmt.Fprintln(tw, "s_1\trunning\tdone unseen\thi")
	tw.Flush()
	fmt.Fprint(tr, "Password: ")
	if out.String() == "" || strings.HasSuffix(out.String(), "密码：") {
		t.Fatalf("a line not ended was written early: %q", out.String())
	}
	tr.Flush()
	got := out.String()
	for _, want := range []string{"会话", "状态", "s_1       running   已完成未看", "密码："} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
}
