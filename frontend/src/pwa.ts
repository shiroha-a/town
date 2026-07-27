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

/** Unregisters everything. 不具合時に手で呼べるよう残しておく。 */
export async function unregisterServiceWorker(): Promise<void> {
  if (!('serviceWorker' in navigator)) return;
  const regs = await navigator.serviceWorker.getRegistrations();
  await Promise.all(regs.map((r) => r.unregister()));
  const keys = await caches.keys();
  await Promise.all(keys.map((k) => caches.delete(k)));
}
