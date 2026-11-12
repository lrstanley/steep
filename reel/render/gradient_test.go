// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"testing"

	"getsteep.dev/steep/reel/vt"
)

func TestMarginGradient(t *testing.T) {
	t.Parallel()

	red, blue, green := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}, color.NRGBA{G: 255, A: 255}
	for _, opts := range [][]Option{{WithMarginGradient(4, 0)}, {WithMarginGradient(4, 0, red, nil)}} {
		if _, err := New(opts...); err == nil {
			t.Error("New() with no colors, or a nil one, did not fail")
		}
	}

	draw := func(opts ...Option) *image.NRGBA {
		d := MustNew(append([]Option{WithScale(scaleFor(9)), WithPalette(Palette{DefaultBackground: green})}, opts...)...)
		t.Cleanup(func() { _ = d.Close() })
		return drawNRGBA(t, d, vt.NewBuffer(20, 4))
	}
	// The ends of a 135 degree gradient are the top-left and bottom-right
	// corners, and the last of WithMargin and WithMarginGradient wins.
	img := draw(WithMargin(12, green), WithMarginGradient(12, 135, red, blue))
	b := img.Bounds()
	if tl, br := img.NRGBAAt(b.Min.X, b.Min.Y), img.NRGBAAt(b.Max.X-1, b.Max.Y-1); tl.R < 240 || br.B < 240 {
		t.Errorf("135deg red to blue: corners = %v, %v", tl, br)
	}
	if img = draw(WithMarginGradient(12, 135, red, blue), WithMargin(12, green)); img.NRGBAAt(0, 0) != green {
		t.Errorf("gradient then solid: corner = %v, want %v", img.NRGBAAt(0, 0), green)
	}

	// Past the window's rounded corners is the gradient where they are.
	img = draw(WithBorderRadius(12), WithMarginGradient(8, 90, red, blue))
	win := img.Bounds().Inset(8)
	left, right := img.NRGBAAt(win.Min.X, win.Min.Y), img.NRGBAAt(win.Max.X-1, win.Min.Y)
	if left.R <= right.R || left.B >= right.B || left.G > 128 || right.G > 128 { // not the window
		t.Errorf("window corners = %v, %v; want the left redder and the right bluer", left, right)
	}
}
