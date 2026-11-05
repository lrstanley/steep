// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/lrstanley/steep/reel"
	"github.com/lrstanley/steep/reel/render"
)

const (
	// DefaultTermWidth is the default terminal width (see [WithWindowSize]).
	DefaultTermWidth = 80
	// DefaultTermHeight is the default terminal height (see [WithWindowSize]).
	DefaultTermHeight = 24

	// DefaultTimeout is the default timeout used by [Harness] blocking waits.
	DefaultTimeout = 2 * time.Second
	// DefaultCheckInterval is the default [WithCheck] interval.
	DefaultCheckInterval = 10 * time.Millisecond
	// DefaultSettleTimeout is the default settle timeout used by [Harness] settle
	// waits. Settle timeouts are capped at 80% of the [WithTimeout] timeout.
	DefaultSettleTimeout = 100 * time.Millisecond
)

type options struct {
	width         int
	height        int
	timeout       time.Duration
	checkInterval time.Duration
	programOpts   []tea.ProgramOption

	settleTimeout time.Duration
	settleIgnore  []reflect.Type

	// scrollOnInput is [WithScrollOnInput].
	scrollOnInput bool

	stripANSI bool

	// reason prefixes failure messages.
	reason string

	// harness is the harness whose timeline waits and assertions record
	// into, or nil to record nothing.
	harness *Harness

	// envVars is nil unless [WithEnvVars] set it.
	envVars []string
	// color is 0 to leave the environment as it is.
	color ColorProfile

	ctx           context.Context
	wasContextSet bool

	// recording includes the environment's patch.
	recording reel.Profile
	// artifacts is empty for [testing.TB.ArtifactDir].
	artifacts string
	policy    exportPolicy
	stills    []reel.File
	// autoMarksOff is the categories [WithAutoMarks] set, true if off.
	autoMarksOff map[string]bool

	recordMessages bool
	// source is [WithSource].
	source bool

	hooks       []Hook
	ignoreEnv   bool
	slow        time.Duration
	typingDelay time.Duration
	snapshotDir string
}

func defaultOptions() options {
	return options{
		width:         DefaultTermWidth,
		height:        DefaultTermHeight,
		timeout:       DefaultTimeout,
		checkInterval: DefaultCheckInterval,
		settleTimeout: DefaultSettleTimeout,
		scrollOnInput: true,
		ctx:           context.Background(),
	}
}

func collectOptions(tb testing.TB, opts ...Option) options {
	cfg := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	applyEnv(tb, &cfg)
	return cfg.validate()
}

func (o options) validate() options {
	if o.checkInterval <= 0 {
		o.checkInterval = DefaultCheckInterval
	}
	if o.timeout <= 0 {
		o.timeout = DefaultTimeout
	}
	o.typingDelay = max(o.typingDelay, 0)
	if o.settleTimeout <= 0 {
		o.settleTimeout = DefaultSettleTimeout
	}
	o.settleTimeout = min(o.settleTimeout, o.timeout*8/10)

	return o
}

func (o options) Errorf(tb testing.TB, format string, args ...any) {
	tb.Helper()
	tb.Errorf("%s", o.failure(format, args...))
}

func (o options) Fatalf(tb testing.TB, format string, args ...any) {
	tb.Helper()
	tb.Fatalf("%s", o.failure(format, args...))
}

// failure formats a failure message, prefixed with the reason. The reason is
// not part of the format, as it can hold what a test passed, such as the
// percent sign of Wait("100%").
func (o options) failure(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	if o.reason != "" {
		return o.reason + ": " + msg
	}
	return msg
}

// Option configures a [Harness] or assertion helper. Options given to
// [NewHarness] are defaults for every [Harness] method that accepts options,
// and per-call options override them.
type Option func(*options)

// withReason sets a prefix for failure messages.
func withReason(format string, args ...any) Option {
	return func(cfg *options) { cfg.reason = fmt.Sprintf(format, args...) }
}

// withHarness makes waits and assertions record their outcome in h's
// timeline. [NewHarness] adds it to every call's options.
func withHarness(h *Harness) Option {
	return func(cfg *options) {
		cfg.harness = h
	}
}

// WithWindowSize configures the starting terminal size.
//
// See also [DefaultTermWidth] and [DefaultTermHeight].
func WithWindowSize(width, height int) Option {
	return func(cfg *options) {
		cfg.width = width
		cfg.height = height
	}
}

// WithContext sets the parent context for blocking waits and the running
// program. A [Harness] defaults to the test's context, canceled on
// interrupt; other helpers default to [context.Background].
func WithContext(ctx context.Context) Option {
	if ctx == nil {
		ctx = context.Background()
	}
	return func(cfg *options) {
		cfg.ctx = ctx
		cfg.wasContextSet = true
	}
}

// WithProgramOptions appends [tea.ProgramOption] values for [NewHarness] and
// [NewComponentHarness], and has no effect on [NewExecHarness]. Environment,
// context, input, output, [tea.WithoutSignals], and [tea.WithWindowSize] are
// always set by the harness and cannot be replaced here.
func WithProgramOptions(opts ...tea.ProgramOption) Option {
	return func(cfg *options) {
		cfg.programOpts = append(cfg.programOpts, opts...)
	}
}

// WithTimeout configures the maximum time blocking waits (startup, waits,
// settle, mutation) may run before failing the test. Defaults to
// [DefaultTimeout].
//
// Environment: STEEP_TIMEOUT (overrides this option unless [WithIgnoreEnv] is set).
func WithTimeout(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.timeout = timeout
	}
}

// WithCheck configures how often [Harness.WaitSettleMessages] re-checks
// for messages. Waits on the screen do not poll: they check it whenever it
// changes. Defaults to [DefaultCheckInterval]. While exporting, it is also
// how long the program's output must stay quiet before an action is marked
// (see [Harness.Mark]).
//
// Environment: STEEP_CHECK (overrides this option unless [WithIgnoreEnv] is set).
func WithCheck(interval time.Duration) Option {
	return func(cfg *options) {
		cfg.checkInterval = interval
	}
}

// WithSettle configures the quiet period settle waits require. Defaults to
// [DefaultSettleTimeout], and is capped at 80% of [WithTimeout]. While
// exporting, it also paces action marks (see [Harness.Mark]).
//
// Environment: STEEP_SETTLE (overrides this option unless [WithIgnoreEnv] is set).
func WithSettle(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.settleTimeout = timeout
	}
}

// WithSettleIgnoreMsgs marks message types that do not reset the quiet period
// used by [Harness.WaitSettleMessages]. Pass a value of each type to ignore,
// such as a zero value of a periodic tick type; types must match exactly.
// Calls accumulate.
func WithSettleIgnoreMsgs(types ...any) Option {
	return func(cfg *options) {
		for _, s := range types {
			if s == nil {
				continue
			}
			cfg.settleIgnore = append(cfg.settleIgnore, reflect.TypeOf(s))
		}
	}
}

// WithScrollOnInput determines whether [Harness.Type], [Harness.Key] (and the
// helpers built on it), and [Harness.Paste] first scroll a viewport scrolled
// with [Harness.ScrollUp] and friends back to the bottom, as a real terminal
// does on a key press. Enabled by default. Mouse events and
// [Harness.SendProgram] never scroll.
func WithScrollOnInput(enable bool) Option {
	return func(cfg *options) {
		cfg.scrollOnInput = enable
	}
}

// WithANSI determines whether ANSI sequences are kept (the default) in the
// output that assertions and [Harness.AssertSnapshot] compare. It does not
// change JSON snapshots, images, or recordings, or the screen text waits
// check, which has none.
func WithANSI(enable bool) Option {
	return func(cfg *options) {
		cfg.stripANSI = !enable
	}
}

// WithHooks appends hooks (see [Hook]). Calls accumulate.
func WithHooks(hooks ...Hook) Option {
	return func(cfg *options) {
		cfg.hooks = append(cfg.hooks, hooks...)
	}
}

// WithIgnoreEnv disables STEEP_* environment overrides. By default they are
// applied after every other option, so they override options set in code.
func WithIgnoreEnv(ignore bool) Option {
	return func(cfg *options) {
		cfg.ignoreEnv = ignore
	}
}

// WithSlow makes the harness sleep for d after every hooked action, before
// its After hooks run, giving the program real time between actions for
// animations, ticks, and timers. Recordings do not need it: they play back
// the timeline as it happened, at [reel.Profile.Speed].
//
// Environment: STEEP_SLOW (overrides this option unless [WithIgnoreEnv] is set).
func WithSlow(d time.Duration) Option {
	return func(cfg *options) {
		cfg.slow = d
	}
}

// WithLog logs every hooked action to tb, as [WithHooks] with a
// [LoggerHook] of a nil logger.
//
// Environment: STEEP_LOG (on, true, or 1) adds the same hook unless
// [WithIgnoreEnv] is set.
func WithLog(tb testing.TB) Option {
	return func(cfg *options) {
		cfg.hooks = append(cfg.hooks, LoggerHook(tb, nil))
	}
}

// WithRecordMessages records each message the model receives as a
// "steep.message" annotation of its type and %+v value. It is off by
// default, since programs that tick or animate receive many messages, each of
// which grows the timeline and ".cast" file.
//
// The [tea.EnvMsg] Bubble Tea sends the model is never recorded, since it
// holds the program's whole environment (see [WithEnvVars]). Other messages
// are recorded as they are, including any secret they hold, as is the text
// the program draws, which the effects package's Redact hides.
//
// Environment: STEEP_RECORD_MESSAGES (on or off) overrides this option
// unless [WithIgnoreEnv] is set.
func WithRecordMessages(enable bool) Option {
	return func(cfg *options) {
		cfg.recordMessages = enable
	}
}

// WithSource records where in the test each annotation and mark of the
// recording came from (see [reel.WithSource]): a "source" member of each
// annotation, and a "reel.mark" annotation after each mark, of the file,
// line, and function of the test's line that led to it, past any of the
// test's helpers. "steep.start" also gets the test's package, its module,
// and its directory in the module, and file paths are relative to the
// module's root, so they hold no home or build directories. Annotations
// and marks no line of the test led to, such as messages the model
// receives, or those [Harness.Close] places as a test cleanup, get none.
// Off by default; while off, no stack is walked.
//
// Environment: STEEP_SOURCE (on or off) overrides this option unless
// [WithIgnoreEnv] is set.
func WithSource(enable bool) Option {
	return func(cfg *options) {
		cfg.source = enable
	}
}

// WithEnvVars sets environment variables for the [tea.Program], in the form of
// "key=value". Without this option the program sees only the defaults:
//
//   - TTY_FORCE=1
//   - TERM=xterm-256color (see [WithColor])
//
// This option replaces those defaults with the test process environment plus
// vars, so pass TTY_FORCE and TERM again if the program needs them. It panics
// if an entry is not in "key=value" form.
//
// The program sees the whole test process environment, which in CI may hold
// tokens and credentials. [WithRecordMessages] does not record it, but the
// program can still read and draw it.
//
// For [NewExecHarness], vars and the test process environment replace those of
// the same name in cmd's environment.
func WithEnvVars(vars ...string) Option {
	return func(cfg *options) {
		current := map[string]string{}
		for _, entry := range os.Environ() {
			key, value, _ := strings.Cut(entry, "=")
			current[key] = value
		}
		for _, v := range vars {
			key, value, ok := strings.Cut(v, "=")
			if !ok {
				panic(fmt.Errorf("invalid environment variable: %s", v))
			}
			current[key] = value
		}
		cfg.envVars = make([]string, 0, len(current))
		for key, value := range current {
			cfg.envVars = append(cfg.envVars, key+"="+value)
		}
	}
}

// ColorProfile is the color support the harness terminal advertises to the
// program (see [WithColor]).
type ColorProfile int

const (
	// ColorASCII advertises no color support (NO_COLOR=1). Text attributes
	// such as bold still render.
	ColorASCII ColorProfile = iota + 1
	// ColorANSI advertises the 16 ANSI colors (TERM=xterm).
	ColorANSI
	// ColorANSI256 advertises 256 colors (TERM=xterm-256color). This is what
	// the harness terminal reports without [WithColor].
	ColorANSI256
	// ColorTrueColor advertises 24-bit color (TERM=xterm-256color,
	// COLORTERM=truecolor).
	ColorTrueColor
)

// String returns the profile's name.
func (p ColorProfile) String() string {
	switch p {
	case ColorASCII:
		return "ascii"
	case ColorANSI:
		return "ansi"
	case ColorANSI256:
		return "ansi256"
	case ColorTrueColor:
		return "truecolor"
	}
	return "unset"
}

// term256 is the TERM the harness terminal reports by default.
const term256 = "TERM=xterm-256color"

// colorEnvKeys are the color detection variables [WithColor] replaces.
var colorEnvKeys = []string{"TERM", "COLORTERM", "NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE"}

// env returns env with the color variables replaced by those that advertise p.
func (p ColorProfile) env(env []string) []string {
	out := slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		return slices.Contains(colorEnvKeys, key)
	})
	switch p {
	case ColorASCII:
		return append(out, term256, "NO_COLOR=1")
	case ColorANSI:
		return append(out, "TERM=xterm")
	case ColorTrueColor:
		return append(out, term256, "COLORTERM=truecolor")
	case ColorANSI256:
	}
	return append(out, term256)
}

// teaProfile is p as Bubble Tea's color profile, set explicitly so it does
// not depend on the host's terminfo database.
func (p ColorProfile) teaProfile() colorprofile.Profile {
	switch p {
	case ColorASCII:
		return colorprofile.ASCII
	case ColorANSI:
		return colorprofile.ANSI
	case ColorTrueColor:
		return colorprofile.TrueColor
	case ColorANSI256:
	}
	return colorprofile.ANSI256
}

// WithColor sets the color support the harness terminal advertises to the
// program, through TERM, COLORTERM, and NO_COLOR and [tea.WithColorProfile].
// Without it the terminal reports [ColorANSI256], so Bubble Tea downsamples
// 24-bit colors; use [ColorTrueColor] for gradients and blends.
//
// It overrides the color variables of [WithEnvVars], whatever the order. A
// [tea.WithColorProfile] passed through [WithProgramOptions] wins over it.
//
// For [NewExecHarness], it also overrides those of cmd's environment, and
// without it the command's color support is as [reel.Exec] sets it.
func WithColor(p ColorProfile) Option {
	return func(cfg *options) {
		cfg.color = p
	}
}

func applyEnv(tb testing.TB, cfg *options) {
	if cfg.ignoreEnv {
		return
	}

	if on, _ := parseSwitch(os.Getenv("STEEP_LOG")); on {
		cfg.hooks = append(cfg.hooks, LoggerHook(tb, nil))
	}

	if on, ok := parseSwitch(os.Getenv("STEEP_RECORD_MESSAGES")); ok {
		cfg.recordMessages = on
	}

	if on, ok := parseSwitch(os.Getenv("STEEP_SOURCE")); ok {
		cfg.source = on
	}

	envDuration("STEEP_SLOW", &cfg.slow)

	envDuration("STEEP_TYPING_DELAY", &cfg.typingDelay)

	applyExportEnv(tb, cfg)

	if v := os.Getenv("STEEP_SNAPSHOT_DIR"); v != "" {
		cfg.snapshotDir = v
	}

	envDuration("STEEP_TIMEOUT", &cfg.timeout)

	envDuration("STEEP_CHECK", &cfg.checkInterval)

	envDuration("STEEP_SETTLE", &cfg.settleTimeout)
}

// applyExportEnv applies the STEEP_EXPORT* and STEEP_RENDERER_* variables.
// Render options are appended to the profile's, so the environment wins
// without dropping the rest.
func applyExportEnv(tb testing.TB, cfg *options) {
	if v := os.Getenv("STEEP_EXPORT_MODE"); v != "" {
		if policy, ok := parseExportMode(v); ok {
			cfg.policy = policy
		}
	}
	if v := os.Getenv("STEEP_EXPORT_DIR"); v != "" {
		cfg.artifacts = v
	}

	var patch reel.Profile
	if v := os.Getenv("STEEP_EXPORT"); v != "" {
		outputs, err := reel.ParseFiles(v)
		if err != nil {
			tb.Fatalf("steep: STEEP_EXPORT: %v", err)
		}
		// Not nil, so it replaces the profile's outputs even when empty.
		patch.Outputs = append([]reel.File{}, outputs...)
		cfg.stills = nil
	}
	if v := os.Getenv("STEEP_EXPORT_FPS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			patch.FPS = n
		}
	}
	if v := os.Getenv("STEEP_EXPORT_SPEED"); v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			patch.Speed = f
		}
	}

	var rend []render.Option
	if v := os.Getenv("STEEP_RENDERER_SCALE"); v != "" {
		if scale, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			rend = append(rend, render.WithScale(scale))
		}
	}
	if v := os.Getenv("STEEP_RENDERER_FONT_FAMILY"); v != "" {
		rend = append(rend, render.WithFontFamilyName(v))
	}
	if rend != nil {
		patch.Render = append(slices.Clip(cfg.recording.Render), rend...)
	}

	cfg.recording = cfg.recording.Merge(patch)
}

// parseSwitch parses a boolean variable: on or off, or what
// [strconv.ParseBool] accepts. ok is false for anything else, such as an
// unset variable.
func parseSwitch(raw string) (on, ok bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "on":
		return true, true
	case "off":
		return false, true
	}
	on, err := strconv.ParseBool(raw)
	return on, err == nil
}

// parseExportMode parses STEEP_EXPORT_MODE: a boolean variable (see
// parseSwitch) for always or off, or always, or failure.
func parseExportMode(raw string) (exportPolicy, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "always":
		return exportAlways, true
	case "failure", "on-failure":
		return exportOnFailure, true
	}
	if on, ok := parseSwitch(raw); ok {
		if on {
			return exportAlways, true
		}
		return exportOff, true
	}
	return exportOff, false
}

// envDuration sets dst from the named variable, if it holds a duration.
func envDuration(name string, dst *time.Duration) {
	if d, err := time.ParseDuration(os.Getenv(name)); err == nil {
		*dst = d
	}
}
