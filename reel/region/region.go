// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package region selects cells of a terminal's screen: fixed cells
// ([Cells], [Row], [Rows]), the cells that show some text ([Text],
// [Regexp], [Env]), which follow it as it moves, or [Any] of them. The
// effects of package effects that work on part of the screen take a
// region, and so do the settle waits of package reel, to leave out part of
// the screen or watch only part of it.
package region

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"image"
	"iter"
	"os"
	"regexp"
	"slices"
	"strings"

	"getsteep.dev/steep/reel/vt"
)

// Region selects cells of a terminal's screen. Each screen is matched on
// its own, so a region that matches text follows it as it moves.
type Region interface {
	// Rects returns the cells the region covers on screen, as rectangles
	// of cell coordinates, where (0, 0) is the top left cell. They may
	// overlap, and may reach past the screen, which clips them.
	Rects(screen *vt.Buffer) []image.Rectangle
}

// Cells returns a region of the cells in r, in cell coordinates, where
// (0, 0) is the top left cell. The part of r outside the screen is left
// out.
func Cells(r image.Rectangle) Region { return cellsRegion(r.Canon()) }

// Row returns a region of row y of the screen, counted from 0 at the top,
// or from -1 at the bottom when y is negative, so Row(-1) is the last row.
func Row(y int) Region { return Rows(y, y+1) }

// Rows returns a region of the rows from y0 up to but not including y1,
// counted from 0 at the top, so Rows(y, y+1) is [Row](y). A negative row
// counts from the bottom, where -1 is the last row, and after a negative
// y0, a y1 of 0 is the bottom of the screen: Rows(-2, 0) is the last two
// rows, and Rows(1, -1) all but the first and last. Rows counted from the
// same end are taken in either order; a range from the top to the bottom
// that ends before it starts, as Rows(1, -1) does on a screen of one row,
// covers nothing.
func Rows(y0, y1 int) Region {
	if (y0 < 0) == (y1 < 0) && y1 < y0 {
		y0, y1 = y1, y0
	}
	return rowsRegion{y0, y1}
}

// Text returns a region of the cells that show s, every time it appears on
// a frame. Text is searched line by line, where a line is a row and the
// rows it soft-wrapped over ([vt.Buffer.Wrapped]), so s matches across a
// soft wrap but never across a line break. Text a program lays out over
// rows itself, as full-screen programs do, is on separate lines. Blank
// cells read as spaces. An empty s, or one that holds a newline, matches
// nothing. See [Regexp] for how a match maps to cells.
func Text(s string) Region { return textRegion(s) }

// Regexp returns a region of the cells re matches, every time it matches on
// a frame. re is matched against each line on its own, as [Text] reads
// lines, so ^ and $ match at the ends of a line. Empty matches select
// nothing.
//
// A match covers every cell it holds any byte of: part of a grapheme
// cluster covers its whole cell, and a wide character covers both of its
// columns.
func Regexp(re *regexp.Regexp) Region { return regexpRegion{re} }

// Env returns a region of the cells that show the values of the
// environment variables names, read when the region is matched, as [Text]
// matches them. Each line of a value matches on its own. An unset variable,
// or a line that is only whitespace, matches nothing.
func Env(names ...string) Region { return envRegion(slices.Clone(names)) }

// Any returns a region of the cells any of regions covers. Nil regions
// are left out.
func Any(regions ...Region) Region {
	return anyRegion(slices.DeleteFunc(slices.Clone(regions), func(r Region) bool { return r == nil }))
}

type cellsRegion image.Rectangle

func (r cellsRegion) Rects(*vt.Buffer) []image.Rectangle {
	return []image.Rectangle{image.Rectangle(r)}
}

type rowsRegion struct{ y0, y1 int }

func (r rowsRegion) Rects(screen *vt.Buffer) []image.Rectangle {
	b := screen.Bounds()
	h := b.Dy()
	y0, y1 := r.y0, r.y1
	if y1 < 0 || (y1 == 0 && y0 < 0) {
		y1 += h
	}
	if y0 < 0 {
		y0 += h
	}
	if y1 <= y0 {
		return nil
	}
	return []image.Rectangle{image.Rect(0, y0, b.Dx(), y1)}
}

type textRegion string

func (r textRegion) Rects(screen *vt.Buffer) []image.Rectangle {
	return findText(screen, []string{string(r)})
}

type regexpRegion struct{ re *regexp.Regexp }

func (r regexpRegion) Rects(screen *vt.Buffer) []image.Rectangle {
	if r.re == nil {
		return nil
	}
	var out []image.Rectangle
	for line := range Lines(screen) {
		for _, m := range r.re.FindAllStringIndex(line.Text, -1) {
			out = line.AppendRects(out, m[0], m[1])
		}
	}
	return out
}

type envRegion []string

func (r envRegion) Rects(screen *vt.Buffer) []image.Rectangle {
	return findText(screen, r.values())
}

// values returns the lines of the variables' values that are not only
// whitespace.
func (r envRegion) values() []string {
	var out []string
	for _, name := range r {
		for line := range strings.Lines(os.Getenv(name)) {
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if strings.TrimSpace(line) != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

type anyRegion []Region

func (r anyRegion) Rects(screen *vt.Buffer) []image.Rectangle {
	var out []image.Rectangle
	for _, region := range r {
		out = append(out, region.Rects(screen)...)
	}
	return out
}

// TextOf returns the text r looks for, for an effect that hides it off
// the screen too, such as in the window title: the literals of its [Text]
// and [Env] regions, with each variable's value read now, and the patterns
// of its [Regexp] regions, from r and any [Any] it holds. Literals that
// match nothing, such as empty ones, are left out. others are the regions
// that find no text, such as [Rows], which only cover cells.
func TextOf(r Region) (literals []string, patterns []*regexp.Regexp, others []Region) {
	var add func(Region)
	add = func(region Region) {
		switch r := region.(type) {
		case nil:
		case anyRegion:
			for _, sub := range r {
				add(sub)
			}
		case textRegion:
			literals = append(literals, string(r))
		case envRegion:
			literals = append(literals, r.values()...)
		case regexpRegion:
			if r.re != nil {
				patterns = append(patterns, r.re)
			}
		default:
			others = append(others, r)
		}
	}
	add(r)
	return textNeedles(literals), patterns, others
}

// findText returns the cells of every match of each of needles in the lines
// of screen (see [textNeedles]).
func findText(screen *vt.Buffer, needles []string) []image.Rectangle {
	needles = textNeedles(needles)
	if len(needles) == 0 {
		return nil
	}
	var out []image.Rectangle
	for line := range Lines(screen) {
		for _, s := range needles {
			for i := 0; ; {
				j := strings.Index(line.Text[i:], s)
				if j < 0 {
					break
				}
				out = line.AppendRects(out, i+j, i+j+len(s))
				i += j + len(s)
			}
		}
	}
	return out
}

// textNeedles returns needles without those that match nothing: empty
// ones, and those with a newline.
func textNeedles(needles []string) []string {
	return slices.DeleteFunc(needles, func(s string) bool {
		return s == "" || strings.ContainsAny(s, "\r\n")
	})
}

// Line is the text of one line of a screen, as [Text] and [Regexp] regions
// read it: a row, and the rows below it that it soft-wrapped over.
type Line struct {
	// Text is the line's text: each cell's grapheme cluster, or a space
	// for a blank cell. The spacer columns of wide characters add nothing.
	Text string

	// starts is the offset into Text of each cell's text, in order, and
	// cells is where each cell is: its first column and the column past
	// its last, on its row.
	starts []int
	cells  []lineCell
}

type lineCell struct{ x0, x1, y int }

// Lines returns an iterator over the lines of screen, top to bottom, for a
// [Region] of one's own that finds text.
//
// The line yielded is only valid until the next: its slices are reused.
func Lines(screen *vt.Buffer) iter.Seq[Line] {
	return func(yield func(Line) bool) {
		b := screen.Bounds()
		w := b.Dx()
		line := Line{starts: make([]int, 0, w), cells: make([]lineCell, 0, w)}
		text := make([]byte, 0, w)
		for y := range b.Dy() {
			for x := range w {
				c := screen.CellAt(b.Min.X+x, b.Min.Y+y)
				if c == nil || c.Width == 0 {
					continue
				}
				line.starts = append(line.starts, len(text))
				line.cells = append(line.cells, lineCell{x, min(x+max(c.Width, 1), w), y})
				if c.Content == "" {
					text = append(text, ' ')
				} else {
					text = append(text, c.Content...)
				}
			}
			if screen.Wrapped(b.Min.Y+y) && y < b.Dy()-1 {
				continue
			}
			line.Text = string(text)
			if !yield(line) {
				return
			}
			line.starts, line.cells, text = line.starts[:0], line.cells[:0], text[:0]
		}
	}
}

// Offset returns the offset into the line's text of the cell at column x
// of row y, or -1 when the line does not hold it.
func (l Line) Offset(x, y int) int {
	for i, c := range l.cells {
		if c.y == y && x >= c.x0 && x < c.x1 {
			return l.starts[i]
		}
	}
	return -1
}

// cellOf returns the index of the cell byte i of the line's text is in.
func (l Line) cellOf(i int) int {
	c, found := slices.BinarySearch(l.starts, i)
	if !found {
		c--
	}
	return c
}

// AppendRects appends the cells of the bytes from i up to j of the line's
// text to rects, a rectangle for each row they are on, and returns the
// result.
func (l Line) AppendRects(rects []image.Rectangle, i, j int) []image.Rectangle {
	if j <= i {
		return rects
	}
	cells := l.cells[l.cellOf(i) : l.cellOf(j-1)+1]
	for len(cells) > 0 {
		first := cells[0]
		n := 1
		for n < len(cells) && cells[n].y == first.y {
			n++
		}
		rects = append(rects, image.Rect(first.x0, first.y, cells[n-1].x1, first.y+1))
		cells = cells[n:]
	}
	return rects
}

// Covered returns which cells of screen r covers, clipped to the screen,
// and widened to the whole of each wide character it covers part of, as a
// grid of the screen's size, row after row, or nil when it covers none.
func Covered(screen *vt.Buffer, r Region) []bool {
	if r == nil {
		return nil
	}
	b := screen.Bounds()
	w, h := b.Dx(), b.Dy()
	grid := image.Rect(0, 0, w, h)
	var covered []bool
	for _, rect := range r.Rects(screen) {
		rect = rect.Canon().Intersect(grid)
		if rect.Empty() {
			continue
		}
		if covered == nil {
			covered = make([]bool, w*h)
		}
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				covered[y*w+x] = true
			}
		}
	}
	if covered != nil {
		widen(screen, covered)
	}
	return covered
}

// Runs returns covered, a w by h grid as [Covered] returns it, or nil, as
// rectangles: a run of covered cells on a row, grown down over the rows
// below that have the same run, in the order of their top left cells.
func Runs(covered []bool, w, h int) []image.Rectangle {
	if covered == nil {
		return nil
	}
	var out []image.Rectangle
	// open maps the runs of the row above, as their first and past-last
	// columns, to the rectangles that end on it.
	open := map[[2]int]int{}
	for y := range h {
		next := make(map[[2]int]int, len(open))
		for x := 0; x < w; {
			if !covered[y*w+x] {
				x++
				continue
			}
			x0 := x
			for x < w && covered[y*w+x] {
				x++
			}
			run := [2]int{x0, x}
			if i, ok := open[run]; ok {
				out[i].Max.Y = y + 1
				next[run] = i
				continue
			}
			next[run] = len(out)
			out = append(out, image.Rect(x0, y, x, y+1))
		}
		open = next
	}
	return out
}

// widen covers the whole of each wide character covered has part of: the
// spacer columns of a wide cell, and the wide cell of a spacer.
func widen(screen *vt.Buffer, covered []bool) {
	b := screen.Bounds()
	w := b.Dx()
	cell := func(x, y int) *vt.Cell { return screen.CellAt(b.Min.X+x, b.Min.Y+y) }
	for i, on := range covered {
		if !on {
			continue
		}
		x, y := i%w, i/w
		c := cell(x, y)
		if c == nil {
			continue
		}
		if c.Width > 1 {
			for k := 1; k < c.Width && x+k < w; k++ {
				covered[i+k] = true
			}
			continue
		}
		// A spacer belongs to the wide cell before it, past any other
		// spacers.
		for hx := x - 1; c.Width == 0 && hx >= 0; hx-- {
			hc := cell(hx, y)
			if hc == nil || hc.Width == 1 {
				break
			}
			covered[y*w+hx] = true
			if hc.Width > 1 {
				break
			}
		}
	}
}

// The types of regions in their JSON descriptions.
const (
	regionTypeCells  = "cells"
	regionTypeRows   = "rows"
	regionTypeText   = "text"
	regionTypeRegexp = "regexp"
	regionTypeEnv    = "env"
	regionTypeAny    = "any"
)

type cellsData struct {
	Type string `json:"type"`
	X0   int    `json:"x0"`
	Y0   int    `json:"y0"`
	X1   int    `json:"x1"`
	Y1   int    `json:"y1"`
}

func (d cellsData) region() (Region, error) { return Cells(image.Rect(d.X0, d.Y0, d.X1, d.Y1)), nil }

type rowsData struct {
	Type string `json:"type"`
	From int    `json:"from"`
	To   int    `json:"to"`
}

func (d rowsData) region() (Region, error) { return Rows(d.From, d.To), nil }

type textData struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (d textData) region() (Region, error) { return Text(d.Text), nil }

type regexpData struct {
	Type    string `json:"type"`
	Pattern string `json:"pattern"`
}

func (d regexpData) region() (Region, error) {
	re, err := regexp.Compile(d.Pattern)
	if err != nil {
		return nil, err
	}
	return Regexp(re), nil
}

type envData struct {
	Type  string   `json:"type"`
	Names []string `json:"names"`
}

func (d envData) region() (Region, error) { return Env(d.Names...), nil }

type anyData struct {
	Type    string           `json:"type"`
	Regions []jsontext.Value `json:"regions"`
}

func (d anyData) region() (Region, error) {
	regions := make([]Region, len(d.Regions))
	for i, sub := range d.Regions {
		var err error
		if regions[i], err = Unmarshal(sub); err != nil {
			return nil, err
		}
	}
	return Any(regions...), nil
}

// Marshal returns the JSON object that describes r, as reel's
// AnnotationSchema defines a region, which [Unmarshal] reads back.
// An [Env] region holds the names of its variables, never their values. A
// region of a caller's own, or [Any] of one, is an error wrapping
// [errors.ErrUnsupported].
func Marshal(r Region) (jsontext.Value, error) {
	var v any
	switch r := r.(type) {
	case nil:
		return nil, errors.New("region: none")
	case cellsRegion:
		v = cellsData{regionTypeCells, r.Min.X, r.Min.Y, r.Max.X, r.Max.Y}
	case rowsRegion:
		v = rowsData{regionTypeRows, r.y0, r.y1}
	case textRegion:
		v = textData{regionTypeText, string(r)}
	case regexpRegion:
		if r.re == nil {
			return nil, errors.New("region: regexp region without a regexp")
		}
		v = regexpData{regionTypeRegexp, r.re.String()}
	case envRegion:
		v = envData{regionTypeEnv, r}
	case anyRegion:
		d := anyData{Type: regionTypeAny, Regions: make([]jsontext.Value, len(r))}
		for i, sub := range r {
			var err error
			if d.Regions[i], err = Marshal(sub); err != nil {
				return nil, err
			}
		}
		v = d
	default:
		return nil, fmt.Errorf("region: %T has no description: %w", r, errors.ErrUnsupported)
	}
	return json.Marshal(v)
}

// Unmarshal returns the region the JSON object v describes, as [Marshal]
// writes it.
func Unmarshal(v jsontext.Value) (Region, error) {
	if len(v) == 0 {
		return nil, errors.New("no region")
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(v, &head); err != nil {
		return nil, fmt.Errorf("region: %w", err)
	}
	var r Region
	var err error
	switch head.Type {
	case regionTypeCells:
		r, err = unmarshalRegionAs(v, cellsData.region)
	case regionTypeRows:
		r, err = unmarshalRegionAs(v, rowsData.region)
	case regionTypeText:
		r, err = unmarshalRegionAs(v, textData.region)
	case regionTypeRegexp:
		r, err = unmarshalRegionAs(v, regexpData.region)
	case regionTypeEnv:
		r, err = unmarshalRegionAs(v, envData.region)
	case regionTypeAny:
		r, err = unmarshalRegionAs(v, anyData.region)
	default:
		return nil, fmt.Errorf("unknown region type %q", head.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("%s region: %w", head.Type, err)
	}
	return r, nil
}

// unmarshalRegionAs decodes v, which may hold no members T does not, as a
// T, and returns the region build makes of it.
func unmarshalRegionAs[T any](v jsontext.Value, build func(T) (Region, error)) (Region, error) {
	var d T
	if err := json.Unmarshal(v, &d, json.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	return build(d)
}
