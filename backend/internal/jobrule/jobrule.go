// Package jobrule holds shared job/work economy rules used by both the action
// service (実際の消費・給料計算) and the content service (職安一覧の表示用).
// 循環参照を避けるため、消費式を1箇所に集約している。
package jobrule

// energyCoefByRank maps a job's hidden rank (1〜6、UIには出さない) to its
// power-consumption coefficient. 高ランクほど消費が重くなる等差(1〜6)。
var energyCoefByRank = map[int]int{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6}

// EnergyCoef returns the consumption coefficient for a job rank.
// 未知ランクは最大係数として扱う。
func EnergyCoef(rank int) int {
	if c, ok := energyCoefByRank[rank]; ok {
		return c
	}
	return 6
}

// PowerSpend returns the body/brain power consumed for one work action:
// 基準 + 基準×ランク係数。基準以下にはならず、青天井のパラメータ値には依存しない。
func PowerSpend(baseCost, rank int) int {
	return baseCost + baseCost*EnergyCoef(rank)
}

// PayPerPower is the salary bonus (円) added per unit of body+brain power
// consumed, so heavier (higher-rank) jobs pay more.
const PayPerPower = 20
