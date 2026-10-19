// Package particle converts the original game's judgement particle prefabs
// (tap effect skin00 and directional flick skin00 "normal", read from the
// original asset bundles on the game's content CDN) and the clear / Full
// Combo / All Perfect fireworks (built into the APK, shipped in assets/apk)
// into a Sonolus particle item. One Sonolus effect is emitted per prefab for
// its view-aligned systems and one per lane-plane tilt (`<prefab>#plane<deg>`).
// The Habahiro variant takes the tap effects from the HABAHIRO bundle and adds
// its width variants (`<prefab>_2` … `_7`) for multi-width charts.
//
// Generate returns the particle item in memory (for embedding in a server);
// WriteSource writes it as a sonolus-pack source tree. The command in
// cmd/sonolus-notgarupa-particle-generator wraps both.
package particle

import (
	"context"
	"fmt"
	"image"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/WindowsSov8forUs/sonolus-core-go/codec"
	"github.com/WindowsSov8forUs/sonolus-core-go/core/resource"
	"github.com/WindowsSov8forUs/sonolus-core-go/database"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/atlas"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/cdn"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/imgutil"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/packout"
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

const habahiroResource = "ingameskin/tapeffect/habahiro"

// Original tap-effect asset bundles by profile key.
var tapBundles = map[string]string{
	"ordinary":    "ingameskin/tapeffect/skin00",
	"directional": "ingameskin/tapeffect/directionalflickskin00normal",
	"habahiro":    habahiroResource,
}

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
	// Fireworks: spawn position and time (see the contract's "clear" note).
	Clear *ContractClear `json:"clear,omitempty"`
}

// ContractClear is the spawn position and time of a firework effect.
type ContractClear struct {
	Branch   string     `json:"branch"`
	AnchorUI [2]float64 `json:"anchorUI"`
	Start    float64    `json:"start"`
	ClipTime float64    `json:"clipTime"`
	Unit     float64    `json:"unit"`
}

// PinnedAssetVersion is the content CDN asset version the generator was
// verified against.
const PinnedAssetVersion = cdn.PinnedVersion

// Source configures where downloadable assets come from.
type Source struct {
	// CacheDir caches downloads (asset bundles under
	// <CacheDir>/cdn/<version>/); empty disables the disk cache.
	CacheDir string
	// AssetVersion is the content CDN asset version ("" = PinnedAssetVersion).
	AssetVersion string
	// Offline fails every download that is not already cached.
	Offline bool
	// HTTPClient is used for downloads (nil: a client with a 300 s timeout).
	HTTPClient *http.Client
	// Log receives one line per download (nil: silent).
	Log func(format string, args ...any)
}

// Variant selects the particle item.
type Variant string

const (
	// Normal is tap effect skin00 (item notgarupa-skin00).
	Normal Variant = "normal"
	// Habahiro is the HABAHIRO tap effects with width variants, for
	// multi-width charts (item notgarupa-habahiro).
	Habahiro Variant = "habahiro"
)

// Options selects what to build. The zero value builds the default item.
type Options struct {
	Variant Variant // "" = Normal
	// Name is the particle item name ("" = notgarupa-skin00, or
	// notgarupa-habahiro for Habahiro).
	Name string
	// Strata is the number of stratified sub-groups per burst for randomised
	// values that Sonolus cannot express linearly (0 = 4).
	Strata int
	// Segments is the maximum number of time segments per particle (0 = 8).
	Segments int
	// Tolerance is the linear-interpolation tolerance in particle units
	// (0 = 0.05).
	Tolerance float64
	// LoopSeconds is the period of looping effects (effect_TapKeep) in
	// seconds (0 = 1).
	LoopSeconds float64
	// NoStandardNames skips the standard Sonolus effect names (copies of the
	// matching prefabs).
	NoStandardNames bool
	Source          Source
}

// Result is one generated particle item.
type Result struct {
	// Item is the database item; its resource fields (Thumbnail, Data,
	// Texture) are left empty for the caller to fill, e.g. with the server's
	// repository URLs of Files().
	Item      database.DatabaseParticleItem
	Data      resource.ParticleData
	Texture   []byte // PNG
	Thumbnail []byte // PNG
	Contract  Contract
}

// Contract describes every effect for the engine (written as contract.json
// by the command).
type Contract struct {
	Clear    string           `json:"clear"`
	Effects  []ContractEffect `json:"effects"`
	Particle string           `json:"particle"`
	Quad     string           `json:"quad"`
	Texture  [2]int           `json:"texture"`
	Width    string           `json:"width"`
}

// Files returns the item's resource files by item.json field name:
// "thumbnail", "data" (gzip-compressed ParticleData) and "texture".
func (r *Result) Files() (map[string][]byte, error) {
	data, err := codec.Compress(r.Data)
	if err != nil {
		return nil, fmt.Errorf("compress particle data: %w", err)
	}
	return map[string][]byte{"thumbnail": r.Thumbnail, "data": data, "texture": r.Texture}, nil
}

// WriteSource (re)creates root as a sonolus-pack source tree holding the
// given particle items.
func WriteSource(root string, results ...*Result) error {
	src, err := packout.Init(root, packout.T("NotGarupa Particles", "NotGarupa 粒子", "NotGarupa パーティクル"))
	if err != nil {
		return err
	}
	for _, r := range results {
		rel, err := src.ItemDir("particles", r.Item.Name, packout.Item{
			Version: r.Item.Version, Title: r.Item.Title, Subtitle: r.Item.Subtitle, Author: r.Item.Author,
			Tags: r.Item.Tags, Description: r.Item.Description,
		})
		if err != nil {
			return err
		}
		files, err := r.Files()
		if err != nil {
			return err
		}
		if err := src.WriteFiles(rel, files); err != nil {
			return err
		}
	}
	return nil
}

// Generate builds the particle item.
func Generate(ctx context.Context, opts Options) (*Result, error) {
	habahiro := false
	switch opts.Variant {
	case "", Normal:
	case Habahiro:
		habahiro = true
	default:
		return nil, fmt.Errorf("unknown variant %q (normal, habahiro)", opts.Variant)
	}
	name := opts.Name
	if name == "" {
		name = "notgarupa-skin00"
		if habahiro {
			name = "notgarupa-habahiro"
		}
	}
	o := convertOptions{maxStrata: opts.Strata, maxSegments: opts.Segments, posTol: opts.Tolerance, loopSeconds: opts.LoopSeconds}
	if o.maxStrata <= 0 {
		o.maxStrata = 4
	}
	if o.maxSegments <= 0 {
		o.maxSegments = 8
	}
	if o.posTol <= 0 {
		o.posTol = 0.05
	}
	if o.loopSeconds <= 0 {
		o.loopSeconds = 1
	}

	// Tap effects: the original asset bundles (official content CDN).
	bundles := &cdn.Client{Version: opts.Source.AssetVersion, CacheDir: opts.Source.CacheDir, Offline: opts.Source.Offline,
		HTTP: opts.Source.HTTPClient, Log: opts.Source.Log, Ctx: ctx}
	store := newTextureStore()
	type tapBundle struct {
		b        *Bundle
		inactive map[string]bool
	}
	tap := map[string]tapBundle{}
	for key, bundleName := range tapBundles {
		if key == "habahiro" && !habahiro {
			continue
		}
		data, err := bundles.Bundle(bundleName)
		if err != nil {
			return nil, err
		}
		b, in, textures, err := readTapBundle(data, key)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", bundleName, err)
		}
		tap[key] = tapBundle{b, in}
		store.addBundle(key, textures)
	}
	hb, hbInactive := tap["habahiro"].b, tap["habahiro"].inactive

	var parts []*part
	for _, key := range []string{"ordinary", "directional"} {
		names := prefabs[key]
		src, skip := tap[key].b, tap[key].inactive
		if habahiro && key == "ordinary" {
			// HABAHIRO: the tap effects come from the HABAHIRO bundle
			src, skip = hb, hbInactive
		}
		for _, prefab := range names {
			ps, err := convertPrefab(src, prefab, o, skip)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", prefab, err)
			}
			parts = append(parts, ps...)
		}
	}

	// Effects added after the standard aliases: fireworks, then the HABAHIRO
	// width variants.
	var extra []*part
	raw, err := readGameClearProfile()
	if err != nil {
		return nil, err
	}
	cb, fireworks, clearFiles, err := loadClear(raw)
	if err != nil {
		return nil, err
	}
	store.addGameClear(clearFiles)
	fo := o
	fo.evenAzimuth = true
	for _, fw := range fireworks {
		ps, err := convertPrefab(cb, fw.name, fo, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fw.name, err)
		}
		for _, p := range ps {
			p.clear = &fw
		}
		extra = append(extra, ps...)
	}
	if habahiro {
		for _, base := range habahiroWidths {
			for w := 2; w <= 7; w++ {
				prefab := fmt.Sprintf("%s_%d", base, w)
				ps, err := convertPrefab(hb, prefab, o, hbInactive)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", prefab, err)
				}
				for _, p := range ps {
					p.width = w
				}
				extra = append(extra, ps...)
			}
		}
	}

	// Sprites: one per (texture content, tile) actually used.
	type spriteEntry struct {
		key  spriteKey
		name string
		img  *image.NRGBA
	}
	var sprites []spriteEntry
	index := map[spriteKey]int{}
	canonical := func(k spriteKey) (spriteKey, error) {
		id, _, err := store.get(k.bundle, k.texture)
		if err != nil {
			return k, err
		}
		k.bundle, k.texture = "", id
		return k, nil
	}
	for _, pt := range append(append([]*part(nil), parts...), extra...) {
		for _, g := range pt.groups {
			for _, p := range g.particles {
				key, err := canonical(p.sprite)
				if err != nil {
					return nil, err
				}
				if _, ok := index[key]; ok {
					continue
				}
				_, tex, err := store.get(p.sprite.bundle, p.sprite.texture)
				if err != nil {
					return nil, err
				}
				tw, th := tex.Rect.Dx()/p.sprite.tilesX, tex.Rect.Dy()/p.sprite.tilesY
				col, row := p.sprite.frame%p.sprite.tilesX, p.sprite.frame/p.sprite.tilesX
				tile, err := imgutil.Crop(tex, col*tw, row*th, tw, th)
				if err != nil {
					return nil, err
				}
				switch p.sprite.blend {
				case "additive":
					tile = imgutil.AdditiveToAlpha(tile)
				case "premultiplied":
					tile = unpremultiply(tile)
				}
				index[key] = len(sprites)
				sprites = append(sprites, spriteEntry{key: key, name: fmt.Sprintf("%s#%d/%dx%d/%s", key.texture[:16], key.frame, key.tilesX, key.tilesY, key.blend), img: tile})
			}
		}
	}
	items := make([]atlas.Item, len(sprites))
	for i, s := range sprites {
		items[i] = atlas.Item{Names: []string{s.name}, Image: s.img}
	}
	packed, err := atlas.PackPOT(items, 2048, 2)
	if err != nil {
		return nil, err
	}
	data := resource.ParticleData{
		Width: float64(packed.Texture.Rect.Dx()), Height: float64(packed.Texture.Rect.Dy()), Interpolation: true,
	}
	for _, s := range sprites {
		p, _ := packed.Rect(s.name)
		data.Sprites = append(data.Sprites, resource.ParticleDataSprite{X: float64(p.X), Y: float64(p.Y), W: float64(p.W), H: float64(p.H)})
	}
	// Every sprite key was canonicalised above, so this cannot fail.
	spriteIndex := func(k spriteKey) int { key, _ := canonical(k); return index[key] }
	var meta []ContractEffect
	byName := map[string]resource.ParticleDataEffect{}
	emit := func(pt *part) {
		e := pt.toSonolus(spriteIndex)
		data.Effects = append(data.Effects, e)
		byName[pt.name] = e
		np := 0
		for _, g := range pt.groups {
			np += len(g.particles) * g.count
		}
		sy := 1.0
		if pt.planeDeg != 0 {
			sy = math.Round(math.Cos(pt.planeDeg*degToRad)*1e6) / 1e6
		}
		m := ContractEffect{Name: pt.name, Prefab: pt.prefab, Duration: pt.duration, Loop: pt.loop,
			PlaneDeg: pt.planeDeg, QuadScale: [2]float64{1, sy}, Groups: len(pt.groups), Particles: np, Systems: pt.systems, Notes: pt.notes,
			Width: pt.width}
		if c := pt.clear; c != nil {
			m.Systems = c.systems
			m.Clear = &ContractClear{Branch: c.branch, AnchorUI: c.anchor, Start: c.start, ClipTime: round6(c.clipAt), Unit: clearUnit}
		}
		meta = append(meta, m)
	}
	for _, pt := range parts {
		emit(pt)
	}
	if !opts.NoStandardNames {
		colours := []string{"NEUTRAL", "RED", "GREEN", "BLUE", "YELLOW", "PURPLE", "CYAN"}
		for _, prefix := range sortedStrings(standardAliases) {
			src, ok := byName[standardAliases[prefix]]
			if !ok {
				continue
			}
			for _, c := range colours {
				alias := src
				alias.Name = resource.ParticleEffectName(prefix + "_" + c)
				data.Effects = append(data.Effects, alias)
			}
		}
		for _, std := range []string{"#LANE_CIRCULAR", "#LANE_LINEAR", "#SLOT_CIRCULAR", "#SLOT_LINEAR", "#JUDGE_LINE_CIRCULAR", "#JUDGE_LINE_LINEAR"} {
			alias := byName["effect_tap"]
			alias.Name = resource.ParticleEffectName(std)
			data.Effects = append(data.Effects, alias)
		}
	}

	for _, pt := range extra {
		emit(pt)
	}

	texture, err := packed.EncodeTexture()
	if err != nil {
		return nil, err
	}
	thumb, err := imgutil.EncodePNG(imgutil.Scale(packed.Texture, 128, 128*packed.Texture.Rect.Dy()/packed.Texture.Rect.Dx()))
	if err != nil {
		return nil, err
	}
	title := "NotGarupa skin00"
	desc := "Tap effect skin00 + directional flick skin00 (normal) + clear / Full Combo / All Perfect fireworks, converted from the original Unity particle systems."
	if habahiro {
		title = "NotGarupa HABAHIRO"
		desc = "Tap effect HABAHIRO (with width variants _2 … _7 for multi-width charts) + directional flick skin00 (normal) + clear / Full Combo / All Perfect fireworks, converted from the original Unity particle systems."
	}
	return &Result{
		Item: database.DatabaseParticleItem{
			Name:        name,
			Version:     packout.ParticleVersion,
			Title:       packout.T(title, title, title),
			Subtitle:    packout.T("BanG Dream! Girls Band Party! (original effects)", "BanG Dream! 少女乐团派对！（原作特效）", "バンドリ！ ガールズバンドパーティ！（原作エフェクト）"),
			Author:      packout.T("sonolus-notgarupa-particle-generator", "sonolus-notgarupa-particle-generator", "sonolus-notgarupa-particle-generator"),
			Tags:        []database.DatabaseTag{},
			Description: packout.T(desc, desc, desc),
		},
		Data:      data,
		Texture:   texture,
		Thumbnail: thumb,
		Contract: Contract{
			Particle: name,
			Quad:     "spawn with the quad centred on the receiving button anchor, half extent g·quadScale (g = world units per particle unit = rate·noteSize·safeRatio); effect coordinates are original particle units (lane spacing 2.2)",
			Clear:    "fireworks (effects with \"clear\"): spawn once per clear at clear.start seconds after the clear (base branch for every clear; fullCombo / allPerfect in addition for status 2 / 3) with the quad centred on uiPoint(clear.anchorUI) (UI units from the screen centre, y up) and half extent clear.unit Sonolus units in both axes (fixed: scaling mode Local ignores the UI scale); clear.clipTime is the activation time inside the branch clip",
			Width:    "HABAHIRO width variants (effects with \"width\"): spawn like the base prefab at the note's effect anchor (centre lane for odd widths, the half-lane button between the two centre lanes for even widths)",
			Texture:  [2]int{packed.Texture.Rect.Dx(), packed.Texture.Rect.Dy()},
			Effects:  meta,
		},
	}, nil
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
