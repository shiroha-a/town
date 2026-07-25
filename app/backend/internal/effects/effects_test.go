package effects

import "testing"

func TestParseEffect(t *testing.T) {
	eff, err := ParseEffect([]byte(`[{"op":"add_money","amount":1000},{"op":"add_param","param":"energy","amount":-1}]`))
	if err != nil {
		t.Fatalf("ParseEffect: %v", err)
	}
	if len(eff.Ops) != 2 {
		t.Fatalf("ops = %d, want 2", len(eff.Ops))
	}
}

func TestParseEffectRejectsUnknown(t *testing.T) {
	if _, err := ParseEffect([]byte(`[{"op":"delete_universe"}]`)); err == nil {
		t.Error("expected error for unknown op")
	}
	if _, err := ParseEffect([]byte(`[{"op":"add_param","param":"mana","amount":1}]`)); err == nil {
		t.Error("expected error for unknown param")
	}
}

func TestParseConditionsRejectsUnknown(t *testing.T) {
	if _, err := ParseConditions([]byte(`[{"pred":"always_true"}]`)); err == nil {
		t.Error("expected error for unknown pred")
	}
}

func TestConditionsCheck(t *testing.T) {
	conds, err := ParseConditions([]byte(`[{"pred":"param_gte","param":"energy","value":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	pass := State{Params: map[string]ParamState{"energy": {Value: 3, Max: 10}}}
	if ok, _ := conds.Check(pass); !ok {
		t.Error("expected conditions to pass with energy=3")
	}
	fail := State{Params: map[string]ParamState{"energy": {Value: 0, Max: 10}}}
	ok, failed := conds.Check(fail)
	if ok {
		t.Error("expected conditions to fail with energy=0")
	}
	if failed == nil || failed.Param != "energy" {
		t.Errorf("failed pred = %+v, want energy", failed)
	}
}

func TestEffectPlanClamps(t *testing.T) {
	eff, err := ParseEffect([]byte(`[{"op":"add_money","amount":1000},{"op":"add_param","param":"energy","amount":-5}]`))
	if err != nil {
		t.Fatal(err)
	}
	// energy 2 - 5 は 0 で下限クランプ。
	plan := eff.Plan(State{Money: 500, Params: map[string]ParamState{"energy": {Value: 2, Max: 10}}})
	if plan.MoneyDelta != 1000 {
		t.Errorf("money delta = %d, want 1000", plan.MoneyDelta)
	}
	if len(plan.Params) != 1 || plan.Params[0].NewValue != 0 {
		t.Errorf("param plan = %+v, want energy -> 0", plan.Params)
	}
	if plan.Params[0].OldValue != 2 {
		t.Errorf("param old value = %d, want 2", plan.Params[0].OldValue)
	}
}

func TestEffectPlanClampsToMax(t *testing.T) {
	eff, _ := ParseEffect([]byte(`[{"op":"add_param","param":"energy","amount":100}]`))
	plan := eff.Plan(State{Params: map[string]ParamState{"energy": {Value: 8, Max: 10}}})
	if plan.Params[0].NewValue != 10 {
		t.Errorf("new value = %d, want 10 (max clamp)", plan.Params[0].NewValue)
	}
}

// bodyState returns a healthy 160cm/55kg player for the scalar-effect tests.
func bodyState() State {
	return State{
		Params:       map[string]ParamState{},
		WeightG:      55000,
		HeightCm:     160,
		DiseaseIndex: DiseaseCeil,
		DiseaseName:  "",
	}
}

func TestEffectPlanWeightAndHeight(t *testing.T) {
	eff, err := ParseEffect([]byte(`[{"op":"add_weight_g","amount":2000},{"op":"add_height_cm","amount":3}]`))
	if err != nil {
		t.Fatalf("ParseEffect: %v", err)
	}
	plan := eff.Plan(bodyState())
	if plan.Weight == nil || plan.Weight.NewValue != 57000 {
		t.Errorf("weight = %+v, want new 57000", plan.Weight)
	}
	if plan.Height == nil || plan.Height.NewValue != 163 {
		t.Errorf("height = %+v, want new 163", plan.Height)
	}
}

func TestEffectPlanWeightClampsToMin(t *testing.T) {
	eff, _ := ParseEffect([]byte(`[{"op":"add_weight_g","amount":-999999}]`))
	plan := eff.Plan(bodyState())
	if plan.Weight == nil || plan.Weight.NewValue != MinWeightG {
		t.Errorf("weight = %+v, want new %d", plan.Weight, MinWeightG)
	}
}

func TestEffectPlanDiseaseUnconditional(t *testing.T) {
	// 万能薬。健康(上限)なら回復しても上限で止まる。
	eff, _ := ParseEffect([]byte(`[{"op":"add_disease","amount":20}]`))
	if plan := eff.Plan(bodyState()); plan.Disease == nil || plan.Disease.NewValue != DiseaseCeil {
		t.Errorf("healthy disease = %+v, want new %d", plan.Disease, DiseaseCeil)
	}
	// 病気なら指数が上がる(回復方向)。
	st := bodyState()
	st.DiseaseIndex, st.DiseaseName = -30, "肺炎"
	if plan := eff.Plan(st); plan.Disease == nil || plan.Disease.NewValue != -10 {
		t.Errorf("sick disease = %+v, want new -10", plan.Disease)
	}
}

func TestEffectPlanDiseaseConditional(t *testing.T) {
	eff, err := ParseEffect([]byte(`[{"op":"add_disease","amount":10,"disease":"風邪"}]`))
	if err != nil {
		t.Fatalf("ParseEffect: %v", err)
	}
	// 対象外の病気には効かない。
	st := bodyState()
	st.DiseaseIndex, st.DiseaseName = -30, "肺炎"
	if plan := eff.Plan(st); plan.Disease != nil {
		t.Errorf("肺炎 with 風邪薬 = %+v, want no change", plan.Disease)
	}
	// 風邪ぎみ にも部分一致で効く(レガシー $byoumei =~ /風邪/ 相当)。
	st.DiseaseIndex, st.DiseaseName = -5, "風邪ぎみ"
	if plan := eff.Plan(st); plan.Disease == nil || plan.Disease.NewValue != 5 {
		t.Errorf("風邪ぎみ with 風邪薬 = %+v, want new 5", plan.Disease)
	}
}

func TestParseEffectRejectsUnknownDisease(t *testing.T) {
	if _, err := ParseEffect([]byte(`[{"op":"add_disease","amount":1,"disease":"二日酔い"}]`)); err == nil {
		t.Error("expected error for unknown disease")
	}
}
