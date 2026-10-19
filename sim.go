package particle

import "math"

// system bundles a ParticleSystem definition with its resolved modules.
type system struct {
	def      *SystemDef
	prof     *SystemProfile
	initial  *InitialModule
	emission *EmissionModule
	shape    *ShapeModule
	color    *ColorModule
	size     *SizeModule
	rotation *RotationModule
	clamp    *ClampVelocityModule
	uv       *UVModule
	renderer *RendererProfile
	material *Material
}

func resolveSystem(b *Bundle, def *SystemDef) *system {
	p, ok := b.Profiles[def.Profile]
	if !ok {
		return nil
	}
	s := &system{def: def, prof: &p}
	m := p.Modules
	get := func(name string) (string, bool) { id, ok := m[name]; return id, ok }
	if id, ok := get("InitialModule"); ok {
		v := b.Modules.Initial[id]
		s.initial = &v
	}
	if id, ok := get("EmissionModule"); ok {
		v := b.Modules.Emission[id]
		s.emission = &v
	}
	if id, ok := get("ShapeModule"); ok {
		v := b.Modules.Shape[id]
		s.shape = &v
	}
	if id, ok := get("ColorModule"); ok {
		v := b.Modules.Color[id]
		s.color = &v
	}
	if id, ok := get("SizeModule"); ok {
		v := b.Modules.Size[id]
		s.size = &v
	}
	if id, ok := get("RotationModule"); ok {
		v := b.Modules.Rotation[id]
		s.rotation = &v
	}
	if id, ok := get("ClampVelocityModule"); ok {
		v := b.Modules.Clamp[id]
		s.clamp = &v
	}
	if id, ok := get("UVModule"); ok {
		v := b.Modules.UV[id]
		s.uv = &v
	}
	if r, ok := b.Renderers[p.Renderer]; ok {
		s.renderer = &r
		if len(r.Materials) > 0 && r.Materials[0] != nil {
			s.material = b.material(r.Materials[0].Name)
		}
	}
	return s
}

// particleParams are the concrete values of one simulated particle. Ratios
// are the random inputs of the corresponding Unity MinMax values.
type particleParams struct {
	lifetime    float64
	speed       float64
	pos, dir    Vec3
	size0       float64 // start size (x)
	size0Y      float64
	rot0        float64 // radians
	rotSign     float64
	colorRatio  float64 // ColorModule random gradient ratio
	startColorR float64 // InitialModule startColor ratio
	sizeCurveR  float64
	rotSpeedR   float64
	clampR      float64
	gravityR    float64
	frameR      float64
}

type sample struct {
	t     float64 // seconds since birth
	pos   Vec3    // emitter-local
	vel   Vec3    // emitter-local
	sizeX float64
	sizeY float64
	rot   float64 // radians, Unity sign (clockwise on screen)
	col   Color   // linear multiplier incl. alpha
	frame int
}

const simStep = 1.0 / 240

// simulate integrates one particle and samples it at the requested ages
// (seconds since birth, ascending, within [0, lifetime]).
func (s *system) simulate(p particleParams, ages []float64) []sample {
	out := make([]sample, 0, len(ages))
	pos := p.pos
	vel := p.dir.mul(p.speed)
	rot := p.rot0
	age := 0.0
	next := 0
	emit := func() {
		for next < len(ages) && ages[next] <= age+1e-9 {
			out = append(out, s.state(p, ages[next], pos, vel, rot))
			next++
		}
	}
	emit()
	for next < len(ages) {
		dt := simStep
		if t := ages[next] - age; t < dt {
			dt = math.Max(t, 1e-9)
		}
		nAge := (age + dt*0.5) / p.lifetime
		// gravity (world −Y, converted to emitter space)
		if s.initial.GravityModifier.Scalar != 0 || s.initial.GravityModifier.State > 1 {
			g := s.initial.GravityModifier.eval(nAge, p.gravityR) * dt * -9.81
			vel = vel.add(s.worldToLocal(Vec3{0, g, 0}))
		}
		// angular velocity
		if s.rotation != nil && s.rotation.Enabled {
			rot += s.rotation.Curve.eval(nAge, p.rotSpeedR) * p.rotSign * dt
		}
		// limit velocity / drag
		if c := s.clamp; c != nil && c.Enabled && (c.Dampen > 0 || c.Drag.Scalar != 0) {
			vel = s.limitVelocity(vel, nAge, dt, p)
		}
		pos = pos.add(vel.mul(dt))
		age += dt
		emit()
	}
	return out
}

func (s *system) worldToLocal(v Vec3) Vec3 {
	t := s.def.Transform
	inv := Quat{-t.Rotation.X, -t.Rotation.Y, -t.Rotation.Z, t.Rotation.W}
	for i := len(s.def.ParentTransforms) - 1; i >= 0; i-- {
		pq := s.def.ParentTransforms[i].Rotation
		v = Quat{-pq.X, -pq.Y, -pq.Z, pq.W}.rotate(v)
	}
	return inv.rotate(v)
}

func (s *system) limitVelocity(v Vec3, nAge, dt float64, p particleParams) Vec3 {
	c := s.clamp
	dampen := 0.0
	if c.Dampen > 0 {
		dampen = 1 - math.Pow(1-c.Dampen, dt*30)
	}
	if c.SeparateAxis {
		lim := Vec3{c.X.eval(nAge, p.clampR), c.Y.eval(nAge, p.clampR), c.Z.eval(nAge, p.clampR)}
		f := func(x, l float64) float64 {
			l = math.Max(0, l)
			if math.Abs(x) <= l {
				return x
			}
			return x + (math.Copysign(l, x)-x)*dampen
		}
		v = Vec3{f(v.X, lim.X), f(v.Y, lim.Y), f(v.Z, lim.Z)}
	} else if c.Dampen > 0 {
		speed := v.length()
		lim := c.Magnitude.eval(nAge, p.clampR)
		if speed > lim {
			v = v.normalized().mul(speed + (lim-speed)*dampen)
		}
	}
	drag := math.Max(0, c.Drag.eval(nAge, p.clampR))
	if c.DragBySize {
		drag *= math.Max(p.size0, p.size0Y)
	}
	if c.DragByVelocity {
		drag *= v.length()
	}
	if drag > 0 {
		v = v.mul(math.Max(0, 1-drag*dt))
	}
	return v
}

func (s *system) state(p particleParams, age float64, pos, vel Vec3, rot float64) sample {
	n := math.Min(age/p.lifetime, 1)
	sx, sy := p.size0, p.size0Y
	if sz := s.size; sz != nil && sz.Enabled {
		kx := sz.Curve.eval(n, p.sizeCurveR)
		ky := kx
		if sz.SeparateAxes {
			ky = sz.Y.eval(n, p.sizeCurveR)
		}
		sx *= kx
		sy *= ky
	}
	col := s.initial.StartColor.eval(0, p.startColorR)
	if c := s.color; c != nil && c.Enabled {
		col = mulColor(col, c.Gradient.eval(n, p.colorRatio))
	}
	return sample{t: age, pos: pos, vel: vel, sizeX: sx, sizeY: sy, rot: rot, col: col, frame: s.frame(n, p.frameR)}
}

// frame returns the texture-sheet tile index (0 when there is no sheet).
func (s *system) frame(n, r float64) int {
	u := s.uv
	if u == nil || !u.Enabled || u.TilesX*u.TilesY <= 1 {
		return 0
	}
	tiles := u.TilesX * u.TilesY
	phase := u.Cycles * math.Max(n, 0)
	phase -= math.Floor(phase)
	f := u.FrameOverTime.eval(phase, r) + u.StartFrame.eval(n, r)
	f -= math.Floor(f)
	idx := int(f * float64(tiles))
	if u.AnimationType == 1 { // single row
		return u.RowIndex*u.TilesX + idx%u.TilesX
	}
	return min(idx, tiles-1)
}

// emissions lists (birth time, count) pairs of the system within one play of
// `window` seconds (looping systems repeat every lengthInSec).
func (s *system) emissions(window float64) [][2]float64 {
	var out [][2]float64
	if s.emission == nil || !s.emission.Enabled {
		return nil
	}
	dur := s.prof.System.LengthInSec
	delay := s.prof.System.StartDelay.eval(0, 0.5)
	cycles := 1
	if s.prof.System.Looping && dur > 0 {
		cycles = int(math.Ceil((window - delay) / dur))
	}
	maxN := 1 << 30
	if s.initial != nil && s.initial.MaxNumParticles > 0 {
		maxN = s.initial.MaxNumParticles
	}
	for c := 0; c < cycles; c++ {
		base := delay + float64(c)*dur
		for _, b := range s.emission.Bursts {
			if b.Probability <= 0 {
				continue
			}
			n := int(math.Round(b.Count.eval(0, 0.5) * math.Min(b.Probability, 1)))
			for i := 0; i < max(1, b.CycleCount); i++ {
				t := base + b.Time + float64(i)*b.RepeatInterval
				if t < base+dur+1e-9 && n > 0 {
					out = append(out, [2]float64{t, float64(min(n, maxN))})
				}
			}
		}
		rate := s.emission.Rate.eval(0, 0.5)
		if rate > 0 {
			for k := 0; ; k++ {
				t := float64(k) / rate
				if t >= dur {
					break
				}
				out = append(out, [2]float64{base + t, 1})
			}
		}
	}
	return out
}
