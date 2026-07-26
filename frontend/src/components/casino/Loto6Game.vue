<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, type Player, type Loto6State } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const state = ref<Loto6State | null>(null);
const picked = ref<number[]>([]);
const busy = ref(false);
const message = ref('');

const numbers = Array.from({ length: 36 }, (_, i) => i + 1);
const canBuy = computed(() => picked.value.length === 6 && !busy.value);

function toggle(n: number) {
  const i = picked.value.indexOf(n);
  if (i >= 0) picked.value.splice(i, 1);
  else if (picked.value.length < 6) picked.value.push(n);
}
function randomPick() {
  const pool = [...numbers];
  const out: number[] = [];
  for (let k = 0; k < 6; k++) out.push(pool.splice(Math.floor(Math.random() * pool.length), 1)[0]);
  picked.value = out.sort((a, b) => a - b);
}

async function load() {
  try {
    state.value = await api.loto6State(props.player.id);
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  }
}
onMounted(load);

async function buy() {
  if (!canBuy.value) return;
  busy.value = true;
  message.value = '';
  try {
    state.value = await api.loto6Buy(props.player.id, [...picked.value].sort((a, b) => a - b));
    emit('update', await api.getPlayer(props.player.id));
    picked.value = [];
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="cg" v-if="state">
    <h3 class="cg-title">ロト6</h3>
    <p class="cg-lead">
      1〜36から6個選んで購入(1口{{ yen(state.cost) }}円、1日{{ state.daily_limit }}口まで)。毎日AM5:00に抽選し、
      一致数に応じた賞金が銀行普通口座へ振り込まれます。
    </p>

    <div v-if="state.last_draw" class="last-draw">
      <span class="ld-label">前回抽選（{{ state.last_draw.date }}）：</span>
      <span v-for="n in state.last_draw.winning" :key="n" class="ball win">{{ n }}</span>
    </div>

    <div class="pick-label">数字を6個選ぶ（{{ picked.length }}/6）</div>
    <div class="grid">
      <button
        v-for="n in numbers"
        :key="n"
        class="numbtn"
        :class="{ on: picked.includes(n) }"
        :disabled="busy"
        @click="toggle(n)"
      >
        {{ n }}
      </button>
    </div>

    <div class="cg-controls">
      <button class="btn" :disabled="busy" data-test="random" @click="randomPick">ランダム</button>
      <button class="btn buy" :disabled="!canBuy" data-test="buy" @click="buy">
        購入（{{ yen(state.cost) }}円）
      </button>
      <span class="count">本日：{{ state.today_count }}/{{ state.daily_limit }}口</span>
    </div>

    <div v-if="state.my_tickets.length" class="mine">
      <div class="mine-label">本日の購入券</div>
      <div v-for="(t, i) in state.my_tickets" :key="i" class="ticket">
        <span v-for="n in t.numbers" :key="n" class="ball">{{ n }}</span>
      </div>
    </div>

    <div v-if="message" class="message error">{{ message }}</div>
  </div>
</template>

<style scoped>
.cg {
  max-width: 600px;
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
.last-draw {
  margin-bottom: 10px;
  font-size: 13px;
}
.ld-label {
  color: #6a2fb5;
}
.pick-label {
  font-size: 13px;
  color: #6a2fb5;
  font-weight: bold;
  margin-bottom: 6px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(9, 1fr);
  gap: 4px;
  margin-bottom: 10px;
}
.numbtn {
  aspect-ratio: 1;
  border: 1px solid #b9a0e0;
  border-radius: 4px;
  background: #ede7fb;
  color: #6a2fb5;
  font-weight: bold;
  cursor: pointer;
  font-size: 13px;
}
.numbtn.on {
  background: #ffd34d;
  color: #6a2fb5;
  border-color: #e0a800;
}
.cg-controls {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.btn.buy {
  background: #ffd700;
  color: #6a2fb5;
}
.count {
  font-size: 12px;
  color: #555;
}
.mine-label {
  font-size: 12px;
  color: #6a2fb5;
  font-weight: bold;
  margin-bottom: 4px;
}
.ticket {
  margin-bottom: 3px;
}
.ball {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: #ede7fb;
  color: #6a2fb5;
  font-size: 12px;
  font-weight: bold;
  margin-right: 3px;
}
.ball.win {
  background: #ffd34d;
}
</style>
