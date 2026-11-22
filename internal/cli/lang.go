package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/samir2015dzh/aide/internal/brandenv"
)

// The CLI's words in the owner's language (work order #38). What a person
// reads — help, progress, status lines, errors and hints — is in Chinese on
// a Chinese system, as the desktop app is; -o json is never touched: its
// fields and values stay as docs/cli-output.md has them, whatever the
// language. The choice: --lang (en, zh-CN, auto), else $AIDE_LANG, else the
// system's language (LC_ALL, LC_MESSAGES, LANG; on Windows the user's
// display language), as the desktop app takes the system's when the owner
// chose none.
//
// How: the help is replaced in place (helpZH), and what commands print in
// text mode passes through a writer that rewrites each known English
// sentence (catalogZH, its %-verbs matching anything) into the Chinese one,
// the matched parts carried over. A sentence not in the catalog stays as it
// was; identifiers, commands and names are never translated.

const langHelp = "the language of what a person reads: zh-CN, en or auto (default: $AIDE_LANG, else the system's); -o json is the same in any"

// language picks "zh-CN" or "en" from the flag value, the environment and
// the system.
func language(flag string) string {
	for _, v := range []string{flag, brandenv.Get("LANG")} {
		switch norm := strings.ToLower(strings.TrimSpace(v)); {
		case norm == "" || norm == "auto":
			continue
		case strings.HasPrefix(norm, "zh"):
			return "zh-CN"
		default:
			return "en"
		}
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if strings.HasPrefix(strings.ToLower(v), "zh") {
				return "zh-CN"
			}
			return "en"
		}
	}
	if strings.HasPrefix(strings.ToLower(systemLanguage()), "zh") {
		return "zh-CN"
	}
	return "en"
}

// langArg is --lang's value in args, read before cobra parses them: help
// (--help, a group alone) is printed without running any hook.
func langArg(args []string) string {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, "--lang="); ok {
			return v
		}
		if a == "--lang" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// localize puts root's help in lang.
func localize(root *cobra.Command, lang string) {
	if lang != "zh-CN" {
		return
	}
	var walk func(c *cobra.Command, path string)
	walk = func(c *cobra.Command, path string) {
		if path != "" {
			path += " "
		}
		path += c.Name()
		if h, ok := helpZH[path]; ok {
			c.Short = h.short
			if h.long != "" {
				c.Long = h.long
			}
		}
		c.SetUsageTemplate(usageZH)
		c.Flags().VisitAll(func(f *pflagFlag) {
			if u, ok := flagsZH[f.Usage]; ok {
				f.Usage = u
			}
		})
		c.PersistentFlags().VisitAll(func(f *pflagFlag) {
			if u, ok := flagsZH[f.Usage]; ok {
				f.Usage = u
			}
		})
		for _, s := range c.Commands() {
			walk(s, path)
		}
	}
	walk(root, "")
	root.InitDefaultHelpFlag()
	if f := root.Flags().Lookup("help"); f != nil {
		f.Usage = "显示帮助"
	}
}

// translator rewrites what it is given with the catalog, as a person reads
// it: only while on() says so (text output).
type translator struct {
	w  io.Writer
	on func() bool
}

func (t translator) Write(p []byte) (int, error) {
	if !t.on() {
		return t.w.Write(p)
	}
	if _, err := io.WriteString(t.w, translateZH(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

type rule struct {
	re *regexp.Regexp
	zh string // with $1, $2… for the parts matched
}

var (
	rulesOnce sync.Once
	rules     []rule
)

// groupRef is ${n} in a translation, once its $ were doubled.
var groupRef = regexp.MustCompile(`\$\$\{(\d+)\}`)

// verb is a fmt verb in a catalog sentence.
var verb = regexp.MustCompile(`%[-+# 0-9.]*[sqdvf]`)

// compileRules turns the catalog into rules: each %-verb matches anything
// (the shortest), or to the line's end when the sentence ends with one;
// longer sentences first, so that one containing another wins.
func compileRules() {
	for _, e := range catalogZH {
		parts := verb.Split(e.en, -1)
		verbs := verb.FindAllString(e.en, -1)
		var b strings.Builder
		for i, p := range parts {
			b.WriteString(regexp.QuoteMeta(p))
			if i < len(parts)-1 {
				switch {
				case strings.HasSuffix(verbs[i], "d"):
					b.WriteString(`(-?\d+)`)
				case i == len(parts)-2 && parts[len(parts)-1] == "":
					b.WriteString(`(.+)`)
				default:
					b.WriteString(`(.+?)`)
				}
			}
		}
		// A $ of the text itself ($AIDE_SERVER) is not a group: only ${n} is.
		zh := strings.ReplaceAll(e.zh, "$", "$$")
		zh = groupRef.ReplaceAllString(zh, "$${$1}")
		rules = append(rules, rule{re: regexp.MustCompile(b.String()), zh: zh})
	}
	// Longest first.
	for i := 1; i < len(rules); i++ {
		for j := i; j > 0 && len(rules[j].re.String()) > len(rules[j-1].re.String()); j-- {
			rules[j], rules[j-1] = rules[j-1], rules[j]
		}
	}
}

// translateZH rewrites s line by line with the catalog.
func translateZH(s string) string {
	rulesOnce.Do(compileRules)
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		for _, r := range rules {
			if r.re.MatchString(l) {
				l = r.re.ReplaceAllString(l, r.zh)
			}
		}
		lines[i] = l
	}
	return strings.Join(lines, "")
}

// zhf is a catalog entry: an English sentence as the code formats it, and
// its Chinese with $1, $2… for what each %-verb matched.
type zhf struct{ en, zh string }

type pflagFlag = pflag.Flag

func (e zhf) String() string { return fmt.Sprintf("%q → %q", e.en, e.zh) }
