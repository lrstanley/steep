// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"getsteep.dev/steep/reel/vt"
)

func TestShadow(t *testing.T) {
	t.Parallel()

	if _, err := New(WithShadow(0, 0, -1, nil)); err == nil {
		t.Error("New(WithShadow(blur -1)) did not fail")
	}

	green := color.NRGBA{G: 255, A: 255}
	draw := func(opts ...Option) (*image.NRGBA, image.Rectangle) {
		d := MustNew(append([]Option{WithScale(scaleFor(9)), WithPalette(Palette{DefaultBackground: color.White})}, opts...)...)
		t.Cleanup(func() { _ = d.Close() })
		img := drawNRGBA(t, d, vt.NewBuffer(20, 4))
		return img, img.Bounds().Inset(16)
	}

	// A sharp shadow 4px down and right is the shadow color over the margin
	// where it lies outside the window, and the margin elsewhere.
	img, win := draw(WithMargin(16, green), WithShadow(4, 4, 0, color.NRGBA{A: 255}))
	for _, c := range []struct {
		name string
		at   image.Point
		want color.NRGBA
	}{
		{"below", image.Pt(win.Min.X+10, win.Max.Y+2), color.NRGBA{A: 255}},
		{"right", image.Pt(win.Max.X+3, win.Min.Y+10), color.NRGBA{A: 255}},
		{"below, past the shadow", image.Pt(win.Min.X+10, win.Max.Y+4), green},
		{"above", image.Pt(win.Min.X+10, win.Min.Y-1), green},
		{"left of it", image.Pt(win.Min.X+2, win.Max.Y+1), green},
	} {
		if got := img.NRGBAAt(c.at.X, c.at.Y); got != c.want {
			t.Errorf("sharp shadow %s, at %v = %v, want %v", c.name, c.at, got, c.want)
		}
	}

	// A blurred one fades out from the window.
	img, win = draw(WithMargin(16, green), WithShadow(0, 0, 12, nil))
	near, far := img.NRGBAAt(win.Min.X+20, win.Max.Y), img.NRGBAAt(win.Min.X+20, win.Max.Y+10)
	if near.G >= far.G || far.G == 255 || img.NRGBAAt(0, 0).G < 250 {
		t.Errorf("blurred shadow: near %v, far %v, corner %v; want darker nearer the window", near, far, img.NRGBAAt(0, 0))
	}

	// Over a transparent margin, the shadow is translucent shadow color.
	img, win = draw(WithMargin(16, nil), WithShadow(0, 0, 12, color.NRGBA{R: 10, G: 20, B: 30, A: 200}))
	px := img.NRGBAAt(win.Min.X+20, win.Max.Y+3)
	if px.R != 10 || px.G != 20 || px.B != 30 || px.A == 0 || px.A >= 200 || img.NRGBAAt(0, 0).A > 5 {
		t.Errorf("shadow on a transparent margin = %v, corner %v; want translucent #0a141e", px, img.NRGBAAt(0, 0))
	}

	// It shows past the window's rounded corners, but not past those it
	// does not reach.
	img, win = draw(WithMargin(16, green), WithBorderRadius(10), WithShadow(6, 6, 0, color.NRGBA{A: 255}))
	if br := img.NRGBAAt(win.Max.X-1, win.Max.Y-1); br != (color.NRGBA{A: 255}) {
		t.Errorf("bottom-right corner = %v, want the shadow", br)
	}
	if tl := img.NRGBAAt(win.Min.X, win.Min.Y); tl != green {
		t.Errorf("top-left corner = %v, want the margin", tl)
	}

	// A shadow with no alpha draws nothing.
	plain, _ := draw(WithMargin(16, green))
	if img, _ = draw(WithMargin(16, green), WithShadow(4, 4, 8, color.NRGBA{R: 255})); img.Rect != plain.Rect || !bytes.Equal(img.Pix, plain.Pix) {
		t.Error("a transparent shadow changed the image")
	}
}

func TestShadowBackdropCached(t *testing.T) {
	t.Parallel()

	d := MustNew(WithScale(scaleFor(9)), WithMargin(8, color.Black), WithShadow(0, 2, 6, nil))
	t.Cleanup(func() { _ = d.Close() })
	drawNRGBA(t, d, vt.NewBuffer(10, 3))
	first := d.backdrop
	drawNRGBA(t, d, vt.NewBuffer(10, 3))
	if first == nil || d.backdrop != first {
		t.Fatal("the backdrop was not kept between draws of one size")
	}
	drawNRGBA(t, d, vt.NewBuffer(12, 3))
	if d.backdrop == first || d.backdrop.Rect.Size() == first.Rect.Size() {
		t.Error("the backdrop was not rebuilt for a new size")
	}

	if d = MustNew(WithMargin(8, color.Black)); d.backdropFor(image.Pt(40, 40), color.NRGBA{A: 255}) != nil {
		t.Error("a one-color margin without a shadow has a backdrop")
	}
	_ = d.Close()
}

func TestGaussianBoxes(t *testing.T) {
	t.Parallel()

	if b := gaussianBoxes(0); b != [3]int{} {
		t.Errorf("gaussianBoxes(0) = %v", b)
	}
	for _, sigma := range []float64{1, 2.5, 6, 12, 40} {
		// A box of radius r has a variance of ((2r+1)^2-1)/12.
		var variance float64
		for _, r := range gaussianBoxes(sigma) {
			variance += (math.Pow(float64(2*r+1), 2) - 1) / 12
		}
		if got := math.Sqrt(variance); math.Abs(got-sigma) > 0.5 {
			t.Errorf("gaussianBoxes(%v) has a sigma of %.2f", sigma, got)
		}
	}
}
