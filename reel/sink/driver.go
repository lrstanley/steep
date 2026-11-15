// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"time"
)

// Driver encodes the video formats ([FormatMP4], [FormatWebM], [FormatWebP])
// for a [VideoEncoder], which hands it frames ready to encode and owns the
// output file. [FFmpeg] is the default; [WithDrivers] picks others, such as
// a pure Go one from another module.
type Driver interface {
	// Check reports whether the driver can encode format with cfg. An error
	// wrapping [errors.ErrUnsupported] means it never encodes format, so the
	// next driver is tried quietly. Any other error, such as a missing
	// binary, should say how to fix it.
	Check(format Format, cfg VideoConfig) error
	// Start begins encoding format at size into out, a new, empty temporary
	// file, which Start may close and reopen by name. It is called on the
	// first frame, after Check reported nil.
	Start(out *os.File, format Format, size image.Point, cfg VideoConfig) (Job, error)
}

// Job is one encode a [Driver] runs. Its methods are called from one
// goroutine.
type Job interface {
	// WriteFrame adds the next frame, which plays for delay: whole
	// milliseconds, at least one. img is tightly packed, its bounds run from
	// the origin to the size given to [Driver.Start], and it is only valid
	// until WriteFrame returns. It is opaque, composited onto
	// [VideoConfig.Background], unless the format keeps alpha ([FormatWebP])
	// and [WithBackground] is unset.
	WriteFrame(img *image.NRGBA, delay time.Duration) error
	// Close finishes the output file, after at least one frame was written.
	// On error, the job cleans up after itself.
	Close() error
	// Abort stops the encode after WriteFrame failed or instead of Close.
	// The output file is removed after.
	Abort()
}

// VideoConfig is what a [Driver] reads of a [VideoEncoder]'s options.
type VideoConfig struct {
	// Quality is [WithQuality]'s value, or -1 when unset, for the driver's
	// own default.
	Quality int
	// FullChroma is [WithFullChroma]'s value.
	FullChroma bool
	// Background is [WithBackground]'s color, made opaque, or black when
	// unset. Frames are already composited onto it where needed; drivers
	// use it for padding.
	Background color.NRGBA
	// Plays is how many times an animation plays, from [WithLoopCount]: 0
	// is forever.
	Plays int
}

// WithDrivers sets the drivers a [VideoEncoder] can use, in order of
// preference, replacing the default of [FFmpeg] on PATH. The first whose
// [Driver.Check] passes encodes the output. PNG and GIF ignore it.
func WithDrivers(drivers ...Driver) Option {
	return func(cfg *options) {
		cfg.drivers = drivers
	}
}

// formatHasAlpha reports whether format keeps transparency.
func formatHasAlpha(format Format) bool { return format == FormatWebP }

func isVideoFormat(format Format) bool {
	switch format { //nolint:exhaustive // GIF and PNG are not video.
	case FormatMP4, FormatWebM, FormatWebP:
		return true
	}
	return false
}

// pickDriver returns the first of drivers that can encode format. When none
// can, the error is the only real failure if there is one, so a lone
// driver's error reads as it wrote it, and otherwise all of them joined.
func pickDriver(drivers []Driver, format Format, cfg VideoConfig) (Driver, error) {
	if !isVideoFormat(format) {
		return nil, fmt.Errorf("sink: unknown video format %q", format)
	}
	var errs, unsupported []error
	for _, d := range drivers {
		if d == nil {
			continue
		}
		err := d.Check(format, cfg)
		switch {
		case err == nil:
			return d, nil
		case errors.Is(err, errors.ErrUnsupported):
			unsupported = append(unsupported, err)
		default:
			errs = append(errs, err)
		}
	}
	switch len(errs) {
	case 0:
		if len(unsupported) == 0 {
			unsupported = append(unsupported, errors.ErrUnsupported)
		}
		return nil, fmt.Errorf("sink: no driver encodes %s: %w", format, errors.Join(unsupported...))
	case 1:
		return nil, errs[0]
	}
	return nil, fmt.Errorf("sink: no driver can encode %s: %w", format, errors.Join(errs...))
}

// videoConfig is the [VideoConfig] of o.
func (o options) videoConfig() VideoConfig {
	cfg := VideoConfig{
		Quality:    -1,
		FullChroma: o.fullChroma,
		Background: color.NRGBA{A: 0xff},
		Plays:      playCount(o.loopCount),
	}
	if o.qualitySet {
		cfg.Quality = o.quality
	}
	if o.background != nil {
		cfg.Background = opaqueNRGBA(o.background)
	}
	return cfg
}

// videoDrivers returns the drivers o picks from.
func (o options) videoDrivers() []Driver {
	if o.drivers != nil {
		return o.drivers
	}
	return defaultDrivers
}

var defaultDrivers = []Driver{FFmpeg("")}
