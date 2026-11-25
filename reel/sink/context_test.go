// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// closeCanceled closes s once ctx is canceled after a moment, and checks
// that Close fails with the cancellation soon after, writing nothing to
// path.
func closeCanceled(t *testing.T, s Sink, cancel context.CancelFunc, path string) {
	t.Helper()
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	err := s.Close()
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("Close took %v after the cancel", took)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Close = %v, want context.Canceled", err)
	}
	if _, serr := os.Stat(path); !errors.Is(serr, os.ErrNotExist) {
		t.Errorf("a canceled encode wrote its output: %v", serr)
	}
}

// TestVideoCloseCanceled checks that Close stops an encode that would not
// finish once its context is canceled.
func TestVideoCloseCanceled(t *testing.T) {
	useFakeFFmpeg(t)
	t.Setenv(fakeFFmpegEnv, "hang")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	path := filepath.Join(t.TempDir(), "out.mp4")
	s, err := CreateVideo(path, FormatMP4, WithDrivers(FFmpeg(exe)), WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WriteFrame(Frame{Image: solidFrame(1, 16, 16), Delay: time.Second}); err != nil {
		t.Fatal(err)
	}
	closeCanceled(t, s, cancel, path)
}

// TestGIFCloseCanceled checks that encoding a GIF in Close stops once its
// context is canceled.
func TestGIFCloseCanceled(t *testing.T) {
	t.Parallel()
	ctx := &cancelOnErr{}
	ctx.Context, ctx.cancel = context.WithCancel(t.Context())
	defer ctx.cancel()
	path := filepath.Join(t.TempDir(), "out.gif")
	s, err := CreateGIF(path, WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	img := solidFrame(0, 64, 48)
	for i := range 20 {
		for y := range 48 {
			img.Pix[y*img.Stride+(i%64)*4] = uint8(i * y)
		}
		if err = s.WriteFrame(Frame{Image: img, Delay: 20 * time.Millisecond}); err != nil {
			t.Fatal(err)
		}
	}
	// The encode cancels at its first check, after it has started.
	ctx.armed.Store(true)
	closeCanceled(t, s, ctx.cancel, path)
}

// cancelOnErr is a context that cancels itself at the first Err call once
// armed, so an encode is canceled partway through however fast it runs.
type cancelOnErr struct {
	context.Context

	cancel context.CancelFunc
	armed  atomic.Bool
}

func (c *cancelOnErr) Err() error {
	if c.armed.Load() {
		c.cancel()
	}
	return c.Context.Err()
}
