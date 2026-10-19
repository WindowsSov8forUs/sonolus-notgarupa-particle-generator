package particle

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestBundleFromUnityMatchesProfile compares a bundle read from the original
// asset bundle (UNITY_BUNDLE) with a reference Bundle model in JSON
// (UNITY_REFERENCE, the converter's Bundle shape). Skipped unless both are set.
func TestBundleFromUnityMatchesProfile(t *testing.T) {
	path, refPath, key := os.Getenv("UNITY_BUNDLE"), os.Getenv("UNITY_REFERENCE"), os.Getenv("UNITY_KEY")
	if path == "" || refPath == "" {
		t.Skip("UNITY_BUNDLE / UNITY_REFERENCE not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, gotInactive, err := bundleFromUnity(data, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Bundle   Bundle          `json:"bundle"`
		Inactive map[string]bool `json:"inactive"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	want := &ref.Bundle
	if len(got.Systems) != len(want.Systems) {
		t.Errorf("systems: got %d want %d", len(got.Systems), len(want.Systems))
	}
	canon := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	resolved := func(b *Bundle, d *SystemDef) map[string]string {
		s := resolveSystem(b, d)
		out := map[string]string{"transform": canon(d.Transform), "parents": canon(d.ParentTransforms), "system": canon(s.prof.System)}
		for name, v := range map[string]any{"initial": s.initial, "emission": s.emission, "shape": s.shape, "color": s.color,
			"size": s.size, "rotation": s.rotation, "clamp": s.clamp, "uv": s.uv, "renderer": s.renderer, "material": s.material} {
			out[name] = canon(v)
		}
		if s.material != nil {
			m := *s.material
			m.Fragment = "" // descriptive only, not used by the converter
			out["material"] = canon(m)
		}
		return out
	}
	byPath := map[string]*SystemDef{}
	for i := range got.Systems {
		byPath[got.Systems[i].Path] = &got.Systems[i]
	}
	diffs := 0
	for i := range want.Systems {
		w := &want.Systems[i]
		if i < len(got.Systems) && got.Systems[i].Path != w.Path {
			t.Errorf("system %d order: got %s want %s", i, got.Systems[i].Path, w.Path)
		}
		g := byPath[w.Path]
		if g == nil {
			t.Errorf("missing system %s", w.Path)
			continue
		}
		rg, rw := resolved(got, g), resolved(want, w)
		for k := range rw {
			if rg[k] != rw[k] && diffs < 20 {
				diffs++
				t.Errorf("%s %s:\n got  %s\n want %s", w.Path, k, rg[k], rw[k])
			}
		}
	}
	var gotTex, wantTex []TextureInfo
	gotTex, wantTex = got.Textures, want.Textures
	texSet := func(l []TextureInfo) map[string]TextureInfo {
		m := map[string]TextureInfo{}
		for _, x := range l {
			m[x.Name] = x
		}
		return m
	}
	for name, w := range texSet(wantTex) {
		if g, ok := texSet(gotTex)[name]; !ok || g != w {
			t.Errorf("texture %s: got %+v want %+v", name, g, w)
		}
	}
	if !reflect.DeepEqual(gotInactive, ref.Inactive) {
		t.Errorf("inactive:\n got  %v\n want %v", gotInactive, ref.Inactive)
	}
	t.Logf("%d systems, %d materials, %d textures, %d inactive", len(got.Systems), len(got.Materials), len(got.Textures), len(gotInactive))
}
