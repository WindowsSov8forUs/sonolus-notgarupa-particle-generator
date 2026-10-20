package particle

// Conversion of Unity particle systems into Sonolus particle groups.
//
// Every Unity particle becomes one Sonolus group instance whose particles are
// consecutive time segments of its life (Sonolus tweens each property once per
// particle, so curves, drag and colour keys are followed piecewise). Inside a
// segment every property is linear in time and linear in the Sonolus random
// inputs:
//
//	r1 (cosr1/sinr1)  azimuth around the emitter axis (sphere, full-turn cone/circle)
//	r3                start size (when it does not feed back into the motion)
//	r4                start rotation
//	r5..r7            box shape position
//
// Every other random choice (lifetime, speed, colour, texture frame, polar
// angle, partial arcs, …) is stratified: a group is emitted per stratum with a
// low-discrepancy concrete value and the stratum's share of the burst count.

import (
	"fmt"
	"math"
	"sort"

	"github.com/WindowsSov8forUs/sonolus-core-go/core/resource"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/imgutil"
)

type expr = map[string]float64

// spriteKey identifies a texture-sheet tile of a bundle's texture.
type spriteKey struct {
	bundle  string
	texture string
	frame   int
	tilesX  int
	tilesY  int
	blend   string // "additive" | "premultiplied" | "alpha"
}

// projection is how a system's particles map to Sonolus rectangles.
type projection int

const (
	projBillboard projection = iota // view or lane-plane billboard
	projStretched                   // stretched billboard
	projQuadMesh                    // built-in Quad mesh spinning in its plane
	projQuad3D                      // billboard or Quad with a 3D rotation
	projRaster                      // rasterised mesh
)

type outParticle struct {
	sprite     spriteKey
	color      string
	start, end float64 // seconds within the effect
	from, to   [6]expr // x, y, w, h, r, a
}

type outGroup struct {
	count     int
	particles []outParticle
}

// part is one output Sonolus effect: the view-aligned systems of a prefab, or
// its lane-plane systems that share one tilt.
type part struct {
	name     string
	prefab   string
	bundle   string  // texture source
	planeDeg float64 // 0 for the view part
	duration float64
	loop     bool
	groups   []outGroup
	systems  []string
	notes    []string
	width    int // HABAHIRO width variant (2..7), 0 otherwise
}

type convertOptions struct {
	maxStrata    int
	maxSegments  int
	posTol       float64
	loopSeconds  float64 // period of looping effects
	maxLoopCycle float64
}

var irrational = [...]float64{0.6180339887, 0.4142135624, 0.7320508076, 0.2360679775, 0.6457513111, 0.1622776602, 0.3166247904, 0.6055512755, 0.8284271247, 0.1231056256, 0.4721359550}

type strataDims struct {
	values [11]float64
}

func lowDiscrepancy(index int) strataDims {
	var d strataDims
	for k := range d.values {
		v := 0.5 + float64(index)*irrational[k]
		d.values[k] = v - math.Floor(v)
	}
	return d
}

// planeTilt returns the tilt (degrees about X, in (−180, 180]) of a
// lane-plane system, or ok=false. A Quad mesh adds its constant start tilt.
func planeTilt(s *system) (float64, bool) {
	// stretched billboards always face the camera along their velocity:
	// render alignment does not apply to them
	if s.renderer == nil || s.renderer.Alignment != 2 || s.renderer.RenderMode == 1 || s.isRaster() {
		return 0, false
	}
	meshRX := 0.0
	if s.isQuadMesh() {
		rx, ok := s.meshTilt()
		if !ok {
			return 0, false
		}
		meshRX = rx
	} else if s.rotates3D() {
		return 0, false
	}
	q := s.def.Transform.Rotation
	for _, p := range s.def.ParentTransforms {
		if math.Abs(p.Rotation.X)+math.Abs(p.Rotation.Y)+math.Abs(p.Rotation.Z) > 1e-4 {
			return 0, false
		}
	}
	if math.Abs(q.Y) > 1e-3 || math.Abs(q.Z) > 1e-3 {
		return 0, false
	}
	deg := math.Round((2*math.Atan2(q.X, q.W) + meshRX) / degToRad)
	deg = math.Mod(deg, 360)
	switch {
	case deg > 180:
		deg -= 360
	case deg <= -180:
		deg += 360
	}
	return deg, true
}

// projectionOf picks how a system is drawn (ok=false: not drawable).
func projectionOf(s *system, planeDeg float64) (projection, string) {
	switch s.renderer.RenderMode {
	case 0:
		if planeDeg == 0 && s.rotates3D() {
			return projQuad3D, ""
		}
		return projBillboard, ""
	case 1:
		return projStretched, ""
	case 4:
		switch {
		case s.isRaster() && s.mesh.geometry != nil:
			return projRaster, ""
		case s.isRaster():
			return -1, "mesh geometry not decoded"
		case planeDeg != 0 || !s.rotates3D():
			return projQuadMesh, ""
		}
		return projQuad3D, ""
	}
	return -1, fmt.Sprintf("render mode %d not supported", s.renderer.RenderMode)
}

// convertSystem appends groups for one Unity system to the part.
func convertSystem(pt *part, s *system, o convertOptions, steady bool) error {
	init := s.initial
	tex := s.material.Texture
	blend := "alpha"
	switch {
	case s.material.SourceBlend == 5 && s.material.DestinationBlend == 1:
		blend = "additive"
	case s.material.SourceBlend == 1 && s.material.DestinationBlend == 10:
		blend = "premultiplied"
	}
	tilesX, tilesY := 1, 1
	if s.uv != nil && s.uv.Enabled {
		tilesX, tilesY = s.uv.TilesX, s.uv.TilesY
	}
	mode, why := projectionOf(s, pt.planeDeg)
	if mode < 0 {
		pt.notes = append(pt.notes, fmt.Sprintf("%s: %s, skipped", s.def.Path, why))
		return nil
	}
	stretched := mode == projStretched
	cosT := 1.0
	if pt.planeDeg != 0 {
		cosT = math.Cos(pt.planeDeg * degToRad)
	}
	gravity := init.GravityModifier.Scalar != 0 || init.GravityModifier.State > 1
	clampAniso := s.clamp != nil && s.clamp.Enabled && s.clamp.SeparateAxis
	linearAzimuth := azimuthSymmetric(s.shape) && !stretched && !gravity && !clampAniso && !init.Rotation3D
	box := s.shape != nil && s.shape.Enabled && s.shape.Type == 5
	sizeCoupled := s.clamp != nil && s.clamp.Enabled && s.clamp.DragBySize
	linearSize := init.StartSize.isRandom() && !sizeCoupled && !init.Size3D
	pivot := s.renderer.Pivot
	meshPivot := (mode == projQuadMesh || mode == projQuad3D && s.isQuadMesh()) && pivot.X*pivot.X+pivot.Y*pivot.Y > 1e-10
	linearRot := init.StartRotation.isRandom() && !stretched && mode != projQuad3D && mode != projRaster && !meshPivot

	period := s.prof.System.LengthInSec
	window := pt.duration
	emissions := s.emissions(window)
	// Steady-state loop: particles born up to one lifetime before the effect
	// starts are visible at its beginning, which is what every later loop
	// cycle shows. A prewarmed system starts that way; any other system
	// starts cold when the effect starts playing (a TapKeep at the start of
	// a hold), so it is only backfilled when its particles live within one
	// loop window. Longer-lived ones (TapKeep's rotating rings: lifetime
	// 20 s, max one particle) are born at the start instead of leaving the
	// dying tail of a backfilled one.
	maxLife := init.StartLifetime.eval(0, 1)
	if steady && s.prof.System.Looping && (s.prof.System.Prewarm || maxLife <= pt.duration) {
		pre := math.Ceil(init.StartLifetime.eval(0, 1)/period) * period
		var shifted [][2]float64
		for k := 1; float64(k)*period <= pre+1e-9; k++ {
			for _, e := range s.emissions(period) {
				shifted = append(shifted, [2]float64{e[0] - float64(k)*period, e[1]})
			}
		}
		emissions = append(shifted, emissions...)
	}
	sort.SliceStable(emissions, func(i, j int) bool { return emissions[i][0] < emissions[j][0] })

	maxAlive := 1 << 30
	if init.MaxNumParticles > 0 {
		maxAlive = init.MaxNumParticles
	}
	type alive struct{ end float64 }
	var living []alive
	global := 0
	for _, em := range emissions {
		birth, count := em[0], int(em[1])
		kept := living[:0]
		for _, a := range living {
			if a.end > birth {
				kept = append(kept, a)
			}
		}
		living = kept
		count = min(count, maxAlive-len(living))
		if count <= 0 {
			continue
		}
		strata := min(count, o.maxStrata)
		if !linearAzimuth {
			strata = min(count, 32)
		}
		for j := 0; j < strata; j++ {
			n := count / strata
			if j < count%strata {
				n++
			}
			dims := lowDiscrepancy(global)
			global++
			g, life, err := buildGroup(s, pt, o, dims, birth, linearAzimuth, box, linearSize, linearRot, mode, cosT, spriteKey{bundle: pt.bundle, texture: tex, tilesX: tilesX, tilesY: tilesY, blend: blend})
			if err != nil {
				return err
			}
			if g == nil {
				continue
			}
			g.count = n
			pt.groups = append(pt.groups, *g)
			for i := 0; i < n; i++ {
				living = append(living, alive{birth + life})
			}
		}
	}
	pt.systems = append(pt.systems, s.def.Path)
	return nil
}

// variant is one evaluation of the particle used to extract linear coefficients.
type variant struct {
	key     string  // Sonolus input this variant isolates ("" = base)
	azimuth float64 // turns
	box     Vec3
	sizeR   float64
	rotR    float64
}

func buildGroup(s *system, pt *part, o convertOptions, d strataDims, birth float64,
	linearAzimuth, box, linearSize, linearRot bool, mode projection, cosT float64, sk spriteKey) (*outGroup, float64, error) {
	init := s.initial
	v := d.values
	base := particleParams{
		lifetime:    math.Max(init.StartLifetime.eval(birth, v[0]), 1e-4),
		speed:       init.StartSpeed.eval(0, v[1]),
		startColorR: v[2],
		colorRatio:  v[3],
		sizeCurveR:  v[4],
		rotSpeedR:   v[5],
		clampR:      v[6],
		gravityR:    v[6],
		frameR:      v[7],
		rotSign:     1,
	}
	if init.RandomizeRotDir > 0 && v[8] < init.RandomizeRotDir {
		base.rotSign = -1
	}
	// further random inputs of the 3D modules, derived from the stratum's
	// draws (they leave the original inputs untouched)
	frac := func(x float64) float64 { return x - math.Floor(x) }
	for k := range base.moduleR {
		base.moduleR[k] = frac(v[(k+2)%11]*0.754877666 + v[(k+5)%11]*0.569840291)
	}
	base.bySpeedR = frac(v[5]*0.381966 + v[2]*0.618034)
	for k := range base.customR {
		base.customR[k] = frac(v[(k+3)%11]*0.414213562 + v[(k+7)%11]*0.732050808)
	}
	sizeR := v[9]
	rotR := v[10]
	shapeBase := shapeDraws{azimuth: v[1]*0.37 + v[5]*0.63, polar: v[8], radius: v[9], length: v[10]}
	shapeBase.azimuth -= math.Floor(shapeBase.azimuth)

	baseVariant := variant{key: "", azimuth: shapeBase.azimuth, sizeR: sizeR, rotR: rotR}
	if linearSize {
		baseVariant.sizeR = 0
	}
	if linearRot {
		baseVariant.rotR = 0
	}
	with := func(key string, f func(*variant)) variant {
		v := baseVariant
		v.key = key
		f(&v)
		return v
	}
	var variants []variant
	if linearAzimuth {
		baseVariant.azimuth = 0
		variants = []variant{baseVariant, with("#pi", func(v *variant) { v.azimuth = 0.5 }), with("#half", func(v *variant) { v.azimuth = 0.25 })}
	} else {
		variants = []variant{baseVariant}
	}
	if box {
		variants = append(variants,
			with("r5", func(v *variant) { v.box.X = 1 }),
			with("r6", func(v *variant) { v.box.Y = 1 }),
			with("r7", func(v *variant) { v.box.Z = 1 }))
	}
	if linearSize {
		variants = append(variants, with("r3", func(v *variant) { v.sizeR = 1 }))
	}
	if linearRot {
		variants = append(variants, with("r4", func(v *variant) { v.rotR = 1 }))
	}

	life := base.lifetime
	sampled := life
	if life > 60 {
		// a persistent particle (e.g. lifetime 100000 s): sample only the
		// part inside the effect window
		sampled = math.Min(life, math.Max(pt.duration-birth, 0)+0.05)
	}
	ages := denseAges(sampled)
	// evaluate every variant → per age the 6 output quantities
	type evaluated struct {
		q       [][6]float64
		frames  []int
		cols    []Color
		rasters []string
	}
	evals := make([]evaluated, len(variants))
	for vi, va := range variants {
		p := base
		sd := shapeBase
		sd.azimuth = va.azimuth
		sd.box = va.box
		p.pos, p.dir = sampleShape(s.shape, sd)
		p.size0 = math.Max(0, init.StartSize.eval(birth, va.sizeR))
		p.size0Y = p.size0
		if init.Size3D {
			p.size0Y = math.Max(0, init.StartSizeY.eval(birth, va.sizeR))
		}
		p.rot0 = init.StartRotation.eval(birth, va.rotR)
		if init.Rotation3D {
			p.rot0X = init.StartRotationX.eval(birth, va.rotR)
			p.rot0Y = init.StartRotationY.eval(birth, va.rotR)
		}
		p.size0Z = p.size0
		if init.Size3D {
			p.size0Z = math.Max(0, init.StartSizeZ.eval(birth, va.sizeR))
		}
		samples := s.simulate(p, ages)
		e := evaluated{}
		for _, smp := range samples {
			q, raster := project(s, smp, mode, cosT, pt.bundle)
			e.q = append(e.q, q)
			e.frames = append(e.frames, smp.frame)
			e.cols = append(e.cols, smp.col)
			e.rasters = append(e.rasters, raster)
		}
		evals[vi] = e
	}
	// linear decomposition per age
	n := len(ages)
	coef := make([]map[string][6]float64, n)
	for i := 0; i < n; i++ {
		m := map[string][6]float64{}
		baseQ := evals[0].q[i]
		if linearAzimuth {
			q0, qpi, qh := evals[0].q[i], evals[1].q[i], evals[2].q[i]
			var c, cc, cs [6]float64
			for k := 0; k < 6; k++ {
				c[k] = (q0[k] + qpi[k]) / 2
				cc[k] = (q0[k] - qpi[k]) / 2
				cs[k] = qh[k] - c[k]
			}
			// rotation and size do not depend on the azimuth
			for _, k := range []int{2, 3, 4, 5} {
				c[k], cc[k], cs[k] = q0[k], 0, 0
			}
			m["c"], m["cosr1"], m["sinr1"] = c, cc, cs
			baseQ = q0
		} else {
			m["c"] = baseQ
		}
		start := 1
		if linearAzimuth {
			start = 3
		}
		for vi := start; vi < len(variants); vi++ {
			var dq [6]float64
			for k := 0; k < 6; k++ {
				dq[k] = evals[vi].q[i][k] - baseQ[k]
			}
			m[variants[vi].key] = dq
		}
		coef[i] = m
	}

	// segment boundaries: curve keys, frame changes, adaptive refinement
	cuts := map[int]bool{0: true, n - 1: true}
	for _, kt := range keyTimes(s) {
		cuts[nearestIndex(ages, kt*life)] = true
	}
	for i := 1; i < n; i++ {
		if evals[0].frames[i] != evals[0].frames[i-1] || evals[0].rasters[i] != evals[0].rasters[i-1] {
			cuts[i] = true
		}
	}
	idx := sortedKeys(cuts)
	idx = refine(idx, coef, o)
	if len(idx)-1 > o.maxSegments {
		idx = thin(idx, o.maxSegments)
	}

	g := &outGroup{}
	for si := 0; si+1 < len(idx); si++ {
		a, b := idx[si], idx[si+1]
		mid := (a + b) / 2
		col := evals[0].cols[mid]
		key := sk
		key.frame = evals[0].frames[mid]
		if r := evals[0].rasters[mid]; r != "" {
			key.texture, key.frame, key.tilesX, key.tilesY = r, 0, 1, 1
			if key.blend != "additive" {
				key.blend = "alpha"
			}
		}
		start, end := birth+ages[a], birth+ages[b]
		if end <= 0 || start >= pt.duration || end-start < 1e-6 {
			continue
		}
		var from, to [6]expr
		for k := 0; k < 6; k++ {
			from[k], to[k] = expr{}, expr{}
			for name, c := range coef[a] {
				if c[k] != 0 {
					from[k][name] = round6(c[k])
				}
			}
			for name, c := range coef[b] {
				if c[k] != 0 {
					to[k][name] = round6(c[k])
				}
			}
		}
		// clip to the effect window (steady-state loops start before 0)
		if start < 0 || end > pt.duration {
			cs, ce := math.Max(start, 0), math.Min(end, pt.duration)
			ta, tb := (cs-start)/(end-start), (ce-start)/(end-start)
			for k := 0; k < 6; k++ {
				f, t := from[k], to[k]
				from[k], to[k] = lerpExpr(f, t, ta), lerpExpr(f, t, tb)
			}
			start, end = cs, ce
		}
		if col.A <= 0 && from[5]["c"] == 0 && to[5]["c"] == 0 && len(from[5]) == 0 && len(to[5]) == 0 {
			continue // invisible
		}
		g.particles = append(g.particles, outParticle{
			sprite: key, color: hexColor(col), start: start, end: end, from: from, to: to,
		})
	}
	if len(g.particles) == 0 {
		return nil, life, nil
	}
	return g, life, nil
}

// project maps an emitter-local sample to effect coordinates (particle units,
// y up) and returns x, y, w (half width), h (half height), r (radians CCW), a,
// and the raster sprite of a rasterised mesh.
func project(s *system, smp sample, mode projection, cosT float64, bundle string) ([6]float64, string) {
	scale := math.Abs(s.def.Transform.Scale.X)
	for _, p := range s.def.ParentTransforms {
		scale *= math.Abs(p.Scale.X)
	}
	head := s.def.point(smp.pos)
	switch mode {
	case projQuadMesh:
		// in-plane spin turns counter-clockwise; the pivot moves with it
		x, y := head.X, head.Y/cosT
		if pv := s.renderer.Pivot; pv.X != 0 || pv.Y != 0 {
			px, py := pv.X*smp.sizeX*scale, pv.Y*smp.sizeY*scale
			c, sn := math.Cos(smp.rot), math.Sin(smp.rot)
			x, y = x+px*c-py*sn, y+px*sn+py*c
		}
		return [6]float64{x, y, smp.sizeX * scale / 2, smp.sizeY * scale / 2, smp.rot, smp.col.A}, ""
	case projQuad3D:
		u, v := s.quadAxes(smp, scale)
		x, y := head.X, head.Y
		if pv := s.renderer.Pivot; s.isQuadMesh() && (pv.X != 0 || pv.Y != 0) {
			off := u.mul(2 * pv.X).add(v.mul(2 * pv.Y))
			x, y = x+off.X, y+off.Y
		}
		w, h, r := rectOf(u, v)
		return [6]float64{x, y, w, h, r, smp.col.A}, ""
	case projRaster:
		key, g := s.rasterFor(bundle, smp)
		k := smp.sizeX * scale
		return [6]float64{head.X + k*(g[0]+g[2])/2, head.Y + k*(g[1]+g[3])/2, k * (g[2] - g[0]) / 2, k * (g[3] - g[1]) / 2, 0, smp.col.A}, key
	}
	if mode == projStretched {
		v := s.def.vector(smp.vel)
		speed := v.length()
		length := smp.sizeX*scale*s.renderer.LengthScale + speed*s.renderer.VelocityScale
		dir := v.normalized()
		if speed < 1e-9 {
			dir = Vec3{0, 1, 0}
		}
		tail := head.sub(dir.mul(length))
		cx, cy := (head.X+tail.X)/2, (head.Y+tail.Y)/2/cosT
		dx, dy := tail.X-head.X, (tail.Y-head.Y)/cosT
		half := math.Hypot(dx, dy) / 2
		r := math.Atan2(dy, dx)
		return [6]float64{cx, cy, half, smp.sizeX * scale / 2, r, smp.col.A}, ""
	}
	return [6]float64{head.X, head.Y / cosT, smp.sizeX * scale / 2, smp.sizeY * scale / 2, -smp.rot, smp.col.A}, ""
}

func keyTimes(s *system) []float64 {
	var out []float64
	if s.size != nil && s.size.Enabled {
		out = append(out, s.size.Curve.keyTimes()...)
		out = append(out, s.size.Y.keyTimes()...)
	}
	if s.color != nil && s.color.Enabled {
		out = append(out, s.color.Gradient.keyTimes()...)
	}
	if s.rotation != nil && s.rotation.Enabled {
		out = append(out, s.rotation.Curve.keyTimes()...)
	}
	return out
}

func denseAges(life float64) []float64 {
	n := int(math.Ceil(life/simStep*0.5)) + 1 // 1/120 s resolution
	n = max(n, 3)
	ages := make([]float64, n)
	for i := range ages {
		ages[i] = life * float64(i) / float64(n-1)
	}
	return ages
}

func nearestIndex(ages []float64, t float64) int {
	best, bd := 0, math.Inf(1)
	for i, a := range ages {
		if d := math.Abs(a - t); d < bd {
			best, bd = i, d
		}
	}
	return best
}

func sortedKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// refine splits segments whose linear interpolation deviates too much.
func refine(idx []int, coef []map[string][6]float64, o convertOptions) []int {
	tol := [6]float64{o.posTol, o.posTol, o.posTol, o.posTol, 0.06, 0.02}
	var out []int
	var rec func(a, b, depth int)
	rec = func(a, b, depth int) {
		if b-a < 2 || depth > 5 {
			out = append(out, a)
			return
		}
		worst, at := 0.0, -1
		for i := a + 1; i < b; i++ {
			t := float64(i-a) / float64(b-a)
			for name, cm := range coef[i] {
				ca, cb := coef[a][name], coef[b][name]
				for k := 0; k < 6; k++ {
					e := math.Abs(cm[k]-(ca[k]+(cb[k]-ca[k])*t)) / tol[k]
					if e > worst {
						worst, at = e, i
					}
				}
			}
		}
		if worst <= 1 || at < 0 {
			out = append(out, a)
			return
		}
		rec(a, at, depth+1)
		rec(at, b, depth+1)
	}
	for i := 0; i+1 < len(idx); i++ {
		rec(idx[i], idx[i+1], 0)
	}
	return append(out, idx[len(idx)-1])
}

func thin(idx []int, maxSeg int) []int {
	out := []int{idx[0]}
	for i := 1; i < maxSeg; i++ {
		out = append(out, idx[i*(len(idx)-1)/maxSeg])
	}
	return append(out, idx[len(idx)-1])
}

func lerpExpr(a, b expr, t float64) expr {
	out := expr{}
	for k, v := range a {
		out[k] += v * (1 - t)
	}
	for k, v := range b {
		out[k] += v * t
	}
	for k, v := range out {
		out[k] = round6(v)
		if out[k] == 0 {
			delete(out, k)
		}
	}
	return out
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// hexColor converts a linear-space particle colour to the display-space hex
// colour Sonolus multiplies with the sprite.
func hexColor(c Color) string {
	to := func(v float64) int {
		v = imgutil.LinearToSRGB(math.Min(math.Max(v, 0), 1))
		return int(math.Round(v * 255))
	}
	return fmt.Sprintf("#%02x%02x%02x", to(c.R), to(c.G), to(c.B))
}

// toSonolus converts a part to a ParticleDataEffect using sprite indices.
func (pt *part) toSonolus(spriteIndex func(spriteKey) int) resource.ParticleDataEffect {
	e := resource.ParticleDataEffect{Name: resource.ParticleEffectName(pt.name), Transform: identityParticleTransform()}
	for _, g := range pt.groups {
		dg := resource.ParticleDataGroup{Count: g.count}
		for _, p := range g.particles {
			prop := func(k int) resource.ParticleDataGroupParticleProperty {
				from := resource.ParticleDataGroupParticlePropertyExpression(p.from[k])
				to := resource.ParticleDataGroupParticlePropertyExpression(p.to[k])
				return resource.ParticleDataGroupParticleProperty{From: &from, To: &to, Ease: "linear"}
			}
			dg.Particles = append(dg.Particles, resource.ParticleDataGroupParticle{
				Sprite: spriteIndex(p.sprite), Color: p.color,
				Start: round6(p.start / pt.duration), Duration: round6((p.end - p.start) / pt.duration),
				X: prop(0), Y: prop(1), W: prop(2), H: prop(3), R: prop(4), A: prop(5),
			})
		}
		e.Groups = append(e.Groups, dg)
	}
	return e
}

func identityParticleTransform() resource.ParticleDataTransform {
	t := resource.ParticleDataTransform{}
	for _, k := range []string{"x1", "y1", "x2", "y2", "x3", "y3", "x4", "y4"} {
		t[k] = resource.ParticleDataTransformEntry{k: 1}
	}
	return t
}
