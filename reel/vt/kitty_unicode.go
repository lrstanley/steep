// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import "strings"

// Unicode placeholders are cells of U+10EEEE that draw part of a virtual
// placement's image. libghostty-vt's render state finds them and reports
// each run of them on a row as a placement (see refreshImagesLocked).

// KittyPlaceholder reports whether c is a Kitty graphics Unicode placeholder
// cell, which draws part of an image (see [Buffer.Images]) instead of text.
func (c *Cell) KittyPlaceholder() bool {
	return strings.HasPrefix(c.Content, string(kittyPlaceholder))
}
