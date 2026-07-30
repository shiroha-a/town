// 新しい版が出ていないかを見る仕組み。
//
// この画面は遷移を全部pushStateでやるので、最初の一回を除いてページ読み込みが
// 起きない。さらにiOSのホーム画面アプリは起動し直しても再読み込みせず、中断して
// いたJSをそのまま再開する。JSは名前にハッシュが入っていてimmutableで配るため、
// 何もしないと古いバンドルが動き続け、デプロイしても何時間・何日も届かない。
// (再読み込みが起きるのはOSがメモリ不足でアプリを捨てた後だけで、起きるかは運)
//
// そこで前面に戻ってきたときにindex.htmlを取り直し、指しているバンドルの名前が
// 変わっていたら新しい版が出ていると判断する。専用のAPIは作らない。index.htmlは
// no-cacheで配られていて1.5KB程度なので、これで足りる。

import { checkServiceWorkerUpdate } from './pwa';

/** index.htmlが指すバンドル。ビルドごとにハッシュが変わる。 */
const BUNDLE = /\/assets\/index-[\w-]+\.js/;

// 復帰のたびに叩かないための下限と、開いたままの端末向けの見回り間隔。
const MIN_INTERVAL = 60_000;
const POLL_INTERVAL = 30 * 60_000;

let running = ''; // いま動いているバンドルの名前
let notify: (() => void) | null = null;
let lastCheck = 0;
let timer: number | undefined;

/** いま動いているバンドル。読み込み時のindex.htmlが指していたものを見る。 */
function currentBundle(): string {
  const el = document.querySelector<HTMLScriptElement>('script[type="module"][src]');
  return el?.src.match(BUNDLE)?.[0] ?? '';
}

/** サーバーが今返すindex.htmlが指すバンドル。 */
async function latestBundle(): Promise<string> {
  // no-storeでHTTPキャッシュを迂回する。ここを経験的キャッシュに任せると、
  // 更新を見に行っているつもりで古い答えを見ることになる。
  const res = await fetch('/', { cache: 'no-store' });
  if (!res.ok) return '';
  return (await res.text()).match(BUNDLE)?.[0] ?? '';
}

async function check(): Promise<void> {
  if (!running || !notify) return;
  const now = Date.now();
  if (now - lastCheck < MIN_INTERVAL) return;
  lastCheck = now;
  // SW本体の修正も同じ機会に取り込む。
  void checkServiceWorkerUpdate();
  try {
    const latest = await latestBundle();
    if (!latest || latest === running) return;
  } catch {
    // 圏外・機内モードなどで取れないことがある。次の復帰で見ればよい。
    return;
  }
  const found = notify;
  stopUpdateWatch(); // 一度見つけたら見張る必要はない
  found();
}

function onVisible(): void {
  if (document.visibilityState === 'visible') void check();
}

/**
 * Starts watching for a new deployment. 見つけたら found を一度だけ呼ぶ。
 * 開発サーバーでは何もしない(バンドルの名前が無く、常に最新のため)。
 */
export function watchForUpdate(found: () => void): void {
  if (import.meta.env.DEV) return;
  running = currentBundle();
  if (!running) return;
  notify = found;
  document.addEventListener('visibilitychange', onVisible);
  timer = window.setInterval(onVisible, POLL_INTERVAL);
}

/** Stops watching. */
export function stopUpdateWatch(): void {
  notify = null;
  document.removeEventListener('visibilitychange', onVisible);
  if (timer !== undefined) window.clearInterval(timer);
  timer = undefined;
}
