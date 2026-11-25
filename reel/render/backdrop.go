// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"math"
	"slices"

	icol "getsteep.dev/steep/reel/render/internal/color"
	"getsteep.dev/steep/reel/render/internal/config"
	idraw "getsteep.dev/steep/reel/render/internal/draw"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

// Backdrop is what a renderer draws behind the window, as a desktop is
// behind a terminal: it fills the whole image, and shows in the margin
// ([WithMargin]), past the window's rounded corners, and through the window
// where its background is translucent ([WithBackgroundOpacity]). The zero
// Backdrop is the palette's backdrop color, or transparent without one. Make
// one with [Solid], [LinearGradient], or [Wallpaper].
type Backdrop struct {
	b   config.Backdrop
	err error
}

// Solid is a backdrop of one color. A nil c is the zero [Backdrop], and
// [color.Transparent] is a transparent one whatever the palette sets.
func Solid(c color.Color) Backdrop {
	if c == nil {
		return Backdrop{}
	}
	return Backdrop{b: config.Backdrop{Colors: []color.Color{c}}}
}

// LinearGradient is a backdrop of a linear gradient of colors, as CSS
// linear-gradient draws one: angle is in degrees clockwise from up, so 90
// runs left to right and 180 top to bottom, and the colors spread evenly
// from one corner of the image to the opposite one, mixed in the Oklab color
// space. It needs one or more colors, none nil, and one is a [Solid].
func LinearGradient(angle float64, colors ...color.Color) Backdrop {
	switch {
	case len(colors) == 0 || slices.Contains(colors, nil):
		return Backdrop{err: errors.New("render: gradient backdrop needs one or more colors, none nil")}
	case math.IsNaN(angle) || math.IsInf(angle, 0):
		return Backdrop{err: errors.New("render: gradient backdrop angle is not finite")}
	}
	return Backdrop{b: config.Backdrop{Colors: slices.Clone(colors), Angle: angle}}
}

// Wallpaper is a backdrop of img, scaled to cover the whole image and
// centered on it, as CSS background-size: cover places one, so the sides
// or the top and bottom of img are cut off when the image is a different
// shape. It is scaled once for each image size, so img must not change while
// a renderer uses it.
func Wallpaper(img image.Image) Backdrop {
	if img == nil || img.Bounds().Empty() {
		return Backdrop{err: errors.New("render: wallpaper backdrop needs a non-empty image")}
	}
	return Backdrop{b: config.Backdrop{Image: img}}
}

// WithBackdrop sets what is drawn behind the window (see [Backdrop]).
func WithBackdrop(b Backdrop) Option {
	return func(d *Renderer) error {
		if b.err != nil {
			return b.err
		}
		d.opts.Backdrop = b.b
		return nil
	}
}

// backdropColor is the backdrop's one color: its first color, else the
// palette's backdrop color, else transparent. It is what the backdrop is
// when [Renderer.backdropFor] has no image of it.
func (d *Renderer) backdropColor() color.NRGBA {
	if c := d.opts.Backdrop.Colors; len(c) > 0 {
		return icol.NRGBA(c[0])
	}
	if d.palette.Backdrop != nil {
		return icol.NRGBA(d.palette.Backdrop)
	}
	return color.NRGBA{}
}

// backdropFor returns the backdrop of an image of size, and the margin: the
// backdrop with the shadow over it, which is what shows outside the window.
// They are built on first use for a size, and kept until the size changes.
// backdrop is nil when the backdrop is [Renderer.backdropColor], and margin
// is nil when it is that too, with no shadow.
func (d *Renderer) backdropFor(size image.Point) (backdrop, margin *image.NRGBA) {
	// The shadow shows in the margin, and past the rounded corners.
	shadow := d.opts.Shadow.Color.A > 0 && (d.opts.Margin.Int() > 0 || d.opts.BorderRadius.Int() > 0)
	if d.backdropRamp == nil && d.opts.Backdrop.Image == nil && !shadow {
		return nil, nil
	}
	if d.margin != nil && d.margin.Rect.Size() == size {
		return d.backdrop, d.margin
	}
	d.window = nil
	rect := image.Rectangle{Max: size}
	switch {
	case d.opts.Backdrop.Image != nil:
		backdrop = coverImage(d.opts.Backdrop.Image, rect)
	case d.backdropRamp != nil:
		backdrop = image.NewNRGBA(rect)
		g := newGradient(d.backdropRamp, d.opts.Backdrop.Angle, rect)
		for y := range size.Y {
			for x := range size.X {
				backdrop.SetNRGBA(x, y, g.at(x, y))
			}
		}
	}
	margin = backdrop
	if shadow {
		margin = image.NewNRGBA(rect)
		if backdrop != nil {
			copy(margin.Pix, backdrop.Pix)
		} else {
			idraw.Fill(margin, rect, d.backdropColor())
		}
		drawShadow(margin, rect.Inset(d.opts.Margin.Int()), d.opts.BorderRadius.Int(), d.opts.Shadow)
	}
	d.backdrop, d.margin = backdrop, margin
	d.backdropOpaque = backdrop != nil && backdrop.Opaque()
	return backdrop, margin
}

// coverImage returns img scaled to cover r and centered on it.
func coverImage(img image.Image, r image.Rectangle) *image.NRGBA {
	src := img.Bounds()
	scale := max(float64(r.Dx())/float64(src.Dx()), float64(r.Dy())/float64(src.Dy()))
	// The center of src goes to the center of r.
	ox := float64(r.Min.X) + (float64(r.Dx())-float64(src.Dx())*scale)/2 - float64(src.Min.X)*scale
	oy := float64(r.Min.Y) + (float64(r.Dy())-float64(src.Dy())*scale)/2 - float64(src.Min.Y)*scale
	// x/image/draw is fast into premultiplied RGBA, so the scaled image is
	// made there and converted once. Transform samples only the pixels of
	// r, where Scale would make all of the scaled image, cut off or not,
	// with scratch space of its width by the height of img.
	scaled := image.NewRGBA(r)
	xdraw.CatmullRom.Transform(scaled, f64.Aff3{scale, 0, ox, 0, scale, oy}, img, src, xdraw.Src, nil)
	out := image.NewNRGBA(r)
	for i := 0; i < len(out.Pix); i += 4 {
		s := scaled.Pix[i : i+4 : i+4]
		switch a := s[3]; a {
		case 0:
		case 0xff:
			copy(out.Pix[i:i+4], s)
		default:
			for c := range 3 {
				// The filter may leave a channel above alpha.
				out.Pix[i+c] = uint8(min(0xff, (uint32(s[c])*0xff+uint32(a)/2)/uint32(a)))
			}
			out.Pix[i+3] = a
		}
	}
	return out
}

// windowFill returns what a window of background bg is painted with: bg,
// or, where bg is translucent and the backdrop opaque, bg blended over the
// backdrop, which is one color when the backdrop is, and otherwise img, an
// image the size of the image f draws, at its origin. img is built on first
// use for each size, and again in its place when the background changes, so
// a frame copies it instead of blending every pixel of the window.
// [underlay] then blends only what is drawn translucent over it. Over a
// backdrop that is not opaque, the window is bg, and [underlay] blends all
// of it, since a pixel blended over the backdrop would be translucent still,
// and could not be told apart.
func (d *Renderer) windowFill(f *renderFrame, bg color.NRGBA) (img *image.NRGBA, fill color.NRGBA) {
	if bg.A == 0xff || f.backdrop == nil && f.backdropColor.A != 0xff || f.backdrop != nil && !d.backdropOpaque {
		return nil, bg
	}
	if f.backdrop == nil {
		px := []byte{bg.R, bg.G, bg.B, bg.A}
		idraw.UnderPixel(px, f.backdropColor)
		return nil, color.NRGBA{R: px[0], G: px[1], B: px[2], A: px[3]}
	}
	if bg.A == 0 {
		return f.backdrop, bg
	}
	if d.window != nil && d.windowBg == bg {
		return d.window, bg
	}
	if d.window == nil {
		d.window = image.NewNRGBA(f.backdrop.Rect)
	}
	img = d.window
	win := f.windowBounds.Sub(f.imageBounds.Min)
	for y := win.Min.Y; y < win.Max.Y; y++ {
		px := img.Pix[img.PixOffset(win.Min.X, y):img.PixOffset(win.Max.X, y)]
		copy(px, f.backdrop.Pix[f.backdrop.PixOffset(win.Min.X, y):])
		for i := 0; i < len(px); i += 4 {
			idraw.OverPixel(px[i:i+4:i+4], bg, 0xff)
		}
	}
	d.windowBg = bg
	return img, bg
}

// copyRect copies r of src, whose origin is at origin of dst, into dst.
func copyRect(dst *image.NRGBA, r image.Rectangle, src *image.NRGBA, origin image.Point) {
	r = r.Intersect(dst.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		copy(dst.Pix[dst.PixOffset(r.Min.X, y):dst.PixOffset(r.Max.X, y)],
			src.Pix[src.PixOffset(r.Min.X-origin.X, y-origin.Y):])
	}
}

// Underlay blends the pixels of dst within r over the backdrop
// ([WithBackdrop]) of an image the size of dst, as [Renderer.Draw] blends
// the window's over it. It is for an effect that draws translucent pixels
// of its own into the window of an image the renderer drew, such as cells
// drawn again larger by another renderer with a transparent backdrop, which
// would otherwise show nothing behind them. r must hold only pixels that
// were not blended over the backdrop yet, unlike the rest of a frame the
// renderer drew. Opaque pixels are left as they are.
func (d *Renderer) Underlay(dst *image.NRGBA, r image.Rectangle) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	if dst == nil {
		panic("render: nil destination image")
	}
	backdrop, _ := d.backdropFor(dst.Rect.Size())
	underlay(dst, r, dst.Rect.Min, backdrop, d.backdropColor())
}

// SeeThrough reports whether the window can show the backdrop through it:
// whether it can be drawn translucent, by [WithBackgroundOpacity] or a
// translucent color of the palette, over a backdrop that is not
// transparent. An effect that moves or scales the window's pixels must then
// draw them without the backdrop, such as with a renderer of a transparent
// one, and [Renderer.Underlay] them where they land, or the backdrop moves
// with them.
func (d *Renderer) SeeThrough() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.translucent && (d.opts.Backdrop.Image != nil || d.backdropRamp != nil || d.backdropColor().A > 0)
}

// paletteTranslucent reports whether a color of p that the window is drawn
// with is translucent.
func paletteTranslucent(p Palette) bool {
	colors := []color.Color{
		p.DefaultForeground, p.DefaultBackground, p.Cursor, p.CursorText,
		p.Progress, p.ProgressError, p.ProgressPause,
	}
	for _, c := range p.Indexed {
		colors = append(colors, c)
	}
	return slices.ContainsFunc(colors, func(c color.Color) bool { return c != nil && icol.NRGBA(c).A < 0xff })
}

// underlay blends the translucent pixels of dst within r over the backdrop
// of an image at origin: backdrop, or one color, fill, when it is nil.
// Blending is associative, so doing this once, after the window is drawn,
// matches blending each fill, glyph, and overlay in it over the backdrop.
func underlay(dst *image.NRGBA, r image.Rectangle, origin image.Point, backdrop *image.NRGBA, fill color.NRGBA) {
	r = r.Intersect(dst.Rect)
	if r.Empty() || backdrop == nil && fill.A == 0 {
		return
	}
	// The alpha bytes of four pixels, read as two little-endian words.
	const alphas = 0xff000000_ff000000
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := dst.Pix[dst.PixOffset(r.Min.X, y):][:4*r.Dx()]
		for i := 0; i < len(row); i += 4 {
			// Most of a window is opaque by now, so four pixels are
			// skipped at a time while they are.
			for i+16 <= len(row) && binary.LittleEndian.Uint64(row[i:])&binary.LittleEndian.Uint64(row[i+8:])&alphas == alphas {
				i += 16
			}
			if i == len(row) || row[i+3] == 0xff {
				continue
			}
			under := fill
			if backdrop != nil {
				under = backdrop.NRGBAAt(r.Min.X+i/4-origin.X, y-origin.Y)
			}
			idraw.UnderPixel(row[i:i+4:i+4], under)
		}
	}
}
