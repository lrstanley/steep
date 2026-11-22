// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package native

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"

	"getsteep.dev/steep/reel/sink"
)

// Driver returns the pure Go [sink.Driver]. It encodes [sink.FormatWebP]
// only: Check reports [errors.ErrUnsupported] for MP4 and WebM, so another
// driver, such as [sink.FFmpeg], can encode them.
func Driver() sink.Driver { return driver{} }

type driver struct{}

func (driver) Check(format sink.Format, _ sink.VideoConfig) error {
	if format != sink.FormatWebP {
		return fmt.Errorf("sink/native: encodes webp only, not %s, which needs ffmpeg: %w", format, errors.ErrUnsupported)
	}
	return nil
}

// Start begins a WebP encode. It ignores ctx, as it encodes each frame in
// WriteFrame, between which the [sink.VideoEncoder] checks it.
func (d driver) Start(_ context.Context, out *os.File, format sink.Format, size image.Point, cfg sink.VideoConfig) (sink.Job, error) {
	if err := d.Check(format, cfg); err != nil {
		return nil, err
	}
	return startWebP(out, size, cfg)
}
