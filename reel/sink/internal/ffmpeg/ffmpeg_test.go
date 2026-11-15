// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package ffmpeg

import "testing"

func TestAtLeast(t *testing.T) {
	t.Parallel()

	// Whether each version is at least 5.1 (-fps_mode) and 6.1
	// (-enc_time_base demux).
	for version, want := range map[string][2]bool{
		"ffmpeg version 4.4.2-0ubuntu0.22.04.1 Copyright": {false, false},
		"ffmpeg version 5.0.1 Copyright":                  {false, false},
		"ffmpeg version 5.1.2 Copyright":                  {true, false},
		"ffmpeg version 6.0 Copyright":                    {true, false},
		"ffmpeg version n6.1 Copyright":                   {true, true},
		"ffmpeg version 7.1-full_build-www.gyan.dev":      {true, true},
		"ffmpeg version 9.0.2 Copyright":                  {true, true},
		"ffmpeg version N-113000-g0123456789 Copyright":   {true, true},
		"ffmpeg version 2024-01-01-git-abc Copyright":     {true, true},
		"something else entirely":                         {true, true},
	} {
		out := []byte(version + "\nbuilt with gcc")
		if got := [2]bool{atLeast(out, 5, 1), atLeast(out, 6, 1)}; got != want {
			t.Errorf("%q: at least 5.1, 6.1 = %v, want %v", version, got, want)
		}
	}
}
