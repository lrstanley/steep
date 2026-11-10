// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/lrstanley/steep/reel"
	"github.com/lrstanley/steep/reel/vt"
)

// Button codes of SGR mouse reports: a press of left or right, motion
// with left held, and motion with no button held.
const (
	sgrLeft       = 0
	sgrRight      = 2
	sgrLeftMotion = 32
	sgrHover      = 35
)

// sgrMouse is the SGR mouse report of button code b at zero-based cell
// (x, y), a release when release is set.
func sgrMouse(b, x, y int, release bool) string {
	final := "M"
	if release {
		final = "m"
	}
	return fmt.Sprintf("\x1b[<%d;%d;%d%s", b, x+1, y+1, final)
}

// mouseScreen is the output the mouse tests draw over.
const mouseScreen = "$ ls\r\nREADME  main.go\r\n$ "

// mouseFrames exports tl with a renderer at 20 FPS through effects, and
// returns the frames the effects were given, and those they emitted.
func mouseFrames(t *testing.T, tl *reel.Timeline, effects ...reel.Effect) (in, out []reel.Frame) {
	t.Helper()
	return zoomFrames(t, tl, 20, zoomRenderer(t, zoomRenderOpts...), zoomRenderOpts, effects...)
}

// assertMouseFrames checks that out plays as long as in, and that the
// frames out from offset hidden on are in's own, the mouse having hidden.
func assertMouseFrames(t *testing.T, in, out []reel.Frame, hidden time.Duration) {
	t.Helper()
	if got, want := totalDelay(out), totalDelay(in); got != want {
		t.Fatalf("frames play for %v, want %v", got, want)
	}
	for i, f := range out {
		if f.At >= hidden && !slices.ContainsFunc(in, func(g reel.Frame) bool { return g.Image == f.Image }) {
			t.Fatalf("frame %d at %v still shows the mouse", i, f.At)
		}
	}
}

// TestMouseClick checks a left click: the pointer fades in on its cell
// before it, then shows a blue ripple that spreads and fades on every step,
// then hides.
func TestMouseClick(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 16, 3, "",
		output(0, mouseScreen),
		input(400, sgrMouse(sgrLeft, 3, 1, false)+sgrMouse(sgrLeft, 3, 1, true)),
		output(3000, " "),
	)
	in, out := mouseFrames(t, tl, Mouse(WithPointerHide(time.Second)))
	// The pointer sets off to fade in 250ms before the click, first showing
	// on the step after.
	if out[0].Image != in[0].Image || out[0].Delay != 200*ms {
		t.Fatalf("first frame %+v, want the terminal's own until the pointer fades in", infoOf(out[:1]))
	}
	// The pointer hides a second after the click, and fades for 250ms.
	assertMouseFrames(t, in, out, 1650*ms)
	assertGolden(t, "mouse-click", out[1:13])
}

// TestMouseDrag checks a left drag down and across, one cell per step: the
// trail follows the pointer through the cells sent, smoothed where they
// step on the grid, the held disc under the pointer at its end, and the
// trail fades once the button is released.
func TestMouseDrag(t *testing.T) {
	t.Parallel()
	events := []event{output(0, mouseScreen), input(100, sgrMouse(sgrLeft, 1, 0, false))}
	for x := 2; x <= 12; x++ {
		// The cells of the line to (12, 2), as reel.Session.Drag sends them.
		y := int(math.Round(float64(x-1) * 2 / 11))
		events = append(events, input(100+40*(x-1), sgrMouse(sgrLeftMotion, x, y, false)))
	}
	events = append(events, input(600, sgrMouse(sgrLeft, 12, 2, true)), output(4000, " "))
	tl := buildTimeline(t, 16, 3, "", events...)
	in, out := mouseFrames(t, tl, Mouse())
	// The pointer hides a second after the release, and fades for 250ms.
	assertMouseFrames(t, in, out, 1850*ms)
	assertGolden(t, "mouse-drag", slices.Concat(out[1:12], out[14:16]))
}

// testStage returns a mouse stage with the shortest glide glide, cells half
// as wide as high, and the defaults' other timings, but a fade of 100ms.
func testStage(glide time.Duration) *mouseStage {
	return &mouseStage{
		parts: MouseAll, glide: glide, hide: time.Second, fade: 100 * ms, ripple: mouseRipple,
		aspect: 0.5, unit: 20,
	}
}

// addAll adds each event to s, as known from the start.
func addAll(s *mouseStage, events ...mouseEvent) {
	for _, ev := range events {
		s.add(ev.at, 0, ev.MouseEvent)
	}
}

// mev is a mouse event of action and button at (x, y), at offset at.
func mev(at time.Duration, action vt.MouseAction, button vt.MouseButton, x, y int) mouseEvent {
	return mouseEvent{at: at, Action: action, Button: button, X: x, Y: y}
}

// assertSmooth checks that the pointer moves smoothly from offset from to
// to, sampled every millisecond: no jump in its position, nor in its
// velocity, as a snap or a stop and start would make.
func assertSmooth(t *testing.T, s *mouseStage, from, to time.Duration) {
	t.Helper()
	prev, pv := s.pos(from), vec{}
	for at := from + ms; at <= to; at += ms {
		p := s.pos(at)
		v := p.sub(prev)
		if d := v.len(); d > 0.5 {
			t.Fatalf("at %v: pointer jumps %v cells in 1ms, from %v to %v", at, d, prev, p)
		}
		if at > from+ms {
			if dv := v.sub(pv).len(); dv > 0.01 {
				t.Fatalf("at %v: pointer's speed jumps by %v cells/ms", at, dv)
			}
		}
		prev, pv = p, v
	}
}

// TestMouseFastDrag checks that, with the defaults, the pointer follows a
// drag of motion quicker than its glide, as [reel.Session.Drag] sends it,
// smoothly: it reaches the press's cell before moving on, even with motion
// at the same offset waiting for it, then stays close behind the latest
// event's cell, at the end of the trail, and comes to rest on the release
// cell soon after the release.
func TestMouseFastDrag(t *testing.T) {
	t.Parallel()
	events := []event{
		output(0, mouseScreen),
		input(0, sgrMouse(sgrHover, 20, 8, false)),
		input(100, sgrMouse(sgrLeft, 2, 4, false)),
	}
	for x := 3; x <= 50; x++ {
		events = append(events, input(100+40*(x-3), sgrMouse(sgrLeftMotion, x, 4, false)))
		if x == 6 {
			events = append(events, input(220, sgrMouse(sgrLeftMotion, 7, 4, false)))
		}
	}
	events = append(events, input(2000, sgrMouse(sgrLeft, 50, 4, true)))
	tl := buildTimeline(t, 60, 10, "", events...)
	s := testStage(mouseGlide)
	s.fade = mouseFade
	s.ctx = &reel.Context{Timeline: tl}
	s.readEvents(0, math.MaxInt64)
	press := s.events[1]
	if l := s.look(press.landed); l.pos != (vec{2.5, 4.5}) {
		t.Errorf("once the glide to the press ends: pointer at %v, want on its cell", l.pos)
	}
	for _, ev := range s.events[2:] {
		if ev.move.start < press.landed {
			t.Errorf("motion at %v leaves at %v, before the pointer reaches the press at %v", ev.at, ev.move.start, press.landed)
		}
	}
	for _, at := range []time.Duration{500 * ms, 1010 * ms, 1555 * ms, 1999 * ms} {
		latest := s.events[s.sent(at)].cell()
		l := s.look(at)
		if l.pos.x < latest.x-2 || l.pos.x > latest.x+1 || math.Abs(l.pos.y-latest.y) > 1e-9 {
			t.Errorf("at %v: pointer at %v, want close behind %v", at, l.pos, latest)
		}
		if path := s.trailPath(l.trail); path[len(path)-1] != l.pos {
			t.Errorf("at %v: trail ends at %v, want at the pointer, %v", at, path[len(path)-1], l.pos)
		}
	}
	if l := s.look(2300 * ms); l.pos != (vec{50.5, 4.5}) {
		t.Errorf("300ms after the release: pointer at %v, want on its cell", l.pos)
	}
	assertSmooth(t, s, press.landed, 2300*ms)
}

// TestResample checks that a path is resampled up to its end, even when its
// last leg is shorter than a step, and that a path with no length is its
// start alone.
func TestResample(t *testing.T) {
	t.Parallel()
	if got, want := resample([]vec{{0, 0}, {2.5, 0}}, 1), []vec{{0, 0}, {1, 0}, {2, 0}, {2.5, 0}}; !slices.Equal(got, want) {
		t.Errorf("resampled: %v, want %v", got, want)
	}
	if got := resample([]vec{{1, 1}, {1, 1}}, 1); !slices.Equal(got, []vec{{1, 1}}) {
		t.Errorf("no length: %v, want its start", got)
	}
}

// TestMotion checks the pointer's motions: that a glide and a spring start
// from the state they are given, that a glide comes to rest on its cell at
// its end, bowed out of its line midway, that a spring is at rest by its
// end, and that each one's velocity and acceleration are those its
// position changes by.
func TestMotion(t *testing.T) {
	t.Parallel()
	near := func(a, b vec, tol float64) bool { return a.sub(b).len() <= tol }
	from, vel, acc, to := vec{1, 2}, vec{30, -10}, vec{-200, 50}, vec{20, 8}
	glide := motion{start: time.Second, dur: 300 * ms, from: from, vel: vel, acc: acc, to: to, bow: bowOf(from, to, 0.5)}
	spring := motion{start: time.Second, from: from, vel: vel, to: to, omega: followStiffness / mouseGlide.Seconds()}
	spring.dur = springRest(from, vel, to, spring.omega)
	for _, tc := range []struct {
		name string
		m    motion
	}{{"glide", glide}, {"spring", spring}} {
		m := tc.m
		if p, v, a := m.state(m.start); !near(p, from, 1e-9) || !near(v, vel, 1e-9) || (m.omega == 0 && !near(a, acc, 1e-9)) {
			t.Errorf("%s: starts at %v, moving %v, accelerating %v; want %v, %v, %v", tc.name, p, v, a, from, vel, acc)
		}
		if p, _, _ := m.state(m.start + m.dur - ms/10); !near(p, to, 0.01) {
			t.Errorf("%s: at %v by its end, want at rest on %v", tc.name, p, to)
		}
		const h = 1e-4
		for k := 1; k < 20; k++ {
			at := m.start + m.dur*time.Duration(k)/20
			p, v, a := m.state(at)
			step := time.Duration(h * float64(time.Second))
			pb, vb, _ := m.state(at - step)
			pa, va, _ := m.state(at + step)
			if dv := pa.sub(pb).scale(0.5 / h); !near(dv, v, 1e-3*max(1, v.len())) {
				t.Errorf("%s at %v: velocity %v, want %v", tc.name, at, v, dv)
			}
			if da := va.sub(vb).scale(0.5 / h); !near(da, a, 1e-2*max(1, a.len())) {
				t.Errorf("%s at %v: acceleration %v, want %v", tc.name, at, a, da)
			}
			_ = p
		}
	}
	// Moving right and down, the bow is up and right of the line.
	if b := glide.bow; b.y >= 0 || b.x <= 0 {
		t.Errorf("bow %v, want up and right", b)
	}
	if b := bowOf(vec{0, 0}, vec{0, 10}, 0.5); b.y != 0 || b.x >= 0 {
		t.Errorf("straight down: bow %v, want left", b)
	}
}

// TestPaintMask checks that paintMask blends exactly as [draw.DrawMask] does,
// over pixels of every opacity, into a piece of a larger image, with masks
// partly or wholly off the piece, and leaves the rest of the image alone.
func TestPaintMask(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(1, 2))
	byteOf := func() uint8 {
		// Favor the ends, where blending takes its special cases.
		switch rng.IntN(4) {
		case 0:
			return 0
		case 1:
			return 0xff
		}
		return uint8(rng.IntN(0x100))
	}
	for range 200 {
		got := image.NewNRGBA(image.Rect(-3, -2, 12, 10))
		for i := range got.Pix {
			got.Pix[i] = byteOf()
		}
		want := image.NewNRGBA(got.Rect)
		copy(want.Pix, got.Pix)
		piece := image.Rect(1, 1, 9, 7)
		at := image.Pt(rng.IntN(16)-8, rng.IntN(14)-7)
		mask := image.NewAlpha(image.Rect(0, 0, 6, 5).Add(at))
		for i := range mask.Pix {
			mask.Pix[i] = byteOf()
		}
		c := color.NRGBA{byteOf(), byteOf(), byteOf(), byteOf()}
		draw.DrawMask(want.SubImage(piece).(*image.NRGBA), mask.Rect, image.NewUniform(c), image.Point{}, mask, mask.Rect.Min, draw.Over)
		paintMask(got.SubImage(piece).(*image.NRGBA), mask, c)
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("color %v, mask at %v: pixels %v, want %v", c, at, got.Pix, want.Pix)
		}
	}
}

// TestMouseTrailMask checks that a trail's mask is drawn again for another
// end or view, and not for another alpha.
func TestMouseTrailMask(t *testing.T) {
	t.Parallel()
	s := testStage(100 * ms)
	addAll(s,
		mev(0, vt.MousePress, vt.MouseLeft, 0, 0),
		mev(10*ms, vt.MouseMotion, vt.MouseLeft, 5, 0),
		mev(20*ms, vt.MouseMotion, vt.MouseLeft, 5, 3),
	)
	tr := trailLook{first: 0, from: 0, until: 15 * ms, alpha: 0xff}
	view, grid := cellView{w: 10, h: 20}, image.Rect(0, 0, 100, 100)
	mask := s.trailMask(tr, view, grid)
	tr.alpha = 0x40
	if s.trailMask(tr, view, grid) != mask {
		t.Error("another alpha: drawn again, want the same mask")
	}
	tr.until = 200 * ms
	longer := s.trailMask(tr, view, grid)
	if longer == mask || longer.Rect.Max.Y <= mask.Rect.Max.Y {
		t.Errorf("another end: mask %v, want a new one past %v", longer.Rect, mask.Rect)
	}
	if zoomed := s.trailMask(tr, cellView{w: 20, h: 40}, grid); zoomed == longer || zoomed.Rect == longer.Rect {
		t.Errorf("another view: mask %v, want a new one, not %v", zoomed.Rect, longer.Rect)
	}
}

// TestMouseRaw checks single mouse events: motion with no button held moves
// the pointer, a right press shows the orange ripple and keeps the disc
// under the pointer until its release, and a wheel turn only moves it.
func TestMouseRaw(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 16, 3, "",
		output(0, mouseScreen),
		input(100, sgrMouse(sgrHover, 1, 0, false)),
		input(300, sgrMouse(sgrHover, 8, 1, false)),
		input(600, sgrMouse(sgrRight, 8, 1, false)),
		input(1200, sgrMouse(sgrRight, 8, 1, true)),
		input(1500, sgrMouse(64, 12, 2, false)),
		output(5000, " "),
	)
	in, out := mouseFrames(t, tl, Mouse(WithPointerStyle(PointerDot), WithPointerGlide(100*ms)))
	assertMouseFrames(t, in, out, 2750*ms)
	at := func(d time.Duration) reel.Frame {
		i := slices.IndexFunc(out, func(f reel.Frame) bool { return f.At >= d })
		return out[i]
	}
	assertGolden(t, "mouse-raw", []reel.Frame{
		at(100 * ms), at(350 * ms), at(400 * ms), at(650 * ms), at(1000 * ms), at(1250 * ms), at(1550 * ms), at(1650 * ms),
	})
}

// TestMouseZoom checks that the pointer and ripple are drawn on the cell a
// [Zoom] before Mouse shows, as it eases out, and that a pointer the zoom
// puts outside the grid is left out.
func TestMouseZoom(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 16, 3, "",
		output(0, mouseScreen),
		input(100, sgrMouse(sgrLeft, 8, 1, false)+sgrMouse(sgrLeft, 8, 1, true)),
		input(300, sgrMouse(sgrHover, 0, 0, false)),
		input(400, sgrMouse(sgrHover, 9, 1, false)),
		mark(600, "out"),
		output(2000, " "),
	)
	zoom := Zoom(Cells(image.Rect(7, 1, 13, 2)), Scale(2), Between("", "out"), EaseOut(150*ms))
	_, out := mouseFrames(t, tl, zoom, Mouse())
	at := func(d time.Duration) reel.Frame {
		i := slices.IndexFunc(out, func(f reel.Frame) bool { return f.At >= d })
		return out[i]
	}
	assertGolden(t, "mouse-zoom", []reel.Frame{
		at(150 * ms), at(350 * ms), at(550 * ms), at(650 * ms), at(700 * ms), at(800 * ms),
	})
}

// TestMouseNothing checks that frames are left alone without mouse input,
// and when no part of the mouse is drawn.
func TestMouseNothing(t *testing.T) {
	t.Parallel()
	clicked := []event{output(0, mouseScreen), input(100, sgrMouse(sgrLeft, 3, 1, false)), output(500, " ")}
	for _, tc := range []struct {
		name   string
		events []event
		effect reel.Effect
	}{
		{"no mouse input", []event{output(0, mouseScreen), input(100, "ab\r"), output(500, " ")}, Mouse()},
		{"no parts", clicked, Mouse(WithMouseParts(0))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in, out := highlightFrames(t, buildTimeline(t, 16, 3, "", tc.events...), tc.effect)
			if len(out) != len(in) {
				t.Fatalf("%d frames, want %d", len(out), len(in))
			}
			for i := range out {
				if out[i].Image != in[i].Image || out[i].Delay != in[i].Delay {
					t.Fatalf("frame %d changed", i)
				}
			}
		})
	}
}

// TestMouseErrors checks the options Start rejects.
func TestMouseErrors(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 4, 1, "", output(0, "a"))
	ctx := &reel.Context{Timeline: tl, Rasterizer: highlightRast, Layout: highlightRast.Layout(4, 1), Step: 10 * ms, Speed: 1}
	for name, e := range map[string]reel.Effect{
		"style":    Mouse(WithPointerStyle(PointerDot + 1)),
		"negative": Mouse(WithPointerStyle(-1)),
		"size":     Mouse(WithPointerSize(-1)),
	} {
		if _, err := e.Start(ctx); err == nil {
			t.Errorf("%s: no error", name)
		}
		if _, err := e.(reel.Describer).Describe(); err == nil {
			t.Errorf("%s: no error from Describe", name)
		}
	}
}

// TestMouseEvents checks what each mouse event leaves the mouse doing, and
// where the pointer moves to it from.
func TestMouseEvents(t *testing.T) {
	t.Parallel()
	s := testStage(100 * ms)
	addAll(s,
		mev(0, vt.MouseMotion, vt.MouseUnknown, 0, 0),
		mev(50*ms, vt.MousePress, vt.MouseLeft, 2, 0),
		mev(200*ms, vt.MouseMotion, vt.MouseLeft, 3, 0),
		mev(200*ms, vt.MousePress, vt.MouseWheelUp, 3, 0),
		mev(300*ms, vt.MouseRelease, vt.MouseLeft, 3, 0),
		mev(400*ms, vt.MouseMotion, vt.MouseRight, 4, 0),
		mev(2*time.Second, vt.MouseRelease, vt.MouseUnknown, 6, 0),
		mev(4*time.Second, vt.MouseMotion, vt.MouseUnknown, 1, 0),
	)
	want := []struct {
		fromX float64
		held  vt.MouseButton
		drag  int
	}{
		{0.5, vt.MouseUnknown, -1},
		{0.5, vt.MouseLeft, 1},
		{2.5, vt.MouseLeft, 1},
		// The motion at the same offset had reached its cell.
		{3.5, vt.MouseLeft, 1},
		{3.5, vt.MouseUnknown, 1},
		// Motion with a button held drags without a press.
		{3.5, vt.MouseRight, 5},
		{4.5, vt.MouseUnknown, 5},
		// The pointer had hidden, so it fades in where it was.
		{6.5, vt.MouseUnknown, -1},
	}
	for i, w := range want {
		ev := s.events[i]
		if ev.move.from.x != w.fromX || ev.held != w.held || ev.drag != w.drag {
			t.Errorf("event %d: from %v, held %v, drag %d; want from %v, held %v, drag %d", i, ev.move.from.x, ev.held, ev.drag, w.fromX, w.held, w.drag)
		}
	}
	if ev := s.events[7]; ev.shown != 0 {
		t.Errorf("after hiding: shown %v as it moves, want 0", ev.shown)
	}
	if a := s.alpha(s.events[7].at); a != 1 {
		t.Errorf("after hiding: alpha %v as the event is sent, want 1", a)
	}
}

// TestMouseArrives checks that the pointer sets off for an event before it
// is sent, to arrive as it is, and with too little time, that it glides
// quicker and arrives late, the events after a press waiting for it, a
// glide at most.
func TestMouseArrives(t *testing.T) {
	t.Parallel()
	s := testStage(150 * ms)
	addAll(s,
		mev(0, vt.MouseMotion, vt.MouseUnknown, 0, 0),
		mev(500*ms, vt.MousePress, vt.MouseLeft, 30, 0),
		mev(540*ms, vt.MouseMotion, vt.MouseLeft, 31, 0),
		mev(1500*ms, vt.MouseRelease, vt.MouseLeft, 31, 0),
		// Presses quicker than the pointer reaches them.
		mev(2000*ms, vt.MousePress, vt.MouseRight, 0, 3),
		mev(2010*ms, vt.MousePress, vt.MouseRight, 30, 3),
		mev(2020*ms, vt.MousePress, vt.MouseRight, 60, 9),
	)
	press := s.events[1]
	if press.move.start >= 400*ms || press.landed != press.at {
		t.Errorf("press: leaves at %v, lands at %v; want to leave well before, and land at %v", press.move.start, press.landed, press.at)
	}
	if x := s.pos(450 * ms).x; x <= 0.5 || x >= 30.5 {
		t.Errorf("at 450ms, x = %v, want on the way to the press", x)
	}
	if p := s.pos(500 * ms); p != (vec{30.5, 0.5}) {
		t.Errorf("at the press, pointer at %v, want on its cell", p)
	}
	if p := s.pos(450 * ms); p.y >= 0.5 {
		t.Errorf("at 450ms, y = %v, want bowed up out of the line", p.y)
	}
	quick, quicker := s.events[5], s.events[6]
	if quick.landed <= quick.at || quick.move.dur < s.reach(vec{0.5, 3.5}, vec{}, quick.cell())/2 {
		t.Errorf("quick press: lands at %v after a %v glide; want late, at most twice as quick", quick.landed, quick.move.dur)
	}
	if want := min(quick.landed, quicker.at+s.glide); quicker.move.start != want || want <= quicker.at {
		t.Errorf("quicker press: leaves at %v, want once the pointer reaches the press before, a glide at most, at %v", quicker.move.start, want)
	}
	for _, ev := range s.events {
		if ev.move.start > ev.at+s.glide {
			t.Errorf("event at %v leaves at %v, more than a glide late", ev.at, ev.move.start)
		}
	}
	assertSmooth(t, s, 0, 3*time.Second)
}

// TestMouseGlideBack checks that a glide is measured from where the pointer
// leaves: back to where a glide before it set off from, it glides rather
// than jumping.
func TestMouseGlideBack(t *testing.T) {
	t.Parallel()
	s := testStage(150 * ms)
	addAll(s,
		mev(0, vt.MouseMotion, vt.MouseUnknown, 0, 0),
		mev(0, vt.MouseMotion, vt.MouseUnknown, 40, 0),
		mev(500*ms, vt.MouseMotion, vt.MouseUnknown, 0, 0),
	)
	if back := s.events[2]; back.move.dur == 0 || back.move.from != (vec{40.5, 0.5}) {
		t.Errorf("back: glides from %v over %v, want from (40.5, 0.5) over a while", back.move.from, back.move.dur)
	}
	assertSmooth(t, s, 0, time.Second)
}

// TestMouseLive checks that, live, the pointer sets off for an event no
// sooner than the start of the frame it is read for.
func TestMouseLive(t *testing.T) {
	t.Parallel()
	s := testStage(150 * ms)
	s.add(0, 0, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseUnknown, X: 0, Y: 0})
	s.add(time.Second, 950*ms, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseLeft, X: 30, Y: 0})
	if ev := s.events[1]; ev.move.start != 950*ms || ev.landed <= ev.at {
		t.Errorf("press: leaves at %v, lands at %v; want to leave at 950ms, and land late", ev.move.start, ev.landed)
	}
	s.add(2*time.Second, 2100*ms, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseLeft, X: 0, Y: 0})
	if ev := s.events[2]; ev.move.start != 2100*ms {
		t.Errorf("press read after it was sent: leaves at %v, want at 2.1s", ev.move.start)
	}
}

// TestMouseLook checks what the mouse shows over a drag: the pointer fading
// in, the ripple of the press, the held button, the trail while it is held
// and as it fades, and the pointer hiding.
func TestMouseLook(t *testing.T) {
	t.Parallel()
	s := testStage(100 * ms)
	addAll(s,
		mev(0, vt.MousePress, vt.MouseLeft, 1, 1),
		mev(100*ms, vt.MouseMotion, vt.MouseLeft, 2, 1),
		mev(200*ms, vt.MouseRelease, vt.MouseLeft, 2, 1),
	)
	ripples := func(l mouseLook) int {
		return slices.IndexFunc(l.ripples[:], func(r rippleLook) bool { return r.button == vt.MouseUnknown })
	}
	for _, tc := range []struct {
		t       time.Duration
		x       float64
		alpha   uint8
		held    vt.MouseButton
		ripples int
		trail   trailLook
	}{
		{0, 1.5, 0, vt.MouseLeft, 1, trailLook{}},
		{50 * ms, 2, 0x80, vt.MouseLeft, 1, trailLook{first: 0, until: 50 * ms, alpha: 0xff}},
		{150 * ms, 2.5, 0xff, vt.MouseLeft, 1, trailLook{first: 0, until: 100 * ms, alpha: 0xff}},
		{300 * ms, 2.5, 0xff, vt.MouseUnknown, 1, trailLook{first: 0, until: 200 * ms, alpha: 0xd7}},
		{500 * ms, 2.5, 0xff, vt.MouseUnknown, 0, trailLook{first: 0, until: 200 * ms, alpha: 0x28}},
		{1250 * ms, 2.5, 0x80, vt.MouseUnknown, 0, trailLook{}},
	} {
		l := s.look(tc.t)
		if l.pos.x != tc.x || l.alpha != tc.alpha || l.held != tc.held || ripples(l) != tc.ripples || l.trail != tc.trail {
			t.Errorf("at %v: pos %v, alpha %#x, held %v, %d ripples, trail %+v; want pos %v, alpha %#x, held %v, %d ripples, trail %+v",
				tc.t, l.pos.x, l.alpha, l.held, ripples(l), l.trail, tc.x, tc.alpha, tc.held, tc.ripples, tc.trail)
		}
	}
	if l := s.look(1400 * ms); l != (mouseLook{}) {
		t.Errorf("after hiding, look %+v, want none", l)
	}
	if l := s.look(-ms); l != (mouseLook{}) {
		t.Errorf("before the first event, look %+v, want none", l)
	}
}

// TestMouseExtraButtons checks that the extra buttons, and the release of
// another button, leave a drag going on.
func TestMouseExtraButtons(t *testing.T) {
	t.Parallel()
	s := testStage(100 * ms)
	addAll(s,
		mev(0, vt.MousePress, vt.MouseLeft, 1, 1),
		mev(10*ms, vt.MousePress, vt.MouseBackward, 2, 1),
		mev(20*ms, vt.MouseMotion, vt.MouseBackward, 3, 1),
		mev(30*ms, vt.MouseRelease, vt.MouseBackward, 3, 1),
		mev(40*ms, vt.MouseRelease, vt.MouseRight, 3, 1),
	)
	for i, ev := range s.events {
		if ev.held != vt.MouseLeft || ev.drag != 0 {
			t.Errorf("event %d: held %v, drag %d; want left held, drag 0", i, ev.held, ev.drag)
		}
	}
}

// TestMouseTrailPath checks that a trail is the path the pointer took, from
// the press to where it is, and once released, that it stops where the
// pointer set off for the next event, rather than follow it on.
func TestMouseTrailPath(t *testing.T) {
	t.Parallel()
	s := testStage(100 * ms)
	addAll(s,
		mev(0, vt.MousePress, vt.MouseLeft, 0, 0),
		mev(50*ms, vt.MouseMotion, vt.MouseLeft, 10, 0),
		mev(100*ms, vt.MouseMotion, vt.MouseLeft, 10, 4),
		mev(150*ms, vt.MouseRelease, vt.MouseLeft, 10, 4),
		mev(160*ms, vt.MouseMotion, vt.MouseUnknown, 0, 9),
	)
	held := s.look(120 * ms).trail
	path := s.trailPath(held)
	if path[0] != (vec{0.5, 0.5}) || path[len(path)-1] != s.pos(120*ms) {
		t.Errorf("held: trail from %v to %v, want from the press to the pointer, %v", path[0], path[len(path)-1], s.pos(120*ms))
	}
	// Each point is where the pointer was at a later sample than the last.
	at := held.from
	for k, p := range path[:len(path)-1] {
		for at < held.until && s.pos(at) != p {
			at += mouseTrailSample
		}
		if at >= held.until {
			t.Fatalf("held: trail point %d at %v, not where the pointer was after the point before", k, p)
		}
	}
	released, rel := s.look(300*ms).trail, s.events[3]
	if released.alpha == 0 || released.until != s.events[4].move.start || released.until >= rel.move.start+rel.move.dur {
		t.Errorf("released: trail %+v, want one until the pointer set off again, at %v", released, s.events[4].move.start)
	}
	for _, p := range s.trailPath(released) {
		if p.y > 4.5+1e-9 {
			t.Errorf("released: trail reaches %v, past the release toward the next event", p)
		}
	}
}
