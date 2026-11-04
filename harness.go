// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"fmt"
	"iter"
	"os"
	"os/signal"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/steep/reel"
)

var _ MessageCollector = (*Harness)(nil)

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
// built on [reel.Session.Wait]. The package-level Assert*, Require*, and
// message Wait* functions of the same names check any [Viewable] or
// [MessageCollector] and return a result instead: whether an Assert*
// passed, or the message that satisfied a Wait*.
type Harness struct {
	tb       testing.TB
	failures *failureTB
	session  *reel.Session
	program  *tea.Program
	observer *observer
	opts     []Option
	cfg      options

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
	// marksMu guards markCount.
	marksMu   sync.Mutex
	markCount map[HookEvent]int
	closed    atomic.Bool
	// waits numbers recorded waits, for their annotation ids.
	waits atomic.Int64
}

// NewHarness runs the root [tea.Model] in a [tea.Program] and returns a
// harness to drive and assert on it. It waits up to the [WithTimeout]
// timeout for the model's first message, and registers [Harness.Close] as a
// test cleanup.
func NewHarness(tb testing.TB, model tea.Model, opts ...Option) *Harness {
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

	// Bubble Tea writes capability queries before its first paint, so the
	// recording starts on that paint rather than on the blank terminal.
	session, err := reel.Open(
		tb.Context(), max(cfg.width, 1), max(cfg.height, 1),
		reel.WithTitle(tb.Name()), reel.WithSkipPristine(),
		reel.WithScrollOnInput(cfg.scrollOnInput), reel.WithTypingDelay(cfg.typingDelay),
		reel.WithSource(cfg.source), reel.WithSourceSkip(sourceSkip...),
	)
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
		observer:  newObserver(model),
		done:      make(chan struct{}),
		cfg:       cfg,
		imager:    imager,
		start:     time.Now(),
		autoMarks: autoMarks,
		markCount: make(map[HookEvent]int),
	}
	// Clipped, so one call's appends cannot write into another's.
	h.opts = slices.Clip(append(slices.Clone(opts), withHarness(h)))
	failures.note = h.noteFailure
	if cfg.recordMessages {
		h.observer.onMessage = h.noteMessage
	}
	h.annotate(newStartNote(tb.Name(), cfg))

	env, programOpts := cfg.envVars, cfg.programOpts
	if cfg.color != 0 {
		env = cfg.color.env(env)
		programOpts = append([]tea.ProgramOption{tea.WithColorProfile(cfg.color.teaProfile())}, programOpts...)
	}
	h.program = tea.NewProgram(
		h.observer,
		append(
			programOpts,
			tea.WithEnvironment(env),
			tea.WithContext(cfg.ctx),
			tea.WithInput(h.session),
			tea.WithOutput(h.session),
			tea.WithoutSignals(),
			tea.WithWindowSize(cfg.width, cfg.height),
		)...,
	)

	h.observer.outputSeq = h.session.Terminal().OutputSeq
	h.observer.send = h.program.Send

	go func() {
		_, h.runErr = h.program.Run()
		h.noteExit(h.runErr)
		close(h.done)
	}()

	// Registered before waiting, so a startup failure still cleans up.
	tb.Cleanup(h.Close)

	h.waitStarted(cfg)

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

	// Unblock the program's input loop before Quit.
	h.session.Terminal().CloseReader()

	go h.program.Quit()

	ctx, cancel := context.WithTimeout(cfg.ctx, cfg.timeout)
	defer cancel()

	select {
	case <-h.done:
	case <-ctx.Done():
		// Kill races with the setup in Run, which a dead context or a test that
		// ends early can still be in. Waiting for it can't hang the test, as
		// waitFinished gives up on the program.
		go func() {
			select {
			case <-h.observer.started:
				h.program.Kill()
			case <-h.done:
			}
		}()
	}
	// The test's context is canceled during cleanups.
	cfg.ctx = context.Background()
	h.waitFinished(cfg, nil)
}

func (h *Harness) waitStarted(cfg options) {
	h.tb.Helper()

	ctx, cancel := context.WithTimeoutCause(cfg.ctx, cfg.timeout, errWaitTimeout)
	defer cancel()

	note := func(o outcome) {
		h.annotate(&readyNote{Outcome: o, ElapsedMS: time.Since(h.start).Milliseconds()})
	}
	select {
	case <-h.observer.firstEvent:
		note(outcomeReady)
		h.hook(HookReady, nil)()
	case <-ctx.Done():
		note(doneOutcome(ctx))
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
	msgType := reflect.TypeOf(msg)
	defer h.hook(HookProgram, nil, func(got uv.Event) int {
		if reflect.TypeOf(got) == msgType {
			return 1
		}
		return 0
	})()
	n := &programNote{Action: "send"}
	n.Type, n.Value = valueNote(msg)
	h.annotate(n)
	h.program.Send(msg)
	return h
}

// QuitProgram requests a graceful shutdown of the [tea.Program].
func (h *Harness) QuitProgram() *Harness {
	h.tb.Helper()
	defer h.hook(HookClose, nil)()
	h.annotate(&programNote{Action: "quit"})
	h.program.Quit()
	return h
}

// Mutate applies fn to the model on the program's goroutine, as part of its
// Update handling. Use it sparingly, for scenarios that are otherwise
// impossible to set up or would pollute the model with test-only state.
//
// fn must take and return the model type passed to [NewHarness] or
// [NewComponentHarness], such as func(Model) Model or func(*Model) *Model,
// or the test fails.
func (h *Harness) Mutate[M any](fn func(M) M, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookProgram, nil, responseTo[mutateMsg])()

	if fn == nil {
		h.tb.Fatalf("mutate function must not be nil")
	}
	h.annotate(&programNote{Action: "mutate", Type: typeName[M]()})

	cfg := collectOptions(h.tb, h.mergedOpts(opts...)...)
	done := make(chan error, 1)
	// Not SendProgram, which would record and hook it as a test message.
	h.program.Send(mutateMsg{done: done, apply: func(model any) (any, error) {
		current, ok := model.(M)
		if !ok {
			var zero M
			return nil, fmt.Errorf("mutate function must accept %T, current model is %T", zero, model)
		}
		return fn(current), nil
	}})

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case err := <-done:
		if err != nil {
			h.tb.Fatalf("mutate failed: %v", err)
		}
	case <-timer.C:
		h.tb.Fatalf("timeout waiting for mutation after %s", cfg.timeout)
	case <-cfg.ctx.Done():
		h.tb.Fatalf("test context canceled waiting for mutation: %v", cfg.ctx.Err())
	}

	return h
}

// FinalModel waits for the [tea.Program] to finish and returns the root
// model.
func (h *Harness) FinalModel(opts ...Option) tea.Model {
	h.tb.Helper()
	h.WaitFinished(opts...)
	return h.observer.currentModel()
}

// FinalModelAs is [Harness.FinalModel], with the model as M. It fails the
// test when the model is not M.
func (h *Harness) FinalModelAs[M any](opts ...Option) M {
	h.tb.Helper()
	model := h.FinalModel(opts...)
	typed, ok := model.(M)
	if !ok {
		var zero M
		h.tb.Fatalf("final model type = %T, want %T", model, zero)
	}
	return typed
}

// MessageHistory returns a copy of all messages the model received.
func (h *Harness) MessageHistory() iter.Seq[uv.Event] {
	h.tb.Helper()
	return h.observer.messages.History()
}

// Messages returns an iterator over all messages the model received, past
// and future, until ctx is done. Past messages are complete, but the iterator
// buffers only 32 new ones: a loop that falls further behind misses the
// newer messages, which [Harness.MessageHistory] still has.
func (h *Harness) Messages(ctx context.Context) iter.Seq[uv.Event] {
	h.tb.Helper()
	return h.observer.messages.SubscribeAll(ctx)
}

// LiveMessages returns an iterator over messages the model receives after
// the call returns, until ctx is done. Like [Harness.Messages], it buffers
// only 32 messages, and a loop that falls further behind misses the newer ones.
func (h *Harness) LiveMessages(ctx context.Context) iter.Seq[uv.Event] {
	h.tb.Helper()
	return h.observer.messages.Subscribe(ctx)
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
