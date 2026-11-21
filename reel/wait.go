// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"context"
	"fmt"
	"regexp"
	"slices"
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
// reel provides are [Contains], [NotContains], [Matches], [NotMatches],
// [ScreenFunc], and [OnCursorLine].
type Condition interface {
	// Check reports whether screen satisfies the condition, and where on
	// it the condition found what it looked for, if it locates anything.
	Check(screen ScreenText) (matches []Match, ok bool)
	// String describes the condition, such as `contains ["$ "]`, for a
	// [WaitError].
	String() string
}

// Contains is a [Condition] that holds when every one of texts is on the
// screen. Its matches are where each first appears.
func Contains(texts ...string) Condition { return contains{texts: texts, present: true} }

// NotContains is a [Condition] that holds when none of texts is on the
// screen.
func NotContains(texts ...string) Condition { return contains{texts: texts} }

type contains struct {
	texts   []string
	present bool
}

func (c contains) Check(screen ScreenText) ([]Match, bool) {
	var matches []Match
	for _, text := range c.texts {
		i := strings.Index(screen.Text, text)
		if (i >= 0) != c.present {
			return nil, false
		}
		if i >= 0 && text != "" {
			matches = append(matches, matchAt(screen.Text, i, i+len(text)))
		}
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

// Matches is a [Condition] that holds when re matches the screen. Its match
// is where re first matches.
func Matches(re *regexp.Regexp) Condition { return pattern{re: re, match: true} }

// NotMatches is a [Condition] that holds when re does not match the screen.
func NotMatches(re *regexp.Regexp) Condition { return pattern{re: re} }

type pattern struct {
	re    *regexp.Regexp
	match bool
}

func (p pattern) Check(screen ScreenText) ([]Match, bool) {
	loc := p.re.FindStringIndex(screen.Text)
	if (loc != nil) != p.match {
		return nil, false
	}
	if loc == nil {
		return nil, true
	}
	return []Match{matchAt(screen.Text, loc[0], loc[1])}, true
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
	line := screen.Text
	for range screen.CursorRow {
		var ok bool
		if _, line, ok = strings.Cut(line, "\n"); !ok {
			return nil, false
		}
	}
	line, _, _ = strings.Cut(line, "\n")
	matches, ok := l.c.Check(ScreenText{Text: line})
	for i := range matches {
		matches[i].Row += screen.CursorRow
	}
	return matches, ok
}

func (l cursorLine) String() string { return l.c.String() + " on the cursor line" }

// matchAt returns the match of text[i:j].
func matchAt(text string, i, j int) Match {
	line := strings.LastIndexByte(text[:i], '\n') + 1
	return Match{
		Text: text[i:j],
		Row:  strings.Count(text[:line], "\n"),
		Col:  ansi.StringWidth(text[line:i]),
	}
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
// s.mu, so the terminal has every recorded event's bytes.
func (s *Session) readScreen(r *screenRead, withScreen bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return false, ErrClosed
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

// ScreenText returns the screen as [Session.Wait] checks it, such as to
// check a [Condition] once. It returns [ErrClosed] after [Session.Close].
func (s *Session) ScreenText() (ScreenText, error) {
	var r screenRead
	_, err := s.readScreen(&r, false)
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
		changed, err := s.readScreen(&r, false)
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
// was, such as a repaint, do not count. It sees changes as [Session.Wait]
// does, and returns a [*WaitError] when ctx is done or the session closes
// first.
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
	err := s.await(ctx, timer, func(fired bool) (bool, error) {
		was := view
		changed, err := s.readScreen(&view.screenRead, withScreen)
		if err != nil || !changed {
			return fired && watch.shows(view), err
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
