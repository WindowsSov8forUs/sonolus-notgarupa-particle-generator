// Package master holds the original game's skin master data (JP 10.1.4) and
// derives from it every Sonolus item the generators build: one item per
// combination of the live settings that affect a resource type, with the
// special (Collaboration / Limited) rows as fixed combinations and HABAHIRO
// as the chart-forced combination.
//
// The rules follow the original:
//   - the normal live settings pick one row per axis independently;
//   - a special row replaces the axes listed in its components, the other
//     axes keep the normal settings (so they are permuted);
//   - judge skin and directional SE are fixed (skin00, directionalflickskin00);
//   - HABAHIRO charts force notes, lane, judge, tap effect and background,
//     and keep the tap SE and the directional skin and effect.
package master

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed skins.json
var skinsJSON []byte

// Row is one master row of a normal axis.
type Row struct {
	ID                 int     `json:"id"`
	Bundle             string  `json:"bundle"`
	Name               string  `json:"name"`
	NoteSyncEdgeMargin float64 `json:"noteSyncEdgeMargin"` // notes only
	EventType          *string `json:"eventType"`          // background only
}

// Special is one Collaboration or Limited row.
type Special struct {
	Kind               string            `json:"kind"` // collaboration | limited
	ID                 int               `json:"id"`
	SeasonSpecialID    int               `json:"seasonSpecialId"`
	Name               string            `json:"name"`
	Components         map[string]string `json:"components"` // axis → bundle
	NoteSyncEdgeMargin float64           `json:"noteSyncEdgeMargin"`
}

// Master is the skin master data.
type Master struct {
	Normal struct {
		Notes            []Row `json:"notes"`
		Lane             []Row `json:"lane"`
		Effect           []Row `json:"effect"`
		SoundEffect      []Row `json:"soundEffect"`
		DirectionalFlick []Row `json:"directionalFlick"`
		Background       []Row `json:"background"`
	} `json:"normal"`
	Special []Special `json:"special"`
}

// Fixed bundles of the original.
const (
	DefaultJudge     = "skin00"
	DirectionalSE    = "directionalflickskin00"
	Habahiro         = "habahiro"
	HabahiroPreField = "skin00" // lane shown before a HABAHIRO lane change
)

// Excluded lists master entries that no source can deliver; they are left
// out entirely (an incomplete combination is never generated).
var Excluded = []Exclusion{
	{Special: "collaboration 3", Reason: "seasonSpecialId 36 (skinapril2019, with a cut-in component): its tap effect and tap SE bundles are no longer on the official CDN and the Bestdori rips are incomplete; the original treats the row as unavailable"},
	{Background: "skin01", Reason: "story event background: no bundle on the official CDN or on any Bestdori server"},
	{Background: "skin04", Reason: "live try event background: no bundle on the official CDN or on any Bestdori server"},
	{Background: "skin_teamlivefestival", Reason: "team live festival event background: the bundle has no live background texture (liveBG); only the team-live-festival multi live composes it, from per-team stage objects, and a single live never shows it"},
}

// Exclusion is one excluded special row (by "<kind> <id>") or background.
type Exclusion struct {
	Special    string
	Background string
	Reason     string
}

func (s Special) key() string { return fmt.Sprintf("%s %d", s.Kind, s.ID) }

var (
	loadOnce sync.Once
	loaded   *Master
	loadErr  error
)

// Load returns the embedded master data.
func Load() (*Master, error) {
	loadOnce.Do(func() {
		loaded = &Master{}
		loadErr = json.Unmarshal(skinsJSON, loaded)
	})
	return loaded, loadErr
}

// Specials returns the special rows that are not excluded.
func (m *Master) Specials() []Special {
	var out []Special
	for _, s := range m.Special {
		excluded := false
		for _, e := range Excluded {
			excluded = excluded || e.Special == s.key()
		}
		if !excluded {
			out = append(out, s)
		}
	}
	return out
}

func bundles(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Bundle
	}
	return out
}

// pick returns the special component bundle for axis, or every normal choice.
func pick(s *Special, axis string, normal []string) []string {
	if s != nil {
		if b, ok := s.Components[axis]; ok {
			return []string{b}
		}
	}
	return normal
}

// DirectionalFlicks returns every directional skin a player can end up with:
// the normal rows, then the special directional components.
func (m *Master) DirectionalFlicks() []string {
	out := bundles(m.Normal.DirectionalFlick)
	seen := map[string]bool{}
	for _, b := range out {
		seen[b] = true
	}
	for _, s := range m.Specials() {
		if b, ok := s.Components["directionalFlick"]; ok && !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	return out
}

// Skin is one skin item: note skin, lane, directional skin and judge skin.
type Skin struct {
	Notes, Lane, Directional, Judge string
	// Habahiro marks the chart-forced HABAHIRO item: Notes, Lane and Judge
	// are habahiro and the lane before the lane change is HabahiroPreField.
	Habahiro bool
	// NoteSyncEdgeMargin is the master value of the note skin (0 for HABAHIRO).
	NoteSyncEdgeMargin float64
}

// Name is the Sonolus item name.
func (s Skin) Name() string {
	if s.Habahiro {
		return "notgarupa-habahiro-" + s.Directional
	}
	name := fmt.Sprintf("notgarupa-%s-%s-%s", s.Notes, s.Lane, s.Directional)
	if s.Judge != DefaultJudge {
		name += "-" + s.Judge
	}
	return name
}

// Skins returns every skin item: the normal permutations, the special rows'
// combinations and the HABAHIRO items, without duplicates.
func (m *Master) Skins() []Skin {
	margin := map[string]float64{}
	for _, r := range m.Normal.Notes {
		margin[r.Bundle] = r.NoteSyncEdgeMargin
	}
	var out []Skin
	seen := map[string]bool{}
	add := func(s *Special) {
		for _, n := range pick(s, "notes", bundles(m.Normal.Notes)) {
			for _, l := range pick(s, "lane", bundles(m.Normal.Lane)) {
				for _, d := range pick(s, "directionalFlick", bundles(m.Normal.DirectionalFlick)) {
					sk := Skin{Notes: n, Lane: l, Directional: d, Judge: pick(s, "judge", []string{DefaultJudge})[0], NoteSyncEdgeMargin: margin[n]}
					if s != nil {
						if _, ok := s.Components["notes"]; ok {
							sk.NoteSyncEdgeMargin = s.NoteSyncEdgeMargin
						}
					}
					if !seen[sk.Name()] {
						seen[sk.Name()] = true
						out = append(out, sk)
					}
				}
			}
		}
	}
	add(nil)
	for _, s := range m.Specials() {
		if touches(s, "notes", "lane", "directionalFlick", "judge") {
			add(&s)
		}
	}
	for _, d := range m.DirectionalFlicks() {
		out = append(out, Skin{Notes: Habahiro, Lane: Habahiro, Directional: d, Judge: Habahiro, Habahiro: true})
	}
	return out
}

func touches(s Special, axes ...string) bool {
	for _, a := range axes {
		if _, ok := s.Components[a]; ok {
			return true
		}
	}
	return false
}

// Particle is one particle item: tap effect, directional skin and the
// directional effect setting.
type Particle struct {
	Effect, Directional string
	Light               bool // directional effect setting 1 (light) instead of 0 (normal)
	Habahiro            bool // Effect is the chart-forced habahiro tap effect
}

// DirectionalEffect is the directional effect bundle name suffix, e.g.
// "directionalflickskin00normal".
func (p Particle) DirectionalEffect() string {
	if p.Light {
		return "directionalflick" + p.Directional + "light"
	}
	return "directionalflick" + p.Directional + "normal"
}

// Name is the Sonolus item name.
func (p Particle) Name() string {
	return fmt.Sprintf("notgarupa-%s-%s", p.Effect, p.DirectionalEffect()[len("directionalflick"):])
}

// Particles returns every particle item.
func (m *Master) Particles() []Particle {
	var out []Particle
	seen := map[string]bool{}
	add := func(s *Special) {
		for _, e := range pick(s, "effect", bundles(m.Normal.Effect)) {
			for _, d := range pick(s, "directionalFlick", bundles(m.Normal.DirectionalFlick)) {
				for _, light := range []bool{false, true} {
					p := Particle{Effect: e, Directional: d, Light: light}
					if !seen[p.Name()] {
						seen[p.Name()] = true
						out = append(out, p)
					}
				}
			}
		}
	}
	add(nil)
	for _, s := range m.Specials() {
		if touches(s, "effect", "directionalFlick") {
			add(&s)
		}
	}
	for _, d := range m.DirectionalFlicks() {
		for _, light := range []bool{false, true} {
			out = append(out, Particle{Effect: Habahiro, Directional: d, Light: light, Habahiro: true})
		}
	}
	return out
}

// Effect is one effect item: the tap SE skin (the directional SE is fixed).
type Effect struct{ SoundEffect string }

// Name is the Sonolus item name.
func (e Effect) Name() string { return "notgarupa-" + e.SoundEffect }

// Effects returns every effect item.
func (m *Master) Effects() []Effect {
	var out []Effect
	seen := map[string]bool{}
	for _, b := range bundles(m.Normal.SoundEffect) {
		seen[b] = true
		out = append(out, Effect{b})
	}
	for _, s := range m.Specials() {
		if b, ok := s.Components["soundEffect"]; ok && !seen[b] {
			seen[b] = true
			out = append(out, Effect{b})
		}
	}
	return out
}

// Background is one background bundle: an event background of the normal
// master, a special background or the HABAHIRO background.
type Background struct {
	Bundle   string
	Habahiro bool
}

// Backgrounds returns every background bundle (each becomes a normal and,
// when the bundle has one, a fever item).
func (m *Master) Backgrounds() []Background {
	var out []Background
	seen := map[string]bool{}
	add := func(b string) {
		for _, e := range Excluded {
			if e.Background == b {
				return
			}
		}
		if !seen[b] {
			seen[b] = true
			out = append(out, Background{Bundle: b})
		}
	}
	for _, r := range m.Normal.Background {
		add(r.Bundle)
	}
	for _, s := range m.Specials() {
		if b, ok := s.Components["background"]; ok {
			add(b)
		}
	}
	return append(out, Background{Bundle: Habahiro, Habahiro: true})
}
