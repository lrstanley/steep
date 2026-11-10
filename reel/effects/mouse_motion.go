// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"math"
	"time"
)

const (
	// glideReach is how much longer a glide gets each time the distance it
	// covers doubles, as a share of the shortest glide: Fitts's law, so a
	// glide across the screen takes about three times one to the next cell.
	glideReach = 0.35
	// glideLongest is the most a glide takes, in shortest glides.
	glideLongest = 4
	// followStiffness is the spring's angular frequency times the shortest
	// glide: stiff enough that a drag's pointer keeps within half a cell of
	// the latest cell sent, and soft enough to round a drag's steps from
	// row to row into a curve.
	followStiffness = 8
	// restSlack is how near, in cells, a spring comes to its cell before it
	// is taken to be at rest there.
	restSlack = 0.005
	// glideBow is how far a glide bows out of a straight line midway, as a
	// share of the distance it covers, as a hand's arcs.
	glideBow = 0.05
)

// add is v plus w.
func (v vec) add(w vec) vec { return vec{v.x + w.x, v.y + w.y} }

// sub is v minus w.
func (v vec) sub(w vec) vec { return vec{v.x - w.x, v.y - w.y} }

// scale is v times k.
func (v vec) scale(k float64) vec { return vec{v.x * k, v.y * k} }

// len is the length of v.
func (v vec) len() float64 { return math.Hypot(v.x, v.y) }

// lerp is the point u of the way from v to w.
func (v vec) lerp(w vec, u float64) vec { return v.add(w.sub(v).scale(u)) }

// motion is how the pointer moves to the cell of one mouse event, in cells
// and seconds, from its position, velocity, and acceleration at start on,
// so it moves on smoothly from wherever the motion before it left it.
//
// With a zero omega it glides there over dur along a minimum-jerk curve, the
// way a hand moves a mouse: easing in and out, at rest once there. With
// omega set it follows the cell as a critically damped spring does, which
// smooths motion quicker than a glide, such as a drag's, into a curve that
// keeps a little behind it; dur is then when it comes to rest.
//
// A glide bows out of its line, as far as bow midway.
type motion struct {
	start, dur     time.Duration
	from, vel, acc vec
	to, bow        vec
	omega          float64
}

// state is the pointer's position, velocity, and acceleration at offset t,
// at or after start.
func (m motion) state(t time.Duration) (p, v, a vec) {
	d := t - m.start
	if d >= m.dur {
		return m.to, vec{}, vec{}
	}
	s := d.Seconds()
	if m.omega > 0 {
		return spring(m.from, m.vel, m.to, m.omega, s)
	}
	span := m.dur.Seconds()
	p, v, a = quintic(m.from, m.vel, m.acc, m.to, span, s)
	// The bow is 64u³(1-u)³ of the way out, which peaks at 1 midway, and
	// leaves the ends' velocity and acceleration as they are.
	u := s / span
	w := u * (1 - u)
	p = p.add(m.bow.scale(64 * w * w * w))
	v = v.add(m.bow.scale(192 * w * w * (1 - 2*u) / span))
	a = a.add(m.bow.scale(384 * w * (1 - 5*u + 5*u*u) / (span * span)))
	return p, v, a
}

// bowOf is the bow of a glide from p to to, with cells aspect times as wide
// as high: out to the side, up, as a hand's arcs, and left when straight
// down or up.
func bowOf(p, to vec, aspect float64) vec {
	// Square cells first, so the bow is square to the line on screen.
	d := vec{(to.x - p.x) * aspect, to.y - p.y}
	side := vec{-d.y, d.x}
	if side.y > 0 || (side.y == 0 && side.x > 0) {
		side = side.scale(-1)
	}
	return vec{side.x * glideBow / aspect, side.y * glideBow}
}

// quintic is the state at s of the minimum-jerk curve from p0, moving at
// v0 and accelerating at a0, to rest at p1 at span: the quintic polynomial
// that meets those ends.
func quintic(p0, v0, a0, p1 vec, span, s float64) (p, v, a vec) {
	// In u = s/span, from 0 to 1, the curve is p0 + v0*span*u +
	// a0*span²/2*u² + c3*u³ + c4*u⁴ + c5*u⁵, with what is left to cover: by
	// position, velocity, and acceleration at u = 1.
	gap := p1.sub(p0).sub(v0.scale(span)).sub(a0.scale(span * span / 2))
	gv := v0.scale(-span).sub(a0.scale(span * span))
	ga := a0.scale(-span * span)
	c3 := gap.scale(10).sub(gv.scale(4)).add(ga.scale(0.5))
	c4 := gap.scale(-15).add(gv.scale(7)).sub(ga)
	c5 := gap.scale(6).sub(gv.scale(3)).add(ga.scale(0.5))
	u := s / span
	u2, u3 := u*u, u*u*u
	p = p0.add(v0.scale(span * u)).add(a0.scale(span * span / 2 * u2)).
		add(c3.scale(u3)).add(c4.scale(u3 * u)).add(c5.scale(u3 * u2))
	v = v0.add(a0.scale(span * u)).
		add(c3.scale(3 * u2 / span)).add(c4.scale(4 * u3 / span)).add(c5.scale(5 * u3 * u / span))
	a = a0.add(c3.scale(6 * u / (span * span))).add(c4.scale(12 * u2 / (span * span))).add(c5.scale(20 * u3 / (span * span)))
	return p, v, a
}

// spring is the state at s of a critically damped spring of angular
// frequency w pulling a point from p0, moving at v0, to target.
func spring(p0, v0, target vec, w, s float64) (p, v, a vec) {
	// The gap to target is (c1 + c2*s)*e^(-w*s).
	c1 := p0.sub(target)
	c2 := v0.add(c1.scale(w))
	e := math.Exp(-w * s)
	gap := c1.add(c2.scale(s))
	p = target.add(gap.scale(e))
	v = c2.sub(gap.scale(w)).scale(e)
	a = gap.scale(w * w).sub(c2.scale(2 * w)).scale(e)
	return p, v, a
}

// springRest is how long the spring from p0, moving at v0, to target takes
// to come within [restSlack] of it, moving slower than restSlack a step,
// and stay so.
func springRest(p0, v0, target vec, w float64) time.Duration {
	c1 := p0.sub(target).len()
	c2 := v0.add(p0.sub(target).scale(w)).len()
	// The gap and the velocity over w are both at most (c + k*s)*e^(-w*s),
	// which rises to its peak at most once, then falls.
	c, k := 2*c1+c2/w, 2*c2
	peak := max(0, 1/w-c/k)
	if k == 0 {
		peak = 0
	}
	for n := range 400 {
		s := float64(n) / (4 * w)
		if s >= peak && (c+k*s)*math.Exp(-w*s) < restSlack {
			return time.Duration(s * float64(time.Second))
		}
	}
	return time.Duration(100 / w * float64(time.Second))
}
