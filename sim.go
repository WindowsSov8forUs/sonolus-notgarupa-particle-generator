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
	bySpeed  *RotationBySpeed
	clamp    *ClampVelocityModule
	uv       *UVModule
	velocity *VelocityModule
	force    *ForceModule
	custom   *CustomDataModule
	renderer *RendererProfile
	material *Material
	mesh     *meshInfo // render mode 4
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
	if id, ok := get("RotationBySpeedModule"); ok {
		v := b.Modules.RotationSpeed[id]
		s.bySpeed = &v
	}
	if id, ok := get("VelocityModule"); ok {
		v := b.Modules.Velocity[id]
		s.velocity = &v
	}
	if id, ok := get("ForceModule"); ok {
		v := b.Modules.Force[id]
		s.force = &v
	}
	if id, ok := get("CustomDataModule"); ok {
		v := b.Modules.CustomData[id]
		s.custom = &v
	}
	if r, ok := b.Renderers[p.Renderer]; ok {
		s.renderer = &r
		if len(r.Materials) > 0 && r.Materials[0] != nil {
			s.material = b.material(r.Materials[0].Name)
		}
		if r.RenderMode == 4 {
			s.mesh = &meshInfo{name: r.Mesh, geometry: b.Meshes[r.Mesh]}
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
	// 3D extensions (unused by systems without the matching modules)
	rot0X, rot0Y float64    // start rotation about X / Y (rotation3D), radians
	size0Z       float64    // start size z (size3D)
	moduleR      [9]float64 // velocity x, y, z, orbital x, y, z, radial, speed modifier, force
	bySpeedR     float64
	customR      [4]float64
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
	// 3D extensions
	rotX, rotY float64 // rotation about X / Y, radians
	sizeZ      float64
	custom     [4]float64 // custom-data vector 0
}

const simStep = 1.0 / 240

// simulate integrates one particle and samples it at the requested ages
// (seconds since birth, ascending, within [0, lifetime]).
func (s *system) simulate(p particleParams, ages []float64) []sample {
	out := make([]sample, 0, len(ages))
	pos := p.pos
	vel := p.dir.mul(p.speed)
	rot := p.rot0
	rotX, rotY := p.rot0X, p.rot0Y
	renderVel := vel
	age := 0.0
	next := 0
	emit := func() {
		for next < len(ages) && ages[next] <= age+1e-9 {
			smp := s.state(p, ages[next], pos, renderVel, rot)
			smp.rotX, smp.rotY = rotX, rotY
			out = append(out, smp)
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
			if s.rotation.SeparateAxes {
				rotX += s.rotation.X.eval(nAge, p.rotSpeedR) * p.rotSign * dt
				rotY += s.rotation.Y.eval(nAge, p.rotSpeedR) * p.rotSign * dt
			}
		}
		// velocity over lifetime (added to this frame's motion only), force
		// (accelerates the base velocity), then the clamp on the sum
		module, speedMod := s.moduleVelocity(pos, nAge, dt, p)
		if f := s.force; f != nil && f.Enabled {
			acc := Vec3{f.X.eval(nAge, p.moduleR[8]), f.Y.eval(nAge, p.moduleR[8]), f.Z.eval(nAge, p.moduleR[8])}
			if f.InWorldSpace {
				acc = s.worldToLocal(acc)
			}
			vel = vel.add(acc.mul(dt))
		}
		if c := s.clamp; c != nil && c.Enabled && (c.Dampen > 0 || c.Drag.Scalar != 0) {
			if module == (Vec3{}) {
				vel = s.limitVelocity(vel, nAge, dt, p)
			} else {
				vel = s.limitVelocity(vel.add(module), nAge, dt, p).sub(module)
			}
		}
		renderVel = vel
		if module != (Vec3{}) || speedMod != 1 {
			renderVel = vel.add(module).mul(speedMod)
		}
		// rotation by speed observes the effective velocity
		if b := s.bySpeed; b != nil && b.Enabled {
			n := 0.0
			if b.Range.Y != b.Range.X {
				n = math.Min(math.Max((renderVel.length()-b.Range.X)/(b.Range.Y-b.Range.X), 0), 1)
			}
			rot += b.Curve.eval(n, p.bySpeedR) * p.rotSign * dt
			if b.SeparateAxes {
				rotX += b.X.eval(n, p.bySpeedR) * p.rotSign * dt
				rotY += b.Y.eval(n, p.bySpeedR) * p.rotSign * dt
			}
		}
		pos = pos.add(renderVel.mul(dt))
		age += dt
		emit()
	}
	return out
}

// moduleVelocity is the Velocity over Lifetime contribution of one step
// (linear, orbital and radial, emitter-local) and its speed modifier.
func (s *system) moduleVelocity(pos Vec3, nAge, dt float64, p particleParams) (Vec3, float64) {
	v := s.velocity
	if v == nil || !v.Enabled {
		return Vec3{}, 1
	}
	r := p.moduleR
	lin := Vec3{v.X.eval(nAge, r[0]), v.Y.eval(nAge, r[1]), v.Z.eval(nAge, r[2])}
	if v.InWorldSpace {
		lin = s.worldToLocal(lin)
	}
	speedMod := v.SpeedModifier.eval(nAge, r[7])
	offset := Vec3{v.OrbitalOffsetX.eval(nAge, r[3]), v.OrbitalOffsetY.eval(nAge, r[4]), v.OrbitalOffsetZ.eval(nAge, r[5])}
	angular := Vec3{v.OrbitalX.eval(nAge, r[3]), v.OrbitalY.eval(nAge, r[4]), v.OrbitalZ.eval(nAge, r[5])}
	radial := v.Radial.eval(nAge, r[6])
	if angular == (Vec3{}) && radial == 0 {
		return lin, speedMod
	}
	rel := pos.sub(offset)
	step := dt * speedMod
	rotated := rotateEuler(rel, angular.mul(step))
	rotated = rotated.add(rotated.normalized().mul(step * radial))
	disp := rotated.sub(rel)
	if math.Abs(speedMod) > 1e-9 && dt > 1e-6 {
		lin = lin.add(disp.mul(1 / speedMod / dt))
	}
	return lin, speedMod
}

// rotateEuler rotates v about X, then Y, then Z (radians).
func rotateEuler(v, r Vec3) Vec3 {
	if r.X != 0 {
		c, s := math.Cos(r.X), math.Sin(r.X)
		v = Vec3{v.X, v.Y*c - v.Z*s, v.Y*s + v.Z*c}
	}
	if r.Y != 0 {
		c, s := math.Cos(r.Y), math.Sin(r.Y)
		v = Vec3{v.X*c + v.Z*s, v.Y, v.Z*c - v.X*s}
	}
	if r.Z != 0 {
		c, s := math.Cos(r.Z), math.Sin(r.Z)
		v = Vec3{v.X*c - v.Y*s, v.X*s + v.Y*c, v.Z}
	}
	return v
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
	sx, sy, sz3 := p.size0, p.size0Y, p.size0Z
	if sz := s.size; sz != nil && sz.Enabled {
		kx := sz.Curve.eval(n, p.sizeCurveR)
		ky, kz := kx, kx
		if sz.SeparateAxes {
			ky = sz.Y.eval(n, p.sizeCurveR)
			kz = sz.Z.eval(n, p.sizeCurveR)
		}
		sx *= kx
		sy *= ky
		sz3 *= kz
	}
	var custom [4]float64
	if c := s.custom; c != nil && c.Enabled && c.Mode0 == 1 {
		for k, curve := range []MinMaxCurve{c.V0, c.V1, c.V2, c.V3} {
			if k < c.Count0 {
				custom[k] = curve.eval(n, p.customR[k])
			}
		}
	}
	col := s.initial.StartColor.eval(0, p.startColorR)
	if c := s.color; c != nil && c.Enabled {
		col = mulColor(col, c.Gradient.eval(n, p.colorRatio))
	}
	return sample{t: age, pos: pos, vel: vel, sizeX: sx, sizeY: sy, sizeZ: sz3, rot: rot, col: col, frame: s.frame(n, p.frameR), custom: custom}
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
			step := 1 / rate
			// A capped system with an enormous rate (e.g. 10⁷/s with one
			// particle that lives 20 s, i.e. one persistent particle) can only
			// emit when a slot frees: step by the shortest lifetime per slot.
			if rate*dur > 10000 && maxN < 1<<30 {
				life := math.Min(s.initial.StartLifetime.eval(0, 0), s.initial.StartLifetime.eval(0, 1))
				step = math.Max(step, life/float64(maxN))
			}
			for k := 0; ; k++ {
				t := float64(k) * step
				if t >= dur {
					break
				}
				out = append(out, [2]float64{base + t, 1})
			}
		}
	}
	return out
}
