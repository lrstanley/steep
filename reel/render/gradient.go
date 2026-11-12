// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"errors"
	"image"
	"image/color"
	"math"
	"slices"

	icol "getsteep.dev/steep/reel/render/internal/color"
	"getsteep.dev/steep/reel/render/internal/config"
	"github.com/lucasb-eyer/go-colorful"
)

// WithMarginGradient sets the margin in pixels, like [WithMargin], and fills
// it with a linear gradient of colors, as CSS linear-gradient draws one:
// angle is in degrees clockwise from up, so 90 runs left to right and 180
// top to bottom, and the colors spread evenly from one corner of the image
// to the opposite one, mixed in the Oklab color space. The last of
// WithMargin and WithMarginGradient wins.
func WithMarginGradient(px int, angle float64, colors ...color.Color) Option {
	return func(d *Renderer) error {
		v, err := config.ParsePositivePx(px, "margin")
		if err != nil {
			return err
		}
		if len(colors) == 0 || slices.Contains(colors, nil) {
			return errors.New("render: margin gradient needs one or more colors, none nil")
		}
		d.opts.Margin, d.opts.MarginFill, d.opts.MarginAngle = v, slices.Clone(colors), angle
		return nil
	}
}

// gradientRamp mixes stops into 1024 steps, or returns nil for fewer than
// two stops, which are a solid fill.
func gradientRamp(stops []color.Color) []color.NRGBA {
	if len(stops) < 2 {
		return nil
	}
	ramp := make([]color.NRGBA, 1024)
	for i := range ramp {
		pos := float64(i) / float64(len(ramp)-1) * float64(len(stops)-1)
		seg := min(int(pos), len(stops)-2)
		from, to := stops[seg], stops[seg+1]
		a, _ := colorful.MakeColor(from)
		b, _ := colorful.MakeColor(to)
		ramp[i].R, ramp[i].G, ramp[i].B = a.BlendOkLab(b, pos-float64(seg)).Clamped().RGB255()
		ramp[i].A = icol.BlendNRGBA(icol.NRGBA(from), icol.NRGBA(to), pos-float64(seg)).A
	}
	return ramp
}

// gradient is a ramp laid across an image: the ramp index of the pixel at
// x, y is t0 + x*dx + y*dy.
type gradient struct {
	ramp       []color.NRGBA
	t0, dx, dy float64
}

func newGradient(ramp []color.NRGBA, angle float64, over image.Rectangle) gradient {
	sin, cos := math.Sincos(angle * math.Pi / 180)
	w, h, last := float64(over.Dx()), float64(over.Dy()), float64(len(ramp)-1)
	scale := last / (math.Abs(w*sin) + math.Abs(h*cos))
	// The center of the image, less half a pixel to sample pixel centers.
	cx, cy := float64(over.Min.X)+w/2-0.5, float64(over.Min.Y)+h/2-0.5
	return gradient{ramp: ramp, t0: (cy*cos-cx*sin)*scale + last/2, dx: sin * scale, dy: -cos * scale}
}

func (g *gradient) at(x, y int) color.NRGBA {
	// The products are rounded before they are summed, so no fused
	// multiply-add makes a pixel differ between repaints.
	i := int(math.Round(g.t0 + float64(float64(x)*g.dx) + float64(float64(y)*g.dy)))
	return g.ramp[min(max(i, 0), len(g.ramp)-1)]
}
