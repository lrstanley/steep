// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/vt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// terminalProbe is a minimal model with a stable empty view.
type terminalProbe struct{}

func (terminalProbe) Init() tea.Cmd { return nil }

func (terminalProbe) Update(uv.Event) (tea.Model, tea.Cmd) {
	return terminalProbe{}, nil
}

func (terminalProbe) View() tea.View {
	return tea.NewView("")
}

// newTerminalHarness returns a harness over a [terminalProbe], and the
// recorder of its messages.
func newTerminalHarness(tb testing.TB, w, h int) (*Harness, *recorder) {
	tb.Helper()
	r := record(terminalProbe{})
	return NewHarness(tb, r, WithWindowSize(w, h)), r
}

func colorEqual(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// waitWindowSize waits for the model to receive a [tea.WindowSizeMsg] of
// wantW by wantH.
func waitWindowSize(tb testing.TB, r *recorder, wantW, wantH int) {
	tb.Helper()
	waitMsg(tb, r, func(m tea.WindowSizeMsg) bool {
		return m.Width == wantW && m.Height == wantH
	})
}

func TestHarness_terminalInitialWindowSizeMsg(t *testing.T) {
	t.Parallel()
	wantW, wantH := 11, 7
	_, r := newTerminalHarness(t, wantW, wantH)
	waitWindowSize(t, r, wantW, wantH)
}

func TestHarness_terminalDimensionsResizeInBandMsgs(t *testing.T) {
	t.Parallel()
	const wantW, wantH = 11, 7
	h, r := newTerminalHarness(t, wantW, wantH)
	waitWindowSize(t, r, wantW, wantH)

	const nextW, nextH = 30, 12
	_ = writeString(h, ansi.SetModeInBandResize)
	h.Resize(nextW, nextH)
	waitWindowSize(t, r, nextW, nextH)

	if h.Width() != nextW || h.Height() != nextH {
		t.Fatalf("TerminalWidth/Height = %dx%d, want %dx%d", h.Width(), h.Height(), nextW, nextH)
	}
	tw, th := h.Dimensions()
	if tw != nextW || th != nextH {
		t.Fatalf("TerminalDimensions = %dx%d, want %dx%d", tw, th, nextW, nextH)
	}
	b := h.Bounds()
	if b.Dx() != nextW || b.Dy() != nextH {
		t.Fatalf("Bounds() = (%d,%d), want (%d,%d)", b.Dx(), b.Dy(), nextW, nextH)
	}
}

func TestHarness_resizeSendsWindowSize(t *testing.T) {
	t.Parallel()
	h, r := newTerminalHarness(t, 11, 7)
	h.Resize(30, 12)
	waitWindowSize(t, r, 30, 12)
}

func TestHarness_terminalColors(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 5, 3)

	fg := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
	bg := color.NRGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}
	cr := color.NRGBA{R: 0xee, G: 0xdd, B: 0xcc, A: 0xff}

	got := h.
		SetFgColor(fg).
		SetBgColor(bg).
		SetCursorColor(cr)
	if got != h {
		t.Fatal("expected color setters to return the same Harness for chaining")
	}

	if !colorEqual(h.FgColor(), fg) {
		t.Errorf("TerminalFgColor = %v, want %#v", h.FgColor(), fg)
	}
	if !colorEqual(h.BgColor(), bg) {
		t.Errorf("TerminalBgColor = %v, want %#v", h.BgColor(), bg)
	}
	if !colorEqual(h.CursorColor(), cr) {
		t.Errorf("TerminalCursorColor = %v, want %#v", h.CursorColor(), cr)
	}

	dfg := color.NRGBA{R: 0x10, G: 0x20, B: 0x40, A: 0xff}
	dbg := color.NRGBA{R: 0x41, G: 0x42, B: 0x43, A: 0xff}
	dcr := color.NRGBA{R: 0xfe, G: 0xdc, B: 0xba, A: 0xff}

	h.SetDefaultFgColor(dfg)
	h.SetDefaultBgColor(dbg)
	h.SetDefaultCursorColor(dcr)

	h.SetFgColor(nil)
	h.SetBgColor(nil)
	h.SetCursorColor(nil)

	if !colorEqual(h.FgColor(), dfg) || !colorEqual(h.BgColor(), dbg) || !colorEqual(h.CursorColor(), dcr) {
		t.Fatalf("nil colors should revert to defaults: fg=%v bg=%v cur=%v", h.FgColor(), h.BgColor(), h.CursorColor())
	}
}

func TestHarness_terminalCursorPosition(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 8, 4)
	pt := h.CursorPosition()
	if pt.X != 0 || pt.Y != 0 {
		t.Fatalf("initial cursor (%d,%d), want (0,0)", pt.X, pt.Y)
	}
}

func TestHarness_terminalScrollbackCopyAndCount(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 5, 4)
	var rows strings.Builder
	rows.WriteString("start\n")
	for i := range 18 {
		fmt.Fprintf(&rows, "row-%d\n", i)
	}
	_ = writeString(h, rows.String())
	vtLen := h.session.Terminal().ScrollbackLen()
	if h.ScrollbackCount() != vtLen {
		t.Fatalf("TerminalScrollbackCount vs vt len: %d vs %d", h.ScrollbackCount(), vtLen)
	}
	if h.ScrollbackCount() == 0 {
		t.Fatal("expected non-zero scrollback after many newlines")
	}

	snap := h.Scrollback()
	if len(snap) != h.ScrollbackCount() {
		t.Fatalf("len(TerminalScrollback()) = %d, TerminalScrollbackCount = %d", len(snap), h.ScrollbackCount())
	}

	h.ClearScrollback()
	if h.ScrollbackCount() != 0 {
		t.Fatalf("after ClearTerminalScrollback, count = %d, want 0", h.ScrollbackCount())
	}
	if len(snap) == 0 {
		t.Fatal("snapshot should have had lines to verify copy retention")
	}
	if len(snap[0]) == 0 {
		t.Fatal("expected first snapshot line to have cells")
	}

	snap[0][0] = vt.Cell{}
	if h.ScrollbackCount() != 0 {
		t.Fatalf("after clearing scrollback, mutating snapshot line should leave count at 0, got %d", h.ScrollbackCount())
	}
}

func TestHarness_terminalSetScrollbackSize(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 4, 3)
	h.SetScrollbackSize(0)
	for i := range 20 {
		_, _ = fmt.Fprintf(h.session, "x%d\n", i)
	}
	if got := h.ScrollbackCount(); got != 0 {
		t.Fatalf("TerminalScrollbackCount = %d, want 0 with scrollback disabled", got)
	}
}

func TestHarness_terminalAltScreen(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 6, 3)
	if h.IsAltScreen() {
		t.Fatal("expected main screen initially")
	}
	_ = writeString(h, "\x1b[?1049h")
	if !h.IsAltScreen() {
		t.Fatal("expected alt screen after CSI ?1049h")
	}
	_ = writeString(h, "\x1b[?1049l")
	if h.IsAltScreen() {
		t.Fatal("expected main screen after leaving alt screen")
	}
}

func TestHarness_terminalFocusBlurMsgs(t *testing.T) {
	t.Parallel()
	h, r := newTerminalHarness(t, 4, 2)
	_ = writeString(h, ansi.SetModeFocusEvent)

	h.Focus()
	waitMsg[tea.FocusMsg](t, r, nil)

	h.Blur()
	waitMsg[tea.BlurMsg](t, r, nil)
}

func TestHarness_terminalTrackedHarnessFocus(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 4, 2)
	if !h.IsFocused() {
		t.Fatal("expected initially focused harness state")
	}
	h.Blur()
	if h.IsFocused() {
		t.Fatal("expected Blur() to record unfocused state")
	}
	h.Focus()
	if !h.IsFocused() {
		t.Fatal("expected Focus() to record focused state")
	}
}

func TestHarness_terminalTrackedCursorDefaults(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 4, 2)
	// NewHarness waits for the first paint, in which Bubble Tea hides the
	// cursor of a view without one.
	if h.CursorVisible() || h.CursorBlink() || h.CursorStyle() != vt.CursorBlock {
		t.Fatalf("defaults: visible=%v blink=%v style=%v", h.CursorVisible(), h.CursorBlink(), h.CursorStyle())
	}
	pt := h.CursorPosition()
	if pt.X != 0 || pt.Y != 0 {
		t.Fatalf("initial cursor %#v, want (0,0)", pt)
	}
}

func TestHarness_terminalTrackedTitle(t *testing.T) {
	t.Parallel()
	const want = "steep-tracked-title"
	h, _ := newTerminalHarness(t, 4, 2)
	_ = writeString(h, ansi.SetWindowTitle(want))
	if got := h.Title(); got != want {
		t.Fatalf("Title() = %q, want %q", got, want)
	}
}

func TestHarness_controlStrings(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 4, 2)
	before := len(h.ControlStrings())
	_ = writeString(h, "\x1b]2;bel\x07\x1b]2;st\x1b\\\x1bP1|unknown\x1b\\")
	got := h.ControlStrings()[before:]
	want := []struct {
		kind vt.SequenceKind
		data string
		end  vt.StringEnd
	}{
		{vt.SequenceOSC, "2;bel", vt.EndBEL},
		{vt.SequenceOSC, "2;st", vt.EndST},
		{vt.SequenceDCS, "1|unknown", vt.EndST},
	}
	if len(got) != len(want) {
		t.Fatalf("ControlStrings() = %+v, want %d strings", got, len(want))
	}
	for i, w := range want {
		if got[i].Kind != w.kind || got[i].Data != w.data || got[i].End != w.end {
			t.Errorf("string %d = %+v, want %+v", i, got[i], w)
		}
	}
	if title := h.Title(); title != "st" {
		t.Fatalf("Title() = %q, want %q", title, "st")
	}
}

func TestHarness_terminalTrackedBell(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 4, 2)
	if got := h.Bell(); got != (vt.Bell{}) {
		t.Fatalf("Bell() = %+v, want none before first bell", got)
	}

	_ = writeString(h, string(rune(ansi.BEL)))

	first := h.Bell()
	if first.Count != 1 {
		t.Fatalf("Bell().Count = %d after one BEL, want 1", first.Count)
	}

	_ = writeString(h, string(rune(ansi.BEL)))

	second := h.Bell()
	if second.Count != 2 || second.At < first.At {
		t.Fatalf("Bell() = %+v after %+v, want count 2 and no earlier", second, first)
	}
}

func TestHarness_terminalTrackedModeDEC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		seq     string
		mode    ansi.Mode
		want    ansi.ModeSetting
		cleanup string
	}{
		{
			name:    "mouse button reporting on then off",
			seq:     ansi.SetModeMouseButtonEvent,
			mode:    ansi.ModeMouseButtonEvent,
			want:    ansi.ModeSet,
			cleanup: ansi.ResetModeMouseButtonEvent,
		},
		{
			name:    "focus event on then off",
			seq:     ansi.SetModeFocusEvent,
			mode:    ansi.ModeFocusEvent,
			want:    ansi.ModeSet,
			cleanup: ansi.ResetModeFocusEvent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			local, _ := newTerminalHarness(t, 4, 2)
			_ = writeString(local, tt.seq)
			if got := local.ModeSetting(tt.mode); got != tt.want {
				t.Fatalf("after enable: ModeSetting(%v)=%v want %v", tt.mode, got, tt.want)
			}
			_ = writeString(local, tt.cleanup)
			if got := local.ModeSetting(tt.mode); got != ansi.ModeReset {
				t.Fatalf("after reset: ModeSetting(%v)=%v want Reset", tt.mode, got)
			}
		})
	}
}

func TestHarness_terminalTrackedModeUnsetKey(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 3, 2)
	unset := ansi.DECMode(0xbeef)
	if h.ModeSetting(unset) != ansi.ModeNotRecognized {
		t.Fatalf("unset mode setting = %v, want ModeNotRecognized", h.ModeSetting(unset))
	}
}

func TestHarness_terminalTrackedCursorHideShow(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 6, 2)
	_ = writeString(h, ansi.HideCursor)
	if h.CursorVisible() {
		t.Fatal("expected hidden cursor after HideCursor sequence")
	}
	_ = writeString(h, ansi.ShowCursor)
	if !h.CursorVisible() {
		t.Fatal("expected visible cursor after ShowCursor sequence")
	}
}

func TestHarness_terminalPasteMsg(t *testing.T) {
	t.Parallel()
	h, r := newTerminalHarness(t, 4, 2)
	_ = writeString(h, ansi.SetModeBracketedPaste)

	const pasted = "hello"
	h.Paste(pasted)

	got := waitMsg(t, r, func(m tea.PasteMsg) bool {
		return m.Content == pasted
	})
	if got.Content != pasted {
		t.Fatalf("PasteMsg = %#v, want Content %q", got, pasted)
	}
}

func TestHarness_TerminalType_deliversPrintableRunesThroughEmulator(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithWindowSize(80, 3))
	h.Type("ab ").Wait("text=ab").Require("text=ab")
}

func TestHarness_TerminalKey_accumulatesPrintableText(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithWindowSize(80, 3))
	h.Key("a").Key("b").Key("space").Wait("text=ab").Require("text=ab")
}

func TestHarness_TerminalTypingDelay_env(t *testing.T) {
	t.Setenv("STEEP_TYPING_DELAY", "20ms")
	const delay = 20 * time.Millisecond
	h := NewHarness(t, rootTestModel{}, WithWindowSize(80, 3))
	h.Type("ab").Key("space").Type("cd", reel.Delay(0)).Wait("text=ab cd")

	inputs := inputEvents(h)
	var got []string
	for _, in := range inputs {
		got = append(got, string(in.Data))
	}
	if want := []string{"a", "b", " ", "cd"}; !slices.Equal(got, want) {
		t.Fatalf("got inputs %q, want %q", got, want)
	}
	assertInputGaps(t, inputs, delay)
	findNote(t, notes(t, h.Timeline()), kindStart, map[string]any{"typing_ms": 20})
}

func TestHarness_TerminalTypingDelay_perCall(t *testing.T) {
	t.Parallel()
	const delay = 20 * time.Millisecond
	h := NewHarness(t, rootTestModel{}, WithWindowSize(80, 3), WithIgnoreEnv(true))
	h.Type("ab", reel.Delay(delay)).KeyBackspace(reel.Delay(delay)).Wait("text=a")

	inputs := inputEvents(h)
	if len(inputs) != 3 {
		t.Fatalf("got %d inputs, want one per key", len(inputs))
	}
	assertInputGaps(t, inputs, delay)
}

// inputEvents returns the [reel.Input] events in h's timeline.
func inputEvents(h *Harness) []reel.Input {
	var inputs []reel.Input
	for ev := range h.Timeline().Events() {
		if in, ok := ev.(reel.Input); ok {
			inputs = append(inputs, in)
		}
	}
	return inputs
}

// assertInputGaps checks that each input came at least d after the one
// before.
func assertInputGaps(t *testing.T, inputs []reel.Input, d time.Duration) {
	t.Helper()
	for i := 1; i < len(inputs); i++ {
		if gap := inputs[i].At() - inputs[i-1].At(); gap < d {
			t.Fatalf("input %d came %v after the one before, want at least %v", i, gap, d)
		}
	}
}

func enableEmulatorMouseReporting(tb testing.TB, h *Harness) {
	tb.Helper()
	_ = writeString(h, ansi.SetModeMouseButtonEvent)
	_ = writeString(h, ansi.SetModeMouseExtSgr)
}

// mouseStep is a normalized mouse line from [tea] for ordered assertions.
type mouseStep struct {
	kind string // "click", "motion", "release", or "wheel"
	tea.Mouse
}

func collectMouseTail(tb testing.TB, r *recorder, nBefore int) []mouseStep {
	tb.Helper()
	all := r.all()
	if nBefore > len(all) {
		tb.Fatalf("nBefore=%d longer than history len=%d", nBefore, len(all))
	}
	return mouseTail(all[nBefore:])
}

// mouseTail returns the mouse messages of msgs as steps.
func mouseTail(msgs []uv.Event) []mouseStep {
	var out []mouseStep
	for _, msg := range msgs {
		switch m := msg.(type) {
		case tea.MouseClickMsg:
			out = append(out, mouseStep{"click", m.Mouse()})
		case tea.MouseMotionMsg:
			out = append(out, mouseStep{"motion", m.Mouse()})
		case tea.MouseReleaseMsg:
			out = append(out, mouseStep{"release", m.Mouse()})
		case tea.MouseWheelMsg:
			out = append(out, mouseStep{"wheel", m.Mouse()})
		}
	}
	return out
}

// waitMouseTail waits for want mouse messages after the first nBefore, and
// then for no more to come for a moment, and returns those after nBefore.
func waitMouseTail(tb testing.TB, r *recorder, nBefore, want int) []mouseStep {
	tb.Helper()
	deadline := time.After(recorderTimeout)
	for {
		msgs, changed := r.history()
		if len(mouseTail(msgs[min(nBefore, len(msgs)):])) >= want {
			break
		}
		select {
		case <-changed:
		case <-deadline:
			tb.Fatalf("fewer than %d mouse messages after %s, got:\n%s", want, recorderTimeout, describeMsgs(msgs[min(nBefore, len(msgs)):]))
		}
	}
	r.settle(tb, recorderQuiet)
	return collectMouseTail(tb, r, nBefore)
}

func TestHarness_TerminalMouse_deliversClick(t *testing.T) {
	t.Parallel()
	h, r := newTerminalHarness(t, 14, 5)

	enableEmulatorMouseReporting(t, h)
	n := r.count()

	h.Mouse(vt.MousePress, vt.MouseLeft, 4, 2)
	got := waitMouseTail(t, r, n, 1)
	if len(got) != 1 || got[0].kind != "click" || got[0].X != 4 || got[0].Y != 2 || got[0].Button != tea.MouseLeft {
		t.Fatalf("unexpected mouse tail: %#v", got)
	}
}

func TestHarness_TerminalMouseClick_deliversPressAndRelease(t *testing.T) {
	t.Parallel()
	h, r := newTerminalHarness(t, 12, 4)
	enableEmulatorMouseReporting(t, h)
	n := r.count()

	h.MouseClick(vt.MouseLeft, 3, 2)
	got := waitMouseTail(t, r, n, 2)
	want := []mouseStep{
		{"click", tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}},
		{"release", tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d mouse msgs, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].kind != want[i].kind || got[i].X != want[i].X || got[i].Y != want[i].Y || got[i].Button != want[i].Button {
			t.Fatalf("step %d: got %#v, want kind=%s Mouse=%+v", i, got[i], want[i].kind, want[i].Mouse)
		}
	}
}

func TestHarness_TerminalLeftMiddleRightClick_buttons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fn   func(*Harness, int, int, ...reel.InputOption) *Harness
		want tea.MouseButton
	}{
		{name: "left", fn: (*Harness).LeftClick, want: tea.MouseLeft},
		{name: "middle", fn: (*Harness).MiddleClick, want: tea.MouseMiddle},
		{name: "right", fn: (*Harness).RightClick, want: tea.MouseRight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, r := newTerminalHarness(t, 10, 5)
			enableEmulatorMouseReporting(t, h)
			n := r.count()

			tt.fn(h, 2, 1)
			got := waitMouseTail(t, r, n, 2)
			if len(got) != 2 || got[0].Button != tt.want || got[1].Button != tt.want {
				t.Fatalf("got %#v, want click+release with button %v", got, tt.want)
			}
		})
	}
}

func TestHarness_TerminalMouseDrag_sequence(t *testing.T) {
	t.Parallel()
	h, r := newTerminalHarness(t, 16, 6)
	enableEmulatorMouseReporting(t, h)
	n := r.count()

	// (1,1) -> (3,1): press, motion at (2,1), motion at (3,1), release.
	h.MouseDrag(vt.MouseLeft, 1, 1, 3, 1)
	got := waitMouseTail(t, r, n, 4)
	want := []mouseStep{
		{"click", tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}},
		{"motion", tea.Mouse{X: 2, Y: 1, Button: tea.MouseLeft}},
		{"motion", tea.Mouse{X: 3, Y: 1, Button: tea.MouseLeft}},
		{"release", tea.Mouse{X: 3, Y: 1, Button: tea.MouseLeft}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].kind != want[i].kind || got[i].X != want[i].X || got[i].Y != want[i].Y || got[i].Button != want[i].Button {
			t.Fatalf("step %d: got %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestHarness_TerminalLeftDrag_equivalentToMouseDragLeft(t *testing.T) {
	t.Parallel()
	h, r := newTerminalHarness(t, 12, 4)
	enableEmulatorMouseReporting(t, h)

	n0 := r.count()
	h.MouseDrag(vt.MouseLeft, 0, 0, 1, 0)
	a := waitMouseTail(t, r, n0, 3)

	n1 := r.count()
	h.LeftDrag(0, 0, 1, 0)
	b := waitMouseTail(t, r, n1, 3)

	if len(a) != len(b) {
		t.Fatalf("TerminalMouseDrag len=%d vs TerminalLeftDrag len=%d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("step %d: mouse drag %#v != left drag %#v", i, a[i], b[i])
		}
	}
}

func TestHarness_TerminalMouseDrag_sameCellIsClickAndRelease(t *testing.T) {
	t.Parallel()
	h, r := newTerminalHarness(t, 8, 4)
	enableEmulatorMouseReporting(t, h)
	n := r.count()

	h.MouseDrag(vt.MouseRight, 2, 2, 2, 2)
	got := waitMouseTail(t, r, n, 2)
	if len(got) != 2 || got[0].kind != "click" || got[1].kind != "release" {
		t.Fatalf("want click+release only, got %#v", got)
	}
	if got[0].Button != tea.MouseRight || got[1].Button != tea.MouseRight {
		t.Fatalf("buttons: got (%v, %v), want right", got[0].Button, got[1].Button)
	}
}

func TestHarness_TerminalMouse_ignoredWithoutMouseModes(t *testing.T) {
	t.Parallel()
	h, r := newTerminalHarness(t, 8, 4)
	n := r.count()
	h.LeftClick(2, 2)
	r.settle(t, recorderQuiet)
	for _, msg := range r.all()[n:] {
		switch msg.(type) {
		case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg:
			t.Fatalf("unexpected mouse message without DEC mouse modes: %T", msg)
		default:
		}
	}
}

func osc7501(body string) string { return "\x1b]7501;" + body + "\x1b\\" }

// writeString writes s to the harness's session as if the program had.
func writeString(h *Harness, s string) error {
	_, err := h.session.Write([]byte(s))
	return err
}

func writeVT(t *testing.T, h *Harness, s string) {
	t.Helper()
	if err := writeString(h, s); err != nil {
		t.Fatalf("write vt: %v", err)
	}
}

func TestProgramStatusProtocol(t *testing.T) {
	t.Parallel()
	h, r := newTerminalHarness(t, 8, 2)
	before := h.session.Terminal().Text()

	stream := osc7501("?") + osc7501("state=blocked:kind=question:progress=42:app=agent:id=job:title=VGl0bGU=:msg=SGVsbG8=")
	for i := range len(stream) {
		if err := writeString(h, stream[i:i+1]); err != nil {
			t.Fatalf("write vt: %v", err)
		}
	}
	after := h.session.Terminal().Text()

	if before != after {
		t.Fatal("program status changed the screen")
	}
	waitMsg[tea.ProgramStatusSupportMsg](t, r, nil)

	got := h.ProgramStatuses()
	if len(got) != 1 {
		t.Fatalf("records = %+v", got)
	}
	want := vt.ProgramStatus{
		State: vt.ProgramBlocked, Kind: vt.ProgramKindQuestion,
		Progress: 42, App: "agent", ID: "job",
		Title: "Title", Message: "Hello",
	}
	if got[0] != want {
		t.Fatalf("record = %+v, want %+v", got[0], want)
	}

	cloned := h.ProgramStatuses()
	cloned[0].State = vt.ProgramError
	writeVT(t, h, osc7501("state=done:id=job"))
	got = h.ProgramStatuses()
	want = vt.ProgramStatus{State: vt.ProgramDone, ID: "job", Progress: -1}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("replacement retained fields: %+v", got)
	}

	for _, bad := range []string{
		"state=unknown:id=job",
		"state=error:id=job:msg=A",
		"state=error:id=job:msg=Gwo=",
		"state=error:id=/",
		"state=error:id=job:msg=" + strings.Repeat("A", 2800),
	} {
		writeVT(t, h, osc7501(bad))
		got = h.ProgramStatuses()
		if len(got) != 1 || got[0] != want {
			t.Fatalf("invalid report applied: %q: %+v", bad, got)
		}
	}
}

func TestProgramStatusLifecycle(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 8, 2)
	writeVT(t, h, osc7501("state=idle:app=root")+
		osc7501("state=working:id=build:app=compiler")+
		osc7501("state=blocked:id=build/test")+
		osc7501("state=done:id=build2"))
	got := h.ProgramStatuses()
	if len(got) != 4 || got[2].App != "compiler" || got[3].App != "root" {
		t.Fatalf("inheritance: %+v", got)
	}
	writeVT(t, h, osc7501("state=clear:id=build"))
	got = h.ProgramStatuses()
	if len(got) != 2 || got[1].ID != "build2" {
		t.Fatalf("subtree clear: %+v", got)
	}

	writeVT(t, h, osc7501("state=working:id=w")+osc7501("state=blocked:id=b")+osc7501("state=error:id=e"))
	writeVT(t, h, "\x1b]133;A\x1b\\")
	got = h.ProgramStatuses()
	if len(got) != 3 || got[2].State != vt.ProgramError {
		t.Fatalf("prompt: %+v", got)
	}

	writeVT(t, h, "\x1b[?1049h\x1b[?1049l\x1b[!p")
	if len(h.ProgramStatuses()) != 3 {
		t.Fatal("screen switch or soft reset cleared status")
	}
	writeVT(t, h, "\x1bc")
	if len(h.ProgramStatuses()) != 0 {
		t.Fatal("RIS retained status")
	}
	writeVT(t, h, osc7501("state=done:id=x")+osc7501("state=clear:msg=A"))
	if len(h.ProgramStatuses()) != 1 {
		t.Fatal("invalid clear removed records")
	}
	writeVT(t, h, osc7501("state=clear"))
	if len(h.ProgramStatuses()) != 0 {
		t.Fatal("root clear retained status")
	}
}

// programStatusMaxRecords is how many OSC 7501 records the terminal keeps.
const programStatusMaxRecords = 256

func TestProgramStatusBoundedEviction(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 4, 1)
	var b strings.Builder
	for i := range programStatusMaxRecords {
		fmt.Fprintf(&b, osc7501("state=working:id=j%03d"), i)
	}
	b.WriteString(osc7501("state=done:id=j000"))
	b.WriteString(osc7501("state=idle:id=new"))
	writeVT(t, h, b.String())

	got := h.ProgramStatuses()
	if len(got) != programStatusMaxRecords {
		t.Fatalf("records = %d, want %d", len(got), programStatusMaxRecords)
	}
	for _, s := range got {
		if s.ID == "j001" {
			t.Fatal("least recently updated record survived")
		}
	}
	var updated vt.ProgramStatus
	for _, s := range got {
		if s.ID == "j000" {
			updated = s
		}
	}
	if updated.State != vt.ProgramDone {
		t.Fatalf("updated record = %+v", updated)
	}
}

func TestProgramStatusInSnapshot(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 4, 1)
	writeVT(t, h, osc7501("state=working:id=job:progress=0:app=demo"))
	snap := h.session.Screen()
	if len(snap.ProgramStatus) != 1 || snap.ProgramStatus[0].State != "working" {
		t.Fatalf("snapshot status = %+v", snap.ProgramStatus)
	}
	if snap.ProgramStatus[0].Progress == nil || *snap.ProgramStatus[0].Progress != 0 {
		t.Fatalf("progress = %v, want 0", snap.ProgramStatus[0].Progress)
	}
}

func TestHarness_terminalScrollViewport(t *testing.T) {
	t.Parallel()
	h, _ := newTerminalHarness(t, 6, 3)
	var rows strings.Builder
	for i := range 12 {
		fmt.Fprintf(&rows, "row-%d\r\n", i)
	}
	writeVT(t, h, rows.String())
	live := h.View()

	h.ScrollUp(2)
	if got := h.ScrollbackOffset(); got != 2 {
		t.Fatalf("ScrollbackOffset = %d, want 2", got)
	}
	if h.View() == live {
		t.Fatalf("View did not change after ScrollUp: %q", live)
	}

	h.ScrollToTop()
	if got, want := h.ScrollbackOffset(), h.ScrollbackCount(); got != want {
		t.Fatalf("after ScrollToTop, offset = %d, want %d", got, want)
	}
	if !strings.Contains(h.View(), "row-0") {
		t.Fatalf("View at the top = %q, want the first row", h.View())
	}

	h.ScrollDown(1)
	if got, want := h.ScrollbackOffset(), h.ScrollbackCount()-1; got != want {
		t.Fatalf("after ScrollDown(1), offset = %d, want %d", got, want)
	}

	h.ScrollToBottom()
	if got := h.ScrollbackOffset(); got != 0 {
		t.Fatalf("after ScrollToBottom, offset = %d, want 0", got)
	}
	if got := h.View(); got != live {
		t.Fatalf("View = %q, want the live screen %q", got, live)
	}
}

func TestHarness_terminalScrollOnInput(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{true, false} {
		h := NewHarness(t, terminalProbe{}, WithWindowSize(6, 3), WithScrollOnInput(enabled))
		var rows strings.Builder
		for i := range 12 {
			fmt.Fprintf(&rows, "row-%d\r\n", i)
		}
		writeVT(t, h, rows.String())
		h.ScrollUp(4).Type("a")
		want := 0
		if !enabled {
			want = 4
		}
		if got := h.ScrollbackOffset(); got != want {
			t.Fatalf("scroll on input %v: offset = %d, want %d", enabled, got, want)
		}
	}
}

func TestHarness_TerminalKey_mapsNamedKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		key      string
		wantCode rune
		wantMod  tea.KeyMod
		wantText string
	}{
		{name: "single ASCII rune", key: "q", wantCode: 'q', wantMod: 0, wantText: "q"},
		{name: "space", key: "space", wantCode: ' ', wantMod: 0, wantText: " "},
		{name: "enter", key: "enter", wantCode: tea.KeyEnter},
		{name: "tab", key: "tab", wantCode: tea.KeyTab},
		{name: "escape alias esc", key: "esc", wantCode: tea.KeyEscape},
		{name: "escape alias escape", key: "escape", wantCode: tea.KeyEscape},
		{name: "backspace", key: "backspace", wantCode: tea.KeyBackspace},
		{name: "delete", key: "delete", wantCode: tea.KeyDelete},
		{name: "arrow up", key: "up", wantCode: tea.KeyUp},
		{name: "arrow down", key: "down", wantCode: tea.KeyDown},
		{name: "arrow left", key: "left", wantCode: tea.KeyLeft},
		{name: "arrow right", key: "right", wantCode: tea.KeyRight},
		{name: "home", key: "home", wantCode: tea.KeyHome},
		{name: "end", key: "end", wantCode: tea.KeyEnd},
		{name: "pgup", key: "pgup", wantCode: tea.KeyPgUp},
		{name: "pageup alias", key: "pageup", wantCode: tea.KeyPgUp},
		{name: "pgdown", key: "pgdown", wantCode: tea.KeyPgDown},
		{name: "pagedown alias", key: "pagedown", wantCode: tea.KeyPgDown},
		{name: "shift+tab", key: "shift+tab", wantCode: tea.KeyTab, wantMod: tea.ModShift},
		{name: "ctrl lowercase letter", key: "ctrl+g", wantCode: 'g', wantMod: tea.ModCtrl},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := record(rootTestModel{})
			h := NewHarness(t, r, WithWindowSize(80, 3))
			h.Key(tt.key)
			waitMsg(t, r, func(km tea.KeyPressMsg) bool {
				k := km.Key()
				if k.Code != tt.wantCode || k.Mod != tt.wantMod {
					return false
				}
				return tt.wantText == "" || k.Text == tt.wantText
			})
		})
	}
}
