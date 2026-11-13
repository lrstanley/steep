// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"getsteep.dev/steep"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/effects"
)

// TestCaptions records the shop with captions and callouts, for the
// captions guide: as a demo, which pauses for a reading time at each, and
// again from its timeline as a test's recording, which plays at its own
// pace.
func TestCaptions(t *testing.T) {
	var typing, hold time.Duration
	if exporting() {
		typing, hold = 80*time.Millisecond, 900*time.Millisecond
	}
	h := steep.NewHarness(t, NewModel(), demo("captions-demo", true,
		effects.Captions(effects.DefaultCaptionPause(effects.AutoPause)),
		effects.Keys(time.Second),
		effects.Pause(),
	)...)
	h.Wait("Sencha").
		Caption("The Tea Shop lists teas to filter and brew").
		Callout(effects.Text("/ filter"), "Press / to filter the menu").
		Key("/").
		Type("green", reel.Delay(typing)).
		EndCallout().
		Caption("Only the green teas are left").
		KeyEsc().
		WaitNot("Filter:").
		EndCaption()
	h.Locate(reel.Contains("Oolong")).LeftClick().
		Callout(effects.Text("Oolong"), "Click a tea, then press enter to brew it", effects.Spotlight()).
		Key("enter").
		Wait("█████").
		EndCallout().
		Caption("The status line shows how far along it is", effects.CaptionPause(0)).
		Wait("Oolong is ready").
		Callout(effects.Text("Oolong is ready"), "Ready to pour", effects.CaptionSide(effects.SideTop)).
		Sleep(hold)
	h.Close()
	if !exporting() {
		return
	}
	tl, err := h.Timeline().Between("", "close:1")
	if err != nil {
		t.Fatal(err)
	}
	err = reel.Export(context.Background(), tl, reel.Profile{
		Outputs: reel.MustFiles(filepath.Join(demoDir, "captions-test.webp")),
		Render:  look(),
		Effects: []reel.Effect{effects.Captions(effects.WithoutCaptionPauses()), effects.Keys(time.Second)},
	})
	if err != nil {
		t.Fatal(err)
	}
}
