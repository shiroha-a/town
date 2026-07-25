// Package condition centralizes the server-authoritative derivations of a
// player's physical condition: BMI and body type (design 17.3), disease name
// from the disease index (17.4), the overall condition label (17.6), the disease
// index drift delta, and hospital treatment fees. Keeping these as pure
// functions in one leaf package lets the player read path, the worker drift job,
// and the work action all agree on the same rules.
package condition

// Condition labels, from best to worst (design 17.6).
const (
	LabelBest   = "最高"
	LabelGood   = "良好"
	LabelNormal = "普通"
	LabelPoor   = "不良"
	LabelBad    = "悪い"
	LabelWorst  = "最悪"
)

// BMI returns the integer BMI (floor), matching the legacy check_BMI.
func BMI(heightCm, weightG int) int {
	if heightCm <= 0 {
		return 0
	}
	h := float64(heightCm) / 100.0
	return int(float64(weightG) / 1000.0 / (h * h))
}

// BodyType returns the Japanese body-shape label for a BMI (design 17.3).
func BodyType(bmi int) string {
	switch {
	case bmi >= 26:
		return "肥満"
	case bmi >= 24:
		return "やや太り気味"
	case bmi >= 20:
		return "標準"
	case bmi >= 18:
		return "やせ気味"
	default:
		return "やせすぎ"
	}
}

// bmiMult is the condition multiplier for a body type (design 17.3).
func bmiMult(bmi int) float64 {
	switch {
	case bmi >= 26: // 肥満
		return 0.8
	case bmi >= 24: // やや太り気味
		return 0.95
	case bmi >= 20: // 標準
		return 1.0
	case bmi >= 18: // やせ気味
		return 0.95
	default: // やせすぎ
		return 0.8
	}
}

// satietyMult maps the 満腹度(0-100) to a condition multiplier. The exact
// threshold alignment against the legacy 7-tier hunger scale is provisional
// (design 17.9); this uses the rewrite's 5-tier satietyLabel boundaries.
func satietyMult(satiety int) float64 {
	switch {
	case satiety >= 80: // 満腹
		return 0.8
	case satiety >= 60: // 丁度いい
		return 1.0
	case satiety >= 40: // やや空腹
		return 0.9
	case satiety >= 15: // 空腹
		return 0.7
	default: // ペコペコ
		return 0.6
	}
}

// DiseaseName maps a disease index to its Japanese disease name (design 17.4).
// A non-negative index is healthy (empty string). Boundaries are exclusive on
// the low side: -10 is still 風邪ぎみ, -100 is still 脳腫瘍.
func DiseaseName(index int) string {
	switch {
	case index >= 0:
		return ""
	case index >= -10:
		return "風邪ぎみ"
	case index >= -15:
		return "風邪"
	case index >= -20:
		return "下痢"
	case index >= -40:
		return "肺炎"
	case index >= -70:
		return "結核"
	case index >= -100:
		return "脳腫瘍"
	default:
		return "癌"
	}
}

// DiseaseDelta is the per-evaluation change applied to the disease index for a
// given base condition label (design 17.4).
func DiseaseDelta(label string) int {
	switch label {
	case LabelBest:
		return 2
	case LabelGood:
		return 1
	case LabelNormal:
		return 0
	case LabelPoor:
		return 1
	case LabelBad:
		return -3
	default: // 最悪
		return 0
	}
}

// TreatmentFee is the hospital fee to cure a disease. A healthy player can buy a
// preventive 「元気」 shot at the default fee (design 17.4).
func TreatmentFee(diseaseName string) int64 {
	switch diseaseName {
	case "風邪ぎみ":
		return 18000
	case "風邪":
		return 28000
	case "下痢":
		return 32000
	case "肺炎":
		return 35000
	case "結核":
		return 48000
	case "脳腫瘍":
		return 64000
	case "癌":
		return 88000
	default: // 健康時の「元気」注射(予防)
		return 10000
	}
}

// Input is the raw player state needed to derive the overall condition.
type Input struct {
	Energy, EnergyMax       int
	NouEnergy, NouEnergyMax int
	Kenkou                  int
	Satiety                 int
	BMI                     int
	DiseaseIndex            int
}

// Result is the derived condition (design 17.6).
type Result struct {
	Sisuu       float64 // 指数
	Label       string  // 基礎コンディション(病気を除いた体調)
	DiseaseName string  // 病名(健康なら空)
	Display     string  // 表示用: 病名があれば病名、なければLabel
}

// Compute derives the condition from player state (design 17.6). The disease
// index only affects the display label and the disease name; the base label
// (used for the disease drift) is computed from energy/health/satiety/BMI so the
// drift does not feed back on itself.
func Compute(in Input) Result {
	enerPct := 0.0
	if in.EnergyMax > 0 {
		enerPct = float64(in.Energy) / float64(in.EnergyMax) * 100
	}
	nouPct := 0.0
	if in.NouEnergyMax > 0 {
		nouPct = float64(in.NouEnergy) / float64(in.NouEnergyMax) * 100
	}
	sisuu := (enerPct+nouPct)/2 + float64(in.Kenkou)/100.0
	sisuu *= satietyMult(in.Satiety)
	sisuu *= bmiMult(in.BMI)

	label := labelOf(sisuu)
	name := DiseaseName(in.DiseaseIndex)
	display := label
	if name != "" {
		display = name
	}
	return Result{Sisuu: sisuu, Label: label, DiseaseName: name, Display: display}
}

// WorkExpBase returns the base work experience for a condition and whether the
// player can work at all (design 17.5 step 3). Severe diseases (結核/脳腫瘍/癌)
// and the 最悪 condition block working entirely. When sick with a milder disease
// the disease drives the (negative) base; otherwise the base condition label
// does.
func WorkExpBase(r Result) (base int, canWork bool) {
	if r.Label == LabelWorst {
		return 0, false
	}
	switch r.DiseaseName {
	case "結核", "脳腫瘍", "癌":
		return 0, false
	case "風邪ぎみ":
		return -8, true
	case "風邪":
		return -14, true
	case "下痢":
		return -17, true
	case "肺炎":
		return -20, true
	}
	switch r.Label {
	case LabelBest:
		return 15, true
	case LabelGood:
		return 10, true
	case LabelNormal:
		return 5, true
	case LabelPoor:
		return 1, true
	case LabelBad:
		return -5, true
	}
	return 0, false
}

// labelOf maps the condition 指数 to a label (design 17.6).
func labelOf(sisuu float64) string {
	switch {
	case sisuu > 98:
		return LabelBest
	case sisuu > 75:
		return LabelGood
	case sisuu > 50:
		return LabelNormal
	case sisuu > 30:
		return LabelPoor
	case sisuu > 10:
		return LabelBad
	default:
		return LabelWorst
	}
}
