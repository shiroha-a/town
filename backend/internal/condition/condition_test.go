package condition

import "testing"

func TestDiseaseNameBoundaries(t *testing.T) {
	cases := []struct {
		index int
		want  string
	}{
		{50, ""},
		{0, ""},
		{-1, "風邪ぎみ"},
		{-10, "風邪ぎみ"}, // -10ちょうどは風邪ぎみ
		{-11, "風邪"},
		{-15, "風邪"},
		{-16, "下痢"},
		{-20, "下痢"},
		{-21, "肺炎"},
		{-40, "肺炎"},
		{-41, "結核"},
		{-70, "結核"},
		{-71, "脳腫瘍"},
		{-100, "脳腫瘍"}, // -100ちょうどは脳腫瘍
		{-101, "癌"},
	}
	for _, c := range cases {
		if got := DiseaseName(c.index); got != c.want {
			t.Errorf("DiseaseName(%d) = %q, want %q", c.index, got, c.want)
		}
	}
}

func TestBMIAndBodyType(t *testing.T) {
	// 171cm 51kg -> 51/1.71^2 = 17.44 -> 17 (やせすぎ)
	if got := BMI(171, 51000); got != 17 {
		t.Errorf("BMI(171,51000) = %d, want 17", got)
	}
	cases := []struct {
		bmi  int
		want string
	}{
		{26, "肥満"}, {25, "やや太り気味"}, {24, "やや太り気味"},
		{23, "標準"}, {20, "標準"}, {19, "やせ気味"}, {18, "やせ気味"}, {17, "やせすぎ"},
	}
	for _, c := range cases {
		if got := BodyType(c.bmi); got != c.want {
			t.Errorf("BodyType(%d) = %q, want %q", c.bmi, got, c.want)
		}
	}
}

func TestDiseaseDelta(t *testing.T) {
	cases := map[string]int{
		LabelBest: 2, LabelGood: 1, LabelNormal: 0,
		LabelPoor: 1, LabelBad: -3, LabelWorst: 0,
	}
	for label, want := range cases {
		if got := DiseaseDelta(label); got != want {
			t.Errorf("DiseaseDelta(%q) = %d, want %d", label, got, want)
		}
	}
}

func TestTreatmentFee(t *testing.T) {
	cases := map[string]int64{
		"風邪ぎみ": 18000, "風邪": 28000, "下痢": 32000, "肺炎": 35000,
		"結核": 48000, "脳腫瘍": 64000, "癌": 88000, "": 10000,
	}
	for name, want := range cases {
		if got := TreatmentFee(name); got != want {
			t.Errorf("TreatmentFee(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestComputeLabelAndDiseaseOverride(t *testing.T) {
	// フルパワー・丁度いい満腹・標準体型なら最高(sisuu ~= 100)。
	full := Compute(Input{
		Energy: 10, EnergyMax: 10, NouEnergy: 10, NouEnergyMax: 10,
		Kenkou: 5, Satiety: 70, BMI: 22, DiseaseIndex: 50,
	})
	if full.Label != LabelBest {
		t.Errorf("full condition label = %q, want %q (sisuu=%.2f)", full.Label, LabelBest, full.Sisuu)
	}
	if full.DiseaseName != "" || full.Display != LabelBest {
		t.Errorf("healthy display = %q / disease = %q, want 最高 / empty", full.Display, full.DiseaseName)
	}

	// 病名があれば表示は病名で上書き。ただし基礎ラベルは体調から決まる(driftはこちらを使う)。
	sick := Compute(Input{
		Energy: 10, EnergyMax: 10, NouEnergy: 10, NouEnergyMax: 10,
		Kenkou: 5, Satiety: 70, BMI: 22, DiseaseIndex: -12,
	})
	if sick.DiseaseName != "風邪" {
		t.Errorf("sick disease = %q, want 風邪", sick.DiseaseName)
	}
	if sick.Display != "風邪" {
		t.Errorf("sick display = %q, want 風邪", sick.Display)
	}
	if sick.Label != LabelBest {
		t.Errorf("sick base label = %q, want %q (病気は基礎ラベルに影響しない)", sick.Label, LabelBest)
	}
}

func TestWorkExpBase(t *testing.T) {
	cases := []struct {
		name     string
		r        Result
		wantBase int
		wantWork bool
	}{
		{"最高", Result{Label: LabelBest}, 15, true},
		{"良好", Result{Label: LabelGood}, 10, true},
		{"普通", Result{Label: LabelNormal}, 5, true},
		{"不良", Result{Label: LabelPoor}, 1, true},
		{"悪い", Result{Label: LabelBad}, -5, true},
		{"最悪は就労不可", Result{Label: LabelWorst}, 0, false},
		{"風邪ぎみ", Result{Label: LabelBest, DiseaseName: "風邪ぎみ"}, -8, true},
		{"肺炎", Result{Label: LabelGood, DiseaseName: "肺炎"}, -20, true},
		{"重病(結核)は就労不可", Result{Label: LabelGood, DiseaseName: "結核"}, 0, false},
		{"重病(癌)は就労不可", Result{Label: LabelBest, DiseaseName: "癌"}, 0, false},
	}
	for _, c := range cases {
		base, canWork := WorkExpBase(c.r)
		if base != c.wantBase || canWork != c.wantWork {
			t.Errorf("%s: WorkExpBase = (%d, %v), want (%d, %v)", c.name, base, canWork, c.wantBase, c.wantWork)
		}
	}
}

func TestComputeWorstWhenDepleted(t *testing.T) {
	// パワー0・空腹極限なら最悪。
	worst := Compute(Input{
		Energy: 0, EnergyMax: 10, NouEnergy: 0, NouEnergyMax: 10,
		Kenkou: 0, Satiety: 0, BMI: 17, DiseaseIndex: 50,
	})
	if worst.Label != LabelWorst {
		t.Errorf("depleted label = %q, want %q (sisuu=%.2f)", worst.Label, LabelWorst, worst.Sisuu)
	}
}
