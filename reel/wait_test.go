// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"getsteep.dev/steep/reel/region"
)

func TestConditions(t *testing.T) {
	t.Parallel()
	screen := ScreenText{Text: "héllo world\n$ ls ", CursorRow: 1}
	for name, tc := range map[string]struct {
		cond    Condition
		ok      bool
		matches []Match
	}{
		"contains": {
			Contains("world", "ls"), true,
			[]Match{{Text: "world", Row: 0, Col: 6}, {Text: "ls", Row: 1, Col: 2}},
		},
		"contains every": {
			Contains("l"), true,
			[]Match{{Text: "l", Row: 0, Col: 2}, {Text: "l", Row: 0, Col: 3}, {Text: "l", Row: 0, Col: 9}, {Text: "l", Row: 1, Col: 2}},
		},
		"contains in reading order": {
			Contains("ls", "o"), true,
			[]Match{{Text: "o", Row: 0, Col: 4}, {Text: "o", Row: 0, Col: 7}, {Text: "ls", Row: 1, Col: 2}},
		},
		"contains missing":  {Contains("world", "nope"), false, nil},
		"contains none":     {Contains(), true, nil},
		"not contains":      {NotContains("nope"), true, nil},
		"not contains some": {NotContains("nope", "ls"), false, nil},
		"matches":           {Matches(regexp.MustCompile(`w\w+`)), true, []Match{{Text: "world", Row: 0, Col: 6}}},
		"matches every": {
			Matches(regexp.MustCompile(`l+`)), true,
			[]Match{{Text: "ll", Row: 0, Col: 2}, {Text: "l", Row: 0, Col: 9}, {Text: "l", Row: 1, Col: 2}},
		},
		"matches not":       {Matches(regexp.MustCompile(`\d`)), false, nil},
		"not matches":       {NotMatches(regexp.MustCompile(`\d`)), true, nil},
		"not matches some":  {NotMatches(regexp.MustCompile(`l+`)), false, nil},
		"func":              {ScreenFunc(func(s string) bool { return strings.HasPrefix(s, "hé") }), true, nil},
		"cursor line":       {OnCursorLine(Contains("$ ")), true, []Match{{Text: "$ ", Row: 1, Col: 0}}},
		"cursor line other": {OnCursorLine(Contains("world")), false, nil},
	} {
		matches, ok := tc.cond.Check(screen)
		if ok != tc.ok || !reflect.DeepEqual(matches, tc.matches) {
			t.Errorf("%s: Check = %+v, %v, want %+v, %v", name, matches, ok, tc.matches, tc.ok)
		}
	}
	if _, ok := OnCursorLine(Contains("$")).Check(ScreenText{Text: "$", CursorRow: -1}); ok {
		t.Error("OnCursorLine held with the viewport scrolled off the cursor")
	}
	if _, ok := OnCursorLine(NotContains("x")).Check(ScreenText{Text: "a\nb", CursorRow: 2}); ok {
		t.Error("OnCursorLine held with the cursor past the last line")
	}
	if got := OnCursorLine(Contains("$ ")).String(); got != `contains ["$ "] on the cursor line` {
		t.Errorf("name = %q", got)
	}
}

func TestNarrowingConditions(t *testing.T) {
	t.Parallel()
	// "世" is two cells wide, so the second "more" starts at column 3.
	screen := ScreenText{Text: "ACCESS  more\n 世more\nSECRETS\n  more x", CursorRow: 3}
	more := Contains("more")
	at := func(row, col int) Match { return Match{Text: "more", Row: row, Col: col} }
	for name, tc := range map[string]struct {
		cond    Condition
		ok      bool
		matches []Match
	}{
		"nth":                {Nth(2, more), true, []Match{at(1, 3)}},
		"nth first":          {Nth(1, more), true, []Match{at(0, 8)}},
		"nth last":           {Nth(-1, more), true, []Match{at(3, 2)}},
		"nth from last":      {Nth(-3, more), true, []Match{at(0, 8)}},
		"nth too many":       {Nth(4, more), false, nil},
		"nth too many back":  {Nth(-4, more), false, nil},
		"nth not holding":    {Nth(1, Contains("more", "nope")), false, nil},
		"nth locates none":   {Nth(1, NotContains("nope")), false, nil},
		"nth reading order":  {Nth(2, Contains("SECRETS", "ACCESS")), true, []Match{{Text: "SECRETS", Row: 2, Col: 0}}},
		"after":              {After(Contains("ACCESS"), more), true, []Match{at(0, 8), at(1, 3), at(3, 2)}},
		"after later row":    {After(Contains("SECRETS"), more), true, []Match{at(3, 2)}},
		"after wide":         {After(Contains("世"), more), true, []Match{at(1, 3), at(3, 2)}},
		"after multi-row":    {After(Matches(regexp.MustCompile(`TS\n `)), more), true, []Match{at(3, 2)}},
		"after first anchor": {After(Contains("more"), Contains("x")), true, []Match{{Text: "x", Row: 3, Col: 7}}},
		"after nth":          {After(Nth(2, more), more), true, []Match{at(3, 2)}},
		"after nothing":      {After(Contains("x"), NotContains("more")), true, nil},
		"after not holding":  {After(Contains("SECRETS"), Contains("ACCESS")), false, nil},
		"after no anchor":    {After(Contains("nope"), NotContains("nope")), false, nil},
		"after anchor none":  {After(NotContains("nope"), more), false, nil},
		"below":              {Below(Contains("ACCESS"), more), true, []Match{at(1, 3), at(3, 2)}},
		"below last row":     {Below(Contains("x"), Contains(" ")), false, nil},
		"below nothing":      {Below(Contains("x"), NotContains("more")), true, nil},
		"after cursor line":  {After(Contains("SECRETS"), OnCursorLine(Contains("x"))), true, []Match{{Text: "x", Row: 3, Col: 7}}},
		"below cursor line":  {Below(Contains("x"), OnCursorLine(NotContains("nope"))), false, nil},
	} {
		matches, ok := tc.cond.Check(screen)
		if ok != tc.ok || !reflect.DeepEqual(matches, tc.matches) {
			t.Errorf("%s: Check = %+v, %v, want %+v, %v", name, matches, ok, tc.matches, tc.ok)
		}
	}

	// An anchor that ends inside a wide character leaves all of it after.
	inside := located{{Text: "x", Row: 1, Col: 1}}
	if got, ok := After(inside, Contains("世")).Check(screen); !ok || !reflect.DeepEqual(got, []Match{{Text: "世", Row: 1, Col: 1}}) {
		t.Errorf("After inside a wide character = %+v, %v", got, ok)
	}
	// A condition may return the same matches each time.
	for range 2 {
		if got, _ := Below(Contains("ACCESS"), inside).Check(screen); !reflect.DeepEqual(got, []Match{{Text: "x", Row: 2, Col: 1}}) {
			t.Errorf("Below a reused match = %+v", got)
		}
	}
	// An empty match below the last row isn't on the screen.
	if got, ok := Below(Contains("x"), Matches(regexp.MustCompile(`^$`))).Check(screen); !ok || got != nil {
		t.Errorf("Below the last row = %+v, %v", got, ok)
	}

	for _, tc := range []struct {
		cond Condition
		want string
	}{
		{Nth(2, more), `2nd match of contains ["more"]`},
		{Nth(11, more), `11th match of contains ["more"]`},
		{Nth(23, more), `23rd match of contains ["more"]`},
		{Nth(-1, more), `last match of contains ["more"]`},
		{Nth(-2, more), `2nd-last match of contains ["more"]`},
		{Nth(-112, more), `112th-last match of contains ["more"]`},
		{Nth(math.MinInt, more), strconv.FormatUint(math.MaxInt+1, 10) + `th-last match of contains ["more"]`},
		{After(Contains("ACCESS"), more), `contains ["more"] after contains ["ACCESS"]`},
		{Below(Contains("ACCESS"), more), `contains ["more"] below contains ["ACCESS"]`},
	} {
		if got, want := tc.cond.String(), tc.want; got != want {
			t.Errorf("name = %q, want %q", got, want)
		}
	}

	defer func() {
		if recover() == nil {
			t.Error("Nth(0, c) didn't panic")
		}
	}()
	Nth(0, more)
}

// located is a [Condition] that always holds, locating itself.
type located []Match

func (l located) Check(ScreenText) ([]Match, bool) { return l, true }
func (located) String() string                     { return "located" }

// writeLater writes out to s after d, in the background.
func writeLater(t *testing.T, s *Session, d time.Duration, out ...string) {
	t.Helper()
	go func() {
		for _, o := range out {
			time.Sleep(d)
			_, _ = s.Write([]byte(o))
		}
	}()
}

func TestSessionWait(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	write(t, s, "ready")
	// Already on the screen.
	got, err := s.Wait(t.Context(), Contains("ready"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []Match{{Text: "ready"}}; !reflect.DeepEqual(got.Matches, want) {
		t.Fatalf("Matches = %+v, want %+v", got.Matches, want)
	}

	screen, err := s.ScreenText()
	if err != nil || !strings.HasPrefix(screen.Text, "ready") || screen.CursorRow != 0 {
		t.Fatalf("ScreenText = %+v, %v", screen, err)
	}

	// Written after the wait starts.
	writeLater(t, s, 20*time.Millisecond, "\r\n$ ")
	got, err = s.Wait(t.Context(), OnCursorLine(Contains("$ ")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Text, "ready") || len(got.Matches) != 1 || got.Matches[0].Row != 1 {
		t.Fatalf("Wait = %+v", got)
	}
}

func TestSessionWaitTimeout(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	write(t, s, "nothing")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := s.Wait(ctx, Contains("never"))
	var werr *WaitError
	if !errors.As(err, &werr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait = %v, want a WaitError for the deadline", err)
	}
	if werr.Wait != `contains ["never"]` || !strings.Contains(werr.Last, "nothing") {
		t.Fatalf("WaitError = %+v", werr)
	}
}

func TestSessionWaitCanceled(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	cause := errors.New("stop")
	ctx, cancel := context.WithCancelCause(t.Context())
	time.AfterFunc(10*time.Millisecond, func() { cancel(cause) })
	if _, err := s.Wait(ctx, Contains("never")); !errors.Is(err, cause) {
		t.Fatalf("Wait = %v, want the context's cause", err)
	}
}

func TestSessionWaitClosed(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	time.AfterFunc(10*time.Millisecond, func() { _ = s.Close() })
	if _, err := s.Wait(t.Context(), Contains("never")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Wait = %v, want ErrClosed", err)
	}
	if _, err := s.ScreenText(); !errors.Is(err, ErrClosed) {
		t.Fatalf("ScreenText = %v, want ErrClosed", err)
	}
	if _, err := s.WaitSettle(t.Context(), time.Hour); !errors.Is(err, ErrClosed) {
		t.Fatalf("WaitSettle = %v, want ErrClosed", err)
	}
	if err := s.WaitIdle(t.Context(), 0, time.Hour, time.Hour); !errors.Is(err, ErrClosed) {
		t.Fatalf("WaitIdle = %v, want ErrClosed", err)
	}
}

func TestSessionWaitSettle(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := openTest(t, 10, 2)
		const quiet = 40 * time.Millisecond
		start := time.Now()
		// Each change restarts the quiet time, and a repaint of the same text
		// does not: the last change is at 60ms, and the repaint at 80ms.
		writeLater(t, s, 20*time.Millisecond, "a", "\rb", "\rc", "\rc")
		got, err := s.WaitSettle(t.Context(), quiet)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, "c") {
			t.Fatalf("settled on %q, want the last change", got)
		}
		if elapsed := time.Since(start); elapsed != 60*time.Millisecond+quiet {
			t.Fatalf("settled after %s, want the last change at 60ms to be quiet for %s", elapsed, quiet)
		}

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		var werr *WaitError
		if _, err = s.WaitSettle(ctx, time.Hour); !errors.As(err, &werr) || werr.Wait != "settle" {
			t.Fatalf("WaitSettle = %v, want a settle WaitError", err)
		}
	})
}

// tickClock writes a clock that ticks every 10ms to row 3 of s, as a status
// bar of "updated Ns ago" would, until the returned stop is called, which
// waits for it to. It goes from 9s to 10s on its third tick, so the text
// grows.
func tickClock(s *Session) (stop func()) {
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for i := 8; ; i++ {
			select {
			case <-done:
				return
			case <-time.After(10 * time.Millisecond):
				_, _ = fmt.Fprintf(s, "\x1b[3;1Hupdated %ds ago", i)
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

func TestSessionWaitSettleRegions(t *testing.T) {
	t.Parallel()
	const quiet = 30 * time.Millisecond
	// Each case's writes are 20ms apart, from the call, while the clock
	// ticks past them.
	moves := []string{"\x1b[1;1Hdone", "\x1b[1;1H\x1b[2K     done"}
	content := []string{"\x1b[1;1Hone", "\x1b[1;1Htwo", "\x1b[2;1Hdone"}
	for _, tc := range []struct {
		name   string
		writes []string
		opts   []SettleOption
		// want is when it settles, or 0 for a wait that ends with its
		// context, with a WaitError of wait.
		want time.Duration
		wait string
	}{
		{name: "clock never settles", writes: content, wait: "settle"},
		{name: "ignore the clock's row", writes: content, opts: []SettleOption{SettleIgnore(region.Row(-1))}, want: 60*time.Millisecond + quiet},
		{name: "ignore the clock's text", writes: content, opts: []SettleOption{SettleIgnore(region.Regexp(regexp.MustCompile(`updated \d+s ago`)))}, want: 60*time.Millisecond + quiet},
		{name: "ignore what covers nothing", writes: content, opts: []SettleOption{SettleIgnore(region.Text("nowhere"), nil)}, wait: "settle"},
		{name: "only a row", writes: content, opts: []SettleOption{SettleOnly(region.Row(0))}, want: 40*time.Millisecond + quiet},
		{name: "only rows, ignoring some", writes: content, opts: []SettleOption{SettleOnly(region.Rows(0, 2)), SettleIgnore(region.Text("done"))}, want: 40*time.Millisecond + quiet},
		{name: "options add up", writes: content, opts: []SettleOption{SettleIgnore(region.Row(1)), SettleIgnore(region.Row(2))}, want: 40*time.Millisecond + quiet},
		// The quiet time starts when the text shows.
		{name: "only text, once it shows", writes: content, opts: []SettleOption{SettleOnly(region.Text("done"))}, want: 60*time.Millisecond + quiet},
		// A watched region that moves has changed.
		{name: "only text that moves", writes: moves, opts: []SettleOption{SettleOnly(region.Text("done"))}, want: 40*time.Millisecond + quiet},
		{name: "only text that never shows", writes: content, opts: []SettleOption{SettleOnly(region.Text("never"))}, wait: "settle, with the regions it watches not on the screen"},
		// SettleOnly of nothing watches everything.
		{name: "only no regions", writes: content, opts: []SettleOption{SettleOnly(), SettleOnly(nil)}, wait: "settle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				s := openTest(t, 20, 3)
				ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
				defer cancel()
				start := time.Now()
				defer tickClock(s)()
				writeLater(t, s, 20*time.Millisecond, tc.writes...)
				got, err := s.WaitSettle(ctx, quiet, tc.opts...)
				elapsed := time.Since(start)
				if tc.want == 0 {
					var werr *WaitError
					if !errors.As(err, &werr) || werr.Wait != tc.wait {
						t.Fatalf("WaitSettle = %q, %v, want a WaitError for %q", got, err, tc.wait)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if elapsed != tc.want {
					t.Fatalf("settled after %s, want %s", elapsed, tc.want)
				}
				if !strings.Contains(got, "done") || !strings.Contains(got, "updated") {
					t.Fatalf("settled on %q, want the whole screen", got)
				}
			})
		})
	}
}

// TestSessionWaitSettleResize checks that a resize changes a settle on
// part of the screen only when it changes the part watched.
func TestSessionWaitSettleResize(t *testing.T) {
	t.Parallel()
	const quiet = 30 * time.Millisecond
	for _, tc := range []struct {
		name string
		opts []SettleOption
		want time.Duration
	}{
		// Rows added below the watched row leave it as it was.
		{"only a row it leaves", []SettleOption{SettleOnly(region.Row(0))}, quiet},
		// Without SettleOnly, the new rows are watched, so they changed.
		{"the whole screen", []SettleOption{SettleIgnore(region.Text("nowhere"))}, 20*time.Millisecond + quiet},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				s := openTest(t, 10, 2)
				write(t, s, "top")
				start := time.Now()
				go func() {
					time.Sleep(20 * time.Millisecond)
					_ = s.Resize(10, 4)
				}()
				if _, err := s.WaitSettle(t.Context(), quiet, tc.opts...); err != nil {
					t.Fatal(err)
				}
				if elapsed := time.Since(start); elapsed != tc.want {
					t.Fatalf("settled after %s, want %s", elapsed, tc.want)
				}
			})
		})
	}
}

func TestSessionWaitIdle(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := openTest(t, 10, 2)

		// No output: it gives up at the limit.
		start := time.Now()
		since := s.Terminal().OutputSeq()
		if err := s.WaitIdle(t.Context(), since, time.Hour, 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed != 20*time.Millisecond {
			t.Fatalf("WaitIdle with no output took %s, want the limit", elapsed)
		}

		// Output already written counts, and further output restarts the quiet
		// time, past the limit.
		write(t, s, "a")
		writeLater(t, s, 15*time.Millisecond, "b", "c")
		start = time.Now()
		if err := s.WaitIdle(t.Context(), since, 30*time.Millisecond, 5*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed != 60*time.Millisecond {
			t.Fatalf("WaitIdle returned after %s, want the last output at 30ms to be quiet for 30ms", elapsed)
		}
		if text := s.Terminal().Text(); !strings.HasPrefix(text, "abc") {
			t.Fatalf("screen = %q, want all the output", text)
		}

		// Output held by synchronized output that never ends is not shown, so
		// the limit still applies.
		held := s.Terminal().OutputSeq()
		write(t, s, "\x1b[?2026hhidden")
		start = time.Now()
		if err := s.WaitIdle(t.Context(), held, time.Hour, 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed != 20*time.Millisecond {
			t.Fatalf("WaitIdle on held output took %s, want the limit", elapsed)
		}
		write(t, s, "\x1b[?2026l")

		// Synchronized output holds the screen, so it is not idle.
		since = s.Terminal().OutputSeq()
		write(t, s, "\x1b[?2026h")
		writeLater(t, s, 50*time.Millisecond, "\x1b[?2026l")
		start = time.Now()
		if err := s.WaitIdle(t.Context(), since, 5*time.Millisecond, time.Hour); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed != 55*time.Millisecond {
			t.Fatalf("WaitIdle returned after %s, want the hold to end at 50ms and be quiet for 5ms", elapsed)
		}
	})
}
