// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"cmp"
	"image"
	"maps"
	"slices"
	"time"

	"getsteep.dev/steep/reel/vt/internal/wasmvt"
)

// DefaultKittyStorage is how many bytes of decoded Kitty graphics a terminal
// keeps unless [WithKittyStorage] or [Terminal.SetKittyImageStorageLimit] says
// otherwise.
const DefaultKittyStorage = 32 << 20

// Kitty graphics are libghostty-vt's own, through ghostty_kitty_graphics_*:
// the module is built with them on (see vt/build/kitty-freestanding.patch).
// Only direct payloads are accepted, since there is no filesystem. PNG
// payloads are decoded by [Terminal.onDecodePNG].

// kittyPlaceholder is the Unicode placeholder character of virtual
// placements.
const kittyPlaceholder = '\U0010EEEE'

// Image is the pixels of one Kitty graphics image, as last decoded. Images
// are shared between buffers and must not be modified.
type Image struct {
	// ID is the protocol image id.
	ID uint32
	// Generation changes whenever the pixels do, such as when the image is
	// sent again or an animation moves to its next frame. It is never zero.
	Generation uint64
	// Pixels is the decoded image, starting at (0, 0).
	Pixels *image.NRGBA
}

// Placement is one Kitty graphics placement in the viewport, or for a
// Unicode placeholder, one run of placeholder cells. Coordinates are in the
// terminal's pixels (see [WithCellSize]), from the top-left corner of the
// viewport; a renderer with another cell size scales them.
type Placement struct {
	// Image is what the placement draws.
	Image *Image
	// PlacementID is the protocol placement id. Zero means none was given.
	PlacementID uint32
	// Z is the layer: see [Placement.BelowBackground] and
	// [Placement.BelowText]. Higher layers draw over lower ones.
	Z int32
	// Rect is where the placement is drawn. It may reach outside the
	// viewport.
	Rect image.Rectangle
	// Source is the part of Image.Pixels drawn into Rect.
	Source image.Rectangle
	// Clip, if not empty, is the only part of Rect drawn: the cells of a
	// run of Unicode placeholders.
	Clip image.Rectangle
}

// BelowBackground reports whether p is drawn under the cell backgrounds,
// with text above it: Kitty's z below -1073741824.
func (p Placement) BelowBackground() bool { return p.Z < -(1 << 30) }

// BelowText reports whether p is drawn over the cell backgrounds but under
// the text: a negative z that is not [Placement.BelowBackground].
func (p Placement) BelowText() bool { return p.Z < 0 && !p.BelowBackground() }

// kittyScratchSize is the size of [kittyState.scratch].
const kittyScratchSize = 64

// kittyState is a terminal's Kitty graphics state.
type kittyState struct {
	// limit is the storage limit last applied. Zero turns Kitty graphics
	// off. def is the one a full reset restores.
	limit, def uint64
	// scratch is scratch for Kitty graphics reads, apart from the
	// terminal's, whose offsets the grid readers reserve. iter is the
	// reusable GhosttyKittyGraphicsPlacementIterator over the storage, for
	// redraws, and images the GhosttyRenderStateImageIterator over the
	// render state's list, for drawing. They live as long as the instance.
	scratch, iter, images int32
	// cache holds decoded images by id, while their generation holds.
	cache map[uint32]*Image
	// animDue is when the next animation frame is due, if animOK.
	animDue time.Duration
	animOK  bool
}

// reset forgets what a full reset clears, and restores the default limit.
func (k *kittyState) reset() {
	*k = kittyState{limit: k.def, def: k.def, scratch: k.scratch, iter: k.iter, images: k.images}
}

// SetKittyImageStorageLimit sets how many bytes of decoded image data Kitty
// graphics may keep. Zero turns Kitty graphics off and deletes every image.
// A full reset restores the limit [WithKittyStorage] set.
func (t *Terminal) SetKittyImageStorageLimit(n uint64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	t.kitty.limit = n
	t.gridFresh = false
	return t.mod.SetU64Option(t.term, wasmvt.TerminalOptionKittyImageStorageLimit, n)
}

// Images returns the placements in the viewport, lowest layer first. The
// slice and images are shared and must not be modified.
func (t *Terminal) Images() []Placement {
	t.mu.Lock()
	defer t.mu.Unlock()
	grid := t.screenLocked()
	if grid == nil {
		return nil
	}
	return grid.images
}

// AnimationDue reports when the next Kitty animation frame is due, on the
// terminal's clock (see [WithClock]). ok is false when no animation is
// running. A read of the screen or state at or after that time shows the
// new frame.
func (t *Terminal) AnimationDue() (at time.Duration, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.gridFresh {
		t.screenLocked()
	}
	return t.kitty.animDue, t.kitty.animOK
}

// Animate moves running Kitty animations to the frame due at the clock's
// current time (see [WithClock]). Reads of the screen and state do this
// too, unless the screen is unchanged since the last read; a host that
// moves the clock without writing calls Animate when [Terminal.AnimationDue]
// says a frame is due.
func (t *Terminal) Animate() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return
	}
	t.gridFresh = false
	t.screenLocked()
}

// tickAnimationsLocked moves running animations to the frame due at the
// clock's time, and records when the next one is. It runs before the render
// state's update, so the update sees the new frames.
func (t *Terminal) tickAnimationsLocked() {
	// Nothing plays until an image is stored.
	g, gen, ok := t.kittyGraphicsLocked()
	if !ok || gen == 0 {
		t.kitty.animOK = false
		return
	}
	now := t.clock()
	ms := int64(now / time.Millisecond)
	out := t.kitty.scratch
	t.mod.Zero(out, 8)
	// next_ms is the clock time of the next frame change, not a delay.
	if t.mod.Xghostty_kitty_graphics_animation_tick(g, ms, out) != 0 {
		t.kitty.animOK = false
		return
	}
	t.kitty.animDue = time.Duration(t.mod.U64(out, 0)) * time.Millisecond //nolint:gosec // times are milliseconds.
	t.kitty.animOK = true
}

// kittyGraphicsLocked returns the active screen's GhosttyKittyGraphics
// handle and its storage generation, which is zero until an image is stored.
// ok is false when Kitty graphics are off.
func (t *Terminal) kittyGraphicsLocked() (g int32, gen uint64, ok bool) {
	if t.kitty.limit == 0 {
		return 0, 0, false
	}
	t.mod.Zero(t.kitty.scratch, 8)
	if t.mod.Get(t.term, wasmvt.TerminalDataKittyGraphics, t.kitty.scratch) != nil {
		return 0, 0, false
	}
	g = t.mod.I32(t.kitty.scratch, 0)
	if g == 0 || t.mod.Xghostty_kitty_graphics_get(g, wasmvt.KittyGraphicsDataGeneration, t.kitty.scratch) != 0 {
		return 0, 0, false
	}
	return g, t.mod.U64(t.kitty.scratch, 0), true
}

// kittyEntry is one placement in the image storage, as read through the
// placement iterator.
type kittyEntry struct {
	image     *Image
	placement uint32
	z         int32
	virtual   bool
	// cols, rows, xoff, and yoff are as the program sent them: zero sizes
	// are taken from the image.
	cols, rows, xoff, yoff int
	// The rest is libghostty's geometry for a placement that is not virtual.
	// col and row are the viewport cell of its top-left corner, and may be
	// negative.
	col, row, gridRows int
	height             int
	source             image.Rectangle
}

// kittyPlacementsLocked reads every placement in g's storage whose image has
// arrived, as the program sent them, for a redraw. Drawing uses the render
// state's list instead (see refreshImagesLocked).
func (t *Terminal) kittyPlacementsLocked(g int32) []kittyEntry {
	t.mod.SetI32(t.kitty.scratch, 0, t.kitty.iter)
	if t.mod.Xghostty_kitty_graphics_get(g, wasmvt.KittyGraphicsDataPlacementIterator, t.kitty.scratch) != 0 {
		return nil
	}
	var out []kittyEntry
	for t.mod.Xghostty_kitty_graphics_placement_next(t.kitty.iter) != 0 {
		id := t.placementU32(wasmvt.KittyGraphicsPlacementDataImageId)
		h := t.mod.Xghostty_kitty_graphics_image(g, wasmvt.AsI32(id))
		img := t.kittyImageLocked(h, id)
		if img == nil {
			continue
		}
		e := kittyEntry{
			image:     img,
			placement: t.placementU32(wasmvt.KittyGraphicsPlacementDataPlacementId),
			z:         wasmvt.AsI32(t.placementU32(wasmvt.KittyGraphicsPlacementDataZ)),
			virtual:   t.placementU32(wasmvt.KittyGraphicsPlacementDataIsVirtual)&0xff != 0,
			cols:      int(t.placementU32(wasmvt.KittyGraphicsPlacementDataColumns)),
			rows:      int(t.placementU32(wasmvt.KittyGraphicsPlacementDataRows)),
			xoff:      int(t.placementU32(wasmvt.KittyGraphicsPlacementDataXOffset)),
			yoff:      int(t.placementU32(wasmvt.KittyGraphicsPlacementDataYOffset)),
		}
		if !e.virtual {
			t.readRenderInfoLocked(h, &e)
		}
		out = append(out, e)
	}
	return out
}

// readRenderInfoLocked fills e's geometry from the iterator's placement of
// the image whose handle is h.
func (t *Terminal) readRenderInfoLocked(h int32, e *kittyEntry) {
	info := t.kitty.scratch
	n := wasmvt.SizeKittyGraphicsPlacementRenderInfo
	t.mod.Zero(info, n)
	t.mod.SetU32(info, wasmvt.KittyGraphicsPlacementRenderInfoSizeOff, uint32(n))
	if t.mod.Xghostty_kitty_graphics_placement_render_info(t.kitty.iter, h, t.term, info) != 0 {
		return
	}
	u := func(off int) int { return int(t.mod.U32(info, off)) }
	e.col = int(t.mod.I32(info, wasmvt.KittyGraphicsPlacementRenderInfoViewportColOff))
	e.row = int(t.mod.I32(info, wasmvt.KittyGraphicsPlacementRenderInfoViewportRowOff))
	e.gridRows = u(wasmvt.KittyGraphicsPlacementRenderInfoGridRowsOff)
	e.height = u(wasmvt.KittyGraphicsPlacementRenderInfoPixelHeightOff)
	sx, sy := u(wasmvt.KittyGraphicsPlacementRenderInfoSourceXOff), u(wasmvt.KittyGraphicsPlacementRenderInfoSourceYOff)
	e.source = image.Rect(sx, sy,
		sx+u(wasmvt.KittyGraphicsPlacementRenderInfoSourceWidthOff),
		sy+u(wasmvt.KittyGraphicsPlacementRenderInfoSourceHeightOff))
}

// refreshImagesLocked updates the images of t.grid from the render state's
// image list, after an update. libghostty-vt resolves every placement in it:
// those at a cell, those relative to another placement, and each row of
// Unicode placeholder cells. The list is read again only when its generation
// changes, which it also does when an image's pixels change, such as on an
// animation frame, and for a new grid. It reports whether the placements
// changed.
func (t *Terminal) refreshImagesLocked() bool {
	g := t.grid
	if g == nil {
		return false
	}
	ok := t.kitty.limit != 0
	if ok {
		t.mod.Zero(t.kitty.scratch, 8)
		ok = t.mod.Xghostty_render_state_get(t.render, wasmvt.RenderStateDataImagePlacementsGeneration, t.kitty.scratch) == 0
	}
	if !ok {
		g.imagesRead = false
		return t.setImagesLocked(nil)
	}
	gen := t.mod.U64(t.kitty.scratch, 0)
	if g.imagesRead && gen == g.imagesGen {
		return false
	}
	g.imagesGen, g.imagesRead = gen, true
	return t.setImagesLocked(t.imageListLocked())
}

// imageListLocked reads the render state's image list, lowest layer first,
// and drops decoded images it does not use from the cache.
func (t *Terminal) imageListLocked() []Placement {
	g, _, ok := t.kittyGraphicsLocked()
	if !ok {
		return nil
	}
	it := t.kitty.images
	t.mod.SetI32(t.kitty.scratch, 0, it)
	if t.mod.Xghostty_render_state_get(t.render, wasmvt.RenderStateDataImageIterator, t.kitty.scratch) != 0 {
		return nil
	}
	type entry struct {
		p     Placement
		layer int32
	}
	var list []entry
	used := make(map[uint32]bool, len(t.kitty.cache))
	cw, ch := int(t.cellW), int(t.cellH)
	for t.mod.Xghostty_render_state_image_iterator_next(it) != 0 {
		// Other protocols may come in later versions.
		if t.imageI32(wasmvt.RenderStateImageDataProtocol) != wasmvt.ImageProtocolKitty {
			continue
		}
		id := t.imageU32(wasmvt.RenderStateImageDataImageId)
		img := t.kittyImageLocked(t.mod.Xghostty_kitty_graphics_image(g, wasmvt.AsI32(id)), id)
		if img == nil {
			continue
		}
		used[id] = true
		col, row := int(t.imageI32(wasmvt.RenderStateImageDataViewportCol)), int(t.imageI32(wasmvt.RenderStateImageDataViewportRow))
		x := col*cw + int(t.imageU32(wasmvt.RenderStateImageDataCellOffsetX))
		y := row*ch + int(t.imageU32(wasmvt.RenderStateImageDataCellOffsetY))
		sx, sy := int(t.imageU32(wasmvt.RenderStateImageDataSourceX)), int(t.imageU32(wasmvt.RenderStateImageDataSourceY))
		e := entry{layer: t.imageI32(wasmvt.RenderStateImageDataLayer), p: Placement{
			Image:       img,
			PlacementID: t.imageU32(wasmvt.RenderStateImageDataKittyPlacementId),
			Z:           t.imageI32(wasmvt.RenderStateImageDataZ),
			Rect: image.Rect(x, y,
				x+int(t.imageU32(wasmvt.RenderStateImageDataPixelWidth)),
				y+int(t.imageU32(wasmvt.RenderStateImageDataPixelHeight))),
			Source: fitSource(image.Rect(sx, sy,
				sx+int(t.imageU32(wasmvt.RenderStateImageDataSourceWidth)),
				sy+int(t.imageU32(wasmvt.RenderStateImageDataSourceHeight))), img.Pixels.Rect),
		}}
		// A run of placeholder cells is clipped to its cells.
		if t.imageI32(wasmvt.RenderStateImageDataKittyKind) == wasmvt.RenderStateKittyPlacementKindVirtual {
			cols, rows := int(t.imageU32(wasmvt.RenderStateImageDataGridCols)), int(t.imageU32(wasmvt.RenderStateImageDataGridRows))
			e.p.Clip = image.Rect(col*cw, row*ch, (col+cols)*cw, (row+rows)*ch)
		}
		list = append(list, e)
	}
	maps.DeleteFunc(t.kitty.cache, func(id uint32, _ *Image) bool { return !used[id] })
	// The list is not in drawing order.
	slices.SortStableFunc(list, func(a, b entry) int {
		return cmp.Or(cmp.Compare(a.layer, b.layer), cmp.Compare(a.p.Z, b.p.Z))
	})
	placed := make([]Placement, len(list))
	for i, e := range list {
		placed[i] = e.p
	}
	return placed
}

// fitSource moves src inside bounds without changing its size, unless it is
// larger, and makes it at least one pixel each way. libghostty-vt rounds the
// part of an image each row of placeholder cells shows to whole image pixels,
// so rows of an image only a few pixels tall can reach past it, or round to
// nothing when it is drawn larger than it is, and would draw nothing.
func fitSource(src, bounds image.Rectangle) image.Rectangle {
	if src.Dx() == 0 {
		src.Max.X++
	}
	if src.Dy() == 0 {
		src.Max.Y++
	}
	shift := image.Point{}
	if over := src.Max.X - bounds.Max.X; over > 0 {
		shift.X = -min(over, src.Min.X-bounds.Min.X)
	}
	if over := src.Max.Y - bounds.Max.Y; over > 0 {
		shift.Y = -min(over, src.Min.Y-bounds.Min.Y)
	}
	return src.Add(shift).Intersect(bounds)
}

// imageU32 reads 4 bytes of data for the image iterator's placement.
func (t *Terminal) imageU32(data int32) uint32 {
	t.mod.Zero(t.kitty.scratch, 4)
	if t.mod.Xghostty_render_state_image_get(t.kitty.images, data, t.kitty.scratch) != 0 {
		return 0
	}
	return t.mod.U32(t.kitty.scratch, 0)
}

func (t *Terminal) imageI32(data int32) int32 { return wasmvt.AsI32(t.imageU32(data)) }

// setImagesLocked replaces t.grid's placements with p, and reports whether
// they differ.
func (t *Terminal) setImagesLocked(p []Placement) bool {
	if t.grid == nil || slices.Equal(t.grid.images, p) {
		return false
	}
	t.grid.images = p
	return true
}

// placementU32 reads 4 bytes of data for the iterator's placement. Bools
// are their low byte.
func (t *Terminal) placementU32(data int32) uint32 {
	t.mod.Zero(t.kitty.scratch, 4)
	if t.mod.Xghostty_kitty_graphics_placement_get(t.kitty.iter, data, t.kitty.scratch) != 0 {
		return 0
	}
	return t.mod.U32(t.kitty.scratch, 0)
}

// kittyImageLocked returns the decoded image of handle h, from the cache
// while its generation is unchanged. It is nil for a zero handle, and while
// the image's data is still arriving.
func (t *Terminal) kittyImageLocked(h int32, id uint32) *Image {
	if h == 0 {
		return nil
	}
	s := t.kitty.scratch
	// get reads data into the scratch, zeroed for a failed read.
	get := func(data int32) {
		t.mod.Zero(s, 8)
		if t.mod.Xghostty_kitty_graphics_image_get(h, data, s) != 0 {
			t.mod.Zero(s, 8)
		}
	}
	get(wasmvt.KittyGraphicsImageDataGeneration)
	gen := t.mod.U64(s, 0)
	if gen == 0 {
		return nil
	}
	if img := t.kitty.cache[id]; img != nil && img.Generation == gen {
		return img
	}
	read := func(data int32) int32 { get(data); return t.mod.I32(s, 0) }
	w, ht := int(read(wasmvt.KittyGraphicsImageDataWidth)), int(read(wasmvt.KittyGraphicsImageDataHeight))
	format := read(wasmvt.KittyGraphicsImageDataFormat)
	// The pixels are a GhosttyString, NO_VALUE until they have all arrived.
	get(wasmvt.KittyGraphicsImageDataPixels)
	ptr := t.mod.I32(s, wasmvt.StringPtrOff)
	n := int(t.mod.U32(s, wasmvt.StringLenOff))
	if ptr == 0 || w <= 0 || ht <= 0 {
		return nil
	}
	pix := decodeKittyPixels(t.mod.Slice(ptr, n), format, w, ht)
	if pix == nil {
		return nil
	}
	img := &Image{ID: id, Generation: gen, Pixels: pix}
	if t.kitty.cache == nil {
		t.kitty.cache = map[uint32]*Image{}
	}
	t.kitty.cache[id] = img
	return img
}
