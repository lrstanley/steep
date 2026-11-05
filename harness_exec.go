// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lrstanley/steep/reel"
)

// NewExecHarness starts cmd on a pseudo-terminal (see [reel.Exec]) and returns
// a harness to drive and assert on it, for programs that cannot run in the
// test's process, such as a built binary or one that does not use Bubble Tea.
// It registers [Harness.Close] as a test cleanup, which kills cmd if it is
// still running. It fails the test if cmd cannot be started.
//
// Unlike [NewHarness], it does not wait for cmd to do anything, as a command
// has no first message to wait for. Wait for what it draws, such as with
// [Harness.Wait].
//
// The environment is cmd.Env, or the host's when nil, changed by [WithEnvVars]
// and [WithColor]. [WithProgramOptions], [WithRecordMessages], and
// [WithSettleIgnoreMsgs] have no effect. Methods that need the program or its
// model fail the test: [Harness.SendProgram], [Harness.QuitProgram],
// [Harness.Mutate], [Harness.FinalModel], and those that read messages, such as
// [Harness.WaitMessage].
func NewExecHarness(tb testing.TB, cmd *exec.Cmd, opts ...Option) *Harness {
	tb.Helper()
	h := newHarness(tb, opts)

	// Canceling ctx is what kills the process.
	ctx, cancel := context.WithCancel(h.cfg.ctx)
	h.stop = func(options) { cancel() }
	h.tb.Cleanup(h.Close)

	h.cfg.applyExecEnv(cmd)
	process, err := reel.Exec(ctx, h.session, cmd)
	if err != nil {
		close(h.done)
		h.tb.Fatalf("steep: %v", err)
	}
	h.process = process

	go func() {
		_, h.runErr = process.Wait()
		if h.runErr != nil && errors.Is(h.runErr, ctx.Err()) {
			h.runErr = fmt.Errorf("%w: %w", tea.ErrProgramKilled, h.runErr)
		}
		h.noteExit(h.runErr)
		close(h.done)
	}()

	h.ready()

	return h
}

// ExitStatus waits for the command of [NewExecHarness] to finish, as
// [Harness.WaitFinished] does, and returns its exit status. On Unix, a command
// killed by a signal has status 128+signal, as shells report it.
func (h *Harness) ExitStatus(opts ...Option) int {
	h.tb.Helper()
	if h.process == nil {
		h.tb.Fatalf("steep: Harness.ExitStatus needs a command, and this harness runs a tea.Program (see FinalModel)")
	}
	h.WaitFinished(opts...)
	status, _ := h.process.Wait()
	return status
}

// applyExecEnv sets cmd's environment from [WithEnvVars] and [WithColor], and
// otherwise leaves it to [reel.Exec].
func (o options) applyExecEnv(cmd *exec.Cmd) {
	if o.envVars != nil {
		cmd.Env = append(cmd.Environ(), o.envVars...)
	}
	if o.color == 0 {
		return
	}
	cmd.Env = o.color.env(cmd.Environ())
	if o.color != ColorTrueColor {
		// Set but empty, as [reel.Exec] adds COLORTERM=truecolor when it is unset.
		cmd.Env = append(cmd.Env, "COLORTERM=")
	}
}
