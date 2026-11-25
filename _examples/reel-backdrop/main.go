// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command backdrop exports a recording of steep's steep-showcase example in
// a translucent window, three ways: over a gradient, over a wallpaper image,
// and over nothing, so the image itself is translucent. The backdrop is
// what is behind the window, as a desktop is behind a terminal: it shows in
// the margin, and through the window's background where
// render.WithBackgroundOpacity makes it translucent.
//
// Each look is written as a PNG of the last frame, and as an animated WebP
// when ffmpeg can encode one. WebP and PNG keep the transparent look's
// translucency; view them over something other than white to see it.
package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"math"
	"os"
	"path/filepath"

	"getsteep.dev/steep/_examples/internal/showcase"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/render"
	"getsteep.dev/steep/reel/sink"
	"github.com/lrstanley/clix/v2"
)

// Flags are the command's flags.
type Flags struct {
	Dir string `name:"dir" type:"path" default:"." help:"Directory to write the files to."`
}

func main() {
	clix.NewWithDefaults[Flags]().Execute(func(ctx context.Context, cli *clix.CLI[Flags], _ *slog.Logger) error {
		files, err := run(ctx, cli.Flags.Dir)
		if err != nil {
			return err
		}
		for _, f := range files {
			fmt.Println(f) //nolint:forbidigo // The example lists the files it wrote.
		}
		return nil
	})
}

// margin is the space around the window, in pixels, where the backdrop
// shows around it.
const margin = 48

// looks are the backdrops the showcase is exported over, by file name.
func looks() map[string]render.Backdrop {
	return map[string]render.Backdrop{
		"gradient": render.LinearGradient(135,
			color.NRGBA{R: 0x67, G: 0x4e, B: 0xff, A: 0xff},
			color.NRGBA{R: 0xff, G: 0x6b, B: 0x9d, A: 0xff},
		),
		// Any image.Image, such as one decoded from a file, scaled to cover
		// the frame.
		"wallpaper": render.Wallpaper(aurora(1600, 1000)),
		// Nothing behind the window: the image keeps its translucency.
		"transparent": render.Solid(color.Transparent),
	}
}

// run exports the showcase over each backdrop into dir, returning the paths
// written.
func run(ctx context.Context, dir string) ([]string, error) {
	tl, err := showcase.Timeline()
	if err != nil {
		return nil, err
	}
	webp := sink.CheckVideo(sink.FormatWebP) == nil
	if !webp {
		fmt.Fprintln(os.Stderr, "skipping the WebP files: ffmpeg cannot encode WebP here")
	}
	var written []string
	for name, backdrop := range looks() {
		files := []string{filepath.Join(dir, name+".png")}
		if webp {
			files = append(files, filepath.Join(dir, name+".webp"))
		}
		err = reel.Export(ctx, tl, reel.Profile{
			Outputs: reel.MustFiles(files...),
			Render: []render.Option{
				render.WithWindowChrome(true),
				render.WithPadding(14),
				render.WithBorderRadius(12),
				render.WithMargin(margin),
				render.WithBackdrop(backdrop),
				// A quarter of the backdrop shows through the window, its
				// title bar, and the cells without a background of their
				// own.
				render.WithBackgroundOpacity(0.75),
				// The shadow shows around the window, not through it.
				render.WithShadow(0, 12, 32, nil),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		written = append(written, files...)
	}
	return written, nil
}

// aurora draws a wallpaper of w by h pixels: soft glows of color on a deep
// blue, darker toward the edges. It stands in for an image read from a file
// with [image.Decode].
func aurora(w, h int) *image.NRGBA {
	glows := []struct {
		x, y, r float64 // center and radius, as fractions of the width
		c       [3]float64
	}{
		{0.18, 0.25, 0.35, [3]float64{0x2d, 0xd4, 0xbf}},
		{0.75, 0.2, 0.3, [3]float64{0x8b, 0x5c, 0xf6}},
		{0.6, 0.8, 0.4, [3]float64{0xec, 0x48, 0x99}},
		{0.1, 0.9, 0.25, [3]float64{0xf5, 0x9e, 0x0b}},
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx, fy := float64(x)/float64(w), float64(y)/float64(w)
			px := [3]float64{0x0b, 0x10, 0x2a}
			for _, g := range glows {
				d := math.Hypot(fx-g.x, fy-g.y*float64(h)/float64(w)) / g.r
				k := 0.85 * math.Exp(-d*d*2)
				for i := range px {
					px[i] += (g.c[i] - px[i]) * k
				}
			}
			// The vignette darkens toward the corners.
			v := 1 - 0.35*math.Hypot(fx-0.5, (fy*float64(w)/float64(h))-0.5)
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(min(255, px[0]*v)), G: uint8(min(255, px[1]*v)), B: uint8(min(255, px[2]*v)), A: 0xff,
			})
		}
	}
	return img
}
