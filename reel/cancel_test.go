// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// longCast returns a cast of n outputs 100ms apart, each changing the
// screen, with a mark "start" before the last ten.
func longCast(t *testing.T, n int) *Timeline {
	t.Helper()
	var b strings.Builder
	b.WriteString(`{"version":2,"width":80,"height":24}` + "\n")
	for i := range n {
		at := float64(i) / 10
		if i == n-10 {
			fmt.Fprintf(&b, "[%g,\"m\",\"start\"]\n", at)
		}
		fmt.Fprintf(&b, "[%g,\"o\",\"\\r\\nline %d %s\"]\n", at, i, strings.Repeat("x", i%60))
	}
	return castOf(t, b.String())
}

// TestExportCancelPromptly checks that canceling an export partway stops
// it within a few seconds, writing nothing.
func TestExportCancelPromptly(t *testing.T) {
	t.Parallel()
	tl := longCast(t, 5000)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	err := Export(ctx, tl, Profile{Outputs: MustFiles(filepath.Join(t.TempDir(), "out.gif"))})
	if took := time.Since(start); !errors.Is(err, context.Canceled) || took > 5*time.Second {
		t.Errorf("Export = %v after %v, want context.Canceled within seconds", err, took)
	}
}

// TestWriteCastCanceled checks that writing a cut timeline stops once its
// context is canceled, as the cut replays what came before.
func TestWriteCastCanceled(t *testing.T) {
	t.Parallel()
	tl, err := longCast(t, 100).Between("start", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var buf bytes.Buffer
	if _, err = tl.WriteCast(ctx, &buf, CastV3); !errors.Is(err, context.Canceled) {
		t.Errorf("WriteCast = %v, want context.Canceled", err)
	}
	if _, err = tl.WriteCast(t.Context(), &buf, CastV3); err != nil {
		t.Errorf("WriteCast: %v", err)
	}
}
