package particle

// Particle textures. A texture is identified by the SHA-256 of its RGBA
// pixels, so equal textures of different bundles (the tap-effect, game-clear
// and HABAHIRO copies of Tex_parSet_1, …) share their sprites. Tap-effect
// textures are decoded from the original asset bundles.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"

	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/imgutil"
)

type textureStore struct {
	images map[string]*image.NRGBA // content id → image
	ids    map[string]string       // bundle:texture → content id
	// Per-bundle texture sources, by material texture name.
	sources map[string]textureSource
}

func newTextureStore() *textureStore {
	return &textureStore{images: map[string]*image.NRGBA{}, ids: map[string]string{}, sources: map[string]textureSource{}}
}

// addBundle registers the texture source of a bundle key.
func (t *textureStore) addBundle(key string, src textureSource) { t.sources[key] = src }

// addGameClear registers the game-clear textures (asset key → PNG file).
func (t *textureStore) addGameClear(files map[string]string) {
	t.sources["gameclear"] = func(name string) (*image.NRGBA, error) {
		f, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("game-clear texture %s not in the profile assets", name)
		}
		data, err := gameClearAsset(f)
		if err != nil {
			return nil, err
		}
		return imgutil.Decode(data)
	}
}

// get returns the content id and image of a bundle's texture.
func (t *textureStore) get(bundle, name string) (string, *image.NRGBA, error) {
	ref := bundle + ":" + name
	if id, ok := t.ids[ref]; ok {
		return id, t.images[id], nil
	}
	src, ok := t.sources[bundle]
	if !ok {
		return "", nil, fmt.Errorf("no texture source for bundle %s", bundle)
	}
	img, err := src(name)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", ref, err)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%dx%d:", img.Rect.Dx(), img.Rect.Dy())
	h.Write(img.Pix)
	id := hex.EncodeToString(h.Sum(nil))
	if _, ok := t.images[id]; !ok {
		t.images[id] = img
	}
	t.ids[ref] = id
	return id, t.images[id], nil
}
