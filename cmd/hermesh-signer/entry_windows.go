package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"github.com/hermeshai/hermesh/internal/signer/unlock"
)

// entry is a window of the panel's that asks for a passphrase: to sign what
// the owner approved, in passphrase mode, or to switch how this PC's root
// key is unlocked (hermesh-signer unlock). Drawn by this elevated process
// as the panel is, the user's programs that are not elevated can neither
// read what is typed in it nor type in it.
type entry struct {
	title, text, warning, button string
	// labels: one field for each.
	labels []string
	// submit checks what was typed, off the window's thread: done closes
	// the window, the owner verified; otherwise problem says what to fix,
	// and it stays. An error closes it.
	submit func(hwnd windows.HWND, values []string) (done bool, problem string, err error)
	// wait: how long until a passphrase is tried again (wrong ones wait).
	wait func() time.Duration
}

const (
	wmChar    = 0x0102
	wmChecked = 0x8002 // WM_APP+2: submit answered
	lockTimer = 2
)

func fieldID(i int) string { return fmt.Sprint("field", i) }

func (v *view) inField() (int, bool) {
	var i int
	_, err := fmt.Sscanf(v.focus, "field%d", &i)
	return i, err == nil && v.entry != nil && i < len(v.values)
}

// ask shows e and waits for it, on this thread; it reports whether the
// owner was verified.
func (p *drawnPanel) ask(e *entry) (bool, error) {
	v := &view{p: p, entry: e, values: make([][]uint16, len(e.labels)), focus: fieldID(0)}
	if err := v.show(); err != nil {
		return false, err
	}
	return v.ok, v.err
}

// passphrase asks for the passphrase of the root key keyID, over the panel
// that waits for it, and opens the key with it for the signature: what
// Windows Hello does in Windows Hello mode.
func (p *drawnPanel) passphrase(keyID string) (bool, error) {
	return p.ask(&entry{title: p.w.PassphraseTitle, text: p.w.PassphraseText, button: p.w.Unlock, labels: []string{p.w.Passphrase},
		wait: func() time.Duration { return p.keys.Wait(keyID) },
		submit: func(_ windows.HWND, values []string) (bool, string, error) {
			return p.w.checked(p.keys.Unlock(keyID, values[0]))
		}})
}

// usePassphrase has the owner choose a passphrase, then confirm with
// Windows Hello, the mode the root key keyID leaves, and seals its PIN.
func (p *drawnPanel) usePassphrase(keyID string) (bool, error) {
	return p.ask(&entry{title: p.w.UsePassphraseTitle, text: p.w.UsePassphraseText, warning: p.w.PassphraseRisk, button: p.w.UsePassphrase,
		labels: []string{p.w.NewPassphrase, p.w.Again},
		submit: func(h windows.HWND, values []string) (bool, string, error) {
			if values[0] != values[1] {
				return false, p.w.Mismatch, nil
			}
			ok, err := p.keys.UsePassphrase(keyID, values[0], func() (bool, error) { return p.hello(h, p.w.HelloSwitch) })
			if errors.Is(err, unlock.ErrShort) {
				return false, fmt.Sprintf(p.w.Short, unlock.MinPassphrase), nil
			}
			return ok, "", err // not verified: it stays, to try again or cancel
		}})
}

// useHello takes the passphrase once more, the mode the root key keyID
// leaves, and keeps its PIN for Windows Hello mode again.
func (p *drawnPanel) useHello(keyID string) (bool, error) {
	return p.ask(&entry{title: p.w.UseHelloTitle, text: p.w.UseHelloText, button: p.w.UseHello, labels: []string{p.w.Passphrase},
		wait: func() time.Duration { return p.keys.Wait(keyID) },
		submit: func(_ windows.HWND, values []string) (bool, string, error) {
			return p.w.checked(p.keys.UseHello(keyID, values[0]))
		}})
}

// checked is what a passphrase's check tells the window.
func (w words) checked(err error) (bool, string, error) {
	var locked *unlock.Locked
	switch {
	case err == nil:
		return true, "", nil
	case errors.Is(err, unlock.ErrPassphrase):
		return false, w.WrongPassphrase, nil
	case errors.As(err, &locked):
		return false, "", nil // the window tells the wait
	}
	return false, "", err
}

// lockLeft is how long until a passphrase is tried again.
func (v *view) lockLeft() time.Duration {
	if v.entry == nil || v.entry.wait == nil {
		return 0
	}
	return v.entry.wait()
}

// entryContent: the shield, what it asks and why, the fields, and what was
// wrong (or how long to wait).
func (v *view) entryContent(c *canvas) (int, []op) {
	th, e := v.th, v.entry
	x, w := v.px(32), v.px(456)
	top := v.px(20)
	o := []op{func(c *canvas, dy int) { v.shield(c, x+w/2-v.px(20), top-dy) }}
	y := top + v.px(40) + v.px(8)
	y += v.para(c, &o, "title", e.title, x, y, w, 1.35, th.fg, true) + v.px(8)
	y += v.para(c, &o, "body", e.text, x, y, w, 1.6, th.fg2, true) + v.px(14)
	if e.warning != "" {
		wh, wo := v.warning(c, e.warning, x, y, w)
		o = append(o, wo...)
		y += wh + v.px(14)
	}
	small := v.fonts["small"]
	fh := v.px(34)
	for i, label := range e.labels {
		ly := y
		o = append(o, func(c *canvas, dy int) { c.text(small, x, ly-dy, label, th.fg2) })
		y += c.lineHeight(small) + v.px(6)
		id, fy := fieldID(i), y
		v.targets[id], v.inMain[id] = rect{x, fy, w, fh}, true
		o = append(o, func(c *canvas, dy int) { v.field(c, i, x, fy-dy, w, fh) })
		y += fh + v.px(12)
	}
	// One line, always there, for what was wrong: the window keeps its size.
	ly := y
	o = append(o, func(c *canvas, dy int) {
		text := v.problem
		if left := v.lockLeft(); left > 0 {
			s := int(left.Round(time.Second).Seconds())
			text = fmt.Sprintf(v.p.w.Locked, fmt.Sprintf("%d:%02d", s/60, s%60))
		}
		if text != "" {
			c.text(small, x, ly-dy, text, th.danger)
		}
	})
	return y + c.lineHeight(small) + v.px(20), o
}

// field i: a box with a dot for each character typed, and the caret.
func (v *view) field(c *canvas, i, x, y, w, h int) {
	th := v.th
	radius, bw := v.px(4), max(1, v.px(1))
	off := v.waiting || v.lockLeft() > 0
	fill := th.card
	if off {
		fill = over(th.card, th.fg, .04)
	}
	c.framed(x, y, w, h, radius, bw, fill, th.btnLine)
	f := v.fonts["body"]
	pad := v.px(10)
	dots := strings.Repeat("●", min(len(utf16.Decode(v.values[i])), 64))
	for dots != "" && c.width(f, dots) > w-2*pad {
		dots = dots[len("●"):]
	}
	ty := y + (h-c.lineHeight(f))/2
	c.text(f, x+pad, ty, dots, th.fg)
	if v.focus == fieldID(i) && !off {
		c.rect(x+pad+c.width(f, dots)+v.px(1), ty+v.px(2), max(1, v.px(1)), c.lineHeight(f)-v.px(4), th.fg)
		g := v.px(3)
		c.ring(x-g, y-g, w+2*g, h+2*g, radius+g, max(1, v.px(2)), th.accent)
	}
}

// typed takes a character the owner typed into the field in focus.
func (v *view) typed(ch uint16) {
	i, ok := v.inField()
	if !ok || v.waiting || v.lockLeft() > 0 {
		return
	}
	u := v.values[i]
	switch {
	case ch == 0x08: // Backspace: a character, both halves of a pair
		n := len(u)
		if n > 1 && utf16.IsSurrogate(rune(u[n-1])) {
			n--
		}
		if n > 0 {
			clear(u[n-1:])
			v.values[i] = u[:n-1]
		}
	case ch == 0x7F: // Ctrl+Backspace: all of it
		clear(u)
		v.values[i] = u[:0]
	case ch < 0x20: // other controls; no pasting, as nothing typed here should be on the clipboard
		return
	default:
		v.values[i] = append(u, ch)
	}
	v.problem = ""
	v.redraw()
}

// enter: Enter in field i goes to the next one, and submits from the last.
func (v *view) enter(i int) {
	if i+1 < len(v.values) {
		v.focus = fieldID(i + 1)
		v.redraw()
		return
	}
	v.submit()
}

// submit checks what was typed, the window waiting meanwhile (under
// Windows Hello, should the check ask it).
func (v *view) submit() {
	if v.waiting || v.lockLeft() > 0 {
		return
	}
	values := make([]string, len(v.values))
	for i, u := range v.values {
		values[i] = string(utf16.Decode(u))
	}
	v.waiting, v.hover, v.pressed, v.problem = true, "", "", ""
	procSetTimer.Call(v.hwnd, 1, 90, 0)
	setTopmost(v.hwnd, false)
	v.redraw()
	h := v.hwnd
	go func() {
		runtime.LockOSThread() // not unlocked: Windows Hello may make the thread's COM multithreaded
		done, problem, err := v.entry.submit(windows.HWND(h), values)
		v.mu.Lock()
		v.checkedOK, v.checkedProblem, v.checkedErr = done, problem, err
		v.mu.Unlock()
		procPostMessage.Call(h, wmChecked, 0, 0)
	}()
}

// checked: submit answered.
func (v *view) checked() {
	v.mu.Lock()
	done, problem, err := v.checkedOK, v.checkedProblem, v.checkedErr
	v.mu.Unlock()
	procKillTimer.Call(v.hwnd, 1)
	v.forget()
	switch {
	case err != nil:
		v.finish(false, err)
		return
	case done:
		v.finish(true, nil)
		return
	}
	v.waiting, v.problem, v.focus = false, problem, fieldID(0)
	setTopmost(v.hwnd, true)
	if v.lockLeft() > 0 {
		procSetTimer.Call(v.hwnd, lockTimer, 1000, 0)
	}
	v.redraw()
}

// forget clears what was typed.
func (v *view) forget() {
	for i, u := range v.values {
		clear(u)
		v.values[i] = u[:0]
	}
}

// lockTick counts the wait down, and lets the owner type again after it.
func (v *view) lockTick() {
	if v.lockLeft() == 0 {
		procKillTimer.Call(v.hwnd, lockTimer)
	}
	v.redraw()
}
