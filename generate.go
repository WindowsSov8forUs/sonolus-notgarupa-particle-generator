package particle

import (
	"context"
	"fmt"
	"image"
	"math"
	"net/http"
	"runtime"
	"sync"

	"github.com/WindowsSov8forUs/sonolus-core-go/codec"
	"github.com/WindowsSov8forUs/sonolus-core-go/core/resource"
	"github.com/WindowsSov8forUs/sonolus-core-go/database"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/atlas"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/cdn"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/imgutil"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/master"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/packout"
)

// PinnedAssetVersion is the content CDN asset version the generator was
// verified against.
const PinnedAssetVersion = cdn.PinnedVersion

// Conversion constants: stratified sub-groups per burst for randomised values
// that Sonolus cannot express linearly, time segments per particle, the
// linear-interpolation tolerance (particle units) and the period of looping
// effects (effect_TapKeep).
var conversion = convertOptions{maxStrata: 4, maxSegments: 8, posTol: 0.05, loopSeconds: 1}

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

// Options configures a run. The zero value builds everything.
type Options struct {
	Source Source
	// Workers bounds the parallel conversions (0 = runtime.NumCPU()).
	Workers int
}

// Result is one generated particle item.
type Result struct {
	// Item is the database item; its resource fields (Thumbnail, Data,
	// Texture) are left empty for the caller to fill, e.g. with the server's
	// repository URLs of Files().
	Item database.DatabaseParticleItem
	// Habahiro marks the items of #HABAHIRO levels, which the server forces
	// for them as the original does.
	Habahiro  bool
	Data      resource.ParticleData
	Texture   []byte // PNG
	Thumbnail []byte // PNG
	Contract  Contract
}

// Contract describes every effect for the engine (written as
// contracts/<item>.json by the command).
type Contract struct {
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

// SourceTree is a sonolus-pack source tree of particle items.
type SourceTree struct{ src *packout.Source }

// CreateSourceTree (re)creates root as an empty sonolus-pack source tree.
func CreateSourceTree(root string) (*SourceTree, error) {
	src, err := packout.Init(root, packout.T("NotGarupa Particles", "NotGarupa 粒子", "NotGarupa パーティクル"))
	if err != nil {
		return nil, err
	}
	return &SourceTree{src}, nil
}

// Add writes one item into the tree.
func (t *SourceTree) Add(r *Result) error {
	rel, err := t.src.ItemDir("particles", r.Item.Name, packout.Item{
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
	return t.src.WriteFiles(rel, files)
}

// Items returns the names of every item Generate builds, in order.
func Items() ([]string, error) {
	m, err := master.Load()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range m.Particles() {
		out = append(out, p.Name())
	}
	return out, nil
}

const tapEffectBundle = "ingameskin/tapeffect/"

// converted is the conversion of one bundle's prefabs.
type converted struct {
	parts []*part
	err   error
}

// Generate builds every particle item and hands each to emit, serially and in
// the order of Items, as soon as it is ready. It stops at the first error.
func Generate(ctx context.Context, opts Options, emit func(*Result) error) error {
	m, err := master.Load()
	if err != nil {
		return err
	}
	specs := m.Particles()
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	client := &cdn.Client{Version: opts.Source.AssetVersion, CacheDir: opts.Source.CacheDir, Offline: opts.Source.Offline,
		HTTP: opts.Source.HTTPClient, Log: opts.Source.Log, Ctx: ctx}

	// Units of conversion: the ordinary prefabs of each tap-effect bundle,
	// the directional prefabs of each directional-effect bundle and the
	// HABAHIRO width variants.
	type unit struct {
		bundle  string
		prefabs []string
		width   bool
	}
	var units []unit
	seen := map[string]bool{}
	addUnit := func(u unit) {
		k := fmt.Sprint(u.bundle, u.width)
		if !seen[k] {
			seen[k] = true
			units = append(units, u)
		}
	}
	for _, p := range specs {
		addUnit(unit{bundle: tapEffectBundle + p.Effect, prefabs: prefabs["ordinary"]})
		addUnit(unit{bundle: tapEffectBundle + p.DirectionalEffect(), prefabs: prefabs["directional"]})
		if p.Habahiro {
			var names []string
			for _, base := range habahiroWidths {
				for w := 2; w <= 7; w++ {
					names = append(names, fmt.Sprintf("%s_%d", base, w))
				}
			}
			addUnit(unit{bundle: tapEffectBundle + p.Effect, prefabs: names, width: true})
		}
	}

	store := newTextureStore()
	// bundles: download and parse once
	type loadedBundle struct {
		b        *Bundle
		inactive map[string]bool
	}
	var bmu sync.Mutex
	loaded := map[string]*loadedBundle{}
	loadOnce := map[string]*sync.Once{}
	loadErr := map[string]error{}
	load := func(name string) (*loadedBundle, error) {
		bmu.Lock()
		once, ok := loadOnce[name]
		if !ok {
			once = &sync.Once{}
			loadOnce[name] = once
		}
		bmu.Unlock()
		once.Do(func() {
			data, err := client.Bundle(name)
			if err == nil {
				var b *Bundle
				var inactive map[string]bool
				var src textureSource
				b, inactive, src, err = readTapBundle(data, name)
				if err == nil {
					store.addBundle(name, src)
					bmu.Lock()
					loaded[name] = &loadedBundle{b, inactive}
					bmu.Unlock()
				}
			}
			if err != nil {
				bmu.Lock()
				loadErr[name] = fmt.Errorf("%s: %w", name, err)
				bmu.Unlock()
			}
		})
		bmu.Lock()
		defer bmu.Unlock()
		return loaded[name], loadErr[name]
	}

	results := make([]converted, len(units))
	if err := parallel(ctx, workers, len(units), func(i int) error {
		u := units[i]
		lb, err := load(u.bundle)
		if err != nil {
			return err
		}
		for _, prefab := range u.prefabs {
			ps, err := convertPrefab(lb.b, prefab, conversion, lb.inactive)
			if err != nil {
				return fmt.Errorf("%s %s: %w", u.bundle, prefab, err)
			}
			if u.width {
				var w int
				fmt.Sscanf(prefab[len(prefab)-1:], "%d", &w)
				for _, p := range ps {
					p.width = w
				}
			}
			results[i].parts = append(results[i].parts, ps...)
		}
		return nil
	}); err != nil {
		return err
	}
	partsOf := func(bundle string, width bool) []*part {
		for i, u := range units {
			if u.bundle == bundle && u.width == width {
				return results[i].parts
			}
		}
		return nil
	}

	tiles := &tileCache{tiles: map[spriteKey]*image.NRGBA{}}
	// items: assemble in parallel, emit in order
	out := make([]*Result, len(specs))
	ready := make([]chan struct{}, len(specs))
	closed := make([]sync.Once, len(specs))
	for i := range ready {
		ready[i] = make(chan struct{})
	}
	signal := func(i int) { closed[i].Do(func() { close(ready[i]) }) }
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		err := parallel(ctx, workers, len(specs), func(i int) error {
			defer signal(i)
			p := specs[i]
			parts := append(append([]*part(nil), partsOf(tapEffectBundle+p.Effect, false)...), partsOf(tapEffectBundle+p.DirectionalEffect(), false)...)
			var extra []*part
			if p.Habahiro {
				extra = append(extra, partsOf(tapEffectBundle+p.Effect, true)...)
			}
			r, err := assemble(p, parts, extra, store, tiles)
			if err != nil {
				return fmt.Errorf("%s: %w", p.Name(), err)
			}
			out[i] = r
			return nil
		})
		for i := range ready {
			signal(i)
		}
		done <- err
	}()
	for i := range specs {
		<-ready[i]
		if out[i] == nil {
			if err := <-done; err != nil {
				return err
			}
			return fmt.Errorf("%s: not built", specs[i].Name())
		}
		if err := emit(out[i]); err != nil {
			cancel()
			<-done
			return err
		}
		out[i] = nil
	}
	return <-done
}

// parallel runs f(0..n-1) on up to workers goroutines and returns the first
// error (cancelling the remaining calls).
func parallel(ctx context.Context, workers, n int, f func(int) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var once sync.Once
	var first error
	next := make(chan int)
	for w := 0; w < min(workers, max(n, 1)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				if err := f(i); err != nil {
					once.Do(func() { first = err; cancel() })
				}
			}
		}()
	}
loop:
	for i := 0; i < n; i++ {
		select {
		case next <- i:
		case <-ctx.Done():
			break loop
		}
	}
	close(next)
	wg.Wait()
	if first != nil {
		return first
	}
	return ctx.Err()
}

// tileCache keeps the sprite images (texture-sheet tiles, converted for
// alpha blending) shared by the items.
type tileCache struct {
	mu    sync.Mutex
	tiles map[spriteKey]*image.NRGBA
}

func (c *tileCache) get(k spriteKey, store *textureStore) (*image.NRGBA, error) {
	c.mu.Lock()
	img, ok := c.tiles[k]
	c.mu.Unlock()
	if ok {
		return img, nil
	}
	_, tex, err := store.get(k.bundle, k.texture)
	if err != nil {
		return nil, err
	}
	tw, th := tex.Rect.Dx()/k.tilesX, tex.Rect.Dy()/k.tilesY
	col, row := k.frame%k.tilesX, k.frame/k.tilesX
	tile, err := imgutil.Crop(tex, col*tw, row*th, tw, th)
	if err != nil {
		return nil, err
	}
	switch k.blend {
	case "additive":
		tile = imgutil.AdditiveToAlpha(tile)
	case "premultiplied":
		tile = unpremultiply(tile)
	}
	c.mu.Lock()
	c.tiles[k] = tile
	c.mu.Unlock()
	return tile, nil
}

// assemble builds one item from converted parts: the tap and directional
// effects, the standard aliases, then the HABAHIRO widths.
func assemble(spec master.Particle, parts, extra []*part, store *textureStore, tiles *tileCache) (*Result, error) {
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
				tile, err := tiles.get(p.sprite, store)
				if err != nil {
					return nil, err
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
		// mesh sprites can outgrow 2048²
		if packed, err = atlas.PackPOT(items, 4096, 2); err != nil {
			return nil, err
		}
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
	var canonErr error
	add := func(pt *part) {
		pt, err := canonicalPart(pt)
		if err != nil {
			if canonErr == nil {
				canonErr = err
			}
			return
		}
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
		meta = append(meta, m)
	}
	for _, pt := range parts {
		add(pt)
	}
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
		if src, ok := byName["effect_tap"]; ok {
			alias := src
			alias.Name = resource.ParticleEffectName(std)
			data.Effects = append(data.Effects, alias)
		}
	}
	for _, pt := range extra {
		add(pt)
	}
	if canonErr != nil {
		return nil, canonErr
	}

	texture, err := packed.EncodeTexture()
	if err != nil {
		return nil, err
	}
	thumb, err := imgutil.EncodePNG(imgutil.Scale(packed.Texture, 128, 128*packed.Texture.Rect.Dy()/packed.Texture.Rect.Dx()))
	if err != nil {
		return nil, err
	}
	setting := "normal"
	if spec.Light {
		setting = "light"
	}
	title := spec.Title
	desc := fmt.Sprintf("Tap effect %s + directional flick %s (%s), converted from the original Unity particle systems.", spec.Effect, spec.Directional, setting)
	if spec.Habahiro {
		desc = fmt.Sprintf("For #HABAHIRO levels: tap effect HABAHIRO (with width variants _2 … _7 for multi-width notes) + directional flick %s (%s), converted from the original Unity particle systems.", spec.Directional, setting)
	}
	name := spec.Name()
	return &Result{
		Item: database.DatabaseParticleItem{
			Name:        name,
			Version:     packout.ParticleVersion,
			Title:       packout.T(title, title, title),
			Subtitle:    packout.T("BanG Dream! Girls Band Party!", "BanG Dream! 少女乐团派对！", "バンドリ！ ガールズバンドパーティ！"),
			Author:      packout.T("GirlsBandParty", "GirlsBandParty", "GirlsBandParty"),
			Tags:        []database.DatabaseTag{},
			Description: packout.T(desc, desc, desc),
		},
		Habahiro:  spec.Habahiro,
		Data:      data,
		Texture:   texture,
		Thumbnail: thumb,
		Contract: Contract{
			Particle: name,
			Quad:     "spawn with the quad centred on the receiving button anchor, half extent g·quadScale (g = world units per particle unit = rate·noteSize·safeRatio); effect coordinates are original particle units (lane spacing 2.2)",
			Width:    "HABAHIRO width variants (effects with \"width\"): spawn like the base prefab at the note's effect anchor (centre lane for odd widths, the half-lane button between the two centre lanes for even widths)",
			Texture:  [2]int{packed.Texture.Rect.Dx(), packed.Texture.Rect.Dy()},
			Effects:  meta,
		},
	}, nil
}
