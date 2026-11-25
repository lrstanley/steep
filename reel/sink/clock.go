// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import "time"

// clock times frames in whole ticks of a format's clock. It rounds each
// frame's end, not its delay, so a recording keeps its length however
// finely its frames are timed: 30 FPS frames on a centisecond clock play 3,
// 4, and 3 ticks rather than 3 each, which would drift to 33 FPS.
type clock struct {
	tick time.Duration
	// end is the exact end of the frames so far; shown is where the last
	// frame ends on the clock.
	end, shown time.Duration
}

// next returns the delay, in whole ticks, of a frame that plays for delay:
// from the last frame's end on the clock to the tick nearest its own exact
// end. A frame always plays at least one tick, even when that puts it past
// its exact end; later frames make up for it.
func (c *clock) next(delay time.Duration) time.Duration {
	c.end += max(delay, 0)
	d := max(c.end.Round(c.tick)-c.shown, c.tick)
	c.shown += d
	return d
}
