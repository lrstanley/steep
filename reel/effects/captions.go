// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"slices"
	"time"

	"getsteep.dev/steep/reel"
)

// captionAccent is the default accent color of captions: a soft violet.
var captionAccent = color.NRGBA{R: 0xa7, G: 0x8b, B: 0xfa, A: 0xff}

// CaptionsOption configures [Captions].
type CaptionsOption func(*captions)

// DefaultCaptionPause pauses playback for d at each caption that does not
// set its own pause ([CaptionPause]), or for a reading time with
// [AutoPause], such as to give a demo's viewers time to read. Zero, the
// default, is no pause; any other d below zero is an error from Start.
func DefaultCaptionPause(d time.Duration) CaptionsOption {
	return func(c *captions) { c.pause = d }
}

// WithoutCaptionPauses leaves out every pause of the captions, those they
// set themselves included, so the recording plays at its own pace, such as
// for a test's recording of a demo's captions.
func WithoutCaptionPauses() CaptionsOption {
	return func(c *captions) { c.noPauses = true }
}

// WithCaptionAccent sets the color of a callout's edge and caret, of the
// dot on a caption, and of the ring a [Spotlight] draws around its anchor.
// Nil is the default, a soft violet.
func WithCaptionAccent(c color.Color) CaptionsOption {
	return func(e *captions) { e.accent = c }
}

// Captions returns an effect that draws the captions and callouts of the
// recording ([NewCaption], [NewCallout]) on the terminal frames, pausing
// where they say to (see [CaptionPause] and [DefaultCaptionPause]).
//
// A caption is a pill, a rounded box with its text at the top or bottom of
// the terminal's cell grid; a callout is a box with a caret pointing at
// cells of the screen. One caption and one callout show at a time: each
// replaces the one before it of its sort. A pause holds the frame as it was
// when its caption started, overlays such as [Keys] included.
//
// Captions that start before the timeline's window, or in what a [Hide]
// cuts, show from the first frame after, if they have not ended, without
// pausing. Stills ([reel.File.Marks]) show the captions of their moment.
//
// A profile draws its captions once: a Captions effect after another in the
// same profile does nothing (see also [HasCaptions]).
func Captions(opts ...CaptionsOption) reel.Effect {
	var c captions
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}

// HasCaptions reports whether fx holds a [Captions] effect, or one that
// wraps it (see [reel.Effect]), such as for a caller that adds a default one
// to a profile without its own.
func HasCaptions(fx []reel.Effect) bool {
	return slices.ContainsFunc(fx, func(e reel.Effect) bool {
		for e != nil {
			if _, ok := e.(captions); ok {
				return true
			}
			w, ok := e.(interface{ Unwrap() reel.Effect })
			if !ok {
				return false
			}
			e = w.Unwrap()
		}
		return false
	})
}

type captions struct {
	// pause is the default pause: zero for none, or AutoPause.
	pause    time.Duration
	noPauses bool
	accent   color.Color
}

func (captions) Priority() int { return reel.PriorityOverlay }

// Stills implements [reel.StillEffect].
func (captions) Stills() bool { return true }

// captionsKey marks a recording whose captions an effect draws.
type captionsKey struct{}

func (e captions) Start(ctx *reel.Context) (reel.Stage, error) {
	if e.pause < 0 && e.pause != AutoPause {
		return nil, fmt.Errorf("effects: caption pause %v below zero", e.pause)
	}
	if ctx.Value(captionsKey{}) != nil {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	ctx.SetValue(captionsKey{}, true)
	cards, err := NewCardRenderer(ctx)
	if err != nil {
		return nil, err
	}
	s := &captionsStage{
		ctx:    ctx,
		e:      e,
		accent: nrgbaOr(e.accent, captionAccent),
		cards:  cards,
		marks:  make(map[string][]time.Duration),
		last:   [2]int{-1, -1},
		shows:  [2]int{-1, -1},
	}
	s.from, _ = ctx.Timeline.Window()
	s.cuts, _ = ctx.Value(cutsKey{}).([]*cuts)
	ctx.OnClose(func() error { return errors.Join(s.errs...) })
	return s.run, nil
}

// captionsStage is the captions of one recording.
type captionsStage struct {
	ctx    *reel.Context
	e      captions
	accent color.NRGBA
	cards  *CardRenderer
	// from is the start of the timeline's window: captions that start
	// before it do not pause.
	from time.Duration
	// cuts are those of each [Hide] before Captions.
	cuts []*cuts
	// prev is the terminal frame before the one run is given, unless that
	// one is the first of its recording.
	prev reel.Frame
	// read is the number of events of the timeline read, which grows while
	// a [reel.Record] runs.
	read int
	// caps are the captions read, in timeline order. They start in order:
	// next is the first that has not.
	caps []*shownCaption
	next int
	// last is the latest caption read of each sort, and shows the one of each
	// sort that shows, or -1. At most one of each sort shows at a time, so
	// shows are all a frame looks at, whatever the number of captions.
	last, shows [2]int
	// ends are the caption ends, of captions and of callouts, in timeline
	// order.
	ends [2][]captionEnd
	// changes are the offsets of the events that can change the screen,
	// such as output, in timeline order.
	changes []time.Duration
	// marks are the offsets of the marks, by label, in timeline order.
	marks map[string][]time.Duration
	// errs are the captions that could not be read, reported when the
	// recording ends.
	errs []error
}

// shownCaption is a caption of the recording, and how it was laid out.
type shownCaption struct {
	*caption

	at time.Duration // Offset into the timeline.
	// seq is its index among the timeline's events.
	seq int
	// replaced is where the next caption of its sort starts, or openTo.
	replaced time.Duration
	// look is how it was laid out as it started, or nil.
	look *captionLook
}

// captionEnd is a caption end in the timeline.
type captionEnd struct {
	seq int // Its index among the timeline's events.
	at  time.Duration
}

// sort is the index of c's sort in [captionsStage.ends].
func (c *caption) sort() int {
	if c.callout() {
		return 1
	}
	return 0
}

// readEvents reads the captions, their ends, and the marks appended to the
// timeline since it last read.
func (s *captionsStage) readEvents() {
	for ev := range s.ctx.Timeline.EventsFrom(s.read) {
		seq := s.read
		s.read++
		switch ev := ev.(type) {
		case reel.Input, reel.Exit:
			// Neither changes the screen.
		case reel.Mark:
			s.marks[ev.Label] = append(s.marks[ev.Label], ev.At())
		case reel.Annotation:
			switch ev.Kind {
			case CaptionKind:
				c, err := readCaption(ev)
				if err != nil {
					s.errs = append(s.errs, fmt.Errorf("%w (at %v)", err, ev.At()))
					continue
				}
				if last := s.last[c.sort()]; last >= 0 {
					s.caps[last].replaced = ev.At()
				}
				s.last[c.sort()] = len(s.caps)
				s.caps = append(s.caps, &shownCaption{caption: c, at: ev.At(), seq: seq, replaced: openTo})
			case CaptionEndKind:
				callout, err := readCaptionEnd(ev)
				if err != nil {
					s.errs = append(s.errs, fmt.Errorf("%w (at %v)", err, ev.At()))
					continue
				}
				sort := 0
				if callout {
					sort = 1
				}
				s.ends[sort] = append(s.ends[sort], captionEnd{seq: seq, at: ev.At()})
			}
		default:
			// Output, a resize, a scroll, and the rest can change the screen.
			s.changes = append(s.changes, ev.At())
		}
	}
}

// endOf is the offset where caption i stops showing: the first of its own
// end, an end of its sort, the start of the next caption of its sort, and
// openTo.
func (s *captionsStage) endOf(i int) time.Duration {
	c := s.caps[i]
	end := c.replaced
	if c.end >= 0 {
		end = c.at + c.end
	}
	if marks := s.marks[c.until]; c.until != "" {
		if j, _ := slices.BinarySearch(marks, c.at); j < len(marks) {
			end = min(end, marks[j])
		}
	}
	// The first end of its sort placed after it, though at its offset.
	ends := s.ends[c.sort()]
	if j, _ := slices.BinarySearchFunc(ends, c.seq, func(e captionEnd, seq int) int { return cmp.Compare(e.seq, seq) }); j < len(ends) {
		end = min(end, ends[j].at)
	}
	return end
}

// shown returns the captions that show at offset at, oldest first: those
// of shows that have not ended by then, and pausing, the caption whose pause
// plays, or -1. Offsets asked for only grow, as frames come in timeline
// order.
func (s *captionsStage) shown(at time.Duration, pausing int) []int {
	out := make([]int, 0, len(s.shows))
	for _, i := range s.shows {
		if i >= 0 && (i == pausing || at < s.endOf(i)) {
			out = append(out, i)
		}
	}
	slices.Sort(out)
	return out
}

// pauseOf is how long playback pauses as c starts, on the delay grid.
func (s *captionsStage) pauseOf(c *shownCaption) time.Duration {
	if s.e.noPauses {
		return 0
	}
	p := c.pause
	if p == pauseDefault {
		p = s.e.pause
	}
	if p == AutoPause {
		p = readingTime(c.lines)
	}
	if p <= 0 {
		return 0
	}
	return max(s.ctx.Step, p.Round(s.ctx.Step))
}

// captionCut is a point in a frame where the captions shown change.
type captionCut struct {
	off time.Duration // Offset into the frame's delay, on the step grid.
	at  time.Duration // Offset into the timeline.
	// start is the caption that starts here, or -1 for an end.
	start int
}

func (s *captionsStage) run(f reel.Frame, emit func(reel.Frame)) {
	if f.Cells == nil {
		emit(f)
		return
	}
	s.readEvents()
	defer s.release(f.At)
	if f.Paused {
		// Another effect's pause, of the timeline at its moment: the
		// captions placed by then start, and show through it.
		s.startBefore(f.At, emit)
		for s.next < len(s.caps) && s.caps[s.next].at <= f.At {
			s.start(s.next, f, f.At, emit)
		}
		emit(s.frame(f, f.At, f.Delay, s.shown(f.At, -1)))
		return
	}
	s.startBefore(f.At, emit)
	// A still is a recording of its own, so the next has no frame before.
	s.prev = reel.Frame{}
	if !f.Last {
		defer func() { s.prev = f }()
	}
	end := f.At + time.Duration(float64(f.Delay)*s.ctx.Speed)
	cuts := []captionCut{{at: f.At, start: -1}}
	endCut := func(i int) {
		if e := s.endOf(i); e > f.At && e < end {
			cuts = append(cuts, captionCut{off: delayOffset(s.ctx, f, e), at: e, start: -1})
		}
	}
	for _, i := range s.shows {
		if i >= 0 {
			endCut(i)
		}
	}
	for i := s.next; i < len(s.caps) && s.caps[i].at < end; i++ {
		at := max(s.caps[i].at, f.At)
		cuts = append(cuts, captionCut{off: delayOffset(s.ctx, f, at), at: at, start: i})
		endCut(i)
	}
	slices.SortStableFunc(cuts, func(a, b captionCut) int {
		return cmp.Or(cmp.Compare(a.off, b.off), cmp.Compare(a.at, b.at))
	})

	// A piece is emitted once the next differs, so only one is held.
	var piece reel.Frame
	var pieceShows []int
	held := false
	flush := func() {
		if held {
			emit(s.frame(piece, piece.At, piece.Delay, pieceShows))
			held = false
		}
	}
	for k, c := range cuts {
		if c.start >= 0 {
			s.start(c.start, f, c.at, func(p reel.Frame) {
				flush()
				emit(p)
			})
		}
		next := f.Delay
		if k+1 < len(cuts) {
			next = cuts[k+1].off
		}
		if next <= c.off {
			continue
		}
		shows := s.shown(c.at, -1)
		if held && slices.Equal(shows, pieceShows) {
			piece.Delay += next - c.off
			continue
		}
		flush()
		piece, pieceShows, held = f, shows, true
		piece.At, piece.Delay = c.at, next-c.off
	}
	flush()
}

// startBefore starts the captions between the last frame's offset and at,
// the next frame's, on the last frame's screen where it is the one that
// showed when they were placed. The step grid can round the last frame's end
// to before them, as for a caption placed right before a key. Where the
// screen changed since that frame, in a frame the grid dropped, they start
// on the next frame instead.
func (s *captionsStage) startBefore(at time.Duration, emit func(reel.Frame)) {
	if s.prev.Cells == nil {
		return
	}
	since, _ := slices.BinarySearch(s.changes, s.prev.At+1)
	for s.next < len(s.caps) {
		c := s.caps[s.next]
		if c.at >= at || c.at < s.prev.At {
			return
		}
		if until, _ := slices.BinarySearch(s.changes, c.at+1); until > since {
			return
		}
		s.start(s.next, s.prev, c.at, emit)
	}
}

// start starts caption i, the next to, at offset at, laid out on the screen
// of g, and emits its pause, of g, if it pauses. It replaces the one of its
// sort that showed. A caption placed before the timeline's window, or in what
// a [Hide] cuts, starts late, without a pause. One that would neither pause
// nor show, being over by at, is not laid out.
func (s *captionsStage) start(i int, g reel.Frame, at time.Duration, emit func(reel.Frame)) {
	c := s.caps[i]
	s.next = i + 1
	late := c.at < s.from || hidden(s.cuts, c.at)
	pause := s.pauseOf(c)
	if late {
		pause = 0
	}
	if pause == 0 && s.endOf(i) <= at {
		return
	}
	c.look = s.layout(c, g.Cells, s.looksOf(s.shown(at, -1)))
	if c.look == nil {
		return
	}
	s.drop(c.sort())
	s.shows[c.sort()] = i
	if pause > 0 {
		g.Paused = true
		emit(s.frame(g, at, pause, s.shown(at, i)))
	}
}

// release drops the captions that show and are over by at. Frames come in
// timeline order, so those show no more.
func (s *captionsStage) release(at time.Duration) {
	for sort, i := range s.shows {
		if i >= 0 && s.endOf(i) <= at {
			s.drop(sort)
		}
	}
}

// drop drops the caption of a sort that shows, and its look.
func (s *captionsStage) drop(sort int) {
	if i := s.shows[sort]; i >= 0 {
		s.caps[i].look = nil
		s.shows[sort] = -1
	}
}

// looksOf returns the looks of the captions shows.
func (s *captionsStage) looksOf(shows []int) []*captionLook {
	looks := make([]*captionLook, 0, len(shows))
	for _, i := range shows {
		looks = append(looks, s.caps[i].look)
	}
	return looks
}

// frame returns f at offset at for delay, with the captions shows drawn on
// it.
func (s *captionsStage) frame(f reel.Frame, at, delay time.Duration, shows []int) reel.Frame {
	f.At, f.Delay = at, delay
	if len(shows) == 0 {
		return f
	}
	b := f.Cells.Screen.Bounds()
	// A caption laid out for another size of terminal is laid out again,
	// and left out if it no longer fits.
	for _, i := range shows {
		if c := s.caps[i]; c.look != nil && c.look.size != b.Size() {
			c.look = s.layout(c, f.Cells, nil)
		}
	}
	img := cloneNRGBA(f.Image)
	for _, i := range shows {
		if c := s.caps[i]; c.spotlight && c.look != nil && !c.look.pill {
			spotlight(img, f.Image, c.look.hole, s.accent, c.look.row)
			f.Isolated = true
		}
	}
	for _, i := range shows {
		if l := s.caps[i].look; l != nil {
			r := l.img.Bounds()
			draw.Draw(img, r.Sub(r.Min).Add(l.at), l.img, r.Min, draw.Over)
		}
	}
	f.Image = img
	return f
}

// cellRect is the pixel rectangle of the cells r covers in a grid of layout
// cells at grid.
func cellRect(grid image.Rectangle, cell image.Point, r image.Rectangle) image.Rectangle {
	return image.Rectangle{
		Min: grid.Min.Add(image.Pt(r.Min.X*cell.X, r.Min.Y*cell.Y)),
		Max: grid.Min.Add(image.Pt(r.Max.X*cell.X, r.Max.Y*cell.Y)),
	}
}
