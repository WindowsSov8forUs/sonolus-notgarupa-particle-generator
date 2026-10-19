package particle

import (
	"context"
	"strings"
	"testing"
)

// Generate reports errors instead of exiting: offline without a cache every
// download fails.
func TestGenerateOfflineWithoutCache(t *testing.T) {
	err := Generate(context.Background(), Options{Source: Source{CacheDir: t.TempDir(), Offline: true}}, func(*Result) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("want an offline error, got %v", err)
	}
}

func TestItems(t *testing.T) {
	names, err := Items()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 154 || names[0] != "notgarupa-skin00-skin00normal" {
		t.Fatalf("items %d, first %s", len(names), names[0])
	}
}
