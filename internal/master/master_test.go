package master

import "testing"

func TestCounts(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	skins, particles, effects, backgrounds := m.Skins(), m.Particles(), m.Effects(), m.Backgrounds()
	t.Logf("skins %d, particles %d, effects %d, backgrounds %d", len(skins), len(particles), len(effects), len(backgrounds))
	names := map[string]bool{}
	for _, s := range skins {
		if names[s.Name()] {
			t.Errorf("duplicate skin %s", s.Name())
		}
		names[s.Name()] = true
	}
	if skins[0].Name() != "notgarupa-skin00-skin00-skin00" {
		t.Errorf("first skin %s", skins[0].Name())
	}
	if particles[0].Name() != "notgarupa-skin00-skin00normal" {
		t.Errorf("first particle %s", particles[0].Name())
	}
	if len(m.Specials()) != len(m.Special)-1 {
		t.Errorf("specials %d", len(m.Specials()))
	}
}
