// /reset.html の中身。ゲーム本体(ビルドしたJS)が壊れていても動くよう、素のJSで
// 書いてビルドを通さない。CSPが script-src 'self' なので別ファイルにしてある。

document.getElementById('go').addEventListener('click', async function () {
  this.disabled = true;
  document.getElementById('msg').textContent = '捨てています…';
  try {
    if ('serviceWorker' in navigator) {
      const regs = await navigator.serviceWorker.getRegistrations();
      await Promise.all(regs.map((r) => r.unregister()));
    }
    // SWに対応していないブラウザでもキャッシュだけは残っていることがある。
    if ('caches' in window) {
      const keys = await caches.keys();
      await Promise.all(keys.map((k) => caches.delete(k)));
    }
  } catch {
    // 消せなかったぶんは諦めて街へ戻る。掴んでいるものが減れば直ることがある。
  }
  // 履歴に残すと戻るボタンでここへ帰ってきてしまう。
  location.replace('/');
});
