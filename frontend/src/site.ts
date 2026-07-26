// 画面に出すゲーム名と副題。管理画面から変えられるのでサーバーから取る。
// ログイン前の入口でも使うため公開APIから読む。
import { ref } from 'vue';
import { api } from './api';

const DEFAULT_TITLE = 'ＴＯＷＮ';

export const siteTitle = ref(DEFAULT_TITLE);
export const siteTagline = ref('');

let loaded = false;

/** Loads the site name once per page load and reflects it in the tab title. */
export async function loadSite(): Promise<void> {
  if (loaded) return;
  try {
    const s = await api.site();
    // 空が返ってきても見出しが消えないよう既定に落とす。
    siteTitle.value = s.title || DEFAULT_TITLE;
    siteTagline.value = s.tagline ?? '';
    document.title = siteTitle.value;
    loaded = true;
  } catch {
    // 取得できなくても画面は出す(既定の名前のまま)。
  }
}
