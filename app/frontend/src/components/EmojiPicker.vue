<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, type PickerEmoji } from '../api';
import { rememberEmoji } from '../emoji';

// カスタム絵文字のピッカー。インスタンスによっては1000個近くあるので、
// カテゴリ絞り込みと名前検索が無いと使い物にならない。
//
// 一覧に載っていても使えるとは限らない(ライセンスの有無は一覧APIに含まれない)。
// 選んだ時点でサーバに問い合わせて確定させ、駄目なら理由を出す。
const emit = defineEmits<{ pick: [shortcode: string]; close: [] }>();

const MAX_SHOWN = 240;

const host = ref('');
const hostInput = ref('');
const items = ref<PickerEmoji[]>([]);
const category = ref('');
const query = ref('');
const loading = ref(false);
const message = ref('');
const busyName = ref('');
// 判定済みの絵文字。'ok' か拒否理由が入る。使えないものに印を付けるために持つ。
const verdicts = ref<Record<string, string>>({});

const REASON_LABEL: Record<string, string> = {
  no_license: 'ライセンスの表記がありません',
  sensitive: 'センシティブ指定です',
  local_only: '連合しない設定です',
  not_found: '見つかりませんでした',
};

function rejectedReason(name: string): string | null {
  const v = verdicts.value[name];
  return v && v !== 'ok' ? v : null;
}

function tip(name: string): string {
  const r = rejectedReason(name);
  return r ? `:${name}: — 使えません（${REASON_LABEL[r] ?? r}）` : `:${name}:`;
}

const categories = computed(() => {
  const set = new Set<string>();
  for (const e of items.value) set.add(e.category || '');
  return [...set].sort();
});

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase();
  return items.value.filter((e) => {
    if (category.value && (e.category || '') !== category.value) return false;
    if (!q) return true;
    if (e.name.toLowerCase().includes(q)) return true;
    return (e.aliases ?? []).some((a) => a.toLowerCase().includes(q));
  });
});

const shown = computed(() => filtered.value.slice(0, MAX_SHOWN));

async function load(h?: string) {
  loading.value = true;
  message.value = '';
  try {
    const res = await api.emojiList(h);
    host.value = res.host;
    hostInput.value = res.host;
    items.value = res.emojis;
    verdicts.value = res.verdicts ?? {};
    category.value = '';
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
    items.value = [];
  } finally {
    loading.value = false;
  }
}

async function pick(e: PickerEmoji) {
  if (busyName.value) return;
  busyName.value = e.name;
  message.value = '';
  try {
    const res = await api.resolveEmoji(host.value, e.name);
    if (!res.allowed) {
      message.value = res.message ?? 'この絵文字は使えません。';
      // その場で印を付ける。押すまで分からないままにしない。
      verdicts.value = { ...verdicts.value, [e.name]: res.reason ?? 'no_license' };
      return;
    }
    verdicts.value = { ...verdicts.value, [e.name]: 'ok' };
    if (res.emoji) rememberEmoji(res.emoji);
    emit('pick', res.shortcode ?? `:${e.name}@${host.value}:`);
  } catch (err) {
    message.value = err instanceof Error ? err.message : String(err);
  } finally {
    busyName.value = '';
  }
}

onMounted(() => load());
</script>

<template>
  <div class="ep-backdrop" @click.self="emit('close')">
    <div class="ep-modal">
      <div class="ep-head">
        <span class="ep-title">絵文字を選ぶ</span>
        <button class="ep-close" @click="emit('close')">×</button>
      </div>

      <div class="ep-controls">
        <input
          v-model="hostInput"
          class="ep-host"
          placeholder="misskey.example"
          spellcheck="false"
          @keydown.enter="load(hostInput)"
        />
        <button class="btn" :disabled="loading" @click="load(hostInput)">読み込む</button>
        <select v-model="category" class="ep-cat">
          <option value="">すべてのカテゴリ</option>
          <option v-for="c in categories" :key="c" :value="c">{{ c || '(未分類)' }}</option>
        </select>
        <input v-model="query" class="ep-search" placeholder="名前で検索" spellcheck="false" />
      </div>

      <div v-if="message" class="ep-message">{{ message }}</div>

      <div class="ep-body">
        <div v-if="loading" class="ep-note">読み込んでいます…</div>
        <template v-else>
          <div v-if="!shown.length" class="ep-note">該当する絵文字がありません。</div>
          <button
            v-for="e in shown"
            :key="e.name"
            class="ep-item"
            :class="{ busy: busyName === e.name, unusable: !!rejectedReason(e.name) }"
            :title="tip(e.name)"
            @click="pick(e)"
          >
            <img :src="e.url" :alt="e.name" loading="lazy" />
          </button>
        </template>
      </div>

      <div class="ep-foot">
        {{ host }} の絵文字 {{ filtered.length }}件
        <span v-if="filtered.length > MAX_SHOWN">（{{ MAX_SHOWN }}件まで表示。検索で絞り込んでください）</span>
        <span class="ep-lic">※ライセンスが設定された絵文字のみ使えます</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ep-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1100;
  padding: 12px;
}
.ep-modal {
  background: #fff;
  border: 1px solid #333;
  width: min(680px, 100%);
  max-height: min(560px, 90vh);
  display: flex;
  flex-direction: column;
}
.ep-head {
  display: flex;
  align-items: center;
  background: #997a44;
  color: #fff;
  padding: 5px 8px;
}
.ep-title {
  flex: 1 1 auto;
  font-size: 13px;
  font-weight: bold;
}
.ep-close {
  border: 0;
  background: none;
  color: #fff;
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
}
.ep-controls {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 8px;
  border-bottom: 1px solid #ddd;
}
.ep-host {
  flex: 1 1 160px;
  font-size: 12px;
  padding: 3px 5px;
}
.ep-cat {
  flex: 0 1 180px;
  font-size: 12px;
}
.ep-search {
  flex: 1 1 140px;
  font-size: 12px;
  padding: 3px 5px;
}
.ep-message {
  background: #ffeeee;
  border-bottom: 1px solid #ecc;
  color: #a33;
  font-size: 12px;
  padding: 6px 8px;
}
.ep-body {
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 8px;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-content: flex-start;
}
.ep-note {
  color: #666;
  font-size: 12px;
  padding: 12px;
}
.ep-item {
  border: 1px solid transparent;
  background: none;
  padding: 3px;
  cursor: pointer;
  line-height: 0;
}
.ep-item:hover {
  border-color: #997a44;
  background: #f5f0e0;
}
.ep-item.busy {
  opacity: 0.4;
}
/* 使えないと判定済みの絵文字。灰色にして斜線を引く。押すと理由が出る
   (拒否キャッシュが切れていれば再判定されて印が消える)。 */
.ep-item.unusable {
  position: relative;
}
.ep-item.unusable img {
  filter: grayscale(1);
  opacity: 0.35;
}
.ep-item.unusable::after {
  content: '';
  position: absolute;
  inset: 3px;
  background: linear-gradient(
    to bottom right,
    transparent calc(50% - 1px),
    #c33 calc(50% - 1px),
    #c33 calc(50% + 1px),
    transparent calc(50% + 1px)
  );
}
.ep-item img {
  width: 30px;
  height: 30px;
  object-fit: contain;
}
.ep-foot {
  border-top: 1px solid #ddd;
  padding: 5px 8px;
  font-size: 11px;
  color: #777;
}
.ep-lic {
  margin-left: 8px;
  color: #996600;
}
@media (max-width: 700px) {
  .ep-item img {
    width: 34px;
    height: 34px;
  }
}
</style>
