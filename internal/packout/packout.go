// Package packout writes generator output as a sonolus-pack source tree.
package packout

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/WindowsSov8forUs/sonolus-core-go/codec"
	"github.com/WindowsSov8forUs/sonolus-core-go/core"
	"github.com/WindowsSov8forUs/sonolus-core-go/database"
)

// T returns an English / Simplified Chinese / Japanese localisation.
func T(en, zhs, ja string) database.LocalizationText {
	return database.LocalizationText{"en": core.Text(en), "zhs": core.Text(zhs), "ja": core.Text(ja)}
}

// Item is the common part of skin/effect/particle/background item.json.
type Item struct {
	Version     int                       `json:"version"`
	Title       database.LocalizationText `json:"title"`
	Subtitle    database.LocalizationText `json:"subtitle"`
	Author      database.LocalizationText `json:"author"`
	Tags        []database.DatabaseTag    `json:"tags"`
	Description database.LocalizationText `json:"description,omitempty"`
	Meta        any                       `json:"meta,omitempty"`
}

// Item versions expected by the current Sonolus schema.
const (
	SkinVersion       = 4
	BackgroundVersion = 2
	EffectVersion     = 5
	ParticleVersion   = 3
)

// Source is a sonolus-pack source directory.
type Source struct{ Root string }

// Init (re)creates the source root and writes info.json.
func Init(root string, title database.LocalizationText) (*Source, error) {
	if err := os.RemoveAll(root); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	s := &Source{Root: root}
	return s, s.WriteJSON("info.json", map[string]any{"title": title})
}

// ItemDir creates <kind>/<name>/ and writes item.json.
func (s *Source) ItemDir(kind, name string, item Item) (string, error) {
	if item.Tags == nil {
		item.Tags = []database.DatabaseTag{}
	}
	rel := filepath.Join(kind, name)
	if err := os.MkdirAll(filepath.Join(s.Root, rel), 0o755); err != nil {
		return "", err
	}
	return rel, s.WriteJSON(filepath.Join(rel, "item.json"), item)
}

func (s *Source) WriteJSON(rel string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return s.WriteFile(rel, data)
}

// WriteData stores a JSON resource gzip-compressed with the Sonolus codec,
// written without extension so sonolus-pack copies it verbatim.
func (s *Source) WriteData(rel string, v any) error {
	data, err := codec.Compress(v)
	if err != nil {
		return fmt.Errorf("compress %s: %w", rel, err)
	}
	return s.WriteFile(rel, data)
}

// WriteFiles writes resource files into an item directory.
func (s *Source) WriteFiles(rel string, files map[string][]byte) error {
	for name, data := range files {
		if err := s.WriteFile(filepath.Join(rel, name), data); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) WriteFile(rel string, data []byte) error {
	p := filepath.Join(s.Root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// IdentityTransform is the identity skin sprite transform.
func IdentityTransform() map[string]map[string]float64 {
	t := map[string]map[string]float64{}
	for _, k := range []string{"x1", "y1", "x2", "y2", "x3", "y3", "x4", "y4"} {
		t[k] = map[string]float64{k: 1}
	}
	return t
}
