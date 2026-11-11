// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image/color"
	"image/draw"
	"math"

	"getsteep.dev/steep/reel/render/internal/config"
	idraw "getsteep.dev/steep/reel/render/internal/draw"
)

// ApplyFocusDimming darkens the terminal window when unfocused.
func ApplyFocusDimming(ctx config.WindowSource, img draw.Image) {
	if ctx.FocusDimming() <= 0 {
		return
	}
	idraw.Overlay(img, ctx.WindowBounds(), color.NRGBA{A: uint8(math.Round(255 * ctx.FocusDimming()))})
}
