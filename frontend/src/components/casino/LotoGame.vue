<script setup lang="ts">
import { ref } from 'vue';
import { api, type Player } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const bets = [500, 1000, 5000, 10000, 50000, 100000];
const bet = ref(bets[0]);
const digitOptions = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9];
// 6桁の予想(各位置0-9)。旧loto.cgiのloto1..loto6に対応。
const picks = ref<number[]>([0, 0, 0, 0, 0, 0]);
const busy = ref(false);
const message = ref('');
// 一致桁数→倍率の対応表(表示用)。
const paytable = [
  { hit: 1, bai: 2 },
  { hit: 2, bai: 5 },
  { hit: 3, bai: 20 },
  { hit: 4, bai: 100 },
  { hit: 5, bai: 500 },
  { hit: 6, bai: 1000 },
];
const result = ref<{
  picks: number[];
  draw: number[];
  matches: number;
  multiplier: number;
  win: boolean;
  net: number;
} | null>(null);

async function play() {
  busy.value = true;
  message.value = '';
  try {
    const res = await api.casinoPlay(props.player.id, 'loto', bet.value, { digits: picks.value });
    emit('update', res.player);
    const d = res.detail as {
      picks: number[];
      draw: number[];
      matches: number;
      multiplier: number;
    };
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
    <h3 class="cg-title">ロト</h3>
    <p class="cg-lead">6桁の数字(各0-9)を予想し、抽選結果と位置ごとに一致した桁数で配当が決まる。</p>

    <div v-if="result" class="cg-result" :class="result.win ? 'win' : 'lose'" data-test="result">
      <div class="loto-draw">
        <span
          v-for="(n, i) in result.draw"
          :key="i"
          class="digit"
          :class="{ hit: n === result.picks[i] }"
          >{{ n }}</span
        >
      </div>
      <span class="band">{{ result.matches }}桁一致 ×{{ result.multiplier }}</span>
      <span class="outcome">
        {{ result.win ? `当たり！ +${yen(result.net)}円` : `はずれ ${yen(result.net)}円` }}
      </span>
    </div>

    <div class="cg-controls">
      <span class="picks-label">予想：</span>
      <select
        v-for="(_, i) in picks"
        :key="i"
        v-model.number="picks[i]"
        class="pick"
        :data-test="`pick-${i}`"
      >
        <option v-for="n in digitOptions" :key="n" :value="n">{{ n }}</option>
      </select>
    </div>

    <div class="cg-controls">
      <label
        >掛け金：
        <select v-model.number="bet" data-test="bet">
          <option v-for="b in bets" :key="b" :value="b">{{ yen(b) }}円</option>
        </select>
      </label>
      <button class="btn" :disabled="busy" data-test="play" @click="play">抽選する</button>
    </div>

    <table class="cg-paytable">
      <thead>
        <tr>
          <th>一致桁</th>
          <th>配当</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in paytable" :key="row.hit">
          <td>{{ row.hit }}桁</td>
          <td>×{{ row.bai }}</td>
        </tr>
      </tbody>
    </table>

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
.cg-result .loto-draw {
  display: flex;
  justify-content: center;
  gap: 6px;
  margin-bottom: 8px;
}
.cg-result .digit {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 6px;
  background: #eee;
  color: #333;
  font-size: 20px;
  font-weight: bold;
}
.cg-result .digit.hit {
  background: #ffd34d;
  color: #6a2fb5;
}
.cg-result .band {
  display: inline-block;
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
  margin-bottom: 10px;
}
.picks-label {
  font-size: 14px;
}
.pick {
  width: 48px;
}
.cg-paytable {
  width: 100%;
  border-collapse: collapse;
  margin-top: 4px;
  font-size: 12px;
}
.cg-paytable th,
.cg-paytable td {
  border: 1px solid #ddd;
  padding: 3px 8px;
  text-align: center;
}
.cg-paytable th {
  background: #f3efff;
  color: #6a2fb5;
}
</style>
