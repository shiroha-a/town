// 投稿本文に埋まった :name@host: を描画するための辞書。
//
// 本文にはショートコードしか保存されていないので、表示側でURLへ引き当てる。
// 辞書は「これまでに一度でも使われた絵文字」だけなので小さく、1回読めば足りる。
// 未知のショートコードはテキストのまま出す(相手インスタンスが落ちていても
// 投稿が壊れないようにする)。
import { ref } from 'vue';
import { api, type UsedEmoji } from './api';

const dict = ref<Map<string, UsedEmoji>>(new Map());
let loaded = false;
let loading: Promise<void> | null = null;

function key(name: string, host: string): string {
  return `${name}@${host.toLowerCase()}`;
}

/** Loads the emoji dictionary once per page load. */
export async function loadEmojiDict(force = false): Promise<void> {
  if (loaded && !force) return;
  if (loading && !force) return loading;
  loading = (async () => {
    try {
      const items = await api.usedEmojis();
      dict.value = new Map(items.map((e) => [key(e.name, e.host), e]));
      loaded = true;
    } finally {
      loading = null;
    }
  })();
  return loading;
}

/** Adds a freshly approved emoji so it renders without a reload. */
export function rememberEmoji(e: UsedEmoji): void {
  dict.value.set(key(e.name, e.host), e);
  dict.value = new Map(dict.value);
}

export function lookupEmoji(name: string, host: string): UsedEmoji | undefined {
  return dict.value.get(key(name, host));
}

export { dict as emojiDict };

export type Token =
  | { kind: 'text'; v: string }
  | { kind: 'link'; v: string }
  | { kind: 'emoji'; v: string; url: string; license?: string };

// :name@host: は投稿本文用(街の辞書を引く)、:name: はプロフィール用
// (その相手のインスタンスの辞書を渡してもらう)。
const pattern =
  /(https?:\/\/[\w.~\-/?&+=:@%;#]+)|:([a-zA-Z0-9_+-]+)@([a-zA-Z0-9.-]+):|:([a-zA-Z0-9_+-]+):/g;

/**
 * Splits text into plain runs, links and custom emoji. An emoji we have no url
 * for stays text, so nothing disappears from a post.
 *
 * @param local shortcode->url for text that carries its own dictionary
 *   (a Misskey profile's name/description), where shortcodes have no host.
 */
export function tokenize(text: string, local?: Record<string, string>): Token[] {
  const out: Token[] = [];
  let last = 0;
  for (const m of text.matchAll(pattern)) {
    const at = m.index ?? 0;
    if (at > last) out.push({ kind: 'text', v: text.slice(last, at) });
    if (m[1]) {
      out.push({ kind: 'link', v: m[1] });
    } else if (m[2]) {
      const e = lookupEmoji(m[2], m[3]);
      if (e) out.push({ kind: 'emoji', v: m[0], url: e.url, license: e.license });
      else out.push({ kind: 'text', v: m[0] });
    } else {
      const url = local?.[m[4]];
      if (url) out.push({ kind: 'emoji', v: m[0], url });
      else out.push({ kind: 'text', v: m[0] });
    }
    last = at + m[0].length;
  }
  if (last < text.length) out.push({ kind: 'text', v: text.slice(last) });
  return out;
}
