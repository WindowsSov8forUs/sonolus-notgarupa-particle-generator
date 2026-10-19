package particle

// Unity ParticleSystem evaluation (curves, gradients, shapes, transforms),
// following GarupaEditor's reconstruction of the original native code but in
// float64 (sub-pixel float32 rounding is irrelevant after conversion).

import "math"

const degToRad = math.Pi / 180

// eval evaluates a Hermite animation curve multiplied by scalar.
func (c AnimationCurve) eval(t, scalar float64) float64 {
	k := c.Keys
	switch {
	case len(k) == 0:
		return 0
	case len(k) == 1 || t < k[0].Time:
		return k[0].Value * scalar
	case t >= k[len(k)-1].Time:
		return k[len(k)-1].Value * scalar
	}
	i := 0
	for i+1 < len(k)-1 && t >= k[i+1].Time {
		i++
	}
	l, r := k[i], k[i+1]
	if math.IsInf(float64(l.OutSlope), 0) || math.IsInf(float64(r.InSlope), 0) {
		if math.IsInf(float64(l.OutSlope), -1) || math.IsInf(float64(r.InSlope), -1) {
			return r.Value * scalar
		}
		return l.Value * scalar
	}
	w := math.Max(r.Time-l.Time, 1e-4)
	s := (t - l.Time) / w
	m0, m1 := float64(l.OutSlope)*w, float64(r.InSlope)*w
	s2, s3 := s*s, s*s*s
	v := (2*s3-3*s2+1)*l.Value + (s3-2*s2+s)*m0 + (-2*s3+3*s2)*r.Value + (s3-s2)*m1
	return v * scalar
}

// keyTimes returns the interior key times of a curve (for segmentation).
func (c AnimationCurve) keyTimes() []float64 {
	var out []float64
	for _, k := range c.Keys {
		if k.Time > 0 && k.Time < 1 {
			out = append(out, k.Time)
		}
	}
	return out
}

// eval returns a MinMaxCurve value at normalised time t for random ratio r.
func (m MinMaxCurve) eval(t, r float64) float64 {
	switch m.State {
	case 0:
		return m.Scalar
	case 1:
		return m.MaxCurve.eval(t, m.Scalar)
	case 2:
		lo, hi := m.MinCurve.eval(t, m.Scalar), m.MaxCurve.eval(t, m.Scalar)
		return lo + (hi-lo)*r
	case 3:
		return m.MinScalar + (m.Scalar-m.MinScalar)*r
	}
	return m.Scalar
}

func (m MinMaxCurve) isRandom() bool { return m.State == 2 || m.State == 3 }
func (m MinMaxCurve) isCurve() bool  { return m.State == 1 || m.State == 2 }

func (m MinMaxCurve) keyTimes() []float64 {
	if !m.isCurve() {
		return nil
	}
	return append(m.MaxCurve.keyTimes(), m.MinCurve.keyTimes()...)
}

// eval evaluates a gradient at normalised time t (Unity stores key times as
// 16-bit fractions of 65535).
func (g Gradient) eval(t float64) Color {
	channel := func(alpha bool, comp func(Color) float64) float64 {
		n, times := g.NC, g.CTime
		if alpha {
			n, times = g.NA, g.ATime
		}
		if n < 2 {
			if n == 1 {
				return comp(g.Keys[0])
			}
			return 1
		}
		x := t * 65535
		x = math.Min(math.Max(x, times[0]), times[n-1])
		i := 1
		if g.Mode == 1 {
			i = 0
		}
		for i < n-1 && x > times[i] {
			i++
		}
		if g.Mode == 1 {
			return comp(g.Keys[i])
		}
		w := math.Min((x-times[i-1])/math.Max(times[i]-times[i-1], 1e-6), 1)
		return comp(g.Keys[i-1]) + (comp(g.Keys[i])-comp(g.Keys[i-1]))*w
	}
	return Color{
		R: channel(false, func(c Color) float64 { return c.R }),
		G: channel(false, func(c Color) float64 { return c.G }),
		B: channel(false, func(c Color) float64 { return c.B }),
		A: channel(true, func(c Color) float64 { return c.A }),
	}
}

func (g Gradient) keyTimes() []float64 {
	var out []float64
	for i := 0; i < g.NC; i++ {
		out = append(out, g.CTime[i]/65535)
	}
	for i := 0; i < g.NA; i++ {
		out = append(out, g.ATime[i]/65535)
	}
	return out
}

func lerpColor(a, b Color, r float64) Color {
	return Color{a.R + (b.R-a.R)*r, a.G + (b.G-a.G)*r, a.B + (b.B-a.B)*r, a.A + (b.A-a.A)*r}
}

func (m MinMaxGradient) eval(t, r float64) Color {
	switch m.State {
	case 0:
		return m.MaxColor
	case 1:
		return m.MaxGradient.eval(t)
	case 2:
		return lerpColor(m.MinColor, m.MaxColor, r)
	case 3:
		return lerpColor(m.MinGradient.eval(t), m.MaxGradient.eval(t), r)
	case 4:
		return m.MaxGradient.eval(r)
	}
	return m.MaxColor
}

func (m MinMaxGradient) isRandom() bool { return m.State >= 2 }

func (m MinMaxGradient) keyTimes() []float64 {
	switch m.State {
	case 1:
		return m.MaxGradient.keyTimes()
	case 3:
		return append(m.MaxGradient.keyTimes(), m.MinGradient.keyTimes()...)
	}
	return nil
}

func mulColor(a, b Color) Color { return Color{a.R * b.R, a.G * b.G, a.B * b.B, a.A * b.A} }

// --- vectors and transforms -------------------------------------------------

func (a Vec3) add(b Vec3) Vec3      { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) sub(b Vec3) Vec3      { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) mul(s float64) Vec3   { return Vec3{a.X * s, a.Y * s, a.Z * s} }
func (a Vec3) hadamard(b Vec3) Vec3 { return Vec3{a.X * b.X, a.Y * b.Y, a.Z * b.Z} }
func (a Vec3) length() float64      { return math.Sqrt(a.X*a.X + a.Y*a.Y + a.Z*a.Z) }
func (a Vec3) normalized() Vec3 {
	l := a.length()
	if l < 1e-12 {
		return Vec3{}
	}
	return a.mul(1 / l)
}

func (q Quat) rotate(v Vec3) Vec3 {
	// v' = q v q*
	u := Vec3{q.X, q.Y, q.Z}
	s := q.W
	uv := Vec3{u.Y*v.Z - u.Z*v.Y, u.Z*v.X - u.X*v.Z, u.X*v.Y - u.Y*v.X}
	uuv := Vec3{u.Y*uv.Z - u.Z*uv.Y, u.Z*uv.X - u.X*uv.Z, u.X*uv.Y - u.Y*uv.X}
	return v.add(uv.mul(2 * s)).add(uuv.mul(2))
}

// eulerZXY builds Unity's Euler rotation (degrees, applied Z, then X, then Y).
func eulerZXY(deg Vec3) Quat {
	qx := axisAngle(Vec3{1, 0, 0}, deg.X*degToRad)
	qy := axisAngle(Vec3{0, 1, 0}, deg.Y*degToRad)
	qz := axisAngle(Vec3{0, 0, 1}, deg.Z*degToRad)
	return qy.mul(qx).mul(qz)
}

func axisAngle(axis Vec3, a float64) Quat {
	s := math.Sin(a / 2)
	return Quat{axis.X * s, axis.Y * s, axis.Z * s, math.Cos(a / 2)}
}

func (a Quat) mul(b Quat) Quat {
	return Quat{
		a.W*b.X + a.X*b.W + a.Y*b.Z - a.Z*b.Y,
		a.W*b.Y - a.X*b.Z + a.Y*b.W + a.Z*b.X,
		a.W*b.Z + a.X*b.Y - a.Y*b.X + a.Z*b.W,
		a.W*b.W - a.X*b.X - a.Y*b.Y - a.Z*b.Z,
	}
}

// point maps a system-local point through the transform chain to the effect
// root (the receiving button): own transform first, then parents.
func (s *SystemDef) point(p Vec3) Vec3 {
	t := s.Transform
	p = t.Rotation.rotate(p.hadamard(t.Scale)).add(t.Position)
	for _, parent := range s.ParentTransforms {
		p = parent.Rotation.rotate(p.hadamard(parent.Scale)).add(parent.Position)
	}
	return p
}

// vector maps a direction (no translation).
func (s *SystemDef) vector(v Vec3) Vec3 {
	t := s.Transform
	v = t.Rotation.rotate(v.hadamard(t.Scale))
	for _, parent := range s.ParentTransforms {
		v = parent.Rotation.rotate(v.hadamard(parent.Scale))
	}
	return v
}

// --- shape sampling -----------------------------------------------------------

// shapeDraws are the random inputs of one shape sample, all in [0, 1).
// azimuth drives the angle around the shape axis (sphere θ, cone/circle arc).
type shapeDraws struct {
	azimuth, polar, radius, length float64
	box                            Vec3
}

// sampleShape returns emitter-local position and direction.
func sampleShape(sh *ShapeModule, d shapeDraws) (pos, dir Vec3) {
	if sh == nil || !sh.Enabled {
		return Vec3{}, Vec3{0, 0, 1}
	}
	radius := sh.Radius.Value
	inner := math.Min(math.Max(1-sh.RadiusThickness, 0), 1)
	arc := sh.Arc.Value * degToRad
	switch sh.Type {
	case 0, 1: // sphere / hemisphere
		theta := 2 * math.Pi * d.azimuth
		z := 2*d.polar - 1
		if sh.Type == 1 {
			z = d.polar
		}
		rad := math.Sqrt(math.Max(0, 1-z*z))
		dir = Vec3{rad * math.Cos(theta), rad * math.Sin(theta), z}
		ic := inner * inner * inner
		rr := math.Cbrt(ic*d.radius + (1 - d.radius))
		pos = dir.mul(radius * rr)
	case 4, 8: // cone / cone volume
		theta := arc * d.azimuth
		radial := math.Sqrt(math.Max(inner, 0.001)*d.radius + (1 - d.radius))
		rx, ry := radial*math.Cos(theta), radial*math.Sin(theta)
		a := sh.Angle * degToRad
		pos = Vec3{radius * rx, radius * ry, 0}
		dir = Vec3{math.Sin(a) * rx, math.Sin(a) * ry, math.Cos(a)}
		if sh.Type == 8 {
			pos = pos.add(dir.normalized().mul(sh.Length * d.length))
		}
	case 5: // box
		pos = Vec3{d.box.X - 0.5, d.box.Y - 0.5, d.box.Z - 0.5}
		dir = Vec3{0, 0, 1}
	case 10: // circle
		theta := arc * d.azimuth
		r := radius * math.Sqrt(inner*inner+(1-inner*inner)*d.radius)
		pos = Vec3{r * math.Cos(theta), r * math.Sin(theta), 0}
		dir = Vec3{math.Cos(theta), math.Sin(theta), 0}
	default:
		return Vec3{}, Vec3{0, 0, 1}
	}
	dir = shapeDirection(dir)
	if sh.SphericalDir > 0 {
		rd := pos.normalized()
		dir = dir.add(rd.sub(dir).mul(sh.SphericalDir))
	}
	// Shape transform: matrix columns are the Euler ZXY rotation scaled by the
	// shape scale (scale applies first), then translation.
	q := eulerZXY(sh.Rotation)
	pos = q.rotate(pos.hadamard(sh.Scale)).add(sh.Position)
	dir = shapeDirection(q.rotate(dir.hadamard(sh.Scale)))
	return pos, dir
}

// shapeDirection normalises an emission direction; a degenerate direction
// (e.g. a zero shape scale) falls back to +Z like the native code.
func shapeDirection(v Vec3) Vec3 {
	if v.X*v.X+v.Y*v.Y+v.Z*v.Z <= 1e-30 {
		return Vec3{0, 0, 1}
	}
	return v.normalized()
}

// azimuthSymmetric reports whether the shape's azimuth draw is a full turn
// around a fixed axis, so that positions and directions depend linearly on
// (cos ψ, sin ψ) and Sonolus' sinr/cosr inputs can randomise it.
func azimuthSymmetric(sh *ShapeModule) bool {
	if sh == nil || !sh.Enabled {
		return false
	}
	switch sh.Type {
	case 0, 1:
		return true
	case 4, 8, 10:
		return sh.Arc.Value >= 360 && sh.Arc.Mode == 0 && sh.RandomDirection == 0 && sh.RandomPosition == 0
	}
	return false
}
