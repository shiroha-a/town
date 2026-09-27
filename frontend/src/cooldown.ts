import { ref, computed, watch, onMounted, onUnmounted, type ComputedRef } from 'vue';
import type { Player } from './api';

const pad = (n: number) => String(n).padStart(2, '0');

/**
 * Remaining cooldown of a facility (e.g. gym/kyushitu/school) as a label such as
 * "あと3分05秒", ticking every second. Returns null while the facility is usable.
 */
export function useFacilityCooldown(
  player: () => Player,
  facility: () => string,
): ComputedRef<string | null> {
  // サーバ時刻とのずれを補正し、端末の時計がずれていても残り時間が合うようにする。
  const skewMs = ref(0);
  function syncSkew() {
    const serverNow = new Date(player().server_now).getTime();
    if (!Number.isNaN(serverNow)) skewMs.value = serverNow - Date.now();
  }
  syncSkew();
  watch(() => player().server_now, syncSkew);

  const nowMs = ref(Date.now());
  let timer: number | undefined;
  onMounted(() => {
    timer = window.setInterval(() => {
      nowMs.value = Date.now();
    }, 1000);
  });
  onUnmounted(() => {
    if (timer !== undefined) window.clearInterval(timer);
  });

  return computed(() => {
    const at = player().status.facility_available_at?.[facility()];
    if (!at) return null;
    const remain = new Date(at).getTime() - (nowMs.value + skewMs.value);
    if (Number.isNaN(remain) || remain <= 0) return null;
    const sec = Math.ceil(remain / 1000);
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    const s = sec % 60;
    // 学校は次のゲーム日まで待つので時間単位になりうる。
    if (h > 0) return `あと${h}時間${pad(m)}分`;
    if (m > 0) return `あと${m}分${pad(s)}秒`;
    return `あと${s}秒`;
  });
}
