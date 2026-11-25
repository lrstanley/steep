// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"getsteep.dev/steep/reel/vt"
)

func TestBackdropGradient(t *testing.T) {
	t.Parallel()

	red, blue, green := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}, color.NRGBA{G: 255, A: 255}
	for _, opts := range [][]Option{{WithBackdrop(LinearGradient(0))}, {WithBackdrop(LinearGradient(0, red, nil))}, {WithBackdrop(LinearGradient(math.NaN(), red))}} {
		if _, err := New(opts...); err == nil {
			t.Error("New() with no colors, a nil one, or no angle did not fail")
		}
	}

	draw := func(opts ...Option) *image.NRGBA {
		d := MustNew(append([]Option{WithScale(scaleFor(9)), WithPalette(Palette{DefaultBackground: green})}, opts...)...)
		t.Cleanup(func() { _ = d.Close() })
		return drawNRGBA(t, d, vt.NewBuffer(20, 4))
	}
	// The ends of a 135 degree gradient are the top-left and bottom-right
	// corners, and the last backdrop wins.
	img := draw(WithMargin(12), WithBackdrop(Solid(green)), WithBackdrop(LinearGradient(135, red, blue)))
	b := img.Bounds()
	if tl, br := img.NRGBAAt(b.Min.X, b.Min.Y), img.NRGBAAt(b.Max.X-1, b.Max.Y-1); tl.R < 240 || br.B < 240 {
		t.Errorf("135deg red to blue: corners = %v, %v", tl, br)
	}
	if img = draw(WithMargin(12), WithBackdrop(LinearGradient(135, red, blue)), WithBackdrop(Solid(green))); img.NRGBAAt(0, 0) != green {
		t.Errorf("gradient then solid: corner = %v, want %v", img.NRGBAAt(0, 0), green)
	}

	// Past the window's rounded corners is the gradient where they are.
	img = draw(WithBorderRadius(12), WithMargin(8), WithBackdrop(LinearGradient(90, red, blue)))
	win := img.Bounds().Inset(8)
	left, right := img.NRGBAAt(win.Min.X, win.Min.Y), img.NRGBAAt(win.Max.X-1, win.Min.Y)
	if left.R <= right.R || left.B >= right.B || left.G > 128 || right.G > 128 { // not the window
		t.Errorf("window corners = %v, %v; want the left redder and the right bluer", left, right)
	}
}

func TestBackdropShowsThroughWindow(t *testing.T) {
	t.Parallel()

	bg, back := color.NRGBA{R: 200, A: 255}, color.NRGBA{B: 200, A: 255}
	draw := func(opts ...Option) (*image.NRGBA, image.Rectangle) {
		d := MustNew(append([]Option{WithScale(scaleFor(9)), WithPalette(Palette{DefaultBackground: bg})}, opts...)...)
		t.Cleanup(func() { _ = d.Close() })
		img := drawNRGBA(t, d, vt.NewBuffer(10, 3))
		return img, img.Bounds().Inset(6)
	}
	// Half the background over the backdrop: opaque, and between the two.
	want := color.NRGBA{R: 100, B: 100, A: 255}
	img, win := draw(WithMargin(6), WithBackdrop(Solid(back)), WithBackgroundOpacity(0.5))
	if got := img.NRGBAAt(win.Min.X+2, win.Min.Y+2); !nearNRGBA(got, want, 1) {
		t.Errorf("window over a solid backdrop = %v, want %v", got, want)
	}
	if got := img.NRGBAAt(0, 0); got != back {
		t.Errorf("margin = %v, want %v", got, back)
	}

	// The palette's backdrop is the default, and with no margin still shows
	// through the window.
	img, _ = draw(WithPalette(Palette{DefaultBackground: bg, Backdrop: back}), WithBackgroundOpacity(0.5))
	if got := img.NRGBAAt(2, 2); !nearNRGBA(got, want, 1) {
		t.Errorf("window over the palette's backdrop = %v, want %v", got, want)
	}

	// Over a transparent backdrop, the window keeps its translucency.
	img, win = draw(WithMargin(6), WithBackdrop(Solid(color.Transparent)), WithBackgroundOpacity(0.5))
	if got := img.NRGBAAt(win.Min.X+2, win.Min.Y+2); got.A < 126 || got.A > 129 || got.R != 200 {
		t.Errorf("window over a transparent backdrop = %v, want red at half opacity", got)
	}

	// Over a translucent backdrop, the window is blended over it once: 0.4
	// over 0.5 is 0.7, where twice would be 0.85. The same holds for one
	// color and for a gradient.
	half := color.NRGBA{B: 200, A: 0x80}
	for _, b := range []Backdrop{Solid(half), LinearGradient(90, half, half)} {
		img, win = draw(WithMargin(6), WithBackdrop(b), WithBackgroundOpacity(0.4))
		if got := img.NRGBAAt(win.Min.X+2, win.Min.Y+2); got.A < 177 || got.A > 180 {
			t.Errorf("window over a translucent backdrop = %v, want alpha 179", got)
		}
	}

	// A translucent background of the palette's own, at full opacity, is
	// blended over a translucent backdrop too.
	img, win = draw(WithMargin(6), WithBackdrop(Solid(half)), WithPalette(Palette{DefaultBackground: color.NRGBA{R: 200, A: 0x66}}))
	if got := img.NRGBAAt(win.Min.X+2, win.Min.Y+2); got.A < 177 || got.A > 180 {
		t.Errorf("translucent palette background over a translucent backdrop = %v, want alpha 179", got)
	}

	// Through a transparent window over a gradient is the gradient, as in
	// the margin beside it.
	img, win = draw(WithMargin(6), WithBackdrop(LinearGradient(0, color.Black, color.White)), WithBackgroundOpacity(0))
	for _, y := range []int{win.Min.Y + 2, win.Max.Y - 3} {
		if got, beside := img.NRGBAAt(win.Min.X+2, y), img.NRGBAAt(win.Min.X-1, y); !nearNRGBA(got, beside, 1) {
			t.Errorf("row %d: window over a gradient = %v, margin beside it %v", y, got, beside)
		}
	}

	// The window does not show its own shadow through it, only the
	// backdrop, and the shadow still shows around it.
	img, win = draw(WithMargin(6), WithBackdrop(Solid(back)), WithBackgroundOpacity(0), WithShadow(0, 0, 0, color.Black))
	if got := img.NRGBAAt(win.Min.X+2, win.Min.Y+2); got != back {
		t.Errorf("window with a shadow under it = %v, want the backdrop %v", got, back)
	}
	if got := img.NRGBAAt(0, 0); got != back {
		t.Errorf("margin past the shadow = %v, want %v", got, back)
	}
}

func TestBackdropWallpaper(t *testing.T) {
	t.Parallel()

	for _, b := range []Backdrop{Wallpaper(nil), Wallpaper(image.NewNRGBA(image.Rect(0, 0, 0, 4)))} {
		if _, err := New(WithBackdrop(b)); err == nil {
			t.Error("New() with a nil or empty wallpaper did not fail")
		}
	}

	// A wallpaper two pixels wide, red then blue, covers an image wider
	// than tall by scaling to its width, so its left half is red and its
	// right half blue, through a transparent window too.
	red, blue := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}
	wall := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	wall.SetNRGBA(0, 0, red)
	wall.SetNRGBA(1, 0, blue)
	d := MustNew(WithScale(scaleFor(9)), WithMargin(4), WithBackdrop(Wallpaper(wall)), WithBackgroundOpacity(0))
	t.Cleanup(func() { _ = d.Close() })
	img := drawNRGBA(t, d, vt.NewBuffer(20, 3))
	b := img.Bounds()
	for _, y := range []int{0, b.Dy() / 2, b.Dy() - 1} {
		if l, r := img.NRGBAAt(b.Min.X, y), img.NRGBAAt(b.Max.X-1, y); l != red || r != blue {
			t.Errorf("row %d: left = %v, right = %v; want red and blue", y, l, r)
		}
	}
	if mid := img.NRGBAAt(b.Min.X+b.Dx()/4, b.Dy()/2); !nearNRGBA(mid, red, 2) {
		t.Errorf("window's left = %v, want the wallpaper's red", mid)
	}
}

func TestUnderlay(t *testing.T) {
	t.Parallel()

	back := color.NRGBA{G: 200, A: 255}
	d := MustNew(WithScale(scaleFor(9)), WithMargin(4), WithBackdrop(Solid(back)))
	t.Cleanup(func() { _ = d.Close() })
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	img.SetNRGBA(1, 1, color.NRGBA{R: 200, A: 255})
	img.SetNRGBA(2, 2, color.NRGBA{R: 200, A: 128})
	d.Underlay(img, image.Rect(0, 0, 4, 4))
	if got, want := img.NRGBAAt(1, 1), (color.NRGBA{R: 200, A: 255}); got != want {
		t.Errorf("opaque pixel = %v, want it left alone", got)
	}
	if got, want := img.NRGBAAt(2, 2), (color.NRGBA{R: 100, G: 100, A: 255}); !nearNRGBA(got, want, 1) {
		t.Errorf("translucent pixel = %v, want %v", got, want)
	}
	if got := img.NRGBAAt(0, 0); got != back {
		t.Errorf("transparent pixel = %v, want the backdrop %v", got, back)
	}
	if got := img.NRGBAAt(5, 5); got != (color.NRGBA{}) {
		t.Errorf("pixel outside r = %v, want it left alone", got)
	}
}

// nearNRGBA reports whether each channel of a and b is within tol.
func nearNRGBA(a, b color.NRGBA, tol int) bool {
	d := func(x, y uint8) bool { return max(int(x)-int(y), int(y)-int(x)) <= tol }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

func TestSeeThrough(t *testing.T) {
	t.Parallel()

	gradient := WithBackdrop(LinearGradient(90, color.Black, color.White))
	for _, c := range []struct {
		name string
		opts []Option
		want bool
	}{
		{"opaque", []Option{gradient}, false},
		{"opacity", []Option{gradient, WithBackgroundOpacity(0.5)}, true},
		{"palette", []Option{gradient, WithPalette(Palette{DefaultBackground: color.NRGBA{R: 200, A: 0x80}})}, true},
		{"cursor", []Option{gradient, WithPalette(Palette{Cursor: color.NRGBA{R: 200, A: 0x80}})}, true},
		{"indexed", []Option{gradient, WithPalette(Palette{Indexed: map[int]color.Color{1: color.NRGBA{R: 200, A: 0x80}}})}, true},
		{"solid", []Option{WithBackdrop(Solid(color.Black)), WithBackgroundOpacity(0.5)}, true},
		{"transparent", []Option{WithBackdrop(Solid(color.Transparent)), WithBackgroundOpacity(0.5)}, false},
	} {
		d := MustNew(c.opts...)
		if got := d.SeeThrough(); got != c.want {
			t.Errorf("%s: SeeThrough() = %v, want %v", c.name, got, c.want)
		}
		_ = d.Close()
	}
}

// TestGlyphEdgesTranslucent checks that the edges of text in a translucent
// window stay translucent, so what is behind the window shows at them as it
// does around them, rather than an opaque fringe of the background color.
func TestGlyphEdgesTranslucent(t *testing.T) {
	t.Parallel()
	d := MustNew(WithScale(scaleFor(20)), WithBackdrop(Solid(color.Transparent)), WithBackgroundOpacity(0.5))
	t.Cleanup(func() { _ = d.Close() })
	scr := vt.NewBuffer(4, 1)
	for x, r := range "MoW@" {
		scr.SetCell(x, 0, &vt.Cell{Content: string(r), Width: 1})
	}
	img := drawNRGBA(t, d, scr)
	var edges int
	for i := 3; i < len(img.Pix); i += 4 {
		if a := img.Pix[i]; a > 130 && a < 250 {
			edges++
		}
	}
	if edges == 0 {
		t.Error("no translucent pixel at the edges of the text")
	}
}
