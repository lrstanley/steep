// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"iter"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"getsteep.dev/steep/reel"
	uv "github.com/charmbracelet/ultraviolet"
)

func TestAssertStringWithStripANSI(t *testing.T) {
	t.Parallel()
	const out = "plain \x1b[31mred\x1b[0m"
	if !Assert(t, func() string { return out }, "plain red", WithANSI(false)) {
		t.Fatal("expected strip-then-contains to match")
	}
}

type otherMsg struct{}

func TestMessagesOfType(t *testing.T) {
	t.Parallel()
	messages := []uv.Event{
		appendMsg("a"),
		otherMsg{},
		appendMsg("b"),
	}

	got := slices.Collect(FilterMessagesType[appendMsg](t, slices.Values(messages)))
	if len(got) != 2 {
		t.Fatalf("messages = %d, want 2", len(got))
	}
	if got[0] != "a" || got[1] != "b" {
		t.Fatalf("messages = %#v, want [a b]", got)
	}
}

func TestWaitMessageWhere(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("first")).SendProgram(appendMsg("second"))

	got := h.WaitMessageWhere(func(m appendMsg) bool {
		return m == "second"
	})
	if got != "second" {
		t.Fatalf("message = %q, want second", got)
	}

	matches := slices.Collect(h.FilterMessagesType[appendMsg]())
	seconds := slices.Collect(h.FilterMessagesFunc(func(m appendMsg) bool {
		return m == "second"
	}))
	if len(seconds) != 1 || seconds[0] != "second" {
		t.Fatalf("filtered messages = %#v, want [second]", seconds)
	}
	if len(matches) != 2 {
		t.Fatalf("messages = %d, want 2", len(matches))
	}
}

type emptyMsgCollector struct{}

func (emptyMsgCollector) MessageHistory() iter.Seq[uv.Event] {
	return func(_ func(uv.Event) bool) {}
}

func (emptyMsgCollector) Messages(_ context.Context) iter.Seq[uv.Event] {
	return emptyMsgCollector{}.MessageHistory()
}

func (emptyMsgCollector) LiveMessages(_ context.Context) iter.Seq[uv.Event] {
	return emptyMsgCollector{}.MessageHistory()
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

// delayedPingModel pings itself repeatedly, as a live wait only sees messages
// after it starts, which can be later than the first ping on a loaded machine.
type delayedPingModel struct{}

func (m *delayedPingModel) View() string { return "ready" }

func (m *delayedPingModel) ping() tea.Cmd {
	return tea.Tick(15*time.Millisecond, func(time.Time) uv.Event {
		return appendMsg("ping")
	})
}

func (m *delayedPingModel) Init() tea.Cmd { return m.ping() }

func (m *delayedPingModel) Update(msg uv.Event) tea.Cmd {
	if _, ok := msg.(appendMsg); ok {
		return m.ping()
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
		WaitNot("BAD", WithTimeout(2*time.Second), WithCheck(5*time.Millisecond)).
		WaitNot([]byte("BAD"), WithTimeout(2*time.Second), WithCheck(5*time.Millisecond))
}

func TestWaitNotStrings(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &swapViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("flip")).
		WaitNot([]string{"foo", "bar"}, WithTimeout(2*time.Second), WithCheck(5*time.Millisecond))
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
		WithCheck(5*time.Millisecond),
	)
	if !strings.Contains(h.View(), "stable") {
		t.Fatalf("view = %q, want stable", h.View())
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

func TestAssertHasMessage_found_andMissing(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("z"))
	if got := h.WaitMessage[appendMsg](); got != "z" {
		t.Fatalf("message = %q, want z", got)
	}
	if !h.AssertHasMessage[appendMsg]() {
		t.Fatal("expected appendMsg in history")
	}
	h.RequireHasMessage[appendMsg]()

	st := &fakeTB{TB: t}
	if AssertHasMessage[appendMsg](st, emptyMsgCollector{}) {
		t.Fatal("expected false")
	}
	if len(st.errors()) != 1 {
		t.Fatalf("error calls = %d, want 1", len(st.errors()))
	}
}

func TestWaitNewMessageWhere(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &delayedPingModel{}, WithWindowSize(80, 1))
	got := h.WaitLiveMessageWhere(func(m appendMsg) bool {
		return m == "ping"
	}, WithTimeout(2*time.Second))
	if got != "ping" {
		t.Fatalf("got %#v", got)
	}
}

func TestIgnoreMessagesReflect(t *testing.T) {
	t.Parallel()
	msgs := []uv.Event{appendMsg("a"), otherMsg{}, appendMsg("b")}
	got := slices.Collect(IgnoreMessagesReflect(t, slices.Values(msgs), reflect.TypeOf(otherMsg{})))
	if len(got) != 2 || got[0] != appendMsg("a") || got[1] != appendMsg("b") {
		t.Fatalf("got %#v", got)
	}
	all := slices.Collect(IgnoreMessagesReflect(t, slices.Values(msgs)))
	if len(all) != 3 {
		t.Fatalf("ignore none: len = %d", len(all))
	}
}

func TestFilterMessagesFunc(t *testing.T) {
	t.Parallel()
	msgs := []uv.Event{appendMsg("a"), appendMsg("b"), otherMsg{}}
	got := slices.Collect(FilterMessagesFunc(t, slices.Values(msgs), func(m appendMsg) bool {
		return m == "b"
	}))
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %#v, want [b]", got)
	}
	empty := slices.Collect(FilterMessagesFunc(t, slices.Values(msgs), func(_ appendMsg) bool {
		return false
	}))
	if len(empty) != 0 {
		t.Fatalf("want empty, got %#v", empty)
	}
}
