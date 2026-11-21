// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/render"
	"getsteep.dev/steep/reel/vt"
	"github.com/charmbracelet/x/ansi"
)

// The colors of captions, besides the accent: near-white bold text on a
// near-black pill with a grey edge, or on a dark violet callout.
var (
	captionText  = color.NRGBA{R: 0xf4, G: 0xf4, B: 0xf6, A: 0xff}
	pillFill     = color.NRGBA{R: 0x16, G: 0x18, B: 0x20, A: 0xff}
	pillEdge     = color.NRGBA{R: 0x3a, G: 0x3e, B: 0x4b, A: 0xff}
	calloutFill  = color.NRGBA{R: 0x1c, G: 0x19, B: 0x2e, A: 0xff}
	captionShade = uint8(0x70) // The opacity of the shadow under the box.
)

const (
	// captionPillShare and captionCalloutShare are how much of the
	// terminal's width a line of a caption and of a callout may take.
	captionPillShare    = 0.7
	captionCalloutShare = 0.5
	// captionCalloutMin is how many columns a callout's lines may take on
	// a narrow terminal, where captionCalloutShare would leave fewer.
	captionCalloutMin = 24
	// spotlightKeep is how bright a [Spotlight] leaves what it darkens.
	spotlightKeep = 0.42
)

// captionLook is a caption laid out on a frame.
type captionLook struct {
	// img is the box, with its caret, text, and shadow, to draw at at.
	img *image.NRGBA
	at  image.Point
	// box is the box in the frame, without its caret or shadow.
	box image.Rectangle
	// pill is set for a caption drawn as a pill, a callout whose anchor
	// matched nothing included.
	pill bool
	// hole is what a spotlight leaves bright: the callout's anchor, padded.
	hole image.Rectangle
	// row is the height of a row of its text.
	row int
	// size is the terminal's size in cells it was laid out for.
	size image.Point
}

// layout lays out c on the frame of cf, keeping clear of the boxes of
// others, which show already. It returns nil when c's text has no room.
func (s *captionsStage) layout(c *shownCaption, cf *reel.CellFrame, others []*captionLook) *captionLook {
	b := cf.Screen.Bounds()
	lay := s.ctx.Rasterizer.Layout(b.Dx(), b.Dy())
	var look *captionLook
	if c.callout() {
		if anchor, ok := firstRect(c.anchor.Rects(cf.Screen), b); ok {
			look = s.callout(c, cf, lay, cellRect(lay.Grid, lay.Cell, anchor), others)
		}
	}
	if look == nil {
		look = s.pill(c, cf, lay, others)
	}
	if look != nil {
		look.size = b.Size()
	}
	return look
}

// firstRect is the first of rects in reading order, clipped to bounds.
func firstRect(rects []image.Rectangle, bounds image.Rectangle) (image.Rectangle, bool) {
	var first image.Rectangle
	found := false
	for _, r := range rects {
		r = r.Intersect(bounds)
		if r.Empty() {
			continue
		}
		if !found || r.Min.Y < first.Min.Y || (r.Min.Y == first.Min.Y && r.Min.X < first.Min.X) {
			first, found = r, true
		}
	}
	return first, found
}

// text returns c's lines as card text on bg, wrapped to cols columns, and
// the height of a row, or nil when none fits.
func (s *captionsStage) text(c *shownCaption, cf *reel.CellFrame, cols int, bg color.NRGBA) (text *image.NRGBA, row int) {
	gridCols, gridRows := s.cards.Grid(cf)
	cols = min(cols, gridCols)
	if cols < 1 || gridRows < 1 {
		return nil, 0
	}
	var lines []string
	for _, l := range c.lines {
		for w := range strings.SplitSeq(ansi.Wrap(l, cols, ""), "\n") {
			lines = append(lines, strings.TrimRight(w, " "))
		}
	}
	if limit := min(captionMaxLines, gridRows); len(lines) > limit {
		lines = lines[:limit]
		lines[limit-1] = ansi.Truncate(lines[limit-1], cols-1, "") + "…"
	}
	var width int
	for _, l := range lines {
		width = max(width, textWidth(l))
	}
	style := vt.Style{Fg: captionText, Bg: bg, Attrs: vt.AttrBold}
	cardLines := make([]Line, len(lines))
	for i, l := range lines {
		// Padded to the widest, so the lines line up on the left.
		cardLines[i] = Line{Text: l + strings.Repeat(" ", width-textWidth(l)), Style: style}
	}
	if text = s.cards.Text(cf, cardLines...); text == nil {
		return nil, 0
	}
	return text, text.Rect.Dy() / len(lines)
}

// lineCols is how many columns of card text a share of the terminal of cf
// is, at least least.
func (s *captionsStage) lineCols(cf *reel.CellFrame, share float64, least int) int {
	gridCols, _ := s.cards.Grid(cf)
	return max(least, int(float64(gridCols)*share))
}

// pill lays out c as a pill at the top or bottom of the grid.
func (s *captionsStage) pill(c *shownCaption, cf *reel.CellFrame, lay render.Layout, others []*captionLook) *captionLook {
	text, row := s.text(c, cf, s.lineCols(cf, captionPillShare, 1), pillFill)
	if text == nil {
		return nil
	}
	tb := text.Bounds()
	padX, padY := row*3/4, row*2/5
	dot, dotGap := max(4, row*2/5), row/2
	size := image.Pt(padX+dot+dotGap+tb.Dx()+padX, tb.Dy()+2*padY)
	inset := row / 2
	frame := lay.Image.Inset(captionEdge(row))
	x := (lay.Grid.Min.X + lay.Grid.Max.X - size.X) / 2
	x += slide(x, x+size.X, frame.Min.X, frame.Max.X)
	bottom := image.Rectangle{Min: image.Pt(x, lay.Grid.Max.Y-inset-size.Y), Max: image.Pt(x+size.X, lay.Grid.Max.Y-inset)}
	top := image.Rectangle{Min: image.Pt(x, lay.Grid.Min.Y+inset), Max: image.Pt(x+size.X, lay.Grid.Min.Y+inset+size.Y)}
	box := bottom
	switch c.side { //nolint:exhaustive // A caption is never on the left or right.
	case SideTop:
		box = top
	case SideBottom:
	default:
		if captionCost(cf, lay, top, others) < captionCost(cf, lay, bottom, others) {
			box = top
		}
	}
	radius := float64(min(size.Y, row+2*padY)) / 2
	look := s.shape(box, radius, nil, pillFill, pillEdge, row)
	look.pill = true
	in := box.Min.Sub(look.at)
	at := in.Add(image.Pt(padX+dot+dotGap, padY))
	draw.Draw(look.img, tb.Sub(tb.Min).Add(at), text, tb.Min, draw.Src)
	// The dot sits by the first line.
	fillDot(look.img, in.Add(image.Pt(padX+dot/2, padY+row/2)), float64(dot)/2, s.accent, pillFill)
	return look
}

// captionEdge is how far a caption keeps from the edges of the frame.
func captionEdge(row int) int { return max(2, row/4) }

// caret is the caret of a callout: on which side of the box, the point it
// points at, and the middle of its base, on that side of the box. The tip
// leans away from the middle of the base where the anchor is too near a
// corner of the box for the caret to point straight at it.
type caret struct {
	side      Side
	tip, base image.Point
	size      int
}

// callout lays out c as a callout pointing at anchor, a rectangle of
// pixels, or returns nil when it fits on no side of it.
func (s *captionsStage) callout(c *shownCaption, cf *reel.CellFrame, lay render.Layout, anchor image.Rectangle, others []*captionLook) *captionLook {
	text, row := s.text(c, cf, s.lineCols(cf, captionCalloutShare, captionCalloutMin), calloutFill)
	if text == nil {
		return nil
	}
	tb := text.Bounds()
	padX, padY := row*3/4, row*2/5
	size := image.Pt(tb.Dx()+2*padX, tb.Dy()+2*padY)
	radius := float64(row) * 0.4
	k := max(4, row*9/20) // The caret's height.
	gap := max(2, row/8)
	frame := lay.Image.Inset(captionEdge(row))
	// The caret keeps to the straight part of its side.
	reach := int(math.Ceil(radius)) + k + 1
	mid := image.Pt((anchor.Min.X+anchor.Max.X)/2, (anchor.Min.Y+anchor.Max.Y)/2)

	sides := []Side{SideBottom, SideTop, SideRight, SideLeft}
	if c.side != SideAuto {
		// Its own side first; the others only where it does not fit.
		sides = append([]Side{c.side}, sides...)
	}
	var bestBox image.Rectangle
	var bestCaret caret
	bestCost := 0
	for rank, side := range sides {
		var box image.Rectangle
		var tip image.Point
		switch side { //nolint:exhaustive // sides holds no SideAuto.
		case SideBottom:
			tip = image.Pt(mid.X, anchor.Max.Y+gap)
			box = image.Rect(mid.X-size.X/2, tip.Y+k, mid.X-size.X/2+size.X, tip.Y+k+size.Y)
		case SideTop:
			tip = image.Pt(mid.X, anchor.Min.Y-gap)
			box = image.Rect(mid.X-size.X/2, tip.Y-k-size.Y, mid.X-size.X/2+size.X, tip.Y-k)
		case SideRight:
			tip = image.Pt(anchor.Max.X+gap, mid.Y)
			box = image.Rect(tip.X+k, mid.Y-size.Y/2, tip.X+k+size.X, mid.Y-size.Y/2+size.Y)
		case SideLeft:
			tip = image.Pt(anchor.Min.X-gap, mid.Y)
			box = image.Rect(tip.X-k-size.X, mid.Y-size.Y/2, tip.X-k, mid.Y-size.Y/2+size.Y)
		}
		// The box moves along the side to stay in the frame, and the
		// caret's base along the box to stay off its corners. Its tip
		// points at the part of the anchor nearest the base, leaning at
		// most twice the caret's height.
		base := tip
		if side == SideTop || side == SideBottom {
			box = box.Add(image.Pt(slide(box.Min.X, box.Max.X, frame.Min.X, frame.Max.X), 0))
			base.X = min(max(tip.X, box.Min.X+reach), box.Max.X-reach)
			tip.X = min(max(base.X, anchor.Min.X+1), anchor.Max.X-1)
			if abs(tip.X-base.X) > 2*k {
				continue
			}
		} else {
			box = box.Add(image.Pt(0, slide(box.Min.Y, box.Max.Y, frame.Min.Y, frame.Max.Y)))
			base.Y = min(max(tip.Y, box.Min.Y+reach), box.Max.Y-reach)
			tip.Y = min(max(base.Y, anchor.Min.Y+1), anchor.Max.Y-1)
			if abs(tip.Y-base.Y) > 2*k {
				continue
			}
		}
		if !box.In(frame) {
			continue
		}
		// Its own side wins wherever it fits.
		cost := captionCost(cf, lay, box, others)*4 + rank
		if c.side != SideAuto && rank == 0 {
			cost = -1
		}
		if bestBox.Empty() || cost < bestCost {
			bestBox, bestCaret, bestCost = box, caret{side: side, tip: tip, base: base, size: k}, cost
		}
	}
	if bestBox.Empty() {
		return nil
	}
	look := s.shape(bestBox, radius, &bestCaret, calloutFill, s.accent, row)
	at := bestBox.Min.Sub(look.at).Add(image.Pt(padX, padY))
	draw.Draw(look.img, tb.Sub(tb.Min).Add(at), text, tb.Min, draw.Src)
	pad := max(1, lay.Cell.X/3)
	look.hole = anchor.Inset(-pad)
	return look
}

// slide is how far to move [lo, hi) to fit in [min, max), or 0 when it
// fits or is too large to.
func slide(lo, hi, minV, maxV int) int {
	switch {
	case hi-lo > maxV-minV:
		return 0
	case lo < minV:
		return minV - lo
	case hi > maxV:
		return maxV - hi
	}
	return 0
}

// captionCost is how much a caption's box r would cover: the cells with
// text under it, and far more for each of others it overlaps.
func captionCost(cf *reel.CellFrame, lay render.Layout, r image.Rectangle, others []*captionLook) int {
	cost := 0
	for _, o := range others {
		if o != nil && o.box.Overlaps(r) {
			cost += 1 << 20
		}
	}
	g := r.Intersect(lay.Grid)
	if g.Empty() {
		return cost
	}
	scr := cf.Screen
	x0, y0 := (g.Min.X-lay.Grid.Min.X)/lay.Cell.X, (g.Min.Y-lay.Grid.Min.Y)/lay.Cell.Y
	x1 := (g.Max.X - lay.Grid.Min.X + lay.Cell.X - 1) / lay.Cell.X
	y1 := (g.Max.Y - lay.Grid.Min.Y + lay.Cell.Y - 1) / lay.Cell.Y
	for y := y0; y < min(y1, scr.Height()); y++ {
		for x := x0; x < min(x1, scr.Width()); x++ {
			if c := scr.CellAt(x, y); c != nil && strings.TrimSpace(c.Content) != "" {
				cost++
			}
		}
	}
	return cost
}

// shape returns the look of a box, with a caret when cr is not nil, filled
// with fill inside an edge of color edge, over a soft shadow below it. The
// box and its caret are one shape, so the edge runs around both.
func (s *captionsStage) shape(box image.Rectangle, radius float64, cr *caret, fill, edge color.NRGBA, row int) *captionLook {
	blur, drop := float64(max(2, row/3)), float64(max(1, row/6))
	edgeW := max(1, float64(row)/18)
	ext := box.Inset(-int(blur) - 2)
	ext.Max.Y += int(drop)
	var t0, t1, t2 vec
	if cr != nil {
		ext = ext.Union(image.Rectangle{Min: cr.tip, Max: cr.tip.Add(image.Pt(1, 1))}.Inset(-2))
		// The caret's base sits a pixel inside the box, so the two join.
		tip, b, k := vec{float64(cr.tip.X), float64(cr.tip.Y)}, vec{float64(cr.base.X), float64(cr.base.Y)}, float64(cr.size)
		switch cr.side { //nolint:exhaustive // A caret is never on SideAuto.
		case SideBottom: // Below its anchor: the caret points up.
			t0, t1, t2 = tip, vec{b.x - k, tip.y + k + 1}, vec{b.x + k, tip.y + k + 1}
		case SideTop:
			t0, t1, t2 = tip, vec{b.x + k, tip.y - k - 1}, vec{b.x - k, tip.y - k - 1}
		case SideRight:
			t0, t1, t2 = tip, vec{tip.x + k + 1, b.y + k}, vec{tip.x + k + 1, b.y - k}
		case SideLeft:
			t0, t1, t2 = tip, vec{tip.x - k - 1, b.y - k}, vec{tip.x - k - 1, b.y + k}
		}
	}
	halfW, halfH := float64(box.Dx())/2, float64(box.Dy())/2
	cx, cy := float64(box.Min.X)+halfW, float64(box.Min.Y)+halfH
	dist := func(x, y float64) float64 {
		d := roundedBox(x-cx, y-cy, halfW, halfH, radius)
		if cr != nil {
			d = min(d, triangleDist(vec{x, y}, t0, t1, t2))
		}
		return d
	}
	img := image.NewNRGBA(ext.Sub(ext.Min))
	for y := ext.Min.Y; y < ext.Max.Y; y++ {
		for x := ext.Min.X; x < ext.Max.X; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			d := dist(px, py)
			cov := clamp01(0.5 - d)
			var shade float64
			if cov < 1 {
				shade = clamp01(1 - max(0, dist(px, py-drop))/blur)
				shade *= shade
			}
			if cov <= 0 && shade <= 0 {
				continue
			}
			inner := clamp01(0.5 - (d + edgeW))
			c := edge
			if cov > 0 {
				c = mix(edge, fill, inner/cov)
			}
			img.Set(x-ext.Min.X, y-ext.Min.Y, over(c, cov, color.NRGBA{A: uint8(math.Round(shade * float64(captionShade)))}, 1))
		}
	}
	return &captionLook{img: img, at: ext.Min, box: box, row: row}
}

// mix is a blended toward b by t, from 0 to 1, opaque.
func mix(a, b color.NRGBA, t float64) color.NRGBA {
	ch := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{R: ch(a.R, b.R), G: ch(a.G, b.G), B: ch(a.B, b.B), A: 0xff}
}

// fillDot draws a dot of radius r centered on c, in col, over the opaque
// bg of img.
func fillDot(img *image.NRGBA, c image.Point, r float64, col, bg color.NRGBA) {
	cx, cy := float64(c.X), float64(c.Y)
	for y := int(cy - r - 1); y <= int(cy+r+1); y++ {
		for x := int(cx - r - 1); x <= int(cx+r+1); x++ {
			cov := clamp01(r + 0.5 - math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy))
			if cov > 0 && image.Pt(x, y).In(img.Rect) {
				img.SetNRGBA(x, y, mix(bg, col, cov))
			}
		}
	}
}

// spotlight darkens img, a copy of src, but for hole, and rings hole in
// accent.
func spotlight(img, src *image.NRGBA, hole image.Rectangle, accent color.NRGBA, row int) {
	dimNRGBA(img, spotlightKeep)
	hole = hole.Intersect(img.Rect)
	draw.Draw(img, hole, src, hole.Min, draw.Src)
	width := max(1.5, float64(row)/12)
	radius := float64(row) / 5
	halfW, halfH := float64(hole.Dx())/2, float64(hole.Dy())/2
	cx, cy := float64(hole.Min.X)+halfW, float64(hole.Min.Y)+halfH
	ring := hole.Inset(-int(math.Ceil(width)) - 2).Intersect(img.Rect)
	for y := ring.Min.Y; y < ring.Max.Y; y++ {
		for x := ring.Min.X; x < ring.Max.X; x++ {
			d := roundedBox(float64(x)+0.5-cx, float64(y)+0.5-cy, halfW, halfH, radius)
			// A band width wide, just outside hole.
			cov := clamp01(0.5-(d-width)) - clamp01(0.5-d)
			if cov > 0 {
				i := img.PixOffset(x, y)
				under := color.NRGBA{R: img.Pix[i], G: img.Pix[i+1], B: img.Pix[i+2], A: 0xff}
				img.SetNRGBA(x, y, mix(under, accent, cov))
			}
		}
	}
}

// dot is the dot product of v and w.
func (v vec) dot(w vec) float64 { return v.x*w.x + v.y*w.y }

// triangleDist is the signed distance from p to the triangle p0 p1 p2:
// negative inside.
func triangleDist(p, p0, p1, p2 vec) float64 {
	e0, e1, e2 := p1.sub(p0), p2.sub(p1), p0.sub(p2)
	v0, v1, v2 := p.sub(p0), p.sub(p1), p.sub(p2)
	pq0 := v0.sub(e0.scale(clamp01(v0.dot(e0) / e0.dot(e0))))
	pq1 := v1.sub(e1.scale(clamp01(v1.dot(e1) / e1.dot(e1))))
	pq2 := v2.sub(e2.scale(clamp01(v2.dot(e2) / e2.dot(e2))))
	s := math.Copysign(1, e0.x*e2.y-e0.y*e2.x)
	d := min(pq0.dot(pq0), pq1.dot(pq1), pq2.dot(pq2))
	inside := min(s*(v0.x*e0.y-v0.y*e0.x), s*(v1.x*e1.y-v1.y*e1.x), s*(v2.x*e2.y-v2.y*e2.x))
	return -math.Sqrt(d) * math.Copysign(1, inside)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
