// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/steep/internal/xansi"
	"github.com/lrstanley/steep/reel"
	"github.com/lrstanley/steep/reel/effects"
	"github.com/lrstanley/steep/reel/vt"
	"github.com/rivo/uniseg"
)

// logoBitmap is a teacup beside "STEEP" in a 5x7 pixel font. "#" is lit.
var logoBitmap = []string{
	"##########........####.#####.#####.#####.####.",
	"##########.##....#.......#...#.....#.....#...#",
	"##########..#....#.......#...#.....#.....#...#",
	"##########.##.....###....#...####..####..####.",
	".########............#...#...#.....#.....#....",
	"..######.............#...#...#.....#.....#....",
	"############.....####....#...#####.#####.#....",
}

const (
	// logoSteamRows is the height above the logo that steam rises into, in
	// logo pixels.
	logoSteamRows = 6
	// logoMinPixel is the smallest logo pixel size worth drawing. Smaller
	// terminals get the plain text card.
	logoMinPixel = 4

	logoEmptyHold = 300 * time.Millisecond
	logoFillStep  = 50 * time.Millisecond
	logoSteamStep = 150 * time.Millisecond
)

// logoSteamX are the cup columns steam rises from.
var logoSteamX = []int{2, 5, 8}

// logoTea is the tea gradient, top pixel row to bottom. The colors are on the
// xterm-256 cube, so GIF palettes that include it keep them exact.
var logoTea = []color.Color{
	vt.Indexed(229), vt.Indexed(228), vt.Indexed(221), vt.Indexed(220),
	vt.Indexed(214), vt.Indexed(208), vt.Indexed(202),
}

var (
	// logoEmpty colors pixel rows the tea has not reached yet.
	logoEmpty = vt.Indexed(237)
	// logoSteam fades from the bottom of a wisp to its top.
	logoSteam = []color.Color{vt.Indexed(245), vt.Indexed(242), vt.Indexed(239), vt.Indexed(236)}
	// logoSway is the sideways drift of a wisp, in half logo pixels.
	logoSway = []int{0, 0, 1, 1, 1, 0, 0, -1, -1, -1}
)

// Logo returns an effect that shows steep's animated logo before the first
// frame of a recording, with steam rising for d while the recording's title
// (the test name, for a [Harness]) fades in. Terminals too small for the logo
// get a plain "steep" card instead. A d of zero or less shows nothing.
// Playback speed does not scale it.
//
// Its frames are [reel.Frame.Isolated] and have no cells, so later effects
// such as [effects.Keys] leave them alone. They count toward GIF frame and
// byte limits.
func Logo(d time.Duration) reel.Effect {
	return logo{d: d}
}

type logo struct{ d time.Duration }

func (logo) Priority() int { return reel.PriorityCard }

func (e logo) Start(ctx *reel.Context) (reel.Stage, error) {
	if e.d <= 0 {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	cards, err := effects.NewCardRenderer(ctx)
	if err != nil {
		return nil, err
	}
	return func(f reel.Frame, emit func(reel.Frame)) {
		if f.First && !logoFrames(cards, f.Cells, ctx.Title, e.d, emit) {
			title := []effects.Line{{Text: "steep", Style: vt.Style{Attrs: vt.AttrBold}}}
			if ctx.Title != "" {
				title = append(title, effects.Line{Text: ctx.Title, Style: faint})
			}
			emit(reel.Frame{Image: cards.Card(f.Cells, title...), Delay: e.d, Isolated: true})
		}
		emit(f)
	}, nil
}

// logoFrames emits the logo's frames on cards the size of src: the logo
// fills with tea from the bottom up, then steam rises for d while title fades
// in. It emits nothing and returns false when the card is too small.
func logoFrames(cards *effects.CardRenderer, src *reel.CellFrame, title string, d time.Duration, emit func(reel.Frame)) bool {
	logoW, logoH := len(logoBitmap[0]), len(logoBitmap)
	base := cards.Card(src)
	b := base.Bounds()
	px := min(b.Dx()*2/3/logoW, b.Dy()/3/(logoH+logoSteamRows))
	px -= px % 2 // Steam is drawn in half pixels.
	if px < logoMinPixel {
		return false
	}

	// The logo sits a pixel above the middle, with the title one text line
	// and a logo pixel below it.
	origin := image.Pt(b.Min.X+(b.Dx()-logoW*px)/2, b.Min.Y+(b.Dy()-logoH*px)/2-px)
	var name *image.NRGBA
	if title != "" {
		name = cards.Text(src, effects.Line{Text: title, Style: faint})
	}
	var nameAt image.Rectangle
	if name != nil {
		nb := name.Bounds()
		nameAt = nb.Sub(nb.Min).Add(image.Pt(b.Min.X+(b.Dx()-nb.Dx())/2, origin.Y+logoH*px+px+nb.Dy()))
	}

	frame := func(level, phase int, showName bool, delay time.Duration) reel.Frame {
		img := &image.NRGBA{Pix: slices.Clone(base.Pix), Stride: base.Stride, Rect: base.Rect}
		if showName && name != nil {
			draw.Draw(img, nameAt, name, name.Bounds().Min, draw.Src)
		}
		dot := func(at image.Point, size int, c color.Color) {
			draw.Draw(img, image.Rect(0, 0, size, size).Add(at), image.NewUniform(c), image.Point{}, draw.Src)
		}
		for y, row := range logoBitmap {
			c := logoTea[y]
			if y < level {
				c = logoEmpty
			}
			for x := range len(row) {
				if row[x] == '#' {
					dot(origin.Add(image.Pt(x*px, y*px)), px, c)
				}
			}
		}
		// Each wisp grows by two half pixels a step, up to the steam rows.
		half := px / 2
		height := min(2*phase, 2*logoSteamRows-2)
		for i, x := range logoSteamX {
			for k := range height {
				at := origin.Add(image.Pt(x*px+logoSway[(k+phase+4*i)%len(logoSway)]*half, -half-k*half))
				dot(at, half, logoSteam[k*len(logoSteam)/height])
			}
		}
		return reel.Frame{Image: img, Delay: delay, Isolated: true}
	}

	emit(frame(logoH, 0, false, logoEmptyHold))
	for level := logoH - 1; level >= 0; level-- {
		emit(frame(level, 0, false, logoFillStep))
	}
	steps := max(int(d/logoSteamStep), 1)
	for phase := 1; phase <= steps; phase++ {
		emit(frame(0, phase, phase >= min(3, steps), logoSteamStep))
	}
	return true
}

// Outro returns an effect that shows a card for d after the last frame of a
// recording (see [effects.OutroFunc]). With lines, the card shows them.
// Without, it shows the *[Result] in [reel.Context.Meta], which a [Harness]
// export sets: PASS or FAIL, the test name, and the first line of as many
// failures as fit. Without lines or a *Result, or with a d of zero or less,
// it shows nothing. Playback speed does not scale d.
//
// For a failed test, the last frame before the card plays for at least 3s
// ([effects.HoldLast]), so the failing screen can be read. An [effects.Pause]
// badges that frame.
func Outro(d time.Duration, lines ...effects.Line) reel.Effect {
	return outro{d: d, lines: lines}
}

type outro struct {
	d     time.Duration
	lines []effects.Line
}

func (outro) Priority() int { return reel.PriorityCard }

func (e outro) Start(ctx *reel.Context) (reel.Stage, error) {
	result, _ := ctx.Meta.(*Result)
	if len(e.lines) == 0 && result == nil {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	card, err := effects.OutroFunc(e.d, func(cols, rows int) []effects.Line {
		if len(e.lines) > 0 {
			return e.lines
		}
		return resultLines(result, cols, rows)
	}).Start(ctx)
	if err != nil || card == nil || result == nil || !result.Failed {
		return card, err
	}
	hold, err := effects.HoldLast(failureHold).Start(ctx)
	if err != nil {
		return nil, err
	}
	return func(f reel.Frame, emit func(reel.Frame)) {
		hold(f, func(f reel.Frame) { card(f, emit) })
	}, nil
}

// failureHold is the minimum duration of a failed test's last frame.
const failureHold = 3 * time.Second

var (
	passStyle = vt.Style{Fg: vt.Indexed(2), Attrs: vt.AttrBold}
	failStyle = vt.Style{Fg: vt.Indexed(1), Attrs: vt.AttrBold}
	faint     = vt.Style{Attrs: vt.AttrFaint}
)

// resultLines are the default outro card lines for a cols by rows grid.
func resultLines(r *Result, cols, rows int) []effects.Line {
	if !r.Failed {
		return []effects.Line{{Text: "PASS", Style: passStyle}, {Text: r.Name, Style: faint}}
	}
	lines := []effects.Line{{Text: "FAIL", Style: failStyle}, {Text: r.Name, Style: faint}}
	return append(lines, failureLines(r.Failures, cols, rows)...)
}

// failureLines lists failures for the FAIL card on a cols by rows grid. Only
// the first line of each is shown; the rest is usually a screen dump the
// recording already shows. Lines are padded to one width so the block stays
// left-aligned when each line is centered.
func failureLines(failures []string, cols, rows int) []effects.Line {
	// Leave rows for the two title lines, a spacer, and a margin above and below.
	room := rows - 5
	width := max(cols-6, 10)

	var msg []string
	shown := 0
	for _, failure := range failures {
		summary, _, _ := strings.Cut(strings.TrimSpace(xansi.StripANSI(failure)), "\n")
		wrapped := strings.Split(ansi.Wrap(strings.ReplaceAll(summary, "\t", " "), width, ""), "\n")
		need := len(msg) + len(wrapped)
		if shown < len(failures)-1 {
			need++ // Room for "+N more".
		}
		if need > room {
			break
		}
		for i, line := range wrapped {
			prefix := "  "
			if i == 0 {
				prefix = "• "
			}
			msg = append(msg, prefix+strings.TrimRight(line, " "))
		}
		shown++
	}
	if shown == 0 {
		return nil
	}

	var block int
	for _, line := range msg {
		block = max(block, uniseg.StringWidth(line))
	}
	lines := []effects.Line{{}}
	for _, line := range msg {
		lines = append(lines, effects.Line{Text: line + strings.Repeat(" ", block-uniseg.StringWidth(line))})
	}
	if more := len(failures) - shown; more > 0 {
		lines = append(lines, effects.Line{Text: fmt.Sprintf("+%d more", more), Style: faint})
	}
	return lines
}
