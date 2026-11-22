// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"errors"
	"fmt"
	"image"
	"math"
	"time"
)

// The defaults of [Limits]. They are well above what a demo or a test
// recording needs.
const (
	// DefaultPlayback is the default [Limits.Playback]: ten minutes.
	DefaultPlayback = 10 * time.Minute
	// DefaultCells is the default [Limits.Cells]: a terminal of 1000
	// columns by 250 rows, several times what a 4K screen shows at a small
	// font.
	DefaultCells = 250_000
	// DefaultPixels is the default [Limits.Pixels]: a frame of 4096 by
	// 4096, above a 5K screen. One such frame takes 64 MiB.
	DefaultPixels = 4096 * 4096
)

// The names of the limits in a [LimitError].
const (
	LimitPlayback = "playback"
	LimitCells    = "cells"
	LimitPixels   = "pixels"
)

// Limits bound what an export or recording may use, so a timeline from
// someone else, such as an asciicast file or a tape, cannot make it play
// for hours or allocate gigabytes. Going over one is a [LimitError].
//
// A zero field is its default, and a negative one sets no limit, for
// timelines you trust.
type Limits struct {
	// Playback bounds how long the output plays: the timeline scaled by
	// [Profile.Speed], footage effects such as Hide cut included, plus the
	// frames effects insert, such as pauses and cards. No one frame may be longer either, so an effect never works
	// through more than this of playback at once. Defaults to
	// [DefaultPlayback].
	Playback time.Duration
	// Cells bounds the size of the terminal, columns times rows, from the
	// [Header] and each [Resize]. Defaults to [DefaultCells].
	Cells int
	// Pixels bounds the size of one frame, window chrome, padding, and
	// margin included, before it is allocated. Defaults to [DefaultPixels].
	Pixels int
}

// DefaultLimits returns the limits a zero [Limits] stands for.
func DefaultLimits() Limits {
	return Limits{Playback: DefaultPlayback, Cells: DefaultCells, Pixels: DefaultPixels}
}

// merge returns l with every non-zero field of o replacing l's.
func (l Limits) merge(o Limits) Limits {
	if o.Playback != 0 {
		l.Playback = o.Playback
	}
	if o.Cells != 0 {
		l.Cells = o.Cells
	}
	if o.Pixels != 0 {
		l.Pixels = o.Pixels
	}
	return l
}

// checkCells returns a [LimitError] when a cols by rows terminal is over
// l.Cells.
func (l Limits) checkCells(cols, rows int) error {
	l = DefaultLimits().merge(l)
	if l.Cells < 0 || cols <= 0 || rows <= 0 || cols <= l.Cells/rows {
		return nil
	}
	return &LimitError{Limit: LimitCells, Value: mulSat(cols, rows), Max: int64(l.Cells)}
}

// checkPixels returns a [LimitError] when an image of r is over l.Pixels.
func (l Limits) checkPixels(r image.Rectangle) error {
	l = DefaultLimits().merge(l)
	w, h := r.Dx(), r.Dy()
	if l.Pixels < 0 || w <= 0 || h <= 0 || w <= l.Pixels/h {
		return nil
	}
	return &LimitError{Limit: LimitPixels, Value: mulSat(w, h), Max: int64(l.Pixels)}
}

// checkPlayback returns a [LimitError] when playing for d nanoseconds, a
// float so a tiny speed cannot overflow it, is over l.Playback.
func (l Limits) checkPlayback(d float64) error {
	l = DefaultLimits().merge(l)
	if l.Playback < 0 || d <= float64(l.Playback) {
		return nil
	}
	v := int64(math.MaxInt64)
	if d < float64(math.MaxInt64) {
		v = int64(d)
	}
	return &LimitError{Limit: LimitPlayback, Value: v, Max: int64(l.Playback)}
}

// CheckPlayback returns a [LimitError] when playing d at speed, which
// divides it, is over l.Playback, for a player of its own, such as one
// that plays a [Timeline] in a terminal.
func (l Limits) CheckPlayback(d time.Duration, speed float64) error {
	return l.checkPlayback(float64(d) / speed)
}

// mulSat returns a*b for positive a and b, or [math.MaxInt64] if that
// overflows.
func mulSat(a, b int) int64 {
	if int64(a) > math.MaxInt64/int64(b) {
		return math.MaxInt64
	}
	return int64(a) * int64(b)
}

// ErrLimit is wrapped by every [LimitError].
var ErrLimit = errors.New("reel: over a limit")

// LimitError is returned when an export or recording goes over one of its
// [Limits]. It wraps [ErrLimit].
type LimitError struct {
	// Limit names the limit: [LimitPlayback], [LimitCells], or
	// [LimitPixels].
	Limit string
	// Value is what was asked for, and Max the limit: nanoseconds for
	// playback, and counts for the others. A Value too large for an int64
	// is [math.MaxInt64].
	Value, Max int64
}

func (e *LimitError) Error() string {
	switch e.Limit {
	case LimitPlayback:
		return fmt.Sprintf("reel: playback of %v is over the limit of %v", time.Duration(e.Value), time.Duration(e.Max))
	case LimitCells:
		return fmt.Sprintf("reel: a terminal of %d cells is over the limit of %d", e.Value, e.Max)
	case LimitPixels:
		return fmt.Sprintf("reel: a frame of %d pixels is over the limit of %d", e.Value, e.Max)
	}
	return fmt.Sprintf("reel: %s %d is over the limit of %d", e.Limit, e.Value, e.Max)
}

func (e *LimitError) Unwrap() error { return ErrLimit }
