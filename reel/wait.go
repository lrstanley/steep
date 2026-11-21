// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"getsteep.dev/steep/reel/region"
	"getsteep.dev/steep/reel/vt"
	"github.com/charmbracelet/x/ansi"
)

// ScreenText is the screen a [Condition] checks.
type ScreenText struct {
	// Text is the viewport's text, as [vt.Terminal.Text] returns it: one
	// line per row.
	Text string
	// CursorRow is the cursor's row in Text, or -1 when there is none, such
	// as when the viewport is scrolled off it or the text is not a
	// terminal's.
	CursorRow int
}

// Match is where a [Condition] found what it looked for on the screen.
type Match struct {
	// Text is the text found.
	Text string `json:"text"`
	// Row and Col are where it starts, from 0, with Col in cells.
	Row int `json:"row"`
	Col int `json:"col"`
}

// Condition is what [Session.Wait] waits for on the screen. The conditions
// reel provides are [Contains], [NotContains], [Matches], [NotMatches], and
// [ScreenFunc]. [OnCursorLine], [Nth], [After], and [Below] narrow another
// condition.
type Condition interface {
	// Check reports whether screen satisfies the condition, and where on
	// it the condition found what it looked for, if it locates anything,
	// in reading order: by row, then by column.
	Check(screen ScreenText) (matches []Match, ok bool)
	// String describes the condition, such as `contains ["$ "]`, for a
	// [WaitError].
	String() string
}

// Contains is a [Condition] that holds when every one of texts is on the
// screen. Its matches are every place each appears, as [strings.Count]
// counts them: "aa" is once in "aaa".
func Contains(texts ...string) Condition { return contains{texts: texts, present: true} }

// NotContains is a [Condition] that holds when none of texts is on the
// screen.
func NotContains(texts ...string) Condition { return contains{texts: texts} }

type contains struct {
	texts   []string
	present bool
}

func (c contains) Check(screen ScreenText) ([]Match, bool) {
	for _, text := range c.texts {
		if strings.Contains(screen.Text, text) != c.present {
			return nil, false
		}
	}
	if !c.present {
		return nil, true
	}
	var matches []Match
	for _, text := range c.texts {
		loc := locator{text: screen.Text}
		for i := 0; text != ""; i += len(text) {
			j := strings.Index(screen.Text[i:], text)
			if j < 0 {
				break
			}
			i += j
			matches = append(matches, loc.match(i, i+len(text)))
		}
	}
	if len(c.texts) > 1 {
		slices.SortStableFunc(matches, compareMatches)
	}
	return matches, true
}

func (c contains) String() string {
	name := "contains"
	if !c.present {
		name = "not contains"
	}
	return fmt.Sprintf("%s %q", name, c.texts)
}

// Matches is a [Condition] that holds when re matches the screen. Its
// matches are every place re matches, as [regexp.Regexp.FindAllString]
// finds them.
func Matches(re *regexp.Regexp) Condition { return pattern{re: re, match: true} }

// NotMatches is a [Condition] that holds when re does not match the screen.
func NotMatches(re *regexp.Regexp) Condition { return pattern{re: re} }

type pattern struct {
	re    *regexp.Regexp
	match bool
}

func (p pattern) Check(screen ScreenText) ([]Match, bool) {
	if !p.match {
		return nil, !p.re.MatchString(screen.Text)
	}
	spans := p.re.FindAllStringIndex(screen.Text, -1)
	if spans == nil {
		return nil, false
	}
	loc := locator{text: screen.Text}
	matches := make([]Match, len(spans))
	for k, span := range spans {
		matches[k] = loc.match(span[0], span[1])
	}
	return matches, true
}

func (p pattern) String() string {
	if p.match {
		return fmt.Sprintf("matches %q", p.re)
	}
	return fmt.Sprintf("not matches %q", p.re)
}

// ScreenFunc is a [Condition] that holds when fn returns true for the
// screen's text. It locates nothing. Like every condition, it is checked
// only when the screen changes, so fn should depend on the text alone.
func ScreenFunc(fn func(text string) bool) Condition { return screenFunc(fn) }

type screenFunc func(text string) bool

func (f screenFunc) Check(screen ScreenText) ([]Match, bool) { return nil, f(screen.Text) }

func (screenFunc) String() string { return "screen func" }

// OnCursorLine is a [Condition] that holds when c holds for the cursor's
// line alone, such as to wait for a prompt. It does not hold while the
// viewport is scrolled off the cursor.
func OnCursorLine(c Condition) Condition { return cursorLine{c: c} }

type cursorLine struct{ c Condition }

func (l cursorLine) Check(screen ScreenText) ([]Match, bool) {
	if screen.CursorRow < 0 {
		return nil, false
	}
	line, ok := rowsFrom(screen.Text, screen.CursorRow)
	if !ok {
		return nil, false
	}
	line, _, _ = strings.Cut(line, "\n")
	matches, ok := l.c.Check(ScreenText{Text: line})
	return shift(matches, screen.CursorRow, 0), ok
}

func (l cursorLine) String() string { return l.c.String() + " on the cursor line" }

// Nth is a [Condition] that holds when c holds and locates at least n
// things, so a wait for it waits for the nth to appear. It counts c's
// matches in reading order, from 1, or back from the last when n is
// negative, so Nth(-1, c) is c's last match. Its match is that one. It
// panics if n is 0.
//
//	reel.Nth(2, reel.Contains("revoke")) // the second "revoke"
func Nth(n int, c Condition) Condition {
	if n == 0 {
		panic("reel: Nth counts from 1")
	}
	return nth{n: n, c: c}
}

type nth struct {
	n int
	c Condition
}

func (m nth) Check(screen ScreenText) ([]Match, bool) {
	matches, ok := m.c.Check(screen)
	if !ok {
		return nil, false
	}
	i := m.n - 1
	if m.n < 0 {
		i = len(matches) + m.n
	}
	if i < 0 || i >= len(matches) {
		return nil, false
	}
	return []Match{matches[i]}, true
}

func (m nth) String() string { return fmt.Sprintf("%s match of %s", ordinal(m.n), m.c) }

// ordinal returns n as Nth counts: "2nd", or "last" and "2nd-last" when n
// is negative.
func ordinal(n int) string {
	if n == -1 {
		return "last"
	}
	digits := strings.TrimPrefix(strconv.Itoa(n), "-")
	last, _ := strconv.Atoi(digits[max(0, len(digits)-2):])
	suffix := "th"
	if last < 11 || last > 13 {
		switch last % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	if n < 0 {
		return digits + suffix + "-last"
	}
	return digits + suffix
}

// After is a [Condition] that holds when c holds for the part of the
// screen after where anchor's first match ends, in reading order: the rest
// of that row, and the rows below it. It does not hold while anchor does
// not locate anything, so a wait for it waits for both. Its matches are
// what c locates, where they are on the whole screen.
//
//	// Every "more" past the ACCESS heading, the first of which Locate finds.
//	reel.After(reel.Contains("ACCESS"), reel.Contains("more"))
func After(anchor, c Condition) Condition { return past{anchor: anchor, c: c} }

// Below is [After] from the row below the one anchor's first match ends
// on, leaving out the rest of that row.
func Below(anchor, c Condition) Condition { return past{anchor: anchor, c: c, below: true} }

type past struct {
	anchor, c Condition
	below     bool
}

func (r past) Check(screen ScreenText) ([]Match, bool) {
	anchors, ok := r.anchor.Check(screen)
	if !ok || len(anchors) == 0 {
		return nil, false
	}
	row, col := matchEnd(anchors[0])
	if r.below {
		row, col = row+1, 0
	}
	text, onScreen := rowsFrom(screen.Text, row)
	line, _, _ := strings.Cut(text, "\n")
	// Where col is inside a wide character, the part starts at it.
	prefix := ansi.Truncate(line, col, "")
	cursor := screen.CursorRow - row
	if screen.CursorRow < 0 || cursor < 0 {
		cursor = -1
	}
	matches, ok := r.c.Check(ScreenText{Text: text[len(prefix):], CursorRow: cursor})
	if !onScreen {
		// Below the last row, c sees no text, and finds nothing on the screen.
		return nil, ok
	}
	return shift(matches, row, ansi.StringWidth(prefix)), ok
}

func (r past) String() string {
	where := "after"
	if r.below {
		where = "below"
	}
	return fmt.Sprintf("%s %s %s", r.c, where, r.anchor)
}

// matchEnd returns the row and column, in cells, just past where m ends.
func matchEnd(m Match) (row, col int) {
	i := strings.LastIndexByte(m.Text, '\n')
	if i < 0 {
		return m.Row, m.Col + ansi.StringWidth(m.Text)
	}
	return m.Row + strings.Count(m.Text, "\n"), ansi.StringWidth(m.Text[i+1:])
}

// rowsFrom returns text from the start of row on, and whether text has that
// row.
func rowsFrom(text string, row int) (string, bool) {
	for range row {
		var ok bool
		if _, text, ok = strings.Cut(text, "\n"); !ok {
			return "", false
		}
	}
	return text, true
}

// shift returns matches found in part of a screen, whose first row starts
// at row and col of the screen, moved to where they are on the screen. It
// copies them, since a condition may return the same matches each time.
func shift(matches []Match, row, col int) []Match {
	matches = slices.Clone(matches)
	for i := range matches {
		if matches[i].Row == 0 {
			matches[i].Col += col
		}
		matches[i].Row += row
	}
	return matches
}

// compareMatches orders matches in reading order: by row, then by column.
func compareMatches(a, b Match) int {
	return cmp.Or(cmp.Compare(a.Row, b.Row), cmp.Compare(a.Col, b.Col))
}

// locator makes matches of text at ascending byte offsets, counting rows
// from one match to the next rather than from the start of text each time.
type locator struct {
	text            string
	row, line, last int
}

// match returns the match of text[i:j], where i is at or past the last
// match's start.
func (l *locator) match(i, j int) Match {
	if n := strings.Count(l.text[l.last:i], "\n"); n > 0 {
		l.row += n
		l.line = strings.LastIndexByte(l.text[:i], '\n') + 1
	}
	l.last = i
	return Match{Text: l.text[i:j], Row: l.row, Col: ansi.StringWidth(l.text[l.line:i])}
}

// WaitResult is the screen a [Session.Wait] condition held on.
type WaitResult struct {
	// Text is the screen's text.
	Text string
	// Matches is where the condition found what it looked for.
	Matches []Match
}

// WaitError is the error of a wait that ended before what it waited for,
// because its context was done or the session closed.
type WaitError struct {
	// Wait names what was waited for, such as `contains ["$ "]`, "settle",
	// or "idle". A settle that ends while the regions of [SettleOnly] are
	// not on the screen says so.
	Wait string
	// Last is the screen text the wait saw last, empty for "idle".
	Last string
	// Err is why the wait ended: the cause of its context, or [ErrClosed].
	Err error
}

func (e *WaitError) Error() string { return "reel: wait for " + e.Wait + ": " + e.Err.Error() }
func (e *WaitError) Unwrap() error { return e.Err }

// screenRead is the screen as a wait last read it.
type screenRead struct {
	ScreenText

	read bool
	gen  uint64
	// screen is a copy of the screen, read in place of the text for a wait
	// that matches regions on it.
	screen *vt.Buffer
}

// readScreen updates r from the terminal, reading the text, or with
// withScreen a copy of the screen in place of it, only when the grid or
// cursor row changed since r was read, and reports whether it did. It holds
// s.mu, so the terminal has every recorded event's bytes. out, if not nil,
// is updated from the same read.
func (s *Session) readScreen(r *screenRead, withScreen bool, out *outputMark) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return false, ErrClosed
	}
	if out != nil {
		seq := s.term.OutputSeq()
		out.partial = seq != out.seq && s.term.InSequence()
		out.seq = seq
	}
	gen, row := s.term.GridGeneration(), s.term.Cursor().Y
	if r.read && gen == r.gen && row == r.CursorRow {
		return false, nil
	}
	*r = screenRead{CursorRow: row, read: true, gen: gen}
	if withScreen {
		r.screen = s.term.Screen()
	} else {
		r.Text = s.term.Text()
	}
	return true, nil
}

// outputMark is the output count a read last saw, and whether the output
// since the one before it ends inside an escape sequence.
type outputMark struct {
	seq     uint64
	partial bool
}

// ScreenText returns the screen as [Session.Wait] checks it, such as to
// check a [Condition] once. It returns [ErrClosed] after [Session.Close].
func (s *Session) ScreenText() (ScreenText, error) {
	var r screenRead
	_, err := s.readScreen(&r, false, nil)
	return r.ScreenText, err
}

// await runs step now, and again after each event appended to the
// timeline and each time timer (if any) fires, until step reports done or
// fails, the session closes (which step sees as [ErrClosed]), or ctx is
// done, returning its cause. step learns whether the timer woke it.
func (s *Session) await(ctx context.Context, timer *time.Timer, step func(fired bool) (bool, error)) error {
	var tick <-chan time.Time
	if timer != nil {
		tick = timer.C
	}
	fired := false
	for {
		updated := s.timeline.Updated()
		if done, err := step(fired); done || err != nil {
			return err
		}
		fired = false
		select {
		case <-updated:
		case <-s.done:
		case <-tick:
			fired = true
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

// Wait waits until cond holds for the screen, and returns the screen it
// held on. It checks the screen when called and again whenever an event
// recorded through the session changes it; changes made through
// [Session.Terminal] are not seen until the next event. It returns a
// [*WaitError] when ctx is done or the session closes first; pass a context
// with a deadline to time out.
func (s *Session) Wait(ctx context.Context, cond Condition) (WaitResult, error) {
	var r screenRead
	var res WaitResult
	err := s.await(ctx, nil, func(bool) (bool, error) {
		changed, err := s.readScreen(&r, false, nil)
		if err != nil || !changed {
			return false, err
		}
		matches, ok := cond.Check(r.ScreenText)
		res = WaitResult{Text: r.Text, Matches: matches}
		return ok, nil
	})
	if err != nil {
		return WaitResult{}, &WaitError{Wait: cond.String(), Last: r.Text, Err: err}
	}
	return res, nil
}

// WaitSettle waits until the screen's text has not changed for quiet,
// counting from the call, and returns it. Changes that leave the text as it
// was, such as a repaint, do not count. Output that leaves the terminal
// inside an escape sequence does, so a screen is not taken as settled half
// way through a sequence the program is still writing, such as a Kitty image
// sent in several writes; once quiet passes with no more of it, the wait
// ends all the same. It sees changes as [Session.Wait] does, and returns a
// [*WaitError] when ctx is done or the session closes first.
//
// [SettleIgnore] leaves out changes to part of the screen, such as a clock,
// and [SettleOnly] watches only part of it.
func (s *Session) WaitSettle(ctx context.Context, quiet time.Duration, opts ...SettleOption) (string, error) {
	var o settleOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	watch := settleWatch{ignore: anyRegion(o.ignore), only: anyRegion(o.only)}
	withScreen := watch.ignore != nil || watch.only != nil
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	// view is the screen as last read, with the cells watch covers on it.
	var view settleView
	// output is what the last read saw of the program's output.
	var output outputMark
	err := s.await(ctx, timer, func(fired bool) (bool, error) {
		was := view
		changed, err := s.readScreen(&view.screenRead, withScreen, &output)
		if err != nil {
			return false, err
		}
		if output.partial {
			timer.Reset(quiet)
			fired = false
		}
		if !changed {
			return fired && watch.shows(view), nil
		}
		watch.cover(&view)
		switch {
		case !watch.shows(view):
			// Nothing to watch is not settled, so the quiet time starts
			// once something shows.
			timer.Stop()
			return false, nil
		case was.read && (!watch.shows(was) || view.differs(was)):
			timer.Reset(quiet)
			return false, nil
		}
		return fired, nil
	})
	text := view.Text
	if withScreen {
		text = view.screen.Text()
	}
	if err != nil {
		wait := "settle"
		if view.read && !watch.shows(view) {
			wait = "settle, with the regions it watches not on the screen"
		}
		return "", &WaitError{Wait: wait, Last: text, Err: err}
	}
	return text, nil
}

// SettleOption is an option of [Session.WaitSettle].
type SettleOption func(*settleOptions)

// settleOptions are the regions of SettleIgnore and SettleOnly.
type settleOptions struct{ ignore, only []region.Region }

// SettleIgnore makes [Session.WaitSettle] leave out changes to the cells
// any of regions covers, such as [region.Row](-1) for a status bar with a
// clock in it, or [region.Regexp] of the clock itself. A region that covers
// nothing leaves out nothing. Calls add to the regions.
func SettleIgnore(regions ...region.Region) SettleOption {
	return func(o *settleOptions) { o.ignore = append(o.ignore, regions...) }
}

// SettleOnly makes [Session.WaitSettle] watch only the cells any of regions
// covers, such as [region.Text] of a result, for changes, and still leave
// out those [SettleIgnore] covers. The screen is not settled while the
// regions cover no cell of it: the quiet time starts once they cover some,
// and starts over each time they come back, so a wait for a region that
// never shows ends with ctx. Calls add to the regions.
func SettleOnly(regions ...region.Region) SettleOption {
	return func(o *settleOptions) { o.only = append(o.only, regions...) }
}

// anyRegion returns [region.Any] of regions, or nil when they are all nil.
func anyRegion(regions []region.Region) region.Region {
	if !slices.ContainsFunc(regions, func(r region.Region) bool { return r != nil }) {
		return nil
	}
	return region.Any(regions...)
}

// settleWatch is the part of the screen a [Session.WaitSettle] watches: all
// of it but what ignore covers, or only what only covers, when not nil.
type settleWatch struct{ ignore, only region.Region }

// settleView is the screen as a [Session.WaitSettle] read it, with the
// cells the SettleOnly and SettleIgnore regions cover on its screen, nil
// when they cover none, or there are none.
type settleView struct {
	screenRead

	only, ignored []bool
}

// cover finds the cells w covers on the screen v read.
func (w settleWatch) cover(v *settleView) {
	v.only, v.ignored = nil, nil
	if v.screen != nil {
		v.only, v.ignored = region.Covered(v.screen, w.only), region.Covered(v.screen, w.ignore)
	}
}

// shows reports whether any of what w watches is on the screen v read:
// without SettleOnly regions, all of it is.
func (w settleWatch) shows(v settleView) bool { return w.only == nil || v.only != nil }

// differs reports whether v has changed from was, comparing their cells by
// where they are. A cell either ignores is left out, so ignored text that
// shows, or grows, does not change it. A cell one watches and the other
// does not, as when a [SettleOnly] region moves or the screen is resized,
// has changed.
func (v settleView) differs(was settleView) bool {
	if v.screen == nil || was.screen == nil {
		return v.Text != was.Text
	}
	vb, wb := v.screen.Bounds(), was.screen.Bounds()
	for y := range max(vb.Dy(), wb.Dy()) {
		for x := range max(vb.Dx(), wb.Dx()) {
			if v.ignores(x, y) || was.ignores(x, y) {
				continue
			}
			watched := v.watches(x, y)
			if watched != was.watches(x, y) || (watched && v.content(x, y) != was.content(x, y)) {
				return true
			}
		}
	}
	return false
}

// cell returns the index of the cell at x, y in v's grids, and whether v's
// screen has it.
func (v settleView) cell(x, y int) (int, bool) {
	b := v.screen.Bounds()
	return y*b.Dx() + x, x < b.Dx() && y < b.Dy()
}

// ignores reports whether v's SettleIgnore regions cover the cell at x, y.
func (v settleView) ignores(x, y int) bool {
	i, ok := v.cell(x, y)
	return ok && len(v.ignored) > i && v.ignored[i]
}

// watches reports whether v watches the cell at x, y: whether its screen
// has it, and without SettleOnly regions, it watches every cell.
func (v settleView) watches(x, y int) bool {
	i, ok := v.cell(x, y)
	return ok && (len(v.only) == 0 || v.only[i])
}

// content returns the text of the cell at x, y, which v's screen has.
func (v settleView) content(x, y int) string {
	b := v.screen.Bounds()
	return v.screen.CellAt(b.Min.X+x, b.Min.Y+y).Content
}

// outputState returns the terminal's output count and whether
// synchronized output holds its screen, under s.mu.
func (s *Session) outputState() (seq uint64, held bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return 0, false, ErrClosed
	}
	return s.term.OutputSeq(), s.term.Held(), nil
}

// WaitIdle waits for the program to finish responding to something, such
// as input: until the terminal has shown output newer than since (a
// [vt.Terminal.OutputSeq] read before sending it), and then no more output
// for quiet. Output written while synchronized output holds the screen
// counts once the hold ends. With no output shown newer than since after
// limit, it returns nil without waiting longer; once there is, only quiet
// ends it. It returns a [*WaitError] when ctx is done or the session closes
// first.
func (s *Session) WaitIdle(ctx context.Context, since uint64, quiet, limit time.Duration) error {
	// The timer runs to limit until there is output, then to quiet after
	// each output.
	timer := time.NewTimer(limit)
	defer timer.Stop()
	output := false
	err := s.await(ctx, timer, func(fired bool) (bool, error) {
		seq, held, err := s.outputState()
		if err != nil {
			return false, err
		}
		if seq != since && !held {
			since, output = seq, true
			timer.Reset(quiet)
			return false, nil
		}
		return fired && (!output || !held), nil
	})
	if err != nil {
		return &WaitError{Wait: "idle", Err: err}
	}
	return nil
}
