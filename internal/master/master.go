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
//
// Item names (garupa-…) join the bundles of the item's axes; titles join the
// original display names of the same axes (Names: the first of the JP, EN,
// TW and CN master names). The axes a special row fixes are one fixed
// combination: it is written once, as its first bundle in the name and as
// the row's name in the title (the season description of a Collaboration
// row, the skin name of a Limited row).
package master

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed skins.json
var skinsJSON []byte

// Prefix starts every item name.
const Prefix = "garupa-"

// Names are a display name per server master (jp, en, tw, cn).
type Names map[string]string

// NameOrder is the server priority of display names.
var NameOrder = []string{"jp", "en", "tw", "cn"}

// Pick returns the first name in NameOrder ("" when there is none).
func (n Names) Pick() string {
	for _, r := range NameOrder {
		if n[r] != "" {
			return n[r]
		}
	}
	return ""
}

// Row is one master row of a normal axis.
type Row struct {
	ID                 int     `json:"id"`
	Bundle             string  `json:"bundle"`
	Name               string  `json:"name"`
	Names              Names   `json:"names"`
	NoteSyncEdgeMargin float64 `json:"noteSyncEdgeMargin"` // notes only
	EventType          *string `json:"eventType"`          // background only
}

// Special is one Collaboration or Limited row.
type Special struct {
	Kind               string            `json:"kind"` // collaboration | limited
	ID                 int               `json:"id"`
	SeasonSpecialID    int               `json:"seasonSpecialId"`
	Name               string            `json:"name"`
	Names              Names             `json:"names"`      // season description / limited skin name
	Components         map[string]string `json:"components"` // axis → bundle
	NoteSyncEdgeMargin float64           `json:"noteSyncEdgeMargin"`
}

// before reports whether s names a fixed combination in preference to o:
// Limited rows (skin names) first, then the earliest season.
func (s *Special) before(o *Special) bool {
	if (s.Kind == "limited") != (o.Kind == "limited") {
		return s.Kind == "limited"
	}
	if s.SeasonSpecialID != o.SeasonSpecialID {
		return s.SeasonSpecialID < o.SeasonSpecialID
	}
	return s.ID < o.ID
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
	Wording struct {
		DirectionalEffectNormal Names `json:"directionalEffectNormal"`
		DirectionalEffectLight  Names `json:"directionalEffectLight"`
	} `json:"wording"`
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

// axisRows returns the normal rows of an axis.
func (m *Master) axisRows(axis string) []Row {
	switch axis {
	case "notes":
		return m.Normal.Notes
	case "lane":
		return m.Normal.Lane
	case "effect":
		return m.Normal.Effect
	case "soundEffect":
		return m.Normal.SoundEffect
	case "directionalFlick":
		return m.Normal.DirectionalFlick
	case "background":
		return m.Normal.Background
	}
	return nil
}

// AxisName is the display name of a bundle on an axis: its normal row's
// name, else the name of the preferred special row with that component.
func (m *Master) AxisName(axis, bundle string) string {
	if bundle == Habahiro {
		return "HABAHIRO"
	}
	for _, r := range m.axisRows(axis) {
		if r.Bundle == bundle {
			if n := r.Names.Pick(); n != "" {
				return n
			}
			return bundle
		}
	}
	var best *Special
	for i := range m.Special {
		s := &m.Special[i]
		if s.Components[axis] == bundle && (best == nil || s.before(best)) {
			best = s
		}
	}
	if best != nil {
		if n := best.Names.Pick(); n != "" {
			return n
		}
	}
	return bundle
}

// axisValue is one axis of an item.
type axisValue struct{ axis, bundle string }

// compose lists an item's axes in order: the axes fixed by group are one
// part, at the first of them (fixed(first fixed axis)); the other axes are
// free(axis), skipped when empty.
func compose(group *Special, axes []axisValue, free func(axisValue) string, fixed func(axisValue) string) []string {
	var out []string
	written := false
	for _, a := range axes {
		if group != nil {
			if _, ok := group.Components[a.axis]; ok {
				if !written {
					out = append(out, fixed(a))
					written = true
				}
				continue
			}
		}
		if t := free(a); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func bundleOf(a axisValue) string { return a.bundle }

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
	// Group is the special row whose components fix some of the axes (nil:
	// every axis is a normal setting).
	Group *Special
	// Title is the display title (the axes' names).
	Title string
}

func (s Skin) axes() []axisValue {
	return []axisValue{{"notes", s.Notes}, {"lane", s.Lane}, {"directionalFlick", s.Directional}, {"judge", s.Judge}}
}

// freeJudge drops the default judge skin (fixed in the original).
func freeJudge(text func(axisValue) string) func(axisValue) string {
	return func(a axisValue) string {
		if a.axis == "judge" && a.bundle == DefaultJudge {
			return ""
		}
		return text(a)
	}
}

// Name is the Sonolus item name.
func (s Skin) Name() string {
	if s.Habahiro {
		return Prefix + "habahiro-" + s.Directional
	}
	return Prefix + strings.Join(compose(s.Group, s.axes(), freeJudge(bundleOf), bundleOf), "-")
}

func (m *Master) skinTitle(s Skin) string {
	if s.Habahiro {
		return "HABAHIRO / " + m.AxisName("directionalFlick", s.Directional)
	}
	name := func(a axisValue) string { return m.AxisName(a.axis, a.bundle) }
	return strings.Join(compose(s.Group, s.axes(), freeJudge(name), func(axisValue) string { return s.Group.Names.Pick() }), " / ")
}

func (s Skin) key() string { return s.Notes + "|" + s.Lane + "|" + s.Directional + "|" + s.Judge }

// Skins returns every skin item: the normal permutations, the special rows'
// combinations and the HABAHIRO items, without duplicates.
func (m *Master) Skins() []Skin {
	margin := map[string]float64{}
	for _, r := range m.Normal.Notes {
		margin[r.Bundle] = r.NoteSyncEdgeMargin
	}
	var out []Skin
	seen := map[string]int{}
	add := func(s *Special) {
		for _, n := range pick(s, "notes", bundles(m.Normal.Notes)) {
			for _, l := range pick(s, "lane", bundles(m.Normal.Lane)) {
				for _, d := range pick(s, "directionalFlick", bundles(m.Normal.DirectionalFlick)) {
					sk := Skin{Notes: n, Lane: l, Directional: d, Judge: pick(s, "judge", []string{DefaultJudge})[0], NoteSyncEdgeMargin: margin[n], Group: s}
					if s != nil {
						if _, ok := s.Components["notes"]; ok {
							sk.NoteSyncEdgeMargin = s.NoteSyncEdgeMargin
						}
					}
					if i, ok := seen[sk.key()]; !ok {
						seen[sk.key()] = len(out)
						out = append(out, sk)
					} else if s != nil && out[i].Group != nil && s.before(out[i].Group) {
						out[i].Group = s
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
	for i := range out {
		out[i].Title = m.skinTitle(out[i])
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
	Group               *Special
	Title               string
}

func (p Particle) setting() string {
	if p.Light {
		return "light"
	}
	return "normal"
}

func (p Particle) axes() []axisValue {
	return []axisValue{{"effect", p.Effect}, {"directionalFlick", p.Directional}}
}

// fixesDirectional reports whether the group fixes the directional skin
// (the directional effect setting then follows the group as its own part).
func (p Particle) fixesDirectional() bool {
	if p.Group == nil {
		return false
	}
	_, ok := p.Group.Components["directionalFlick"]
	return ok
}

// DirectionalEffect is the directional effect bundle name suffix, e.g.
// "directionalflickskin00normal".
func (p Particle) DirectionalEffect() string {
	if p.Light {
		return "directionalflick" + p.Directional + "light"
	}
	return "directionalflick" + p.Directional + "normal"
}

// Name is the Sonolus item name: the directional part carries the
// directional effect bundle suffix (<directional><normal|light>).
func (p Particle) Name() string {
	if p.Habahiro {
		return Prefix + "habahiro-" + p.Directional + p.setting()
	}
	parts := compose(p.Group, p.axes(), func(a axisValue) string {
		if a.axis == "directionalFlick" {
			return a.bundle + p.setting()
		}
		return a.bundle
	}, bundleOf)
	if p.fixesDirectional() {
		parts = append(parts, p.setting())
	}
	return Prefix + strings.Join(parts, "-")
}

func (m *Master) particleTitle(p Particle) string {
	setting := m.Wording.DirectionalEffectNormal.Pick()
	if p.Light {
		setting = m.Wording.DirectionalEffectLight.Pick()
	}
	name := func(a axisValue) string { return m.AxisName(a.axis, a.bundle) }
	parts := compose(p.Group, p.axes(), name, func(axisValue) string { return p.Group.Names.Pick() })
	return strings.Join(append(parts, setting), " / ")
}

func (p Particle) key() string { return fmt.Sprintf("%s|%s|%v", p.Effect, p.Directional, p.Light) }

// Particles returns every particle item.
func (m *Master) Particles() []Particle {
	var out []Particle
	seen := map[string]int{}
	add := func(s *Special) {
		for _, e := range pick(s, "effect", bundles(m.Normal.Effect)) {
			for _, d := range pick(s, "directionalFlick", bundles(m.Normal.DirectionalFlick)) {
				for _, light := range []bool{false, true} {
					p := Particle{Effect: e, Directional: d, Light: light, Group: s}
					if i, ok := seen[p.key()]; !ok {
						seen[p.key()] = len(out)
						out = append(out, p)
					} else if s != nil && out[i].Group != nil && s.before(out[i].Group) {
						out[i].Group = s
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
	for i := range out {
		out[i].Title = m.particleTitle(out[i])
	}
	return out
}

// Effect is one effect item: the tap SE skin (the directional SE is fixed).
type Effect struct{ SoundEffect, Title string }

// Name is the Sonolus item name.
func (e Effect) Name() string { return Prefix + e.SoundEffect }

// Effects returns every effect item.
func (m *Master) Effects() []Effect {
	var out []Effect
	seen := map[string]bool{}
	for _, b := range bundles(m.Normal.SoundEffect) {
		seen[b] = true
		out = append(out, Effect{SoundEffect: b})
	}
	for _, s := range m.Specials() {
		if b, ok := s.Components["soundEffect"]; ok && !seen[b] {
			seen[b] = true
			out = append(out, Effect{SoundEffect: b})
		}
	}
	for i := range out {
		out[i].Title = m.AxisName("soundEffect", out[i].SoundEffect)
	}
	return out
}

// Background is one background bundle: an event background of the normal
// master, a special background or the HABAHIRO background.
type Background struct {
	Bundle   string
	Habahiro bool
	Title    string
}

// Name is the Sonolus item name.
func (b Background) Name() string { return Prefix + strings.ToLower(b.Bundle) }

// Backgrounds returns every background bundle (each becomes one background
// item from its liveBG_normal texture).
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
	out = append(out, Background{Bundle: Habahiro, Habahiro: true})
	for i := range out {
		out[i].Title = m.AxisName("background", out[i].Bundle)
	}
	return out
}
