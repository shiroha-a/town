// パワー表示の先読み。
//
// サーバーは読み出し時点の値と「次の1ポイントが回復する時刻」を返す。画面は
// それを起点に、設定された回復秒数どおりに数字を増やして見せる。通信を待って
// から増やすと、ポーリング間隔でカクッと跳ねて「設定した秒数で回復している」
// ようには見えないため。
//
// サーバーと同じ式なので、次のポーリングで値が飛ぶことはない。
export type PowerState = {
  value: number;
  max: number;
  /** 次の1ポイントが回復する時刻(ISO)。満タンなら null。 */
  nextAt: string | null;
  /** 1ポイントあたりのミリ秒(入浴倍率を反映済み)。入浴中は1秒未満になる。 */
  recoveryMs: number;
};

/** Projects the power value at `nowMs` from the last server response. */
export function projectedPower(s: PowerState, nowMs: number): number {
  if (!s.nextAt || s.recoveryMs <= 0 || s.value >= s.max) return Math.min(s.value, s.max);
  const next = new Date(s.nextAt).getTime();
  if (Number.isNaN(next) || nowMs < next) return s.value;
  // 次の回復時刻を過ぎたぶんだけ加算する(画面を開いたまま放置しても追従する)。
  const gained = 1 + Math.floor((nowMs - next) / s.recoveryMs);
  return Math.min(s.max, s.value + gained);
}
