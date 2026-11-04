// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/steep/internal/broker"
)

type observer struct {
	tb       testing.TB
	mu       sync.RWMutex
	model    tea.Model
	messages *broker.Broker[uv.Event]

	// firstEvent closes once the first message, other than a mutate
	// request, has been through Update.
	firstEvent     chan struct{}
	firstEventOnce sync.Once
	// lastViewSnapshot is the most recent View content.
	lastViewSnapshot string
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
func newObserver(tb testing.TB, model tea.Model) *observer {
	return &observer{
		tb:         tb,
		firstEvent: make(chan struct{}),
		model:      model,
		messages:   broker.New[uv.Event](broker.WithMaxHistory(0)),
	}
}

func (o *observer) Init() tea.Cmd {
	o.tb.Helper()
	return o.currentModel().Init()
}

// Update serves mutate requests itself, and passes other messages to the
// wrapped model before publishing them to the broker.
func (o *observer) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	o.tb.Helper()
	copiedMsg := msg

	// Not a message of the program's, so neither it nor the test sees it.
	if b, ok := msg.(paintBarrier); ok {
		b.watch.seq = o.outputSeq()
		close(b.watch.painted)
		return o, nil
	}

	// Recorded before taking the lock, which View waits on. Update only runs
	// on the program's goroutine, so the order is kept.
	if _, ok := msg.(mutateRequest); !ok && o.onMessage != nil {
		o.onMessage(msg)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if msg, ok := msg.(mutateRequest); ok {
		err := o.mutateLocked(msg)
		o.handled(msg)
		msg.respond(err)
		return o, nil
	}

	next, cmd := o.model.Update(msg)
	if next != nil {
		o.replaceLocked(next)
	}
	o.handled(copiedMsg)
	o.messages.Publish(copiedMsg)
	o.firstEventOnce.Do(func() { close(o.firstEvent) })

	return o, cmd
}

func (o *observer) View() tea.View {
	o.tb.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()

	view := o.model.View()
	o.lastViewSnapshot = view.Content
	return view
}

// currentModel returns the wrapped model, which callers must treat as
// read-only.
func (o *observer) currentModel() tea.Model {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.model
}

// replaceLocked sets the wrapped model, unwrapping an observer. Caller must
// hold mu.
func (o *observer) replaceLocked(model tea.Model) {
	if model == nil {
		return
	}

	if wrapper, ok := model.(*observer); ok {
		if wrapper == o {
			return
		}
		model = wrapper.currentModel()
	}

	o.model = model
}

// replace is replaceLocked, taking mu.
func (o *observer) replace(model tea.Model) {
	o.mu.Lock()
	o.replaceLocked(model)
	o.mu.Unlock()
}

// mutate applies req to the wrapped model.
func (o *observer) mutate(req mutateRequest) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.mutateLocked(req)
}

// mutateLocked is mutate with mu held.
func (o *observer) mutateLocked(req mutateRequest) error {
	if wrapper, ok := o.model.(*observer); ok && wrapper != o {
		return wrapper.mutate(req)
	}

	if model, ok := o.model.(mutatableModel); ok {
		return model.mutate(req)
	}

	next, err := req.mutateTeaModel(o.model)
	if err != nil {
		return err
	}
	o.replaceLocked(next)
	return nil
}
