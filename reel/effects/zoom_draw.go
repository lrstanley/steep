// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image"

	"getsteep.dev/steep/reel"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

// drawZoom returns a copy of f's image with its grid zoomed l of the way to
// t. cells is t's crop drawn by the cell renderer, or nil to scale the
// frame's own pixels. On the way, the grid is scaled from the frame, or from
// flat, the grid drawn without the backdrop, when it is not nil, and the
// cells scaled down over the part they cover, so the image moves smoothly
// into the sharp one at 1.
func (s *zoomStage) drawZoom(f reel.Frame, t *zoomTarget, l float64, cells, flat *image.NRGBA) *image.NRGBA {
	out := cloneNRGBA(f.Image)
	grid := t.layout.Grid.Add(f.Image.Rect.Min)
	// Where the cells are drawn at full strength.
	at := grid.Min.Add(t.at)
	if l == 1 && cells != nil {
		// The cells were drawn without the backdrop; the rest of the grid
		// has it already.
		s.rend.Underlay(out, pasteNRGBA(out, grid, cells, at))
		return out
	}
	x, y := t.x, t.y
	if l < 1 {
		size := grid.Size()
		x, y = x.at(l, float64(size.X)), y.at(l, float64(size.Y))
	}
	if s.scratch == nil || s.scratch.Rect != grid {
		s.scratch = image.NewRGBA(grid)
	}
	// The grid is scaled from the frame, or from the grid drawn again
	// without the backdrop when the window shows it through, as the
	// backdrop does not move. With flat, the whole grid, cells too, is
	// blended over the backdrop once below.
	var src image.Image = f.Image
	from := grid
	if flat != nil {
		src, from = flat, flat.Rect
	}
	// The zoomed grid covers the whole of the scratch image, so nothing
	// drawn before shows through.
	gx, gy := float64(grid.Min.X), float64(grid.Min.Y)
	sx, sy := float64(from.Min.X), float64(from.Min.Y)
	xdraw.CatmullRom.Transform(s.scratch, f64.Aff3{
		x.k, 0, gx + x.off - x.k*sx,
		0, y.k, gy + y.off - y.k*sy,
	}, src, from, xdraw.Src, nil)
	if cells != nil {
		// A pixel of the cells at u is at at+u of the image at full
		// strength, which is the frame's grid pixel at+u-off over k.
		rx, ry := x.k/t.x.k, y.k/t.y.k
		m := f64.Aff3{
			rx, 0, gx + x.off + rx*(float64(at.X)-gx-t.x.off),
			0, ry, gy + y.off + ry*(float64(at.Y)-gy-t.y.off),
		}
		xdraw.CatmullRom.Transform(s.scratch, m, cells, cells.Rect, xdraw.Src, nil)
	}
	unpremultiply(out, s.scratch)
	if flat != nil {
		s.rend.Underlay(out, grid)
	}
	return out
}

// pasteNRGBA copies src into dst with its top left at at, clipped to clip,
// and returns the rectangle of dst it copied to.
func pasteNRGBA(dst *image.NRGBA, clip image.Rectangle, src *image.NRGBA, at image.Point) image.Rectangle {
	r := src.Rect.Sub(src.Rect.Min).Add(at).Intersect(clip).Intersect(dst.Rect)
	if r.Empty() {
		return r
	}
	sp := src.Rect.Min.Add(r.Min.Sub(at))
	n := r.Dx() * 4
	for y := range r.Dy() {
		d := dst.PixOffset(r.Min.X, r.Min.Y+y)
		s := src.PixOffset(sp.X, sp.Y+y)
		copy(dst.Pix[d:d+n], src.Pix[s:s+n])
	}
	return r
}

// unpremultiply copies src, premultiplied, into dst over src's bounds,
// which dst must hold.
func unpremultiply(dst *image.NRGBA, src *image.RGBA) {
	r := src.Rect
	n := r.Dx() * 4
	for y := r.Min.Y; y < r.Max.Y; y++ {
		s := src.Pix[src.PixOffset(r.Min.X, y):][:n]
		d := dst.Pix[dst.PixOffset(r.Min.X, y):][:n]
		for i := 0; i < n; i += 4 {
			switch a := s[i+3]; a {
			case 0xff:
				copy(d[i:i+4], s[i:i+4])
			case 0:
				d[i], d[i+1], d[i+2], d[i+3] = 0, 0, 0, 0
			default:
				for c := range 3 {
					// A filter's overshoot may leave a channel above alpha.
					d[i+c] = uint8(min(0xff, (uint32(s[i+c])*0xff+uint32(a)/2)/uint32(a)))
				}
				d[i+3] = a
			}
		}
	}
}
