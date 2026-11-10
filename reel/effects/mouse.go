// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"cmp"
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/lrstanley/steep/reel"
	"github.com/lrstanley/steep/reel/render"
	"github.com/lrstanley/steep/reel/vt"
)

const (
	// mouseGlide and mouseHide are the defaults of [WithPointerGlide] and
	// [WithPointerHide].
	mouseGlide = 150 * time.Millisecond
	mouseHide  = time.Second
	// mouseFade is how long a hidden pointer takes to fade out.
	mouseFade = 250 * time.Millisecond
	// mouseRipple is how long a press's ripple takes to spread and fade,
	// and a drag's trail to fade once it is released.
	mouseRipple = 400 * time.Millisecond
	// mouseRipples is how many ripples show at once, newest kept.
	mouseRipples = 4
	// mouseTrailStep is how far apart, in cell heights, the points of a
	// drawn trail are.
	mouseTrailStep = 0.25
	// mouseTrailSample is how often, in timeline time, a trail samples the
	// path the pointer took.
	mouseTrailSample = 4 * time.Millisecond
)

var (
	// pointerFill is the default color of the pointer.
	pointerFill = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	// buttonColors are the default colors of the buttons, by
	// [vt.MouseButton]: left blue, right orange, and middle green, xterm 75,
	// 215, and 114.
	buttonColors = [...]color.NRGBA{
		vt.MouseLeft:   {R: 0x5f, G: 0xaf, B: 0xff, A: 0xff},
		vt.MouseRight:  {R: 0xff, G: 0xaf, B: 0x5f, A: 0xff},
		vt.MouseMiddle: {R: 0x87, G: 0xd7, B: 0x87, A: 0xff},
	}
)

// PointerStyle is the shape of the pointer [Mouse] draws.
type PointerStyle int

const (
	// PointerArrow is an arrow with its tip on the center of the cell.
	PointerArrow PointerStyle = iota
	// PointerDot is a disc centered on the cell.
	PointerDot
)

// pointerStyleNames are the names of the pointer styles, by value.
var pointerStyleNames = []string{"arrow", "dot"}

// MouseParts is a set of the parts of the mouse [Mouse] draws.
type MouseParts uint8

const (
	// MousePointer is the pointer, which glides to the cell of each mouse
	// event and hides once the mouse is idle.
	MousePointer MouseParts = 1 << iota
	// MouseClicks is a ripple where a button is pressed, and a disc under
	// the pointer while it is held.
	MouseClicks
	// MouseDrags is a trail along the path of the pointer while a button is
	// held, which fades once it is released.
	MouseDrags

	// MouseAll is every part, the default.
	MouseAll = MousePointer | MouseClicks | MouseDrags
)

// mousePartNames are the names of the parts, by bit.
var mousePartNames = []string{"pointer", "clicks", "drags"}

// MouseOption configures [Mouse].
type MouseOption func(*mouse)

// WithPointerStyle sets the pointer's shape. The default is [PointerArrow].
// Any other value is an error from Start.
func WithPointerStyle(s PointerStyle) MouseOption {
	return func(m *mouse) { m.style = s }
}

// WithPointerSize sets the pointer's height in pixels, or its diameter for
// [PointerDot]. Zero, the default, is a cell's height for an arrow, and
// half a cell's height for a dot. Below zero is an error from
// Start.
func WithPointerSize(px int) MouseOption {
	return func(m *mouse) { m.size = px }
}

// WithPointerColor sets the color the pointer is filled with. Its outline
// is black or white, whichever contrasts more. The default, and a nil c, is
// white.
func WithPointerColor(c color.Color) MouseOption {
	return func(m *mouse) { m.pointer = nrgbaOr(c, pointerFill) }
}

// WithButtonColor sets the color of the ripples, held disc, and trail of
// button, one of [vt.MouseLeft], [vt.MouseMiddle], and [vt.MouseRight]; other
// buttons are ignored. The defaults are blue for left, green for middle,
// and orange for right, and a nil c restores button's.
func WithButtonColor(button vt.MouseButton, c color.Color) MouseOption {
	return func(m *mouse) {
		if isButton(button) {
			m.buttons[button] = nrgbaOr(c, buttonColors[button])
		}
	}
}

// WithPointerGlide sets how long, in playback time, the pointer takes to
// glide to the next cell. It glides as a hand moves a mouse: easing in and
// out along a gentle arc, and taking longer the farther it goes, up to four
// times as long across a wide screen. It sets off ahead of each event, so it
// arrives as the event is sent. With too little time for that, it glides up
// to twice as quick, then arrives late, and the events after a press wait
// for it to reach the press, a glide at most.
//
// Motion quicker than a glide after the motion before it, such as a drag's,
// and the release that ends it, are followed smoothly instead, the pointer
// keeping close behind them, so a drag that steps from cell to cell is drawn
// as a curve. The default is 150ms; zero or less moves the pointer to each
// event's cell at once.
func WithPointerGlide(d time.Duration) MouseOption {
	return func(m *mouse) { m.glide = d }
}

// WithPointerHide sets how long, in playback time, the pointer stays after
// the last mouse event while no button is held. It then fades out over
// 250ms, and fades in again where it was as it sets off for the next mouse
// event. The default is 1s; zero or less never hides it.
func WithPointerHide(d time.Duration) MouseOption {
	return func(m *mouse) { m.hide = d }
}

// WithMouseParts sets which parts of the mouse are drawn. The default is
// [MouseAll]; none draws nothing.
func WithMouseParts(p MouseParts) MouseOption {
	return func(m *mouse) { m.parts = p & MouseAll }
}

// Mouse returns an effect that draws the mouse on the terminal frames, from
// the mouse events of the timeline's [reel.Input] events, such as those of
// [reel.Session.Mouse], [reel.Session.Click], and [reel.Session.Drag]: a
// pointer that glides to the cell of each event, arriving as it is sent (see
// [WithPointerGlide]), a
// ripple where a button is pressed, a disc under the pointer while it is
// held, and a trail along the path of a drag, which fades once the button is
// released. Ripples, discs, and trails take the button's color (see
// [WithButtonColor]). The pointer hides once the mouse is idle (see
// [WithPointerHide]). Wheel turns and the extra buttons only move the
// pointer.
//
// A session sends a mouse event only when the program asked for it, so
// events the program never saw are not drawn either.
//
// The pointer is drawn where a [Zoom] draws its cell, and a [Highlight] does
// not dim it. With zooms that overlap in time, the pointer follows the last
// of them. The ripples of presses in what a [Hide] cuts are left out.
func Mouse(opts ...MouseOption) reel.Effect {
	m := mouse{
		pointer: pointerFill,
		buttons: buttonColors,
		glide:   mouseGlide,
		hide:    mouseHide,
		parts:   MouseAll,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&m)
		}
	}
	return m
}

type mouse struct {
	style       PointerStyle
	size        int
	pointer     color.NRGBA
	buttons     [len(buttonColors)]color.NRGBA
	glide, hide time.Duration
	parts       MouseParts
}

// check returns an error for options Start would reject.
func (e mouse) check() error {
	switch {
	case e.style < PointerArrow || e.style > PointerDot:
		return fmt.Errorf("effects: pointer style %d out of range", e.style)
	case e.size < 0:
		return fmt.Errorf("effects: pointer size %d is below zero", e.size)
	}
	return nil
}

func (mouse) Priority() int { return reel.PriorityOverlay }

func (e mouse) Start(ctx *reel.Context) (reel.Stage, error) {
	if err := e.check(); err != nil {
		return nil, err
	}
	if e.parts == 0 {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	// The options are in playback time, the stage works in timeline time.
	timeline := func(d time.Duration) time.Duration {
		return time.Duration(float64(max(d, 0)) * ctx.Speed)
	}
	size := ctx.Layout.Cell.Y
	if e.style == PointerDot {
		size = ctx.Layout.Cell.Y / 2
	}
	s := &mouseStage{
		ctx:     ctx,
		parts:   e.parts,
		buttons: e.buttons,
		glide:   timeline(e.glide),
		hide:    timeline(e.hide),
		fade:    timeline(mouseFade),
		ripple:  timeline(mouseRipple),
		aspect:  float64(ctx.Layout.Cell.X) / float64(max(ctx.Layout.Cell.Y, 1)),
		unit:    float64(ctx.Layout.Cell.Y),
		pointer: pointerSprite(e.style, cmp.Or(e.size, size), e.pointer),
	}
	s.zooms, _ = ctx.Value(zoomsKey{}).([]*zoomStage)
	s.cuts, _ = ctx.Value(cutsKey{}).([]*cuts)
	return s.run, nil
}

// isButton reports whether b is a button [Mouse] draws presses of.
func isButton(b vt.MouseButton) bool {
	return b == vt.MouseLeft || b == vt.MouseMiddle || b == vt.MouseRight
}

// nrgbaOr is c as NRGBA, or def when c is nil.
func nrgbaOr(c color.Color, def color.NRGBA) color.NRGBA {
	if c == nil {
		return def
	}
	n, _ := color.NRGBAModel.Convert(c).(color.NRGBA)
	return n
}

// mouseStage is the mouse of one recording.
type mouseStage struct {
	ctx     *reel.Context
	parts   MouseParts
	buttons [len(buttonColors)]color.NRGBA
	// glide, hide, fade, and ripple are in timeline time. glide is the
	// shortest glide, and a zero one moves the pointer at once. A zero hide
	// never hides the pointer.
	glide, hide, fade, ripple time.Duration
	// aspect is a cell's width over its height, which glides measure
	// distance by.
	aspect float64
	// unit is the height of a cell of the terminal font, which ripples,
	// discs, and trails are sized by.
	unit float64
	// pointer is the pointer's image, with its hotspot at (0, 0).
	pointer *image.NRGBA
	// zooms are those of each [Zoom] before Mouse, and cuts those of each
	// [Hide].
	zooms []*zoomStage
	cuts  []*cuts

	// events are the mouse events read from the timeline, oldest first, and
	// read is the number of timeline events read.
	events []mouseEvent
	read   int
	// reached is when the pointer reaches the cell of the last press, which
	// later events wait for, up to a glide, and late is the most any press
	// was reached after it was sent.
	reached, late time.Duration

	// layout is the rasterizer's layout for screens of size.
	layout render.Layout
	size   image.Point

	// mask is the trail mask last drawn and what it was drawn for. A trail
	// fading out, or one whose pointer holds still, draws the same mask
	// frame after frame.
	mask    *image.Alpha
	maskFor trailMaskKey
}

// trailMaskKey is what a trail mask depends on. A trail's alpha does not
// change its mask, so it is left zero.
type trailMaskKey struct {
	tr   trailLook
	view cellView
	grid image.Rectangle
}

// vec is a point in cells, or in pixels once a [cellView] maps it.
type vec struct{ x, y float64 }

// mouseEvent is a mouse event at offset at into the timeline, with what it
// left the mouse doing.
type mouseEvent struct {
	vt.MouseEvent

	at time.Duration
	// move is how the pointer moves to the event's cell. It starts once the
	// event before it was sent and the event is known, so the pointer
	// arrives as it is sent when there is time to, or as soon after as it
	// can. landed is when it reaches the cell, or at if sooner.
	move   motion
	landed time.Duration
	// shown is the pointer's opacity, from 0 to 1, as move starts. A pointer
	// that had hidden fades in from there as it moves.
	shown float64
	// held is the button held after the event, or [vt.MouseUnknown].
	held vt.MouseButton
	// drag is the index of the event that started the drag the event is
	// part of, its release included, or -1.
	drag int
}

// cell is the center of the event's cell.
func (ev mouseEvent) cell() vec { return vec{float64(ev.X) + 0.5, float64(ev.Y) + 0.5} }

func (s *mouseStage) run(f reel.Frame, emit func(reel.Frame)) {
	if f.Cells == nil {
		emit(f)
		return
	}
	// The pointer moves to an event once it is read, so it can arrive as
	// the event is sent. A complete timeline is read at once. A live one is
	// read up to the end of the frame, as far as it is sure to have grown,
	// so the pointer never changes what a frame before showed, and moves
	// the same however far ahead the recording is.
	end := time.Duration(math.MaxInt64)
	if s.ctx.Live {
		end = f.At
		if !f.Paused {
			end += s.timeline(f.Delay)
		}
	}
	s.readEvents(f.At, end)
	if len(s.events) == 0 {
		emit(f)
		return
	}
	if size := f.Cells.Screen.Bounds().Size(); size != s.size {
		s.layout, s.size = s.ctx.Rasterizer.Layout(size.X, size.Y), size
	}
	grid := s.layout.Grid.Add(f.Image.Rect.Min)
	// The mouse holds still from settled on, until the next event.
	last := s.events[len(s.events)-1]
	settled := max(last.move.start+max(last.move.dur, s.fade), last.landed+s.ripple, last.at+s.hide+s.fade)
	var cuts []time.Duration
	for p := time.Duration(0); p < f.Delay && f.At+s.timeline(p) <= settled; p += s.ctx.Step {
		cuts = append(cuts, p)
	}
	splitFrame(s.ctx, f, trimCuts(cuts, f.Delay, s.ctx.Step), func(p time.Duration) mouseLook {
		return s.lookAt(f, grid, p)
	}, func(l mouseLook) *image.NRGBA {
		return s.draw(f.Image, grid, l)
	}, emit)
}

// timeline is the timeline time playback time p plays.
func (s *mouseStage) timeline(p time.Duration) time.Duration {
	return time.Duration(float64(p) * s.ctx.Speed)
}

// readEvents adds the mouse events of the inputs appended to the timeline
// since it last read, up to those at offset end or later, unless at now,
// the start of the frame they are read for.
func (s *mouseStage) readEvents(now, end time.Duration) {
	known := time.Duration(0)
	if s.ctx.Live {
		known = now
	}
	for ev := range s.ctx.Timeline.EventsFrom(s.read) {
		if at := ev.At(); at >= end && at > now {
			return
		}
		s.read++
		in, ok := ev.(reel.Input)
		if !ok {
			continue
		}
		for _, st := range vt.DecodeInput(in.Data) {
			if st.Kind == vt.StrokeMouse {
				s.add(in.At(), known, st.Mouse)
			}
		}
	}
}

// add appends the mouse event m at offset at, known from offset known on.
func (s *mouseStage) add(at, known time.Duration, m vt.MouseEvent) {
	ev := mouseEvent{MouseEvent: m, at: at, drag: -1}
	to := ev.cell()
	prev := mouseEvent{drag: -1}
	// The pointer leaves for the event's cell once it is known, the event
	// before it was sent, and the pointer reached the last press, waiting a
	// glide at most.
	from := known
	n := len(s.events)
	if n > 0 {
		prev = s.events[n-1]
		from = max(from, prev.at, prev.move.start, min(s.reached, at+s.glide))
	}
	p, v, a := s.state(from)
	if n == 0 {
		p = to
	}
	hidden := s.alpha(at) < 1
	mv := motion{start: from, from: p, vel: v, acc: a, to: to}
	switch d := at - prev.at; {
	case s.glide <= 0:
		mv = motion{start: max(from, at), from: to, to: to}
	case n > 0 && m.Action != vt.MousePress && prev.Action == vt.MouseMotion && 0 < d && d < s.glide:
		// Motion quicker than a glide, such as a drag's, would stop and
		// start the pointer on every event. Following it as a spring
		// does smooths it into a curve, which keeps close behind it.
		mv.omega = followStiffness / s.glide.Seconds()
		mv.dur = springRest(p, v, to, mv.omega)
	default:
		// Glide to arrive as the event is sent: as long as the distance
		// calls for, and a pointer that had hidden fades in first. With
		// less time than that, glide quicker, up to twice as quick, then
		// arrive late. A pointer still moving as the event is sent leaves
		// at once; one at rest by then may wait there, as its state does
		// not change before it leaves.
		rest := n == 0
		if n > 0 {
			if end := prev.move.start + prev.move.dur; end <= max(from, at) {
				from, rest = max(from, end), true
				p, v, a = s.state(from)
			}
		}
		mv.from, mv.vel, mv.acc = p, v, a
		mv.dur = s.reach(p, v, to)
		lead := mv.dur
		if hidden {
			lead = max(lead, s.fade)
		}
		mv.start = from
		if rest {
			mv.start = max(from, at-lead)
		}
		if room := at - from; room < mv.dur {
			mv.dur = max(room, mv.dur/2)
		}
		mv.bow = bowOf(mv.from, to, s.aspect)
	}
	ev.move, ev.landed = mv, at
	if mv.omega == 0 {
		ev.landed = max(at, mv.start+mv.dur)
	}
	ev.shown = s.alpha(mv.start)
	if m.Action == vt.MousePress {
		s.reached = ev.landed
		s.late = max(s.late, ev.landed-at)
	}
	switch {
	case m.Action == vt.MousePress && isButton(m.Button):
		ev.held, ev.drag = m.Button, len(s.events)
	case m.Action == vt.MouseMotion && isButton(m.Button):
		// Motion with a button held drags, even without a press before it.
		ev.held, ev.drag = m.Button, prev.drag
		if prev.held != m.Button {
			ev.drag = len(s.events)
		}
	case m.Action == vt.MouseRelease && (m.Button == prev.held || m.Button == vt.MouseUnknown):
		// X10 and urxvt releases do not say which button was let go.
		ev.drag = prev.drag
	case m.Action == vt.MouseMotion && m.Button == vt.MouseUnknown:
		// Motion with no button held.
	case prev.held != vt.MouseUnknown:
		// A wheel turn, an extra button, or the release of a button other
		// than the one held changes nothing held.
		ev.held, ev.drag = prev.held, prev.drag
	}
	s.events = append(s.events, ev)
}

// reach is how long a glide from p, moving at v, to to takes: the shortest
// glide to the next cell, longer the farther it goes (see [glideReach]), up
// to [glideLongest] times that, and none for a pointer at rest there.
func (s *mouseStage) reach(p, v, to vec) time.Duration {
	gap := to.sub(p)
	dist := math.Hypot(gap.x*s.aspect, gap.y)
	if dist < restSlack && v.len()*s.glide.Seconds() < restSlack {
		return 0
	}
	return time.Duration(float64(s.glide) * min(1+glideReach*math.Log2(1+dist), glideLongest))
}

// moving is the index of the last event the pointer has started moving to
// by offset t, or -1.
func (s *mouseStage) moving(t time.Duration) int {
	return sort.Search(len(s.events), func(i int) bool { return s.events[i].move.start > t }) - 1
}

// sent is the index of the last event sent by offset t, or -1.
func (s *mouseStage) sent(t time.Duration) int {
	return sort.Search(len(s.events), func(i int) bool { return s.events[i].at > t }) - 1
}

// state is the pointer's position, velocity, and acceleration at offset t,
// at rest before it first moves.
func (s *mouseStage) state(t time.Duration) (p, v, a vec) {
	j := s.moving(t)
	if j < 0 {
		return vec{}, vec{}, vec{}
	}
	return s.events[j].move.state(t)
}

// pos is where the pointer is at offset t.
func (s *mouseStage) pos(t time.Duration) vec {
	p, _, _ := s.state(t)
	return p
}

// alpha is the pointer's opacity at offset t, from 0 to 1: fading in as it
// moves to an event after it had hidden, and fading out once the mouse is
// idle with no button held.
func (s *mouseStage) alpha(t time.Duration) float64 {
	j := s.moving(t)
	if j < 0 {
		return 0
	}
	ev := s.events[j]
	a := ev.shown + (1-ev.shown)*(1-eased(t-ev.move.start, s.fade))
	if idle := t - ev.at - s.hide; s.hide > 0 && ev.held == vt.MouseUnknown && idle > 0 {
		a = min(a, eased(idle, s.fade))
	}
	return a
}

// mouseLook is what the mouse shows at one moment, mapped onto the frame by
// view. The zero mouseLook shows nothing.
type mouseLook struct {
	view cellView
	// pos is the pointer's position, drawn at opacity alpha, and with a
	// disc under it in the color of the button held.
	pos     vec
	alpha   uint8
	held    vt.MouseButton
	ripples [mouseRipples]rippleLook
	trail   trailLook
}

// rippleLook is a ripple of a press of button, age of the way through.
// A zero button is no ripple.
type rippleLook struct {
	at     vec
	button vt.MouseButton
	age    uint8
}

// trailLook is the trail of the drag the event first started: the path the
// pointer took from offset from to until, at opacity alpha. A zero alpha is
// no trail.
type trailLook struct {
	first       int
	from, until time.Duration
	alpha       uint8
}

// cellView maps cells onto a frame's image: column x to x0+w*x, and row y
// to y0+h*y.
type cellView struct{ x0, y0, w, h float64 }

func (v cellView) at(c vec) vec { return vec{v.x0 + v.w*c.x, v.y0 + v.h*c.y} }

// lookAt is the look at playback offset p into f, a frame drawn with its
// cell grid at grid.
func (s *mouseStage) lookAt(f reel.Frame, grid image.Rectangle, p time.Duration) mouseLook {
	t := f.At + s.timeline(p)
	l := s.look(t)
	if l == (mouseLook{}) {
		return l
	}
	l.view = cellView{x0: float64(grid.Min.X), y0: float64(grid.Min.Y), w: float64(s.layout.Cell.X), h: float64(s.layout.Cell.Y)}
	// A zoom draws a piece of a frame as of the middle of its first step,
	// or a paused one as of its start.
	if !f.Paused {
		t += s.timeline(s.ctx.Step / 2)
	}
	for _, z := range slices.Backward(s.zooms) {
		if x, y, ok := z.axes(t); ok {
			l.view = cellView{x0: l.view.x0 + x.off, y0: l.view.y0 + y.off, w: l.view.w * x.k, h: l.view.h * y.k}
			break
		}
	}
	return l
}

// look is what the mouse shows at offset t, with a zero view.
func (s *mouseStage) look(t time.Duration) mouseLook {
	if s.moving(t) < 0 {
		return mouseLook{}
	}
	l := mouseLook{pos: s.pos(t)}
	if s.parts&MousePointer != 0 {
		l.alpha = uint8(math.Round(0xff * s.alpha(t)))
	}
	if i := s.sent(t); i >= 0 {
		if s.parts&MouseClicks != 0 {
			l.held = s.events[i].held
			n := 0
			for j := i; j >= 0 && n < mouseRipples && t-s.events[j].at < s.ripple+s.late; j-- {
				e := s.events[j]
				if age := t - e.landed; age >= 0 && age < s.ripple && e.Action == vt.MousePress && isButton(e.Button) && !hidden(s.cuts, e.at) {
					l.ripples[n] = rippleLook{at: e.cell(), button: e.Button, age: uint8(0xff * float64(age) / float64(s.ripple))}
					n++
				}
			}
		}
		if s.parts&MouseDrags != 0 {
			l.trail = s.trail(i, t)
		}
	}
	if l.alpha == 0 && l.held == vt.MouseUnknown && l.ripples[0].button == vt.MouseUnknown && l.trail.alpha == 0 {
		return mouseLook{}
	}
	return l
}

// trail is the trail at offset t, after event i: of the drag going on, or
// of one released less than a ripple before t. A drag that never moved has
// none. The trail is the path the pointer took, from when it reached the
// drag's start to where it is, or to where it came to rest after the
// release, so it never runs ahead of the pointer, nor follows it on.
func (s *mouseStage) trail(i int, t time.Duration) trailLook {
	r := i
	for r >= 0 && s.events[r].drag < 0 && t-s.events[r].at < s.ripple {
		r--
	}
	if r < 0 || s.events[r].drag < 0 {
		return trailLook{}
	}
	ev := s.events[r]
	alpha := 1.0
	until := t
	switch {
	case ev.held == vt.MouseUnknown:
		alpha = eased(t-ev.at, s.ripple)
		until = min(until, ev.move.start+ev.move.dur)
		if r+1 < len(s.events) {
			until = min(until, s.events[r+1].move.start)
		}
	case r < i:
		// The button was let go without a release the encoding reported.
		return trailLook{}
	}
	// The pointer may have set off for motion of the drag not sent yet. At
	// rest, it draws the same trail.
	j := s.moving(until)
	until = min(until, s.events[j].move.start+s.events[j].move.dur)
	start := s.events[ev.drag]
	moved := func(e mouseEvent) bool { return e.drag == ev.drag && (e.X != start.X || e.Y != start.Y) }
	if alpha == 0 || until <= start.landed || !slices.ContainsFunc(s.events[ev.drag:max(r, j)+1], moved) {
		return trailLook{}
	}
	return trailLook{first: ev.drag, from: start.landed, until: until, alpha: uint8(math.Round(0xff * alpha))}
}

// trailPath is the path tr draws, in cells: where the pointer was every
// [mouseTrailSample] from tr.from, and at tr.until. A pointer at rest adds
// one point, however long a drag holds still.
func (s *mouseStage) trailPath(tr trailLook) []vec {
	var path []vec
	j := s.moving(tr.from)
	for t := tr.from; t < tr.until; t += mouseTrailSample {
		for j+1 < len(s.events) && s.events[j+1].move.start <= t {
			j++
		}
		mv := s.events[j].move
		if t < mv.start+mv.dur {
			p, _, _ := mv.state(t)
			path = append(path, p)
			continue
		}
		if len(path) == 0 || path[len(path)-1] != mv.to {
			path = append(path, mv.to)
		}
		if j+1 == len(s.events) {
			break
		}
		// Skip to the last sample before the pointer moves again.
		if gap := s.events[j+1].move.start - t; gap > mouseTrailSample {
			t += gap - 1 - (gap-1)%mouseTrailSample
		}
	}
	return append(path, s.pos(tr.until))
}

// trailMask is the coverage of the trail tr, mapped onto a frame by view
// and cut to grid. The mask is shared with later calls, and is not to be
// changed.
func (s *mouseStage) trailMask(tr trailLook, view cellView, grid image.Rectangle) *image.Alpha {
	tr.alpha = 0
	key := trailMaskKey{tr: tr, view: view, grid: grid}
	if s.mask != nil && key == s.maskFor {
		return s.mask
	}
	cells := s.trailPath(tr)
	path := make([]vec, len(cells))
	for k, c := range cells {
		path[k] = view.at(c)
	}
	path = resample(path, view.h*mouseTrailStep)
	// cover scans each shape's box, and the legs are a fraction of a cell
	// long, so each is a capsule of its own rather than one shape for the
	// trail.
	segs := make([]shape, max(1, len(path)-1))
	var box image.Rectangle
	for k := range segs {
		segs[k] = capsule(path[k], path[min(k+1, len(path)-1)], s.unit/4)
		box = box.Union(segs[k].box)
	}
	// A zoom may put much of the trail off the grid.
	mask := image.NewAlpha(box.Intersect(grid))
	for _, sh := range segs {
		cover(mask, sh)
	}
	s.mask, s.maskFor = mask, key
	return mask
}

// draw returns a copy of src, a frame with its cell grid at grid, with the
// mouse l shows drawn on it, or src when it shows nothing. Ripples and the
// pointer the view puts outside the grid, such as past a zoom's edge, are
// left out, and the trail is cut to the grid.
func (s *mouseStage) draw(src *image.NRGBA, grid image.Rectangle, l mouseLook) *image.NRGBA {
	if l == (mouseLook{}) {
		return src
	}
	img := cloneNRGBA(src)
	shown := func(p vec) bool { return image.Pt(int(math.Floor(p.x)), int(math.Floor(p.y))).In(grid) }
	if tr := l.trail; tr.alpha > 0 {
		c := s.buttons[s.events[tr.first].held]
		paintMask(img, s.trailMask(tr, l.view, grid), faded(c, 0.5*float64(tr.alpha)/0xff))
	}
	for _, r := range l.ripples {
		if at := l.view.at(r.at); r.button != vt.MouseUnknown && shown(at) {
			u := float64(r.age) / 0xff
			radius := s.unit * (0.3 + 0.9*(1-(1-u)*(1-u)))
			paintMask(img, maskOf(ring(at, radius, max(1.5, s.unit/8))), faded(s.buttons[r.button], 1-u))
		}
	}
	at := l.view.at(l.pos)
	if !shown(at) {
		return img
	}
	if l.held != vt.MouseUnknown {
		paintMask(img, maskOf(disc(at, s.unit*0.45)), faded(s.buttons[l.held], 0.56))
	}
	if l.alpha > 0 {
		tip := image.Pt(int(math.Round(at.x)), int(math.Round(at.y)))
		drawFaded(img, s.pointer, tip.Add(s.pointer.Rect.Min), l.alpha)
	}
	return img
}

// faded is c with its opacity scaled by keep, from 0 to 1.
func faded(c color.NRGBA, keep float64) color.NRGBA {
	c.A = uint8(math.Round(float64(c.A) * keep))
	return c
}

// pointerArrow is the outline of [PointerArrow], with its tip at (0, 0),
// pointerArrowHeight high.
var pointerArrow = []vec{{0, 0}, {0, 15}, {3.6, 11.6}, {6.2, 17.4}, {8.6, 16.4}, {6.1, 10.8}, {10.8, 10.8}}

const pointerArrowHeight = 17.4

// pointerSprite returns the pointer of style, size pixels high, filled with
// fill and outlined in black or white, whichever contrasts more, with its
// hotspot at (0, 0).
func pointerSprite(style PointerStyle, size int, fill color.NRGBA) *image.NRGBA {
	h := float64(max(size, 4))
	edge := max(1, h/16)
	var sh shape
	if style == PointerDot {
		sh = disc(vec{}, h/2)
	} else {
		pts := make([]vec, len(pointerArrow))
		for i, p := range pointerArrow {
			pts[i] = vec{p.x * h / pointerArrowHeight, p.y * h / pointerArrowHeight}
		}
		sh = polygon(pts)
	}
	outline := sh.grow(edge)
	img := image.NewNRGBA(outline.box)
	edgeColor := color.NRGBA{A: 0xff}
	if luma(fill) < 0x80 {
		edgeColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	paintMask(img, maskOf(outline), edgeColor)
	paintMask(img, maskOf(sh), fill)
	return img
}

// luma is the perceived brightness of c, from 0 to 255.
func luma(c color.NRGBA) float64 {
	return 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
}
