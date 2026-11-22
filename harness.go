// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"getsteep.dev/steep/reel"
	uv "github.com/charmbracelet/ultraviolet"
)

// Harness is a test harness for a [tea.Program].
//
// Its checks follow one naming convention:
//
//   - Assert* report a failed check with [testing.TB.Errorf] and let the
//     test continue.
//   - Require* stop the test with [testing.TB.FailNow] when the check fails.
//   - Wait* block until the condition holds, and stop the test with
//     [testing.TB.Fatalf] if [WithTimeout] passes or the context is canceled
//     first.
//
// Most methods return the harness for chaining. Waits on the screen are
// built on [reel.Session.Wait]. The package-level Assert* and Require*
// functions of the same names check any [Viewable], and Assert* returns
// whether it passed.
//
// The harness tests a program from outside, as a user would: through the
// terminal it draws on. It does not keep or inspect the messages the model
// receives. Check a model's state in an ordinary unit test that calls its
// Update method instead.
//
// A harness from [NewExecHarness] runs a command rather than a [tea.Program]:
// methods that need the program fail the test.
type Harness struct {
	tb       testing.TB
	failures *failureTB
	session  *reel.Session
	// program is nil for a command, and process is nil for a [tea.Program].
	program *tea.Program
	process *reel.Process
	opts    []Option
	cfg     options

	// stop ends the program for [Harness.Close].
	stop func(cfg options)
	// kill cancels the context of the [tea.Program], which kills it.
	kill context.CancelFunc

	// endMu keeps what the program notes out of the end of the recording,
	// between the result and where [Harness.Close] cuts it.
	endMu sync.Mutex
	// done closes when the program has finished, with runErr set.
	done   chan struct{}
	runErr error

	// imager draws [Harness.Image] and [Harness.ImageInto].
	imager *reel.Imager
	// start is when the harness was created, for [Result.Duration].
	start time.Time

	// autoMarks is whether each of autoMarkCategories is marked.
	autoMarks map[string]bool
	// marksMu guards markCount and pendingMarks.
	marksMu   sync.Mutex
	markCount map[HookEvent]int
	// pendingMarks are the marks of actions that wait for the test's next
	// step (see [Harness.Mark]), oldest first.
	pendingMarks []reel.Mark
	closed       atomic.Bool
	// waits numbers recorded waits, for their annotation ids.
	waits atomic.Int64
	// controls are the strings the program wrote, for
	// [Harness.ControlStrings].
	controls *controlLog
}

// NewHarness runs the root [tea.Model] in a [tea.Program] and returns a
// harness to drive and assert on it. It waits up to the [WithTimeout]
// timeout for the program's first paint, when its first view and the modes
// it asks for, such as mouse tracking, are on the terminal, so input is
// encoded as the program expects. A program that paints nothing is taken as
// started at the timeout. It registers [Harness.Close] as a test cleanup.
func NewHarness(tb testing.TB, model tea.Model, opts ...Option) *Harness {
	tb.Helper()
	h := newHarness(tb, opts)
	cfg := h.cfg
	h.stop = h.stopProgram

	env, programOpts := cfg.envVars, cfg.programOpts
	if env == nil {
		// Force TTY mode, since we're not connecting to a real TTY/PTY.
		env = []string{"TTY_FORCE=1", term256}
	}
	if cfg.color != 0 {
		env = cfg.color.env(env)
		programOpts = append([]tea.ProgramOption{tea.WithColorProfile(cfg.color.teaProfile())}, programOpts...)
	}
	// The program's own context, as canceling it is a Kill that cannot be lost
	// to Run not having started yet.
	ctx, kill := context.WithCancel(cfg.ctx)
	h.kill = kill
	h.program = tea.NewProgram(
		model,
		append(
			programOpts,
			tea.WithEnvironment(env),
			tea.WithContext(ctx),
			tea.WithInput(h.session),
			tea.WithOutput(h.session),
			tea.WithoutSignals(),
			tea.WithWindowSize(cfg.width, cfg.height),
		)...,
	)

	go func() {
		_, h.runErr = h.program.Run()
		kill()
		h.noteExit(h.runErr)
		close(h.done)
	}()

	// Registered before waiting, so a startup failure still cleans up.
	h.tb.Cleanup(h.Close)

	h.waitStarted(cfg)

	return h
}

// newHarness returns a harness with its terminal and recording open, and no
// program running in it yet.
func newHarness(tb testing.TB, opts []Option) *Harness {
	tb.Helper()
	failures := &failureTB{TB: tb}
	tb = failures

	cfg := collectOptions(tb, opts...)

	if !cfg.wasContextSet {
		ctx, cancel := signal.NotifyContext(tb.Context(), os.Interrupt)
		tb.Cleanup(cancel)
		opts = append(opts, WithContext(ctx))
		cfg.ctx = ctx
	}
	autoMarks := cfg.autoMarked(tb)

	// Bubble Tea writes capability queries before its first paint, and leaves
	// the alternate screen as it exits, so the recording starts and ends on
	// its paints rather than on the blank terminal.
	controls := &controlLog{}
	sessionOpts := []reel.SessionOption{
		reel.WithTitle(tb.Name()), reel.WithSkipPristine(),
		reel.WithControlStrings(controls.add),
		reel.WithScrollOnInput(cfg.scrollOnInput), reel.WithTypingDelay(cfg.typingDelay),
		reel.WithSource(cfg.source), reel.WithSourceSkip(sourceSkip...),
	}
	// Bubble Tea on Windows writes LF to mean only "down a row". Elsewhere,
	// with no terminal for input, it expects LF to start a new line, as the
	// session's default output does.
	if runtime.GOOS == "windows" {
		sessionOpts = append(sessionOpts, reel.WithoutONLCR())
	}
	session, err := reel.Open(tb.Context(), max(cfg.width, 1), max(cfg.height, 1), sessionOpts...)
	if err != nil {
		tb.Fatal(err)
	}
	imager, err := reel.NewImager(session, cfg.recording.Render...)
	if err != nil {
		_ = session.Close()
		tb.Fatal(err)
	}

	h := &Harness{
		tb:        tb,
		failures:  failures,
		session:   session,
		done:      make(chan struct{}),
		cfg:       cfg,
		imager:    imager,
		start:     time.Now(),
		autoMarks: autoMarks,
		markCount: make(map[HookEvent]int),
		controls:  controls,
	}
	// Clipped, so one call's appends cannot write into another's.
	h.opts = slices.Clip(append(slices.Clone(opts), withHarness(h)))
	failures.note = h.noteFailure
	h.annotate(newStartNote(tb.Name(), cfg))

	return h
}

// NewComponentHarness is [NewHarness] for a component model, which must have
// a View() string method and an Update method returning either a [tea.Cmd]
// or the updated model and a [tea.Cmd]. Init is optional.
func NewComponentHarness[M any](tb testing.TB, model M, opts ...Option) *Harness {
	tb.Helper()
	m := &componentWrapper[M]{model: model}
	m.validate(tb)
	return NewHarness(tb, m, opts...)
}

// Close stops the program, exports the recording if the export policy says
// to, and releases the terminal. [NewHarness] registers it as a test cleanup,
// so calling it is optional. Calls after the first do nothing.
func (h *Harness) Close() {
	if h.closed.Swap(true) {
		return
	}
	// These run even when a hook or stopping the program fails the test. The
	// terminal returns to the pool after the After hooks and the imager.
	defer h.session.Close()
	defer func() {
		if err := h.imager.Close(); err != nil {
			h.tb.Errorf("failed to close the imager: %v", err)
		}
	}()
	defer h.hook(HookClose, nil)()

	cfg := collectOptions(h.tb, h.mergedOpts()...)

	defer h.exportRecording(h.endRecording())

	h.stop(cfg)
	// The test's context is canceled during cleanups.
	cfg.ctx = context.Background()
	h.waitFinished(cfg, nil)
}

// stopProgram asks the [tea.Program] to quit, and kills it if it has not by
// the timeout, by canceling its context.
func (h *Harness) stopProgram(cfg options) {
	// Unblock the program's input loop before Quit.
	h.session.Terminal().CloseReader()

	go h.program.Quit()

	ctx, cancel := context.WithTimeout(cfg.ctx, cfg.timeout)
	defer cancel()

	select {
	case <-h.done:
	case <-ctx.Done():
		// Not Program.Kill, whose shutdown a Kill before Run starts uses up, so
		// Run then leaks its renderer. Run notices the context once its setup,
		// which runs the model's Init, is done, and shuts down as it would for a
		// Kill. A program stuck before then is given up on by waitFinished.
		h.kill()
	}
}

// errProgramEnded ends the wait of [Harness.waitStarted] when the program
// ends before it paints.
var errProgramEnded = errors.New("the program ended")

// waitStarted waits up to the timeout for the program's first paint (see
// [reel.Session.WaitPainted]). Bubble Tea writes its frames on a timer, so
// this is when the program's first view, and the modes it asks for, are on
// the terminal, and input is encoded as it expects. A program that paints
// nothing is taken as started at the timeout, and the test goes on. One that
// ends first, such as by quitting from Init, has started.
func (h *Harness) waitStarted(cfg options) {
	h.tb.Helper()

	ctx, cancel := context.WithTimeoutCause(cfg.ctx, cfg.timeout, errWaitTimeout)
	defer cancel()
	wait, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	go func() {
		select {
		case <-h.done:
			stop(errProgramEnded)
		case <-wait.Done():
		}
	}()

	err := h.session.WaitPainted(wait)
	if err == nil || ctx.Err() == nil && errors.Is(err, errProgramEnded) {
		h.ready()
		return
	}
	h.noteReady(doneOutcome(ctx))
}

// ready records that the program has started.
func (h *Harness) ready() {
	h.tb.Helper()
	h.noteReady(outcomeReady)
	h.hook(HookReady, nil)()
}

func (h *Harness) noteReady(o outcome) {
	h.annotate(&readyNote{Outcome: o, ElapsedMS: time.Since(h.start).Milliseconds()})
}

// requireProgram fails the test if the harness runs a command, as method
// needs a [tea.Program].
func (h *Harness) requireProgram(method string) {
	h.tb.Helper()
	if h.program == nil {
		h.tb.Fatalf("steep: Harness.%s needs a tea.Program, and this harness runs a command (see NewExecHarness)", method)
	}
}

// check reports err from a terminal action without stopping the test.
func (h *Harness) check(err error) {
	h.tb.Helper()
	if err != nil {
		h.tb.Errorf("steep: %v", err)
	}
}

func (h *Harness) mergedOpts(call ...Option) []Option {
	h.tb.Helper()
	return append(h.opts, call...)
}

// SendProgram sends msg to the [tea.Program].
//
// Prefer driving input with [Harness.Type], [Harness.Key], [Harness.Paste],
// or the mouse helpers, which go through the terminal as real input would.
func (h *Harness) SendProgram(msg uv.Event) *Harness {
	h.tb.Helper()
	h.requireProgram("SendProgram")
	defer h.hook(HookProgram, nil)()
	n := &programNote{Action: "send"}
	n.Type, n.Value = valueNote(msg)
	h.annotate(n)
	h.program.Send(msg)
	return h
}

// QuitProgram requests a graceful shutdown of the [tea.Program].
func (h *Harness) QuitProgram() *Harness {
	h.tb.Helper()
	h.requireProgram("QuitProgram")
	defer h.hook(HookClose, nil)()
	h.annotate(&programNote{Action: "quit"})
	h.program.Quit()
	return h
}

// failureTB records the failure messages the harness reports through it, for
// [Result.Failures], and passes each to note, with its number counted from
// 1, before reporting it.
type failureTB struct {
	testing.TB

	// note is set once, before the program starts. It is called with mu
	// held, so notes keep the order of msgs, and must not report through the
	// failureTB.
	note func(n int, msg string)

	mu   sync.Mutex
	msgs []string
}

func (f *failureTB) Error(args ...any) {
	f.TB.Helper()
	f.record(sprintln(args...))
	f.TB.Error(args...)
}

func (f *failureTB) Errorf(format string, args ...any) {
	f.TB.Helper()
	f.record(fmt.Sprintf(format, args...))
	f.TB.Errorf(format, args...)
}

func (f *failureTB) Fatal(args ...any) {
	f.TB.Helper()
	f.record(sprintln(args...))
	f.TB.Fatal(args...)
}

func (f *failureTB) Fatalf(format string, args ...any) {
	f.TB.Helper()
	f.record(fmt.Sprintf(format, args...))
	f.TB.Fatalf(format, args...)
}

// sprintln formats args as [testing.TB.Error] does.
func sprintln(args ...any) string {
	return strings.TrimSuffix(fmt.Sprintln(args...), "\n")
}

func (f *failureTB) record(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg)
	if f.note != nil {
		f.note(len(f.msgs), msg)
	}
}

func (f *failureTB) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.msgs)
}
