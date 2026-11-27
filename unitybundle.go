package particle

// Tap-effect particle bundles read directly from the original asset bundles
// (official CDN): the prefabs' GameObject / Transform hierarchy and every
// ParticleSystem, ParticleSystemRenderer, Material and Texture2D, through the
// bundles' type trees, into the converter's Bundle model. Materials a bundle
// references in the bundles it depends on (AssetBundle.m_Dependencies, e.g.
// the mainland directionalflickskin10001light → directionalflickskin00light)
// are read from those bundles.

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/unity"
)

// Unity class IDs.
const (
	classGameObject  = 1
	classTransform   = 4
	classMaterial    = 21
	classTexture2D   = 28
	classMesh        = 43
	classShader      = 48
	classParticle    = 198
	classRenderer    = 199
	classAssetBundle = 142
)

// builtinShaders names shaders referenced in Unity's built-in resource files
// by path ID (the bundles only reference them).
var builtinShaders = map[string]string{
	// The mainland Default-ParticleSystem material (path ID
	// 8308402369294867413 in directionalflickskin00light and
	// directionalflickskin10001normal/light) references it; GarupaEditor's
	// mainland skin catalog resolves that material's shader as this one.
	"Resources/unity_builtin_extra#211":   "Particles/Standard Unlit",
	"Resources/unity_builtin_extra#10720": "Mobile/Particles/Additive",
	// The tap-effect materials named "…alpha" use it where their "…add"
	// twins on the same texture use 10720.
	"Resources/unity_builtin_extra#10721": "Mobile/Particles/Alpha Blended",
}

// builtinQuad is Unity's built-in Quad mesh (1×1 in the XY plane, facing −Z),
// the mesh of most mesh-mode particles.
const builtinQuad = "Library/unity default resources#10210"

// meshQuad marks a renderer that draws the built-in Quad.
const meshQuad = "#quad"

// uvScrollShader adds the first custom-data vector (y, x) to the UVs; its
// blend comes from the material's _BlendSrc / _BlendDst.
const uvScrollShader = "star/Customdata_CurveUVScroll"

// shaderBlend is the fixed blend of a particle shader (source, destination
// factor: 1 One, 5 SrcAlpha, 10 OneMinusSrcAlpha). Shaders with a
// material-controlled blend (_SrcBlend / _DstBlend) are not listed.
var shaderBlend = map[string][2]int{
	"Mobile/Particles/Additive":                          {5, 1},
	"Legacy Shaders/Particles/Additive":                  {5, 1},
	"Legacy Shaders/Particles/Alpha Blended":             {5, 10},
	"Legacy Shaders/Particles/Alpha Blended Premultiply": {1, 10},
	"Mobile/Particles/Alpha Blended":                     {5, 10},
}

// modules the converter reads; a module is part of a system only when enabled.
var unityModules = []string{"InitialModule", "EmissionModule", "ShapeModule", "ColorModule", "SizeModule",
	"RotationModule", "RotationBySpeedModule", "ClampVelocityModule", "UVModule", "VelocityModule", "ForceModule", "CustomDataModule"}

type pptr struct {
	file int
	id   int64
}

func toPPtr(v any) pptr {
	m, _ := v.(map[string]any)
	f, _ := m["m_FileID"].(float64)
	id, _ := m["m_PathID"].(int64)
	return pptr{int(f), id}
}

func num(m map[string]any, k string) float64 {
	switch v := m[k].(type) {
	case float64:
		return v
	case bool:
		if v {
			return 1
		}
	}
	return 0
}
func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }

// jsonSafe replaces infinite numbers by the "number:±infinity" strings the
// profile model decodes (curve tangents).
func jsonSafe(v any) any {
	switch x := v.(type) {
	case float64:
		switch {
		case math.IsInf(x, 1):
			return "number:+infinity"
		case math.IsInf(x, -1):
			return "number:-infinity"
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = jsonSafe(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsonSafe(e)
		}
		return out
	}
	return v
}

// decodeInto converts a deserialised object (or part of one) into a profile
// type through JSON.
func decodeInto(v any, out any) error {
	b, err := json.Marshal(jsonSafe(v))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func transformProfile(t map[string]any) (TransformProfile, error) {
	var p TransformProfile
	err := decodeInto(map[string]any{"m_LocalPosition": t["m_LocalPosition"], "m_LocalRotation": t["m_LocalRotation"],
		"m_LocalScale": t["m_LocalScale"]}, &p)
	return p, err
}

// textureSource decodes a texture of a bundle by its key (Material.Texture).
type textureSource func(name string) (*image.NRGBA, error)

// Dependency is an asset bundle that a bundle depends on.
type Dependency struct {
	Name string // asset bundle name, e.g. "ingameskin/tapeffect/directionalflickskin00light"
	Data []byte // UnityFS bytes
}

// serialized is the serialized file of an asset bundle with the bundle's
// other files (resource streams).
type serialized struct {
	cab    string // serialized file name, e.g. "CAB-41ce…"
	bundle string // asset bundle name ("" for the bundle being read)
	f      *unity.File
	files  map[string][]byte
}

func parseBundle(data []byte) (*serialized, error) {
	files, err := unity.ReadBundle(data)
	if err != nil {
		return nil, err
	}
	var s *serialized
	for name, sf := range files {
		if strings.HasSuffix(name, ".resS") || strings.HasSuffix(name, ".resource") {
			continue
		}
		if s != nil {
			return nil, fmt.Errorf("bundle has more than one serialized file (%s, %s)", s.cab, name)
		}
		f, err := unity.Parse(sf)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		s = &serialized{cab: name, f: f, files: files}
	}
	if s == nil {
		return nil, fmt.Errorf("bundle has no serialized file")
	}
	return s, nil
}

// BundleDependencies returns the asset bundles a bundle depends on
// (AssetBundle.m_Dependencies).
func BundleDependencies(data []byte) ([]string, error) {
	s, err := parseBundle(data)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, id := range s.f.Order {
		if s.f.Objects[id].ClassID != classAssetBundle {
			continue
		}
		o, err := s.f.Read(id)
		if err != nil {
			return nil, err
		}
		deps, _ := o["m_Dependencies"].([]any)
		for _, d := range deps {
			if name, ok := d.(string); ok {
				out = append(out, name)
			}
		}
	}
	return out, nil
}

// bundleFromUnity builds the Bundle of one original tap-effect asset bundle
// (key prefixes the system identities) and returns the paths of systems whose
// GameObject is inactive in the prefab hierarchy (ParticleSystem.Play skips
// them).
func bundleFromUnity(data []byte, key string) (*Bundle, map[string]bool, error) {
	b, inactive, _, err := readTapBundle(data, key, nil)
	return b, inactive, err
}

// readTapBundle is bundleFromUnity plus a decoder for the bundle's textures
// (original pixels, from the resource streams of the bundle or of the
// dependency that holds them).
func readTapBundle(data []byte, key string, deps []Dependency) (*Bundle, map[string]bool, textureSource, error) {
	b, inactive, textures, err := buildBundle(data, key, deps)
	if err != nil {
		return nil, nil, nil, err
	}
	src := func(name string) (*image.NRGBA, error) {
		t, ok := textures[name]
		if !ok {
			return nil, fmt.Errorf("bundle %s has no texture %s", key, name)
		}
		return unity.DecodeTexture(t.s.f, t.s.files, t.id)
	}
	return b, inactive, src, nil
}

// textureRef is a texture object of a bundle or of one of its dependencies.
type textureRef struct {
	s  *serialized
	id int64
}

func buildBundle(data []byte, key string, deps []Dependency) (*Bundle, map[string]bool, map[string]textureRef, error) {
	main, err := parseBundle(data)
	if err != nil {
		return nil, nil, nil, err
	}
	f, files := main.f, main.files
	depFiles := map[string]*serialized{} // by serialized file name
	for _, d := range deps {
		s, err := parseBundle(d.Data)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("dependency %s: %w", d.Name, err)
		}
		s.bundle = d.Name
		depFiles[s.cab] = s
	}
	// external resolves file index n (> 0) of s to a dependency's serialized
	// file ("archive:/CAB-…/CAB-…"); nil for Unity's built-in resources.
	external := func(s *serialized, n int) *serialized {
		if n <= 0 || n > len(s.f.Externals) {
			return nil
		}
		path := s.f.Externals[n-1]
		if !strings.HasPrefix(path, "archive:/") {
			return nil
		}
		return depFiles[path[strings.LastIndex(path, "/")+1:]]
	}
	objects := map[int64]map[string]any{}
	byClass := map[int32][]int64{}
	for _, id := range f.Order {
		class := f.Objects[id].ClassID
		switch class {
		case classGameObject, classTransform, classMaterial, classTexture2D, classShader, classParticle, classRenderer, classMesh:
		default:
			continue
		}
		o, err := f.Read(id)
		if err != nil {
			return nil, nil, nil, err
		}
		objects[id] = o
		byClass[class] = append(byClass[class], id)
	}
	component := func(goID int64, class int32) (int64, map[string]any) {
		g := objects[goID]
		list, _ := g["m_Component"].([]any)
		for _, c := range list {
			cm, _ := c.(map[string]any)
			p := toPPtr(cm["component"])
			if p.file == 0 && f.Objects[p.id].ClassID == class {
				return p.id, objects[p.id]
			}
		}
		return 0, nil
	}
	goName := func(t map[string]any) string { return str(objects[toPPtr(t["m_GameObject"]).id], "m_Name") }
	// path of a transform: GameObject names from the prefab root, siblings
	// sharing a name told apart by "[index]" in child order
	paths := map[int64]string{}
	var pathOf func(tid int64) string
	pathOf = func(tid int64) string {
		if p, ok := paths[tid]; ok {
			return p
		}
		t := objects[tid]
		name := goName(t)
		father := toPPtr(t["m_Father"]).id
		p := name
		if father != 0 {
			siblings, _ := objects[father]["m_Children"].([]any)
			same, index := 0, 0
			for _, s := range siblings {
				sid := toPPtr(s).id
				if goName(objects[sid]) == name {
					if sid == tid {
						index = same
					}
					same++
				}
			}
			if same > 1 {
				name = fmt.Sprintf("%s[%d]", name, index)
			}
			p = pathOf(father) + "/" + name
		}
		paths[tid] = p
		return p
	}
	activeInHierarchy := func(tid int64) bool {
		for id := tid; id != 0; id = toPPtr(objects[id]["m_Father"]).id {
			if g := objects[toPPtr(objects[id]["m_GameObject"]).id]; g["m_IsActive"] != true {
				return false
			}
		}
		return true
	}

	b := &Bundle{Key: key, Profiles: map[string]SystemProfile{}, Renderers: map[string]RendererProfile{}}
	b.Modules = ModuleProfiles{Initial: map[string]InitialModule{}, Emission: map[string]EmissionModule{}, Shape: map[string]ShapeModule{},
		Color: map[string]ColorModule{}, Size: map[string]SizeModule{}, Rotation: map[string]RotationModule{},
		RotationSpeed: map[string]RotationBySpeed{}, Clamp: map[string]ClampVelocityModule{}, UV: map[string]UVModule{},
		Velocity: map[string]VelocityModule{}, Force: map[string]ForceModule{}, CustomData: map[string]CustomDataModule{}}
	b.Meshes = map[string]*unity.Mesh{}
	inactive := map[string]bool{}
	// Texture keys: the texture name, made unique by the path ID when the
	// name is not unique in its file, prefixed by the asset bundle name for a
	// dependency's texture.
	textures := map[string]textureRef{}
	textureKey := func(s *serialized, id int64) (string, error) {
		o, err := s.f.Read(id)
		if err != nil {
			return "", err
		}
		name := str(o, "m_Name")
		same := 0
		for _, other := range s.f.Order {
			if s.f.Objects[other].ClassID == classTexture2D {
				if t, err := s.f.Read(other); err == nil && str(t, "m_Name") == name {
					same++
				}
			}
		}
		k := name
		if same > 1 {
			k += "#" + strconv.FormatInt(id, 10)
		}
		if s.bundle != "" {
			k = s.bundle + ":" + k
		}
		textures[k] = textureRef{s, id}
		return k, nil
	}
	// Materials by object, named by m_Name (a dependency's prefixed by its
	// asset bundle name).
	type materialRef struct {
		s  *serialized
		id int64
	}
	var materialRefs []materialRef
	materialKeys := map[materialRef]string{}
	addMaterial := func(s *serialized, id int64) string {
		r := materialRef{s, id}
		if k, ok := materialKeys[r]; ok {
			return k
		}
		o, err := s.f.Read(id)
		if err != nil || s.f.Objects[id].ClassID != classMaterial {
			return ""
		}
		k := str(o, "m_Name")
		if s.bundle != "" {
			k = s.bundle + ":" + k
		}
		materialKeys[r] = k
		materialRefs = append(materialRefs, r)
		return k
	}
	for _, id := range byClass[classMaterial] {
		addMaterial(main, id)
	}
	var unresolved []string
	materialName := func(p pptr) string {
		switch {
		case p.id == 0:
			return ""
		case p.file == 0:
			return str(objects[p.id], "m_Name")
		}
		if d := external(main, p.file); d != nil {
			if k := addMaterial(d, p.id); k != "" {
				return k
			}
		}
		unresolved = append(unresolved, fmt.Sprintf("%v", p))
		return ""
	}
	for _, psID := range byClass[classParticle] {
		ps := objects[psID]
		goID := toPPtr(ps["m_GameObject"]).id
		tid, t := component(goID, classTransform)
		if t == nil {
			return nil, nil, nil, fmt.Errorf("particle system %d has no Transform", psID)
		}
		path := pathOf(tid)
		if !activeInHierarchy(tid) {
			inactive[path] = true
		}
		def := SystemDef{Identity: key + ":" + path, Path: path, Profile: path, ParentTransforms: []TransformProfile{}}
		if def.Transform, err = transformProfile(t); err != nil {
			return nil, nil, nil, err
		}
		for father := toPPtr(t["m_Father"]).id; father != 0; father = toPPtr(objects[father]["m_Father"]).id {
			pt, err := transformProfile(objects[father])
			if err != nil {
				return nil, nil, nil, err
			}
			def.ParentTransforms = append(def.ParentTransforms, pt)
		}
		var prof SystemProfile
		if err := decodeInto(ps, &prof.System); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		prof.Modules = map[string]string{}
		for _, name := range unityModules {
			m, _ := ps[name].(map[string]any)
			if m == nil || m["enabled"] != true {
				continue
			}
			prof.Modules[name] = path
			var err error
			switch name {
			case "InitialModule":
				var v InitialModule
				err = decodeInto(m, &v)
				b.Modules.Initial[path] = v
			case "EmissionModule":
				var v EmissionModule
				err = decodeInto(m, &v)
				b.Modules.Emission[path] = v
			case "ShapeModule":
				var v ShapeModule
				err = decodeInto(m, &v)
				b.Modules.Shape[path] = v
			case "ColorModule":
				var v ColorModule
				err = decodeInto(m, &v)
				b.Modules.Color[path] = v
			case "SizeModule":
				var v SizeModule
				err = decodeInto(m, &v)
				b.Modules.Size[path] = v
			case "RotationModule":
				var v RotationModule
				err = decodeInto(m, &v)
				b.Modules.Rotation[path] = v
			case "RotationBySpeedModule":
				var v RotationBySpeed
				err = decodeInto(m, &v)
				b.Modules.RotationSpeed[path] = v
			case "ClampVelocityModule":
				var v ClampVelocityModule
				err = decodeInto(m, &v)
				b.Modules.Clamp[path] = v
			case "UVModule":
				var v UVModule
				err = decodeInto(m, &v)
				b.Modules.UV[path] = v
			case "VelocityModule":
				var v VelocityModule
				err = decodeInto(m, &v)
				b.Modules.Velocity[path] = v
			case "ForceModule":
				var v ForceModule
				err = decodeInto(m, &v)
				b.Modules.Force[path] = v
			case "CustomDataModule":
				var v CustomDataModule
				err = decodeInto(m, &v)
				b.Modules.CustomData[path] = v
			}
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%s %s: %w", path, name, err)
			}
		}
		if _, r := component(goID, classRenderer); r != nil {
			rp := RendererProfile{Enabled: num(r, "m_Enabled") != 0, RenderMode: int(num(r, "m_RenderMode")),
				Alignment: int(num(r, "m_RenderAlignment")), LengthScale: num(r, "m_LengthScale"),
				VelocityScale: num(r, "m_VelocityScale"), SortingOrder: int(num(r, "m_SortingOrder"))}
			if err := decodeInto(r["m_Pivot"], &rp.Pivot); err != nil {
				return nil, nil, nil, err
			}
			if rp.RenderMode == 4 {
				mp := toPPtr(r["m_Mesh"])
				switch {
				case mp.file > 0 && mp.file-1 < len(f.Externals) && fmt.Sprintf("%s#%d", f.Externals[mp.file-1], mp.id) == builtinQuad:
					rp.Mesh = meshQuad
				case mp.file == 0 && mp.id != 0:
					mesh, err := unity.DecodeMesh(f, files, mp.id)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("%s: %w", path, err)
					}
					rp.Mesh = mesh.Name
					b.Meshes[mesh.Name] = mesh
				default:
					return nil, nil, nil, fmt.Errorf("%s: unsupported particle mesh %v", path, mp)
				}
			}
			mats, _ := r["m_Materials"].([]any)
			for _, m := range mats {
				if name := materialName(toPPtr(m)); name != "" {
					rp.Materials = append(rp.Materials, &struct {
						Name string `json:"name"`
					}{name})
				} else {
					rp.Materials = append(rp.Materials, nil)
				}
			}
			b.Renderers[path] = rp
			prof.Renderer = path
		}
		b.Profiles[path] = prof
		b.Systems = append(b.Systems, def)
	}
	sort.Slice(b.Systems, func(i, j int) bool { return b.Systems[i].Path < b.Systems[j].Path })
	if len(unresolved) > 0 {
		return nil, nil, nil, fmt.Errorf("materials in no loaded bundle: %v (externals %v)", unresolved, f.Externals)
	}

	shaderName := func(s *serialized, p pptr) string {
		if p.file == 0 {
			o, _ := s.f.Read(p.id)
			parsed, _ := o["m_ParsedForm"].(map[string]any)
			return str(parsed, "m_Name")
		}
		if p.file-1 < len(s.f.Externals) {
			return builtinShaders[fmt.Sprintf("%s#%d", s.f.Externals[p.file-1], p.id)]
		}
		return ""
	}
	for _, r := range materialRefs {
		m, err := r.s.f.Read(r.id)
		if err != nil {
			return nil, nil, nil, err
		}
		mat := Material{Name: materialKeys[r], Shader: shaderName(r.s, toPPtr(m["m_Shader"])), TextureScale: struct{ X, Y float64 }{1, 1}}
		props, _ := m["m_SavedProperties"].(map[string]any)
		envs, _ := props["m_TexEnvs"].([]any)
		for _, e := range envs {
			pair, _ := e.(map[string]any)
			if pair["first"] != "_MainTex" {
				continue
			}
			env, _ := pair["second"].(map[string]any)
			if tp := toPPtr(env["m_Texture"]); tp.id != 0 {
				ts := r.s
				if tp.file != 0 {
					// a dependency's texture; Unity's built-in textures are not read
					ts = external(r.s, tp.file)
					if ts == nil && tp.file <= len(r.s.f.Externals) && strings.HasPrefix(r.s.f.Externals[tp.file-1], "archive:/") {
						return nil, nil, nil, fmt.Errorf("material %s: texture %v in no loaded bundle", mat.Name, tp)
					}
				}
				if ts != nil {
					if mat.Texture, err = textureKey(ts, tp.id); err != nil {
						return nil, nil, nil, err
					}
				}
			}
			_ = decodeInto(env["m_Scale"], &mat.TextureScale)
			_ = decodeInto(env["m_Offset"], &mat.TextureOffset)
		}
		blend, fixed := shaderBlend[mat.Shader]
		mat.UVScroll = mat.Shader == uvScrollShader
		srcKey, dstKey := "_SrcBlend", "_DstBlend"
		if mat.UVScroll {
			srcKey, dstKey = "_BlendSrc", "_BlendDst"
		}
		floats, _ := props["m_Floats"].([]any)
		for _, e := range floats {
			pair, _ := e.(map[string]any)
			switch pair["first"] {
			case srcKey:
				if !fixed {
					blend[0] = int(pair["second"].(float64))
				}
			case dstKey:
				if !fixed {
					blend[1] = int(pair["second"].(float64))
				}
			}
		}
		mat.SourceBlend, mat.DestinationBlend = blend[0], blend[1]
		b.Materials = append(b.Materials, mat)
	}
	for _, id := range byClass[classTexture2D] {
		t := objects[id]
		b.Textures = append(b.Textures, TextureInfo{Name: str(t, "m_Name"), Width: int(num(t, "m_Width")), Height: int(num(t, "m_Height"))})
	}
	return b, inactive, textures, nil
}
