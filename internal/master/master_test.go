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
	if skins[0].Name() != "garupa-skin00-skin00-skin00" || skins[0].Title["ja"] != "TYPE1 / TYPE1 / レーンスキン00" {
		t.Errorf("first skin %s %q", skins[0].Name(), skins[0].Title)
	}
	if particles[0].Name() != "garupa-skin00-skin00" || particles[0].Title["ja"] != "エフェクトスキン00 / TYPE1" {
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
			if s.Title["ja"] != title {
				t.Errorf("%s: title %q, want %q", s.Name(), s.Title, title)
			}
			delete(want, s.Name())
		}
	}
	for name := range want {
		t.Errorf("missing skin %s", name)
	}
	// each locale shows its own server's names (ja JP, en EN, zht TW, zhs CN)
	for _, s := range skins {
		if s.Name() == "garupa-skin00-skin00-skin08" {
			want := Text{"ja": "TYPE1 / TYPE1 / Roseliaのレーン", "en": "TYPE1 / TYPE1 / Roselia Lane",
				"zht": "TYPE1 / TYPE1 / Roselia的軌跡", "zhs": "TYPE1 / TYPE1 / Roselia的按键条皮肤"}
			for l, w := range want {
				if s.Title[l] != w {
					t.Errorf("%s %s: %q, want %q", s.Name(), l, s.Title[l], w)
				}
			}
		}
	}
	if len(m.Specials()) != len(m.Special)-1 {
		t.Errorf("specials %d", len(m.Specials()))
	}
}

func TestServerRows(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Limited 5 (JP) and the mainland-only rows are items.
	want := map[string]string{
		"garupa-skin_collabo_2026-skin00": "魔法少女ノ魔女裁判コラボ / TYPE1",
		"garupa-skin_bilicat":             "小豆泥联动演出皮肤①",
		"garupa-skin_bilicat01":           "小豆泥联动演出皮肤②",
		"garupa-skin10001":                "彩虹节拍",
		"garupa-habahiro-skin10001":       "HABAHIRO / 彩虹节拍",
	}
	for _, s := range m.Skins() {
		if title, ok := want[s.Name()]; ok {
			if s.Title["ja"] != title || s.Title["zhs"] != title {
				t.Errorf("%s: title %q, want %q", s.Name(), s.Title, title)
			}
			delete(want, s.Name())
		}
	}
	for name := range want {
		t.Errorf("missing skin %s", name)
	}
	servers := m.BundleServers()
	for _, b := range []string{"ingameskin/tapeffect/skin_bilicat", "ingameskin/tapeffect/directionalflickskin10001light",
		"ingameskin/noteskin/directionalflickskin_bilicat01", "sound/tapseskin/skin10001", "ingameskin/bgskin/skin10001"} {
		if servers[b] != "cn" {
			t.Errorf("%s: server %q, want cn", b, servers[b])
		}
	}
	for _, b := range []string{"ingameskin/noteskin/skin_collabo_2026", "ingameskin/tapeffect/skin00", "sound/tapseskin/skin_bilicat"} {
		if s, ok := servers[b]; ok {
			t.Errorf("%s: server %q, want jp", b, s)
		}
	}
}
