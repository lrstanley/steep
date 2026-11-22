// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/region"
	uv "github.com/charmbracelet/ultraviolet"
)

func TestAssertStringWithStripANSI(t *testing.T) {
	t.Parallel()
	const out = "plain \x1b[31mred\x1b[0m"
	if !Assert(t, func() string { return out }, "plain red", WithANSI(false)) {
		t.Fatal("expected strip-then-contains to match")
	}
}

type swapViewModel struct {
	clean bool
}

func (m *swapViewModel) View() string {
	if !m.clean {
		return "foo BAD bar"
	}
	return "all fine"
}

func (m *swapViewModel) Update(msg uv.Event) tea.Cmd {
	if _, ok := msg.(appendMsg); ok {
		m.clean = true
	}
	return nil
}

func TestWaitFor(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{text: "hi"}, WithWindowSize(80, 1))
	h.WaitFor(reel.ScreenFunc(func(s string) bool { return strings.HasPrefix(s, "text=hi") }),
		WithTimeout(2*time.Second)).
		WaitFor(reel.OnCursorLine(reel.Contains("text=")), WithTimeout(2*time.Second))
}

func TestWaitString_WaitBytes(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{text: "hi"}, WithWindowSize(80, 1))
	h.Wait("text=hi", WithTimeout(2*time.Second)).Wait([]byte("text=hi"), WithTimeout(2*time.Second))
}

func TestWait_timeout(t *testing.T) {
	t.Parallel()
	tb := &fakeTB{TB: t}
	h := NewComponentHarness(tb, &mutableViewModel{text: "hi"}, WithWindowSize(80, 1), WithIgnoreEnv(true))
	h.Wait("text=hi")
	runFailing(func() { h.Wait("nope", WithTimeout(20*time.Millisecond)) })
	errs := strings.Join(tb.errors(), "\n")
	if !strings.Contains(errs, `Wait("nope")`) || !strings.Contains(errs, "text=hi") {
		t.Fatalf("errors = %q, want the reason and the last screen", errs)
	}
}

func TestWait_timeoutPercent(t *testing.T) {
	t.Parallel()
	tb := &fakeTB{TB: t}
	h := NewComponentHarness(tb, &mutableViewModel{text: "hi"}, WithWindowSize(80, 1), WithIgnoreEnv(true))
	h.Wait("text=hi")
	runFailing(func() { h.Wait("100%", WithTimeout(20*time.Millisecond)) })
	errs := strings.Join(tb.errors(), "\n")
	if !strings.Contains(errs, `Wait("100%"): `) || !strings.Contains(errs, "text=hi") || strings.Contains(errs, "%!") {
		t.Fatalf("errors = %q, want the reason as written and the last screen", errs)
	}
}

func TestWaitStrings(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{text: "ab"}, WithWindowSize(80, 1))
	h.Wait([]string{"text=", "ab"}, WithTimeout(2*time.Second))
}

func TestWaitNotString_WaitNotBytes(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &swapViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("flip")).
		WaitNot("BAD", WithTimeout(2*time.Second)).
		WaitNot([]byte("BAD"), WithTimeout(2*time.Second))
}

func TestWaitNotStrings(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &swapViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("flip")).
		WaitNot([]string{"foo", "bar"}, WithTimeout(2*time.Second))
}

func TestWaitFor_matches(t *testing.T) {
	t.Parallel()
	bad := regexp.MustCompile(`BAD`)
	h := NewComponentHarness(t, &swapViewModel{}, WithWindowSize(80, 1))
	h.WaitFor(reel.Matches(bad), WithTimeout(2*time.Second)).
		SendProgram(appendMsg("flip")).
		WaitFor(reel.NotMatches(bad), WithTimeout(2*time.Second))
}

func TestWaitSettleView(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &viewSettleStableModel{}, WithWindowSize(80, 1))
	h.Wait("stable").WaitSettle(
		WithSettle(25*time.Millisecond),
		WithTimeout(2*time.Second),
	)
	if !strings.Contains(h.View(), "stable") {
		t.Fatalf("view = %q, want stable", h.View())
	}
}

// clockTick is a tick of clockModel.
type clockTick struct{}

// clockModel shows "stable" over a status line whose clock ticks every
// 10ms, as "updated Ns ago" would.
type clockModel struct{ ticks int }

func (m *clockModel) View() string { return fmt.Sprintf("stable\nupdated %ds ago", m.ticks) }

func (m *clockModel) Init() tea.Cmd { return m.tick() }

func (m *clockModel) Update(msg uv.Event) tea.Cmd {
	if _, ok := msg.(clockTick); !ok {
		return nil
	}
	m.ticks++
	return m.tick()
}

func (m *clockModel) tick() tea.Cmd {
	return tea.Tick(10*time.Millisecond, func(time.Time) uv.Event { return clockTick{} })
}

func TestWaitSettleRegions(t *testing.T) {
	t.Parallel()
	settle := []Option{WithSettle(50 * time.Millisecond), WithTimeout(5 * time.Second)}
	for name, opts := range map[string][]Option{
		"ignore the clock's row":  {WithSettleIgnore(region.Row(-1))},
		"ignore the clock's text": {WithSettleIgnore(region.Regexp(regexp.MustCompile(`updated \d+s ago`)))},
		"only the result":         {WithSettleOnly(region.Text("stable"))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// As harness options, which each wait takes.
			h := NewComponentHarness(t, &clockModel{}, append([]Option{WithWindowSize(40, 2)}, opts...)...)
			h.Wait("updated").WaitSettle(settle...)
		})
	}

	// Without them, the clock keeps the screen from settling. The quiet time
	// is many ticks long, which makes a scheduling stall under -race on a busy
	// machine much less likely to pass for a settled screen.
	tb := &fakeTB{TB: t}
	h := NewComponentHarness(tb, &clockModel{}, WithWindowSize(40, 2), WithIgnoreEnv(true))
	h.Wait("updated")
	runFailing(func() { h.WaitSettle(WithSettle(250*time.Millisecond), WithTimeout(time.Second)) })
	if errs := strings.Join(tb.errors(), "\n"); !strings.Contains(errs, "wait for settle") {
		t.Fatalf("errors = %q, want a settle that timed out", errs)
	}
	// Nor does a region that never shows.
	tb = &fakeTB{TB: t}
	h = NewComponentHarness(tb, &clockModel{}, WithWindowSize(40, 2), WithIgnoreEnv(true))
	runFailing(func() {
		h.WaitSettle(WithSettleOnly(region.Text("never")), WithSettle(50*time.Millisecond), WithTimeout(300*time.Millisecond))
	})
	if errs := strings.Join(tb.errors(), "\n"); !strings.Contains(errs, "regions it watches not on the screen") {
		t.Fatalf("errors = %q, want a settle on regions not on the screen", errs)
	}
}

func TestAssertString_fail(t *testing.T) {
	t.Parallel()
	st := &fakeTB{TB: t}
	if assertNo := Assert(st, func() string { return "abc" }, "nope"); assertNo {
		t.Fatal("expected false")
	}
	if len(st.errors()) != 1 {
		t.Fatalf("error calls = %d, want 1", len(st.errors()))
	}
}

func TestAssertStrings_fail(t *testing.T) {
	t.Parallel()
	st := &fakeTB{TB: t}
	if Assert(st, func() string { return "only a" }, []string{"a", "missing"}) {
		t.Fatal("expected false")
	}
	if len(st.errors()) != 1 {
		t.Fatalf("error calls = %d, want 1", len(st.errors()))
	}
}

func TestAssertNotString_fail(t *testing.T) {
	t.Parallel()
	st := &fakeTB{TB: t}
	if AssertNot(st, func() string { return "has needle" }, "needle") {
		t.Fatal("expected false")
	}
	if len(st.errors()) != 1 {
		t.Fatalf("error calls = %d, want 1", len(st.errors()))
	}
}

func TestAssertNotStrings_fail(t *testing.T) {
	t.Parallel()
	st := &fakeTB{TB: t}
	if AssertNot(st, func() string { return "x bad y" }, []string{"zzz", "bad"}) {
		t.Fatal("expected false")
	}
	if len(st.errors()) != 1 {
		t.Fatalf("error calls = %d, want 1", len(st.errors()))
	}
}

func TestAssertFor_fail(t *testing.T) {
	t.Parallel()
	for _, cond := range []reel.Condition{
		reel.Matches(regexp.MustCompile(`^\d+$`)),
		reel.NotMatches(regexp.MustCompile(`\d`)),
		reel.OnCursorLine(reel.Contains("abc")),
	} {
		st := &fakeTB{TB: t}
		if AssertFor(st, func() string { return "abc123" }, cond) {
			t.Fatalf("%s held", cond)
		}
		if len(st.errors()) != 1 {
			t.Fatalf("%s: error calls = %d, want 1", cond, len(st.errors()))
		}
	}
}

func TestAssertHeightWidthDimensions(t *testing.T) {
	t.Parallel()
	v := func() string { return "ab\nxy" }
	if !AssertWidth(t, v, 2) || !AssertHeight(t, v, 2) || !AssertDimensions(t, v, 2, 2) {
		t.Fatal("layout assertions should pass")
	}
	RequireWidth(t, v, 2)
	RequireHeight(t, v, 2)
	RequireDimensions(t, v, 2, 2)
}

func TestAssertHeight_fail(t *testing.T) {
	t.Parallel()
	st := &fakeTB{TB: t}
	if AssertHeight(st, func() string { return "one\ntwo" }, 99) {
		t.Fatal("expected false")
	}
	if len(st.errors()) != 1 {
		t.Fatalf("error calls = %d, want 1", len(st.errors()))
	}
}

func TestPackageRequireString_andAssertHelpers_ok(t *testing.T) {
	t.Parallel()
	v := func() string { return "alpha beta" }
	Require(t, v, "beta")
	Require(t, v, []string{"alpha", "beta"})
	RequireNot(t, v, "gamma")
	RequireNot(t, v, []string{"x", "y"})
	if !AssertNot(t, v, "gamma") || !AssertNot(t, v, []string{"nope"}) {
		t.Fatal("negative asserts should pass")
	}
	RequireFor(t, v, reel.Matches(regexp.MustCompile(`b.t`)))
	RequireFor(t, v, reel.NotMatches(regexp.MustCompile(`^\d`)))
	if !AssertFor(t, v, reel.Matches(regexp.MustCompile(`alpha`))) {
		t.Fatal("condition asserts should pass")
	}
}
