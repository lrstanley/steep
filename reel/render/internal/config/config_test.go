// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config_test

import (
	"image/color"
	"math"
	"testing"

	"getsteep.dev/steep/reel/render/internal/config"
	"getsteep.dev/steep/reel/render/units"
)

func TestDefaultOptions(t *testing.T) {
	t.Parallel()

	opts := config.DefaultOptions()
	if opts.Scale != config.DefaultScale {
		t.Fatalf("Scale = %v, want %v", opts.Scale, config.DefaultScale)
	}
	if opts.CursorThickness != 1 {
		t.Fatalf("CursorThickness = %v, want 1", opts.CursorThickness)
	}
	if opts.BackgroundOpacity != config.DefaultBgOpacity {
		t.Fatalf("BackgroundOpacity = %v, want %v", opts.BackgroundOpacity, config.DefaultBgOpacity)
	}
}

func TestParsePositivePx(t *testing.T) {
	t.Parallel()

	got, err := config.ParsePositivePx(5, "test")
	if err != nil {
		t.Fatalf("ParsePositivePx() error = %v", err)
	}
	if got.Int() != 5 {
		t.Fatalf("ParsePositivePx(5) = %d, want 5", got.Int())
	}
	_, err = config.ParsePositivePx(-1, "test")
	if err == nil {
		t.Fatal("ParsePositivePx(-1) error = nil, want error")
	}
}

func TestParseRequiredPx(t *testing.T) {
	t.Parallel()

	got, err := config.ParseRequiredPx(3, "test")
	if err != nil {
		t.Fatalf("ParseRequiredPx() error = %v", err)
	}
	if got.Int() != 3 {
		t.Fatalf("ParseRequiredPx(3) = %d, want 3", got.Int())
	}
	_, err = config.ParseRequiredPx(0, "test")
	if err == nil {
		t.Fatal("ParseRequiredPx(0) error = nil, want error")
	}
	_, err = config.ParseRequiredPx(-1, "test")
	if err == nil {
		t.Fatal("ParseRequiredPx(-1) error = nil, want error")
	}
}

func TestClonePalette(t *testing.T) {
	t.Parallel()

	orig := color.NRGBA{R: 1}
	p := config.ClonePalette(config.Options{}.Palette)
	p.Indexed = map[int]color.Color{1: orig}

	cloned := config.ClonePalette(p)
	cloned.Indexed[1] = color.NRGBA{R: 2}

	if orig.R != 1 {
		t.Fatalf("original color mutated: R=%d", orig.R)
	}
	c, ok := cloned.Indexed[1].(color.NRGBA)
	if !ok {
		t.Fatal("cloned indexed color is not NRGBA")
	}
	if c.R != 2 {
		t.Fatalf("cloned color R = %d, want 2", c.R)
	}
}

func TestParseIconHeightScale(t *testing.T) {
	t.Parallel()

	got, err := config.ParseIconHeightScale(1.5)
	if err != nil {
		t.Fatalf("ParseIconHeightScale() error = %v", err)
	}
	if got != 1.5 {
		t.Fatalf("ParseIconHeightScale(1.5) = %v, want 1.5", got)
	}
	_, err = config.ParseIconHeightScale(-0.1)
	if err == nil {
		t.Fatal("ParseIconHeightScale(-0.1) error = nil, want error")
	}
}

// TestParseBounds checks that pixel lengths and scales past their maxima
// are errors, so the layout's arithmetic cannot overflow.
func TestParseBounds(t *testing.T) {
	t.Parallel()

	for name, err := range map[string]error{
		"positive px":  second(config.ParsePositivePx(config.MaxPx+1, "padding")),
		"required px":  second(config.ParseRequiredPx(config.MaxPx+1, "cursor thickness")),
		"scale":        second(config.ParseScale(config.MaxScale + 0.5)),
		"scale NaN":    second(config.ParseScale(math.NaN())),
		"icon height":  second(config.ParseIconHeightScale(config.MaxIconHeightScale + 1)),
		"icon NaN":     second(config.ParseIconHeightScale(math.NaN())),
		"negative px":  second(config.ParsePositivePx(-1, "padding")),
		"zero px":      second(config.ParseRequiredPx(0, "cursor thickness")),
		"zero scale":   second(config.ParseScale(0)),
		"infinite":     second(config.ParseScale(math.Inf(1))),
		"negative ico": second(config.ParseIconHeightScale(-1)),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	for name, err := range map[string]error{
		"positive px": second(config.ParsePositivePx(config.MaxPx, "padding")),
		"required px": second(config.ParseRequiredPx(config.MaxPx, "cursor thickness")),
		"scale":       second(config.ParseScale(config.MaxScale)),
		"icon height": second(config.ParseIconHeightScale(config.MaxIconHeightScale)),
	} {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// second returns the error of a value and an error.
func second[T any](_ T, err error) error { return err }

func TestBoxThicknessOverride(t *testing.T) {
	t.Parallel()

	if (config.Options{}).BoxThicknessOverride() {
		t.Fatal("zero BoxThickness should not override")
	}
	if !(config.Options{BoxThickness: units.Px(2)}).BoxThicknessOverride() {
		t.Fatal("positive BoxThickness should override")
	}
}
