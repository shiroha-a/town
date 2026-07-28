// Package effects implements the data-driven effect/condition schema. Items,
// jobs and events store their behavior as JSON that is parsed, validated and
// evaluated here — entirely server-side, with no arbitrary code execution.
// Admin-authored content is untrusted input: unknown ops/predicates/params are
// rejected at parse time.
package effects

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SchemaVersion is the current effect/condition schema version.
const SchemaVersion = 1

// AllParams lists every parameter an effect/condition may reference. These map
// 1:1 to player_status columns. Note: satiety(空腹値) is a status value, not a
// displayed "parameter", but effects (食事) target it, so it is listed here.
var AllParams = []string{
	"energy", "nou_energy", "satiety",
	"kokugo", "suugaku", "rika", "syakai", "eigo", "ongaku", "bijutsu",
	"looks", "tairyoku", "kenkou", "speed", "power", "wanryoku", "kyakuryoku",
	"love", "omoshirosa",
}

// AllDiseases lists the disease names an add_disease op may target. These match
// condition.DiseaseName's output; 風邪ぎみ is covered by 風邪 (substring match).
var AllDiseases = []string{"風邪", "下痢", "肺炎", "結核", "脳腫瘍", "癌"}

var knownDiseases = func() map[string]bool {
	m := make(map[string]bool, len(AllDiseases))
	for _, d := range AllDiseases {
		m[d] = true
	}
	return m
}()

// knownParams are the parameters an effect may touch (mapped to status columns).
var knownParams = func() map[string]bool {
	m := make(map[string]bool, len(AllParams))
	for _, p := range AllParams {
		m[p] = true
	}
	return m
}()

// Op is a single effect operation.
type Op struct {
	Kind   string
	Param  string
	Amount int64
	// Disease restricts an add_disease op to players whose current disease name
	// contains this text (legacy basic0.cgi: 風邪薬 only helps 風邪 etc).
	// Empty means unconditional (legacy 万能).
	Disease string
	// Random draws the applied amount from [0, Amount] instead of using Amount
	// as-is (legacy basic0.cgi の商品目「ランダム品」= int(rand($v + 1)))。
	// Amountが負なら [Amount, 0]。add_param のみ。
	Random bool
}

// Effect is an ordered list of operations applied atomically.
type Effect struct {
	Ops []Op
}

type opJSON struct {
	Op      string `json:"op"`
	Param   string `json:"param,omitempty"`
	Amount  int64  `json:"amount"`
	Disease string `json:"disease,omitempty"`
	Random  bool   `json:"random,omitempty"`
}

// ParseEffect parses and validates effect JSON.
func ParseEffect(data []byte) (Effect, error) {
	var raw []opJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return Effect{}, fmt.Errorf("effect json: %w", err)
	}
	eff := Effect{Ops: make([]Op, 0, len(raw))}
	for i, r := range raw {
		// ランダムはパラメータ上昇にだけ許す(レガシーの「ランダム品」もパラメータ
		// だけを振っていた)。お金や体重で通すと蛇口の見積もりが立たなくなる。
		if r.Random && r.Op != "add_param" {
			return Effect{}, fmt.Errorf("effect[%d]: random is only for add_param", i)
		}
		switch r.Op {
		case "add_money":
			eff.Ops = append(eff.Ops, Op{Kind: r.Op, Amount: r.Amount})
		case "add_param":
			if !knownParams[r.Param] {
				return Effect{}, fmt.Errorf("effect[%d]: unknown param %q", i, r.Param)
			}
			eff.Ops = append(eff.Ops, Op{Kind: r.Op, Param: r.Param, Amount: r.Amount, Random: r.Random})
		case "add_weight_g", "add_height_cm":
			// 体重(g)/身長(cm)。レガシー basic0.cgi の ウエイトアップ/ダイエット/身長/縮み。
			eff.Ops = append(eff.Ops, Op{Kind: r.Op, Amount: r.Amount})
		case "add_disease":
			// 病気指数の回復。diseaseを指定すると、その病名のときだけ効く(風邪薬など)。
			if r.Disease != "" && !knownDiseases[r.Disease] {
				return Effect{}, fmt.Errorf("effect[%d]: unknown disease %q", i, r.Disease)
			}
			eff.Ops = append(eff.Ops, Op{Kind: r.Op, Amount: r.Amount, Disease: r.Disease})
		default:
			return Effect{}, fmt.Errorf("effect[%d]: unknown op %q", i, r.Op)
		}
	}
	return eff, nil
}

// MoneySum returns the net money delta of the effect (for display).
func (e Effect) MoneySum() int64 {
	var sum int64
	for _, op := range e.Ops {
		if op.Kind == "add_money" {
			sum += op.Amount
		}
	}
	return sum
}

// SpecialSummary renders the non-parameter effects (体重/身長/病気) as a short
// Japanese label for the item tables. Empty when the effect has none. These do
// not fit the per-parameter columns, so the UI shows them on a separate row
// (legacy depart.cgi rendered the item's 備考 as a colspan row underneath).
func (e Effect) SpecialSummary() string {
	var weightG, heightCm int64
	// 病気は「万能」と病名別を分けて集計する(同じ薬に両方入ることは想定しないが、
	// 入っていても順に並べて表示できるようにしておく)。
	var parts []string
	var random bool
	for _, op := range e.Ops {
		if op.Random {
			random = true
		}
		switch op.Kind {
		case "add_weight_g":
			weightG += op.Amount
		case "add_height_cm":
			heightCm += op.Amount
		case "add_disease":
			if op.Amount <= 0 {
				continue
			}
			if op.Disease == "" {
				parts = append(parts, "どの病気にも効く")
			} else {
				parts = append(parts, op.Disease+"に効く")
			}
		}
	}
	var out []string
	if weightG != 0 {
		out = append(out, fmt.Sprintf("体重%+.1fkg", float64(weightG)/1000))
	}
	if heightCm != 0 {
		out = append(out, fmt.Sprintf("身長%+dcm", heightCm))
	}
	out = append(out, parts...)
	if random {
		// 表の数値は「最大でこれだけ」の意味になるので、そこを断っておく。
		out = append(out, "上がる値はランダム(0〜表の数値)")
	}
	return strings.Join(out, "・")
}

// ParamSum returns the net delta per parameter (for display of rising params).
func (e Effect) ParamSum() map[string]int {
	m := map[string]int{}
	for _, op := range e.Ops {
		if op.Kind == "add_param" {
			m[op.Param] += int(op.Amount)
		}
	}
	return m
}

// Pred is a single condition predicate.
type Pred struct {
	Kind  string
	Param string
	Value int64
}

// Conditions is a list of predicates combined with logical AND.
type Conditions struct {
	Preds []Pred
}

type predJSON struct {
	Pred  string `json:"pred"`
	Param string `json:"param,omitempty"`
	Value int64  `json:"value"`
}

// ParseConditions parses and validates condition JSON.
func ParseConditions(data []byte) (Conditions, error) {
	var raw []predJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return Conditions{}, fmt.Errorf("conditions json: %w", err)
	}
	c := Conditions{Preds: make([]Pred, 0, len(raw))}
	for i, r := range raw {
		switch r.Pred {
		case "money_gte":
			c.Preds = append(c.Preds, Pred{Kind: r.Pred, Value: r.Value})
		case "param_gte":
			if !knownParams[r.Param] {
				return Conditions{}, fmt.Errorf("conditions[%d]: unknown param %q", i, r.Param)
			}
			c.Preds = append(c.Preds, Pred{Kind: r.Pred, Param: r.Param, Value: r.Value})
		default:
			return Conditions{}, fmt.Errorf("conditions[%d]: unknown pred %q", i, r.Pred)
		}
	}
	return c, nil
}

// InsufficientParam reports the first parameter that a negative effect would
// drive below zero given the current state (e.g. not enough energy to work).
// Returns ("", false) if the effect can be afforded.
func (e Effect) InsufficientParam(s State) (string, bool) {
	for _, op := range e.Ops {
		if op.Kind == "add_param" && op.Amount < 0 {
			if s.Params[op.Param].Value+int(op.Amount) < 0 {
				return op.Param, true
			}
		}
	}
	return "", false
}

// ParamMins returns the minimum required value per parameter (for display of
// job requirements).
func (c Conditions) ParamMins() map[string]int {
	m := map[string]int{}
	for _, p := range c.Preds {
		if p.Kind == "param_gte" {
			if int(p.Value) > m[p.Param] {
				m[p.Param] = int(p.Value)
			}
		}
	}
	return m
}

// MoneyMin returns the highest money_gte requirement (0 if none).
func (c Conditions) MoneyMin() int64 {
	var min int64
	for _, p := range c.Preds {
		if p.Kind == "money_gte" && p.Value > min {
			min = p.Value
		}
	}
	return min
}

// ParamState is the current and maximum value of a parameter.
type ParamState struct {
	Value int
	Max   int
}

// State is a snapshot of a player used to evaluate conditions and effects.
type State struct {
	Money  int64
	Params map[string]ParamState
	// 体格・病状。add_weight_g / add_height_cm / add_disease の評価に使う。
	WeightG      int
	HeightCm     int
	DiseaseIndex int
	DiseaseName  string // 現在の病名(健康なら"")
}

// Check reports whether all conditions hold. If not, it returns the first
// failing predicate so the caller can produce a suitable message.
func (c Conditions) Check(s State) (bool, *Pred) {
	for i := range c.Preds {
		p := &c.Preds[i]
		switch p.Kind {
		case "money_gte":
			if s.Money < p.Value {
				return false, p
			}
		case "param_gte":
			if int64(s.Params[p.Param].Value) < p.Value {
				return false, p
			}
		}
	}
	return true, nil
}

// ParamChange is the applied change to one parameter after clamping.
type ParamChange struct {
	Name     string `json:"name"`
	OldValue int    `json:"old_value"`
	NewValue int    `json:"new_value"`
}

// ValueChange is the applied change to one scalar status value after clamping.
type ValueChange struct {
	OldValue int `json:"old_value"`
	NewValue int `json:"new_value"`
}

// Plan is the concrete set of changes an effect produces for a given state.
// Weight/Height/Disease are nil when the effect does not touch them (or when a
// conditional add_disease did not apply).
type Plan struct {
	MoneyDelta int64         `json:"money_delta"`
	Params     []ParamChange `json:"params"`
	Weight     *ValueChange  `json:"weight,omitempty"`
	Height     *ValueChange  `json:"height,omitempty"`
	Disease    *ValueChange  `json:"disease,omitempty"`
}

// Bounds for the scalar status values an effect may change. The disease range
// matches the daily drift job so items and the worker cannot diverge.
const (
	MinWeightG   = 1000 // 1kg。イベントの体重減と同じ下限
	MinHeightCm  = 1    // BMIのゼロ除算を避ける最低限のガード
	DiseaseCeil  = 50   // 健康時の上限(基準50を超えて回復はしない)
	DiseaseFloor = -150 // 病気指数の下限(暴走防止のクリップ)
)

// Roller supplies the randomness for ops marked Random. rng.Rand satisfies it.
type Roller interface {
	// IntN returns a value in [0, n).
	IntN(n int) int
}

// Plan computes the result of applying the effect to the state. Parameters are
// clamped to [0, max]. Money is returned as a raw delta; the ledger remains the
// source of truth for balances.
//
// Random ops keep their full amount here: this is the deterministic view used by
// the admin's effect simulator and by the shop tables, where the configured
// value reads as "how much this can give at most".
func (e Effect) Plan(s State) Plan { return e.plan(s, nil) }

// PlanWith is Plan with a random source, so ops marked Random draw their amount
// from [0, Amount] (負なら [Amount, 0])。実際にプレイヤーへ適用するときはこちら。
func (e Effect) PlanWith(s State, r Roller) Plan { return e.plan(s, r) }

// rollAmount returns the amount to apply for one op.
func rollAmount(op Op, r Roller) int64 {
	if !op.Random || r == nil || op.Amount == 0 {
		return op.Amount
	}
	// レガシー int(rand($v + 1)) と同じく両端を含む。
	if op.Amount > 0 {
		return int64(r.IntN(int(op.Amount) + 1))
	}
	return -int64(r.IntN(int(-op.Amount) + 1))
}

func (e Effect) plan(s State, r Roller) Plan {
	var plan Plan
	cur := make(map[string]int, len(s.Params))
	orig := make(map[string]int, len(s.Params))
	var order []string

	for _, op := range e.Ops {
		switch op.Kind {
		case "add_money":
			plan.MoneyDelta += op.Amount
		case "add_param":
			ps := s.Params[op.Param]
			if _, seen := orig[op.Param]; !seen {
				orig[op.Param] = ps.Value
				cur[op.Param] = ps.Value
				order = append(order, op.Param)
			}
			// [0, max] にクランプ
			cur[op.Param] = max(0, min(cur[op.Param]+int(rollAmount(op, r)), ps.Max))
		case "add_weight_g":
			old := s.WeightG
			if plan.Weight != nil {
				old = plan.Weight.NewValue
			}
			plan.Weight = &ValueChange{OldValue: s.WeightG, NewValue: max(MinWeightG, old+int(op.Amount))}
		case "add_height_cm":
			old := s.HeightCm
			if plan.Height != nil {
				old = plan.Height.NewValue
			}
			plan.Height = &ValueChange{OldValue: s.HeightCm, NewValue: max(MinHeightCm, old+int(op.Amount))}
		case "add_disease":
			// 病名指定つきは、その病気にかかっているときだけ効く(部分一致。
			// "風邪"は"風邪ぎみ"にも効く)。レガシー basic0.cgi の $byoumei =~ /風邪/ 相当。
			if op.Disease != "" && !strings.Contains(s.DiseaseName, op.Disease) {
				continue
			}
			old := s.DiseaseIndex
			if plan.Disease != nil {
				old = plan.Disease.NewValue
			}
			next := max(DiseaseFloor, min(old+int(op.Amount), DiseaseCeil))
			plan.Disease = &ValueChange{OldValue: s.DiseaseIndex, NewValue: next}
		}
	}

	for _, name := range order {
		plan.Params = append(plan.Params, ParamChange{
			Name:     name,
			OldValue: orig[name],
			NewValue: cur[name],
		})
	}
	return plan
}
