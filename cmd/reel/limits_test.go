// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"getsteep.dev/steep/reel"
)

// TestLimitFlags checks that the limit flags and their environment
// variables parse into a profile's limits.
func TestLimitFlags(t *testing.T) {
	t.Setenv("REEL_MAX_CELLS", "1000x500")
	f, err := parse(t, "export", "a.cast", "-o", "a.gif", "--max-playback", "1h", "--max-pixels", "none")
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.Export.profile()
	if err != nil {
		t.Fatal(err)
	}
	if want := (reel.Limits{Playback: time.Hour, Cells: 500_000, Pixels: -1}); p.Limits != want {
		t.Errorf("limits = %+v, want %+v", p.Limits, want)
	}

	for _, tc := range []struct {
		flags LimitFlags
		want  reel.Limits
		err   string
	}{
		{flags: LimitFlags{}, want: reel.Limits{}},
		{flags: LimitFlags{MaxPlayback: "none", MaxCells: "none"}, want: reel.Limits{Playback: -1, Cells: -1}},
		{flags: LimitFlags{MaxCells: "250000", MaxPixels: "8192x8192"}, want: reel.Limits{Cells: 250_000, Pixels: 8192 * 8192}},
		{flags: LimitFlags{MaxPlayback: "0s"}, err: "--max-playback: 0s is not above zero"},
		{flags: LimitFlags{MaxPlayback: "soon"}, err: `--max-playback: "soon" is not a duration or none`},
		{flags: LimitFlags{MaxCells: "0"}, err: "--max-cells:"},
		{flags: LimitFlags{MaxCells: "-5"}, err: "--max-cells:"},
		{flags: LimitFlags{MaxPixels: "10x"}, err: "--max-pixels:"},
		{flags: LimitFlags{MaxPixels: "1x2x3"}, err: "--max-pixels:"},
		{flags: LimitFlags{MaxPixels: "9223372036854775807x2"}, err: "too large"},
	} {
		got, lerr := tc.flags.limits()
		switch {
		case tc.err != "" && (lerr == nil || !strings.Contains(lerr.Error(), tc.err)):
			t.Errorf("%+v: error = %v, want %q", tc.flags, lerr, tc.err)
		case tc.err == "" && (lerr != nil || got != tc.want):
			t.Errorf("%+v = %+v, %v, want %+v", tc.flags, got, lerr, tc.want)
		}
	}
}

// TestLimitHint checks that an export over a limit fails with a hint
// naming the flag that raises it.
func TestLimitHint(t *testing.T) {
	t.Parallel()
	cast := filepath.Join(t.TempDir(), "big.cast")
	if err := os.WriteFile(cast, []byte(`{"version":2,"width":4096,"height":4096}`+"\n"+`[0,"o","x"]`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := ExportCmd{Cast: cast, Outputs: []string{filepath.Join(t.TempDir(), "out.gif")}}
	err := cmd.Run(context.Background())
	if hint := hintFor(err); !strings.Contains(hint, "--max-cells") {
		t.Errorf("Run = %v, hint %q, want one naming --max-cells", err, hint)
	}
}

// TestPlayPlaybackLimit checks that reel play stops at the playback limit
// at once, for a long gap or a tiny speed, rather than waiting it out.
func TestPlayPlaybackLimit(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		src   string
		speed float64
	}{
		"gap":   {src: `[31536000,"o","b"]`, speed: 1},
		"speed": {src: `[1,"o","b"]`, speed: 1e-300},
	} {
		tl, err := reel.ReadTimeline(strings.NewReader(`{"version":2,"width":10,"height":2}` + "\n" + `[0,"o","a"]` + "\n" + tc.src + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		none := func() error { return nil }
		apply := func(int, reel.Event) error { return nil }
		start := time.Now()
		err = replay(t.Context(), tl, tc.speed, 0, reel.Limits{}, apply, none)
		if !errors.Is(err, reel.ErrLimit) || time.Since(start) > 5*time.Second {
			t.Errorf("%s: replay = %v after %v, want a limit error at once", name, err, time.Since(start))
		}
	}
}
