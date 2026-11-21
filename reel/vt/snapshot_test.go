// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
)

// snapshotState is a terminal with history, a scrolled viewport, styles,
// colors, a Kitty image, a glyph, prompt marks, and program reports.
func snapshotScene() string {
	var b strings.Builder
	for i := range 12 {
		fmt.Fprintf(&b, "line %d\r\n", i)
	}
	b.WriteString(promptCmd("ok", 0) + promptCmd("bad", 3))
	b.WriteString("\x1b]2;snap title\x07\x1b]7;file:///tmp/snap\x07\x1b]9;4;1;40\x07")
	b.WriteString("\x1b]4;1;#102030\x07\x1b]10;#445566\x07\x1b[1;31mred\x1b[0m \x1b]8;;https://e.x\x1b\\link\x1b]8;;\x1b\\\r\n")
	b.WriteString("\x1b_25a1;r;cp=e000;upm=1000;" + glyphTriangle + "\x1b\\\r\n")
	b.WriteString(kittyAPC("a=T,i=4,f=32,s=2,v=2,c=1,r=1,q=2", solid(2, 2, color.NRGBA{B: 0xff, A: 0xff})))
	b.WriteString("\x1b[?2004h\x1b[?1000h\x1b[2;3r\x1b[1;1H")
	return b.String()
}

func TestSnapshotRestore(t *testing.T) {
	t.Parallel()
	term := openTest(t, 20, 4)
	writeTerm(t, term, snapshotScene())
	if err := term.ScrollTo(3); err != nil {
		t.Fatal(err)
	}
	want := openTest(t, 20, 4)
	writeTerm(t, want, snapshotScene())
	if err := want.ScrollTo(3); err != nil {
		t.Fatal(err)
	}

	snap, err := term.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if cols, rows := snap.Size(); cols != 20 || rows != 4 || snap.Len() == 0 {
		t.Fatalf("snapshot of %dx%d, %d bytes", cols, rows, snap.Len())
	}
	// Change everything, then go back.
	writeTerm(t, term, "\x1b[r\x1b[?1049h\x1b]2;other\x07\x1b_25a1;c\x1b\\"+kittyAPC("a=d", nil)+"\x1bc after")
	for range 2 {
		if err = term.Restore(snap); err != nil {
			t.Fatal(err)
		}
		assertRestored(t, want, term)
		// A restored terminal goes on as the original would.
		next := "\r\nmore " + promptCmd("again", 0)
		writeTerm(t, term, next)
		writeTerm(t, want, next)
		assertRestored(t, want, term)
		writeTerm(t, want, "\x1bc")
		writeTerm(t, want, snapshotScene())
		if err = want.ScrollTo(3); err != nil {
			t.Fatal(err)
		}
	}

	// Another terminal of the same runtime takes the snapshot too.
	other := openTest(t, 5, 2)
	if err = other.Restore(snap); err != nil {
		t.Fatal(err)
	}
	assertRestored(t, want, other)
}

// assertRestored fails unless got shows what want does, including its
// images, glyphs, prompt marks, and viewport.
func assertRestored(t *testing.T, want, got *Terminal) {
	t.Helper()
	assertSameTerminal(t, want, got)
	if w, g := want.ScrollbackOffset(), got.ScrollbackOffset(); w != g {
		t.Errorf("scrollback offset = %d, want %d", g, w)
	}
	if w, g := want.State().PromptMarks, got.State().PromptMarks; !slices.Equal(w, g) {
		t.Errorf("prompt marks = %+v, want %+v", g, w)
	}
	if w, g := want.State().Glyphs.Lookup(0xe000), got.State().Glyphs.Lookup(0xe000); (w == nil) != (g == nil) ||
		w != nil && (w.UnitsPerEm != g.UnitsPerEm || !slices.EqualFunc(w.Contours, g.Contours, slices.Equal)) {
		t.Errorf("glyph = %+v, want %+v", g, w)
	}
	wi, gi := want.Images(), got.Images()
	if len(wi) != len(gi) {
		t.Fatalf("images = %+v, want %+v", gi, wi)
	}
	for i := range wi {
		if wi[i].Rect != gi[i].Rect || wi[i].Image.ID != gi[i].Image.ID || !bytes.Equal(wi[i].Image.Pixels.Pix, gi[i].Image.Pixels.Pix) {
			t.Errorf("image %d = %+v, want %+v", i, gi[i], wi[i])
		}
	}
}

func TestSnapshotInSequence(t *testing.T) {
	t.Parallel()
	plain := openTest(t, 10, 2)
	writeTerm(t, plain, "\x1b]2;par")
	if _, err := plain.Snapshot(); !errors.Is(err, ErrSnapshot) {
		t.Fatalf("Snapshot part way through a sequence = %v, want ErrSnapshot", err)
	}

	term, err := testRuntime.Open(context.Background(), 10, 2, WithSnapshots())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	writeTerm(t, term, "\x1b]2;par")
	snap, err := term.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	writeTerm(t, term, "\x07other")
	if err = term.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if !term.InSequence() {
		t.Fatal("restored terminal is not inside the sequence")
	}
	writeTerm(t, term, "tial\x07")
	if title := term.Title(); title != "partial" {
		t.Fatalf("title = %q, want %q", title, "partial")
	}

	// Glyphs could only be registered again into the middle of it.
	writeTerm(t, term, "\x1b_25a1;r;cp=e000;upm=1000;"+glyphTriangle+"\x1b\\\x1b[3")
	if _, err = term.Snapshot(); !errors.Is(err, ErrSnapshot) {
		t.Fatalf("Snapshot with glyphs part way through a sequence = %v, want ErrSnapshot", err)
	}
}

func TestSnapshotAltScreenMarks(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 3)
	writeTerm(t, term, promptCmd("ok", 0)+"\x1b[?1049h")
	if _, err := term.Snapshot(); !errors.Is(err, ErrSnapshot) {
		t.Fatalf("Snapshot on the alternate screen with marks = %v, want ErrSnapshot", err)
	}
	writeTerm(t, term, "\x1b[?1049l")
	if _, err := term.Snapshot(); err != nil {
		t.Fatal(err)
	}
}

// TestSnapshotHold checks that a snapshot taken during synchronized output
// restores the hold, and that the program's end of it still ends it.
func TestSnapshotHold(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 2)
	writeTerm(t, term, "before\x1b[?2026h\r\nheld")
	if !term.Held() {
		t.Fatal("not held")
	}
	snap, err := term.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	writeTerm(t, term, "\x1b[?2026l")
	if err = term.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if !term.Held() || strings.Contains(term.Text(), "held") {
		t.Fatalf("restored held = %v, text %q, want the frozen frame", term.Held(), term.Text())
	}
	writeTerm(t, term, "\x1b[?2026l")
	if term.Held() || !strings.Contains(term.Text(), "held") {
		t.Fatalf("after the hold ends: held = %v, text %q", term.Held(), term.Text())
	}
}

// TestSnapshotHostColors checks that a restore brings back the host colors
// of the snapshot, not those of the terminal it restores onto.
func TestSnapshotHostColors(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 2)
	red, blue := color.NRGBA{R: 0xff, A: 0xff}, color.NRGBA{B: 0xff, A: 0xff}
	if err := term.SetDefaultForeground(red); err != nil {
		t.Fatal(err)
	}
	snap, err := term.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err = term.SetDefaultForeground(blue); err != nil {
		t.Fatal(err)
	}
	if err = term.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if fg := term.Foreground(); fg != red {
		t.Fatalf("foreground = %v, want %v", fg, red)
	}
	// Clearing the default goes back to the terminal's own.
	if err = term.SetDefaultForeground(nil); err != nil {
		t.Fatal(err)
	}
	if fg := term.Foreground(); fg == red || fg == blue {
		t.Fatalf("foreground after clearing = %v", fg)
	}
}

// TestSnapshotStatusGeneration checks that a restore moves the program
// status generation on, so no value names two sets of records.
func TestSnapshotStatusGeneration(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 2)
	writeTerm(t, term, "\x1b]7501;state=working:id=a\x07")
	snap, err := term.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	writeTerm(t, term, "\x1b]7501;state=blocked:id=b\x07")
	later := term.State().ProgramStatusGeneration
	if err = term.Restore(snap); err != nil {
		t.Fatal(err)
	}
	st := term.State()
	if st.ProgramStatusGeneration <= later || len(st.ProgramStatuses) != 1 || st.ProgramStatuses[0].ID != "a" {
		t.Fatalf("restored generation %d (was %d), statuses %+v", st.ProgramStatusGeneration, later, st.ProgramStatuses)
	}
}

// TestSnapshotGlyphCommand checks that a snapshot part way through a Glyph
// Protocol command is refused: the restored terminal would ignore it.
func TestSnapshotGlyphCommand(t *testing.T) {
	t.Parallel()
	term, err := testRuntime.Open(context.Background(), 10, 2, WithSnapshots())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	for _, part := range []string{"\x1b_2", "5a1;r;cp=e000;upm=1000;"} {
		writeTerm(t, term, part)
		if _, err = term.Snapshot(); !errors.Is(err, ErrSnapshot) {
			t.Fatalf("Snapshot after %q = %v, want ErrSnapshot", part, err)
		}
	}
	writeTerm(t, term, glyphTriangle+"\x1b\\\x1b_Gq=2;")
	if _, err = term.Snapshot(); err == nil || !strings.Contains(err.Error(), "glyphs registered") {
		t.Fatalf("Snapshot in a Kitty command with a glyph = %v, want the glyphs refusal", err)
	}
}

// TestSnapshotControlStringInProgress checks that a terminal restored part
// way through a string reports it when it ends.
func TestSnapshotControlStringInProgress(t *testing.T) {
	t.Parallel()
	src, err := testRuntime.Open(context.Background(), 10, 2, WithSnapshots())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	writeTerm(t, src, "\x1b]2;par")
	snap, err := src.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var got []ControlString
	dst, err := testRuntime.Open(context.Background(), 10, 2,
		WithControlStrings(func(s ControlString) { got = append(got, s) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dst.Close() })
	if err = dst.Restore(snap); err != nil {
		t.Fatal(err)
	}
	writeTerm(t, dst, "tial\x07")
	if len(got) != 1 || got[0].Data != "2;partial" || got[0].End != EndBEL {
		t.Fatalf("control strings = %+v, want the restored OSC", got)
	}
}
