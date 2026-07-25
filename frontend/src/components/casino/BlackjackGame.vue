<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, type Player, type BJState } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const bets = [10000, 100000, 500000, 1000000];
const rate = ref(bets[0]);
const state = ref<BJState | null>(null);
const busy = ref(false);
const message = ref('');

// カード0-51をトランプ表記に(スート記号+ランク)。
const suits = ['♠', '♥', '♦', '♣']; // スペード/ハート/ダイヤ/クラブ
function cardLabel(card: number): string {
  const rank = (card % 13) + 1;
  const r = rank === 1 ? 'A' : rank === 11 ? 'J' : rank === 12 ? 'Q' : rank === 13 ? 'K' : String(rank);
  return `${suits[Math.floor(card / 13)]}${r}`;
}
// ハート/ダイヤは赤。
const isRed = (card: number) => Math.floor(card / 13) === 1 || Math.floor(card / 13) === 2;

const resultText = computed(() => {
  switch (state.value?.result) {
    case 'win':
      return 'あなたの勝ち！';
    case 'lose':
      return 'あなたの負け…';
    case 'push':
      return '引き分け';
    default:
      return '';
  }
});
const resultClass = computed(() =>
  state.value?.result === 'win' ? 'win' : state.value?.result === 'lose' ? 'lose' : 'push',
);

async function load() {
  try {
    state.value = await api.bjState(props.player.id);
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  }
}
onMounted(load);

async function run(fn: () => Promise<BJState>) {
  if (busy.value) return;
  busy.value = true;
  message.value = '';
  try {
    state.value = await fn();
    // 掛け金/払戻で所持金が変わるので再取得してヘッダを更新する。
    emit('update', await api.getPlayer(props.player.id));
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
const start = () => run(() => api.bjStart(props.player.id, rate.value));
const hit = () => run(() => api.bjHit(props.player.id));
const stand = () => run(() => api.bjStand(props.player.id));
function reset() {
  if (state.value) state.value = { ...state.value, active: false };
  message.value = '';
}
</script>

<template>
  <div class="cg">
    <h3 class="cg-title">ブラックジャック</h3>
    <p class="cg-lead">21に近い方が勝ち(超えたら負け)。ディーラーは17以上で止まる。配当1:1。</p>

    <!-- 未開始 -->
    <div v-if="!state?.active" class="cg-controls">
      <label
        >掛け金：
        <select v-model.number="rate" data-test="bet">
          <option v-for="b in bets" :key="b" :value="b">{{ yen(b) }}円</option>
        </select>
      </label>
      <button class="btn" :disabled="busy" data-test="start" @click="start">ゲーム開始</button>
    </div>

    <!-- 進行中/決着 -->
    <div v-else>
      <div class="hand">
        <div class="hand-label">
          ディーラー（{{ state.phase === 'over' ? state.oya_score : `${state.oya_score} + ?` }}）
        </div>
        <div class="cards">
          <span v-for="(c, i) in state.oya" :key="i" class="card" :class="{ red: isRed(c) }">{{ cardLabel(c) }}</span>
          <span v-for="n in state.oya_hidden" :key="'h' + n" class="card back">?</span>
        </div>
      </div>
      <div class="hand">
        <div class="hand-label">あなた（{{ state.ply_score }}）</div>
        <div class="cards">
          <span v-for="(c, i) in state.ply" :key="i" class="card" :class="{ red: isRed(c) }">{{ cardLabel(c) }}</span>
        </div>
      </div>

      <div v-if="state.phase === 'playing'" class="cg-controls">
        <button class="btn" :disabled="busy" data-test="hit" @click="hit">ヒット</button>
        <button class="btn" :disabled="busy" data-test="stand" @click="stand">スタンド</button>
      </div>
      <div v-else class="cg-result" :class="resultClass" data-test="result">
        {{ resultText }}
        <span class="net">{{ state.payout > 0 ? `+${yen(state.payout - state.rate)}円` : `${yen(-state.rate)}円` }}</span>
        <button class="btn" :disabled="busy" data-test="again" @click="reset">もう一度</button>
      </div>
    </div>

    <div v-if="message" class="message error">{{ message }}</div>
  </div>
</template>

<style scoped>
.cg {
  max-width: 560px;
  margin: 0 auto;
  background: #fff;
  border: 1px solid #7a5cff;
  border-radius: 8px;
  padding: 14px 16px;
}
.cg-title {
  margin: 0 0 4px;
  color: #6a2fb5;
}
.cg-lead {
  font-size: 12px;
  color: #555;
  margin: 0 0 12px;
}
.hand {
  margin-bottom: 12px;
}
.hand-label {
  font-size: 12px;
  color: #6a2fb5;
  font-weight: bold;
  margin-bottom: 4px;
}
.cards {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.card {
  min-width: 40px;
  height: 54px;
  border: 1px solid #999;
  border-radius: 5px;
  background: #fff;
  color: #222;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  font-weight: bold;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.15);
}
.card.red {
  color: #cc0000;
}
.card.back {
  background: linear-gradient(135deg, #6a2fb5, #a06cff);
  color: #fff;
}
.cg-controls {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.cg-result {
  padding: 10px;
  border-radius: 6px;
  font-weight: bold;
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}
.cg-result.win {
  background: #eaffea;
  color: #067a06;
}
.cg-result.lose {
  background: #ffecec;
  color: #cc2200;
}
.cg-result.push {
  background: #f0f0f0;
  color: #555;
}
.cg-result .net {
  font-size: 13px;
}
</style>
