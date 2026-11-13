// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/render"
)

// note is an annotation event of a timeline built with [buildTimeline].
func note(atMS int, a reel.Annotation) event {
	obj := `{"kind":"` + a.Kind + `"`
	if d := string(a.Data); len(d) > 2 {
		obj += "," + d[1:]
	} else {
		obj += "}"
	}
	return event{time.Duration(atMS) * ms, "R", obj}
}

func TestNewCaptionErrors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		make        func() (reel.Annotation, error)
		unsupported bool
	}{
		"empty":                {make: func() (reel.Annotation, error) { return NewCaption(" \n\x1b[1m\n ") }},
		"caption left":         {make: func() (reel.Annotation, error) { return NewCaption("x", CaptionSide(SideLeft)) }},
		"unknown side":         {make: func() (reel.Annotation, error) { return NewCaption("x", CaptionSide(Side(9))) }},
		"caption spotlight":    {make: func() (reel.Annotation, error) { return NewCaption("x", Spotlight()) }},
		"negative for":         {make: func() (reel.Annotation, error) { return NewCaption("x", CaptionFor(-ms)) }},
		"negative pause":       {make: func() (reel.Annotation, error) { return NewCaption("x", CaptionPause(-5*ms)) }},
		"long pause":           {make: func() (reel.Annotation, error) { return NewCaption("x", CaptionPause(6*time.Minute)) }},
		"long for":             {make: func() (reel.Annotation, error) { return NewCaption("x", CaptionFor(6*time.Minute)) }},
		"nil anchor":           {make: func() (reel.Annotation, error) { return NewCallout(nil, "x") }},
		"nil regexp anchor":    {make: func() (reel.Annotation, error) { return NewCallout(Regexp(nil), "x") }},
		"custom anchor":        {make: func() (reel.Annotation, error) { return NewCallout(customRegion{}, "x") }, unsupported: true},
		"callout without text": {make: func() (reel.Annotation, error) { return NewCallout(Text("x"), "") }},
	} {
		_, err := tc.make()
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if got := errors.Is(err, errors.ErrUnsupported); got != tc.unsupported {
			t.Errorf("%s: %v: errors.Is(err, errors.ErrUnsupported) = %v, want %v", name, err, got, tc.unsupported)
		}
	}
}

// TestCaptionRoundTrip checks that an annotation reads back as the caption
// it was made with.
func TestCaptionRoundTrip(t *testing.T) {
	t.Parallel()
	a, err := NewCallout(Regexp(regexp.MustCompile(`v\d+`)), "Deploys\n\n  the build  ",
		CaptionPause(AutoPause), CaptionFor(2*time.Second),
		CaptionUntil("done"), CaptionSide(SideRight), Spotlight())
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != CaptionKind {
		t.Fatalf("kind %q, want %q", a.Kind, CaptionKind)
	}
	want := `{"text":"Deploys\n\n  the build  ","anchor":{"type":"regexp","pattern":"v\\d+"},"pause_auto":true,"for_ms":2000,"until":"done","side":"right","spotlight":true}`
	if string(a.Data) != want {
		t.Fatalf("data\n%s\nwant\n%s", a.Data, want)
	}
	c, err := readCaption(a)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.lines, []string{"Deploys", "", "the build"}) || !c.callout() ||
		c.pause != AutoPause || c.end != 2*time.Second || c.until != "done" || c.side != SideRight || !c.spotlight {
		t.Fatalf("read back %+v", *c)
	}

	a, err = NewCaption("plain", CaptionPause(0))
	if err != nil {
		t.Fatal(err)
	}
	if wantPlain := `{"text":"plain","pause_ms":0}`; string(a.Data) != wantPlain {
		t.Fatalf("data %s, want %s", a.Data, wantPlain)
	}
	if c, err = readCaption(a); err != nil || c.pause != 0 || c.end != -1 || c.callout() {
		t.Fatalf("read back %+v, %v", c, err)
	}
	if c, err = readCaption(must(NewCaption("defaults"))); err != nil || c.pause != pauseDefault {
		t.Fatalf("read back %+v, %v", c, err)
	}

	if callout, endErr := readCaptionEnd(EndCaption()); endErr != nil || callout {
		t.Fatalf("EndCaption reads back as callout %v, %v", callout, endErr)
	}
	if callout, endErr := readCaptionEnd(EndCallout()); endErr != nil || !callout {
		t.Fatalf("EndCallout reads back as callout %v, %v", callout, endErr)
	}
}

func must(a reel.Annotation, err error) reel.Annotation {
	if err != nil {
		panic(err)
	}
	return a
}

func TestCaptionLines(t *testing.T) {
	t.Parallel()
	for in, want := range map[string][]string{
		"one line":                       {"one line"},
		"\n\n two \r\n\r\n lines \n\n":   {"two", "", "lines"},
		"bell\x07 and \x1b[1mescape":     {"bell and escape"},
		"tabs\tare\tspaces":              {"tabs are spaces"},
		"1\n2\n3\n4\n5\n6\n7\n8":         {"1", "2", "3", "4", "5", "6 …"},
		"\u202egnp.exe\u200b":            {"gnp.exe"},
		"   ":                            nil,
		"日本語のテキスト\nand more":             {"日本語のテキスト", "and more"},
		"  leading and trailing space  ": {"leading and trailing space"},
	} {
		if got := captionLines(in); !slices.Equal(got, want) {
			t.Errorf("captionLines(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadingTime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		lines []string
		want  time.Duration
	}{
		{[]string{"one"}, 1400 * ms},
		{[]string{"one two three"}, 1480 * ms},
		{[]string{"a b c d e", "f g h i j"}, 3300 * ms},
		{[]string{strings.Repeat("word ", 2000)}, MaxDuration},
	} {
		if got := readingTime(tc.lines); got != tc.want {
			t.Errorf("readingTime(%q) = %v, want %v", tc.lines, got, tc.want)
		}
	}
}

// TestCaptionEnds checks where captions end: where the next of their sort
// replaces them, after a time, at a mark, at an end of their sort, or as
// their pause ends.
func TestCaptionEnds(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 8, "",
		output(0, "$ ./demo"),
		note(100, must(NewCaption("one"))),
		note(200, must(NewCallout(Text("demo"), "c1", CaptionFor(300*ms)))),
		note(400, must(NewCaption("two"))),
		mark(600, "m"),
		note(700, must(NewCallout(Text("demo"), "c2", CaptionUntil("m2")))),
		mark(800, "m2"),
		note(900, EndCaption()),
		note(1000, must(NewCaption("pause only", CaptionFor(0)))),
		note(1100, EndCaption()),
		note(1100, must(NewCaption("to the end"))),
		note(1200, EndCallout()),
		output(1300, "!"),
	)
	s := &captionsStage{
		ctx:   &reel.Context{Timeline: tl, Step: 10 * ms, Speed: 1},
		marks: make(map[string][]time.Duration),
		last:  [2]int{-1, -1},
	}
	s.readEvents()
	if len(s.caps) != 6 || len(s.errs) > 0 {
		t.Fatalf("read %d captions, errors %v", len(s.caps), s.errs)
	}
	want := []time.Duration{
		400 * ms,  // Replaced by the next caption.
		500 * ms,  // For 300ms; a caption does not end it.
		900 * ms,  // At its end.
		800 * ms,  // Until m2.
		1000 * ms, // Only while its pause lasts.
		openTo,    // The end placed before it, at its offset, and the callout's end leave it.
	}
	for i, w := range want {
		if got := s.endOf(i); got != w {
			t.Errorf("caption %d (%q) ends at %v, want %v", i, s.caps[i].lines, got, w)
		}
	}
}

// captionRenderOpts are the render options of the caption tests: no
// chrome, margin, or padding, so callouts meet the edges of the frame.
var captionRenderOpts = []render.Option{render.WithPadding(0)}

// captionFrames exports tl through Captions with opts, and returns the
// frames emitted.
func captionFrames(t *testing.T, tl *reel.Timeline, effects ...reel.Effect) []reel.Frame {
	t.Helper()
	return exportFrames(t, tl, reel.Profile{
		Rasterizer: zoomRenderer(t, captionRenderOpts...),
		Render:     captionRenderOpts,
		Effects:    effects,
	})
}

// pausesOf returns the offset and delay of each paused frame.
func pausesOf(frames []reel.Frame) [][2]time.Duration {
	var out [][2]time.Duration
	for _, f := range frames {
		if f.Paused {
			out = append(out, [2]time.Duration{f.At, f.Delay})
		}
	}
	return out
}

// TestCaptionsPauses checks where captions pause: as each says, as the
// effect defaults, never with WithoutCaptionPauses, and not for a caption
// that starts in what a Hide cuts.
func TestCaptionsPauses(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 30, 6, "",
		output(0, "$ ./demo"),
		note(100, must(NewCaption("own pause", CaptionPause(500*ms)))),
		note(200, must(NewCaption("no pause", CaptionPause(0)))),
		note(300, must(NewCaption("default pause"))),
		note(400, must(NewCallout(Text("demo"), "read this", CaptionPause(AutoPause)))),
		mark(500, "hide"),
		note(600, must(NewCaption("hidden", CaptionPause(time.Second)))),
		mark(700, "show"),
		output(800, "!"),
		mark(900, "end"),
	)
	auto := readingTime([]string{"read this"}).Round(50 * ms)
	defaultAuto := readingTime([]string{"default pause"}).Round(50 * ms)
	hide := Hide(HideBetween("hide", "show"))
	for _, tc := range []struct {
		name    string
		hide    bool
		effects []reel.Effect
		want    [][2]time.Duration
	}{
		{"own", false, []reel.Effect{Captions()}, [][2]time.Duration{{100 * ms, 500 * ms}, {400 * ms, auto}, {600 * ms, time.Second}}},
		{"default", false, []reel.Effect{Captions(DefaultCaptionPause(250 * ms))}, [][2]time.Duration{{100 * ms, 500 * ms}, {300 * ms, 250 * ms}, {400 * ms, auto}, {600 * ms, time.Second}}},
		{"default auto", false, []reel.Effect{Captions(DefaultCaptionPause(AutoPause))}, [][2]time.Duration{{100 * ms, 500 * ms}, {300 * ms, defaultAuto}, {400 * ms, auto}, {600 * ms, time.Second}}},
		{"none", false, []reel.Effect{Captions(WithoutCaptionPauses(), DefaultCaptionPause(time.Second))}, nil},
		{"once", false, []reel.Effect{Captions(), Captions(DefaultCaptionPause(time.Second))}, [][2]time.Duration{{100 * ms, 500 * ms}, {400 * ms, auto}, {600 * ms, time.Second}}},
		{"hidden", true, []reel.Effect{hide, Captions()}, [][2]time.Duration{{100 * ms, 500 * ms}, {400 * ms, auto}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := captionFrames(t, tl, tc.effects...)
			if got := pausesOf(out); !slices.Equal(got, tc.want) {
				t.Fatalf("pauses %v, want %v", got, tc.want)
			}
			// The pauses are all the captions add.
			var paused time.Duration
			for _, p := range tc.want {
				paused += p[1]
			}
			base := []reel.Effect{Captions(WithoutCaptionPauses())}
			if tc.hide {
				base = append(base, hide)
			}
			if got, want := totalDelay(out), totalDelay(captionFrames(t, tl, base...))+paused; got != want {
				t.Fatalf("total delay %v, want %v", got, want)
			}
		})
	}
}

// TestCaptionsPauseBeforeOutput checks that a caption placed just before
// output, as a tape places one before a key, pauses on the screen before
// that output, though the step grid rounds the end of that screen's frame
// to before the caption.
func TestCaptionsPauseBeforeOutput(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 30, 6, "",
		output(0, "$ ./demo"),
		output(300, "\r\nbefore"),
		note(520, must(NewCaption("press a key", CaptionPause(time.Second)))),
		output(523, "\r\nafter"),
		mark(900, "end"),
	)
	out := captionFrames(t, tl, Captions())
	if got, want := pausesOf(out), [][2]time.Duration{{520 * ms, time.Second}}; !slices.Equal(got, want) {
		t.Fatalf("pauses %v, want %v", got, want)
	}
	text := frameAt(t, out, 520*ms, true).Cells.Screen.Text()
	if !strings.Contains(text, "before") || strings.Contains(text, "after") {
		t.Fatalf("paused on screen %q, want the one before the output", text)
	}
}

// TestCaptionsPauseAfterBriefChange checks that a caption placed just after
// the screen changes, as after a wait for output, pauses on the changed
// screen, though the step grid drops its frame for the next one.
func TestCaptionsPauseAfterBriefChange(t *testing.T) {
	t.Parallel()
	var lines strings.Builder
	lines.WriteString("$ ./demo")
	for i := range 12 {
		fmt.Fprintf(&lines, "\r\nline %d", i)
	}
	for _, tc := range []struct {
		name         string
		change, next event
	}{
		{"output", output(500, "\r\nready"), output(502, "!")},
		{"scroll", event{500 * ms, "s", "4"}, event{502 * ms, "s", "3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tl := buildTimeline(t, 30, 6, "",
				output(0, lines.String()),
				output(300, "\r\nbefore"),
				tc.change,
				note(501, must(NewCaption("look", CaptionPause(time.Second)))),
				tc.next,
				mark(900, "end"),
			)
			out := captionFrames(t, tl, Captions())
			if got := pausesOf(out); len(got) != 1 || got[0][1] != time.Second {
				t.Fatalf("pauses %v, want one of 1s", got)
			}
			before := frameAt(t, out, 300*ms, false).Cells.Screen.Text()
			for _, f := range out {
				if f.Paused && f.Cells.Screen.Text() == before {
					t.Fatalf("paused on the screen from before the change: %q", before)
				}
			}
		})
	}
}

// TestCaptionsMany checks that many captions at once cost each the same:
// those another replaces at once are not laid out, and only the last shows.
func TestCaptionsMany(t *testing.T) {
	t.Parallel()
	const n = 20000
	events := []event{output(0, "$ ./demo")}
	for i := range n {
		events = append(events, note(100, must(NewCaption(fmt.Sprintf("caption %d", i)))))
	}
	events = append(events, output(200, "!"), mark(300, "end"))
	tl := buildTimeline(t, 30, 6, "", events...)
	with := captionFrames(t, tl, Captions())
	last := buildTimeline(t, 30, 6, "",
		output(0, "$ ./demo"), note(100, must(NewCaption(fmt.Sprintf("caption %d", n-1)))), output(200, "!"), mark(300, "end"))
	want := captionFrames(t, last, Captions())
	if len(with) != len(want) {
		t.Fatalf("%d frames, want %d", len(with), len(want))
	}
	for i := range want {
		if !bytes.Equal(with[i].Image.Pix, want[i].Image.Pix) {
			t.Fatalf("frame %d differs from showing only the last caption", i)
		}
	}
}

// TestCaptionsInOtherPauses checks that a caption shows through another
// effect's pause.
func TestCaptionsInOtherPauses(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 30, 6, "",
		output(0, "$ ./demo"),
		note(100, must(NewCaption("still here"))),
		mark(300, "m"),
		mark(600, "end"),
	)
	pause := Pause(PauseAtMarks(time.Second, "m"), WithoutBadge())
	with := frameAt(t, captionFrames(t, tl, pause, Captions()), 300*ms, true)
	without := frameAt(t, captionFrames(t, tl, pause), 300*ms, true)
	if bytes.Equal(with.Image.Pix, without.Image.Pix) {
		t.Fatal("the mark's pause leaves out the caption")
	}

	// A caption placed with the mark shows through its pause too.
	tl = buildTimeline(t, 30, 6, "",
		output(0, "$ ./demo"),
		mark(300, "m"),
		note(300, must(NewCaption("with the mark"))),
		mark(600, "end"),
	)
	with = frameAt(t, captionFrames(t, tl, pause, Captions()), 300*ms, true)
	without = frameAt(t, captionFrames(t, tl, pause), 300*ms, true)
	if bytes.Equal(with.Image.Pix, without.Image.Pix) {
		t.Fatal("the mark's pause leaves out the caption placed with it")
	}
}

// TestCaptionsPauseShowsOthers checks that a caption's pause on the frame
// before shows the captions that showed then, though they end before the
// next frame.
func TestCaptionsPauseShowsOthers(t *testing.T) {
	t.Parallel()
	events := func(others bool) []event {
		evs := []event{output(0, "$ ./demo"), output(300, "\r\nbefore")}
		if others {
			evs = append(evs, note(400, must(NewCaption("brief", CaptionSide(SideTop), CaptionFor(122*ms)))))
		}
		return append(evs,
			note(520, must(NewCallout(Text("demo"), "look", CaptionPause(time.Second)))),
			output(523, "\r\nafter"),
			mark(900, "end"),
		)
	}
	with := frameAt(t, captionFrames(t, buildTimeline(t, 30, 6, "", events(true)...), Captions()), 520*ms, true)
	without := frameAt(t, captionFrames(t, buildTimeline(t, 30, 6, "", events(false)...), Captions()), 520*ms, true)
	if bytes.Equal(with.Image.Pix, without.Image.Pix) {
		t.Fatal("the callout's pause leaves out the caption that showed then")
	}
}

// TestCaptionsStills checks that a still shows the screen of its own mark,
// with the captions of its moment, whether or not the captions pause.
func TestCaptionsStills(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 30, 6, "",
		output(0, "$ ./demo"),
		mark(100, "a"),
		note(200, must(NewCaption("press a key", CaptionPause(time.Second)))),
		output(250, "\r\nafter"),
		mark(300, "b"),
		mark(400, "end"),
	)
	still := func(fx reel.Effect) *image.NRGBA {
		dir := t.TempDir()
		err := reel.Export(t.Context(), tl, reel.Profile{
			Outputs:    []reel.File{{Path: filepath.Join(dir, "{mark}.png"), Marks: []string{"a", "b"}}},
			Rasterizer: zoomRenderer(t, captionRenderOpts...),
			Render:     captionRenderOpts,
			Effects:    []reel.Effect{fx},
		})
		if err != nil {
			t.Fatal(err)
		}
		return readPNG(t, filepath.Join(dir, "b.png"))
	}
	if !bytes.Equal(still(Captions()).Pix, still(Captions(WithoutCaptionPauses())).Pix) {
		t.Fatal("a caption's pause changes the still")
	}
}

// TestCaptionsLeaveCardsAlone checks that a frame without a terminal, such
// as a card, passes through as is.
func TestCaptionsLeaveCardsAlone(t *testing.T) {
	t.Parallel()
	card := reel.Frame{Image: image.NewNRGBA(image.Rect(0, 0, 4, 4)), Delay: 50 * ms}
	var got []reel.Frame
	(&captionsStage{}).run(card, func(f reel.Frame) { got = append(got, f) })
	if len(got) != 1 || got[0].Image != card.Image || got[0].Delay != card.Delay {
		t.Fatalf("emitted %+v, want the card as is", got)
	}
}

// frameAt returns the frame at offset at, paused or not as paused.
func frameAt(t *testing.T, frames []reel.Frame, at time.Duration, paused bool) reel.Frame {
	t.Helper()
	i := slices.IndexFunc(frames, func(f reel.Frame) bool { return f.At == at && f.Paused == paused })
	if i < 0 {
		t.Fatalf("no frame at %v (paused %v) in %+v", at, paused, infoOf(frames))
	}
	return frames[i]
}

// TestCaptionsPill checks the look of captions: at the bottom of an empty
// screen, at the top where the bottom shows text, and on two lines where
// they are long.
func TestCaptionsPill(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 8, "",
		output(0, "$ ./demo"),
		note(100, must(NewCaption("Press enter to start", CaptionPause(100*ms)))),
		output(200, "\x1b[8;1Hstatus: ok · 3 jobs running · press q to quit"),
		note(300, must(NewCaption("The status line shows what runs, so this caption goes to the top instead", CaptionPause(100*ms)))),
		note(400, must(NewCaption("Forced to the bottom", CaptionSide(SideBottom), CaptionPause(100*ms)))),
		mark(600, "end"),
	)
	out := captionFrames(t, tl, Captions())
	var golden []reel.Frame
	for _, at := range []time.Duration{100 * ms, 300 * ms, 400 * ms} {
		golden = append(golden, frameAt(t, out, at, true))
	}
	assertGolden(t, "captions-pill", golden)
}

// TestCaptionsCallout checks the look of callouts: below an anchor in the
// top left corner, above one in the bottom right, moved along their side
// to stay in the frame with the caret still on the anchor, on a side asked
// for, and as a caption where the anchor matches nothing.
func TestCaptionsCallout(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 10, "",
		output(0, "$ ./deploy --env prod\r\nbuilding... done\r\n\x1b[10;34Hv1.2.3"),
		note(100, must(NewCallout(Text("$"), "The prompt", CaptionPause(100*ms)))),
		note(200, must(NewCallout(Text("v1.2.3"), "The version this deploy ships", CaptionPause(100*ms)))),
		note(300, must(NewCallout(Text("building"), "Asked to the right", CaptionSide(SideRight), CaptionPause(100*ms)))),
		note(400, must(NewCallout(Text("nowhere"), "No anchor, so a caption", CaptionPause(100*ms)))),
		mark(600, "end"),
	)
	out := captionFrames(t, tl, Captions())
	var golden []reel.Frame
	for _, at := range []time.Duration{100 * ms, 200 * ms, 300 * ms, 400 * ms} {
		golden = append(golden, frameAt(t, out, at, true))
	}
	assertGolden(t, "captions-callout", golden)
}

// TestCaptionsSpotlight checks a spotlight, which darkens all but the
// anchor for as long as its callout shows, and marks its frames isolated.
func TestCaptionsSpotlight(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 8, "",
		output(0, "$ make deploy\r\nstep 1/3: build\r\nstep 2/3: test\r\nstep 3/3: ship"),
		note(100, must(NewCallout(Text("step 2/3: test"), "Tests run before anything ships",
			Spotlight(), CaptionPause(100*ms), CaptionFor(200*ms)))),
		output(250, "\r\n$ "),
		mark(500, "end"),
	)
	out := captionFrames(t, tl, Captions(WithCaptionAccent(nil)))
	f := frameAt(t, out, 100*ms, true)
	if !f.Isolated {
		t.Error("spotlight frame not isolated")
	}
	for _, g := range out {
		if !g.Paused && g.At >= 300*ms && g.Isolated {
			t.Errorf("frame at %v isolated after the callout ended", g.At)
		}
	}
	assertGolden(t, "captions-spotlight", []reel.Frame{f})
}

// TestCaptionsBadAnnotation checks that a caption that cannot be read is
// left out, and reported once the recording ends.
func TestCaptionsBadAnnotation(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 30, 6, "",
		output(0, "$ ./demo"),
		event{100 * ms, "R", `{"kind":"reel.caption","text":"x","side":"diagonal"}`},
		mark(300, "end"),
	)
	p := reel.Profile{
		Outputs:    reel.MustFiles(t.TempDir() + "/out.gif"),
		Rasterizer: zoomRenderer(t, captionRenderOpts...),
		Effects:    []reel.Effect{Captions()},
	}
	err := reel.Export(t.Context(), tl, p)
	if err == nil || !strings.Contains(err.Error(), "diagonal") {
		t.Fatalf("Export = %v, want the unknown side reported", err)
	}
}

// wrapped is an effect that wraps another.
type wrapped struct{ reel.Effect }

func (w wrapped) Unwrap() reel.Effect { return w.Effect }

func TestHasCaptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fx   []reel.Effect
		want bool
	}{
		{nil, false},
		{[]reel.Effect{Keys(time.Second), Pause()}, false},
		{[]reel.Effect{Keys(time.Second), Captions(WithoutCaptionPauses())}, true},
		{[]reel.Effect{wrapped{Captions()}}, true},
		{[]reel.Effect{wrapped{Keys(time.Second)}}, false},
	} {
		if got := HasCaptions(tc.fx); got != tc.want {
			t.Errorf("HasCaptions(%v) = %v, want %v", tc.fx, got, tc.want)
		}
	}
}
