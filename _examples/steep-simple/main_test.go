// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"

	"github.com/lrstanley/steep"
	"github.com/lrstanley/steep/snapshot"
)

func TestModel(t *testing.T) {
	// The first frame is drawn before the model gets its window size, and shows
	// only the first rows. Row 20 is on screen only once the table has resized.
	steep.NewHarness(t, newRankingModel()).
		Wait("Guangzhou").
		AssertSnapshot(snapshot.WithANSI(false)).
		AssertJSON()
}
