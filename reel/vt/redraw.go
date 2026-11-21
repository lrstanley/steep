// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"strings"

	"getsteep.dev/steep/reel/vt/internal/wasmvt"
	"github.com/charmbracelet/x/ansi"
)

// Redraw returns VT bytes that, written to a fresh terminal of the same size,
// draw what t shows, so a recording can start part way through. They hold:
//
//   - the active screen and its scrollback, with styles and hyperlinks
//   - the cursor's position, shape, and visibility, and its pen: style,
//     hyperlink, protection, and character sets
//   - the modes that differ from their defaults, such as the alternate
//     screen, bracketed paste, and mouse and keyboard reporting, the kitty
//     keyboard flags, and modifyOtherKeys
//   - the scrolling region and tab stops
//   - the window title and working directory
//   - the foreground, background, cursor, and indexed colors the program set
//   - the Glyph Protocol glossary, oldest registration first
//   - the OSC 9;4 progress and OSC 7501 program status
//   - the OSC 133 prompt marks, how their commands ended, and the step the
//     last one is at, but not which cells were typed input or output
//
// Kitty graphics placements on the active screen are sent again with their
// images, as of the current animation frame, cropped to the screen.
//
// Left out are the screen that is not active (the primary screen while the
// alternate screen is shown), the saved cursor, Kitty images with no
// placement on the active screen, notifications, and which rows were
// soft-wrapped, so a later resize reflows them as separate lines.
// In synchronized output, the bytes start a hold, but over the screen as it
// is rather than the screen the hold froze.
//
// The screen is written by libghostty-vt's VT formatter, which also restores
// the modes, scrolling region, tab stops, keyboard state, and cursor.
func (t *Terminal) Redraw() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return nil, errClosed
	}
	screenVT, err := t.screenVTLocked()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	// The cursor shape first: the modes the formatter restores include
	// the cursor's blinking, which DECSCUSR also sets.
	b.WriteString(cursorShape(t.cursorLocked()))
	b.WriteString(glyphsVT(t.glyphsLocked()))
	b.Write(screenVT)
	t.redrawImagesLocked(&b)
	if t.title != "" {
		b.WriteString(ansi.SetWindowTitle(t.title))
	}
	for _, c := range []struct {
		current, def int32
		set          func(string) string
	}{
		{wasmvt.TerminalDataColorForeground, wasmvt.TerminalDataColorForegroundDefault, ansi.SetForegroundColor},
		{wasmvt.TerminalDataColorBackground, wasmvt.TerminalDataColorBackgroundDefault, ansi.SetBackgroundColor},
		{wasmvt.TerminalDataColorCursor, wasmvt.TerminalDataColorCursorDefault, ansi.SetCursorColor},
	} {
		// A color the program set differs from the default the host set.
		// A color neither set reads as the zero fallback both times.
		cur := t.readRGB(c.current, color.NRGBA{})
		if def := t.readRGB(c.def, color.NRGBA{}); cur != def {
			b.WriteString(c.set(fmt.Sprintf("#%02x%02x%02x", cur.R, cur.G, cur.B)))
		}
	}
	b.WriteString(paletteVT(t.paletteLocked()))
	b.WriteString(progressVT(t.progress))
	if t.statusReported && len(t.statuses) == 0 {
		b.WriteString(ansi.ClearProgramStatus)
	}
	for _, s := range t.statuses {
		b.WriteString(programStatusVT(s))
	}
	return b.Bytes(), nil
}

// screenVTLocked returns the formatter's output for the terminal, with the
// OSC 133 prompt marks written into the rows they belong to. Written to a
// fresh terminal, the rows fill its scrollback and screen as they were: with
// trimming off, the formatter writes every row, blank ones too.
func (t *Terminal) screenVTLocked() ([]byte, error) {
	// Every extra but the palette, which paletteVT writes.
	full, err := t.formatLocked(wasmvt.FormatterFormatVt, 0, formatterExtrasAll&^formatterExtraPalette)
	if err != nil {
		return nil, err
	}
	start, end, ok := screenRowsVT(full)
	// Every row but the last ends with CRLF, and the first is at the top of
	// the scrollback, so a split in the wrong place shows in the count.
	if !ok || bytes.Count(full[start:end], []byte("\r\n")) != t.scrollbackLenLocked()+t.rows-1 {
		return nil, errors.New("vt: redraw: unexpected formatter output")
	}
	marks := t.promptMarksLocked()
	out := make([]byte, 0, len(full)+16*len(marks))
	out = append(out, full[:start]...)
	out = appendPromptVT(out, full[start:end], marks, t.prompt)
	return append(out, full[end:]...), nil
}

// screenRowsVT finds the screen's rows in the VT formatter's output, between
// the extras before and after them. The ones before end with a cursor home.
// Rows hold text, CRLFs, SGR, ECH and CUF (CSI m, X and C with plain
// parameters), and OSC 8 hyperlinks. The extras after them start with
// something rows never hold: the scrolling region, modifyOtherKeys, OSC 7,
// or the cursor position, which is always written.
func screenRowsVT(full []byte) (start, end int, ok bool) {
	home := bytes.Index(full, []byte("\x1b[H"))
	if home < 0 {
		return 0, 0, false
	}
	start = home + 3
	for i := start; ; {
		j := bytes.IndexByte(full[i:], ansi.ESC)
		if j < 0 {
			return 0, 0, false
		}
		i += j
		n := rowSequenceLen(full[i:])
		if n == 0 {
			return start, i, true
		}
		i += n
	}
}

// rowSequenceLen is the length of the escape sequence p starts with when it
// is one the formatter writes inside a row, or 0 when it isn't.
func rowSequenceLen(p []byte) int {
	if link, ok := bytes.CutPrefix(p, []byte("\x1b]8;")); ok {
		if k := bytes.Index(link, []byte("\x1b\\")); k >= 0 {
			return len(p) - len(link) + k + 2
		}
		return 0
	}
	if len(p) < 3 || p[1] != '[' {
		return 0
	}
	for k := 2; k < len(p); k++ {
		switch c := p[k]; {
		case c >= '0' && c <= '9', c == ';', c == ':':
		case c == 'm', c == 'X', c == 'C':
			return k + 1
		default:
			return 0
		}
	}
	return 0
}

// GhosttyFormatterExtras bits (GHOSTTY_FORMATTER_EXTRA_* in formatter.h).
// The manifest describes the type only as a u32, so the bits are not
// generated.
const (
	formatterExtraPalette = 1 << 0
	formatterExtrasNone   = 0
	formatterExtrasAll    = 0xfff
)

// formatLocked runs a formatter over the terminal with format, extras, and
// trimming and unwrapping off. sel is a staged GhosttySelection, or 0 for
// the whole screen and its scrollback. The output is formatted once, into a
// buffer the library allocates.
func (t *Terminal) formatLocked(format, sel int32, extras uint32) ([]byte, error) {
	out, err := t.mod.Alloc(4)
	if err != nil {
		return nil, err
	}
	defer t.mod.Free(out, 4)
	t.mod.Zero(out, 4)
	if rerr := wasmvt.Result("formatter new", t.mod.Xghostty_formatter_terminal_new(0, out, t.term)); rerr != nil {
		return nil, rerr
	}
	f := t.mod.I32(out, 0)
	defer t.mod.Xghostty_formatter_free(f)

	// Options are passed by pointer and copied by the formatter.
	t.mod.SetI32(out, 0, format)
	if rerr := wasmvt.Result("formatter emit", t.mod.Xghostty_formatter_set(f, wasmvt.FormatterOptionEmit, out)); rerr != nil {
		return nil, rerr
	}
	t.mod.SetU32(out, 0, 0)
	if rerr := wasmvt.Result("formatter trim", t.mod.Xghostty_formatter_set(f, wasmvt.FormatterOptionTrim, out)); rerr != nil {
		return nil, rerr
	}
	t.mod.SetU32(out, 0, extras)
	if rerr := wasmvt.Result("formatter extras", t.mod.Xghostty_formatter_set(f, wasmvt.FormatterOptionExtras, out)); rerr != nil {
		return nil, rerr
	}
	if sel != 0 {
		if rerr := wasmvt.Result("formatter selection", t.mod.Xghostty_formatter_set(f, wasmvt.FormatterOptionSelection, sel)); rerr != nil {
			return nil, rerr
		}
	}

	res, err := t.mod.Alloc(8)
	if err != nil {
		return nil, err
	}
	defer t.mod.Free(res, 8)
	t.mod.Zero(res, 8)
	if rerr := wasmvt.Result("format", t.mod.Xghostty_formatter_format_alloc(f, 0, res, res+4)); rerr != nil {
		return nil, rerr
	}
	ptr, n := t.mod.I32(res, 0), int(t.mod.U32(res, 4))
	if ptr == 0 || n == 0 {
		return nil, nil
	}
	defer t.mod.Xghostty_free(0, ptr, wasmvt.Int32(n))
	return append([]byte(nil), t.mod.Slice(ptr, n)...), nil
}

// cursorShape returns the DECSCUSR sequence for c's shape and blinking.
func cursorShape(c Cursor) string {
	var style int
	switch c.Shape {
	case CursorUnderline:
		style = 3
	case CursorBar:
		style = 5
	case CursorBlock, CursorBlockHollow:
		style = 1
	}
	if !c.Blink {
		style++
	}
	return ansi.SetCursorStyle(style)
}

// progressVT returns the OSC 9;4 report that sets p, or nothing when there
// is none.
func progressVT(p Progress) string {
	if !p.Reported || p.State == ProgressRemove {
		return ""
	}
	seq := fmt.Sprintf("\x1b]9;4;%d", p.State)
	if p.Percent >= 0 {
		seq += fmt.Sprintf(";%d", p.Percent)
	}
	return seq + "\x07"
}

// paletteVT returns the OSC 4 that sets p's colors, or nothing when it has
// none.
func paletteVT(p *Palette) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\x1b]4")
	for i := range 256 {
		if c, ok := p.Color(uint8(i)); ok {
			fmt.Fprintf(&b, ";%d;#%02x%02x%02x", i, c.R, c.G, c.B)
		}
	}
	return b.String() + "\x07"
}

// programStatusVT returns the OSC 7501 report of s.
func programStatusVT(s ProgramStatus) string {
	return ansi.SetProgramStatus(ansi.ProgramStatus{
		State:       ansi.ProgramState(s.State.String()),
		ID:          s.ID,
		App:         s.App,
		Kind:        ansi.ProgramStatusKind(s.Kind.String()),
		Progress:    max(s.Progress, 0),
		HasProgress: s.Progress >= 0,
		Title:       s.Title,
		Message:     s.Message,
	})
}
