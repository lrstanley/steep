// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"strings"
	"testing"
)

func TestRapidBlink(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 1)
	if _, err := term.Write([]byte("\x1b[5ma\x1b[6mb\x1b[5mc\x1b[25md\x1b[6me")); err != nil {
		t.Fatal(err)
	}
	scr := term.Screen()
	for i, want := range []Attr{AttrBlink, AttrRapidBlink, AttrBlink, 0, AttrRapidBlink} {
		if got := scr.CellAt(i, 0).Style.Attrs; got != want {
			t.Errorf("cell %d attrs = %b, want %b", i, got, want)
		}
	}
}

// TestRapidBlinkQuery checks that what a program reads back restores it.
func TestRapidBlinkQuery(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 1)
	replies, err := term.WriteReplies([]byte("\x1b[6m\x1bP$qm\x1b\\"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "\x1bP1$r0;6m\x1b\\"; string(replies) != want {
		t.Errorf("DECRQSS = %q, want %q", replies, want)
	}
}

func TestLinkParams(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 1)
	long := "https://example.com/" + strings.Repeat("x", 300)
	id := strings.Repeat("i", 200)
	if _, err := term.Write([]byte(
		"\x1b]8;id=one;https://a.example\x07a\x1b]8;;\x07" +
			"\x1b]8;;https://b.example\x07b\x1b]8;;\x07" +
			"\x1b]8;id=" + id + ";" + long + "\x07c\x1b]8;;\x07d",
	)); err != nil {
		t.Fatal(err)
	}
	scr := term.Screen()
	for i, want := range []Link{
		{URL: "https://a.example", Params: "id=one"},
		{URL: "https://b.example"},
		{URL: long, Params: "id=" + id},
		{},
	} {
		if got := scr.CellAt(i, 0).Link; got != want {
			t.Errorf("cell %d link = %+v, want %+v", i, got, want)
		}
	}
}
