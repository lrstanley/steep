// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"math"

	icol "getsteep.dev/steep/reel/render/internal/color"
	"getsteep.dev/steep/reel/render/internal/config"
	idraw "getsteep.dev/steep/reel/render/internal/draw"
	"getsteep.dev/steep/reel/render/internal/effects"
)

// DefaultShadowColor is the color of [WithShadow] with a nil color: black
// at half opacity.
var DefaultShadowColor = color.NRGBA{A: 0x80}

// WithShadow draws a drop shadow behind the window, into the margin, as CSS
// box-shadow draws one: the window's shape, rounded as [WithBorderRadius]
// rounds it, moved x pixels right and y pixels down, blurred by blur pixels
// (a Gaussian blur of half that standard deviation; 0 is a sharp edge), in
// c. A nil c is [DefaultShadowColor]; one with no alpha draws no shadow.
//
// It shows only in the margin and past the window's rounded corners, so it
// needs a [WithMargin] or [WithMarginGradient] about as wide as the blur and
// the offset together, or the image's edge cuts it off. Over a transparent
// margin, its pixels are translucent, which a GIF cannot hold: see
// [getsteep.dev/steep/reel/sink.WithBackground].
//
// It is drawn once for each image size, with the margin, so it costs nothing
// more per frame than the margin itself.
func WithShadow(x, y, blur int, c color.Color) Option {
	return func(d *Renderer) error {
		v, err := config.ParsePositivePx(blur, "shadow blur")
		if err != nil {
			return err
		}
		col := DefaultShadowColor
		if c != nil {
			col = icol.NRGBA(c)
		}
		d.opts.Shadow = config.Shadow{Offset: image.Pt(x, y), Blur: v, Color: col}
		return nil
	}
}

// backdropFor returns what shows outside the window in an image of size: the
// margin, with its gradient or its fill, and the shadow over it. It is built
// on first use for each size and kept, and is nil when the margin is one
// color with no shadow, which [Renderer.drawBackground] fills directly.
func (d *Renderer) backdropFor(size image.Point, fill color.NRGBA) *image.NRGBA {
	shadow := d.opts.Shadow.Color.A > 0
	if d.marginRamp == nil && !shadow {
		return nil
	}
	if d.backdrop != nil && d.backdrop.Rect.Size() == size {
		return d.backdrop
	}
	img := image.NewNRGBA(image.Rectangle{Max: size})
	if d.marginRamp != nil {
		g := newGradient(d.marginRamp, d.opts.MarginAngle, img.Rect)
		for y := range size.Y {
			for x := range size.X {
				img.SetNRGBA(x, y, g.at(x, y))
			}
		}
	} else {
		idraw.Fill(img, img.Rect, fill)
	}
	if shadow {
		win := img.Rect.Inset(d.opts.Margin.Int())
		drawShadow(img, win, d.opts.BorderRadius.Int(), d.opts.Shadow)
	}
	d.backdrop = img
	return img
}

// drawShadow composites the shadow of a window at win, with corners of
// radius, over dst.
func drawShadow(dst *image.NRGBA, win image.Rectangle, radius int, s config.Shadow) {
	shape := win.Add(s.Offset)
	if shape.Empty() {
		return
	}
	radius = max(0, min(radius, shape.Dx()/2, shape.Dy()/2))
	boxes := gaussianBoxes(float64(s.Blur) / 2)
	reach := boxes[0] + boxes[1] + boxes[2]
	// The mask past dst by more than the blur reaches cannot show in it.
	area := shape.Inset(-reach).Intersect(dst.Rect.Inset(-reach))
	if area.Empty() {
		return
	}

	w, h := area.Dx(), area.Dy()
	mask, tmp := make([]float32, w*h), make([]float32, w*h)
	for y := max(area.Min.Y, shape.Min.Y); y < min(area.Max.Y, shape.Max.Y); y++ {
		for x := max(area.Min.X, shape.Min.X); x < min(area.Max.X, shape.Max.X); x++ {
			mask[(y-area.Min.Y)*w+x-area.Min.X] = float32(effects.RoundedCoverage(shape, radius, x, y))
		}
	}
	// Three box blurs in a row are close to a Gaussian one, and cost the
	// same whatever the blur.
	for _, r := range boxes {
		if r == 0 {
			continue
		}
		for y := range h {
			boxBlurLine(tmp[y*w:], mask[y*w:], 1, w, r)
		}
		for x := range w {
			boxBlurLine(mask[x:], tmp[x:], w, h, r)
		}
	}

	paint := area.Intersect(dst.Rect)
	for y := paint.Min.Y; y < paint.Max.Y; y++ {
		row := mask[(y-area.Min.Y)*w:]
		for x := paint.Min.X; x < paint.Max.X; x++ {
			cov := row[x-area.Min.X]
			if cov <= 0 {
				continue
			}
			i := dst.PixOffset(x, y)
			idraw.OverPixel(dst.Pix[i:i+4], s.Color, uint8(math.Round(float64(min(cov, 1))*255)))
		}
	}
}

// gaussianBoxes are the radii of three box blurs that, one after another,
// approximate a Gaussian blur of sigma. They are all zero for a sigma too
// small to blur.
func gaussianBoxes(sigma float64) [3]int {
	var radii [3]int
	if sigma <= 0 {
		return radii
	}
	const n = 3
	// The widths are odd, wl or wl+2, m of them wl, so their variances sum
	// to sigma squared.
	wl := int(math.Sqrt(12*sigma*sigma/n + 1))
	if wl%2 == 0 {
		wl--
	}
	m := int(math.Round((12*sigma*sigma - float64(n*wl*wl+4*n*wl+3*n)) / float64(-4*wl-4)))
	for i := range radii {
		width := wl + 2
		if i < m {
			width = wl
		}
		radii[i] = (width - 1) / 2
	}
	return radii
}

// boxBlurLine averages each of the n values of src, stride apart, with the r
// on either side of it, into dst, with zeros past the ends.
func boxBlurLine(dst, src []float32, stride, n, r int) {
	var sum float64
	for i := range min(r, n) {
		sum += float64(src[i*stride])
	}
	inv := 1 / float64(2*r+1)
	for i := range n {
		if j := i + r; j < n {
			sum += float64(src[j*stride])
		}
		if j := i - r - 1; j >= 0 {
			sum -= float64(src[j*stride])
		}
		dst[i*stride] = float32(sum * inv)
	}
}
