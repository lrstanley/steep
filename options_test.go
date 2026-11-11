// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/sink"
	"github.com/charmbracelet/colorprofile"
)

func outputPaths(files []reel.File) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

func TestParseExportMode(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]exportPolicy{
		"on": exportAlways, "Always": exportAlways, "true": exportAlways, "1": exportAlways,
		"failure": exportOnFailure, "on-failure": exportOnFailure,
		"off": exportOff, "false": exportOff, "0": exportOff,
	} {
		if got, ok := parseExportMode(raw); !ok || got != want {
			t.Errorf("%q: got %v %v, want %v", raw, got, ok, want)
		}
	}
	if _, ok := parseExportMode("sometimes"); ok {
		t.Error("unknown mode parsed")
	}
}

func TestApplyEnvExportMode(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MODE", "failure")
	if cfg := collectOptions(t, WithExportAlways()); cfg.policy != exportOnFailure {
		t.Fatalf("got policy %v, want the environment's on failure", cfg.policy)
	}
	t.Setenv("STEEP_EXPORT_MODE", "nonsense")
	if cfg := collectOptions(t, WithExportAlways()); cfg.policy != exportAlways {
		t.Fatalf("got policy %v, want an unknown mode to leave the option alone", cfg.policy)
	}
}

func TestDefaultPolicyOff(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MODE", "")
	if cfg := collectOptions(t, WithRecording(reel.Profile{Outputs: reel.MustFiles("x.gif")})); cfg.policy != exportOff {
		t.Fatalf("got policy %v, want off: a profile alone exports nothing", cfg.policy)
	}
}

func TestWithIgnoreEnv(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MODE", "on")
	t.Setenv("STEEP_EXPORT", "x.mp4")
	t.Setenv("STEEP_EXPORT_FPS", "10")
	t.Setenv("STEEP_SLOW", "500ms")
	cfg := collectOptions(t, WithIgnoreEnv(true))
	if cfg.policy != exportOff || cfg.slow != 0 || cfg.recording.Outputs != nil || cfg.recording.FPS != 0 {
		t.Fatalf("environment applied: policy %v slow %v outputs %v fps %d", cfg.policy, cfg.slow, cfg.recording.Outputs, cfg.recording.FPS)
	}
}

func TestApplyEnvSlow(t *testing.T) {
	t.Setenv("STEEP_SLOW", "25ms")
	cfg := collectOptions(t)
	if cfg.slow != 25*time.Millisecond {
		t.Fatalf("got %v", cfg.slow)
	}
}

func TestApplyEnvTypingDelay(t *testing.T) {
	t.Setenv("STEEP_TYPING_DELAY", "50ms")
	if cfg := collectOptions(t); cfg.typingDelay != 50*time.Millisecond {
		t.Fatalf("got %v", cfg.typingDelay)
	}
}

func TestApplyEnvRecordMessages(t *testing.T) {
	t.Setenv("STEEP_RECORD_MESSAGES", "1")
	if cfg := collectOptions(t); !cfg.recordMessages {
		t.Fatal("STEEP_RECORD_MESSAGES=1 did not record messages")
	}
	t.Setenv("STEEP_RECORD_MESSAGES", "false")
	if cfg := collectOptions(t, WithRecordMessages(true)); cfg.recordMessages {
		t.Fatal("STEEP_RECORD_MESSAGES=false did not override the option")
	}
}

// TestApplyEnvProfile checks the environment's patch of the recording
// profile: timing, outputs, and stills replace the profile's, and render
// options are added after its own.
func TestApplyEnvProfile(t *testing.T) {
	t.Setenv("STEEP_EXPORT", "{name}_{status}.webm?quality=80,{name}_{status}_{mark}.png")
	t.Setenv("STEEP_EXPORT_DIR", "out")
	t.Setenv("STEEP_EXPORT_FPS", "10")
	t.Setenv("STEEP_EXPORT_SPEED", "0.5")
	t.Setenv("STEEP_RENDERER_SCALE", "2")
	t.Setenv("STEEP_RENDERER_FONT_FAMILY", "")

	user := reel.Profile{
		Outputs: reel.MustFiles("mine.gif"),
		Encode:  []sink.Option{sink.WithLoopCount(sink.LoopOnce)},
		FPS:     50,
		Vars:    map[string]string{"name": "fixed"},
	}
	cfg := collectOptions(t, WithRecording(user), WithStills(StillSnapshot, HookAssert))
	p := cfg.recording
	if got := outputPaths(p.Outputs); !slices.Equal(got, []string{"{name}_{status}.webm", "{name}_{status}_{mark}.png"}) {
		t.Errorf("outputs = %q, want the environment's", got)
	}
	if len(p.Outputs[0].Options) != 1 || len(p.Encode) != 1 {
		t.Errorf("%d output options and %d encode options, want the URL's and the profile's", len(p.Outputs[0].Options), len(p.Encode))
	}
	if cfg.stills != nil {
		t.Errorf("stills = %v, want those of WithStills replaced", cfg.stills)
	}
	if p.FPS != 10 || p.Speed != 0.5 {
		t.Errorf("fps %d speed %v, want 10 and 0.5", p.FPS, p.Speed)
	}
	if len(p.Render) != 1 {
		t.Errorf("%d render options, want the scale", len(p.Render))
	}
	if cfg.artifacts != "out" {
		t.Errorf("artifacts = %q", cfg.artifacts)
	}
	if p.Vars["name"] != "fixed" {
		t.Errorf("vars = %v, want the profile's kept", p.Vars)
	}
	// Collecting the options again must not grow the profile's slices.
	if again := collectOptions(t, WithRecording(user)); len(again.recording.Render) != 1 || len(user.Render) != 0 {
		t.Errorf("render options grew: %d, profile %d", len(again.recording.Render), len(user.Render))
	}
}

func TestApplyEnvSource(t *testing.T) {
	for _, tt := range []struct {
		env  string
		opt  bool
		want bool
	}{
		{"on", false, true},
		{"true", false, true},
		{"1", false, true},
		{"off", true, false},
		{"false", true, false},
		{"0", true, false},
		{"", true, true},
		{"bogus", true, true},
		{"", false, false},
	} {
		t.Setenv("STEEP_SOURCE", tt.env)
		if cfg := collectOptions(t, WithSource(tt.opt)); cfg.source != tt.want {
			t.Errorf("STEEP_SOURCE=%q with WithSource(%v): source %v, want %v", tt.env, tt.opt, cfg.source, tt.want)
		}
	}
	t.Setenv("STEEP_SOURCE", "on")
	if cfg := collectOptions(t, WithIgnoreEnv(true)); cfg.source {
		t.Fatal("STEEP_SOURCE applied with WithIgnoreEnv")
	}
}

func TestWithColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		opts []Option
		want colorprofile.Profile
	}{
		{nil, colorprofile.ANSI256},
		{[]Option{WithColor(ColorASCII)}, colorprofile.ASCII},
		{[]Option{WithColor(ColorANSI)}, colorprofile.ANSI},
		{[]Option{WithColor(ColorANSI256)}, colorprofile.ANSI256},
		{[]Option{WithColor(ColorTrueColor)}, colorprofile.TrueColor},
		// WithColor applies after WithEnvVars, whatever the order.
		{[]Option{WithColor(ColorTrueColor), WithEnvVars("TERM=dumb", "NO_COLOR=1")}, colorprofile.TrueColor},
		// An explicit program option wins.
		{[]Option{WithColor(ColorTrueColor), WithProgramOptions(tea.WithColorProfile(colorprofile.ANSI))}, colorprofile.ANSI},
	}
	for _, tt := range tests {
		h := NewHarness(t, terminalProbe{}, append(tt.opts, WithIgnoreEnv(true))...)
		got := h.WaitMessage[tea.ColorProfileMsg]()
		if got.Profile != tt.want {
			t.Errorf("WithColor options %d: program detected %v, want %v", len(tt.opts), got.Profile, tt.want)
		}
	}
}

func TestColorEnv(t *testing.T) {
	t.Parallel()

	env := []string{"TTY_FORCE=1", "TERM=xterm-256color", "NO_COLOR=1", "HOME=/tmp"}
	got := ColorTrueColor.env(env)
	want := []string{"TTY_FORCE=1", "HOME=/tmp", "TERM=xterm-256color", "COLORTERM=truecolor"}
	if !slices.Equal(got, want) {
		t.Fatalf("env = %q, want %q", got, want)
	}
}

// The explicit Bubble Tea profile must agree with what detection finds from
// the environment alone, which other programs reading it would see.
func TestColorProfileEnvMatchesDetection(t *testing.T) {
	t.Parallel()

	for _, p := range []ColorProfile{ColorASCII, ColorANSI, ColorANSI256, ColorTrueColor} {
		if got := colorprofile.Env(p.env([]string{"TTY_FORCE=1"})); got != p.teaProfile() {
			t.Errorf("%v: environment detects %v, want %v", p, got, p.teaProfile())
		}
	}
}

// TestFailureReason checks that a reason is prefixed as text, as it can hold
// what a test passed, such as the percent sign of Wait("100%").
func TestFailureReason(t *testing.T) {
	t.Parallel()
	for reason, want := range map[string]string{
		"":     "got 1",
		"Wait": "Wait: got 1",
		"100%": "100%: got 1",
	} {
		ftb := &fakeTB{TB: t}
		options{reason: reason}.Errorf(ftb, "got %d", 1)
		runFailing(func() { options{reason: reason}.Fatalf(ftb, "got %d", 1) })
		if got := ftb.errors(); !slices.Equal(got, []string{want, want}) {
			t.Errorf("reason %q: errors = %q, want %q twice", reason, got, want)
		}
	}
}
