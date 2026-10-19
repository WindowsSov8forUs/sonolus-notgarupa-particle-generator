package particle

// Assets built into the original game's APK (not available as downloadable
// asset bundles), shipped with the generator: the game-clear prefab graph
// (Full Combo / All Perfect / clear fireworks) and its textures.

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
)

//go:embed assets/apk
var apkAssets embed.FS

// readGameClearProfile returns the game-clear prefab profile (JSON).
func readGameClearProfile() ([]byte, error) {
	data, err := apkAssets.ReadFile("assets/apk/gameclear/game-clear-profile.json.gz")
	if err != nil {
		return nil, err
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// gameClearAsset returns a file of the game-clear prefab (a texture PNG).
func gameClearAsset(name string) ([]byte, error) {
	return apkAssets.ReadFile("assets/apk/gameclear/" + name)
}
