// Service Workerの登録。中身は public/sw.js(ビルドを通さない素のJS)。
//
// 登録すると「ホーム画面に追加」「インストール」が出せるようになり、再訪時の
// 表示も速くなる。オフラインで遊べるようにはしない(全部サーバー処理のため)。

/** Registers the service worker. 対応していないブラウザでは何もしない。 */
export function registerServiceWorker(): void {
  if (!('serviceWorker' in navigator)) return;
  // 開発サーバー(vite)では登録しない。ビルド前のファイルを掴んだまま古い画面を
  // 返し続けることがあり、開発の邪魔になる。
  if (import.meta.env.DEV) return;

  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {
      // 登録に失敗しても遊べる。ホーム画面へ入れられなくなるだけ。
    });
  });
}

/**
 * Asks the browser to re-fetch sw.js. 前面に戻ったときに呼ぶ。
 *
 * ブラウザが自前で更新を確かめるのはページ遷移のときだが、この画面は遷移を
 * pushStateでやるので、放っておくとSW側の修正(通知の受け口・キャッシュの
 * 扱いなど)がいつまでも届かない。
 */
export async function checkServiceWorkerUpdate(): Promise<void> {
  if (!('serviceWorker' in navigator)) return;
  try {
    const reg = await navigator.serviceWorker.getRegistration();
    await reg?.update();
  } catch {
    // 圏外などで失敗する。次の機会にまた見る。
  }
}

/**
 * Unregisters the worker and drops every cache. 取り違えたものを掴んだまま
 * 直せないのが一番困るので、ユーザー設定と /reset.html から呼べるようにしてある。
 *
 * ログインはHttpOnly cookieのセッションなので、これで外れることはない。
 */
export async function unregisterServiceWorker(): Promise<void> {
  if ('serviceWorker' in navigator) {
    const regs = await navigator.serviceWorker.getRegistrations();
    await Promise.all(regs.map((r) => r.unregister()));
  }
  // SWに対応していないブラウザでもキャッシュだけは残っていることがある。
  if ('caches' in window) {
    const keys = await caches.keys();
    await Promise.all(keys.map((k) => caches.delete(k)));
  }
}
