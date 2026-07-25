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
