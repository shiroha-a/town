// 画面を消さない仕組み(Screen Wake Lock)。
//
// 温泉の入浴中のように「操作せずに数分眺める」場面では、端末の自動ロックで画面が
// 暗くなってしまう。入浴中だけロックを取り、離れたら必ず返す。
//
// 対応していないブラウザ(iOS Safari 16.4未満など)では黙って何もしない。
// あれば嬉しい程度のもので、無くても遊べる。

type WakeLockSentinelLike = { released: boolean; release(): Promise<void> };

let sentinel: WakeLockSentinelLike | null = null;
let want = false;

function api(): { request(type: 'screen'): Promise<WakeLockSentinelLike> } | null {
  const nav = navigator as Navigator & {
    wakeLock?: { request(type: 'screen'): Promise<WakeLockSentinelLike> };
  };
  return nav.wakeLock ?? null;
}

async function acquire(): Promise<void> {
  const wl = api();
  if (!wl || !want || sentinel) return;
  try {
    sentinel = await wl.request('screen');
    // 端末側の都合で外れることがある(通話・電源設定など)。参照を残さない。
    (sentinel as unknown as EventTarget).addEventListener?.('release', () => {
      sentinel = null;
    });
  } catch {
    // 権限が無い・タブが背面などで失敗する。諦めてよい。
  }
}

// タブを裏に回すとロックは自動で外れる。戻ってきたら取り直す。
function onVisible(): void {
  if (document.visibilityState === 'visible') void acquire();
}

/** Keeps the screen awake until releaseWakeLock() is called. */
export function requestWakeLock(): void {
  if (want) return;
  want = true;
  document.addEventListener('visibilitychange', onVisible);
  void acquire();
}

/** Releases the lock. 呼び忘れると画面が点いたままになるので必ず対で呼ぶ。 */
export function releaseWakeLock(): void {
  want = false;
  document.removeEventListener('visibilitychange', onVisible);
  const s = sentinel;
  sentinel = null;
  if (s && !s.released) void s.release().catch(() => {});
}
