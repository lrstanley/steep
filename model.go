// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"getsteep.dev/steep/internal/broker"
	uv "github.com/charmbracelet/ultraviolet"
)

type observer struct {
	mu       sync.RWMutex
	model    tea.Model
	messages *broker.Broker[uv.Event]

	// started closes when the program is past its setup, just before it runs
	// the model's Init, which can take as long as it likes.
	started chan struct{}
	// firstEvent closes once the first message, other than a mutate
	// request, has been through Update.
	firstEvent     chan struct{}
	firstEventOnce sync.Once
	// onMessage, when set, is called with each message but mutate requests
	// before the wrapped model receives it, without mu held. It is set
	// before the program starts.
	onMessage func(uv.Event)
	// outputSeq returns the terminal's output count, and send sends the
	// program a message, for watches. They are set before the program starts.
	outputSeq func() uint64
	send      func(uv.Event)
	// watchMu guards watch and its seen. It is not mu, which is held for as
	// long as the model takes, and a slow model must not hold up a harness
	// that has stopped waiting for it.
	watchMu sync.Mutex
	// watch is the current action's paintWatch, if any.
	watch *paintWatch
}

// paintWatch follows the messages an action causes through the model, to
// the point where a paint can only show them.
type paintWatch struct {
	// match returns how many of want msg is worth.
	match func(msg uv.Event) int
	// want is how much must match before the action counts as handled, and
	// seen how much has so far.
	want, seen int
	// seq is the terminal's output count at a point when the renderer holds a
	// view showing the handled messages, so output newer than it can only be
	// that view or a later one. It is set before painted closes.
	seq uint64
	// painted closes when seq is set.
	painted chan struct{}
}

// paintBarrier is sent to the program once a watch's messages are handled. The
// program handles messages one at a time, passing the view of each to its
// renderer before the next, which waits for a paint in progress. So by the time
// a barrier arrives, nothing older than that view can still be written.
type paintBarrier struct{ watch *paintWatch }

// watchPaint starts following messages that match, and replaces any other
// watch. The caller should unwatch it when done.
func (o *observer) watchPaint(want int, match func(uv.Event) int) *paintWatch {
	w := &paintWatch{match: match, want: want, painted: make(chan struct{})}
	o.watchMu.Lock()
	defer o.watchMu.Unlock()
	o.watch = w
	return w
}

// unwatch stops following w, if it has not finished already.
func (o *observer) unwatch(w *paintWatch) {
	o.watchMu.Lock()
	defer o.watchMu.Unlock()
	if o.watch == w {
		o.watch = nil
	}
}

// handled counts msg towards the current watch, and sends its barrier once it
// has seen enough. It cannot be sent from here, as the program is busy calling
// this.
func (o *observer) handled(msg uv.Event) {
	o.watchMu.Lock()
	defer o.watchMu.Unlock()
	w := o.watch
	if w == nil {
		return
	}
	if w.seen += w.match(msg); w.seen >= w.want {
		o.watch = nil
		go o.send(paintBarrier{w})
	}
}

// newObserver returns a [tea.Model] wrapping model, which records every
// message in a broker and serves [Harness.Mutate] requests.
func newObserver(model tea.Model) *observer {
	return &observer{
		started:    make(chan struct{}),
		firstEvent: make(chan struct{}),
		model:      model,
		messages:   broker.New[uv.Event](broker.WithMaxHistory(0)),
	}
}

func (o *observer) Init() tea.Cmd {
	close(o.started)
	return o.currentModel().Init()
}

// Update serves mutate requests itself, and passes other messages to the
// wrapped model before publishing them to the broker.
func (o *observer) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	// Not a message of the program's, so neither it nor the test sees it.
	if b, ok := msg.(paintBarrier); ok {
		b.watch.seq = o.outputSeq()
		close(b.watch.painted)
		return o, nil
	}

	// Recorded before taking the lock, which View waits on. Update only runs
	// on the program's goroutine, so the order is kept.
	if _, ok := msg.(mutateMsg); !ok && o.onMessage != nil {
		o.onMessage(msg)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if msg, ok := msg.(mutateMsg); ok {
		err := o.mutateLocked(msg)
		o.handled(msg)
		msg.done <- err
		return o, nil
	}

	next, cmd := o.model.Update(msg)
	if next != nil {
		o.model = next
	}
	o.handled(msg)
	o.messages.Publish(msg)
	o.firstEventOnce.Do(func() { close(o.firstEvent) })

	return o, cmd
}

func (o *observer) View() tea.View {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.model.View()
}

// currentModel returns the wrapped model, which callers must treat as
// read-only.
func (o *observer) currentModel() tea.Model {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.model
}

// mutateLocked applies req to the wrapped model. Caller must hold mu.
func (o *observer) mutateLocked(req mutateMsg) error {
	if model, ok := o.model.(mutatableModel); ok {
		return model.mutate(req)
	}

	next, err := req.apply(o.model)
	if err != nil {
		return err
	}
	model, ok := next.(tea.Model)
	if !ok {
		return fmt.Errorf("mutate function must return tea.Model for root harnesses, got %T", next)
	}
	o.model = model
	return nil
}

// mutateMsg is a [Harness.Mutate] request, which the observer serves on the
// program's goroutine instead of passing it to the model.
type mutateMsg struct {
	// apply returns the mutate function's result for model, or an error if
	// the function does not accept model's type.
	apply func(model any) (any, error)
	done  chan error
}

// mutatableModel is a wrapper that applies mutate requests to its own model.
type mutatableModel interface {
	mutate(mutateMsg) error
}

type simpleViewer interface {
	View() string
}

type simpleUpdater interface {
	Update(uv.Event) tea.Cmd
}

type replacementUpdater[T any] interface {
	Update(uv.Event) (T, tea.Cmd)
}

type initializer interface {
	Init() tea.Cmd
}

// componentWrapper adapts a component model, as [NewComponentHarness] takes,
// to a [tea.Model].
type componentWrapper[M any] struct {
	model M
}

func (cw *componentWrapper[M]) validate(tb testing.TB) {
	tb.Helper()

	if _, ok := any(cw.model).(simpleViewer); !ok {
		tb.Fatalf("model must implement View() string")
	}
	_, supportsCommandUpdate := any(cw.model).(simpleUpdater)
	_, supportsReplacementUpdate := any(cw.model).(replacementUpdater[M])
	if !supportsCommandUpdate && !supportsReplacementUpdate {
		tb.Fatalf("model must implement Update(uv.Msg) tea.Cmd or Update(uv.Msg) (%T, tea.Cmd)", cw.model)
	}
}

func (cw *componentWrapper[M]) Init() tea.Cmd {
	if m, ok := any(cw.model).(initializer); ok {
		return m.Init()
	}
	return nil
}

func (cw *componentWrapper[M]) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	switch m := any(cw.model).(type) {
	case replacementUpdater[M]:
		next, cmd := m.Update(msg)
		cw.model = next
		return cw, cmd
	case simpleUpdater:
		return cw, m.Update(msg)
	}

	return cw, nil
}

func (cw *componentWrapper[M]) View() tea.View {
	viewer, ok := any(cw.model).(simpleViewer)
	if !ok {
		panic("model must implement View() string")
	}

	return tea.NewView(viewer.View())
}

func (cw *componentWrapper[M]) mutate(req mutateMsg) error {
	next, err := req.apply(cw.model)
	if err != nil {
		return err
	}

	nextModel, ok := next.(M)
	if !ok {
		return fmt.Errorf("mutate function must return %T, got %T", cw.model, next)
	}

	cw.model = nextModel
	return nil
}
