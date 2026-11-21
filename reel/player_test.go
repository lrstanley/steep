// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"getsteep.dev/steep/reel/render"
)

func TestPlayerSeek(t *testing.T) {
	tl := testTimeline(t,
		Output{event: at(0), Data: []byte("one")},
		Mark{event: at(10), Label: "m"},
		Output{event: at(20), Data: []byte("\r\ntwo")},
		Resize{event: at(30), Cols: 40, Rows: 10},
	)
	p, err := NewPlayer(t.Context(), tl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })

	text := func() string {
		var lines []string
		for _, row := range p.Screen().Cells {
			var b strings.Builder
			for _, c := range row {
				if c != nil {
					b.WriteString(c.Content)
				}
			}
			lines = append(lines, strings.TrimSpace(b.String()))
		}
		return strings.TrimRight(strings.Join(lines, "|"), "|")
	}

	for _, tc := range []struct {
		n          int
		want       string
		cols, rows int
	}{
		{n: 3, want: "one|two", cols: 80, rows: 24},
		{n: 1, want: "one", cols: 80, rows: 24},
		{n: 99, want: "one|two", cols: 40, rows: 10},
		{n: 0, want: "", cols: 80, rows: 24},
	} {
		if err = p.Seek(tc.n); err != nil {
			t.Fatal(err)
		}
		if got := text(); got != tc.want {
			t.Errorf("Seek(%d) text = %q, want %q", tc.n, got, tc.want)
		}
		scr := p.Screen()
		if scr.Cols != tc.cols || scr.Rows != tc.rows {
			t.Errorf("Seek(%d) size = %dx%d, want %dx%d", tc.n, scr.Cols, scr.Rows, tc.cols, tc.rows)
		}
	}
	if p.Pos() != 0 || p.Len() != 4 {
		t.Errorf("Pos, Len = %d, %d, want 0, 4", p.Pos(), p.Len())
	}

	// An image is kept until a Seek, so it is drawn again at another event
	// of the same size.
	if err = p.Seek(3); err != nil {
		t.Fatal(err)
	}
	img, err := p.Image()
	if err != nil {
		t.Fatal(err)
	}
	two := bytes.Clone(img.Pix)
	if again, _ := p.Image(); again != img || !bytes.Equal(again.Pix, two) {
		t.Error("Image changed without a Seek")
	}
	if err = p.Seek(1); err != nil {
		t.Fatal(err)
	}
	if img, err = p.Image(); err != nil || bytes.Equal(img.Pix, two) {
		t.Errorf("Image after Seek(1) = %v, want it drawn again", err)
	}

	if err = p.Seek(4); err != nil {
		t.Fatal(err)
	}
	img, err = p.Image()
	if err != nil {
		t.Fatal(err)
	}
	if img.Rect.Empty() {
		t.Error("Image is empty")
	}
	l, err := p.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if l.Image != img.Rect || l.Grid.Dx() != 40*l.Cell.X || l.Grid.Dy() != 10*l.Cell.Y {
		t.Errorf("Layout = %+v, want a 40x10 grid in %v", l, img.Rect)
	}

	// Window chrome adds a title bar above the grid.
	if err = p.SetOptions(render.WithWindowChrome(true)); err != nil {
		t.Fatal(err)
	}
	chrome, err := p.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if chrome.Grid.Min.Y <= l.Grid.Min.Y || chrome.Image.Dy() <= l.Image.Dy() {
		t.Errorf("Layout with chrome = %+v, want the grid lower than %+v", chrome, l)
	}
	if img, err = p.Image(); err != nil || img.Rect != chrome.Image {
		t.Errorf("Image with chrome = %v, %v, want %v", img.Rect, err, chrome.Image)
	}
}

// TestPlayerPrepare checks that a Seek back from checkpoints shows the same
// screen as one replayed from the start, and that the checkpoints last.
func TestPlayerPrepare(t *testing.T) {
	var evs []Event
	for i := range 40 {
		evs = append(evs, Output{event: at(i * 10), Data: []byte(fmt.Sprintf("\r\nline %d", i))})
	}
	tl := testTimeline(t, evs...)
	p, err := NewPlayer(t.Context(), tl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	plain, err := NewPlayer(t.Context(), tl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })

	// A zero budget does no work, so it says there is more.
	if more, perr := p.Prepare(3, 0); perr != nil || !more {
		t.Fatalf("Prepare with no budget = %v, %v, want more", more, perr)
	}
	for more := true; more; {
		if more, err = p.Prepare(3, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(p.checkpoints); got != 3 {
		t.Fatalf("%d checkpoints, want 3", got)
	}

	screen := func(p *Player) string {
		var b strings.Builder
		for _, row := range p.Screen().Cells {
			for _, c := range row {
				if c != nil {
					b.WriteString(c.Content)
				}
			}
		}
		return b.String()
	}
	if err = p.Seek(40); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{25, 12, 31, 3} {
		if err = p.Seek(n); err != nil {
			t.Fatal(err)
		}
		if err = plain.Seek(0); err != nil {
			t.Fatal(err)
		}
		if err = plain.Seek(n); err != nil {
			t.Fatal(err)
		}
		if got, want := screen(p), screen(plain); got != want {
			t.Errorf("Seek(%d) from a checkpoint = %q, want %q", n, got, want)
		}
	}
	// Seeks restore checkpoints without using them up.
	if more, perr := p.Prepare(3, time.Second); perr != nil || more || len(p.checkpoints) != 3 {
		t.Errorf("Prepare after seeking = %v, %v with %d checkpoints, want nothing to do", more, perr, len(p.checkpoints))
	}
	if _, err = p.Prepare(0, time.Second); err != nil || len(p.checkpoints) != 0 {
		t.Errorf("Prepare(0) left %d checkpoints, %v", len(p.checkpoints), err)
	}
}

// TestPlayerCheckpoints checks that seeking through checkpoints of a busy
// timeline shows each frame as a replay from the start does: through
// images, glyphs, a sequence split across events, the alternate screen,
// resizes, scrolling, and focus. A point the terminal cannot be snapshotted
// at gets no checkpoint.
func TestPlayerCheckpoints(t *testing.T) {
	var evs []Event
	add := func(ev Event) { evs = append(evs, ev) }
	ms := 0
	out := func(s string) {
		ms += 10
		add(Output{event: at(ms), Data: []byte(s)})
	}
	for i := range 6 {
		out(fmt.Sprintf("line %d\r\n", i))
	}
	out("\x1b]133;A\x07$ \x1b]133;B\x07ls\r\n\x1b]133;C\x07\x1b]133;D;0\x07")
	out("\x1b]2;one\x07\x1b_25a1;r;cp=e000;upm=1000;AAEAZABkA4QDhAACAAABAQEB9P5wAyADhPzgAAA=\x1b\\\ue000\r\n")
	out("\x1b_Ga=T,i=9,f=24,s=1,v=1,q=2;/wAA\x1b\\\r\n")
	out("\x1b]2;sp")
	out("lit\x07half")
	out("\x1b[?1049h\x1b[2Jalt screen")
	out("\x1b[2;2Hmore alt")
	out("\x1b[?1049lback\r\n")
	ms += 10
	add(Resize{event: at(ms), Cols: 30, Rows: 5})
	for i := range 6 {
		out(fmt.Sprintf("after %d\r\n", i))
	}
	ms += 10
	add(Scroll{event: at(ms), Offset: 2})
	ms += 10
	add(Focus{event: at(ms), On: false})
	out("end")
	tl := testTimeline(t, evs...)

	p, err := NewPlayer(t.Context(), tl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	for more := true; more; {
		if more, err = p.Prepare(len(evs), time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.checkpoints) == 0 || len(p.skipped) == 0 {
		t.Fatalf("%d checkpoints, %d skipped, want both: the alternate screen keeps marks", len(p.checkpoints), len(p.skipped))
	}
	plain, err := NewPlayer(t.Context(), tl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	frame := func(p *Player) string {
		f := p.Frame()
		// Counters of changes, which a restore moves on.
		f.State.Generation, f.State.ProgramStatusGeneration = 0, 0
		f.State.Glyphs = nil
		var b strings.Builder
		for y := range f.Screen.Height() {
			for x := range f.Screen.Width() {
				if c := f.Screen.CellAt(x, y); c != nil {
					fmt.Fprintf(&b, "%s%v", c.Content, c.Style)
				}
			}
		}
		fmt.Fprintf(&b, "|%+v|%d images", f.State, len(f.Screen.Images()))
		return b.String()
	}
	order := []int{len(evs)}
	for n := range slices.Backward(evs) {
		order = append(order, n)
	}
	for _, n := range append(order, 5, len(evs), 12, 3, 20) {
		if err = p.Seek(n); err != nil {
			t.Fatal(err)
		}
		if err = plain.Seek(0); err != nil {
			t.Fatal(err)
		}
		if err = plain.Seek(n); err != nil {
			t.Fatal(err)
		}
		if got, want := frame(p), frame(plain); got != want {
			t.Errorf("Seek(%d) from a checkpoint:\n%s\nwant:\n%s", n, got, want)
		}
	}
}
