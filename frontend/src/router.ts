// 画面とURLの対応。
//
// これまで表示中の画面はコンポーネント内の変数だけで持っていたため、リロードすると
// 街トップへ戻り、ブラウザの戻るボタンも効かず、特定の画面へのリンクも作れなかった。
// URLに出すことで、リロード・戻る・共有・ホーム画面のショートカットが成立する。
//
// ルーターのライブラリは入れない。画面は1階層で、パラメータも家のIDと建築マスだけ
// なので、History APIを直接使うほうが依存も分岐も少なく済む。

/** 家訪問(house)と建設会社(kentiku)だけがパラメータを取る。 */
export type NavParam = number | { town: number; row: number; col: number } | null;

export interface Route {
  view: string;
  param: NavParam;
}

/** 画面キーとURLの対応。街トップだけは "/" にする。 */
const TOWN = 'town';

/** Builds the path for a view. */
export function pathFor(view: string, param?: NavParam): string {
  if (view === TOWN) return '/';
  if (view === 'house' && typeof param === 'number') return `/house/${param}`;
  if (view === 'kentiku' && param && typeof param === 'object') {
    const { town, row, col } = param;
    return `/kentiku?town=${town}&row=${row}&col=${col}`;
  }
  return `/${view}`;
}

/** Parses the current location into a view and its parameter. */
export function parsePath(path: string, search = ''): Route {
  const seg = path.replace(/^\/+|\/+$/g, '').split('/');
  const head = seg[0] ?? '';
  if (head === '') return { view: TOWN, param: null };

  if (head === 'house') {
    const id = Number(seg[1]);
    return { view: 'house', param: Number.isFinite(id) && id > 0 ? id : null };
  }
  if (head === 'kentiku') {
    const q = new URLSearchParams(search);
    const nums = ['town', 'row', 'col'].map((k) => Number(q.get(k)));
    const ok = nums.every((n) => Number.isFinite(n));
    return {
      view: 'kentiku',
      param: ok ? { town: nums[0], row: nums[1], col: nums[2] } : null,
    };
  }
  return { view: head, param: null };
}

/** Reads the route from the address bar. */
export function currentRoute(): Route {
  return parsePath(window.location.pathname, window.location.search);
}

/** Pushes a new route unless it is already the current one. */
export function pushRoute(view: string, param?: NavParam): void {
  const path = pathFor(view, param);
  if (path === window.location.pathname + window.location.search) return;
  window.history.pushState({ view }, '', path);
}

/** Replaces the current entry (起動時の正規化に使う)。 */
export function replaceRoute(view: string, param?: NavParam): void {
  window.history.replaceState({ view }, '', pathFor(view, param));
}
