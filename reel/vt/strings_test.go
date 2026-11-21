// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWithControlStrings(t *testing.T) {
	t.Parallel()
	var got []ControlString
	now := 3 * time.Second
	term, err := testRuntime.Open(context.Background(), 20, 2,
		WithClock(func() time.Duration { return now }),
		WithControlStrings(func(s ControlString) { got = append(got, s) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	long := strings.Repeat("y", ControlStringMaxBytes+5)
	writes := []string{
		"a\x1b]2;title\x07b",
		"\x1b]8;;https://e.x\x1b\\link\x1b]8;;\x1b\\",
		"\x1b]7400;sp", "lit\x07", // A string split across writes.
		"\x1bP1;2|dcs data\x1b\\",
		"\x1b_Gq=2;\x1b\\",
		"\x1bXsos\x1b\\\x1b^pm\x1b\\",
		"\x1b]7401;gone\x18",
		"\x1b_" + long + "\x1b\\",
		"\x1b_apc\x9c",        // The 8-bit ST ends an APC.
		"\x1b]2;esc\x1b[m",    // ESC ends it, without a backslash.
		"\x1b]2;st\x1b", "\\", // ST split across writes.
	}
	for _, w := range writes {
		if _, err = term.Write([]byte(w)); err != nil {
			t.Fatal(err)
		}
	}
	want := []ControlString{
		{Kind: SequenceOSC, Data: "2;title", End: EndBEL, At: now},
		{Kind: SequenceOSC, Data: "8;;https://e.x", End: EndST, At: now},
		{Kind: SequenceOSC, Data: "8;;", End: EndST, At: now},
		{Kind: SequenceOSC, Data: "7400;split", End: EndBEL, At: now},
		{Kind: SequenceDCS, Data: "1;2|dcs data", End: EndST, At: now},
		{Kind: SequenceAPC, Data: "Gq=2;", End: EndST, At: now},
		{Kind: SequenceSOS, Data: "sos", End: EndST, At: now},
		{Kind: SequencePM, Data: "pm", End: EndST, At: now},
		{Kind: SequenceOSC, Data: "7401;gone", End: EndCancelled, At: now},
		{Kind: SequenceAPC, Data: long[:ControlStringMaxBytes], Truncated: true, End: EndST, At: now},
		{Kind: SequenceAPC, Data: "apc", End: EndSTC1, At: now},
		{Kind: SequenceOSC, Data: "2;esc", End: EndESC, At: now},
		{Kind: SequenceOSC, Data: "2;st", End: EndST, At: now},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("control strings:\n got %+v\nwant %+v", got, want)
	}
	// The strings ran as they would have without stops.
	if title := term.Title(); title != "st" {
		t.Fatalf("title = %q, want %q", title, "st")
	}
	if text := term.Text(); !strings.HasPrefix(text, "ablink") {
		t.Fatalf("text = %q, want it to start with %q", text, "ablink")
	}
	if cell := term.Screen().CellAt(2, 0); cell.Link.URL != "https://e.x" {
		t.Fatalf("link = %q", cell.Link.URL)
	}
}

func TestUnknownDCS(t *testing.T) {
	t.Parallel()
	var got []UnknownSequence
	term, err := testRuntime.Open(context.Background(), 20, 2,
		WithClock(func() time.Duration { return time.Second }),
		WithUnknownSequence(func(s UnknownSequence) { got = append(got, s) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	reply, err := term.WriteReplies([]byte(
		"\x1bP1;2|unknown\x1b\\" +
			"\x1bP+q544e\x1b\\" + // XTGETTCAP is implemented.
			"\x1bP$qm\x1b\\" + // So is DECRQSS.
			"\x1bPq#0;2;0;0;0\x1b\\" + // Sixel is not.
			"\x1bPcut\x18" + // CAN cancels it, so it does not run.
			"\x1b]7400;osc\x07",
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(reply) == 0 {
		t.Fatal("no replies to XTGETTCAP and DECRQSS")
	}
	want := []UnknownSequence{
		{Kind: SequenceDCS, Data: "1;2|unknown", At: time.Second},
		{Kind: SequenceDCS, Data: "q#0;2;0;0;0", At: time.Second},
		{Kind: SequenceOSC, Data: "7400;osc", At: time.Second},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unknown sequences:\n got %+v\nwant %+v", got, want)
	}
}

func TestKnownDCS(t *testing.T) {
	t.Parallel()
	for data, want := range map[string]bool{
		"+q544e":   true,
		"$qm":      true,
		"1$q\"p":   true,
		"q":        false,
		"1000p":    false,
		"+p":       false,
		"++q":      false,
		"":         false,
		"1;2|data": false,
	} {
		if got := knownDCS([]byte(data)); got != want {
			t.Errorf("knownDCS(%q) = %v, want %v", data, got, want)
		}
	}
}

func TestInSequence(t *testing.T) {
	t.Parallel()
	term := openTest(t, 20, 2)
	for _, step := range []struct {
		write string
		want  bool
	}{
		{"", false},
		{"plain", false},
		{"\x1b]2;par", true},
		{"tial\x07", false},
		{"\x1b[3", true},
		{"1m", false},
		{"\xe2\x82", true}, // Two bytes of a three-byte character.
		{"\xac", false},
		{"\x1b", true},
		{"\\", false},
	} {
		if _, err := term.Write([]byte(step.write)); err != nil {
			t.Fatal(err)
		}
		if got := term.InSequence(); got != step.want {
			t.Fatalf("after %q: InSequence = %v, want %v", step.write, got, step.want)
		}
	}
	// The query writes nothing: the split OSC still lands.
	if term.Title() != "partial" {
		t.Fatalf("title = %q, want %q", term.Title(), "partial")
	}
}
