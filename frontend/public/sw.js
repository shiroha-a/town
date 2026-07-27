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

const VERSION = 'v3';
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

/** キャッシュしてよい静的ファイルか。 */
function isStatic(url) {
  return (
    url.pathname.startsWith('/assets/') || // ハッシュ付きJS/CSS(中身は不変)
    url.pathname.startsWith('/img/') ||
    url.pathname.startsWith('/icons/') ||
    url.pathname === '/icon.svg' ||
    // 管理者がアップロードした街の背景画像。/api/ の下にあるが中身は画像で、
    // 名前が変われば別物になるため貯めてよい。
    url.pathname.startsWith('/api/v1/assets/')
  );
}

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

  // 静的ファイルはキャッシュ優先(名前が変われば別物として取り直される)。
  if (isStatic(url)) {
    e.respondWith(
      caches.match(req).then(
        (hit) =>
          hit ||
          fetch(req).then((res) => {
            if (res.ok) {
              const copy = res.clone();
              caches.open(ASSETS).then((c) => c.put(req, copy));
            }
            return res;
          }),
      ),
    );
  }
});
