// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package native

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"time"

	"github.com/gen2brain/webp"

	"getsteep.dev/steep/reel/sink"
)

const (
	// webpMaxDimension is the largest canvas side VP8X stores (24 bits).
	webpMaxDimension = 1 << 24
	// webpMaxDuration is the longest frame duration ANMF stores: 24 bits of
	// milliseconds.
	webpMaxDuration = 1<<24 - 1
	// webpMaxRIFF is the largest RIFF payload, as its size is 32 bits.
	webpMaxRIFF = 1<<32 - 2

	vp8xFlagAlpha     = 0x10
	vp8xFlagAnimation = 0x02
	// anmfNoBlend makes a frame replace the canvas pixels under it. Without
	// it, the frame is alpha-blended over the canvas.
	anmfNoBlend = 0x02
)

// webpJob streams an animated WebP: a VP8X and ANIM header, then one ANMF
// chunk per frame, holding the rectangle that changed. The last frame is
// held back until the next one, which may only extend its duration. The
// RIFF size and the alpha flag are patched on Close.
type webpJob struct {
	f    *os.File
	w    *bufio.Writer
	size image.Point
	opts webp.Options
	// prev is the canvas as the frames written so far leave it.
	prev *image.NRGBA
	// pending is the last frame, not yet written, as ANMF payload without
	// its duration.
	pending      []byte
	pendingDelay time.Duration
	// written counts the bytes after the RIFF header.
	written uint64
	alpha   bool
	buf     bytes.Buffer
	// tile backs the tightly packed copy of a frame's rectangle that is
	// encoded, sized for the whole canvas.
	tile []byte
}

func startWebP(out *os.File, size image.Point, cfg sink.VideoConfig) (*webpJob, error) {
	if size.X > webpMaxDimension || size.Y > webpMaxDimension {
		return nil, fmt.Errorf("sink/native: %dx%d is larger than WebP's %d pixel limit", size.X, size.Y, webpMaxDimension)
	}
	opts := webp.Options{Lossless: true, Method: webp.DefaultMethod}
	if cfg.Quality >= 0 && cfg.Quality < sink.MaxQuality {
		// Quality 0 would select the encoder's default.
		opts = webp.Options{Quality: max(cfg.Quality, 1), Method: webp.DefaultMethod}
	}
	j := &webpJob{
		f:    out,
		w:    bufio.NewWriterSize(out, 64<<10),
		size: size,
		opts: opts,
		prev: image.NewNRGBA(image.Rectangle{Max: size}),
	}
	var hdr [12 + 8 + 10 + 8 + 6]byte
	copy(hdr[0:], "RIFF")
	copy(hdr[8:], "WEBP")
	copy(hdr[12:], "VP8X")
	binary.LittleEndian.PutUint32(hdr[16:], 10)
	hdr[20] = vp8xFlagAnimation
	putUint24(hdr[24:], size.X-1)
	putUint24(hdr[27:], size.Y-1)
	copy(hdr[30:], "ANIM")
	binary.LittleEndian.PutUint32(hdr[34:], 6)
	// Background color (left 0: transparent), then the loop count.
	binary.LittleEndian.PutUint16(hdr[42:], uint16(cfg.Plays)) //nolint:gosec // <= 65535.
	if _, err := j.w.Write(hdr[:]); err != nil {
		return nil, err
	}
	j.written = uint64(len(hdr) - 8)
	return j, nil
}

func (j *webpJob) WriteFrame(img *image.NRGBA, delay time.Duration) error {
	first := j.pending == nil
	rect := image.Rectangle{Max: j.size}
	if !first {
		rect = changedRect(j.prev, img)
		if rect.Empty() {
			j.pendingDelay += delay
			return nil
		}
		// ANMF stores the offset in units of two pixels.
		rect.Min.X &^= 1
		rect.Min.Y &^= 1
	}
	if err := j.flush(); err != nil {
		return err
	}
	sub := img.SubImage(rect).(*image.NRGBA) //nolint:errcheck // SubImage of NRGBA is NRGBA.
	if !j.alpha && !sub.Opaque() {
		j.alpha = true
	}
	// A frame blended over the canvas only needs the pixels that changed,
	// and transparent ones elsewhere compress to almost nothing. That only
	// holds when every changed pixel is opaque.
	flags := byte(anmfNoBlend)
	src := sub
	if !first {
		// The encoder reads rows as tightly packed, which a sub-image's aren't.
		src = j.tileOf(rect)
		if j.deltaOf(sub, src) {
			flags = 0
			j.alpha = true
		} else {
			for y := range rect.Dy() {
				copy(src.Pix[y*src.Stride:(y+1)*src.Stride], sub.Pix[y*sub.Stride:])
			}
		}
	}
	j.buf.Reset()
	if err := webp.Encode(&j.buf, src, j.opts); err != nil {
		return fmt.Errorf("sink/native: encoding webp frame: %w", err)
	}
	chunks, err := imageChunks(j.buf.Bytes())
	if err != nil {
		return err
	}
	var anmf [16]byte
	putUint24(anmf[0:], rect.Min.X/2)
	putUint24(anmf[3:], rect.Min.Y/2)
	putUint24(anmf[6:], rect.Dx()-1)
	putUint24(anmf[9:], rect.Dy()-1)
	anmf[15] = flags
	j.pending = append(append(j.pending[:0], anmf[:]...), chunks...)
	j.pendingDelay = delay
	copyRect(j.prev, img, rect)
	return nil
}

// flush writes the pending frame with its duration.
func (j *webpJob) flush() error {
	if j.pending == nil {
		return nil
	}
	putUint24(j.pending[12:], int(min(max(j.pendingDelay/time.Millisecond, 1), webpMaxDuration)))
	var hdr [8]byte
	copy(hdr[:], "ANMF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(len(j.pending))) //nolint:gosec // bounded by the frame.
	if _, err := j.w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := j.w.Write(j.pending); err != nil {
		return err
	}
	n := uint64(len(j.pending))
	if n&1 == 1 {
		if err := j.w.WriteByte(0); err != nil {
			return err
		}
	}
	j.written += 8 + n + n&1
	if j.written > webpMaxRIFF {
		return errors.New("sink/native: webp is larger than 4GiB")
	}
	return nil
}

func (j *webpJob) Close() error {
	if err := j.flush(); err != nil {
		return errors.Join(err, j.f.Close())
	}
	if err := j.w.Flush(); err != nil {
		return errors.Join(err, j.f.Close())
	}
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(j.written)) //nolint:gosec // checked in flush.
	_, err := j.f.WriteAt(size[:], 4)
	if err == nil && j.alpha {
		_, err = j.f.WriteAt([]byte{vp8xFlagAnimation | vp8xFlagAlpha}, 20)
	}
	return errors.Join(err, j.f.Close())
}

func (j *webpJob) Abort() { _ = j.f.Close() }

// tileOf returns j.tile as a tightly packed image the size of r.
func (j *webpJob) tileOf(r image.Rectangle) *image.NRGBA {
	if j.tile == nil {
		j.tile = make([]byte, j.size.X*j.size.Y*4)
	}
	return &image.NRGBA{Pix: j.tile[:r.Dx()*r.Dy()*4], Stride: r.Dx() * 4, Rect: image.Rectangle{Max: r.Size()}}
}

// deltaOf sets dst, a tile the size of sub, to the pixels of sub that differ
// from j.prev, with the rest transparent, and reports whether every one that
// differs is opaque. When one isn't, dst is left partly written.
func (j *webpJob) deltaOf(sub, dst *image.NRGBA) bool {
	r := sub.Rect
	for y := range r.Dy() {
		i := j.prev.PixOffset(r.Min.X, r.Min.Y+y)
		cur := sub.Pix[y*sub.Stride : y*sub.Stride+r.Dx()*4]
		old := j.prev.Pix[i : i+r.Dx()*4]
		out := dst.Pix[y*dst.Stride : (y+1)*dst.Stride]
		for x := 0; x < len(cur); x += 4 {
			if cur[x] == old[x] && cur[x+1] == old[x+1] && cur[x+2] == old[x+2] && cur[x+3] == old[x+3] {
				out[x], out[x+1], out[x+2], out[x+3] = 0, 0, 0, 0
				continue
			}
			if cur[x+3] != 0xff {
				return false
			}
			copy(out[x:x+4], cur[x:x+4])
		}
	}
	return true
}

// imageChunks returns the chunks of the still WebP data that go in an ANMF
// frame (ALPH, VP8, and VP8L), headers included.
func imageChunks(data []byte) ([]byte, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, errors.New("sink/native: webp encoder wrote no RIFF header")
	}
	var out []byte
	for off := 12; off+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[off+4:]))
		end := min(off+8+size+size&1, len(data))
		switch string(data[off : off+4]) {
		case "ALPH", "VP8 ", "VP8L":
			out = append(out, data[off:end]...)
		}
		off = end
	}
	if out == nil {
		return nil, errors.New("sink/native: webp encoder wrote no image")
	}
	return out, nil
}

// changedRect returns the bounds of the pixels that differ between a and b,
// which are tightly packed and the same size.
func changedRect(a, b *image.NRGBA) image.Rectangle {
	w, h := a.Rect.Dx(), a.Rect.Dy()
	row := w * 4
	r := image.Rectangle{Min: image.Point{X: w, Y: h}}
	for y := range h {
		ra, rb := a.Pix[y*a.Stride:y*a.Stride+row], b.Pix[y*b.Stride:y*b.Stride+row]
		if bytes.Equal(ra, rb) {
			continue
		}
		left := 0
		for left < row && ra[left] == rb[left] {
			left++
		}
		right := row
		for right > left && ra[right-1] == rb[right-1] {
			right--
		}
		r.Min.X = min(r.Min.X, left/4)
		r.Max.X = max(r.Max.X, (right+3)/4)
		r.Min.Y = min(r.Min.Y, y)
		r.Max.Y = y + 1
	}
	if r.Max.Y == 0 {
		return image.Rectangle{}
	}
	return r
}

// copyRect copies r of src into dst, which are tightly packed and the same
// size.
func copyRect(dst, src *image.NRGBA, r image.Rectangle) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := dst.PixOffset(r.Min.X, y)
		copy(dst.Pix[i:i+r.Dx()*4], src.Pix[i:i+r.Dx()*4])
	}
}

func putUint24(b []byte, v int) {
	b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) //nolint:gosec // v fits in 24 bits.
}

var _ io.Closer = (*webpJob)(nil)
