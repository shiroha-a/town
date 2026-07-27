import type { Player } from './api';

// お金の表示まわりで共有する計算。街トップと銀行で式がずれていて、街トップだけ
// スーパー定期とローンを見落としていたため、1か所にまとめた。
// サーバー側の定義(ranking.go の assetsExpr)と同じ式にしてある。

/** 円を3桁区切りにする。 */
export const yen = (n: number) => n.toLocaleString('ja-JP');

/**
 * Total assets: cash + 普通口座 + スーパー定期 - ローン残高(日額×残回数).
 * 役場のランキングと同じ式。
 */
export function totalAssets(p: Player): number {
  return p.money + p.savings + p.super_savings - p.loan_daily * p.loan_count;
}
