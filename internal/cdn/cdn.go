// Package cdn downloads original asset bundles from the game's content CDNs
// at pinned asset versions, with a disk cache: the JP content CDN, and the
// mainland (CN) asset bundle CDN for the bundles of master rows that exist
// only in the CN master (Client.Servers).
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

// PinnedVersion is the JP asset version ("<data version>_<hash>") whose
// bundles the generator was verified against. 10.2.0.190 with the version
// hash of the 10.2.0 (231) client is the first version that has the Limited 5
// bundles (skin_collabo_2026); bundles new since 10.1.0.200 answer 403 under
// older versions.
const PinnedVersion = "10.2.0.190_b3da1643334f3d8433dbffe65574774e5743b2e72bcd44920f09efc8270aad9d"

// PinnedCNVersion is the mainland bundle version (the version of every
// AssetBundleInfo entry of the CN 10.1.3 client; mainland URLs have no hash).
const PinnedCNVersion = "10.1.3.0"

const (
	root   = "https://content.garupa.jp/Release/"
	rootCN = "https://line3-prod-patch-bd.bilibiligame.net/assetbundle/"
)

type Client struct {
	Version   string // JP asset version; empty: PinnedVersion
	CNVersion string // mainland bundle version; empty: PinnedCNVersion
	// Servers maps bundles to the server they come from ("cn"); the others
	// come from JP.
	Servers  map[string]string
	CacheDir string // cache root; bundles go to <CacheDir>/cdn/<version>/ (mainland: cdn/cn/<version>/)
	Offline  bool
	HTTP     *http.Client // nil: a client with a 300 s timeout
	Log      func(format string, args ...any)
	Ctx      context.Context // nil: context.Background()
}

// Bundle returns the raw UnityFS bytes of an Android asset bundle, e.g.
// "ingameskin/tapeffect/skin00".
func (c *Client) Bundle(name string) ([]byte, error) {
	return c.BundleOn(c.Server(name), name)
}

// Server is the server a bundle comes from ("jp" unless Servers says otherwise).
func (c *Client) Server(name string) string {
	if s := c.Servers[name]; s != "" {
		return s
	}
	return "jp"
}

// BundleOn returns a bundle from the given server's CDN, e.g. a dependency of
// a mainland bundle, which the mainland CDN serves under its own name.
func (c *Client) BundleOn(server, name string) ([]byte, error) {
	version, base, cacheDir := c.Version, root, "cdn"
	if version == "" {
		version = PinnedVersion
	}
	switch server {
	case "", "jp":
	case "cn":
		version, base, cacheDir = c.CNVersion, rootCN, filepath.Join("cdn", "cn")
		if version == "" {
			version = PinnedCNVersion
		}
	default:
		return nil, fmt.Errorf("asset bundle %s: no CDN for server %q", name, server)
	}
	var cachePath string
	if c.CacheDir != "" {
		cachePath = filepath.Join(c.CacheDir, cacheDir, version, filepath.FromSlash(name))
		if data, err := os.ReadFile(cachePath); err == nil {
			return data, nil
		}
	}
	if c.Offline {
		return nil, fmt.Errorf("offline and not cached: asset bundle %s", name)
	}
	u := base + version + "/Android/" + name
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
