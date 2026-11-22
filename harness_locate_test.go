// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"regexp"
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

// locateProbe draws its view, which [setTextMsg] replaces.
type locateProbe struct{ view string }

func (m locateProbe) Init() tea.Cmd { return nil }

func (m locateProbe) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	if text, ok := msg.(setTextMsg); ok {
		m.view = string(text)
	}
	return m, nil
}

func (m locateProbe) View() tea.View { return tea.NewView(m.view) }

// newLocateHarness returns a harness over tb, with mouse reporting on, that
// has drawn view, and the recorder of its messages.
func newLocateHarness(tb testing.TB, view string) (*Harness, *recorder) {
	tb.Helper()
	r := record(locateProbe{view: view})
	h := NewHarness(tb, r, WithWindowSize(20, 4))
	enableEmulatorMouseReporting(tb, h)
	return h, r
}

func TestHarness_Locate_clicksWhereConditionMatched(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`\[ \w+ \]`)
	tests := map[string]struct {
		view string
		loc  func(*Harness) Location
		x, y int
	}{
		"text":        {"\n  [ OK ]", func(h *Harness) Location { return h.Locate(reel.Contains("OK")) }, 4, 1},
		"regexp":      {"\n  [ OK ]", func(h *Harness) Location { return h.Locate(reel.Matches(re)) }, 2, 1},
		"after wide":  {"世界 OK", func(h *Harness) Location { return h.Locate(reel.Contains("OK")) }, 5, 0},
		"first match": {"OK\nOK", func(h *Harness) Location { return h.Locate(reel.Contains("OK")) }, 0, 0},
		"nth match":   {"OK\nOK", func(h *Harness) Location { return h.Locate(reel.Nth(2, reel.Contains("OK"))) }, 0, 1},
		"last match":  {"OK OK\nOK", func(h *Harness) Location { return h.Locate(reel.Nth(-1, reel.Contains("OK"))) }, 0, 1},
		"after": {"OK\nA  OK\nB OK", func(h *Harness) Location {
			return h.Locate(reel.After(reel.Contains("A"), reel.Contains("OK")))
		}, 3, 1},
		"below": {"A OK\nB OK", func(h *Harness) Location {
			return h.Locate(reel.Below(reel.Contains("A"), reel.Contains("OK")))
		}, 2, 1},
		"offset":       {"Name:\n", func(h *Harness) Location { return h.Locate(reel.Contains("Name:")).Offset(6, 1) }, 6, 1},
		"offset back":  {"\n  OK", func(h *Harness) Location { return h.Locate(reel.Contains("OK")).Offset(-2, -1) }, 0, 0},
		"offset chain": {"OK", func(h *Harness) Location { return h.Locate(reel.Contains("OK")).Offset(1, 0).Offset(1, 2) }, 2, 2},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, r := newLocateHarness(t, tt.view)
			n := r.count()

			tt.loc(h).LeftClick()
			got := waitMouseTail(t, r, n, 2)
			want := tea.Mouse{X: tt.x, Y: tt.y, Button: tea.MouseLeft}
			if len(got) != 2 || got[0].kind != "click" || got[0].Mouse != want || got[1].kind != "release" {
				t.Fatalf("got %#v, want click+release of %+v", got, want)
			}
		})
	}
}

func TestHarness_Locate_buttons(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		fn   func(Location, ...reel.InputOption) *Harness
		want tea.MouseButton
	}{
		"left":   {Location.LeftClick, tea.MouseLeft},
		"middle": {Location.MiddleClick, tea.MouseMiddle},
		"right":  {Location.RightClick, tea.MouseRight},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, r := newLocateHarness(t, "OK")
			n := r.count()

			tt.fn(h.Locate(reel.Contains("OK")), reel.Repeat(2))
			got := waitMouseTail(t, r, n, 4)
			if len(got) != 4 || got[0].Button != tt.want {
				t.Fatalf("got %#v, want two clicks with button %v", got, tt.want)
			}
		})
	}
}

// mouseSteps is a compact form of steps for test failures.
func mouseSteps(steps []mouseStep) string {
	var b strings.Builder
	for _, st := range steps {
		fmt.Fprintf(&b, "%s %v (%d, %d); ", st.kind, st.Button, st.X, st.Y)
	}
	return b.String()
}

func TestHarness_Locate_hover(t *testing.T) {
	t.Parallel()
	h, r := newLocateHarness(t, "\n  [ OK ]")
	// Motion with no button held is only reported with any-event tracking.
	_ = writeString(h, ansi.SetModeMouseAnyEvent)
	n := r.count()

	h.Locate(reel.Contains("OK")).Hover()
	got := waitMouseTail(t, r, n, 1)
	want := mouseStep{"motion", tea.Mouse{X: 4, Y: 1, Button: tea.MouseNone}}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %s, want %s", mouseSteps(got), mouseSteps([]mouseStep{want}))
	}
}

func TestHarness_Locate_drag(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		view string
		drag func(*Harness) *Harness
		want []mouseStep
	}{
		"offset": {"\n  [ OK ]", func(h *Harness) *Harness {
			at := h.Locate(reel.Contains("OK"))
			return at.LeftDrag(at.Offset(2, 1))
		}, []mouseStep{
			{"click", tea.Mouse{X: 4, Y: 1, Button: tea.MouseLeft}},
			{"motion", tea.Mouse{X: 5, Y: 2, Button: tea.MouseLeft}},
			{"motion", tea.Mouse{X: 6, Y: 2, Button: tea.MouseLeft}},
			{"release", tea.Mouse{X: 6, Y: 2, Button: tea.MouseLeft}},
		}},
		"onto another": {"card\n   bin", func(h *Harness) *Harness {
			return h.Locate(reel.Contains("card")).MouseDrag(vt.MouseRight, h.Locate(reel.Contains("bin")))
		}, []mouseStep{
			{"click", tea.Mouse{X: 0, Y: 0, Button: tea.MouseRight}},
			{"motion", tea.Mouse{X: 1, Y: 0, Button: tea.MouseRight}},
			{"motion", tea.Mouse{X: 2, Y: 1, Button: tea.MouseRight}},
			{"motion", tea.Mouse{X: 3, Y: 1, Button: tea.MouseRight}},
			{"release", tea.Mouse{X: 3, Y: 1, Button: tea.MouseRight}},
		}},
		"backward": {"  OK", func(h *Harness) *Harness {
			at := h.Locate(reel.Contains("OK"))
			return at.LeftDrag(at.Offset(-2, 0))
		}, []mouseStep{
			{"click", tea.Mouse{X: 2, Y: 0, Button: tea.MouseLeft}},
			{"motion", tea.Mouse{X: 1, Y: 0, Button: tea.MouseLeft}},
			{"motion", tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}},
			{"release", tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}},
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, r := newLocateHarness(t, tt.view)
			n := r.count()

			tt.drag(h)

			if got := waitMouseTail(t, r, n, len(tt.want)); !slices.Equal(got, tt.want) {
				t.Fatalf("got %s\nwant %s", mouseSteps(got), mouseSteps(tt.want))
			}
		})
	}
}

func TestHarness_Locate_wheel(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		button vt.MouseButton
		opts   []reel.InputOption
		want   tea.MouseButton
		count  int
	}{
		"down":     {vt.MouseWheelDown, nil, tea.MouseWheelDown, 1},
		"up":       {vt.MouseWheelUp, []reel.InputOption{reel.Repeat(3)}, tea.MouseWheelUp, 3},
		"left":     {vt.MouseWheelLeft, nil, tea.MouseWheelLeft, 1},
		"right":    {vt.MouseWheelRight, nil, tea.MouseWheelRight, 1},
		"repeated": {vt.MouseWheelDown, []reel.InputOption{reel.Repeat(40)}, tea.MouseWheelDown, 40},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, r := newLocateHarness(t, "\n  [ OK ]")
			n := r.count()

			h.Locate(reel.Contains("OK")).Wheel(tt.button, tt.opts...)
			got := waitMouseTail(t, r, n, tt.count)
			want := slices.Repeat([]mouseStep{{"wheel", tea.Mouse{X: 4, Y: 1, Button: tt.want}}}, tt.count)
			if !slices.Equal(got, want) {
				t.Fatalf("got %s\nwant %d of %s", mouseSteps(got), tt.count, mouseSteps(want[:1]))
			}
		})
	}
}

func TestHarness_Locate_waits(t *testing.T) {
	t.Parallel()
	h, _ := newLocateHarness(t, "loading")
	time.AfterFunc(20*time.Millisecond, func() { h.SendProgram(setTextMsg("  ready")) })

	loc := h.Locate(reel.Contains("ready"))
	if loc.X != 2 || loc.Y != 0 {
		t.Fatalf("located (%d, %d), want (2, 0)", loc.X, loc.Y)
	}
	loc.LeftClick().Require("ready")
}

// A wait for the nth match waits for it to be drawn, not just the first.
func TestHarness_Locate_waitsForNth(t *testing.T) {
	t.Parallel()
	h, _ := newLocateHarness(t, "revoke")
	time.AfterFunc(20*time.Millisecond, func() { h.SendProgram(setTextMsg("revoke\n  revoke")) })

	loc := h.Locate(reel.Nth(2, reel.Contains("revoke")))
	if loc.X != 2 || loc.Y != 1 {
		t.Fatalf("located (%d, %d), want (2, 1)", loc.X, loc.Y)
	}
}

func TestHarness_Locate_fails(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		fn   func(*Harness)
		want string
	}{
		"timeout": {
			func(h *Harness) { h.Locate(reel.Contains("missing"), WithTimeout(10*time.Millisecond)) },
			"missing",
		},
		"no nth": {
			func(h *Harness) { h.Locate(reel.Nth(2, reel.Contains("OK")), WithTimeout(10*time.Millisecond)) },
			`2nd match of contains ["OK"]`,
		},
		"locates nothing": {
			func(h *Harness) { h.Locate(reel.NotContains("missing")) },
			"locates nothing",
		},
		"off the left": {
			func(h *Harness) { h.Locate(reel.Contains("OK")).Offset(-1, 0).LeftClick() },
			"off the 20x4 screen",
		},
		"off the top": {
			func(h *Harness) { h.Locate(reel.Contains("OK")).Offset(0, -1).LeftClick() },
			"off the 20x4 screen",
		},
		"off the right": {
			func(h *Harness) { h.Locate(reel.Contains("OK")).Offset(20, 0).LeftClick() },
			"off the 20x4 screen",
		},
		"off the bottom": {
			func(h *Harness) { h.Locate(reel.Contains("OK")).Offset(0, 4).LeftClick() },
			"click at (0, 4) is off the 20x4 screen",
		},
		"hover off": {
			func(h *Harness) { h.Locate(reel.Contains("OK")).Offset(-1, 0).Hover() },
			"hover at (-1, 0) is off the 20x4 screen",
		},
		"drag from off": {
			func(h *Harness) {
				at := h.Locate(reel.Contains("OK"))
				at.Offset(0, -1).LeftDrag(at)
			},
			"drag from (0, -1) is off the 20x4 screen",
		},
		"drag to off": {
			func(h *Harness) {
				at := h.Locate(reel.Contains("OK"))
				at.MouseDrag(vt.MouseRight, at.Offset(20, 0))
			},
			"drag to (20, 0) is off the 20x4 screen",
		},
		"wheel off": {
			func(h *Harness) { h.Locate(reel.Contains("OK")).Offset(0, 4).Wheel(vt.MouseWheelDown) },
			"wheel at (0, 4) is off the 20x4 screen",
		},
		"not a wheel button": {
			func(h *Harness) { h.Locate(reel.Contains("OK")).Wheel(vt.MouseLeft) },
			"not a wheel button",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tb := &fakeTB{TB: t}
			h, r := newLocateHarness(tb, "OK")
			n := r.count()

			runFailing(func() { tt.fn(h) })

			errs := strings.Join(tb.errors(), "\n")
			if !strings.Contains(errs, tt.want) {
				t.Fatalf("errors %q, want %q", errs, tt.want)
			}
			if got := collectMouseTail(t, r, n); len(got) != 0 {
				t.Fatalf("sent mouse messages: %#v", got)
			}
		})
	}
}
