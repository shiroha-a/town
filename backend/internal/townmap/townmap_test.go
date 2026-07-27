package townmap

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		fs      []Facility
		wantErr bool
	}{
		{"default is valid", Default(), false},
		{"empty is valid", []Facility{}, false},
		{
			"missing key",
			[]Facility{{Img: "bank", Col: 1, Row: 0}},
			true,
		},
		{
			"missing img",
			[]Facility{{Key: "bank", Col: 1, Row: 0}},
			true,
		},
		{
			"col below range",
			[]Facility{{Key: "bank", Img: "bank", Col: 0, Row: 0}},
			true,
		},
		{
			"col above range",
			[]Facility{{Key: "bank", Img: "bank", Col: Cols + 1, Row: 0}},
			true,
		},
		{
			"row below range",
			[]Facility{{Key: "bank", Img: "bank", Col: 1, Row: -1}},
			true,
		},
		{
			"row above range",
			[]Facility{{Key: "bank", Img: "bank", Col: 1, Row: Rows}},
			true,
		},
		{
			"duplicate cell",
			[]Facility{
				{Key: "bank", Img: "bank", Col: 3, Row: 2},
				{Key: "gym", Img: "gym", Col: 3, Row: 2},
			},
			true,
		},
		{
			"distinct cells ok",
			[]Facility{
				{Key: "bank", Img: "bank", Col: 3, Row: 2},
				{Key: "gym", Img: "gym", Col: 4, Row: 2},
			},
			false,
		},
		{
			"same cell different town ok",
			[]Facility{
				{Key: "bank", Img: "bank", Town: 0, Col: 3, Row: 2},
				{Key: "gym", Img: "gym", Town: 1, Col: 3, Row: 2},
			},
			false,
		},
		{
			"same cell same town duplicate",
			[]Facility{
				{Key: "bank", Img: "bank", Town: 2, Col: 3, Row: 2},
				{Key: "gym", Img: "gym", Town: 2, Col: 3, Row: 2},
			},
			true,
		},
		{
			"town above range",
			[]Facility{{Key: "bank", Img: "bank", Town: MaxTowns, Col: 1, Row: 0}},
			true,
		},
		{
			"town below range",
			[]Facility{{Key: "bank", Img: "bank", Town: -1, Col: 1, Row: 0}},
			true,
		},
		{
			"dest above range",
			[]Facility{{Key: "walk", Img: "mati_link", Col: 1, Row: 0, Dest: MaxTowns}},
			true,
		},
		{
			"move facility with valid dest ok",
			[]Facility{{Key: "walk", Img: "mati_link", Town: 0, Col: 1, Row: 0, Dest: 2}},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.fs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAssets(t *testing.T) {
	tests := []struct {
		name    string
		as      []Asset
		wantErr bool
	}{
		{"empty is valid", []Asset{}, false},
		{"missing img", []Asset{{Col: 1, Row: 0}}, true},
		{"col below range", []Asset{{Img: "kusa", Col: 0, Row: 0}}, true},
		{"col above range", []Asset{{Img: "kusa", Col: Cols + 1, Row: 0}}, true},
		{"row below range", []Asset{{Img: "kusa", Col: 1, Row: -1}}, true},
		{"row above range", []Asset{{Img: "kusa", Col: 1, Row: Rows}}, true},
		{
			"stacked layers within cap ok",
			[]Asset{
				{Img: "kusa", Col: 3, Row: 2},
				{Img: "tree1", Col: 3, Row: 2},
				{Img: "tree2", Col: 3, Row: 2},
			},
			false,
		},
		{
			"stacked layers exceed cap",
			[]Asset{
				{Img: "kusa", Col: 3, Row: 2},
				{Img: "tree1", Col: 3, Row: 2},
				{Img: "tree2", Col: 3, Row: 2},
				{Img: "tree3", Col: 3, Row: 2},
			},
			true,
		},
		{
			"distinct cells ok",
			[]Asset{
				{Img: "kusa", Col: 3, Row: 2},
				{Img: "umi", Col: 4, Row: 2},
			},
			false,
		},
		{
			"same cell different town ok",
			[]Asset{
				{Img: "kusa", Town: 0, Col: 3, Row: 2},
				{Img: "umi", Town: 1, Col: 3, Row: 2},
			},
			false,
		},
		{
			"town out of range",
			[]Asset{{Img: "kusa", Town: MaxTowns, Col: 1, Row: 0}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAssets(tt.as)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateAssets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestDefaultMap guards the embedded initial layout (default_map.json). 管理画面で
// 組んだ盤面を書き出したものなので、書き出しミスやマスの重なりが混ざると新規
// インストールの街が壊れる。埋め込みJSONなのでビルドでは気付けない。
func TestDefaultMap(t *testing.T) {
	facilities := Default()
	assets := DefaultAssets()

	if err := ValidateAssets(assets); err != nil {
		t.Fatalf("既定マップの背景が不正: %v", err)
	}
	if len(assets) == 0 {
		t.Fatal("既定マップに背景アセットが無い")
	}

	// 公園(0)とシー・リゾート(1)の2つだけが入っていること。
	byTown := map[int]int{}
	for _, f := range facilities {
		byTown[f.Town]++
	}
	for _, town := range []int{0, 1} {
		if byTown[town] == 0 {
			t.Errorf("街%dに施設が無い", town)
		}
	}
	for town := range byTown {
		if town != 0 && town != 1 {
			t.Errorf("街%dの施設が混ざっている(既定は街0と1だけ)", town)
		}
	}

	// 遊べる状態か。カジノは既定マップから漏れていた実績があるので名指しで見る。
	keys := map[string]bool{}
	for _, f := range facilities {
		keys[f.Key] = true
	}
	for _, key := range []string{
		"bank", "depart", "syokudou", "hanbai", "gym", "onsen", "school", "kyushitu",
		"hospital", "yakuba", "jobchange", "kabu", "keiba", "kentiku", "tsuri",
		"gifutoya", "tokuten", "bingo", "prof", "casino", "akichi",
	} {
		if !keys[key] {
			t.Errorf("既定マップに施設 %q が無い", key)
		}
	}

	// 街0と街1を行き来できること。
	moveFrom := map[int]bool{}
	for _, f := range facilities {
		if f.Key == "walk" || f.Key == "bus" {
			moveFrom[f.Town] = true
		}
	}
	for _, town := range []int{0, 1} {
		if !moveFrom[town] {
			t.Errorf("街%dに移動施設(徒歩/バス)が無い", town)
		}
	}

	// 返す配列は毎回コピーで、呼び出し側の書き換えが既定値に漏れないこと。
	facilities[0].Key = "broken"
	if Default()[0].Key == "broken" {
		t.Error("Default()が内部配列を共有している")
	}
	assets[0].Img = "broken"
	if DefaultAssets()[0].Img == "broken" {
		t.Error("DefaultAssets()が内部配列を共有している")
	}
}
