package particle

import (
	"context"
	"strings"
	"testing"
)

// Generate reports errors instead of exiting: offline without a cache every
// download fails.
func TestGenerateOfflineWithoutCache(t *testing.T) {
	_, err := Generate(context.Background(), Options{Source: Source{CacheDir: t.TempDir(), Offline: true}})
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("want an offline error, got %v", err)
	}
}
