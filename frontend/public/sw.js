// Service Worker。ホーム画面/デスクトップから起動できるようにするための最低条件で
// あり、あわせて再訪時の表示を速くする。
//
// このゲームはサーバーが全部を処理するのでオフラインでは何もできない。よって
// 「オフラインでも遊べるようにする」ことは狙わず、
//   - 起動に要る一式(JS/CSS/画像)を手元に持って表示を速くする
//   - 通信できないときは正直にその旨を出す
// の2つに絞る。
//
// 事前キャッシュ(precache)はしない。ビルド成果物の名前がハッシュ付きで変わるため、
// 一覧を作るとビルド側との二重管理になる。代わりに実際に使われたものを都度貯める。

const VERSION = 'v5';
const ASSETS = `town-assets-${VERSION}`; // ハッシュ付きJS/CSS・画像
const PAGES = `town-pages-${VERSION}`; // オフラインページ
const OFFLINE = '/offline.html';

self.addEventListener('install', (e) => {
  // オフライン時に出すページだけは先に持っておく(通信できない状況で取りに行けないため)。
  e.waitUntil(caches.open(PAGES).then((c) => c.add(OFFLINE)));
  self.skipWaiting();
});

self.addEventListener('activate', (e) => {
  // 版が上がったら古いキャッシュを捨てる。放置すると容量を食い続ける。
  e.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(keys.filter((k) => k !== ASSETS && k !== PAGES).map((k) => caches.delete(k))),
      )
      .then(() => self.clients.claim()),
  );
});

/**
 * 名前が変われば別物になるファイルか。中身が変わることはないので、手元にあれば
 * それを使って取りに行かなくてよい。
 */
function isImmutable(url) {
  return (
    url.pathname.startsWith('/assets/') || // ハッシュ付きJS/CSS
    // 管理者がアップロードした街の背景画像。/api/ の下にあるが中身は画像で、
    // 名前にIDが入るため貯めてよい。
    url.pathname.startsWith('/api/v1/assets/')
  );
}

/**
 * 同じ名前で中身が変わりうるファイルか。同梱の画像はファイル名にハッシュが
 * 入らないので、アイコンを描き直しても名前が変わらない。手元のものを即返しつつ
 * 裏で取り直し、次回から新しいものが出るようにする。
 */
function isRevalidated(url) {
  return (
    url.pathname.startsWith('/img/') ||
    url.pathname.startsWith('/icons/') ||
    url.pathname === '/icon.svg'
  );
}

// 通知を受け取って出す。中身はサーバーが送るJSON(title/body/url/kind)。
self.addEventListener('push', (e) => {
  let d = {};
  try {
    d = e.data ? e.data.json() : {};
  } catch {
    // 形が違うものは無視する(表示できないため)。
  }
  if (!d.title) return;
  e.waitUntil(
    self.registration.showNotification(d.title, {
      body: d.body || '',
      icon: '/icons/icon-192.png',
      badge: '/icons/icon-192.png',
      // 同じ種類の通知は積み上げず、新しいもので置き換える。
      tag: d.kind || 'town',
      renotify: true,
      data: { url: d.url || '/' },
    }),
  );
});

// 通知をタップしたら、その用件の画面を開く。既に開いているタブがあればそれを使う。
self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const url = (e.notification.data && e.notification.data.url) || '/';
  e.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
      for (const c of list) {
        if (new URL(c.url).origin === self.location.origin) {
          return c.focus().then(() => c.navigate(url));
        }
      }
      return self.clients.openWindow(url);
    }),
  );
});

self.addEventListener('fetch', (e) => {
  const req = e.request;
  if (req.method !== 'GET') return;

  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return; // Misskeyのアイコン等は素通し

  // APIは絶対にキャッシュしない。ログイン状態や所持金が古いまま出ると実害が出る。
  // ただし /api/v1/assets/ はアップロード画像なので、下の静的扱いに回す。
  if (url.pathname.startsWith('/api/') && !url.pathname.startsWith('/api/v1/assets/')) return;

  // 画面遷移(HTML)は必ずネットワークから取る。デプロイがすぐ反映されるうえ、
  // HTMLは小さく no-cache で配られているので手元に持つ意味が薄い。
  // 取れなければオフラインページを出す。キャッシュした画面を出すと、中身の無い
  // 街が表示されてから通信エラーが並ぶことになり、かえって分かりにくい。
  if (req.mode === 'navigate') {
    e.respondWith(fetch(req).catch(() => caches.match(OFFLINE)));
    return;
  }

  // 中身が変わらないものはキャッシュ優先(名前が変われば別物として取り直される)。
  if (isImmutable(url)) {
    e.respondWith(caches.match(req).then((hit) => hit || fetchAndStore(req)));
    return;
  }

  // 同じ名前で差し替わりうる画像は、手元のものを即返してから裏で取り直す。
  if (isRevalidated(url)) {
    e.respondWith(
      caches.match(req).then((hit) => {
        if (!hit) return fetchAndStore(req);
        // 差し替えが次回に間に合うよう、応答を返した後も取り直しを走らせる。
        e.waitUntil(fetchAndStore(req).catch(() => {}));
        return hit;
      }),
    );
  }
});

/** 取ってきて手元にも残す。 */
function fetchAndStore(req) {
  return fetch(req).then((res) => {
    if (res.ok) {
      const copy = res.clone();
      caches.open(ASSETS).then((c) => c.put(req, copy));
    }
    return res;
  });
}
