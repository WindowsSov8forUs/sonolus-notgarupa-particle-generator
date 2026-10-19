// Package cdn downloads original asset bundles from the game's content CDN
// at a pinned asset version, with a disk cache.
package cdn

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// PinnedVersion is the asset version ("<data version>_<hash>") whose bundles
// the generator was verified against.
const PinnedVersion = "10.1.0.200_e019f5bbbae4e182041ed1731df6e08b7708aa3536aefaeaeec04b1951c4d048"

const root = "https://content.garupa.jp/Release/"

type Client struct {
	Version  string // empty: PinnedVersion
	CacheDir string // cache root; bundles go to <CacheDir>/cdn/<version>/
	Offline  bool
	HTTP     *http.Client // nil: a client with a 300 s timeout
	Log      func(format string, args ...any)
	Ctx      context.Context // nil: context.Background()
}

// Bundle returns the raw UnityFS bytes of an Android asset bundle, e.g.
// "ingameskin/tapeffect/skin00".
func (c *Client) Bundle(name string) ([]byte, error) {
	version := c.Version
	if version == "" {
		version = PinnedVersion
	}
	var cachePath string
	if c.CacheDir != "" {
		cachePath = filepath.Join(c.CacheDir, "cdn", version, filepath.FromSlash(name))
		if data, err := os.ReadFile(cachePath); err == nil {
			return data, nil
		}
	}
	if c.Offline {
		return nil, fmt.Errorf("offline and not cached: asset bundle %s", name)
	}
	u := root + version + "/Android/" + name
	if c.Log != nil {
		c.Log("GET %s", u)
	}
	ctx := c.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 300 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", u, resp.StatusCode)
	}
	if cachePath != "" {
		writeCache(cachePath, data)
	}
	return data, nil
}

// writeCache stores a download atomically so that concurrent generators
// sharing a cache directory never read a partial file.
func writeCache(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}
