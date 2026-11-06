// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build docsfailures

package main

import (
	"os/exec"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lrstanley/steep"
	"github.com/lrstanley/steep/reel"
	"github.com/lrstanley/steep/reel/effects"
	"github.com/lrstanley/steep/snapshot"
)

// These tests fail on purpose: each is a failure the site shows, with the
// output and the recording it leaves. TestCaptureFailures runs them one at a
// time. The build tag keeps them out of every other run.

func failing(name string) []steep.Option {
	return demo(name, true, effects.Keys(time.Second), effects.Pause(), steep.Outro(2*time.Second))
}

func TestFailAssert(t *testing.T) {
	steep.NewHarness(t, NewModel(), failing("fail-assert")...).
		Wait("Sencha").
		Assert("Genmaicha").
		Assert("Hojicha").
		KeyDown().
		Assert("Matcha")
}

func TestFailRequire(t *testing.T) {
	steep.NewHarness(t, NewModel(), failing("fail-require")...).
		Wait("Sencha").
		Require("Genmaicha").
		Assert("Hojicha").
		KeyDown()
}

func TestFailWait(t *testing.T) {
	steep.NewHarness(t, NewModel(), append(failing("fail-wait"), steep.WithTimeout(time.Second))...).
		Wait("Sencha").
		KeyDown().
		Wait("Genmaicha")
}

type clock struct{ n int }

type clockTick struct{}

func (c *clock) Init() tea.Cmd {
	return tea.Tick(20*time.Millisecond, func(time.Time) tea.Msg { return clockTick{} })
}

func (c *clock) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(clockTick); ok {
		c.n++
		return c, c.Init()
	}
	return c, nil
}

func (c *clock) View() tea.View { return tea.NewView("frame " + string(rune('a'+c.n%26))) }

func TestFailSettle(t *testing.T) {
	steep.NewHarness(t, &clock{}, steep.WithWindowSize(24, 3), steep.WithTimeout(500*time.Millisecond)).
		WaitSettle()
}

func TestFailSnapshot(t *testing.T) {
	steep.NewHarness(t, NewModel(), append(failing("fail-snapshot"), steep.WithWindowSize(72, 16))...).
		Wait("Sencha").
		AssertSnapshot(snapshot.WithDir(fixtureDir))
}

func TestFailSnapshotMissing(t *testing.T) {
	steep.NewHarness(t, NewModel(), steep.WithWindowSize(72, 16)).
		Wait("Sencha").
		AssertSnapshot(snapshot.WithDir(t.TempDir()))
}

func TestFailExecProgram(t *testing.T) {
	steep.NewExecHarness(t, exec.Command("sh", "-c", "echo ready; sleep 5")).
		Wait("ready").
		QuitProgram()
}

func TestFailLocate(t *testing.T) {
	h := steep.NewHarness(t, NewModel(), steep.WithWindowSize(72, 16))
	h.Wait("Sencha")
	h.Locate(reel.NotContains("Genmaicha")).LeftClick()
}

func TestFailBadExport(t *testing.T) {
	t.Setenv("STEEP_EXPORT", "out.gif?bogus=1")
	steep.NewHarness(t, NewModel(), steep.WithWindowSize(72, 16)).Wait("Sencha")
}

// TestExportRemoved passes, and logs that its files are removed without
// "go test -artifacts".
func TestExportRemoved(t *testing.T) {
	steep.NewHarness(t, NewModel(), steep.WithWindowSize(72, 16), steep.WithExportAlways(), steep.WithIgnoreEnv(true),
		steep.WithRecording(reel.Profile{Outputs: reel.MustFiles("{name}.snap")})).
		Wait("Sencha")
}
