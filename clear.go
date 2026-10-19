package particle

// Clear / Full Combo / All Perfect fireworks. The game-clear prefabs are built
// into the APK; assets/apk/gameclear holds them as raw Unity component trees
// together with the Animator clips that switch the firework roots on; this
// file adapts them to the Bundle shape the tap-effect converter consumes.
//
// Every firework ParticleSystem uses scaling mode Local: particles are scaled
// by the system's own Transform scale (0.185) and not by its parents, so one
// particle unit is 0.185 UI-camera world units (= Sonolus units) whatever the
// UI scale. Emitter positions do go through the whole hierarchy and are
// authored in UI units. Each output effect therefore holds the systems of one
// firework root that share an emitter position and an activation time; the
// engine spawns it at that UI position and time.

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type gameClearProfile struct {
	Base       gameClearBranch `json:"base"`
	FullCombo  gameClearBranch `json:"fullCombo"`
	AllPerfect gameClearBranch `json:"allPerfect"`
	Assets     []struct {
		LogicalKey string `json:"logical_key"`
		File       string `json:"file"`
	} `json:"assets"`
}

type gameClearBranch struct {
	Graph struct {
		Objects []gameClearObject `json:"objects"`
	} `json:"graph"`
	Clip gameClearClip `json:"clip"`
}

type gameClearObject struct {
	Path       string     `json:"path"`
	Active     bool       `json:"active"`
	Position   [3]float64 `json:"local_position"`
	Rotation   [4]float64 `json:"local_rotation"`
	Scale      [3]float64 `json:"local_scale"`
	Components []struct {
		Class string          `json:"class"`
		Tree  json.RawMessage `json:"serializedTree"`
		Asset *string         `json:"asset"`
	} `json:"components"`
}

func (o *gameClearObject) component(class string) (json.RawMessage, *string) {
	for _, c := range o.Components {
		if c.Class == class {
			return c.Tree, c.Asset
		}
	}
	return nil, nil
}

// gameClearClip is a streamed Unity AnimationClip: each key starts a cubic
// segment of one curve; curves past the streamed ones are constants.
type gameClearClip struct {
	StopTime       float64 `json:"stop_time"`
	StreamedCurves int     `json:"streamed_curve_count"`
	Bindings       []struct {
		Channels []string `json:"channels"`
	} `json:"bindings"`
	Frames []struct {
		Time float64 `json:"time"`
		Keys []struct {
			Index        int        `json:"index"`
			Coefficients [4]float64 `json:"coefficients"`
		} `json:"keys"`
	} `json:"streamed_frames"`
	Constants []float64 `json:"constants"`
}

func (c *gameClearClip) channels() []string {
	var out []string
	for _, b := range c.Bindings {
		out = append(out, b.Channels...)
	}
	return out
}

// value evaluates curve i at clip time t (the last key at or before t).
func (c *gameClearClip) value(i int, t float64) float64 {
	if i >= c.StreamedCurves {
		if k := i - c.StreamedCurves; k < len(c.Constants) {
			return c.Constants[k]
		}
		return 0
	}
	found, kt, co := false, 0.0, [4]float64{}
	for _, f := range c.Frames {
		if f.Time > t {
			break
		}
		for _, k := range f.Keys {
			if k.Index == i {
				found, kt, co = true, f.Time, k.Coefficients
			}
		}
	}
	if !found {
		return 0
	}
	d := t - kt
	return ((co[0]*d+co[1])*d+co[2])*d + co[3]
}

// clearBranches lists the three game-clear branches: the container that names
// their effects and when their clip starts (seconds after the clear). The base
// clip MusicGameClear is started by a coroutine after WaitForSeconds(0.2); the
// Full Combo / All Perfect text-in clips start at the clear.
var clearBranches = []struct {
	key       string
	container string
	clipStart float64
}{
	{"base", "GameClearParticle", 0.2},
	{"fullCombo", "FullComboAnimation", 0},
	{"allPerfect", "AllPerfectAnimation", 0},
}

// clearUnit is the Sonolus size of one particle unit of a firework system
// (its own Transform scale under scaling mode Local).
const clearUnit = 0.185

// clearEffect is one output effect of the fireworks.
type clearEffect struct {
	name    string
	branch  string
	anchor  [2]float64 // emitter position, UI units from the screen centre, y up
	start   float64    // seconds after the clear
	clipAt  float64    // activation time inside the branch clip
	systems []string   // original object paths
}

// Modules the converter evaluates; any other enabled module is an error.
var clearModules = map[string]bool{
	"InitialModule": true, "EmissionModule": true, "ShapeModule": true, "ColorModule": true,
	"SizeModule": true, "RotationModule": true, "ClampVelocityModule": true, "UVModule": true,
}

type clearOverride struct {
	pos           [3]*float64
	startRotation *float64
	looping       *bool
}

// loadClear builds one Bundle holding every firework system that the clips
// activate. System paths are "<effect name>/<system object>", so the regular
// prefab conversion picks up one effect at a time.
func loadClear(raw []byte) (*Bundle, []clearEffect, map[string]string, error) {
	var prof gameClearProfile
	if err := json.Unmarshal(raw, &prof); err != nil {
		return nil, nil, nil, fmt.Errorf("game-clear profile: %w", err)
	}
	b := &Bundle{
		Key:       "gameclear",
		Profiles:  map[string]SystemProfile{},
		Renderers: map[string]RendererProfile{},
		Modules: ModuleProfiles{
			Initial: map[string]InitialModule{}, Emission: map[string]EmissionModule{}, Shape: map[string]ShapeModule{},
			Color: map[string]ColorModule{}, Size: map[string]SizeModule{}, Rotation: map[string]RotationModule{},
			Clamp: map[string]ClampVelocityModule{}, UV: map[string]UVModule{},
		},
	}
	files := map[string]string{}
	for _, a := range prof.Assets {
		files[a.LogicalKey] = a.File
	}
	var effects []clearEffect
	for _, br := range clearBranches {
		data := map[string]*gameClearBranch{"base": &prof.Base, "fullCombo": &prof.FullCombo, "allPerfect": &prof.AllPerfect}[br.key]
		es, err := addClearBranch(b, data, br.key, br.container, br.clipStart)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("game-clear %s: %w", br.key, err)
		}
		effects = append(effects, es...)
	}
	return b, effects, files, nil
}

func addClearBranch(b *Bundle, br *gameClearBranch, key, container string, clipStart float64) ([]clearEffect, error) {
	objects := br.Graph.Objects
	byPath := map[string]*gameClearObject{}
	root := ""
	for i := range objects {
		o := &objects[i]
		byPath[o.Path] = o
		if root == "" || strings.Count(o.Path, "/") < strings.Count(root, "/") {
			root = o.Path
		}
	}
	channels := br.Clip.channels()
	full := func(rel string) string {
		if rel == "" {
			return root
		}
		return root + "/" + rel
	}
	parentOf := func(path string) string {
		if i := strings.LastIndex(path, "/"); i >= 0 {
			return path[:i]
		}
		return ""
	}

	// Activation: the first clip key time at which the object and all its
	// ancestors in the graph are active (serialised flag or m_IsActive curve).
	phases := []float64{0}
	for _, f := range br.Clip.Frames {
		phases = append(phases, f.Time)
	}
	sort.Float64s(phases)
	activeAt := func(t float64) map[string]bool {
		own := map[string]bool{}
		for _, o := range objects {
			own[o.Path] = o.Active
		}
		for i, ch := range channels {
			if rel, ok := strings.CutSuffix(ch, ".m_IsActive.value"); ok {
				own[full(rel)] = br.Clip.value(i, math.Min(t, br.Clip.StopTime)) >= 0.5
			}
		}
		eff := map[string]bool{}
		var resolve func(string) bool
		resolve = func(p string) bool {
			if v, ok := eff[p]; ok {
				return v
			}
			v, known := own[p]
			if !known {
				v = true
			}
			if parent := parentOf(p); v && parent != "" {
				if _, inGraph := own[parent]; inGraph {
					v = resolve(parent)
				}
			}
			eff[p] = v
			return v
		}
		for _, o := range objects {
			resolve(o.Path)
		}
		return eff
	}
	activation := map[string]float64{}
	for _, t := range phases {
		act := activeAt(t)
		for _, o := range objects {
			if _, done := activation[o.Path]; !done && act[o.Path] {
				activation[o.Path] = t
			}
		}
	}

	// Animated Transform positions and ParticleSystem fields at clip time t.
	overrides := func(t float64) map[string]*clearOverride {
		out := map[string]*clearOverride{}
		t = math.Min(t, br.Clip.StopTime-1.0/6000)
		for i, ch := range channels {
			get := func(rel string) *clearOverride {
				p := full(rel)
				if out[p] == nil {
					out[p] = &clearOverride{}
				}
				return out[p]
			}
			v := br.Clip.value(i, t)
			if rel, axis, ok := strings.Cut(ch, ".m_LocalPosition."); ok {
				if k := strings.Index("xyz", axis); len(axis) == 1 && k >= 0 {
					get(rel).pos[k] = &v
				}
			} else if rel, ok := strings.CutSuffix(ch, ".attribute_hash:1133446416.value"); ok {
				get(rel).startRotation = &v // InitialModule.startRotation
			} else if rel, ok := strings.CutSuffix(ch, ".attribute_hash:925582877.value"); ok {
				l := v >= 0.5 // looping
				get(rel).looping = &l
			}
		}
		return out
	}

	type group struct {
		effect  *clearEffect
		root    string
		anchor  [2]float64
		systems []*gameClearObject
	}
	var groups []*group
	for i := range objects {
		o := &objects[i]
		tree, _ := o.component("ParticleSystem")
		rtree, asset := o.component("ParticleSystemRenderer")
		at, active := activation[o.Path]
		if tree == nil || rtree == nil || asset == nil || !active {
			continue
		}
		// The firework root: the outermost effect_par_* ancestor (or self).
		fw := ""
		for p := o.Path; p != ""; p = parentOf(p) {
			if strings.HasPrefix(p[strings.LastIndex(p, "/")+1:], "effect_par") {
				fw = p
			}
		}
		if fw == "" {
			return nil, fmt.Errorf("%s: no effect_par root", o.Path)
		}
		ov := overrides(at)
		// Emitter position: own position through every graph ancestor (UI units).
		pos := func(x *gameClearObject) Vec3 {
			v := x.Position
			if a := ov[x.Path]; a != nil {
				for k, p := range a.pos {
					if p != nil {
						v[k] = *p
					}
				}
			}
			return Vec3{v[0], v[1], v[2]}
		}
		quat := func(x *gameClearObject) Quat {
			return Quat{x.Rotation[0], x.Rotation[1], x.Rotation[2], x.Rotation[3]}
		}
		p, q := pos(o), quat(o)
		for a := parentOf(o.Path); a != ""; a = parentOf(a) {
			anc := byPath[a]
			if anc == nil {
				break
			}
			sc := Vec3{anc.Scale[0], anc.Scale[1], anc.Scale[2]}
			p = quat(anc).rotate(p.hadamard(sc)).add(pos(anc))
			q = quat(anc).mul(q)
		}
		anchor := [2]float64{round6(p.X), round6(p.Y)}

		// Group by root, emitter position (to 0.25 UI units) and start.
		var g *group
		for _, c := range groups {
			if c.root == fw && c.effect.clipAt == at &&
				math.Abs(c.anchor[0]-anchor[0]) < 0.25 && math.Abs(c.anchor[1]-anchor[1]) < 0.25 {
				g = c
			}
		}
		if g == nil {
			g = &group{root: fw, anchor: anchor, effect: &clearEffect{branch: key, anchor: anchor, clipAt: at, start: round6(clipStart + at)}}
			groups = append(groups, g)
		}
		g.systems = append(g.systems, o)
		g.effect.systems = append(g.effect.systems, o.Path)

		if err := addClearSystem(b, o, tree, rtree, *asset, q, ov[o.Path]); err != nil {
			return nil, err
		}
	}

	// Name the effects after their roots; a root split into several groups
	// adds the first system of each group.
	perRoot := map[string]int{}
	for _, g := range groups {
		perRoot[g.root]++
	}
	var out []clearEffect
	for _, g := range groups {
		name := container + "/" + g.root[strings.LastIndex(g.root, "/")+1:]
		if perRoot[g.root] > 1 {
			name += "#" + g.systems[0].Path[strings.LastIndex(g.systems[0].Path, "/")+1:]
		}
		g.effect.name = name
		for _, o := range g.systems {
			path := name + "/" + o.Path[strings.LastIndex(o.Path, "/")+1:]
			for i := range b.Systems {
				if b.Systems[i].Identity == o.Path {
					b.Systems[i].Path = path
				}
			}
		}
		out = append(out, *g.effect)
	}
	return out, nil
}

// addClearSystem adds one serialised ParticleSystem (with renderer) to the
// bundle. The Transform keeps only the accumulated rotation: the position is
// the effect anchor and the scale is the engine's quad (clearUnit).
func addClearSystem(b *Bundle, o *gameClearObject, tree, rtree json.RawMessage, asset string, rot Quat, ov *clearOverride) error {
	id := o.Path
	var sp SystemProfile
	if err := json.Unmarshal(tree, &sp.System); err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}
	if ov != nil && ov.looping != nil {
		sp.System.Looping = *ov.looping
	}
	if sp.System.ScalingMode != 1 {
		return fmt.Errorf("%s: scaling mode %d (only Local is converted)", id, sp.System.ScalingMode)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(tree, &fields); err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}
	sp.Modules = map[string]string{}
	for name, m := range fields {
		if !strings.HasSuffix(name, "Module") {
			continue
		}
		var en struct {
			Enabled bool `json:"enabled"`
		}
		_ = json.Unmarshal(m, &en)
		if !en.Enabled {
			continue
		}
		if !clearModules[name] {
			return fmt.Errorf("%s: enabled %s is not converted", id, name)
		}
		mid := id + ":" + name
		sp.Modules[name] = mid
		var err error
		switch name {
		case "InitialModule":
			var v InitialModule
			err = json.Unmarshal(m, &v)
			if ov != nil && ov.startRotation != nil {
				v.StartRotation.Scalar, v.StartRotation.MinScalar = *ov.startRotation, *ov.startRotation
			}
			b.Modules.Initial[mid] = v
		case "EmissionModule":
			var v EmissionModule
			err = json.Unmarshal(m, &v)
			b.Modules.Emission[mid] = v
		case "ShapeModule":
			var v ShapeModule
			err = json.Unmarshal(m, &v)
			b.Modules.Shape[mid] = v
		case "ColorModule":
			var v ColorModule
			err = json.Unmarshal(m, &v)
			b.Modules.Color[mid] = v
		case "SizeModule":
			var v SizeModule
			err = json.Unmarshal(m, &v)
			b.Modules.Size[mid] = v
		case "RotationModule":
			var v RotationModule
			err = json.Unmarshal(m, &v)
			b.Modules.Rotation[mid] = v
		case "ClampVelocityModule":
			var v ClampVelocityModule
			err = json.Unmarshal(m, &v)
			b.Modules.Clamp[mid] = v
		case "UVModule":
			var v UVModule
			err = json.Unmarshal(m, &v)
			b.Modules.UV[mid] = v
		}
		if err != nil {
			return fmt.Errorf("%s %s: %w", id, name, err)
		}
	}
	var r RendererProfile
	if err := json.Unmarshal(rtree, &r); err != nil {
		return fmt.Errorf("%s renderer: %w", id, err)
	}
	// The serialised material slot is a file reference; the asset names it.
	r.Materials = nil
	slot, _ := json.Marshal([]map[string]string{{"name": asset}})
	_ = json.Unmarshal(slot, &r.Materials)
	sp.Renderer = id
	b.Profiles[id] = sp
	b.Renderers[id] = r
	if b.material(asset) == nil {
		// Every game-clear material is Mobile/Particles/Additive on one texture.
		m := Material{Name: asset, Shader: "Mobile/Particles/Additive", Texture: asset, SourceBlend: 5, DestinationBlend: 1}
		m.TextureScale.X, m.TextureScale.Y = 1, 1
		b.Materials = append(b.Materials, m)
	}
	b.Systems = append(b.Systems, SystemDef{
		Identity:  id,
		Path:      id, // replaced by "<effect>/<system>" once the effect is named
		Transform: TransformProfile{Rotation: rot, Scale: Vec3{1, 1, 1}},
		Profile:   id,
	})
	return nil
}
