// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// recorder is a [tea.Model] for steep's own tests of what reaches a
// program, such as how input is encoded. It passes everything to the model
// it wraps, and keeps every message that model receives, which the harness
// does not.
type recorder struct {
	// model is only used on the program's goroutine, or once it has
	// finished.
	model tea.Model

	mu   sync.Mutex
	msgs []uv.Event
	// changed is closed, and replaced, on each message.
	changed chan struct{}
}

// record returns a recorder wrapping model.
func record(model tea.Model) *recorder {
	return &recorder{model: model, changed: make(chan struct{})}
}

func (r *recorder) Init() tea.Cmd { return r.model.Init() }

func (r *recorder) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	next, cmd := r.model.Update(msg)
	if next != nil {
		r.model = next
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
	close(r.changed)
	r.changed = make(chan struct{})
	return r, cmd
}

func (r *recorder) View() tea.View { return r.model.View() }

// history returns a copy of the messages so far, and a channel closed on
// the next.
func (r *recorder) history() (msgs []uv.Event, next <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uv.Event(nil), r.msgs...), r.changed
}

// next returns a channel closed on the next message.
func (r *recorder) next() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.changed
}

// all returns a copy of the messages so far.
func (r *recorder) all() []uv.Event {
	msgs, _ := r.history()
	return msgs
}

// count returns how many messages there have been so far.
func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}

// recorderTimeout is how long the recorder's waits wait.
const recorderTimeout = 5 * time.Second

// waitMsg waits for a message of type T that fn accepts, or any when fn is
// nil, and returns it.
func waitMsg[T uv.Event](tb testing.TB, r *recorder, fn func(T) bool) T {
	tb.Helper()
	deadline := time.After(recorderTimeout)
	for {
		msgs, changed := r.history()
		for _, msg := range msgs {
			if typed, ok := msg.(T); ok && (fn == nil || fn(typed)) {
				return typed
			}
		}
		select {
		case <-changed:
		case <-deadline:
			var zero T
			tb.Fatalf("no %T message after %s, got:\n%s", zero, recorderTimeout, describeMsgs(msgs))
			return zero
		}
	}
}

// settle waits until the model has received no message for quiet.
func (r *recorder) settle(tb testing.TB, quiet time.Duration) {
	tb.Helper()
	deadline := time.After(recorderTimeout)
	for {
		select {
		case <-r.next():
		case <-time.After(quiet):
			return
		case <-deadline:
			tb.Fatalf("messages did not settle for %s in %s", quiet, recorderTimeout)
			return
		}
	}
}

// recorderQuiet is the quiet period of [recorder.settle] for input, whose
// messages come as soon as the program reads it.
const recorderQuiet = 50 * time.Millisecond

// describeMsgs lists msgs, one per line, for a failure.
func describeMsgs(msgs []uv.Event) string {
	if len(msgs) == 0 {
		return "  (none)\n"
	}
	var b strings.Builder
	for _, msg := range msgs {
		fmt.Fprintf(&b, "  %T %+v\n", msg, msg)
	}
	return b.String()
}
