import { ref, onUnmounted } from 'vue';
import { PARAM_ORDER, PARAM_FULL } from './params';
import type { Player } from './api';

// 画面上部トースト(iOS通知バナー風)の1件分のデータ。variantで色/アイコンの見た目を切り替える。
export interface ToastData {
  variant: 'work' | 'event-good' | 'event-bad' | 'error' | 'item';
  title: string;
  lines: string[];
  icon: string; // CommandIcon の name
}

// トースト状態を管理するcomposable。showToastで表示し、6秒後に自動で消える。
// 街トップ(仕事/イベント)とアイテム使用・食事・トレーニングなど複数画面で共有する。
export function useToast() {
  const toast = ref<ToastData | null>(null);
  let timer: number | undefined;
  function showToast(t: ToastData) {
    toast.value = t;
    if (timer !== undefined) window.clearTimeout(timer);
    timer = window.setTimeout(() => {
      toast.value = null;
    }, 6000);
  }
  function closeToast() {
    toast.value = null;
  }
  onUnmounted(() => {
    if (timer !== undefined) window.clearTimeout(timer);
  });
  return { toast, showToast, closeToast };
}

const yen = (n: number) => n.toLocaleString('ja-JP');

// 使用前後のプレイヤー状態の差分を、トースト行リストに整形する。
// アイテム使用・食事・トレーニング・勉強など効果系アクションで共有する。
export function buildEffectLines(before: Player, after: Player): string[] {
  const lines: string[] = [];
  const moneyDiff = after.money - before.money;
  if (moneyDiff !== 0) lines.push(`お金 ${moneyDiff > 0 ? '+' : ''}${yen(moneyDiff)}円`);
  const eDiff = after.status.energy - before.status.energy;
  if (eDiff !== 0) lines.push(`身体パワー ${eDiff > 0 ? '+' : ''}${eDiff}`);
  const nDiff = after.status.nou_energy - before.status.nou_energy;
  if (nDiff !== 0) lines.push(`頭脳パワー ${nDiff > 0 ? '+' : ''}${nDiff}`);
  const sDiff = after.status.satiety - before.status.satiety;
  if (sDiff !== 0) lines.push(`満腹度 ${sDiff > 0 ? '+' : ''}${sDiff}`);
  // 体重はg保持なのでkg小数1位で表示する(カロリーのある飲食で増える)。
  const wDiff = after.status.weight_g - before.status.weight_g;
  if (wDiff !== 0) lines.push(`体重 ${wDiff > 0 ? '+' : ''}${(wDiff / 1000).toFixed(1)}kg`);
  const bp = before.params as unknown as Record<string, number>;
  const ap = after.params as unknown as Record<string, number>;
  for (const key of PARAM_ORDER) {
    const diff = (ap[key] ?? 0) - (bp[key] ?? 0);
    if (diff !== 0) lines.push(`${PARAM_FULL[key] ?? key} ${diff > 0 ? '+' : ''}${diff}`);
  }
  return lines.length ? lines : ['変化なし'];
}
