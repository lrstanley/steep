// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"getsteep.dev/steep/reel/internal/testutil"
	"getsteep.dev/steep/reel/vt"
)

// castOf reads the asciicast file src.
func castOf(t *testing.T, src string) *Timeline {
	t.Helper()
	tl, err := ReadTimeline(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

// exportOver exports tl with p to a GIF, and checks that it fails over
// limit, without writing the GIF.
func exportOver(t *testing.T, tl *Timeline, p Profile, limit string) *LimitError {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.gif")
	p.Outputs = MustFiles(path)
	err := Export(t.Context(), tl, p)
	var le *LimitError
	if !errors.As(err, &le) || !errors.Is(err, ErrLimit) || le.Limit != limit {
		t.Fatalf("Export = %v, want a %s LimitError", err, limit)
	}
	if _, serr := os.Stat(path); !errors.Is(serr, os.ErrNotExist) {
		t.Errorf("the output was written: %v", serr)
	}
	return le
}

// TestLimitsCells checks that a terminal larger than Limits.Cells, from a
// header or a resize, fails before it is created.
func TestLimitsCells(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"header": `{"version":2,"width":4096,"height":4096}` + "\n" + `[0,"o","x"]`,
		"resize": `{"version":2,"width":80,"height":24}` + "\n" + `[0,"o","x"]` + "\n" + `[0.1,"r","4096x4096"]`,
	} {
		le := exportOver(t, castOf(t, src), Profile{}, LimitCells)
		if le.Value != 4096*4096 || le.Max != DefaultCells {
			t.Errorf("%s: %+v, want 16777216 cells over %d", name, le, DefaultCells)
		}
	}
	// The limit can be raised.
	tl := castOf(t, `{"version":2,"width":600,"height":500}`+"\n"+`[0,"o","x"]`)
	exportOver(t, tl, Profile{}, LimitCells)
	p := Profile{Outputs: MustFiles(filepath.Join(t.TempDir(), "out.snap")), Limits: Limits{Cells: 600 * 500}}
	if err := Export(t.Context(), tl, p); err != nil {
		t.Errorf("Export with Cells raised: %v", err)
	}
}

// TestLimitsPixels checks that a frame larger than Limits.Pixels fails
// before it is allocated, at the start or after a resize.
func TestLimitsPixels(t *testing.T) {
	t.Parallel()
	rast := testutil.Rasterizer{Cell: image.Pt(10, 20)}
	for name, src := range map[string]string{
		// 80x24 cells of 10x20 pixels are 384,000 pixels.
		"header": `{"version":2,"width":80,"height":24}` + "\n" + `[0,"o","x"]`,
		"resize": `{"version":2,"width":40,"height":10}` + "\n" + `[0,"o","x"]` + "\n" + `[0.1,"r","80x24"]` + "\n" + `[0.2,"o","y"]`,
	} {
		le := exportOver(t, castOf(t, src), Profile{Rasterizer: rast, Limits: Limits{Pixels: 300_000}}, LimitPixels)
		if le.Value != 384_000 || le.Max != 300_000 {
			t.Errorf("%s: %+v, want 384000 pixels over 300000", name, le)
		}
	}
}

// TestLimitsPlaybackSpeed checks that a tiny speed fails over
// Limits.Playback rather than overflowing.
func TestLimitsPlaybackSpeed(t *testing.T) {
	t.Parallel()
	tl := castOf(t, `{"version":2,"width":10,"height":2}`+"\n"+`[0,"o","a"]`+"\n"+`[1,"o","b"]`)
	for _, speed := range []float64{1e-12, 1e-300} {
		exportOver(t, tl, Profile{Rasterizer: testutil.Rasterizer{}, Speed: speed}, LimitPlayback)
	}
}

// TestLimitsPlaybackLength checks that a timeline longer than
// Limits.Playback fails, unless it is raised or removed.
func TestLimitsPlaybackLength(t *testing.T) {
	t.Parallel()
	tl := castOf(t, `{"version":2,"width":10,"height":2}`+"\n"+`[0,"o","a"]`+"\n"+`[660,"o","b"]`)
	le := exportOver(t, tl, Profile{Rasterizer: testutil.Rasterizer{}}, LimitPlayback)
	if le.Max != int64(DefaultPlayback) {
		t.Errorf("Max = %v, want %v", time.Duration(le.Max), DefaultPlayback)
	}
	for _, lim := range []time.Duration{time.Hour, -1} {
		p := Profile{Rasterizer: testutil.Rasterizer{}, Outputs: MustFiles(filepath.Join(t.TempDir(), "out.gif")), Limits: Limits{Playback: lim}}
		if err := Export(t.Context(), tl, p); err != nil {
			t.Errorf("Export with Playback %v: %v", lim, err)
		}
	}
}

// TestLimitsPlaybackHold checks that a long gap while an indicator animates
// fails at once, rather than repainting a frame every 50ms for a year.
func TestLimitsPlaybackHold(t *testing.T) {
	t.Parallel()
	tl := castOf(t, `{"version":2,"width":10,"height":2}`+"\n"+`[0,"o","\u001b]9;4;3\u0007"]`+"\n"+`[31536000,"o","x"]`)
	start := time.Now()
	exportOver(t, tl, Profile{}, LimitPlayback)
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("took %v", took)
	}
}

// TestLimitsPlaybackPauses checks that frames effects insert count toward
// Limits.Playback, though each is shorter than it.
func TestLimitsPlaybackPauses(t *testing.T) {
	t.Parallel()
	tl := castOf(t, `{"version":2,"width":10,"height":2}`+"\n"+`[0,"o","a"]`)
	pauses := effectFunc(func(f Frame, emit func(Frame)) {
		emit(f)
		for range 2 {
			p := f
			p.Paused, p.Delay = true, 6*time.Second
			emit(p)
		}
	})
	p := Profile{Rasterizer: testutil.Rasterizer{}, Effects: []Effect{pauses}, Limits: Limits{Playback: 10 * time.Second}}
	exportOver(t, tl, p, LimitPlayback)
}

// TestLimitsPlaybackFrame checks that a frame longer than Limits.Playback
// fails before the next stage sees it, so a stage that splits frames into
// steps never works through it.
func TestLimitsPlaybackFrame(t *testing.T) {
	t.Parallel()
	tl := castOf(t, `{"version":2,"width":10,"height":2}`+"\n"+`[0,"o","a"]`)
	long := effectFunc(func(f Frame, emit func(Frame)) {
		f.Paused, f.Delay = true, 24*time.Hour
		emit(f)
	})
	var longest time.Duration
	seen := effectFunc(func(f Frame, emit func(Frame)) {
		longest = max(longest, f.Delay)
		emit(f)
	})
	p := Profile{Rasterizer: testutil.Rasterizer{}, Effects: []Effect{long, seen}}
	exportOver(t, tl, p, LimitPlayback)
	if longest > DefaultPlayback {
		t.Errorf("the next stage saw a frame of %v", longest)
	}
}

// countingAnimator is a fakeAnimator that counts its repaints.
type countingAnimator struct {
	fakeAnimator

	repaints int
}

func (a *countingAnimator) Repaint(dst *image.NRGBA, scr vt.Screen, st vt.State, from, at time.Duration) {
	a.repaints++
	a.fakeAnimator.Repaint(dst, scr, st, from, at)
}

// TestPipelineSweepStops checks that a held frame stops being repainted
// after a write fails or the context is canceled.
func TestPipelineSweepStops(t *testing.T) {
	t.Parallel()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for name, tc := range map[string]struct {
		ctx  context.Context
		out  *collectSink
		want error
	}{
		"write": {ctx: t.Context(), out: &collectSink{err: errors.New("disk full")}},
		"ctx":   {ctx: canceled, out: &collectSink{}, want: context.Canceled},
	} {
		rast := &countingAnimator{}
		p := newPipeline(tc.ctx, rast, settings{step: 10 * ms, speed: 1}, nil, tc.out)
		if err := p.record(cellsOf(2, 1, "spin", 0), nil); err != nil {
			t.Fatal(err)
		}
		err := p.finish(10 * time.Second)
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%s: finish = %v, want an error", name, err)
		}
		if rast.repaints > 1 {
			t.Errorf("%s: %d repaints after the error", name, rast.repaints)
		}
	}
}

// TestPlayerLimits checks that a Player holds its terminal and images to
// its limits.
func TestPlayerLimits(t *testing.T) {
	t.Parallel()
	tl := castOf(t, `{"version":2,"width":4096,"height":4096}`+"\n"+`[0,"o","x"]`)
	if _, err := NewPlayer(t.Context(), tl, Limits{}); !errors.Is(err, ErrLimit) {
		t.Errorf("NewPlayer of 4096x4096 = %v, want ErrLimit", err)
	}
	tl = castOf(t, `{"version":2,"width":80,"height":24}`+"\n"+`[0,"o","x"]`)
	p, err := NewPlayer(t.Context(), tl, Limits{Pixels: 1000})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err = p.Image(); !errors.Is(err, ErrLimit) {
		t.Errorf("Image over 1000 pixels = %v, want ErrLimit", err)
	}
}

// TestLimitsMerge checks that Profile.Merge merges limits by field.
func TestLimitsMerge(t *testing.T) {
	t.Parallel()
	got := Profile{Limits: Limits{Playback: time.Hour, Cells: 5}}.Merge(Profile{Limits: Limits{Cells: -1, Pixels: 7}})
	if want := (Limits{Playback: time.Hour, Cells: -1, Pixels: 7}); got.Limits != want {
		t.Errorf("Merge = %+v, want %+v", got.Limits, want)
	}
}

// TestLimitErrorMessages checks that each LimitError names its limit and
// values.
func TestLimitErrorMessages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want string
	}{
		{Limits{}.checkPlayback(float64(11 * time.Minute)), "reel: playback of 11m0s is over the limit of 10m0s"},
		{Limits{}.checkPlayback(1e300), "over the limit of 10m0s"},
		{Limits{}.checkCells(1000, 1000), "reel: a terminal of 1000000 cells is over the limit of 250000"},
		{Limits{}.checkPixels(image.Rect(0, 0, 5000, 5000)), "reel: a frame of 25000000 pixels is over the limit of 16777216"},
		{Limits{Cells: 10}.checkCells(1<<40, 1<<40), "9223372036854775807 cells"},
	} {
		if tc.err == nil || !strings.Contains(tc.err.Error(), tc.want) {
			t.Errorf("error = %v, want %q", tc.err, tc.want)
		}
	}
	for _, err := range []error{
		Limits{}.checkPlayback(float64(10 * time.Minute)),
		Limits{}.checkCells(1000, 250),
		Limits{Cells: -1}.checkCells(1<<40, 1<<40),
		Limits{Pixels: -1, Playback: -1}.checkPixels(image.Rect(0, 0, 1<<30, 1<<30)),
	} {
		if err != nil {
			t.Errorf("error = %v, want none", err)
		}
	}
}

// TestSessionLimits checks that a session's terminal is held to its cell
// limit when it opens and when it is resized.
func TestSessionLimits(t *testing.T) {
	t.Parallel()
	if _, err := Open(t.Context(), 4096, 4096); !errors.Is(err, ErrLimit) {
		t.Errorf("Open of 4096x4096 = %v, want ErrLimit", err)
	}
	s, err := Open(t.Context(), 10, 2, WithLimits(Limits{Cells: 100}))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Resize(20, 10); !errors.Is(err, ErrLimit) {
		t.Errorf("Resize to 20x10 over 100 cells = %v, want ErrLimit", err)
	}
	if cols, rows := s.Terminal().Size(); cols != 10 || rows != 2 {
		t.Errorf("size after a refused resize = %dx%d, want 10x2", cols, rows)
	}
	for ev := range s.Timeline().Events() {
		if _, ok := ev.(Resize); ok {
			t.Errorf("a refused resize was recorded: %+v", ev)
		}
	}
}
