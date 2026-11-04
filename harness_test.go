// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

type setTextMsg string

type rootTestModel struct {
	width  int
	height int
	text   string
}

func (m rootTestModel) Init() tea.Cmd {
	return nil
}

func (m rootTestModel) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyPressMsg:
		m.text += msg.Key().Text
	case setTextMsg:
		m.text = string(msg)
	case tea.QuitMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m rootTestModel) View() tea.View {
	return tea.NewView(fmt.Sprintf("size=%dx%d\ntext=%s", m.width, m.height, m.text))
}

// fakeTB is a [testing.TB] whose errors are recorded instead of failing the
// test, so a harness can run as a failing test inside a passing one.
type fakeTB struct {
	testing.TB

	mu     sync.Mutex
	failed bool
	errs   []string
}

func (f *fakeTB) Error(args ...any) { f.record(fmt.Sprintln(args...)) }

func (f *fakeTB) Errorf(format string, args ...any) { f.record(fmt.Sprintf(format, args...)) }

func (f *fakeTB) Fail() { f.record("") }

// FailNow marks the test failed, and stops the goroutine, so it must be
// called on a goroutine of the test's own.
func (f *fakeTB) FailNow() {
	f.record("")
	runtime.Goexit()
}

// Fatalf records the error, and stops the goroutine as FailNow does, so it
// must be called on a goroutine of the test's own.
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.record(fmt.Sprintf(format, args...))
	runtime.Goexit()
}

func (f *fakeTB) record(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = true
	if msg != "" {
		f.errs = append(f.errs, msg)
	}
}

func (f *fakeTB) Failed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failed
}

func (f *fakeTB) errors() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.errs)
}

func TestHarness(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithWindowSize(80, 2))

	h.Wait("size=80x2").
		Require("size=80x2").
		RequireWidth(80).
		RequireHeight(2).
		RequireDimensions(80, 2).
		Type("ab").Wait([]byte("text=ab")).
		SendProgram(setTextMsg("done")).Wait("text=done").WaitNot("text=ab").RequireNot("text=ab")

	msgs := slices.Collect(h.MessageHistory())
	if len(msgs) < 4 {
		t.Fatalf("expected at least 4 messages, got %d", len(msgs))
	}

	h.QuitProgram()
	h.WaitFinished(WithTimeout(time.Second))

	finalModel := h.FinalModelAs[rootTestModel]()
	if finalModel.text != "done" {
		t.Fatalf("final model text = %q, want done", finalModel.text)
	}
}

func TestHarness_QuitProgram_returnsHarnessForChaining(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithWindowSize(80, 3))
	if ptr := h.QuitProgram(); ptr != h {
		t.Fatalf("QuitProgram should return the same harness, got %p want %p", ptr, h)
	}
	h.WaitFinished(WithTimeout(time.Second))
}

func TestFailureTBRecords(t *testing.T) {
	t.Parallel()
	f := &failureTB{TB: &fakeTB{TB: t}}
	f.Error("want", 3, "rows")
	f.Errorf("timeout after %s", "2s")
	want := []string{"want 3 rows", "timeout after 2s"}
	if got := f.messages(); !slices.Equal(got, want) {
		t.Fatalf("messages = %q, want %q", got, want)
	}
}
