// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image"

	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/region"
	"getsteep.dev/steep/reel/render"
	"getsteep.dev/steep/reel/vt"
)

// regionCells returns the cells r covers on f, as [region.Covered] finds
// them, as rectangles (see [region.Runs]).
func regionCells(r region.Region, f *reel.CellFrame) []image.Rectangle {
	b := f.Screen.Bounds()
	return region.Runs(region.Covered(f.Screen, r), b.Dx(), b.Dy())
}

// regionPixels finds the pixels of a region on terminal frames, matching
// each screen once.
type regionPixels struct {
	region region.Region
	rast   reel.Rasterizer

	layout render.Layout
	size   image.Point
	// screen is the screen rects were found on.
	screen *vt.Buffer
	rects  []image.Rectangle
}

// find returns the pixel rectangles of the cells the region covers on cf.
func (p *regionPixels) find(cf *reel.CellFrame) []image.Rectangle {
	if cf.Screen == p.screen {
		return p.rects
	}
	cells := regionCells(p.region, cf)
	if size := cf.Screen.Bounds().Size(); size != p.size {
		p.layout, p.size = p.rast.Layout(size.X, size.Y), size
	}
	rects := make([]image.Rectangle, 0, len(cells))
	for _, c := range cells {
		r := p.layout.CellRect(c.Min.X, c.Min.Y).Union(p.layout.CellRect(c.Max.X-1, c.Max.Y-1))
		if !r.Empty() {
			rects = append(rects, r)
		}
	}
	p.screen, p.rects = cf.Screen, rects
	return rects
}
