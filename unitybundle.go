package particle

// Tap-effect particle bundles read directly from the original asset bundles
// (official CDN): the prefabs' GameObject / Transform hierarchy and every
// ParticleSystem, ParticleSystemRenderer, Material and Texture2D, through the
// bundles' type trees, into the converter's Bundle model.

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"sort"
	"strings"

	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/unity"
)

// Unity class IDs.
const (
	classGameObject = 1
	classTransform  = 4
	classMaterial   = 21
	classTexture2D  = 28
	classMesh       = 43
	classShader     = 48
	classParticle   = 198
	classRenderer   = 199
)

// builtinShaders names shaders referenced in Unity's built-in resource files
// by path ID (the bundles only reference them).
var builtinShaders = map[string]string{
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

// textureSource decodes a texture of a bundle by name.
type textureSource func(name string) (*image.NRGBA, error)

// bundleFromUnity builds the Bundle of one original tap-effect asset bundle
// (key prefixes the system identities) and returns the paths of systems whose
// GameObject is inactive in the prefab hierarchy (ParticleSystem.Play skips
// them).
func bundleFromUnity(data []byte, key string) (*Bundle, map[string]bool, error) {
	b, inactive, _, err := readTapBundle(data, key)
	return b, inactive, err
}

// readTapBundle is bundleFromUnity plus a decoder for the bundle's textures
// (original pixels, from the bundle's resource stream).
func readTapBundle(data []byte, key string) (*Bundle, map[string]bool, textureSource, error) {
	b, inactive, f, files, err := buildBundle(data, key)
	if err != nil {
		return nil, nil, nil, err
	}
	textures := map[string]int64{}
	for _, id := range f.Order {
		if f.Objects[id].ClassID != classTexture2D {
			continue
		}
		o, err := f.Read(id)
		if err != nil {
			return nil, nil, nil, err
		}
		textures[str(o, "m_Name")] = id
	}
	src := func(name string) (*image.NRGBA, error) {
		id, ok := textures[name]
		if !ok {
			return nil, fmt.Errorf("bundle %s has no texture %s", key, name)
		}
		return unity.DecodeTexture(f, files, id)
	}
	return b, inactive, src, nil
}

func buildBundle(data []byte, key string) (*Bundle, map[string]bool, *unity.File, map[string][]byte, error) {
	files, err := unity.ReadBundle(data)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var f *unity.File
	for name, sf := range files {
		if strings.HasSuffix(name, ".resS") {
			continue
		}
		if f, err = unity.Parse(sf); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	if f == nil {
		return nil, nil, nil, nil, fmt.Errorf("bundle has no serialized file")
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
			return nil, nil, nil, nil, err
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
	materialName := func(p pptr) string {
		if p.file != 0 || p.id == 0 {
			return ""
		}
		return str(objects[p.id], "m_Name")
	}
	for _, psID := range byClass[classParticle] {
		ps := objects[psID]
		goID := toPPtr(ps["m_GameObject"]).id
		tid, t := component(goID, classTransform)
		if t == nil {
			return nil, nil, nil, nil, fmt.Errorf("particle system %d has no Transform", psID)
		}
		path := pathOf(tid)
		if !activeInHierarchy(tid) {
			inactive[path] = true
		}
		def := SystemDef{Identity: key + ":" + path, Path: path, Profile: path, ParentTransforms: []TransformProfile{}}
		if def.Transform, err = transformProfile(t); err != nil {
			return nil, nil, nil, nil, err
		}
		for father := toPPtr(t["m_Father"]).id; father != 0; father = toPPtr(objects[father]["m_Father"]).id {
			pt, err := transformProfile(objects[father])
			if err != nil {
				return nil, nil, nil, nil, err
			}
			def.ParentTransforms = append(def.ParentTransforms, pt)
		}
		var prof SystemProfile
		if err := decodeInto(ps, &prof.System); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("%s: %w", path, err)
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
				return nil, nil, nil, nil, fmt.Errorf("%s %s: %w", path, name, err)
			}
		}
		if _, r := component(goID, classRenderer); r != nil {
			rp := RendererProfile{Enabled: num(r, "m_Enabled") != 0, RenderMode: int(num(r, "m_RenderMode")),
				Alignment: int(num(r, "m_RenderAlignment")), LengthScale: num(r, "m_LengthScale"),
				VelocityScale: num(r, "m_VelocityScale"), SortingOrder: int(num(r, "m_SortingOrder"))}
			if err := decodeInto(r["m_Pivot"], &rp.Pivot); err != nil {
				return nil, nil, nil, nil, err
			}
			if rp.RenderMode == 4 {
				mp := toPPtr(r["m_Mesh"])
				switch {
				case mp.file > 0 && mp.file-1 < len(f.Externals) && fmt.Sprintf("%s#%d", f.Externals[mp.file-1], mp.id) == builtinQuad:
					rp.Mesh = meshQuad
				case mp.file == 0 && mp.id != 0:
					mesh, err := unity.DecodeMesh(f, files, mp.id)
					if err != nil {
						return nil, nil, nil, nil, fmt.Errorf("%s: %w", path, err)
					}
					rp.Mesh = mesh.Name
					b.Meshes[mesh.Name] = mesh
				default:
					return nil, nil, nil, nil, fmt.Errorf("%s: unsupported particle mesh %v", path, mp)
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

	shaderName := func(p pptr) string {
		if p.file == 0 {
			parsed, _ := objects[p.id]["m_ParsedForm"].(map[string]any)
			return str(parsed, "m_Name")
		}
		if p.file-1 < len(f.Externals) {
			return builtinShaders[fmt.Sprintf("%s#%d", f.Externals[p.file-1], p.id)]
		}
		return ""
	}
	for _, id := range byClass[classMaterial] {
		m := objects[id]
		mat := Material{Name: str(m, "m_Name"), Shader: shaderName(toPPtr(m["m_Shader"])), TextureScale: struct{ X, Y float64 }{1, 1}}
		props, _ := m["m_SavedProperties"].(map[string]any)
		envs, _ := props["m_TexEnvs"].([]any)
		for _, e := range envs {
			pair, _ := e.(map[string]any)
			if pair["first"] != "_MainTex" {
				continue
			}
			env, _ := pair["second"].(map[string]any)
			if tp := toPPtr(env["m_Texture"]); tp.file == 0 && tp.id != 0 {
				mat.Texture = str(objects[tp.id], "m_Name")
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
	return b, inactive, f, files, nil
}
