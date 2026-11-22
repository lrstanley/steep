// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/snapshot"
	uv "github.com/charmbracelet/ultraviolet"
)

type appendMsg string

type mutableViewModel struct {
	text string
}

func (m *mutableViewModel) View() string {
	return "text=" + m.text
}

func (m *mutableViewModel) Update(msg uv.Event) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.text += msg.Key().Text
	case appendMsg:
		m.text += string(msg)
		if msg == "?" {
			return func() uv.Event {
				return appendMsg("!")
			}
		}
	}
	return nil
}

type replacementViewModel struct {
	text string
}

func (m replacementViewModel) View() string {
	return "text=" + m.text
}

func (m replacementViewModel) Update(msg uv.Event) (replacementViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.text += msg.Key().Text
	case appendMsg:
		m.text += string(msg)
	}
	return m, nil
}

func TestComponentHarnessMutableUpdate(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{}, WithWindowSize(80, 1))

	h.Type("ab").Wait([]byte("text=ab")).Require("text=ab")

	h.SendProgram(appendMsg("?")).Wait("text=ab?!").WaitNot([]byte("missing")).
		Require("text=ab?!").RequireNot("missing").
		RequireWidth(80).RequireHeight(1).RequireDimensions(80, 1)
}

func TestComponentHarnessReplacementUpdate(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, replacementViewModel{}, WithWindowSize(80, 1))

	// Typed input and a sent message take different paths, so the input must
	// be on the screen before the message is sent.
	h.Type("go").Wait("text=go").SendProgram(appendMsg("!")).WaitFor(reel.ScreenFunc(func(text string) bool {
		return strings.Contains(text, "text=go!")
	}))
}

type sizeViewModel struct {
	width  int
	height int
}

func (m *sizeViewModel) View() string {
	return fmt.Sprintf("size=%dx%d", m.width, m.height)
}

func (m *sizeViewModel) Update(msg uv.Event) tea.Cmd {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
	}
	return nil
}

func TestComponentHarnessInitialSize(t *testing.T) {
	t.Parallel()
	t.Run("uses program default size", func(t *testing.T) {
		t.Parallel()
		h := NewComponentHarness(t, &sizeViewModel{})

		h.Wait("size=80x24")
	})

	t.Run("explicit size", func(t *testing.T) {
		t.Parallel()
		h := NewComponentHarness(t, &sizeViewModel{}, WithWindowSize(70, 10))

		h.Wait("size=70x10")
	})

	t.Run("explicit zero size", func(t *testing.T) {
		t.Parallel()
		// The terminal is 1x1, too small to show the size.
		r := record(&componentWrapper[*sizeViewModel]{model: &sizeViewModel{}})
		NewHarness(t, r, WithWindowSize(0, 0))

		msg := waitMsg[tea.WindowSizeMsg](t, r, nil)
		if msg.Width != 0 || msg.Height != 0 {
			t.Fatalf("initial size = %dx%d, want 0x0", msg.Width, msg.Height)
		}
	})
}

type asyncViewModel struct {
	width  int
	height int
	text   string
}

func (m *asyncViewModel) Init() tea.Cmd {
	return func() uv.Event {
		return appendMsg("ready")
	}
}

func (m *asyncViewModel) View() string {
	return fmt.Sprintf("size=%dx%d\ntext=%s", m.width, m.height, m.text)
}

func (m *asyncViewModel) Update(msg uv.Event) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case appendMsg:
		m.text += string(msg)
	}
	return nil
}

func TestComponentHarnessAsyncBridge(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &asyncViewModel{}, WithWindowSize(33, 4))

	h.Wait([]string{"size=33x4", "text=ready"})
}

type viewSettleTick struct{}

type viewSettleStableModel struct {
	ticks int
}

func (m *viewSettleStableModel) View() string {
	return "stable"
}

func (m *viewSettleStableModel) Init() tea.Cmd {
	return tea.Tick(2*time.Millisecond, func(time.Time) uv.Event {
		return viewSettleTick{}
	})
}

func (m *viewSettleStableModel) Update(msg uv.Event) tea.Cmd {
	if _, ok := msg.(viewSettleTick); !ok {
		return nil
	}
	if m.ticks >= 15 {
		return nil
	}
	m.ticks++
	return tea.Tick(2*time.Millisecond, func(time.Time) uv.Event {
		return viewSettleTick{}
	})
}

func TestComponentHarnessWaitSettledView(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &viewSettleStableModel{}, WithWindowSize(80, 1))
	h.Wait("stable")

	h.WaitSettle(
		WithSettle(25*time.Millisecond),
		WithTimeout(2*time.Second),
	)
	if !strings.Contains(h.View(), "stable") {
		t.Fatalf("view = %q, want stable", h.View())
	}
}

func TestComponentHarnessSendFilterReceivesOriginalMessage(t *testing.T) {
	t.Parallel()
	seen := make(chan struct{}, 1)
	h := NewComponentHarness(
		t,
		&mutableViewModel{},
		WithWindowSize(80, 1),
		WithProgramOptions(tea.WithFilter(func(_ tea.Model, msg uv.Event) uv.Event {
			if _, ok := msg.(appendMsg); ok {
				select {
				case seen <- struct{}{}:
				default:
				}
			}
			return msg
		})),
	)

	h.SendProgram(appendMsg("x")).Wait("text=x")

	select {
	case <-seen:
	default:
		t.Fatalf("filter did not receive appendMsg")
	}
}

func TestComponentHarnessRequirePlainSnapshot(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")

	h := NewComponentHarness(t, &mutableViewModel{text: "\x1b[31mred\x1b[0m"}, WithWindowSize(80, 1))
	h.Wait("red")
	h.RequireSnapshot(snapshot.WithANSI(false), snapshot.WithDir(snapDir), snapshot.WithUpdate(true))

	got := readSteepSnapshot(t, filepath.Join(snapDir, "TestComponentHarnessRequirePlainSnapshot.snap"))
	if got != padScreen([]string{"text=red"}, 80) {
		t.Fatalf("snapshot = %q, want text=red padded to the window", got)
	}
}

func readSteepSnapshot(t *testing.T, path string) string {
	t.Helper()

	bts, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read snapshot: %v", err)
	}
	return string(bts)
}
