// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"errors"
	"fmt"
	"image/color"
	"slices"

	"getsteep.dev/steep/reel/vt/internal/wasmvt"
)

// SnapshotContinuationMaxBytes is how much of an unfinished escape sequence
// a terminal opened [WithSnapshots] keeps, so a [Terminal.Snapshot] taken
// part way through one restores it. A longer one makes Snapshot fail until
// the sequence ends.
const SnapshotContinuationMaxBytes = 1 << 20

// WithSnapshots has the terminal keep the unfinished escape sequence at the
// end of its input, so [Terminal.Snapshot] works part way through one, as
// between two writes of a Kitty image. Without it, Snapshot fails while
// [Terminal.InSequence] is true.
func WithSnapshots() Option {
	return func(cfg *openConfig) { cfg.snapshots = true }
}

// ErrSnapshot is returned by [Terminal.Snapshot] for a terminal it cannot
// capture so that [Terminal.Restore] puts it back exactly. The terminal is
// unchanged, and a later Snapshot may work.
var ErrSnapshot = errors.New("vt: snapshot")

// Snapshot is a terminal at one moment: libghostty-vt's snapshot of the
// emulator, with its screens, scrollback, modes, colors, Kitty images, and
// unfinished input, and the state package vt keeps beside it, such as the
// title, progress, prompt marks, and registered glyphs. It is held in Go
// memory, a small part of what a terminal holds, and is read-only, so
// [Terminal.Restore] can use it any number of times.
//
// The bytes are libghostty-vt's snapshot format, which may change in any
// release, so a Snapshot is only for the process that took it.
type Snapshot struct {
	data []byte
	// glyphs are the registered glyphs, which the snapshot format leaves
	// out. Restore registers them again.
	glyphs *Glyphs
	// marks are the prompt marks, as rows from the top of the scrollback.
	marks []PromptMark
	// offset is how far the viewport is scrolled up, which the format
	// leaves out.
	offset int
	// state is the rest package vt keeps, and the Kitty storage limits.
	state                termState
	kittyLimit, kittyDef uint64
}

// Size is the terminal's size in cells when the snapshot was taken, which
// [Terminal.Restore] gives the terminal.
func (s *Snapshot) Size() (cols, rows int) { return s.state.cols, s.state.rows }

// Len is the size of the snapshot's bytes.
func (s *Snapshot) Len() int { return len(s.data) }

// termState is the part of a [Terminal] a [Snapshot] copies as it is: the
// state package vt keeps beside the emulator, which libghostty-vt's snapshot
// format leaves out. A field added here is snapshotted and restored with
// the rest. clone copies what is changed in place.
type termState struct {
	// cols and rows are the grid size in cells.
	cols, rows int
	// cellW and cellH are the pixel size of one cell. Mouse encoding and
	// in-band size reports use them. They are not changed by Resize.
	cellW, cellH uint32
	// scrollbackLines is the history cap applied with
	// TerminalOptionScrollbackMaxLines. Zero disables history.
	scrollbackLines int
	// scrollbackOff is true after a zero cap sets the byte limit to
	// zero. libghostty keeps that screen in no-scrollback mode until
	// the byte limit is cleared, even if a line cap is set later.
	scrollbackOff bool
	// checksumReport is the host DECRQCRA reply switch. A new checkout
	// starts off. Reset keeps the current value.
	checksumReport bool
	// checksumFlags is the XTCHECKSUM variant restored by a full reset.
	checksumFlags ChecksumFlags
	// cursorBlink is [WithCursorBlink]'s default, which a full reset keeps.
	cursorBlink bool

	// title is the last OSC 0 or OSC 2 window title.
	title string
	// pwd is the last OSC 7 working directory.
	pwd string

	// scan is the string a write is inside, while unknown or controls
	// watches strings.
	scan stringScan

	// bell is the latest BEL.
	bell Bell
	// alt is true when the alternate screen is active.
	alt bool
	// held is true during synchronized output (mode 2026).
	held bool
	// frozen is the grid captured when the current hold started. Reads
	// return it instead of a partial update. frozenCur is its caret.
	frozen    *Buffer
	frozenCur Cursor

	// progress is the latest OSC 9;4 report.
	progress Progress
	// notifications are OSC 9 and OSC 777 messages, oldest first. Only
	// appended to, and replaced on reset, so a [State] can share it.
	notifications []Notification
	// clipRead is the host clipboard returned for an OSC 52 read.
	clipRead string
	// clipWrote is the last plain text the program wrote to the clipboard.
	clipWrote ClipboardWrite
	// prompt is the latest OSC 133 semantic-prompt step.
	prompt Prompt

	// statuses are OSC 7501 program-status records in update order, least
	// recently updated first, which is the eviction order.
	statuses []ProgramStatus
	// statusView is statuses as [Terminal.ProgramStatuses] returns them:
	// sorted by id, with App inherited. It is replaced, never changed in
	// place, so a [State] can share it. statusGen counts changes to it,
	// and is never reset, so equal generations mean the same records.
	statusView []ProgramStatus
	statusGen  uint64
	// statusReported is true once any OSC 7501 report has arrived since the
	// last full reset.
	statusReported bool
	// resizeCols and resizeRows are the last CSI 8 t window-resize
	// request. resizeOK is false until the program asks. The terminal
	// does not apply the request.
	resizeCols int
	resizeRows int
	resizeOK   bool

	// fg, bg, and cursor are the colors last shown, after overrides.
	fg, bg, cursor color.NRGBA
	// fgOverride, bgOverride, and cursorOverride replace the emulator
	// colors when non-nil. Nil means use the default.
	fgOverride     color.Color
	bgOverride     color.Color
	cursorOverride color.Color
	// fgDefault, bgDefault, and cursorDefault are restored when the
	// matching override is cleared.
	fgDefault     color.Color
	bgDefault     color.Color
	cursorDefault color.Color
}

// clone returns a copy of s that shares nothing s may change in place.
func (s termState) clone() termState {
	if s.frozen != nil {
		s.frozen = s.frozen.Clone()
	}
	// Clipped, so appends to either never write into what a State from the
	// other shares.
	s.notifications = slices.Clip(s.notifications)
	s.statuses = slices.Clone(s.statuses)
	// Only a string still open or waiting on its ESC end is worth keeping.
	if s.scan.open || s.scan.escEnd {
		s.scan.data = slices.Clone(s.scan.data)
	} else {
		s.scan = stringScan{}
	}
	return s
}

// Snapshot captures the terminal so [Terminal.Restore] can put it back, on
// this terminal or another of the same [Runtime]. It fails with an error
// wrapping [ErrSnapshot] when it cannot capture the terminal exactly: part way
// through an escape sequence, unless the terminal was opened [WithSnapshots];
// part way through one with glyphs registered, since registering them again
// would write into it; and on the alternate screen while prompt marks are
// kept for the primary one, which can only be tracked on the screen shown.
//
// What a snapshot leaves out: the selection, live [Search] values, replies
// not yet read, and which row a Kitty animation is on between frames.
func (t *Terminal) Snapshot() (*Snapshot, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return nil, errClosed
	}
	if t.activeAlt() && len(t.prompts.marks) > 0 {
		return nil, fmt.Errorf("%w: prompt marks of the primary screen while the alternate screen is shown", ErrSnapshot)
	}
	inSeq := t.inSequenceLocked()
	if inSeq && !t.snapshots {
		return nil, fmt.Errorf("%w: input ends inside an escape sequence", ErrSnapshot)
	}
	glyphs := t.glyphsLocked()
	if inSeq && glyphs != nil {
		return nil, fmt.Errorf("%w: input ends inside an escape sequence, with glyphs registered", ErrSnapshot)
	}
	// The decoded terminal runs the unfinished input before the Glyph
	// Protocol is turned on for it, so it would ignore a glyph's register,
	// which can come in an APC, SOS, or PM.
	if inSeq && t.scan.open && t.scan.kind != SequenceDCS && t.scan.kind != SequenceOSC {
		return nil, fmt.Errorf("%w: input ends inside an APC, SOS, or PM", ErrSnapshot)
	}
	marks := slices.Clone(t.promptMarksLocked())

	out, err := t.mod.Alloc(8)
	if err != nil {
		return nil, err
	}
	defer t.mod.Free(out, 8)
	t.mod.Zero(out, 8)
	if rc := t.mod.Xghostty_snapshot_encode_alloc(t.term, 0, out, out+4); rc != 0 {
		if rc == wasmvt.ResultInvalidValue {
			// The unfinished input is longer than the terminal keeps.
			return nil, fmt.Errorf("%w: %w", ErrSnapshot, wasmvt.Result("encode", rc))
		}
		return nil, wasmvt.Result("snapshot encode", rc)
	}
	ptr, n := t.mod.I32(out, 0), int(t.mod.U32(out, 4))
	data := append([]byte(nil), t.mod.Slice(ptr, n)...)
	t.mod.Xghostty_free(0, ptr, wasmvt.Int32(n))

	s := &Snapshot{
		data:       data,
		glyphs:     glyphs,
		marks:      marks,
		offset:     t.scrollbackOffsetLocked(),
		state:      t.termState.clone(),
		kittyLimit: t.kitty.limit,
		kittyDef:   t.kitty.def,
	}
	return s, nil
}

// Restore puts the terminal back as s captured it, from this terminal or
// another of the same [Runtime], replacing everything it shows. It keeps
// what was set when it was opened, such as [WithClock] and
// [WithUnknownSequence], and the replies not yet read. Live [Search] values
// are closed. A restore counts as a write for [Terminal.OutputSeq].
func (t *Terminal) Restore(s *Snapshot) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	if s == nil || len(s.data) == 0 {
		return errors.New("vt: restore: empty snapshot")
	}
	term, err := t.decodeSnapshotLocked(s)
	if err != nil {
		return err
	}
	// A fresh render state, so the update reads every row of the new
	// terminal. The new terminal is set up before the old one goes, so a
	// failure leaves the terminal as it was.
	render, err := t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_render_state_new(0, slot)
	})
	if err != nil {
		t.mod.Xghostty_terminal_free(term)
		return err
	}
	oldTerm, oldState, oldLimit := t.term, t.termState, t.kitty.limit
	t.term = term
	t.termState = s.state.clone()
	// The generation counts changes on this terminal, so it moves on rather
	// than back, and never names two sets of records.
	t.statusGen = oldState.statusGen + 1
	t.kitty.limit = s.kittyLimit
	if err = t.applyRestoredOptionsLocked(); err != nil {
		t.term, t.termState, t.kitty.limit = oldTerm, oldState, oldLimit
		t.mod.Xghostty_terminal_free(term)
		t.mod.Xghostty_render_state_free(render)
		return err
	}

	// The old terminal's searches and tracked rows go with it.
	t.freeSearchesLocked()
	t.freePromptMarksLocked()
	t.mod.Xghostty_terminal_free(oldTerm)
	t.mod.Xghostty_render_state_free(t.render)
	t.render = render
	t.kitty.def = s.kittyDef
	t.kitty.reset()
	t.kitty.limit = s.kittyLimit
	// A terminal that watches no strings would keep the scanner's state
	// stale, so it starts with none.
	if t.boundariesLocked() == 0 {
		t.scan = stringScan{}
	}
	t.grid = nil
	t.gridFresh = false
	t.invalidateRowWords()
	t.paletteFresh = false
	t.glyphs, t.glyphsGen, t.glyphsFresh = nil, 0, false
	t.outputSeq++
	t.restoreGlyphsLocked(s.glyphs)
	t.restorePromptMarksLocked(s.marks)
	if s.offset > 0 {
		t.setOffsetLocked(s.offset, t.scrollbackLenLocked())
	}
	return nil
}

// decodeSnapshotLocked decodes s into a new GhosttyTerminal, in this
// terminal's module.
func (t *Terminal) decodeSnapshotLocked(s *Snapshot) (int32, error) {
	n := len(s.data)
	buf, err := t.mod.Alloc(n)
	if err != nil {
		return 0, err
	}
	defer t.mod.Free(buf, n)
	t.mod.Put(buf, s.data)
	dec, err := t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_snapshot_decoder_new_buf(0, slot, buf, wasmvt.Int32(n))
	})
	if err != nil {
		return 0, fmt.Errorf("vt: restore: %w", err)
	}
	defer t.mod.Xghostty_snapshot_decoder_free(dec)

	opt := t.scratch
	t.mod.Zero(opt, 8)
	t.mod.SetU32(opt, 0, SnapshotContinuationMaxBytes)
	err = wasmvt.Result("restore continuation", t.mod.Xghostty_snapshot_decoder_set(dec, wasmvt.SnapshotDecoderOptionMaxContinuationBytes, opt))
	if err != nil {
		return 0, err
	}
	// Tracking follows this terminal's WithSnapshots, not the snapshot's.
	t.mod.SetU32(opt, 0, 0)
	if t.snapshots {
		t.mod.SetU8(opt, 0, 1)
	}
	err = wasmvt.Result("restore continuation", t.mod.Xghostty_snapshot_decoder_set(dec, wasmvt.SnapshotDecoderOptionRetainContinuation, opt))
	if err != nil {
		return 0, err
	}
	t.mod.Zero(opt, 8)
	limit := s.kittyLimit
	t.mod.SetU32(opt, 0, uint32(limit))     //nolint:gosec // the low word of a u64.
	t.mod.SetU32(opt, 4, uint32(limit>>32)) //nolint:gosec // the high word.
	err = wasmvt.Result("restore kitty storage", t.mod.Xghostty_snapshot_decoder_set(dec, wasmvt.SnapshotDecoderOptionKittyImageStorageLimit, opt))
	if err != nil {
		return 0, err
	}
	out := t.scratch + 8
	t.mod.Zero(out, 4)
	err = wasmvt.Result("restore", t.mod.Xghostty_snapshot_decoder_decode(dec, out))
	if err != nil {
		return 0, err
	}
	term := t.mod.I32(out, 0)
	if term == 0 {
		return 0, errors.New("vt: restore: no terminal")
	}
	return term, nil
}

// applyRestoredOptionsLocked sets on a restored terminal the host options
// and effects, which the snapshot format leaves to the host. Colors, the
// cursor defaults, and the size in cells and pixels are in the snapshot.
func (t *Terminal) applyRestoredOptionsLocked() error {
	if err := t.installCallbacks(); err != nil {
		return err
	}
	var unknownMax uint32
	if t.unknown != nil {
		unknownMax = UnknownSequenceMaxBytes
	}
	if err := t.mod.SetU32Option(t.term, wasmvt.TerminalOptionUnknownMaxBytes, unknownMax); err != nil {
		return err
	}
	return t.reapplyLocked()
}

// restoreGlyphsLocked registers g's glyphs again, oldest first, with their
// replies and string reports dropped.
func (t *Terminal) restoreGlyphsLocked(g *Glyphs) {
	vt := glyphsVT(g)
	if vt == "" {
		return
	}
	replies, unknown, controls := t.replies, t.unknown, t.controls
	t.replies, t.unknown, t.controls = nil, nil, nil
	_ = t.writeLocked([]byte(vt)) // It never fails.
	t.replies, t.unknown, t.controls = replies, unknown, controls
}

// restorePromptMarksLocked tracks marks again, at their rows from the top of
// the scrollback. A mark whose row cannot be tracked is dropped.
func (t *Terminal) restorePromptMarksLocked(marks []PromptMark) {
	pm := &t.prompts
	for _, m := range marks {
		pt, out := t.scratch, t.scratch+int32(wasmvt.SizePoint)
		t.writePoint(pt, Point{Tag: PointScreen, Y: m.Row})
		t.mod.Zero(out, wasmvt.SizeTrackedGridRef)
		if t.mod.Xghostty_terminal_grid_ref_track(t.term, pt, out) != 0 {
			continue
		}
		pm.marks = append(pm.marks, promptMark{PromptMark: m, ref: t.mod.I32(out, 0)})
	}
	pm.moved = true
}
