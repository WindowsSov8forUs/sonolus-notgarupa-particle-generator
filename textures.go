package particle

// Particle textures. A texture is identified by the SHA-256 of its RGBA
// pixels, so equal textures of different bundles (the tap-effect and
// HABAHIRO copies of Tex_parSet_1, …) share their sprites. Tap-effect
// textures are decoded from the original asset bundles.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"sync"
)

type textureStore struct {
	mu     sync.Mutex
	images map[string]*image.NRGBA // content id → image
	ids    map[string]string       // bundle:texture → content id
	// Per-bundle texture sources, by material texture name.
	sources map[string]textureSource
}

func newTextureStore() *textureStore {
	return &textureStore{images: map[string]*image.NRGBA{}, ids: map[string]string{}, sources: map[string]textureSource{}}
}

// addBundle registers the texture source of a bundle key; the bundle's
// rasterised mesh sprites ("raster:…") render from its textures.
func (t *textureStore) addBundle(key string, src textureSource) {
	t.mu.Lock()
	defer t.mu.Unlock()
	raster := rasterSource(t, key)
	t.sources[key] = func(name string) (*image.NRGBA, error) {
		if isRasterName(name) {
			return raster(name)
		}
		return src(name)
	}
}

// get returns the content id and image of a bundle's texture.
func (t *textureStore) get(bundle, name string) (string, *image.NRGBA, error) {
	ref := bundle + ":" + name
	t.mu.Lock()
	if id, ok := t.ids[ref]; ok {
		img := t.images[id]
		t.mu.Unlock()
		return id, img, nil
	}
	src, ok := t.sources[bundle]
	t.mu.Unlock()
	if !ok {
		return "", nil, fmt.Errorf("no texture source for bundle %s", bundle)
	}
	// decode outside the lock (raster sources read other textures)
	img, err := src(name)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", ref, err)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%dx%d:", img.Rect.Dx(), img.Rect.Dy())
	h.Write(img.Pix)
	id := hex.EncodeToString(h.Sum(nil))
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.images[id]; !ok {
		t.images[id] = img
	}
	t.ids[ref] = id
	return id, t.images[id], nil
}
