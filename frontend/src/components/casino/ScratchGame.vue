<script setup lang="ts">
import { ref, onMounted, watch } from 'vue';
import { api, type Player, type ScratchState } from '../../api';

const props = defineProps<{ player: Player; game: string }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const state = ref<ScratchState | null>(null);
const busy = ref(false);
const message = ref('');
const lastPrize = ref<number | null>(null);
const lastBonus = ref(false);

async function load() {
  message.value = '';
  try {
    state.value = await api.scratchState(props.player.id, props.game);
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  }
}
onMounted(load);
// scratch/sukurattiを切り替えたら読み直す。
watch(() => props.game, load);

async function open(card: number, cell: number) {
  const c = state.value?.cards[card];
  if (busy.value || !c || c.finished || c.values[cell] !== undefined) return;
  busy.value = true;
  message.value = '';
  lastPrize.value = null;
  lastBonus.value = false;
  try {
    const res = await api.scratchOpen(props.player.id, props.game, card, cell);
    emit('update', res.player);
    state.value = res.state;
    lastPrize.value = res.prize;
    lastBonus.value = res.bonus;
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div v-if="state" class="cg">
    <h3 class="cg-title">{{ game === 'sukuratti' ? 'スクラッチ2' : 'スクラッチ' }}</h3>
    <p class="cg-lead">
      1日{{ state.cards.length }}枚。各カード{{ state.open_max }}マスまで開けられ、{{ state.atari_max }}以下が当たり(1マス
      +{{ yen(100000) }}円)。開けた{{ state.open_max }}マスが全て当たりならボーナス+{{ yen(300000) }}円。無料。
    </p>

    <div
      v-if="lastPrize !== null"
      class="cg-result"
      :class="lastPrize > 0 ? 'win' : 'lose'"
      data-test="result"
    >
      {{ lastPrize > 0 ? `当たり！ +${yen(lastPrize)}円` : 'はずれ…' }}
      <span v-if="lastBonus">（ボーナス達成！）</span>
    </div>

    <div class="cards">
      <div v-for="c in state.cards" :key="c.index" class="card" :class="{ finished: c.finished }">
        <div class="card-head">カード{{ c.index + 1 }}（{{ c.opened }}/{{ state.open_max }}）</div>
        <div class="grid" :style="{ gridTemplateColumns: `repeat(${state.cols}, 1fr)` }">
          <button
            v-for="i in state.cells"
            :key="i - 1"
            class="cell"
            :class="{
              open: c.values[i - 1] !== undefined,
              atari: c.values[i - 1] !== undefined && c.values[i - 1] <= state.atari_max,
            }"
            :disabled="busy || c.finished || c.values[i - 1] !== undefined"
            @click="open(c.index, i - 1)"
          >
            {{ c.values[i - 1] !== undefined ? c.values[i - 1] : '?' }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="message" class="message error">{{ message }}</div>
  </div>
</template>

<style scoped>
.cg {
  max-width: 660px;
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
.cg-result {
  text-align: center;
  padding: 8px;
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
.cards {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: center;
}
.card {
  border: 1px solid #cbb8ff;
  border-radius: 6px;
  padding: 6px 8px;
}
.card.finished {
  opacity: 0.55;
}
.card-head {
  font-size: 11px;
  color: #6a2fb5;
  margin-bottom: 4px;
  text-align: center;
}
.grid {
  display: grid;
  gap: 3px;
}
.cell {
  width: 34px;
  height: 34px;
  border: 1px solid #b9a0e0;
  border-radius: 4px;
  background: #ede7fb;
  color: #6a2fb5;
  font-weight: bold;
  cursor: pointer;
  font-size: 15px;
}
.cell:disabled {
  cursor: default;
}
.cell.open {
  background: #f2f2ea;
  color: #999;
}
.cell.atari {
  background: #ffd34d;
  color: #6a2fb5;
}
</style>
