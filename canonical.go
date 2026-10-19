package particle

import (
	"fmt"
	"math"
	"strings"
)

// Canonical effect durations. A Sonolus particle effect plays its particles
// in fractions of the duration the engine spawns it with, and an engine
// cannot read item metadata, so every item puts each effect family on one
// fixed duration that the engine spawns it with (the NotGarupa engine's
// dur* constants):
//   - one-shot effects: the longest one of the family over every tap effect
//     skin; a shorter effect keeps its timing and ends early;
//   - looping effects: a common multiple of every period of the family; a
//     shorter period repeats.
//
// Families group the width variants (<prefab>_2 … _7) with their base
// prefab; the lane-plane parts of TapKeep loop on their own period.
var canonicalDurations = map[string]float64{
	"effect_tap":                            0.8,
	"effect_tap_good":                       0.8,
	"effect_tap_great":                      0.85,
	"effect_tap_perfect":                    1,
	"effect_tap_skill_good":                 0.8,
	"effect_tap_skill_great":                0.85,
	"effect_tap_skill_perfect":              1,
	"effect_tap_swipe":                      1,
	"effect_TapKeep":                        1,
	"effect_TapKeep#plane":                  18,
	"effect_tap_directional_flick_l":        0.7,
	"effect_tap_directional_flick_r":        0.7,
	"effect_tap_directional_flick_l_2":      1.1,
	"effect_tap_directional_flick_r_2":      1.1,
	"effect_tap_directional_flick_l_3":      1.1,
	"effect_tap_directional_flick_r_3":      1.1,
	"effect_tap_directional_flick_l_finger": 0.35,
	"effect_tap_directional_flick_r_finger": 0.35,
}

// family is the canonical-duration family of a part.
func family(pt *part) string {
	name := pt.prefab
	if pt.width > 0 {
		name = strings.TrimSuffix(name, fmt.Sprintf("_%d", pt.width))
	}
	if pt.loop && pt.planeDeg != 0 {
		name += "#plane"
	}
	return name
}

// canonicalPart returns pt on its family's canonical duration (a copy when
// that changes anything; parts are shared between items). Fireworks keep
// their own durations: they are the same in every item.
func canonicalPart(pt *part) (*part, error) {
	if pt.clear != nil {
		return pt, nil
	}
	c, ok := canonicalDurations[family(pt)]
	if !ok {
		return nil, fmt.Errorf("%s: no canonical duration for family %s", pt.name, family(pt))
	}
	if math.Abs(pt.duration-c) <= 1e-6 { // float32 source values
		return pt, nil
	}
	out := *pt
	out.duration = c
	if !pt.loop {
		if pt.duration > c+1e-6 {
			return nil, fmt.Errorf("%s: duration %g s exceeds the canonical %g s", pt.name, pt.duration, c)
		}
		return &out, nil
	}
	repeats := c / pt.duration
	if math.Abs(repeats-math.Round(repeats)) > 1e-6 {
		return nil, fmt.Errorf("%s: loop period %g s does not divide the canonical %g s", pt.name, pt.duration, c)
	}
	out.groups = make([]outGroup, len(pt.groups))
	for i, g := range pt.groups {
		ng := outGroup{count: g.count}
		for k := 0; k < int(math.Round(repeats)); k++ {
			shift := float64(k) * pt.duration
			for _, p := range g.particles {
				p.start += shift
				p.end += shift
				ng.particles = append(ng.particles, p)
			}
		}
		out.groups[i] = ng
	}
	return &out, nil
}
