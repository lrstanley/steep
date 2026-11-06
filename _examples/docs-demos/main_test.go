// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lrstanley/steep"
	"github.com/lrstanley/steep/reel"
	"github.com/lrstanley/steep/reel/effects"
	"github.com/lrstanley/steep/reel/render"
	"github.com/lrstanley/steep/snapshot"
)

// These tests run with the others, as tests of the tea shop. With exporting
// turned on, as "task docs:demos" does, they also write what the site shows:
//
//	STEEP_EXPORT_MODE=on go test -count 1 ./docs-demos/
const (
	// demoDir is relative to this package's directory.
	demoDir = "../../docs/public/demos"
	// fixtureDir holds text the site's pages include: golden files, and
	// the output of failing tests.
	fixtureDir = "../../docs/fixtures"
	// demoScale draws at twice the size, so recordings stay sharp at the width
	// of a page, and on a high-density screen.
	demoScale = 2
)

// exporting reports whether this run writes the site's files.
func exporting() bool { return os.Getenv("STEEP_EXPORT_MODE") == "on" }

// look is how the site's recordings are drawn.
func look() []render.Option {
	return []render.Option{
		render.WithScale(demoScale),
		render.WithWindowChrome(true),
		render.WithPadding(20),
		render.WithMargin(32, lipgloss.Color("#0b150f")),
		render.WithBorderRadius(20),
	}
}

// demo returns the options of a test that records name.webp and, with cast,
// name.cast for the inspector, in the shop's size. WithSlow leaves time to read
// each step, and only when recording, so the tests stay quick otherwise.
func demo(name string, cast bool, fx ...reel.Effect) []steep.Option {
	outputs := []string{name + ".webp"}
	if cast {
		outputs = append(outputs, "../casts/"+name+".cast")
	}
	opts := []steep.Option{
		steep.WithWindowSize(72, 16),
		steep.WithColor(steep.ColorTrueColor),
		steep.WithTimeout(15 * time.Second),
		steep.WithArtifacts(demoDir),
		steep.WithRecording(reel.Profile{Outputs: reel.MustFiles(outputs...), Render: look(), Effects: fx}),
	}
	if exporting() {
		opts = append(opts, steep.WithSlow(280*time.Millisecond))
	}
	return opts
}

// TestFirstTest is the test the home page and the tutorial show.
func TestFirstTest(t *testing.T) {
	h := steep.NewHarness(t, NewModel(), demo("first-test", true,
		steep.Logo(1500*time.Millisecond),
		effects.Keys(time.Second),
		effects.Pause(),
		steep.Outro(1500*time.Millisecond),
	)...)
	h.Wait("Sencha").
		KeyDown().
		Assert("Matcha").
		Key("enter").
		Wait("Matcha is ready").
		AssertSnapshot(snapshot.WithDir(fixtureDir))
}

func TestKeyboard(t *testing.T) {
	steep.NewHarness(t, NewModel(), demo("keyboard", false, effects.Keys(time.Second), effects.Pause())...).
		Wait("Sencha").
		KeyDown(reel.Repeat(2)).
		KeyUp().
		Key("/").
		Type("green", reel.Delay(80*time.Millisecond)).
		Wait("Filter: green").
		KeyEsc().
		WaitNot("Filter:")
}

func TestMouse(t *testing.T) {
	h := steep.NewHarness(t, NewModel(), demo("mouse", false, effects.Mouse(), effects.Pause())...)
	h.Wait("Sencha").
		Locate(reel.Contains("Oolong")).LeftClick().
		Key("enter").
		Wait("Oolong is ready")
	h.Locate(reel.Contains("Darjeeling")).LeftClick()
	h.LeftClick(10, 4).WaitSettle()
}

func TestResize(t *testing.T) {
	h := steep.NewHarness(t, NewModel(), demo("resize", false, effects.Pause())...)
	h.Wait("Sencha").
		Resize(48, 16).
		WaitSettle().
		Assert("╮").
		Assert("q quit").
		Resize(72, 16).
		WaitSettle().
		Assert("╮")
	if w, _ := h.Dimensions(); w != 72 {
		t.Errorf("width = %d, want 72", w)
	}
}

func TestTerminalState(t *testing.T) {
	h := steep.NewHarness(t, NewModel(), demo("brew", true, effects.Toasts(), effects.Pause())...)
	h.Wait("Sencha").
		Key("enter").
		Wait("█")
	if p := h.Progress(); p.Percent == 0 {
		t.Errorf("progress = %+v, want some", p)
	}
	h.Wait("Sencha is ready")
	if got := h.Title(); got != "tea shop" {
		t.Errorf("title = %q", got)
	}
	if !h.IsAltScreen() {
		t.Error("the shop should be on the alternate screen")
	}
	// tea.Raw runs as a command of its own, so the notification can land after
	// the frame that announces it, and Wait can't see it arrive, since it doesn't
	// change the screen.
	deadline := time.Now().Add(5 * time.Second)
	for len(h.Notifications()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := h.Notifications(); len(n) != 1 || n[0].Body != "Sencha is ready" {
		t.Errorf("notifications = %+v", n)
	}
}

func TestWaitSettle(t *testing.T) {
	steep.NewHarness(t, NewModel(), demo("settle", false, effects.Pause())...).
		Wait("Sencha").
		Key("enter").
		WaitSettle(). // The brew redraws every 60ms, so this only returns when it is done.
		Assert("Sencha is ready")
}

func TestMessages(t *testing.T) {
	h := steep.NewHarness(t, NewModel(), steep.WithWindowSize(72, 16), steep.WithSettleIgnoreMsgs(brewTick{}))
	size := h.WaitMessage[tea.WindowSizeMsg]()
	if size.Width != 72 {
		t.Errorf("width = %d, want 72", size.Width)
	}
	h.Key("enter").WaitLiveMessage[brewTick]()
	h.RequireHasMessage[tea.KeyPressMsg]()
	m := h.Wait("Sencha is ready").QuitProgram().FinalModelAs[*Model]()
	if len(m.Served) != 1 {
		t.Errorf("served = %v", m.Served)
	}
}

// counter is a component: it has no Init, and its View returns a string.
type counter struct{ n int }

func (c *counter) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "+" {
		c.n++
	}
	return nil
}

func (c *counter) View() string {
	return titleStyle.Render("Cups") + " " + rowStyle.Render(strings.Repeat("+", c.n))
}

func TestComponent(t *testing.T) {
	h := steep.NewComponentHarness(t, &counter{}, append(demo("component", false, effects.Keys(time.Second), effects.Pause()),
		steep.WithWindowSize(24, 4),
	)...)
	h.Wait("Cups").Key("+", reel.Repeat(3)).Wait("+++")
}

// TestStringSnapshot and TestJSONSnapshot write the golden files the snapshot
// pages show, of the same small component.
func TestStringSnapshot(t *testing.T) {
	view := (&counter{n: 2}).View()
	snapshot.AssertEqual(t, view, snapshot.WithDir(fixtureDir))
	snapshot.AssertEqual(t, view, snapshot.WithDir(fixtureDir), snapshot.WithANSI(false), snapshot.WithSuffix("plain"))
}

func TestJSONSnapshot(t *testing.T) {
	steep.NewComponentHarness(t, &counter{n: 2}, steep.WithWindowSize(24, 4), steep.WithColor(steep.ColorTrueColor)).
		Wait("++").
		AssertJSON(snapshot.WithDir(fixtureDir), snapshot.WithIndent(true))
}

func TestExec(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "teashop")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	h := steep.NewExecHarness(t, exec.Command(bin), demo("exec", false, effects.Keys(time.Second))...)
	h.Wait("Sencha").KeyDown().Assert("Matcha").Key("q")
	if status := h.ExitStatus(); status != 0 {
		t.Errorf("exit status = %d", status)
	}
}

func TestStills(t *testing.T) {
	opts := append(demo("stills", false), steep.WithStills(steep.StillPNG, steep.HookInput))
	steep.NewHarness(t, NewModel(), opts...).
		Wait("Sencha").
		KeyDown().
		KeyDown().
		Key("enter").
		Wait("Gyokuro is ready")
}
