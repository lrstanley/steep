// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"math"
	"testing"
)

// TestScrollbackMax checks that a scrollback cap above MaxScrollback, from
// an option or later, is lowered to it.
func TestScrollbackMax(t *testing.T) {
	t.Parallel()
	term, err := NewRuntime(-1).Open(t.Context(), 4, 2, WithScrollbackLines(math.MaxInt))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if term.scrollbackLines != MaxScrollback {
		t.Errorf("WithScrollbackLines(MaxInt) keeps %d lines, want %d", term.scrollbackLines, MaxScrollback)
	}
	for n, want := range map[int]int{MaxScrollback + 1: MaxScrollback, 500: 500, -1: 0} {
		if err = term.SetScrollbackMaxLines(n); err != nil {
			t.Fatal(err)
		}
		if term.scrollbackLines != want {
			t.Errorf("SetScrollbackMaxLines(%d) keeps %d lines, want %d", n, term.scrollbackLines, want)
		}
	}
}

// TestKittyStorageMax checks that a Kitty storage limit above
// MaxKittyStorage, from an option or later, is lowered to it.
func TestKittyStorageMax(t *testing.T) {
	t.Parallel()
	term, err := NewRuntime(-1).Open(t.Context(), 4, 2, WithKittyStorage(math.MaxUint64))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if term.kitty.limit != MaxKittyStorage || term.kitty.def != MaxKittyStorage {
		t.Errorf("WithKittyStorage(MaxUint64) keeps %d bytes, want %d", term.kitty.limit, MaxKittyStorage)
	}
	if err = term.SetKittyImageStorageLimit(1 << 40); err != nil {
		t.Fatal(err)
	}
	if term.kitty.limit != MaxKittyStorage {
		t.Errorf("SetKittyImageStorageLimit(1 TiB) keeps %d bytes, want %d", term.kitty.limit, MaxKittyStorage)
	}
}
