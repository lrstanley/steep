// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"getsteep.dev/steep/reel"
)

// LimitFlags raise or lower the [reel.Limits] of the recordings a command
// writes or the files it reads. They are flags and environment variables
// only, never tape settings, so a tape cannot raise its own.
type LimitFlags struct {
	MaxPlayback string `name:"max-playback" env:"REEL_MAX_PLAYBACK" placeholder:"DUR|none" help:"Longest a recording may play, pauses and cards included (default 10m)."`
	MaxCells    string `name:"max-cells" env:"REEL_MAX_CELLS" placeholder:"N|COLSxROWS|none" help:"Largest terminal, in cells (default 250000)."`
	MaxPixels   string `name:"max-pixels" env:"REEL_MAX_PIXELS" placeholder:"N|WxH|none" help:"Largest frame, in pixels (default 4096x4096)."`
}

// limits returns the limits the flags set; unset ones are zero, the
// default.
func (f LimitFlags) limits() (reel.Limits, error) {
	var l reel.Limits
	var err error
	if l.Playback, err = parseLimitDuration(f.MaxPlayback); err != nil {
		return l, fmt.Errorf("--max-playback: %w", err)
	}
	if l.Cells, err = parseLimitCount(f.MaxCells); err != nil {
		return l, fmt.Errorf("--max-cells: %w", err)
	}
	if l.Pixels, err = parseLimitCount(f.MaxPixels); err != nil {
		return l, fmt.Errorf("--max-pixels: %w", err)
	}
	return l, nil
}

// parseLimitDuration reads a duration limit: empty for the default, none
// for no limit, or a duration above zero.
func parseLimitDuration(s string) (time.Duration, error) {
	switch s {
	case "":
		return 0, nil
	case "none":
		return -1, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration or none", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%v is not above zero", d)
	}
	return d, nil
}

// parseLimitCount reads a count limit: empty for the default, none for no
// limit, a count above zero, or the product of a size such as 1000x250.
func parseLimitCount(s string) (int, error) {
	switch s {
	case "":
		return 0, nil
	case "none":
		return -1, nil
	}
	bad := fmt.Errorf("%q is not a count, a size such as 1000x250, or none", s)
	w, h, sized := strings.Cut(s, "x")
	if !sized {
		h = "1"
	}
	x, xerr := strconv.Atoi(w)
	y, yerr := strconv.Atoi(h)
	switch {
	case xerr != nil || yerr != nil || x <= 0 || y <= 0:
		return 0, bad
	case x > math.MaxInt/y:
		return 0, fmt.Errorf("%q is too large", s)
	}
	return x * y, nil
}

// limitFlag names the flag that raises the limit err is over, if any.
func limitFlag(err error) (string, bool) {
	var le *reel.LimitError
	if !errors.As(err, &le) {
		return "", false
	}
	switch le.Limit {
	case reel.LimitPlayback:
		return "--max-playback", true
	case reel.LimitCells:
		return "--max-cells", true
	case reel.LimitPixels:
		return "--max-pixels", true
	}
	return "", false
}
