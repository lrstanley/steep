// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"math"

	xdraw "golang.org/x/image/draw"

	"getsteep.dev/steep/reel/vt"
)

// imageLayer is one of the three Kitty graphics layers, drawn in order.
type imageLayer uint8

const (
	// layerBelowBackground is drawn over the window background, under the
	// cell backgrounds.
	layerBelowBackground imageLayer = iota
	// layerBelowText is drawn over the cell backgrounds, under the text.
	layerBelowText
	// layerAboveText is drawn over the text, under the cursor.
	layerAboveText
)

func layerOf(p vt.Placement) imageLayer {
	switch {
	case p.BelowBackground():
		return layerBelowBackground
	case p.BelowText():
		return layerBelowText
	}
	return layerAboveText
}

// maxCacheBytes bounds the scaled copies [imageCache] keeps for a frame,
// so a recording with many large placements cannot make it gigabytes.
const maxCacheBytes = 128 << 20

// maxKernelScratch bounds the scratch [xdraw.BiLinear] takes to scale a
// source to a width, in 32-byte elements of that width times the source's
// height: 256 MiB, which a 4096 by 2048 source drawn the frame's width
// takes. A larger one is scaled with [xdraw.ApproxBiLinear], which takes
// none.
const maxKernelScratch = 8 << 20

// maxDrawnFrames bounds the pixels [Renderer.drawImages] draws for a frame,
// in frames' worth, so a recording with thousands of placements cannot make
// one frame take minutes.
const maxDrawnFrames = 16

// scaledKey identifies one scaled copy of part of an image.
type scaledKey struct {
	gen    uint64
	source image.Rectangle
	size   image.Point
}

// imageCache keeps the scaled copies the last frame drew, so a placement
// that did not change is not scaled again.
type imageCache struct {
	last, next map[scaledKey]*image.NRGBA
	// nextBytes is the size of next's copies, at most maxCacheBytes.
	nextBytes int
}

// get returns the source part of img scaled to size, or nil when keeping
// it would take this frame's copies past maxCacheBytes.
func (c *imageCache) get(img *vt.Image, source image.Rectangle, size image.Point) *image.NRGBA {
	key := scaledKey{gen: img.Generation, source: source, size: size}
	if c.next == nil {
		c.next = map[scaledKey]*image.NRGBA{}
	}
	if s := c.next[key]; s != nil {
		return s
	}
	// Divided, so a placement of any size cannot overflow it.
	if size.X > (maxCacheBytes-c.nextBytes)/4/size.Y {
		return nil
	}
	c.nextBytes += 4 * size.X * size.Y
	s := c.last[key]
	if s == nil {
		s = image.NewNRGBA(image.Rectangle{Max: size})
		var scaler xdraw.Scaler = xdraw.BiLinear
		if size.X*source.Dy() > maxKernelScratch {
			scaler = xdraw.ApproxBiLinear
		}
		scaler.Scale(s, s.Rect, img.Pixels, source, xdraw.Src, nil)
	}
	c.next[key] = s
	return s
}

// rotate keeps what this frame drew, for the next.
func (c *imageCache) rotate() {
	c.last, c.next, c.nextBytes = c.next, nil, 0
}

// scaleRect maps r, in the pixels of a terminal whose cells are cell, to a
// frame whose grid starts at origin, with cells of cw by ch pixels.
func scaleRect(r image.Rectangle, origin, cell image.Point, cw, ch int) image.Rectangle {
	sx, sy := float64(cw)/float64(cell.X), float64(ch)/float64(cell.Y)
	scale := func(v int, s float64) int { return int(math.Round(float64(v) * s)) }
	return image.Rect(
		scale(r.Min.X, sx), scale(r.Min.Y, sy),
		scale(r.Max.X, sx), scale(r.Max.Y, sy),
	).Add(origin)
}

// drawImages draws f's Kitty graphics placements in layer, scaled from the
// terminal's cells to the frame's and clipped to the grid.
func (d *Renderer) drawImages(dst *image.NRGBA, f *renderFrame, layer imageLayer) {
	clip := dst.Bounds().Intersect(f.gridBounds)
	if len(f.images) == 0 || clip.Empty() {
		return
	}
	// Of the grid, not the clip, so repainting a band draws what the frame did.
	budget := maxDrawnFrames * f.gridBounds.Dx() * f.gridBounds.Dy()
	for _, p := range f.images {
		if layerOf(p) != layer || p.Image == nil || p.Source.Empty() {
			continue
		}
		r := scaleRect(p.Rect, f.gridBounds.Min, f.imageCell, f.cellW, f.cellH)
		area := clip
		if !p.Clip.Empty() {
			area = area.Intersect(scaleRect(p.Clip, f.gridBounds.Min, f.imageCell, f.cellW, f.cellH))
		}
		if r.Empty() || !r.Overlaps(area) {
			continue
		}
		// A recording can make placements any size, and any number. One past
		// the budget is not drawn.
		vis := r.Intersect(area)
		if vis.Dx()*vis.Dy() > budget {
			continue
		}
		budget -= vis.Dx() * vis.Dy()
		sub, _ := dst.SubImage(area).(*image.NRGBA) // NRGBA.SubImage is always an NRGBA.
		scaled := d.images.get(p.Image, p.Source, r.Size())
		if scaled == nil {
			// One too large to keep a scaled copy of, such as one far larger
			// than the frame, is sampled straight into what shows of it, a
			// little more roughly.
			xdraw.ApproxBiLinear.Scale(sub, r, p.Image.Pixels, p.Source, xdraw.Over, nil)
			continue
		}
		xdraw.Draw(sub, r, scaled, image.Point{}, xdraw.Over)
	}
}
