<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player, type PokerState } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const state = ref<PokerState | null>(null);
const hold = ref<boolean[]>([false, false, false, false, false]);
const busy = ref(false);
const message = ref('');

const suits = ['♠', '♥', '♦', '♣'];
function cardLabel(card: number): string {
  const rank = (card % 13) + 1;
  const r = rank === 1 ? 'A' : rank === 11 ? 'J' : rank === 12 ? 'Q' : rank === 13 ? 'K' : String(rank);
  return `${suits[Math.floor(card / 13)]}${r}`;
}
const isRed = (card: number) => Math.floor(card / 13) === 1 || Math.floor(card / 13) === 2;

async function load() {
  try {
    state.value = await api.pokerState(props.player.id);
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  }
}
onMounted(load);

async function run(fn: () => Promise<PokerState>) {
  if (busy.value) return;
  busy.value = true;
  message.value = '';
  try {
    state.value = await fn();
    emit('update', await api.getPlayer(props.player.id));
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
const buy = () => run(() => api.pokerBuy(props.player.id));
const deal = () =>
  run(() => {
    hold.value = [false, false, false, false, false];
    return api.pokerDeal(props.player.id);
  });
const draw = () =>
  run(() =>
    api.pokerDraw(
      props.player.id,
      hold.value.map((h, i) => (h ? i : -1)).filter((i) => i >= 0),
    ),
  );
const cashout = () => run(() => api.pokerCashout(props.player.id));
</script>

<template>
  <div class="cg" v-if="state">
    <h3 class="cg-title">ポーカー</h3>
    <p class="cg-lead">
      5000円で5ポイント購入。配札→キープするカードを選んで交換→役でポイント増減。清算で1000円/ポイント換金(1点は手数料)。
    </p>

    <div class="pts">所持ポイント：<span class="pv">{{ state.points }}</span></div>

    <div
      v-if="state.result >= 0"
      class="cg-result"
      :class="state.gain >= 0 ? 'win' : 'lose'"
      data-test="result"
    >
      {{ state.result_name }}（{{ state.gain >= 0 ? '+' : '' }}{{ state.gain }}点）
    </div>

    <div v-if="state.phase === 'dealt'" class="hand">
      <div v-for="(c, i) in state.hand" :key="i" class="cardwrap">
        <span class="card" :class="{ red: isRed(c) }">{{ cardLabel(c) }}</span>
        <label class="keep"><input type="checkbox" v-model="hold[i]" :data-test="`hold-${i}`" /> キープ</label>
      </div>
    </div>

    <div class="cg-controls">
      <button v-if="!state.active" class="btn" :disabled="busy" data-test="buy" @click="buy">
        5000円で購入（5ポイント）
      </button>
      <button v-if="state.phase === 'ready'" class="btn" :disabled="busy" data-test="deal" @click="deal">
        配札する
      </button>
      <button v-if="state.phase === 'dealt'" class="btn" :disabled="busy" data-test="draw" @click="draw">
        交換して勝負
      </button>
      <button
        v-if="state.active && state.phase === 'ready'"
        class="btn cashout"
        :disabled="busy"
        data-test="cashout"
        @click="cashout"
      >
        清算する（{{ yen(Math.max(0, state.points - 1) * 1000) }}円）
      </button>
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
  margin: 0 0 10px;
}
.pts {
  font-size: 14px;
  margin-bottom: 10px;
}
.pts .pv {
  font-size: 18px;
  font-weight: bold;
  color: #6a2fb5;
}
.cg-result {
  padding: 8px 10px;
  border-radius: 6px;
  margin-bottom: 12px;
  font-weight: bold;
}
.cg-result.win {
  background: #eaffea;
  color: #067a06;
}
.cg-result.lose {
  background: #ffecec;
  color: #cc2200;
}
.hand {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}
.cardwrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}
.card {
  min-width: 42px;
  height: 56px;
  border: 1px solid #999;
  border-radius: 5px;
  background: #fff;
  color: #222;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 17px;
  font-weight: bold;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.15);
}
.card.red {
  color: #cc0000;
}
.keep {
  font-size: 11px;
  color: #6a2fb5;
}
.cg-controls {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.btn.cashout {
  background: #ffd700;
  color: #6a2fb5;
}
</style>
