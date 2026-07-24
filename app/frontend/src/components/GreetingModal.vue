<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { api, type Player } from '../api';

// あいさつのSNS風投稿モーダル(旧ChatViewの専用ページを置き換え)。
// 投稿フォームのみのコンパクトなモーダル。最新の投稿は街トップの
// チャット窓に表示される。投稿すると報酬(ランダム+ジャンケンで倍/半)がもらえる。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{
  update: [player: Player];
  close: [];
  // 投稿成功: 結果メッセージ(報酬/ジャンケン/罰金)を親のトーストで表示する。
  posted: [lines: string[], good: boolean];
}>();

const isAdmin = computed(() => props.player.roles.includes('admin'));

const CATEGORIES = ['あいさつ', '雑談', '今日の出来事', '今の気分', 'なんとなく', 'お話ししよう', '宣伝'];
const COLORS = [
  { label: '黒', value: '#333333' },
  { label: '緑', value: '#009933' },
  { label: '茶', value: '#996633' },
  { label: '紫', value: '#663399' },
  { label: '桃', value: '#cc3399' },
  { label: '橙', value: '#ff6600' },
  { label: '紺', value: '#333399' },
];
const JANKEN = [
  { label: 'ジャンケンしない', value: 'none' },
  { label: 'おまかせ', value: 'omakase' },
  { label: 'グー', value: 'gu' },
  { label: 'チョキ', value: 'choki' },
  { label: 'パー', value: 'pa' },
];
const MAX_LEN = 60;

const category = ref('あいさつ');
const body = ref('');
const color = ref('#333333');
const janken = ref('none');
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);

const yen = (n: number) => n.toLocaleString('ja-JP');
const remain = computed(() => MAX_LEN - body.value.length);

function fail(e: unknown) {
  message.value = e instanceof Error ? e.message : String(e);
  kind.value = 'error';
}

async function post() {
  if (!body.value.trim()) {
    message.value = 'ひとことを入力してください。';
    kind.value = 'error';
    return;
  }
  busy.value = true;
  message.value = '';
  try {
    const res = await api.postGreeting(props.player.id, category.value, body.value, color.value, janken.value);
    emit('update', res.player);
    const r = res.result;
    const lines: string[] = [];
    if (r.janken) lines.push(`ジャンケン${r.janken}(相手は${r.janken_pc})`);
    if (r.jackpot) lines.push('大当たり！');
    if (r.reward > 0) lines.push(`${yen(r.reward)}円もらいました。`);
    else if (r.reward < 0) lines.push(`${yen(-r.reward)}円払いました。`);
    if (r.fine) lines.push('NGワードで罰金30,000円!');
    body.value = '';
    // 投稿できたらフォームは閉じ、結果は親(街トップ)のトーストで見せる。
    emit('posted', lines, !r.fine && r.reward >= 0);
    emit('close');
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

// Escで閉じる。
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close');
}
onMounted(() => window.addEventListener('keydown', onKey));
onUnmounted(() => window.removeEventListener('keydown', onKey));
</script>

<template>
  <div class="gm-overlay" @click.self="emit('close')">
    <div class="gm-card" role="dialog" aria-label="あいさつ">
      <div class="gm-head">
        <span class="gm-title">あいさつ</span>
        <button class="gm-close" aria-label="閉じる" @click="emit('close')">×</button>
      </div>

      <!-- 投稿フォーム(SNS風コンポーザ) -->
      <div class="gm-compose">
        <textarea
          v-model="body"
          :maxlength="MAX_LEN"
          rows="3"
          class="gm-input"
          placeholder="いまなにしてる？（60字まで）"
          @keydown.ctrl.enter="post"
        ></textarea>
        <div class="gm-opts">
          <select v-model="category" title="種類">
            <option v-for="c in CATEGORIES" :key="c" :value="c">{{ c }}</option>
            <option v-if="isAdmin" value="管理人">管理人</option>
          </select>
          <select v-model="color" title="文字色" :style="{ color }">
            <option v-for="c in COLORS" :key="c.value" :value="c.value" :style="{ color: c.value }">
              ●{{ c.label }}
            </option>
          </select>
          <select v-model="janken" title="ジャンケン">
            <option v-for="j in JANKEN" :key="j.value" :value="j.value">{{ j.label }}</option>
          </select>
          <span class="gm-remain" :class="{ low: remain <= 10 }">{{ remain }}</span>
          <button class="gm-post" :disabled="busy || !body.trim()" @click="post">投稿</button>
        </div>
        <div class="gm-hint">
          投稿すると報酬がもらえます（ジャンケンに勝つと倍、負けると半分）。「宣伝」は2万円、NGワードは罰金。
        </div>
        <div v-if="message" :class="['gm-message', kind]">{{ message }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.gm-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  z-index: 1000;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 6vh 12px 12px;
}
.gm-card {
  width: 500px;
  max-width: 100%;
  max-height: 84vh;
  background: #fff;
  border-radius: 10px;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.3);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.gm-head {
  display: flex;
  align-items: center;
  padding: 8px 12px;
  border-bottom: 1px solid #e4e0d2;
  background: #f8f5ea;
}
.gm-title {
  font-weight: bold;
  color: #663300;
}
.gm-close {
  margin-left: auto;
  border: 0;
  background: none;
  font-size: 20px;
  line-height: 1;
  color: #999;
  cursor: pointer;
  padding: 2px 6px;
}
.gm-close:hover {
  color: #333;
}
.gm-compose {
  padding: 10px 12px;
}
.gm-input {
  width: 100%;
  box-sizing: border-box;
  border: 1px solid #ccc;
  border-radius: 6px;
  padding: 8px;
  font-size: 14px;
  resize: none;
}
.gm-input:focus {
  outline: none;
  border-color: #cc6600;
}
.gm-opts {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  flex-wrap: wrap;
}
.gm-opts select {
  font-size: 12px;
  padding: 2px 4px;
}
.gm-remain {
  margin-left: auto;
  font-size: 12px;
  color: #999;
}
.gm-remain.low {
  color: #cc3333;
  font-weight: bold;
}
.gm-post {
  background: #cc6600;
  color: #fff;
  border: 1px solid #994c00;
  border-radius: 14px;
  padding: 3px 18px;
  font-size: 13px;
  font-weight: bold;
  cursor: pointer;
}
.gm-post:disabled {
  opacity: 0.5;
  cursor: default;
}
.gm-hint {
  margin-top: 6px;
  font-size: 10px;
  color: #999;
  line-height: 1.5;
}
.gm-message {
  margin-top: 6px;
  font-size: 12px;
  padding: 5px 8px;
  border-radius: 4px;
}
.gm-message.ok {
  background: #eef7e8;
  color: #2a6a2a;
}
.gm-message.error {
  background: #fbe9e9;
  color: #b33;
}
</style>
