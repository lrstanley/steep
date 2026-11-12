// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"getsteep.dev/steep"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/effects"
	"getsteep.dev/steep/reel/render"
	"github.com/charmbracelet/x/ansi"
)

// record drives the shop through its whole tour, with marks to cut and time
// effects on, and returns the timeline. It is the recording every reel page
// shows, exported with one change at a time.
func record(t *testing.T) *reel.Timeline {
	t.Helper()
	opts := append(demo("tour", true), steep.WithSource(true), steep.WithRecordMessages(true))
	// Time to follow along, only when recording, so the test stays quick otherwise.
	var typing, hold time.Duration
	if exporting() {
		typing, hold = 80*time.Millisecond, 900*time.Millisecond
	}
	h := steep.NewHarness(t, NewModel(), opts...)
	h.Wait("Sencha").
		KeyDown(reel.Repeat(2)).
		Mark("filter:start").
		Key("/").
		Type("green", reel.Delay(typing)).
		Mark("filter:typed").
		KeyEsc().
		WaitNot("Filter:").
		Mark("filter:done")
	h.Locate(reel.Contains("Oolong")).LeftClick().
		Mark("brew:start").
		Key("enter").
		Wait("█████").
		Mark("brew:mid").
		Wait("Oolong is ready").
		Mark("brew:done").
		Sleep(hold)
	h.Close()
	// The harness ends its own exports where Close starts, so the program's
	// repaint as it exits is left out. Exports of the timeline must cut it too.
	tl, err := h.Timeline().Between("", "close:1")
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestTour(t *testing.T) {
	tl := record(t)
	if !exporting() {
		return
	}

	secret := effects.Text(License)
	row := effects.Regexp(regexp.MustCompile(` Oolong.*3m `))

	// The gallery: the same recording, one change at a time.
	gallery := map[string]reel.Profile{
		// How a window is drawn. Stills, so the differences are easy to see.
		"look-plain.png":  {Render: []render.Option{render.WithScale(demoScale)}},
		"look-chrome.png": {Render: []render.Option{render.WithScale(demoScale), render.WithWindowChrome(true)}},
		"look-framed.png": {Render: look()},
		"look-gradient.png": {Render: append(look(), render.WithMarginGradient(
			32, 135, lipgloss.Color("#0b150f"), lipgloss.Color("#2d6a4f"), lipgloss.Color("#95d5b2"),
		))},
		"look-scale.png": {Render: []render.Option{render.WithWindowChrome(true), render.WithScale(1)}},

		// One effect each.
		"effect-keys.webp":   {Effects: []reel.Effect{effects.Keys(time.Second)}},
		"effect-toasts.webp": {Effects: []reel.Effect{effects.Toasts()}},
		"effect-mouse.webp":  {Effects: []reel.Effect{effects.Mouse()}},
		"effect-cards.webp": {Effects: []reel.Effect{
			effects.Intro(1200*time.Millisecond, effects.Line{Text: "The Tea Shop"}, effects.Line{Text: "a reel recording"}),
			effects.Outro(1200*time.Millisecond, effects.Line{Text: "Thanks for watching"}),
		}},
		"effect-pause.webp": {Effects: []reel.Effect{
			effects.Pause(effects.PauseAtMarks(900*time.Millisecond, "filter:typed"), effects.PauseAtEnd(time.Second)),
		}},
		"effect-highlight.webp": {Effects: []reel.Effect{
			effects.Highlight(row, effects.Between("brew:start", "brew:done"), effects.DimOthers(0.6), effects.Ease(200*time.Millisecond)),
		}},
		"effect-zoom.webp": {Effects: []reel.Effect{
			effects.Zoom(effects.Cells(image.Rect(1, 10, 38, 14)), effects.Between("brew:start", "brew:done"), effects.Ease(250*time.Millisecond)),
		}},
		"effect-hide.webp": {Effects: []reel.Effect{
			effects.Hide(effects.HideBetween("filter:start", "filter:done")),
		}},
		"effect-redact-block.webp":   {Effects: []reel.Effect{effects.Redact(secret, effects.Block())}},
		"effect-redact-blur.webp":    {Effects: []reel.Effect{effects.Redact(secret, effects.Blur())}},
		"effect-redact-replace.webp": {Effects: []reel.Effect{effects.Redact(secret, effects.Replace('•'))}},

		// One output of each format, to compare their sizes.
		"format.gif":  {},
		"format.webp": {},
		"format.mp4":  {},
		"format.webm": {},
		"format.png":  {},
		"format.snap": {},
		"format.json": {},
		"format.cast": {},

		// Stills at marks, and a part of the recording.
		"mark-{mark}.png?match=brew:*": {},
		"between.webp":                 {},
	}

	ctx := context.Background()
	for out, p := range gallery {
		t.Run(out, func(t *testing.T) {
			if p.Render == nil {
				p.Render = look()
			}
			p.Outputs = reel.MustFiles(filepath.Join(demoDir, out))
			from := tl
			if out == "between.webp" {
				var err error
				if from, err = tl.Between("brew:start", "brew:done"); err != nil {
					t.Fatal(err)
				}
			}
			if err := reel.Export(ctx, from, p); err != nil {
				t.Fatal(err)
			}
		})
	}

	// The sizes the formats page lists.
	sizes := map[string]int64{}
	for _, ext := range []string{"gif", "webp", "mp4", "webm", "png", "snap", "json", "cast"} {
		fi, err := os.Stat(filepath.Join(demoDir, "format."+ext))
		if err != nil {
			t.Fatal(err)
		}
		sizes[ext] = fi.Size()
	}
	b, err := json.MarshalIndent(sizes, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "format-sizes.json"), append(b, '\n'), 0o644); err != nil { //nolint:gosec // Part of the site.
		t.Fatal(err)
	}
}

// TestIndicators draws what a renderer shows of the terminal's own state, by
// writing the sequences a program would write.
func TestIndicators(t *testing.T) {
	if !exporting() {
		t.Skip("exporting is off")
	}
	ctx := context.Background()
	s, err := reel.Open(ctx, 72, 10, reel.WithTitle("shell"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	write := func(text string) {
		t.Helper()
		if _, err := s.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}

	// A shell's history: two commands, the second of which fails, reported
	// with OSC 133, enough to scroll, and a link.
	for range 12 {
		write("\x1b]133;A\x07$ \x1b]133;B\x07ls\r\n\x1b]133;C\x07main.go  main_test.go\r\n\x1b]133;D;0\x07")
	}
	write("\x1b]133;A\x07$ \x1b]133;B\x07go test\r\n\x1b]133;C\x07--- FAIL: TestShop\r\n\x1b]133;D;1\x07")
	write("See " + ansi.SetHyperlink("https://getsteep.dev") + "getsteep.dev" + ansi.ResetHyperlink() + " for help\r\n")
	write("\x1b]9;4;1;60\x07")
	write(ansi.SetProgramStatus(ansi.ProgramStatus{ID: "root", Title: "deploy", Message: "testing", State: ansi.ProgramStateWorking}))

	for out, opts := range map[string][]render.Option{
		"indicator-none.png":         nil,
		"indicator-scrollbar.png":    {render.WithScrollbar(true)},
		"indicator-prompt-marks.png": {render.WithScrollbar(true), render.WithPromptMarks(true)},
		"indicator-links.png":        {render.WithLinkUnderline(true)},
	} {
		err := reel.Export(ctx, s.Timeline(), reel.Profile{
			Outputs: reel.MustFiles(filepath.Join(demoDir, out)),
			Render:  append(look(), append(opts, render.WithProgressBar(out != "indicator-none.png"), render.WithProgramStatus(out != "indicator-none.png"))...),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
