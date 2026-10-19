package particle

// Mesh-mode particles and 3D-rotated quads.
//
// Sonolus particles are rotated rectangles, so:
//   - Unity's built-in Quad mesh is drawn like a billboard (its rotation turns
//     counter-clockwise, a billboard's clockwise); a constant tilt about X
//     joins the lane-plane parts (exact, the engine squashes the quad);
//   - a quad with any other 3D rotation becomes the rectangle that keeps the
//     projected X edge and the projected area (shear is lost);
//   - other meshes are rasterised: the mesh, oriented, scaled and textured
//     like the original, is rendered into a sprite per quantised orientation
//     and UV-scroll offset, and the particle draws that sprite over the
//     projected bounds.

import (
	"fmt"
	"image"
	"math"
	"strings"
	"sync"

	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/unity"
)

type meshInfo struct {
	name     string      // meshQuad or a bundle mesh name
	geometry *unity.Mesh // nil for the built-in Quad
}

func (s *system) isQuadMesh() bool { return s.mesh != nil && s.mesh.name == meshQuad }
func (s *system) isRaster() bool   { return s.mesh != nil && s.mesh.name != meshQuad }

// curveZero reports whether a MinMaxCurve is 0 for every time and ratio.
func curveZero(m MinMaxCurve) bool {
	switch m.State {
	case 0:
		return m.Scalar == 0
	case 3:
		return m.Scalar == 0 && m.MinScalar == 0
	}
	if m.Scalar == 0 {
		return true
	}
	curves := []AnimationCurve{m.MaxCurve}
	if m.State == 2 {
		curves = append(curves, m.MinCurve)
	}
	for _, c := range curves {
		for _, k := range c.Keys {
			if k.Value != 0 || k.InSlope != 0 || k.OutSlope != 0 {
				return false
			}
		}
	}
	return true
}

// curveConst returns the value of a MinMaxCurve that does not vary.
func curveConst(m MinMaxCurve) (float64, bool) {
	switch m.State {
	case 0:
		return m.Scalar, true
	case 3:
		if m.Scalar == m.MinScalar {
			return m.Scalar, true
		}
	}
	if curveZero(m) {
		return 0, true
	}
	return 0, false
}

// spinXY reports whether particles turn about X or Y over their life.
func (s *system) spinXY() bool {
	if r := s.rotation; r != nil && r.Enabled && r.SeparateAxes && (!curveZero(r.X) || !curveZero(r.Y)) {
		return true
	}
	if b := s.bySpeed; b != nil && b.Enabled && b.SeparateAxes && (!curveZero(b.X) || !curveZero(b.Y)) {
		return true
	}
	return false
}

// startXY returns the constant start rotation about X and Y, or ok=false.
func (s *system) startXY() (rx, ry float64, ok bool) {
	if !s.initial.Rotation3D {
		return 0, 0, true
	}
	rx, okx := curveConst(s.initial.StartRotationX)
	ry, oky := curveConst(s.initial.StartRotationY)
	return rx, ry, okx && oky
}

// rotates3D reports whether particles have any rotation about X or Y.
func (s *system) rotates3D() bool {
	rx, ry, ok := s.startXY()
	return !ok || rx != 0 || ry != 0 || s.spinXY()
}

// meshTilt returns the constant tilt (radians about X) of a Quad mesh that
// only otherwise spins in its plane, or ok=false.
func (s *system) meshTilt() (float64, bool) {
	if !s.isQuadMesh() || s.spinXY() {
		return 0, false
	}
	rx, ry, ok := s.startXY()
	if !ok || ry != 0 {
		return 0, false
	}
	return rx, true
}

// basis returns the alignment axes of a quad or mesh (effect coordinates):
// view-aligned (0) or the transform chain's rotation (local, 2).
func (s *system) basis() [3]Vec3 {
	b := [3]Vec3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	if s.renderer.Alignment == 0 {
		return b
	}
	for i := range b {
		b[i] = s.def.vectorRotation(b[i])
	}
	return b
}

// vectorRotation maps a direction through the transform chain's rotations.
func (d *SystemDef) vectorRotation(v Vec3) Vec3 {
	v = d.Transform.Rotation.rotate(v)
	for _, p := range d.ParentTransforms {
		v = p.Rotation.rotate(v)
	}
	return v
}

func applyBasis(b [3]Vec3, v Vec3) Vec3 { return b[0].mul(v.X).add(b[1].mul(v.Y)).add(b[2].mul(v.Z)) }

// quadAxes returns the half-extent axes of a 3D-rotated quad in effect
// coordinates.
func (s *system) quadAxes(smp sample, scale float64) (Vec3, Vec3) {
	hx, hy := smp.sizeX*scale/2, smp.sizeY*scale/2
	b := s.basis()
	if s.isQuadMesh() {
		q := eulerZXYRad(Vec3{smp.rotX, smp.rotY, smp.rot})
		return applyBasis(b, q.rotate(Vec3{hx, 0, 0})), applyBasis(b, q.rotate(Vec3{0, hy, 0}))
	}
	// billboard: the native 3D billboard rotation (opposite orientation)
	cz, sz := math.Cos(smp.rot), -math.Sin(smp.rot)
	cx, sx := math.Cos(smp.rotX), -math.Sin(smp.rotX)
	cy, sy := math.Cos(smp.rotY), -math.Sin(smp.rotY)
	xcol := Vec3{cz*cy + sz*sx*sy, sz * cx, sz*cy*sx - cz*sy}
	ycol := Vec3{sy*cz*sx - sz*cy, cz * cx, sx*cz*cy + sz*sy}
	return applyBasis(b, xcol).mul(hx), applyBasis(b, ycol).mul(hy)
}

// rectOf turns projected quad axes into a Sonolus rectangle: the X edge
// kept, the height chosen to keep the projected (signed) area.
func rectOf(u, v Vec3) (w, h, r float64) {
	w = math.Hypot(u.X, u.Y)
	if w < 1e-12 {
		return 0, 0, 0
	}
	return w, (u.X*v.Y - u.Y*v.X) / w, math.Atan2(u.Y, u.X)
}

func eulerZXYRad(r Vec3) Quat { return eulerZXY(r.mul(1 / degToRad)) }

// --- rasterised meshes ------------------------------------------------------

// Quantisation of rasterised sprites: orientation, UV scroll and size ratio.
const (
	rasterAngleStep = 5 * degToRad
	rasterUVStep    = 1.0 / 64
	rasterSizeStep  = 0.02
	rasterPixels    = 192 // longest sprite side
	rasterSuper     = 3   // supersampling per axis
)

// rasterView is one quantised rendering of a mesh particle.
type rasterView struct {
	bundle   string
	mesh     *unity.Mesh
	texture  string
	frame    int
	tilesX   int
	tilesY   int
	scaleUV  [2]float64
	offsetUV [2]float64
	scroll   [2]float64 // custom-data UV offset (u, v)
	rot      Vec3       // quantised radians
	ratio    [2]float64 // size y/x, z/x
	pivot    Vec3
	basis    [3]Vec3
	additive bool
}

func (r rasterView) key() string {
	return fmt.Sprintf("raster:%s:%s:%s:%d/%dx%d:%.4f,%.4f:%.4f,%.4f:%.4f,%.4f:%.4f,%.4f,%.4f:%.2f,%.2f:%.4f,%.4f,%.4f:%v:%v",
		r.bundle, r.mesh.Name, r.texture, r.frame, r.tilesX, r.tilesY, r.scaleUV[0], r.scaleUV[1], r.offsetUV[0], r.offsetUV[1],
		r.scroll[0], r.scroll[1], r.rot.X, r.rot.Y, r.rot.Z, r.ratio[0], r.ratio[1], r.pivot.X, r.pivot.Y, r.pivot.Z, r.basis, r.additive)
}

// rasterRegistry maps raster sprite names to their views (shared by all
// conversions; the texture store renders them on demand).
type rasterRegistry struct {
	mu    sync.Mutex
	views map[string]rasterView
	geom  map[string][4]float64 // key → bounds (x0, y0, x1, y1) in size units
}

var rasters = &rasterRegistry{views: map[string]rasterView{}, geom: map[string][4]float64{}}

func quantise(v, step float64) float64 { return math.Round(v/step) * step }

// rasterFor registers the quantised view of a mesh particle sample and
// returns its key and projected bounds in units of the particle size x.
func (s *system) rasterFor(bundle string, smp sample) (string, [4]float64) {
	ratio := [2]float64{1, 1}
	if smp.sizeX > 1e-9 {
		ratio = [2]float64{quantise(smp.sizeY/smp.sizeX, rasterSizeStep), quantise(smp.sizeZ/smp.sizeX, rasterSizeStep)}
	}
	v := rasterView{
		bundle: bundle, mesh: s.mesh.geometry, texture: s.material.Texture, frame: smp.frame, tilesX: 1, tilesY: 1,
		scaleUV:  [2]float64{s.material.TextureScale.X, s.material.TextureScale.Y},
		offsetUV: [2]float64{s.material.TextureOffset.X, s.material.TextureOffset.Y},
		rot:      Vec3{quantise(smp.rotX, rasterAngleStep), quantise(smp.rotY, rasterAngleStep), quantise(smp.rot, rasterAngleStep)},
		ratio:    ratio,
		pivot:    s.renderer.Pivot,
		basis:    s.basis(),
		additive: s.material.SourceBlend == 5 && s.material.DestinationBlend == 1,
	}
	if s.uv != nil && s.uv.Enabled {
		v.tilesX, v.tilesY = s.uv.TilesX, s.uv.TilesY
	}
	if s.material.UVScroll {
		v.scroll = [2]float64{quantise(smp.custom[1], rasterUVStep), quantise(smp.custom[0], rasterUVStep)}
		v.scroll[0] -= math.Floor(v.scroll[0])
		v.scroll[1] -= math.Floor(v.scroll[1])
	}
	key := v.key()
	rasters.mu.Lock()
	defer rasters.mu.Unlock()
	if g, ok := rasters.geom[key]; ok {
		return key, g
	}
	pts := v.project()
	g := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range pts {
		g[0], g[1] = math.Min(g[0], p.X), math.Min(g[1], p.Y)
		g[2], g[3] = math.Max(g[2], p.X), math.Max(g[3], p.Y)
	}
	rasters.views[key] = v
	rasters.geom[key] = g
	return key, g
}

// project returns the mesh vertices in effect coordinates for a particle of
// size x 1 at the origin (pivot included).
func (v rasterView) project() []Vec3 {
	q := eulerZXYRad(v.rot)
	size := Vec3{1, v.ratio[0], v.ratio[1]}
	pivot := applyBasis(v.basis, q.rotate(v.pivot.hadamard(size)))
	out := make([]Vec3, len(v.mesh.Positions))
	for i, p := range v.mesh.Positions {
		out[i] = applyBasis(v.basis, q.rotate(Vec3{p[0], p[1], p[2]}.hadamard(size))).add(pivot)
	}
	return out
}

// render rasterises the view into a sprite covering its bounds (top-down,
// straight alpha; additive views store the summed light with alpha 1).
func (v rasterView) render(tex *image.NRGBA, bounds [4]float64) *image.NRGBA {
	bw, bh := bounds[2]-bounds[0], bounds[3]-bounds[1]
	ppu := float64(rasterPixels) / math.Max(math.Max(bw, bh), 1e-6)
	W, H := max(1, int(math.Ceil(bw*ppu))), max(1, int(math.Ceil(bh*ppu)))
	SW, SH := W*rasterSuper, H*rasterSuper
	sppu := ppu * rasterSuper
	// premultiplied accumulation
	acc := make([][4]float64, SW*SH)
	pts := v.project()
	tw, th := tex.Rect.Dx(), tex.Rect.Dy()
	sampleTex := func(u, w float64) [4]float64 {
		// texture-sheet tile, material scale/offset, UV scroll; repeat wrap
		col, rowTop := v.frame%v.tilesX, v.frame/v.tilesX
		u = (float64(col)+u)/float64(v.tilesX)*v.scaleUV[0] + v.offsetUV[0] + v.scroll[0]
		w = (float64(v.tilesY-1-rowTop)+w)/float64(v.tilesY)*v.scaleUV[1] + v.offsetUV[1] + v.scroll[1]
		u -= math.Floor(u)
		w -= math.Floor(w)
		x := u*float64(tw) - 0.5
		y := (1-w)*float64(th) - 0.5
		x0, y0 := int(math.Floor(x)), int(math.Floor(y))
		fx, fy := x-float64(x0), y-float64(y0)
		var c [4]float64
		for _, d := range [4][3]float64{{0, 0, (1 - fx) * (1 - fy)}, {1, 0, fx * (1 - fy)}, {0, 1, (1 - fx) * fy}, {1, 1, fx * fy}} {
			px, py := (x0+int(d[0])+tw)%tw, (y0+int(d[1])+th)%th
			o := tex.PixOffset(px, py)
			for k := 0; k < 4; k++ {
				c[k] += float64(tex.Pix[o+k]) / 255 * d[2]
			}
		}
		return c
	}
	toPx := func(p Vec3) (float64, float64) { return (p.X - bounds[0]) * sppu, (bounds[3] - p.Y) * sppu }
	for _, tri := range v.mesh.Triangles {
		var xs, ys [3]float64
		for k, i := range tri {
			xs[k], ys[k] = toPx(pts[i])
		}
		area := (xs[1]-xs[0])*(ys[2]-ys[0]) - (xs[2]-xs[0])*(ys[1]-ys[0])
		if math.Abs(area) < 1e-12 {
			continue
		}
		minX, maxX := max(0, int(math.Floor(min(xs[0], xs[1], xs[2])))), min(SW-1, int(math.Ceil(max(xs[0], xs[1], xs[2]))))
		minY, maxY := max(0, int(math.Floor(min(ys[0], ys[1], ys[2])))), min(SH-1, int(math.Ceil(max(ys[0], ys[1], ys[2]))))
		for py := minY; py <= maxY; py++ {
			for px := minX; px <= maxX; px++ {
				cx, cy := float64(px)+0.5, float64(py)+0.5
				w0 := ((xs[1]-cx)*(ys[2]-cy) - (xs[2]-cx)*(ys[1]-cy)) / area
				w1 := ((xs[2]-cx)*(ys[0]-cy) - (xs[0]-cx)*(ys[2]-cy)) / area
				w2 := 1 - w0 - w1
				if w0 < 0 || w1 < 0 || w2 < 0 {
					continue
				}
				a, b, c := tri[0], tri[1], tri[2]
				u := w0*v.mesh.UV[a][0] + w1*v.mesh.UV[b][0] + w2*v.mesh.UV[c][0]
				t := w0*v.mesh.UV[a][1] + w1*v.mesh.UV[b][1] + w2*v.mesh.UV[c][1]
				col := sampleTex(u, t)
				if v.mesh.Colors != nil {
					for k := 0; k < 4; k++ {
						col[k] *= w0*v.mesh.Colors[a][k] + w1*v.mesh.Colors[b][k] + w2*v.mesh.Colors[c][k]
					}
				}
				d := &acc[py*SW+px]
				if v.additive {
					for k := 0; k < 3; k++ {
						d[k] += col[k] * col[3]
					}
					d[3] = 1
				} else {
					for k := 0; k < 3; k++ {
						d[k] = col[k]*col[3] + d[k]*(1-col[3])
					}
					d[3] = col[3] + d[3]*(1-col[3])
				}
			}
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, W, H))
	n := float64(rasterSuper * rasterSuper)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			var sum [4]float64
			for sy := 0; sy < rasterSuper; sy++ {
				for sx := 0; sx < rasterSuper; sx++ {
					p := acc[(y*rasterSuper+sy)*SW+x*rasterSuper+sx]
					for k := 0; k < 4; k++ {
						sum[k] += p[k]
					}
				}
			}
			o := img.PixOffset(x, y)
			if v.additive {
				for k := 0; k < 3; k++ {
					img.Pix[o+k] = uint8(math.Round(math.Min(sum[k]/n, 1) * 255))
				}
				img.Pix[o+3] = uint8(math.Round(sum[3] / n * 255))
				continue
			}
			a := sum[3] / n
			img.Pix[o+3] = uint8(math.Round(a * 255))
			if a > 0 {
				for k := 0; k < 3; k++ {
					img.Pix[o+k] = uint8(math.Round(math.Min(sum[k]/n/a, 1) * 255))
				}
			}
		}
	}
	return img
}

// rasterSource renders registered raster sprites of a bundle with the
// bundle's textures.
func rasterSource(store *textureStore, bundle string) textureSource {
	return func(name string) (*image.NRGBA, error) {
		rasters.mu.Lock()
		v, ok := rasters.views[name]
		g := rasters.geom[name]
		rasters.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("no raster view %s", name)
		}
		_, tex, err := store.get(bundle, v.texture)
		if err != nil {
			return nil, err
		}
		return v.render(tex, g), nil
	}
}

func isRasterName(name string) bool { return strings.HasPrefix(name, "raster:") }
