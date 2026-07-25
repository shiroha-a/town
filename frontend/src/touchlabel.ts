import type { Directive } from 'vue';

/**
 * Long-press label directive for touch devices.
 *
 * Desktop shows the element's `title` attribute on mouseover, but mobile
 * browsers have no hover. This directive shows the same label in a floating
 * bubble while the user long-presses the element, and suppresses the click
 * navigation and the OS context menu for that press.
 */

// ラベル表示用のバブルは全画面で1つを使い回す(bodyに遅延生成)。
let tip: HTMLDivElement | null = null;

function showTip(text: string, x: number, y: number) {
  if (!tip) {
    tip = document.createElement('div');
    tip.className = 'touch-label-tip';
    document.body.appendChild(tip);
  }
  tip.textContent = text;
  tip.style.display = 'block';
  // 幅確定後に指の上へ配置し、画面端でははみ出さないよう補正する。
  // 指で隠れないよう指先からやや大きめに離す。
  requestAnimationFrame(() => {
    if (!tip || tip.style.display === 'none') return;
    const pad = 8;
    const w = tip.offsetWidth;
    const left = Math.min(Math.max(x - w / 2, pad), window.innerWidth - w - pad);
    tip.style.left = `${left}px`;
    tip.style.top = `${Math.max(y - 80, pad)}px`;
  });
}

function hideTip() {
  if (tip) tip.style.display = 'none';
}

const HOLD_MS = 450;
const MOVE_CANCEL_PX = 12;

export const vTouchLabel: Directive<HTMLElement> = {
  mounted(el) {
    // iOSの画像保存コールアウトと文字選択を抑止する(長押しをラベル表示に使うため)
    el.style.setProperty('-webkit-touch-callout', 'none');
    el.style.setProperty('-webkit-user-select', 'none');
    el.style.setProperty('user-select', 'none');

    let timer: number | undefined;
    let sx = 0;
    let sy = 0;
    let shown = false;

    const clearTimer = () => {
      if (timer !== undefined) {
        window.clearTimeout(timer);
        timer = undefined;
      }
    };
    const end = () => {
      clearTimer();
      if (shown) hideTip();
    };

    el.addEventListener(
      'touchstart',
      (e) => {
        if (e.touches.length !== 1) return;
        const t = e.touches[0];
        sx = t.clientX;
        sy = t.clientY;
        shown = false;
        clearTimer();
        timer = window.setTimeout(() => {
          timer = undefined;
          // titleは動的に変わる(仕事クールタイム等)ため長押し時点の値を読む
          const label = el.getAttribute('title') || '';
          if (!label) return;
          shown = true;
          showTip(label, sx, sy);
        }, HOLD_MS);
      },
      { passive: true },
    );
    el.addEventListener(
      'touchmove',
      (e) => {
        const t = e.touches[0];
        if (t && Math.hypot(t.clientX - sx, t.clientY - sy) > MOVE_CANCEL_PX) end();
      },
      { passive: true },
    );
    el.addEventListener('touchend', (e) => {
      // ラベルを出した長押しはクリック(施設への移動等)を発火させない
      if (shown && e.cancelable) e.preventDefault();
      end();
    });
    el.addEventListener('touchcancel', end);
    el.addEventListener('contextmenu', (e) => {
      // 長押し中のOSコンテキストメニュー(画像保存等)を抑止する
      if (timer !== undefined || shown) e.preventDefault();
    });
  },
};
