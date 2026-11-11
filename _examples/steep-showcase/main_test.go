// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"os"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"getsteep.dev/steep"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/effects"
	"getsteep.dev/steep/reel/render"
)

// The recording is only written when exporting is turned on, as with:
//
//	STEEP_EXPORT_DIR=testdata/ STEEP_EXPORT_MODE=on go test -v -count 1 ./steep-showcase/
//
// which writes testdata/showcase.gif and testdata/showcase.cast. Exporting
// the .cast again with the same profile writes the same GIF.

// showcaseProfile is what the showcase's recording writes: the steep logo,
// the keys pressed, a badged pause on the last frame, and a PASS or FAIL
// card after it, in a window with chrome.
var showcaseProfile = reel.Profile{
	Outputs: reel.MustFiles("showcase.gif", "showcase.cast"),
	Render: []render.Option{
		render.WithWindowChrome(true),
		render.WithProgramStatus(true),
		render.WithProgressBar(true),
		render.WithPadding(14),
		render.WithMargin(36, lipgloss.Color("#1d212c")),
		render.WithBorderRadius(12),
	},
	Effects: []reel.Effect{
		steep.Logo(2 * time.Second),
		effects.Keys(1200 * time.Millisecond),
		effects.Pause(),
		steep.Outro(2 * time.Second),
	},
}

func TestShowcase(t *testing.T) {
	runShowcase(t, steep.WithRecording(showcaseProfile))
}

// TestInspectorSamples records the sample cast of this example that reel's
// inspector (inspector) offers, into the directory REEL_INSPECTOR_SAMPLES
// names. It is skipped without it.
func TestInspectorSamples(t *testing.T) {
	dir := os.Getenv("REEL_INSPECTOR_SAMPLES")
	if dir == "" {
		t.Skip("REEL_INSPECTOR_SAMPLES is not set")
	}
	runShowcase(t,
		steep.WithIgnoreEnv(true),
		steep.WithSource(true),
		steep.WithRecordMessages(true),
		steep.WithExportAlways(),
		steep.WithArtifacts(dir),
		steep.WithRecording(reel.Profile{Outputs: reel.MustFiles("showcase.cast"), Title: "showcase"}),
	)
}

// runShowcase walks the whole program: the boot, every theme, a deploy that
// goes live, and one that fails.
func runShowcase(t *testing.T, opts ...steep.Option) {
	t.Helper()
	h := steep.NewHarness(t, newModel(), append([]steep.Option{
		steep.WithWindowSize(110, 32),
		steep.WithTimeout(10 * time.Second),
		// steep's terminal reports 256 colors by default; the gradients
		// need every bit.
		steep.WithColor(steep.ColorTrueColor),
	}, opts...)...)

	// Boot: the wordmark sweeps in while OSC 9;4 reports progress.
	h.Wait("all systems nominal").
		Wait("control room").
		Sleep(2500 * time.Millisecond)

	// Cycle every theme; each switch cross-fades the whole palette,
	// including the terminal background the window chrome is tinted from.
	for range themes {
		h.Key("t")
		h.Sleep(1400 * time.Millisecond)
	}

	// Deploy the selected service. OSC 7501 reports it as working (comets
	// around the window), then blocked on a question while it waits for
	// approval (a breathing outline), then done.
	h.Key("d").
		Wait("approval required").
		Sleep(1800 * time.Millisecond).
		Key("y").
		Wait("is live").
		Sleep(2200 * time.Millisecond)

	// Deploy a service whose tests fail, which reports an error.
	h.Key("j").
		Sleep(250 * time.Millisecond).
		Key("j").
		Sleep(400 * time.Millisecond).
		Key("d").
		Wait("deploy failed").
		Sleep(2200 * time.Millisecond)

	// The recording ends here, on the failed deploy: Close, which the
	// harness runs as a cleanup, quits the program, and leaves its exit out.
}
