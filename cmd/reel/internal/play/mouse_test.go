// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package play

import (
	"context"
	"errors"
	"fmt"
	"image"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	syntax "getsteep.dev/steep/cmd/reel/internal/tape"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/vt"
)

// describeTarget is a target as the tests compare it, as the condition's
// String, since a regexp's is a pointer.
func describeTarget(t target) string {
	if t.find == nil {
		return fmt.Sprintf("cell %d,%d", t.cell.X, t.cell.Y)
	}
	return fmt.Sprintf("find %s %+d %+d timeout=%v", t.find, t.cell.X, t.cell.Y, t.timeout)
}

// describeMouse is a mouse command's step as the tests compare it.
func describeMouse(s step) (string, bool) {
	switch s := s.(type) {
	case clickStep:
		return fmt.Sprintf("click %d %s delay=%v repeat=%d", s.button, describeTarget(s.at), s.delay, s.repeat), true
	case hoverStep:
		return "hover " + describeTarget(s.at), true
	case dragStep:
		return fmt.Sprintf("drag %d %s to %s delay=%v", s.button, describeTarget(s.from), describeTarget(s.to), s.delay), true
	case wheelStep:
		return fmt.Sprintf("wheel %d %s delay=%v repeat=%d", s.button, describeTarget(s.at), s.delay, s.repeat), true
	}
	return "", false
}

func cellAt(x, y int) target { return target{cell: image.Pt(x, y)} }

func findText(text string, dx, dy int) target {
	return target{find: trimRows{reel.Contains(text)}, cell: image.Pt(dx, dy), timeout: defaultWaitTimeout}
}

func TestParseMouse(t *testing.T) {
	ms := time.Millisecond
	tests := []struct {
		src  string
		want []string
		err  string
	}{
		{src: `Click 3 4`, want: []string{describe(clickStep{button: vt.MouseLeft, at: cellAt(3, 4), repeat: 1})}},
		{src: `Click+Right@50ms "OK" 2`, want: []string{describe(clickStep{button: vt.MouseRight, at: findText("OK", 0, 0), delay: 50 * ms, repeat: 2})}},
		{src: `Click+Middle OK`, want: []string{describe(clickStep{button: vt.MouseMiddle, at: findText("OK", 0, 0), repeat: 1})}},
		{src: `Click+Left 0 0 3`, want: []string{describe(clickStep{button: vt.MouseLeft, at: cellAt(0, 0), repeat: 3})}},
		{src: `Click "Name:" +6`, want: []string{describe(clickStep{button: vt.MouseLeft, at: findText("Name:", 6, 0), repeat: 1})}},
		{src: `Click "Name:" -2 +1 2`, want: []string{describe(clickStep{button: vt.MouseLeft, at: findText("Name:", -2, 1), repeat: 2})}},
		{src: `Click /N\w+/ +1 -1`, want: []string{describe(clickStep{button: vt.MouseLeft, at: target{
			find: trimRows{reel.Matches(regexp.MustCompile(`N\w+`))}, cell: image.Pt(1, -1), timeout: defaultWaitTimeout,
		}, repeat: 1})}},
		{src: "Set WaitTimeout 2s\nHover \"[?]\"", want: []string{describe(hoverStep{at: target{
			find: trimRows{reel.Contains("[?]")}, timeout: 2 * time.Second,
		}})}},
		{src: `Hover 0 1`, want: []string{describe(hoverStep{at: cellAt(0, 1)})}},
		{src: `Drag "a" "b"`, want: []string{describe(dragStep{button: vt.MouseLeft, from: findText("a", 0, 0), to: findText("b", 0, 0)})}},
		{src: `Drag+Right@10ms 1 2 "bin" -1`, want: []string{describe(dragStep{
			button: vt.MouseRight, from: cellAt(1, 2), to: findText("bin", -1, 0), delay: 10 * ms,
		})}},
		{src: `Drag "Name:" +1 "Name:" +11`, want: []string{describe(dragStep{button: vt.MouseLeft, from: findText("Name:", 1, 0), to: findText("Name:", 11, 0)})}},
		{src: `Wheel+Down "Sencha" 40`, want: []string{describe(wheelStep{button: vt.MouseWheelDown, at: findText("Sencha", 0, 0), repeat: 40})}},
		{src: `Wheel+Left@5ms 0 0`, want: []string{describe(wheelStep{button: vt.MouseWheelLeft, at: cellAt(0, 0), delay: 5 * ms, repeat: 1})}},
		{src: "Wheel+Up 1 1\nWheel+Right 1 1", want: []string{
			describe(wheelStep{button: vt.MouseWheelUp, at: cellAt(1, 1), repeat: 1}),
			describe(wheelStep{button: vt.MouseWheelRight, at: cellAt(1, 1), repeat: 1}),
		}},
		{src: `Click`, err: "1:1: Click expects a cell X Y, a string, or a /regexp/"},
		{src: `Click 3`, err: "1:7: Click's row expects a whole number"},
		{src: `Click ""`, err: `1:7: Click: "" is not a cell X Y, a string, or a /regexp/`},
		{src: `Click /(/`, err: "1:7: Click: error parsing regexp"},
		{src: `Click+Up 1 1`, err: `1:7: Click+ expects Left, Middle, or Right, not "Up"`},
		{src: `Click+Left+Right 1 1`, err: "1:12: Click takes one of Left, Middle, or Right"},
		{src: `Click "OK" 0`, err: "1:12: a repeat count must be at least 1, got 0"},
		{src: `Click "OK" +`, err: "1:12: Click: + expects a whole number after it"},
		{src: `Click "OK" +1 +2 +3`, err: `1:18: unexpected "+" after Click`},
		{src: `Click 1 1 +2`, err: `1:11: unexpected "+" after Click`},
		{src: `Hover@1s 0 0`, err: "1:7: Hover takes no @1s"},
		{src: `Hover+Left 0 0`, err: "1:7: Hover takes no +Left"},
		{src: `Hover 0 0 2`, err: `1:11: unexpected "2" after Hover`},
		{src: `Drag 1 1`, err: "1:1: Drag expects a cell X Y, a string, or a /regexp/"},
		{src: `Wheel 1 1`, err: "1:1: Wheel expects one of Up, Down, Left, or Right"},
		{src: `Wheel+Down+Up 1 1`, err: "1:12: Wheel takes one of Up, Down, Left, or Right"},
		{src: `Wheel+Middle 1 1`, err: `1:7: Wheel+ expects Up, Down, Left, or Right, not "Middle"`},
		{src: `Wheel+Down@soon 1 1`, err: `1:12: Wheel@: "soon" is not a duration`},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			tp, err := parseString(tt.src)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error = %v, want one containing %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := describeSteps(tp); !slices.Equal(got, tt.want) {
				t.Errorf("steps:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// mouseBoard is an in-process program that reports mouse events of any
// kind in SGR, draws a few things to point at, and reads what it is sent.
func mouseBoard(_ context.Context, s *reel.Session) error {
	if _, err := s.Write([]byte("\x1b[?1003h\x1b[?1006hName: x\r\n  Sencha\r\n card    bin")); err != nil {
		return err
	}
	buf := make([]byte, 256)
	for {
		if _, err := s.Read(buf); err != nil {
			return nil //nolint:nilerr // The session closed.
		}
	}
}

// mouseInputs are the SGR mouse reports in tl's input, in order.
func mouseInputs(tl *reel.Timeline) []string {
	var out []string
	for in := range tl.Inputs() {
		if after, ok := strings.CutPrefix(string(in.Data), "\x1b[<"); ok {
			out = append(out, after)
		}
	}
	return out
}

// TestRunMouse runs each mouse command against a program that reports
// them, and checks what it was sent.
func TestRunMouse(t *testing.T) {
	res, _, err := runTape(t, `
Output "%s/out.snap"
Set Columns 20
Set Rows 4
Click "Name:" +6
Click+Right 1 1 2
Hover "Sencha"
Drag "card" "bin"
Drag+Middle "Name:" +1 -0 /S\w+/ -1 +1
Wheel+Down "Sencha" 3
Wheel+Up@1ms 19 3
Hover /(?m)^ +Sencha$/
`, WithProgram(mouseBoard))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		// Click "Name:" +6, at (6, 0).
		"0;7;1M", "0;7;1m",
		// A right double click at (1, 1).
		"2;2;2M", "2;2;2m", "2;2;2M", "2;2;2m",
		// Motion with no button held, at Sencha's (2, 1).
		"35;3;2M",
		// From card's (1, 2) to bin's (9, 2).
		"0;2;3M", "32;3;3M", "32;4;3M", "32;5;3M", "32;6;3M", "32;7;3M", "32;8;3M", "32;9;3M", "32;10;3M", "0;10;3m",
		// From (1, 0) to (1, 2), with the middle button held.
		"1;2;1M", "33;2;2M", "33;2;3M", "1;2;3m",
		// Three turns down at (2, 1), and one up at the last cell.
		"65;3;2M", "65;3;2M", "65;3;2M", "64;20;4M",
		// A pattern that holds only on the row without its trailing spaces,
		// as Wait matches, at the row's start.
		"35;1;2M",
	}
	if got := mouseInputs(res.Timeline); !slices.Equal(got, want) {
		t.Errorf("mouse input:\n got %q\nwant %q", got, want)
	}
}

// TestRunMouseFails checks that a mouse command reports a target it cannot
// find, or one off the screen, where it is in the tape, and sends nothing.
func TestRunMouseFails(t *testing.T) {
	tests := map[string]struct {
		cmd  string
		want string
	}{
		"not found":    {`Click "missing"`, `Click: contains ["missing"] is not on the screen after 100ms; the screen was:` + "\nName: x\n  Sencha\n card    bin"},
		"off by cell":  {`Click 20 0`, "Click at (20, 0) is off the 20x4 screen"},
		"off by text":  {`Wheel+Down "bin" +20`, "Wheel at (29, 2) is off the 20x4 screen"},
		"above":        {`Hover "Name:" +0 -1`, "Hover at (0, -1) is off the 20x4 screen"},
		"drag from":    {`Drag "Sencha" -3 1 1`, "Drag at (-1, 1) is off the 20x4 screen"},
		"drag to":      {`Drag 1 1 "bin" +0 +2`, "Drag at (9, 4) is off the 20x4 screen"},
		"drag to lost": {`Drag 1 1 /nowhere/`, `Drag: matches "nowhere" is not on the screen after 100ms`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			res, _, err := runTape(t, "Output \"%s/out.snap\"\nSet Columns 20\nSet Rows 4\nSet WaitTimeout 100ms\n"+tt.cmd, WithProgram(mouseBoard))
			var terr *syntax.Error
			if !errors.As(err, &terr) || terr.Pos.Line != 5 || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one at line 5 containing %q", err, tt.want)
			}
			if res != nil {
				if got := mouseInputs(res.Timeline); len(got) != 0 {
					t.Errorf("sent mouse input %q", got)
				}
			}
		})
	}
}

// TestDragCanceled checks that a drag ended by its context stops where the
// pointer got to and releases the button there.
func TestDragCanceled(t *testing.T) {
	s, err := reel.Open(t.Context(), 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err = s.Write([]byte("\x1b[?1002h\x1b[?1006h")); err != nil {
		t.Fatal(err)
	}
	// The first pause outlasts the context.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err = Drag(ctx, s, vt.MouseLeft, image.Pt(0, 0), image.Pt(5, 0), time.Hour)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Drag = %v, want the deadline", err)
	}
	if got, want := mouseInputs(s.Timeline()), []string{"0;1;1M", "32;2;1M", "0;2;1m"}; !slices.Equal(got, want) {
		t.Errorf("mouse input = %q, want a press, one step, and a release there", got)
	}

	// A drag whose context has ended sends nothing, even to its own cell.
	canceled, stop := context.WithCancel(t.Context())
	stop()
	if err = Drag(canceled, s, vt.MouseLeft, image.Pt(3, 1), image.Pt(3, 1), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Drag = %v, want context.Canceled", err)
	}
	if got := mouseInputs(s.Timeline()); len(got) != 3 {
		t.Errorf("mouse input after a canceled drag = %q, want nothing more", got)
	}
}
