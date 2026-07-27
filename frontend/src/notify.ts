// 通知(Web Push)の購読まわり。
//
// 既定はオフ。ユーザー設定でオンにしたときに初めてブラウザの許可を求める。
// 許可のダイアログは、頼んでもいないのに出ると嫌われるため。
//
// iOSはホーム画面に追加したときだけ届く(Safariのタブでは購読自体が作れない)。

import { api } from './api';

/** この端末で通知を扱えるか。 */
export function pushSupported(): boolean {
  return 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;
}

/** 許可の状態。'granted' なら出せる。 */
export function permission(): NotificationPermission | 'unsupported' {
  return pushSupported() ? Notification.permission : 'unsupported';
}

/** base64url の公開鍵を、PushManager が要求する形へ直す。 */
function toKeyBytes(base64url: string): ArrayBuffer {
  const pad = '='.repeat((4 - (base64url.length % 4)) % 4);
  const raw = atob((base64url + pad).replace(/-/g, '+').replace(/_/g, '/'));
  const buf = new ArrayBuffer(raw.length);
  const view = new Uint8Array(buf);
  for (let i = 0; i < raw.length; i++) view[i] = raw.charCodeAt(i);
  return buf;
}

/** 購読の鍵を base64url の文字列で取り出す。 */
function keyOf(sub: PushSubscription, name: 'p256dh' | 'auth'): string {
  const buf = sub.getKey(name);
  if (!buf) return '';
  const bytes = new Uint8Array(buf);
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/** この端末が登録済みか。人ごとの設定とは別で、端末ごとに要る。 */
export async function deviceSubscribed(): Promise<boolean> {
  if (!pushSupported()) return false;
  try {
    const reg = await navigator.serviceWorker.ready;
    return (await reg.pushManager.getSubscription()) !== null;
  } catch {
    return false;
  }
}

/**
 * この端末を通知の宛先として登録する。許可が要る場合はここで求める。
 * 返り値は登録できたか。断られた場合は false。
 */
export async function subscribe(playerID: number): Promise<boolean> {
  if (!pushSupported()) return false;
  const perm = await Notification.requestPermission();
  if (perm !== 'granted') return false;

  const reg = await navigator.serviceWorker.ready;
  const { key } = await api.pushKey();
  if (!key) return false;

  // 既に購読があればそれを使う(作り直すと宛先が変わって通知が二重になる)。
  const sub =
    (await reg.pushManager.getSubscription()) ??
    (await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: toKeyBytes(key),
    }));

  await api.pushSubscribe(playerID, {
    endpoint: sub.endpoint,
    p256dh: keyOf(sub, 'p256dh'),
    auth: keyOf(sub, 'auth'),
  });
  return true;
}

/** この端末の登録を解除する。 */
export async function unsubscribe(playerID: number): Promise<void> {
  if (!pushSupported()) return;
  const reg = await navigator.serviceWorker.ready;
  const sub = await reg.pushManager.getSubscription();
  if (!sub) return;
  await api.pushUnsubscribe(playerID, { endpoint: sub.endpoint, p256dh: '', auth: '' });
  await sub.unsubscribe().catch(() => {});
}
