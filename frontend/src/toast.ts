import { ref } from 'vue';
import { PARAM_ORDER, PARAM_FULL } from './params';
import { projectedPower } from './power';
import type { Player } from './api';

// 画面上部トースト(iOS通知バナー風)の1件分のデータ。variantで色/アイコンの見た目を切り替える。
export interface ToastData {
  variant: ToastVariant;
  title: string;
  lines: string[];
  icon: string; // CommandIcon の name
}

// ok/error は画面を問わない汎用の成功・失敗。ほかは出どころが決まっているもの
// (仕事・ランダムイベント・アイテム)で、専用のアイコンと色を持つ。
export type ToastVariant = 'ok' | 'error' | 'work' | 'event-good' | 'event-bad' | 'item';

// アイコン省略時の既定。指定があればそちらを優先する(施設ごとの絵柄を出せる)。
const DEFAULT_ICON: Record<ToastVariant, string> = {
  ok: 'ok',
  error: 'error',
  work: 'go_work',
  'event-good': 'event',
  'event-bad': 'event',
  item: 'item',
};

// 表示中のトースト。アプリ全体で1つだけ持ち、App.vueが描画する。画面ごとに
// 持たせると、遷移した瞬間に消えたり、親子で二重に出たりするため。
const toast = ref<ToastData | null>(null);
let timer: number | undefined;

/** The single toast shown at the top of the app (App.vue renders it). */
export function currentToast() {
  return toast;
}

/** Shows a toast for 6 seconds, replacing whatever was on screen. */
export function showToast(t: {
  variant: ToastVariant;
  title: string;
  lines?: string[];
  icon?: string;
}) {
  toast.value = {
    variant: t.variant,
    title: t.title,
    lines: t.lines ?? [],
    icon: t.icon ?? DEFAULT_ICON[t.variant],
  };
  if (timer !== undefined) window.clearTimeout(timer);
  timer = window.setTimeout(() => {
    toast.value = null;
  }, 6000);
}

export function closeToast() {
  toast.value = null;
  if (timer !== undefined) window.clearTimeout(timer);
}

/** Success notification: 操作が通ったことの短い知らせ。 */
export function notifyOk(title: string, lines: string[] = [], icon?: string) {
  showToast({ variant: 'ok', title, lines, icon });
}

/**
 * Failure notification. errには例外をそのまま渡せる(文言を取り出して出す)。
 * サーバーの文言は改行を含むことがある(凍結の理由など)ので、行に分けて渡す。
 */
export function notifyError(title: string, err?: unknown, icon?: string) {
  showToast({
    variant: 'error',
    title,
    lines: err === undefined ? [] : errorText(err).split('\n'),
    icon,
  });
}

/** Pulls the message out of whatever was thrown (APIのエラーはError)。 */
export function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

const yen = (n: number) => n.toLocaleString('ja-JP');

// パワーは時間で自然回復するため、行動前のスナップショットとそのまま比べると
// 画面を開いていた時間ぶんの回復まで「効果」として出てしまう(特典コードのように
// 何も起こさない行動でも「身体パワー +34」と出る)。行動時点まで先読みした値
// (=画面のバーに見えていた値)と比べて、行動そのものの増減だけを出す。
// 現在時刻はレスポンスのserver_nowを使う(クライアント時計のずれを持ち込まない)。
function powerBefore(before: Player, after: Player): { energy: number; nou: number } {
  const nowMs = Date.parse(after.server_now) || Date.now();
  const s = before.status;
  return {
    energy: projectedPower(
      {
        value: s.energy,
        max: s.energy_max,
        nextAt: s.energy_next_at,
        recoveryMs: s.energy_recovery_ms,
      },
      nowMs,
    ),
    nou: projectedPower(
      {
        value: s.nou_energy,
        max: s.nou_energy_max,
        nextAt: s.nou_energy_next_at,
        recoveryMs: s.nou_recovery_ms,
      },
      nowMs,
    ),
  };
}

// 使用前後のプレイヤー状態の差分を、トースト行リストに整形する。
// アイテム使用・食事・トレーニング・勉強など効果系アクションで共有する。
export function buildEffectLines(before: Player, after: Player): string[] {
  const lines: string[] = [];
  const moneyDiff = after.money - before.money;
  if (moneyDiff !== 0) lines.push(`お金 ${moneyDiff > 0 ? '+' : ''}${yen(moneyDiff)}円`);
  const power = powerBefore(before, after);
  const eDiff = after.status.energy - power.energy;
  if (eDiff !== 0) lines.push(`身体パワー ${eDiff > 0 ? '+' : ''}${eDiff}`);
  const nDiff = after.status.nou_energy - power.nou;
  if (nDiff !== 0) lines.push(`頭脳パワー ${nDiff > 0 ? '+' : ''}${nDiff}`);
  const sDiff = after.status.satiety - before.status.satiety;
  if (sDiff !== 0) lines.push(`満腹度 ${sDiff > 0 ? '+' : ''}${sDiff}`);
  // 体重はg保持なのでkg小数1位で表示する(カロリーのある飲食や特殊効果で増減)。
  const wDiff = after.status.weight_g - before.status.weight_g;
  if (wDiff !== 0) lines.push(`体重 ${wDiff > 0 ? '+' : ''}${(wDiff / 1000).toFixed(1)}kg`);
  // 身長・体調(病気指数)の特殊効果。病気指数は数値を出さず、回復した旨と病名の変化で見せる。
  const hDiff = after.status.height_cm - before.status.height_cm;
  if (hDiff !== 0) lines.push(`身長 ${hDiff > 0 ? '+' : ''}${hDiff}cm`);
  const dDiff = after.status.disease_index - before.status.disease_index;
  if (dDiff > 0) {
    lines.push(
      before.status.disease_name && !after.status.disease_name
        ? `${before.status.disease_name}が治った`
        : '体調が回復した',
    );
  }
  const bp = before.params as unknown as Record<string, number>;
  const ap = after.params as unknown as Record<string, number>;
  for (const key of PARAM_ORDER) {
    const diff = (ap[key] ?? 0) - (bp[key] ?? 0);
    if (diff !== 0) lines.push(`${PARAM_FULL[key] ?? key} ${diff > 0 ? '+' : ''}${diff}`);
  }
  return lines.length ? lines : ['変化なし'];
}
