// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/internal/testutil"
	"getsteep.dev/steep/reel/region"
)

// exportErr exports tl with effects to a GIF and returns the error.
func exportErr(t *testing.T, tl *reel.Timeline, fx ...reel.Effect) error {
	t.Helper()
	return reel.Export(t.Context(), tl, reel.Profile{
		Outputs:    reel.MustFiles(filepath.Join(t.TempDir(), "out.gif")),
		Rasterizer: testutil.Rasterizer{},
		Effects:    fx,
	})
}

// TestEffectSizesBounded checks that pixel sizes above MaxSizePX, from an
// annotation or from Go, are an error rather than an allocation that
// panics or takes gigabytes.
func TestEffectSizesBounded(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 10, 2, "", output(0, "x"), input(100, "a"), output(200, "a"))
	for name, data := range map[string]string{
		"pointer":           `{"effect":"mouse","pointer":"dot","pointer_size_px":10000000000}`,
		"badge":             `{"effect":"pause","points":[{"at":"end","duration_ms":100}],"badge_size_px":1025}`,
		"highlight outline": `{"effect":"highlight","region":{"type":"text","text":"x"},"outline":{"width_px":1025}}`,
		"highlight padding": `{"effect":"highlight","region":{"type":"text","text":"x"},"padding_px":1025}`,
	} {
		e, err := FromAnnotation(reel.Annotation{Kind: reel.EffectKind, Data: jsontext.Value(data)})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err = exportErr(t, tl, e); err == nil || !strings.Contains(err.Error(), "to 1024") {
			t.Errorf("%s: Export = %v, want an error naming the limit", name, err)
		}
	}
	for name, e := range map[string]reel.Effect{
		"pointer":   Mouse(WithPointerSize(MaxSizePX + 1)),
		"badge":     Pause(PauseAtEnd(time.Second), WithBadgeSize(MaxSizePX+1)),
		"outline":   Highlight(region.Text("x"), Outline(nil, MaxSizePX+1)),
		"padding":   Highlight(region.Text("x"), Padding(MaxSizePX+1)),
		"max sizes": Highlight(region.Text("x"), Outline(nil, MaxSizePX), Padding(MaxSizePX)),
	} {
		err := exportErr(t, tl, e)
		if want := name != "max sizes"; (err != nil) != want {
			t.Errorf("%s: Export = %v, want an error: %t", name, err, want)
		}
	}
}

// TestLongEffectsHitPlaybackLimit checks that a long pause or card read
// from someone else's recording fails the export at the playback limit,
// rather than playing for hours.
func TestLongEffectsHitPlaybackLimit(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 10, 2, "", output(0, "x"))
	for name, data := range map[string]string{
		"pause": `{"effect":"pause","points":[{"at":"end","duration_ms":3600000}]}`,
		"intro": `{"effect":"intro","duration_ms":3600000,"lines":[{"text":"hi"}]}`,
	} {
		e, err := FromAnnotation(reel.Annotation{Kind: reel.EffectKind, Data: jsontext.Value(data)})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var le *reel.LimitError
		if err = exportErr(t, tl, e); !errors.As(err, &le) || le.Limit != reel.LimitPlayback {
			t.Errorf("%s: Export = %v, want a playback LimitError", name, err)
		}
	}
}

// TestEffectsCancelPromptly checks that canceling an export with effects
// that hold and animate long frames stops it within a few seconds.
func TestEffectsCancelPromptly(t *testing.T) {
	t.Parallel()
	var events []event
	for i := range 3000 {
		events = append(events, output(i*100, fmt.Sprintf("\r\nline %d", i)))
	}
	tl := buildTimeline(t, 80, 24, "", events...)
	fx := []reel.Effect{
		Intro(2*time.Minute, Line{Text: "hi"}),
		Pause(PauseAtEnd(4 * time.Minute)),
		Zoom(region.Row(0)),
		Highlight(region.Text("line")),
		Mouse(),
	}
	for _, at := range []time.Duration{50 * time.Millisecond, 500 * time.Millisecond, 2 * time.Second} {
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(at, cancel)
		start := time.Now()
		err := reel.Export(ctx, tl, reel.Profile{Outputs: reel.MustFiles(filepath.Join(t.TempDir(), "out.gif")), Effects: fx})
		took := time.Since(start)
		cancel()
		if !errors.Is(err, context.Canceled) || took > at+5*time.Second {
			t.Errorf("canceled at %v: Export = %v after %v, want context.Canceled within seconds", at, err, took)
		}
	}
}
