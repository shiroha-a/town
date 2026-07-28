package streetfight

import (
	"strings"
	"testing"

	"github.com/shiroha-a/town/internal/rng"
)

// flat builds a parameter set where every ability has the same value.
func flat(v int) map[string]int {
	p := map[string]int{}
	for _, k := range Abilities {
		p[k] = v
	}
	return p
}

func TestPowerMaxMatchesLegacyFormula(t *testing.T) {
	// レガシー: looks/12 + tairyoku/4 + kenkou/4 + (speed+power+wanryoku+kyakuryoku)/8
	// 全能力5なら 0.41+1.25+1.25+2.5 = 5.41 -> 5、リライトは +1 して 6。
	if got := EnergyMax(flat(5)); got != 6 {
		t.Errorf("EnergyMax(5) = %d, want 6", got)
	}
	// 頭脳は7科目/6。5なら 5.83 -> 5、+1 で 6。
	if got := NouEnergyMax(flat(5)); got != 6 {
		t.Errorf("NouEnergyMax(5) = %d, want 6", got)
	}
}

func TestFightStrongPlayerWins(t *testing.T) {
	m := Monster{Name: "スライム", Params: flat(5), WinMoney: 10, LoseMoney: 1}
	res := Fight(flat(500), 100, 100, m, rng.New(1))
	if res.Outcome != "win" {
		t.Fatalf("outcome = %q, want win (turns=%d)", res.Outcome, len(res.Turns))
	}
	if res.Money != 10 {
		t.Errorf("money = %d, want 10", res.Money)
	}
	// 相手より全能力で上回っているので、こちらは一度も削られない。
	if res.Energy != 100 || res.Nou != 100 {
		t.Errorf("power after = %d/%d, want 100/100", res.Energy, res.Nou)
	}
}

func TestFightWeakPlayerLoses(t *testing.T) {
	m := Monster{Name: "強敵", Params: flat(500), WinMoney: 10, LoseMoney: 300}
	res := Fight(flat(5), 20, 20, m, rng.New(2))
	if res.Outcome != "lose" {
		t.Fatalf("outcome = %q, want lose", res.Outcome)
	}
	if res.Money != -300 {
		t.Errorf("money = %d, want -300", res.Money)
	}
	// 負けた側のパワーは0止まり(マイナスのまま保存しない)。
	if res.Energy < 0 || res.Nou < 0 {
		t.Errorf("power after = %d/%d, want >= 0", res.Energy, res.Nou)
	}
	if res.Energy != 0 && res.Nou != 0 {
		t.Errorf("power after = %d/%d, want one of them 0", res.Energy, res.Nou)
	}
}

func TestFightEqualsDraw(t *testing.T) {
	// 能力が同じなら差が0で誰も削れず、50ターン尽きて引き分け。
	m := Monster{Name: "鏡", Params: flat(50)}
	res := Fight(flat(50), 30, 30, m, rng.New(3))
	if res.Outcome != "draw" {
		t.Fatalf("outcome = %q, want draw", res.Outcome)
	}
	if len(res.Turns) != maxTurns {
		t.Errorf("turns = %d, want %d", len(res.Turns), maxTurns)
	}
	if res.Money != 0 {
		t.Errorf("money = %d, want 0", res.Money)
	}
}

func TestFasterSideStrikesFirst(t *testing.T) {
	slow := flat(10)
	slow["speed"] = 1
	m := Monster{Name: "俊足", Params: slow}
	fast := flat(10)
	fast["speed"] = 99
	if got := Fight(fast, 50, 50, m, rng.New(4)).Turns[0].Attacker; got != "player" {
		t.Errorf("first attacker = %q, want player (速いほうが先攻)", got)
	}
	m.Params["speed"] = 99
	if got := Fight(slow, 50, 50, m, rng.New(4)).Turns[0].Attacker; got != "monster" {
		t.Errorf("first attacker = %q, want monster", got)
	}
}

func TestBrainAndBodyDamageSplit(t *testing.T) {
	// 前半8つ(国語〜ルックス)は頭脳、残りは身体を削る。
	for i, a := range Abilities {
		want := "energy"
		if i < brainAttacks {
			want = "nou"
		}
		p, mp := flat(0), flat(0)
		p[a] = 100
		m := Monster{Name: "的", Params: mp}
		res := Fight(p, 500, 500, m, rng.New(int64(i+1)))
		var hit *Turn
		for j := range res.Turns {
			if res.Turns[j].Ability == a && res.Turns[j].Damage > 0 {
				hit = &res.Turns[j]
				break
			}
		}
		if hit == nil {
			continue // その能力の攻撃が引かれなかった回は飛ばす
		}
		if hit.Target != want {
			t.Errorf("%s hits %q, want %q", a, hit.Target, want)
		}
	}
}

func TestRewardDropsOnWin(t *testing.T) {
	id := int64(42)
	m := Monster{Name: "スライム", Params: flat(1), RewardItemID: &id, RewardName: "お鍋のふた", ItemRate: 1}
	// ItemRate=1 なら IntN(1)==0 で必ず当たる。
	res := Fight(flat(500), 100, 100, m, rng.New(5))
	if res.Outcome != "win" {
		t.Fatalf("outcome = %q, want win", res.Outcome)
	}
	if res.RewardItemID == nil || *res.RewardItemID != id {
		t.Errorf("reward = %v, want %d", res.RewardItemID, id)
	}
	// 負けたら景品は出ない。
	strong := Monster{Name: "強敵", Params: flat(500), RewardItemID: &id, ItemRate: 1}
	lost := Fight(flat(1), 5, 5, strong, rng.New(6))
	if lost.RewardItemID != nil {
		t.Errorf("reward on %s = %v, want nil", lost.Outcome, lost.RewardItemID)
	}
}

func TestTurnTextNamesTheDefender(t *testing.T) {
	m := Monster{Name: "スライム", Params: flat(1)}
	res := Fight(flat(500), 100, 100, m, rng.New(7))
	first := res.Turns[0]
	if first.Attacker != "player" {
		t.Fatalf("first attacker = %q", first.Attacker)
	}
	if !strings.Contains(first.Text, "スライム") {
		t.Errorf("text = %q, want it to name スライム", first.Text)
	}
	if strings.Contains(first.Text, "%") {
		t.Errorf("text = %q, format placeholder left over", first.Text)
	}
}
