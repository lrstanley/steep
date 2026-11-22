// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"time"
)

// playCount converts a [WithLoopCount] value, which counts repeats, into a
// count of plays, as WebP stores it (0 is forever).
func playCount(n int) int {
	switch n {
	case LoopForever:
		return 0
	case LoopOnce:
		return 1
	}
	return min(n+1, maxGIFLoopCount)
}

// CheckVideo reports whether [CreateVideo] can encode format with opts: that
// one of its drivers ([WithDrivers]) can, such as [FFmpeg] being installed
// with an encoder for it.
func CheckVideo(format Format, opts ...Option) error {
	cfg, err := videoOptions(opts)
	if err != nil {
		return err
	}
	_, err = pickDriver(cfg.videoDrivers(), format, cfg.videoConfig())
	return err
}

// VideoEncoder is a [Sink] that encodes frames as they arrive through a
// [Driver], [FFmpeg] by default. Each frame keeps its own delay, so a hold
// stays a single frame. The canvas is the first frame's size: later frames of
// another size are anchored at the top left, and cropped or padded with the
// background.
type VideoEncoder struct {
	opts   options
	cfg    VideoConfig
	format Format
	driver Driver
	file   *pendingFile

	job    Job
	bounds image.Rectangle
	// canvas holds frames that cannot be passed on as is.
	canvas *image.NRGBA
	frames int
	// err is the first encode error; once set, frames are rejected.
	err  error
	done bool
}

var _ Sink = (*VideoEncoder)(nil)

// CreateVideo returns an encoder that writes frames to path as format, via a
// temporary file beside path that [VideoEncoder.Close] renames into place.
// The driver is picked here, so a missing ffmpeg (wrapping
// [ErrFFmpegNotFound]) or encoder ([MissingEncoderError]) is reported before
// any frames are recorded.
//
// It reads [WithQuality], [WithFullChroma], [WithBackground], [WithDrivers],
// and, for WebP, [WithLoopCount]. Other options are ignored.
func CreateVideo(path string, format Format, opts ...Option) (*VideoEncoder, error) {
	return newVideo(func() (*pendingFile, error) { return createPending(path) }, format, opts)
}

// NewVideo is [CreateVideo] writing to w instead of a file. The driver
// encodes into a temporary file, as MP4 and WebP need a seekable output,
// which [VideoEncoder.Close] copies to w; nothing is written to w before
// then.
func NewVideo(w io.Writer, format Format, opts ...Option) (*VideoEncoder, error) {
	if w == nil {
		return nil, errors.New("sink: nil writer")
	}
	return newVideo(func() (*pendingFile, error) { return createPendingWriter(w) }, format, opts)
}

func newVideo(create func() (*pendingFile, error), format Format, opts []Option) (*VideoEncoder, error) {
	o, err := videoOptions(opts)
	if err != nil {
		return nil, err
	}
	cfg := o.videoConfig()
	driver, err := pickDriver(o.videoDrivers(), format, cfg)
	if err != nil {
		return nil, err
	}
	file, err := create()
	if err != nil {
		return nil, fmt.Errorf("sink: creating %s output: %w", format, err)
	}
	return &VideoEncoder{opts: o, cfg: cfg, format: format, driver: driver, file: file}, nil
}

// WriteFrame encodes f for f.Delay, truncated to whole milliseconds (at least
// one). [Frame.Isolated] is ignored.
func (s *VideoEncoder) WriteFrame(f Frame) error {
	img := f.Image
	switch {
	case s.done:
		return errors.New("sink: write to closed video encoder")
	case s.err != nil:
		return s.err
	case img == nil:
		return errNilFrame
	case img.Bounds().Empty():
		return errEmptyFrame
	}
	if err := s.opts.ctx.Err(); err != nil {
		s.err = err
		return err
	}
	if s.job == nil {
		s.bounds = image.Rectangle{Max: img.Bounds().Size()}
		job, err := s.driver.Start(s.opts.ctx, s.file.f, s.format, s.bounds.Size(), s.cfg)
		if err != nil {
			s.err = err
			return err
		}
		s.job = job
	}
	delay := max(f.Delay.Truncate(time.Millisecond), time.Millisecond)
	if err := s.job.WriteFrame(s.streamable(img), delay); err != nil {
		// A write fails once the context kills the encode; say so.
		s.err = cmp.Or(s.opts.ctx.Err(), err)
		return s.err
	}
	s.frames++
	return nil
}

// streamable returns img as tightly packed NRGBA on the canvas, with its
// bounds at the origin, composited onto the background when the format has
// no alpha or [WithBackground] is set. Such frames, the common case, are
// returned as is.
func (s *VideoEncoder) streamable(img image.Image) *image.NRGBA {
	composite := !formatHasAlpha(s.format) || s.opts.background != nil
	if src, ok := img.(*image.NRGBA); ok && src.Rect == s.bounds && src.Stride == src.Rect.Dx()*4 && (!composite || src.Opaque()) {
		return src
	}
	src := asNRGBA(img)
	if s.canvas == nil {
		s.canvas = image.NewNRGBA(s.bounds)
	}
	dst := s.canvas
	w := min(dst.Rect.Dx(), src.Rect.Dx())
	h := min(dst.Rect.Dy(), src.Rect.Dy())
	if w < dst.Rect.Dx() || h < dst.Rect.Dy() {
		fill := color.NRGBA{}
		if composite {
			fill = s.cfg.Background
		}
		fillNRGBA(dst, fill)
	}
	for y := range h {
		si := src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y+y)
		srcRow := src.Pix[si : si+w*4]
		dstRow := dst.Pix[y*dst.Stride : y*dst.Stride+w*4]
		if !composite {
			copy(dstRow, srcRow)
			continue
		}
		compositeRow(dstRow, srcRow, s.cfg.Background)
	}
	return dst
}

// fillNRGBA sets every pixel of the tightly packed img to c. draw.Draw has no
// fast path for an NRGBA destination.
func fillNRGBA(img *image.NRGBA, c color.NRGBA) {
	row := img.Pix[:img.Rect.Dx()*4]
	for i := 0; i < len(row); i += 4 {
		row[i], row[i+1], row[i+2], row[i+3] = c.R, c.G, c.B, c.A
	}
	for y := 1; y < img.Rect.Dy(); y++ {
		copy(img.Pix[y*img.Stride:], row)
	}
}

// compositeRow blends NRGBA src over the opaque bg into dst.
func compositeRow(dst, src []byte, bg color.NRGBA) {
	dst = dst[:len(src)]
	for i := 0; i+3 < len(src); i += 4 {
		a := uint32(src[i+3])
		switch a {
		case 0xff:
			j := i + 4
			for j+3 < len(src) && src[j+3] == 0xff {
				j += 4
			}
			copy(dst[i:j], src[i:j])
			i = j - 4
			continue
		case 0:
			dst[i], dst[i+1], dst[i+2] = bg.R, bg.G, bg.B
		default:
			inv := 0xff - a
			dst[i] = uint8((uint32(src[i])*a + uint32(bg.R)*inv + 0x7f) / 0xff)     //nolint:gosec // <= 0xff.
			dst[i+1] = uint8((uint32(src[i+1])*a + uint32(bg.G)*inv + 0x7f) / 0xff) //nolint:gosec // <= 0xff.
			dst[i+2] = uint8((uint32(src[i+2])*a + uint32(bg.B)*inv + 0x7f) / 0xff) //nolint:gosec // <= 0xff.
		}
		dst[i+3] = 0xff
	}
}

// Close finishes the encode and moves it into place, or copies it to the
// writer. With no frames written, it writes nothing. If the encode failed, the
// destination is left alone. Later calls return the same result.
func (s *VideoEncoder) Close() error {
	if s.done {
		return s.err
	}
	s.done = true
	if s.job != nil {
		if s.err == nil {
			s.err = s.opts.ctx.Err()
		}
		if s.err == nil {
			s.err = s.job.Close()
			if cerr := s.opts.ctx.Err(); cerr != nil && s.err != nil {
				// The job failed because the context killed it.
				s.err = cerr
			}
		} else {
			s.job.Abort()
		}
	}
	if s.err != nil || s.frames == 0 {
		_ = s.file.abort()
		return s.err
	}
	if err := s.file.commit(); err != nil {
		s.err = fmt.Errorf("sink: moving %s into place: %w", s.format, err)
	}
	return s.err
}

// Discard stops the encode and removes its output without touching the
// destination. It returns nil after [VideoEncoder.Close] has run.
func (s *VideoEncoder) Discard() error {
	if s.done {
		return nil
	}
	s.done = true
	if s.job != nil {
		s.job.Abort()
	}
	return s.file.abort()
}
