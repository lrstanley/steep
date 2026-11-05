// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/steep/reel"
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
// has drawn view.
func newLocateHarness(tb testing.TB, view string) *Harness {
	tb.Helper()
	h := NewHarness(tb, locateProbe{view: view}, WithWindowSize(20, 4))
	h.WaitMessage[tea.WindowSizeMsg]()
	enableEmulatorMouseReporting(tb, h)
	return h
}

func TestHarness_Locate_clicksWhereConditionMatched(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`\[ \w+ \]`)
	tests := map[string]struct {
		view string
		loc  func(*Harness) Location
		x, y int
	}{
		"text":         {"\n  [ OK ]", func(h *Harness) Location { return h.Locate(reel.Contains("OK")) }, 4, 1},
		"regexp":       {"\n  [ OK ]", func(h *Harness) Location { return h.Locate(reel.Matches(re)) }, 2, 1},
		"after wide":   {"世界 OK", func(h *Harness) Location { return h.Locate(reel.Contains("OK")) }, 5, 0},
		"first match":  {"OK\nOK", func(h *Harness) Location { return h.Locate(reel.Contains("OK")) }, 0, 0},
		"offset":       {"Name:\n", func(h *Harness) Location { return h.Locate(reel.Contains("Name:")).Offset(6, 1) }, 6, 1},
		"offset back":  {"\n  OK", func(h *Harness) Location { return h.Locate(reel.Contains("OK")).Offset(-2, -1) }, 0, 0},
		"offset chain": {"OK", func(h *Harness) Location { return h.Locate(reel.Contains("OK")).Offset(1, 0).Offset(1, 2) }, 2, 2},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newLocateHarness(t, tt.view)
			n := len(slices.Collect(h.MessageHistory()))

			tt.loc(h).LeftClick()
			WaitSettleMessages(t, h)

			got := collectMouseTail(t, h, n)
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
			h := newLocateHarness(t, "OK")
			n := len(slices.Collect(h.MessageHistory()))

			tt.fn(h.Locate(reel.Contains("OK")), reel.Repeat(2))
			WaitSettleMessages(t, h)

			got := collectMouseTail(t, h, n)
			if len(got) != 4 || got[0].Button != tt.want {
				t.Fatalf("got %#v, want two clicks with button %v", got, tt.want)
			}
		})
	}
}

func TestHarness_Locate_waits(t *testing.T) {
	t.Parallel()
	h := newLocateHarness(t, "loading")
	time.AfterFunc(20*time.Millisecond, func() { h.SendProgram(setTextMsg("  ready")) })

	loc := h.Locate(reel.Contains("ready"))
	if loc.X != 2 || loc.Y != 0 {
		t.Fatalf("located (%d, %d), want (2, 0)", loc.X, loc.Y)
	}
	loc.LeftClick().Require("ready")
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
			"off the 20x4 screen",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tb := &fakeTB{TB: t}
			h := newLocateHarness(tb, "OK")
			n := len(slices.Collect(h.MessageHistory()))

			runFailing(func() { tt.fn(h) })

			errs := strings.Join(tb.errors(), "\n")
			if !strings.Contains(errs, tt.want) {
				t.Fatalf("errors %q, want %q", errs, tt.want)
			}
			if got := collectMouseTail(t, h, n); len(got) != 0 {
				t.Fatalf("sent mouse messages: %#v", got)
			}
		})
	}
}
