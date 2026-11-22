// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"
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

	h.QuitProgram()
	h.WaitFinished(WithTimeout(time.Second))
}

// stuckInitModel blocks in Init until release closes.
type stuckInitModel struct {
	rootTestModel

	release chan struct{}
}

func (m stuckInitModel) Init() tea.Cmd {
	<-m.release
	return nil
}

// TestCloseStuckInit checks that a program stuck in its model's Init fails the
// test when closed, rather than hanging it.
func TestCloseStuckInit(t *testing.T) {
	t.Parallel()
	ftb := &fakeTB{TB: t}
	m := stuckInitModel{release: make(chan struct{})}
	t.Cleanup(func() { close(m.release) })
	h := NewHarness(ftb, m, WithIgnoreEnv(true), WithWindowSize(20, 2), WithTimeout(50*time.Millisecond))
	runFailing(h.Close)
	if errs := strings.Join(ftb.errors(), "\n"); !strings.Contains(errs, "waiting for the program to finish") {
		t.Fatalf("errors = %q, want a failure to stop the program", errs)
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

// panicModel panics in Update on "u", or in a command on "c".
type panicModel struct{ rootTestModel }

func (m panicModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "u":
			panic("boom in Update")
		case "c":
			return m, func() tea.Msg { panic("boom in a command") }
		}
	}
	next, cmd := m.rootTestModel.Update(msg)
	return panicModel{next.(rootTestModel)}, cmd //nolint:forcetypeassert // rootTestModel returns itself.
}

// TestPanicFailsTest checks that a panicking program fails the test, whether
// the test waits for its end or the harness finds out as it closes.
func TestPanicFailsTest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, key string
		end       func(h *Harness)
	}{
		{"Update, WaitFinished", "u", func(h *Harness) { h.WaitFinished() }},
		{"command, WaitFinished", "c", func(h *Harness) { h.WaitFinished() }},
		{"Update, Close", "u", func(h *Harness) { <-h.done; h.Close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ftb := &fakeTB{TB: t}
			h := NewHarness(ftb, panicModel{}, WithWindowSize(20, 2), WithIgnoreEnv(true), WithTimeout(5*time.Second))
			h.Key(tc.key)
			runFailing(func() { tc.end(h) })
			if errs := ftb.errors(); !strings.Contains(strings.Join(errs, "\n"), tea.ErrProgramPanic.Error()) {
				t.Errorf("errors = %q, want the panic", errs)
			}
			findNote(t, notes(t, h.Timeline()), kindExit, map[string]any{"error": "program was killed: " + tea.ErrProgramPanic.Error()})
		})
	}
}

// TestCancelBeforeRun checks what [Harness.stopProgram] relies on of Bubble
// Tea: canceling the program's context before Run starts, as when a test
// closes a harness on a dead context before the program's goroutine runs,
// kills the program as it starts, rather than being lost.
func TestCancelBeforeRun(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	p := tea.NewProgram(rootTestModel{},
		tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignals(),
		tea.WithEnvironment([]string{"TTY_FORCE=1", term256}), tea.WithContext(ctx),
	)
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, tea.ErrProgramKilled) {
			t.Fatalf("Run = %v, want %v", err, tea.ErrProgramKilled)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after an earlier cancel")
	}
}
