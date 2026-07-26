<script setup lang="ts">
import { ref } from 'vue';
import { api, type Player } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const bets = [10000, 100000, 500000, 1000000];
const bet = ref(bets[0]);
const busy = ref(false);
const message = ref('');
const result = ref<{
  dice1: number;
  dice2: number;
  sum: number;
  result: string;
  win: boolean;
  net: number;
} | null>(null);

async function play(choice: 'even' | 'odd') {
  busy.value = true;
  message.value = '';
  try {
    const res = await api.casinoPlay(props.player.id, 'saikoro', bet.value, { choice });
    emit('update', res.player);
    const d = res.detail as { dice1: number; dice2: number; sum: number; result: string };
    result.value = { ...d, win: res.win, net: res.win ? res.payout - bet.value : -bet.value };
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="cg">
    <h3 class="cg-title">サイコロ</h3>
    <p class="cg-lead">2つのサイコロの合計が偶数か奇数かを当てる(当たれば掛け金と同額の配当)。</p>

    <div v-if="result" class="cg-result" :class="result.win ? 'win' : 'lose'" data-test="result">
      <span class="dice">{{ result.dice1 }} ＋ {{ result.dice2 }} ＝ {{ result.sum }}</span>
      <span class="band">{{ result.result === 'even' ? '偶数' : '奇数' }}</span>
      <span class="outcome">
        {{ result.win ? `当たり！ +${yen(result.net)}円` : `はずれ ${yen(result.net)}円` }}
      </span>
    </div>

    <div class="cg-controls">
      <label
        >掛け金：
        <select v-model.number="bet" data-test="bet">
          <option v-for="b in bets" :key="b" :value="b">{{ yen(b) }}円</option>
        </select>
      </label>
      <button class="btn" :disabled="busy" data-test="even" @click="play('even')">偶数に賭ける</button>
      <button class="btn" :disabled="busy" data-test="odd" @click="play('odd')">奇数に賭ける</button>
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
.cg-result {
  text-align: center;
  padding: 12px;
  border-radius: 6px;
  margin-bottom: 12px;
  font-size: 15px;
}
.cg-result .dice {
  font-size: 20px;
  font-weight: bold;
  margin-right: 10px;
}
.cg-result .band {
  font-weight: bold;
  margin-right: 10px;
}
.cg-result.win {
  background: #eaffea;
  color: #067a06;
}
.cg-result.lose {
  background: #ffecec;
  color: #cc2200;
}
.cg-result .outcome {
  font-weight: bold;
}
.cg-controls {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
</style>
