package unity

import (
	"crypto/sha256"
	"os"
	"testing"
)

// TestBundle parses a real bundle given by UNITY_TEST_BUNDLE (skipped
// otherwise): every object must deserialise through its type tree.
func TestBundle(t *testing.T) {
	path := os.Getenv("UNITY_TEST_BUNDLE")
	if path == "" {
		t.Skip("UNITY_TEST_BUNDLE not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	files, err := ReadBundle(data)
	if err != nil {
		t.Fatal(err)
	}
	for name, sf := range files {
		if len(name) > 5 && name[len(name)-5:] == ".resS" {
			continue
		}
		f, err := Parse(sf)
		if err != nil {
			t.Fatal(err)
		}
		classes := map[int32]int{}
		for _, id := range f.Order {
			if _, err := f.Read(id); err != nil {
				t.Fatal(err)
			}
			classes[f.Objects[id].ClassID]++
		}
		t.Logf("%s: %d objects %v externals %v", name, len(f.Order), classes, f.Externals)
	}
}

// TestTextures decodes every Texture2D of UNITY_TEST_BUNDLE and logs the
// RGBA SHA-256 of each (top-down, like a PNG export).
func TestTextures(t *testing.T) {
	path := os.Getenv("UNITY_TEST_BUNDLE")
	if path == "" {
		t.Skip("UNITY_TEST_BUNDLE not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	files, err := ReadBundle(data)
	if err != nil {
		t.Fatal(err)
	}
	for name, sf := range files {
		if len(name) > 5 && name[len(name)-5:] == ".resS" {
			continue
		}
		f, err := Parse(sf)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range f.Order {
			if f.Objects[id].ClassID != textureClassID {
				continue
			}
			img, err := DecodeTexture(f, files, id)
			if err != nil {
				t.Fatal(err)
			}
			o, _ := f.Read(id)
			t.Logf("%s %v %X", o["m_Name"], img.Rect.Size(), sha256.Sum256(img.Pix))
		}
	}
}
