// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"image"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/effects"
	"getsteep.dev/steep/reel/region"
	"getsteep.dev/steep/reel/vt"
)

// captionNotes describes each caption annotation in tl: its kind and its
// data.
func captionNotes(tl *reel.Timeline) []string {
	var out []string
	for a := range tl.Annotations() {
		if a.Kind == effects.CaptionKind || a.Kind == effects.CaptionEndKind {
			out = append(out, a.Kind+" "+string(a.Data))
		}
	}
	return out
}

// TestCaptions checks that captions and callouts are placed in the
// recording, and their ends.
func TestCaptions(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithWindowSize(20, 4), WithIgnoreEnv(true))
	h.Wait("size=20x4").
		Caption("first").
		Callout(region.Text("size"), "the size", effects.Spotlight()).
		Caption("second", effects.CaptionPause(time.Second)).
		EndCaption().
		EndCallout()
	h.Close()
	want := []string{
		`reel.caption {"text":"first"}`,
		`reel.caption {"text":"the size","anchor":{"type":"text","text":"size"},"spotlight":true}`,
		`reel.caption {"text":"second","pause_ms":1000}`,
		`reel.caption.end `,
		`reel.caption.end {"callout":true}`,
	}
	if got := captionNotes(h.Timeline()); !slices.Equal(got, want) {
		t.Fatalf("annotations\n%q\nwant\n%q", got, want)
	}
}

// emptyRegion is a region of a caller's own, which no annotation can
// describe.
type emptyRegion struct{}

func (emptyRegion) Rects(*vt.Buffer) []image.Rectangle { return nil }

// TestCaptionErrors checks that captions that cannot be placed are test
// errors, and leave no annotation.
func TestCaptionErrors(t *testing.T) {
	t.Parallel()
	ftb := &fakeTB{TB: t}
	h := NewHarness(ftb, rootTestModel{}, WithIgnoreEnv(true))
	h.Caption(" \x1b[1m ").
		Caption("x", effects.Spotlight()).
		Callout(nil, "x").
		Callout(emptyRegion{}, "x")
	h.Close()
	if got := captionNotes(h.Timeline()); len(got) > 0 {
		t.Fatalf("annotations %q, want none", got)
	}
	errs := ftb.errors()
	if len(errs) != 4 {
		t.Fatalf("errors = %q, want 4", errs)
	}
	for i, want := range []string{"no text", "spotlight", "nil anchor", "unsupported"} {
		if !strings.Contains(errs[i], want) {
			t.Errorf("error %d = %q, want it to mention %q", i, errs[i], want)
		}
	}
}

// TestExportCaptions checks that an export draws the test's captions,
// pausing where they say to, unless the profile has a Captions effect of
// its own.
func TestExportCaptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		effects []reel.Effect
		paused  bool
	}{
		{"default", nil, true},
		{"own", []reel.Effect{effects.Captions(effects.WithoutCaptionPauses())}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			h := NewHarness(t, rootTestModel{}, exportOptions(dir,
				WithRecording(reel.Profile{Outputs: reel.MustFiles("{name}.gif"), Effects: tc.effects}),
				WithExportAlways(),
			)...)
			h.Wait("size=20x4").Caption("hold on", effects.CaptionPause(730*time.Millisecond))
			h.Close()
			// The pause plays with the frames that show the caption after it,
			// which look the same, so it shows in the total.
			g := readGIF(t, filepath.Join(dir, normalizeTestName(t.Name())+".gif"))
			var total int
			for _, d := range g.Delay {
				total += d
			}
			if got := total >= 73; got != tc.paused {
				t.Fatalf("plays %dcs, paused %v, want %v", total, got, tc.paused)
			}
		})
	}
}
