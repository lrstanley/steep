// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package native

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gen2brain/webp"

	"getsteep.dev/steep/reel/sink"
)

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Rect, image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

// withBox returns a copy of img with r filled with c.
func withBox(img *image.NRGBA, r image.Rectangle, c color.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(img.Rect)
	copy(out.Pix, img.Pix)
	draw.Draw(out, r, image.NewUniform(c), image.Point{}, draw.Src)
	return out
}

type testFrame struct {
	img   *image.NRGBA
	delay time.Duration
}

func encode(t *testing.T, frames []testFrame, opts ...sink.Option) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.webp")
	enc, err := sink.CreateVideo(path, sink.FormatWebP, append([]sink.Option{sink.WithDrivers(Driver())}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		if err = enc.WriteFrame(sink.Frame{Image: f.img, Delay: f.delay}); err != nil {
			t.Fatal(err)
		}
	}
	if err = enc.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decode(t *testing.T, data []byte) *webp.WEBP {
	t.Helper()
	anim, err := webp.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return anim
}

func nrgbaOf(img image.Image) *image.NRGBA {
	out := image.NewNRGBA(image.Rectangle{Max: img.Bounds().Size()})
	draw.Draw(out, out.Rect, img, img.Bounds().Min, draw.Src)
	return out
}

func TestWebPLosslessRoundTrip(t *testing.T) {
	t.Parallel()

	const w, h = 37, 21
	bg := solid(w, h, color.NRGBA{R: 20, G: 20, B: 30, A: 0xff})
	a := withBox(bg, image.Rect(3, 5, 9, 8), color.NRGBA{R: 200, G: 40, B: 40, A: 0xff})
	b := withBox(a, image.Rect(30, 18, 37, 21), color.NRGBA{R: 40, G: 200, B: 90, A: 0xff})
	c := withBox(b, image.Rect(0, 0, 37, 1), color.NRGBA{R: 1, G: 2, B: 3, A: 0xff})
	frames := []testFrame{
		{bg, 500 * time.Millisecond},
		{a, 30 * time.Millisecond},
		// Identical to the frame before, so it extends it.
		{a, 70 * time.Millisecond},
		{b, 2 * time.Second},
		{c, time.Millisecond},
	}
	data := encode(t, frames, sink.WithLoopCount(2))
	anim := decode(t, data)

	want := []*image.NRGBA{bg, a, b, c}
	wantDelays := []int{500, 100, 2000, 1}
	if len(anim.Image) != len(want) {
		t.Fatalf("decoded %d frames, want %d", len(anim.Image), len(want))
	}
	if !slices.Equal(anim.Delay, wantDelays) {
		t.Fatalf("delays = %v, want %v", anim.Delay, wantDelays)
	}
	// ANIM follows VP8X; its loop count is after the background color.
	if string(data[30:34]) != "ANIM" || binary.LittleEndian.Uint16(data[42:]) != 3 {
		t.Fatalf("loop count = %d, want 3 plays", binary.LittleEndian.Uint16(data[42:]))
	}
	for i, img := range anim.Image {
		if got := nrgbaOf(img); !bytes.Equal(got.Pix, want[i].Pix) {
			t.Fatalf("frame %d differs from the input", i)
		}
	}
}

func TestWebPKeepsTransparency(t *testing.T) {
	t.Parallel()

	empty := solid(16, 16, color.NRGBA{})
	soft := withBox(empty, image.Rect(4, 4, 12, 12), color.NRGBA{R: 255, G: 255, B: 255, A: 0x80})
	// Back to transparent: the frame must replace, not blend over, the one
	// before.
	data := encode(t, []testFrame{{soft, time.Second}, {empty, time.Second}})
	anim := decode(t, data)
	if len(anim.Image) != 2 {
		t.Fatalf("decoded %d frames, want 2", len(anim.Image))
	}
	if got := nrgbaOf(anim.Image[0]); !bytes.Equal(got.Pix, soft.Pix) {
		t.Fatal("translucent frame not kept exactly")
	}
	if got := nrgbaOf(anim.Image[1]); !bytes.Equal(got.Pix, empty.Pix) {
		t.Fatal("frame blended over the one before")
	}
	if data[20]&vp8xFlagAlpha == 0 {
		t.Fatal("alpha flag not set")
	}
}

// TestWebPTranslucentSubRect checks a changed rectangle that can't blend
// (its pixels aren't opaque) on a canvas whose other pixels all differ, so
// encoding the rectangle with the canvas's stride would show.
func TestWebPTranslucentSubRect(t *testing.T) {
	t.Parallel()

	bg := solid(16, 16, color.NRGBA{})
	for i := 0; i < len(bg.Pix); i += 4 {
		bg.Pix[i], bg.Pix[i+1], bg.Pix[i+3] = byte(i), byte(i>>8), 0xff
	}
	soft := withBox(bg, image.Rect(4, 4, 8, 8), color.NRGBA{R: 255, B: 255, A: 0x80}) // Survives premultiplying.
	anim := decode(t, encode(t, []testFrame{{bg, time.Second}, {soft, time.Second}}))
	if len(anim.Image) != 2 {
		t.Fatalf("decoded %d frames, want 2", len(anim.Image))
	}
	if got := nrgbaOf(anim.Image[1]); !bytes.Equal(got.Pix, soft.Pix) {
		t.Fatalf("translucent rectangle not kept exactly: pixel (4,4) = %v", got.NRGBAAt(4, 4))
	}
}

func TestWebPLossy(t *testing.T) {
	t.Parallel()

	frames := []testFrame{
		{solid(64, 32, color.NRGBA{R: 200, G: 40, B: 40, A: 0xff}), time.Second},
		{solid(64, 32, color.NRGBA{R: 40, G: 40, B: 200, A: 0xff}), time.Second},
	}
	data := encode(t, frames, sink.WithQuality(60))
	if !bytes.Contains(data, []byte("VP8 ")) || bytes.Contains(data, []byte("VP8L")) {
		t.Fatal("WithQuality(60) should select lossy VP8 frames")
	}
	if anim := decode(t, data); len(anim.Image) != 2 {
		t.Fatalf("decoded %d frames, want 2", len(anim.Image))
	}
}

func TestWebPSubRectFrames(t *testing.T) {
	t.Parallel()

	// A large frame with a one-cell change: the second frame stores far less
	// than the first.
	bg := solid(800, 400, color.NRGBA{R: 10, G: 10, B: 10, A: 0xff})
	for x := 0; x < 800; x += 7 {
		draw.Draw(bg, image.Rect(x, 0, x+3, 400), image.NewUniform(color.NRGBA{R: uint8(x), G: 90, B: 160, A: 0xff}), image.Point{}, draw.Src)
	}
	cursor := withBox(bg, image.Rect(101, 201, 109, 217), color.NRGBA{R: 255, G: 255, B: 255, A: 0xff})
	one := encode(t, []testFrame{{bg, time.Second}})
	two := encode(t, []testFrame{{bg, time.Second}, {cursor, time.Second}})
	if grew := len(two) - len(one); grew > 256 {
		t.Fatalf("a 8x16 change added %d bytes; want a small sub-rect frame", grew)
	}
	anim := decode(t, two)
	if got := nrgbaOf(anim.Image[1]); !bytes.Equal(got.Pix, cursor.Pix) {
		t.Fatal("sub-rect frame does not decode to the input")
	}
}

func TestChangedRect(t *testing.T) {
	t.Parallel()

	a := solid(10, 10, color.NRGBA{A: 0xff})
	if r := changedRect(a, a); !r.Empty() {
		t.Fatalf("changedRect of equal images = %v", r)
	}
	b := withBox(a, image.Rect(3, 4, 5, 9), color.NRGBA{R: 1, A: 0xff})
	if r := changedRect(a, b); r != image.Rect(3, 4, 5, 9) {
		t.Fatalf("changedRect = %v, want (3,4)-(5,9)", r)
	}
}

func TestDriverOnlyWebP(t *testing.T) {
	t.Parallel()

	for _, format := range []sink.Format{sink.FormatMP4, sink.FormatWebM} {
		if err := Driver().Check(format, sink.VideoConfig{}); !errors.Is(err, errors.ErrUnsupported) {
			t.Fatalf("Check(%s) = %v, want ErrUnsupported", format, err)
		}
	}
	if err := Driver().Check(sink.FormatWebP, sink.VideoConfig{}); err != nil {
		t.Fatalf("Check(webp) = %v", err)
	}
}
