// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"testing"

	"getsteep.dev/steep/internal/xansi"
	"getsteep.dev/steep/reel"
)

// Viewable returns the output the package-level assertions check, such as
// [Harness.View].
type Viewable func() string

func texts[T string | []byte | []string](contents T) []string {
	switch c := any(contents).(type) {
	case string:
		return []string{c}
	case []byte:
		return []string{string(c)}
	case []string:
		return append([]string{}, c...)
	default:
		panic("unreachable")
	}
}

func renderedView(view Viewable, stripANSI bool) string {
	out := view()
	if stripANSI {
		out = xansi.StripANSI(out)
	}
	return out
}

// AssertFor reports an error unless cond holds for the output, such as
// [reel.Matches] for a regular expression. It returns whether it held. The
// output has no cursor, so [reel.OnCursorLine] never holds; use
// [Harness.AssertFor] for that.
func AssertFor(tb testing.TB, view Viewable, cond reel.Condition, opts ...Option) bool {
	tb.Helper()
	return assertView(tb, view, cond, conditionNote(cond, false), opts)
}

// RequireFor is [AssertFor], failing the test immediately unless cond holds.
func RequireFor(tb testing.TB, view Viewable, cond reel.Condition, opts ...Option) {
	tb.Helper()
	assertView(tb, view, cond, conditionNote(cond, true), opts)
}

// Assert reports an error unless content appears in output. content is a
// string or every string in a []string. It returns whether it did.
func Assert[T string | []string](tb testing.TB, view Viewable, content T, opts ...Option) bool {
	tb.Helper()
	parts := texts(content)
	return assertView(tb, view, reel.Contains(parts...), textNote(AssertKindContains, parts, false), opts)
}

// Require fails the test immediately unless content appears in output.
// content is a string or every string in a []string.
func Require[T string | []string](tb testing.TB, view Viewable, content T, opts ...Option) {
	tb.Helper()
	parts := texts(content)
	assertView(tb, view, reel.Contains(parts...), textNote(AssertKindContains, parts, true), opts)
}

// AssertNot reports an error if content appears in output. content is a string
// or any string in a []string. It returns whether none appeared.
func AssertNot[T string | []string](tb testing.TB, view Viewable, content T, opts ...Option) bool {
	tb.Helper()
	parts := texts(content)
	return assertView(tb, view, reel.NotContains(parts...), textNote(AssertKindNotContains, parts, false), opts)
}

// RequireNot fails the test immediately if content appears in output.
// content is a string or any string in a []string.
func RequireNot[T string | []string](tb testing.TB, view Viewable, content T, opts ...Option) {
	tb.Helper()
	parts := texts(content)
	assertView(tb, view, reel.NotContains(parts...), textNote(AssertKindNotContains, parts, true), opts)
}

// conditionNote and textNote are the records of an assertion of a
// condition, and of text appearing or not.
func conditionNote(cond reel.Condition, required bool) assertNote {
	return assertNote{Kind: AssertKindCondition, Required: required, Condition: cond.String()}
}

func textNote(kind AssertKind, parts []string, required bool) assertNote {
	return assertNote{Kind: kind, Required: required, Text: parts}
}

// assertView is [checkAssert] of view's output, which has no cursor.
func assertView(tb testing.TB, view Viewable, cond reel.Condition, n assertNote, opts []Option) bool {
	tb.Helper()
	cfg := collectOptions(tb, opts...)
	return checkAssert(tb, cfg, reel.ScreenText{Text: renderedView(view, cfg.stripANSI), CursorRow: -1}, cond, n)
}

// checkAssert finishes n, an assertion that cond holds for screen, with what
// cond found (see [finishAssert]).
func checkAssert(tb testing.TB, cfg options, screen reel.ScreenText, cond reel.Condition, n assertNote) bool {
	tb.Helper()
	n.Matches, n.Pass = cond.Check(screen)
	return finishAssert(tb, cfg, n, "expected output to satisfy %s\noutput:\n%s", cond, screen.Text)
}

// finishAssert reports n, an assertion with its outcome set, as a failure
// with failf and args unless it passed, and records it. A required one that
// failed stops the test. It returns whether n passed.
func finishAssert(tb testing.TB, cfg options, n assertNote, failf string, args ...any) bool {
	tb.Helper()
	if !n.Pass {
		cfg.Errorf(tb, failf, args...)
	}
	cfg.noteAssert(n)
	if !n.Pass && n.Required {
		tb.FailNow()
	}
	return n.Pass
}

// AssertHeight reports an error unless output has n rows. Unlike
// [charm.land/lipgloss/v2.Height], empty output has a height of 0. It returns
// whether the output matched.
func AssertHeight(tb testing.TB, view Viewable, n int, opts ...Option) bool {
	tb.Helper()
	return assertHeight(tb, view, n, false, opts)
}

// RequireHeight fails the test immediately unless output has n rows.
func RequireHeight(tb testing.TB, view Viewable, n int, opts ...Option) {
	tb.Helper()
	assertHeight(tb, view, n, true, opts)
}

func assertHeight(tb testing.TB, view Viewable, n int, required bool, opts []Option) bool {
	tb.Helper()
	cfg := collectOptions(tb, opts...)
	gotw, goth := xansi.Dimensions(renderedView(view, cfg.stripANSI))
	return finishAssert(tb, cfg, assertNote{
		Kind: AssertKindHeight, Required: required,
		Pass:     goth == n,
		Expected: &dims{Height: &n}, Actual: &dims{Width: &gotw, Height: &goth},
	}, "expected output height %d, got %d", n, goth)
}

// AssertWidth reports an error unless output has n columns. It returns whether
// the output matched.
func AssertWidth(tb testing.TB, view Viewable, n int, opts ...Option) bool {
	tb.Helper()
	return assertWidth(tb, view, n, false, opts)
}

// RequireWidth fails the test immediately unless output has n columns.
func RequireWidth(tb testing.TB, view Viewable, n int, opts ...Option) {
	tb.Helper()
	assertWidth(tb, view, n, true, opts)
}

func assertWidth(tb testing.TB, view Viewable, n int, required bool, opts []Option) bool {
	tb.Helper()
	cfg := collectOptions(tb, opts...)
	gotw, goth := xansi.Dimensions(renderedView(view, cfg.stripANSI))
	return finishAssert(tb, cfg, assertNote{
		Kind: AssertKindWidth, Required: required,
		Pass:     gotw == n,
		Expected: &dims{Width: &n}, Actual: &dims{Width: &gotw, Height: &goth},
	}, "expected output width %d, got %d", n, gotw)
}

// AssertDimensions reports an error unless output has specified dimensions.
// It returns whether the output matched.
func AssertDimensions(tb testing.TB, view Viewable, width, height int, opts ...Option) bool {
	tb.Helper()
	return assertDimensions(tb, view, width, height, false, opts)
}

// RequireDimensions fails the test immediately unless output has specified dimensions.
func RequireDimensions(tb testing.TB, view Viewable, width, height int, opts ...Option) {
	tb.Helper()
	assertDimensions(tb, view, width, height, true, opts)
}

func assertDimensions(tb testing.TB, view Viewable, width, height int, required bool, opts []Option) bool {
	tb.Helper()
	cfg := collectOptions(tb, opts...)
	gotw, goth := xansi.Dimensions(renderedView(view, cfg.stripANSI))
	return finishAssert(tb, cfg, assertNote{
		Kind: AssertKindDimensions, Required: required,
		Pass:     gotw == width && goth == height,
		Expected: &dims{Width: &width, Height: &height}, Actual: &dims{Width: &gotw, Height: &goth},
	}, "expected output dimensions %dx%d, got %dx%d", width, height, gotw, goth)
}

// Dimensions returns the width and height of the output.
func Dimensions(tb testing.TB, out string, opts ...Option) (w, h int) {
	cfg := collectOptions(tb, opts...)
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	return xansi.Dimensions(out)
}
