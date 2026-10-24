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
	names = map[string]bool{} // item types have separate name spaces
	for _, p := range particles {
		if names[p.Name()] {
			t.Errorf("duplicate particle %s", p.Name())
		}
		names[p.Name()] = true
	}
	if skins[0].Name() != "garupa-skin00-skin00-skin00" || skins[0].Title != "TYPE1 / TYPE1 / レーンスキン00" {
		t.Errorf("first skin %s %q", skins[0].Name(), skins[0].Title)
	}
	if particles[0].Name() != "garupa-skin00-skin00" || particles[0].Title != "エフェクトスキン00 / TYPE1" {
		t.Errorf("first particle %s %q", particles[0].Name(), particles[0].Title)
	}
	// a fixed combination is written once, in the name and in the title
	want := map[string]string{
		"garupa-collabo23_summer_g-skin00": "collabo23_summer_g / TYPE1",
		"garupa-skin_persona":              "ファントム復刻スキン",
		"garupa-skin_april2021-skin00":     "エイプリルフール2021 / TYPE1",
		"garupa-habahiro-skin00":           "HABAHIRO / TYPE1",
	}
	for _, s := range skins {
		if title, ok := want[s.Name()]; ok {
			if s.Title != title {
				t.Errorf("%s: title %q, want %q", s.Name(), s.Title, title)
			}
			delete(want, s.Name())
		}
	}
	for name := range want {
		t.Errorf("missing skin %s", name)
	}
	if len(m.Specials()) != len(m.Special)-1 {
		t.Errorf("specials %d", len(m.Specials()))
	}
}
