// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package steep

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/steep/reel"
)

// execTimeout is long enough for bash to start on a busy machine.
const execTimeout = 10 * time.Second

// bash returns a command running script with bash, skipping the test when the
// host has no bash or pseudo-terminals.
func bash(t *testing.T, script string) *exec.Cmd {
	t.Helper()
	path, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	_ = ptmx.Close()
	return exec.Command(path, "--norc", "--noprofile", "-c", script) //nolint:gosec,noctx // The harness owns the command's context.
}

// TestExecHarness drives a command through its input and output, including a
// resize, and reads its exit status.
func TestExecHarness(t *testing.T) {
	t.Parallel()
	h := NewExecHarness(t, bash(t, `read -r -p "name? " name; echo "hello $name"; read -r; stty size; exit 3`),
		WithIgnoreEnv(true), WithWindowSize(40, 6), WithTimeout(execTimeout))

	h.Wait("name?").Type("steep").Key("enter").Wait("hello steep").Assert("hello steep")
	h.Resize(50, 7).Key("enter").Wait("7 50")

	if got := h.ExitStatus(); got != 3 {
		t.Errorf("ExitStatus = %d, want 3", got)
	}
	h.Assert("7 50")
}

// TestExecHarnessEnv checks where each color and other variable comes from.
func TestExecHarnessEnv(t *testing.T) {
	t.Setenv("STEEP_EXEC_HOST", "host")
	t.Setenv("COLORTERM", "host")
	tests := []struct {
		name string
		env  []string
		opts []Option
		want string
	}{
		{
			name: "cmd env is kept",
			env:  []string{"PATH=" + os.Getenv("PATH"), "TERM=dumb", "COLORTERM=no", "STEEP_EXEC_CMD=cmd"},
			want: "[dumb][no][][cmd]",
		},
		{
			name: "env vars and the host replace cmd env",
			env:  []string{"PATH=" + os.Getenv("PATH"), "TERM=dumb", "COLORTERM=no", "STEEP_EXEC_CMD=cmd"},
			opts: []Option{WithEnvVars("TERM=linux")},
			want: "[linux][host][host][cmd]",
		},
		{
			name: "color overrides cmd env",
			env:  []string{"PATH=" + os.Getenv("PATH"), "TERM=dumb", "COLORTERM=truecolor", "STEEP_EXEC_CMD=cmd"},
			opts: []Option{WithColor(ColorANSI)},
			want: "[xterm][][][cmd]",
		},
		{
			name: "color overrides host env",
			opts: []Option{WithColor(ColorANSI256)},
			want: "[xterm-256color][][host][]",
		},
		{
			name: "true color",
			opts: []Option{WithColor(ColorTrueColor)},
			want: "[xterm-256color][truecolor][host][]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := bash(t, `echo "[$TERM][$COLORTERM][$STEEP_EXEC_HOST][$STEEP_EXEC_CMD]"`)
			cmd.Env = tt.env
			opts := append([]Option{WithIgnoreEnv(true), WithTimeout(execTimeout)}, tt.opts...)
			NewExecHarness(t, cmd, opts...).Wait(tt.want)
		})
	}
}

// TestExecHarnessCloseKills checks that Close kills a command still running,
// which is not a failure.
func TestExecHarnessCloseKills(t *testing.T) {
	t.Parallel()
	h := NewExecHarness(t, bash(t, `echo started; exec sleep 100`), WithIgnoreEnv(true), WithTimeout(execTimeout))
	h.Wait("started")
	h.Close()
	if status, err := h.process.Wait(); status != 137 || err == nil {
		t.Errorf("Wait = %d, %v, want status 137 and an error", status, err)
	}
	findNote(t, notes(t, h.Timeline()), kindExit, map[string]any{"killed": true})
}

// TestExecHarnessContextKills checks that the context of [WithContext] ends the
// command, as it does a [tea.Program].
func TestExecHarnessContextKills(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	h := NewExecHarness(t, bash(t, `echo started; exec sleep 100`), WithIgnoreEnv(true), WithContext(ctx), WithTimeout(execTimeout))
	h.Wait("started")
	cancel()
	select {
	case <-h.done:
	case <-time.After(execTimeout):
		t.Fatal("the command did not end with its context")
	}
}

// TestExecHarnessExport checks that actions wait for the command's output while
// exporting, and that the recording holds it, the exit status included.
func TestExecHarnessExport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := NewExecHarness(t, bash(t, `read -r line; echo "got $line"; exit 2`),
		WithIgnoreEnv(true), WithTimeout(execTimeout), WithArtifacts(dir), WithExportAlways(),
		WithRecording(reel.Profile{Outputs: reel.MustFiles("recording.cast")}))
	h.Type("hi").Key("enter").Wait("got hi")
	if got := h.ExitStatus(); got != 2 {
		t.Errorf("ExitStatus = %d, want 2", got)
	}
	h.Close()

	data, err := os.ReadFile(filepath.Join(dir, "recording.cast"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"got hi", "input:2", "steep.exit", `"x","2"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("recording has no %q:\n%s", want, data)
		}
	}
}

func TestExecHarnessStartFailure(t *testing.T) {
	t.Parallel()
	ftb := &fakeTB{TB: t}
	runFailing(func() {
		NewExecHarness(ftb, exec.Command(filepath.Join(t.TempDir(), "missing")), WithIgnoreEnv(true)) //nolint:gosec,noctx // A command that cannot start.
	})
	if errs := strings.Join(ftb.errors(), "\n"); !strings.Contains(errs, "missing") {
		t.Fatalf("errors = %q, want the command's failure", errs)
	}
}

// TestExecHarnessProgramMethods checks that what needs a [tea.Program] fails
// with a message saying so, rather than panicking or running out its timeout.
func TestExecHarnessProgramMethods(t *testing.T) {
	t.Parallel()
	cmd := bash(t, `sleep 100`)
	ftb := &fakeTB{TB: t}
	h := NewExecHarness(ftb, cmd, WithIgnoreEnv(true), WithTimeout(execTimeout))

	tests := []struct {
		name string
		want string
		call func()
	}{
		{"SendProgram", "SendProgram", func() { h.SendProgram(tea.KeyPressMsg{}) }},
		{"QuitProgram", "QuitProgram", func() { h.QuitProgram() }},
		{"Mutate", "Mutate", func() { h.Mutate(func(m tea.Model) tea.Model { return m }) }},
		{"FinalModel", "FinalModel", func() { h.FinalModel() }},
		{"MessageHistory", "MessageHistory", func() { h.MessageHistory() }},
		{"Messages", "Messages", func() { h.Messages(t.Context()) }},
		{"LiveMessages", "LiveMessages", func() { h.LiveMessages(t.Context()) }},
		{"WaitMessage", "Messages", func() { h.WaitMessage[uv.WindowSizeEvent]() }},
		{"Harness.WaitSettleMessages", "LiveMessages", func() { h.WaitSettleMessages() }},
		{"WaitSettleMessages", "LiveMessages", func() { WaitSettleMessages(ftb, h) }},
	}
	for _, tt := range tests {
		var returned bool
		runFailing(func() {
			tt.call()
			returned = true
		})
		if returned {
			t.Errorf("%s: did not stop the test", tt.name)
		}
		if errs := ftb.errors(); !slices.ContainsFunc(errs, func(e string) bool {
			return strings.Contains(e, "Harness."+tt.want+" needs a tea.Program")
		}) {
			t.Errorf("%s: errors = %q", tt.name, errs)
		}
	}
}

// TestExitStatusNeedsCommand checks the opposite of
// [TestExecHarnessProgramMethods].
func TestExitStatusNeedsCommand(t *testing.T) {
	t.Parallel()
	ftb := &fakeTB{TB: t}
	h := NewHarness(ftb, rootTestModel{}, WithIgnoreEnv(true))
	runFailing(func() { h.ExitStatus() })
	if errs := strings.Join(ftb.errors(), "\n"); !strings.Contains(errs, "Harness.ExitStatus needs a command") {
		t.Fatalf("errors = %q", errs)
	}
}
