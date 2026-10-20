// Package particle converts the original game's judgement particle prefabs
// (tap effects and directional flick effects, read from the original asset
// bundles on the game's content CDN) into Sonolus particle items: one item per combination of tap effect, directional skin and
// directional effect setting that the original can show (see
// internal/master). One Sonolus effect is emitted per prefab for its
// view-aligned systems and one per lane-plane tilt (`<prefab>#plane<deg>`).
// The HABAHIRO items take the tap effects from the HABAHIRO bundle and add
// its width variants (`<prefab>_2` … `_7`) for multi-width charts.
//
// Generate streams the items in memory (for embedding in a server);
// CreateSourceTree writes them as a sonolus-pack source tree. The command in
// cmd/sonolus-notgarupa-particle-generator wraps both.
package particle

import (
	"fmt"
	"image"
	"math"
	"sort"
	"strings"
)

// Prefabs per bundle, in routing order.
var prefabs = map[string][]string{
	"ordinary": {"effect_tap", "effect_tap_good", "effect_tap_great", "effect_tap_perfect",
		"effect_tap_skill_good", "effect_tap_skill_great", "effect_tap_skill_perfect", "effect_tap_swipe", "effect_TapKeep"},
	"directional": {"effect_tap_directional_flick_l", "effect_tap_directional_flick_l_2", "effect_tap_directional_flick_l_3",
		"effect_tap_directional_flick_l_finger", "effect_tap_directional_flick_r", "effect_tap_directional_flick_r_2",
		"effect_tap_directional_flick_r_3", "effect_tap_directional_flick_r_finger"},
}

// Width variants of the HABAHIRO bundle: `<base>_2` … `<base>_7` stretch the
// horizontal glow to the note width (effect_tap has no variants).
var habahiroWidths = []string{"effect_tap_good", "effect_tap_great", "effect_tap_perfect",
	"effect_tap_skill_good", "effect_tap_skill_great", "effect_tap_skill_perfect", "effect_tap_swipe", "effect_TapKeep"}

// Standard Sonolus effect names mapped onto the original prefabs so that the
// pack also works with engines that only use standard names.
var standardAliases = map[string]string{
	"#NOTE_CIRCULAR_TAP":         "effect_tap_perfect",
	"#NOTE_LINEAR_TAP":           "effect_tap_perfect",
	"#NOTE_CIRCULAR_ALTERNATIVE": "effect_tap_swipe",
	"#NOTE_LINEAR_ALTERNATIVE":   "effect_tap_swipe",
	"#NOTE_CIRCULAR_HOLD":        "effect_TapKeep",
	"#NOTE_LINEAR_HOLD":          "effect_TapKeep",
}

// ContractEffect describes one Sonolus effect of the item.
type ContractEffect struct {
	Name      string     `json:"name"`
	Prefab    string     `json:"prefab"`
	Duration  float64    `json:"duration"`
	Loop      bool       `json:"loop"`
	PlaneDeg  float64    `json:"planeDeg,omitempty"`
	QuadScale [2]float64 `json:"quadScale"`
	Groups    int        `json:"groups"`
	Particles int        `json:"particles"`
	Systems   []string   `json:"systems"`
	Notes     []string   `json:"notes,omitempty"`
	// HABAHIRO width variant: note width in lanes.
	Width int `json:"width,omitempty"`
}

func sortedStrings(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func unpremultiply(src *image.NRGBA) *image.NRGBA {
	// The texture of a premultiplied material is stored straight in the PNG;
	// nothing to undo for normal alpha blending.
	return src
}

// convertPrefab converts all active systems under one prefab; skip lists the
// systems that are inactive in the original prefab.
func convertPrefab(b *Bundle, prefab string, o convertOptions, skip map[string]bool) ([]*part, error) {
	var systems []*system
	var edgeOn []string
	for i := range b.Systems {
		def := &b.Systems[i]
		if def.Path != prefab && !strings.HasPrefix(def.Path, prefab+"/") {
			continue
		}
		if skip[def.Path] {
			continue
		}
		s := resolveSystem(b, def)
		if s == nil || s.initial == nil || s.emission == nil || !s.emission.Enabled || s.renderer == nil || !s.renderer.Enabled || s.material == nil {
			continue
		}
		if deg, isPlane := planeTilt(s); isPlane && math.Abs(math.Cos(deg*degToRad)) < 0.02 {
			edgeOn = append(edgeOn, fmt.Sprintf("%s: lane plane seen edge-on (%g°), invisible, skipped", def.Path, deg))
			continue
		}
		systems = append(systems, s)
	}
	if len(systems) == 0 {
		return nil, fmt.Errorf("no active systems")
	}
	loop := false
	for _, s := range systems {
		if s.def.Path == prefab {
			continue
		}
		if s.prof.System.Looping {
			loop = true
		}
	}
	// Duration: the last particle death of one play (non-looping) or the loop period.
	duration := 0.0
	for _, s := range systems {
		for _, e := range s.emissions(10) {
			duration = math.Max(duration, e[0]+s.initial.StartLifetime.eval(0, 1))
		}
	}
	duration = math.Min(duration, 5)
	if loop {
		duration = o.loopSeconds
	}
	parts := map[float64]*part{}
	get := func(deg float64) *part {
		if p, ok := parts[deg]; ok {
			return p
		}
		name := prefab
		if deg != 0 {
			name = fmt.Sprintf("%s#plane%g", prefab, deg)
		}
		p := &part{name: name, prefab: prefab, bundle: b.Key, planeDeg: deg, duration: duration, loop: loop}
		parts[deg] = p
		return p
	}
	// For looping effects, a plane part that holds spinning rings gets a
	// period long enough for every spin to come back to its start angle.
	for _, s := range systems {
		deg, isPlane := planeTilt(s)
		p := get(0)
		if isPlane {
			p = get(deg)
			if loop && s.rotation != nil && s.rotation.Enabled && s.rotation.Curve.State == 0 {
				p.duration = math.Max(p.duration, spinPeriod(s.rotation.Curve.Scalar, o.loopSeconds))
			}
		}
	}
	for _, s := range systems {
		deg, isPlane := planeTilt(s)
		p := get(0)
		if isPlane {
			p = get(deg)
		}
		if err := convertSystem(p, s, o, loop); err != nil {
			return nil, fmt.Errorf("%s: %w", s.def.Path, err)
		}
	}
	if len(edgeOn) > 0 {
		get(0).notes = append(get(0).notes, edgeOn...)
	}
	var out []*part
	keys := make([]float64, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Float64s(keys)
	for _, k := range keys {
		if len(parts[k].groups) > 0 {
			out = append(out, parts[k])
		}
	}
	return out, nil
}

// spinPeriod returns the shortest multiple of base (≤ 12 s) after which a
// constant angular velocity ω (rad/s) completes whole turns.
func spinPeriod(omega, base float64) float64 {
	if omega == 0 {
		return base
	}
	for k := 1; k <= 12; k++ {
		t := float64(k) * base
		turns := math.Abs(omega) * t / (2 * math.Pi)
		if math.Abs(turns-math.Round(turns)) < 0.02 {
			return t
		}
	}
	return base
}
