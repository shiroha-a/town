<script setup lang="ts">
import { computed, ref } from 'vue';
import { api, type Player } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const bets = [500, 1000, 5000, 10000, 50000, 100000];
const bet = ref(bets[0]);
const busy = ref(false);
const message = ref('');

// 絵柄0..6の表示トークン/名称/倍率。slot.goのslotMultipliersと対応させる。
const symTokens = ['チェ', 'ベル', 'スイ', 'プラ', 'ＢＡＲ', 'ダイ', '７'];
const symNames = ['チェリー', 'ベル', 'スイカ', 'プラム', 'ＢＡＲ', 'ダイヤ', 'セブン'];
const symMults = [1, 20, 80, 200, 800, 2000, 7777];

interface SlotLine {
  line: number;
  symbol: number;
  mult: number;
}
interface SlotDetail {
  grid: number[][];
  lines: SlotLine[];
  multiplier: number;
  big: boolean;
}

const result = ref<(SlotDetail & { win: boolean; net: number }) | null>(null);

// slot.goのslotLineCellsと同順。成立ラインのセルを強調表示するために持つ。
const lineCells: [number, number][][] = [
  [
    [0, 1],
    [1, 1],
    [2, 1],
  ],
  [
    [0, 0],
    [1, 0],
    [2, 0],
  ],
  [
    [0, 2],
    [1, 2],
    [2, 2],
  ],
  [
    [0, 0],
    [1, 1],
    [2, 2],
  ],
  [
    [0, 2],
    [1, 1],
    [2, 0],
  ],
  [
    [0, 0],
    [0, 1],
    [0, 2],
  ],
  [
    [1, 0],
    [1, 1],
    [1, 2],
  ],
  [
    [2, 0],
    [2, 1],
    [2, 2],
  ],
];

const winCells = computed(() => {
  const s = new Set<string>();
  if (!result.value) return s;
  for (const ln of result.value.lines) {
    for (const [row, reel] of lineCells[ln.line - 1]) s.add(`${row}-${reel}`);
  }
  return s;
});

async function play() {
  busy.value = true;
  message.value = '';
  try {
    const res = await api.casinoPlay(props.player.id, 'slot', bet.value, {});
    emit('update', res.player);
    const d = res.detail as unknown as SlotDetail;
    result.value = { ...d, win: res.win, net: res.payout - bet.value };
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="cg">
    <h3 class="cg-title">スロット</h3>
    <p class="cg-lead">
      3×3のリールを回し、8本のライン上に同じ絵柄が3つ揃うと配当(複数ライン成立は合算)。
    </p>

    <div v-if="result" class="cg-result" :class="result.win ? 'win' : 'lose'" data-test="result">
      <div class="slot-grid">
        <template v-for="row in [0, 1, 2]" :key="row">
          <div
            v-for="reel in [0, 1, 2]"
            :key="`${row}-${reel}`"
            class="slot-cell"
            :class="[`sym${result.grid[row][reel]}`, { hit: winCells.has(`${row}-${reel}`) }]"
          >
            {{ symTokens[result.grid[row][reel]] }}
          </div>
        </template>
      </div>
      <div class="slot-outcome">
        <template v-if="result.win">
          <span class="band">{{ result.big ? '大当たり！！' : '当たり！' }}</span>
          <span class="mult">合計{{ result.multiplier }}倍</span>
          <span class="net">収支 {{ result.net >= 0 ? '+' : '' }}{{ yen(result.net) }}円</span>
        </template>
        <template v-else>
          <span class="band">はずれ</span>
          <span class="net">{{ yen(result.net) }}円</span>
        </template>
      </div>
      <ul v-if="result.lines.length" class="slot-lines">
        <li v-for="ln in result.lines" :key="ln.line">
          ライン{{ ln.line }}：{{ symNames[ln.symbol] }}が3つ ×{{ ln.mult }}
        </li>
      </ul>
    </div>

    <div class="cg-controls">
      <label
        >掛け金：
        <select v-model.number="bet" data-test="bet">
          <option v-for="b in bets" :key="b" :value="b">{{ yen(b) }}円</option>
        </select>
      </label>
      <button class="btn" :disabled="busy" data-test="spin" @click="play()">スロットを回す</button>
    </div>

    <table class="slot-odds">
      <thead>
        <tr>
          <th>絵柄</th>
          <th>倍率</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(name, i) in symNames" :key="i">
          <td><span class="chip" :class="`sym${i}`">{{ symTokens[i] }}</span>{{ name }}</td>
          <td>×{{ symMults[i] }}</td>
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
.cg-result.win {
  background: #eaffea;
  color: #067a06;
}
.cg-result.lose {
  background: #ffecec;
  color: #cc2200;
}
.slot-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
  max-width: 260px;
  margin: 0 auto 10px;
}
.slot-cell {
  aspect-ratio: 1 / 1;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  font-weight: bold;
  border-radius: 6px;
  border: 2px solid transparent;
  color: #333;
  background: #f2f2f2;
}
.slot-cell.hit {
  border-color: #ff9800;
  box-shadow: 0 0 8px rgba(255, 152, 0, 0.6);
}
.sym0 {
  background: #eeeeee;
  color: #888;
}
.sym1 {
  background: #e8f0ff;
  color: #2a5db0;
}
.sym2 {
  background: #e6f8e6;
  color: #1f8a3b;
}
.sym3 {
  background: #f3e8ff;
  color: #7a2fb5;
}
.sym4 {
  background: #fff3e0;
  color: #c9700a;
}
.sym5 {
  background: #e0f7fa;
  color: #00838f;
}
.sym6 {
  background: #fff7d6;
  color: #cc0000;
}
.slot-outcome {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
}
.slot-outcome .band {
  font-size: 18px;
  font-weight: bold;
}
.slot-outcome .net {
  font-weight: bold;
}
.slot-lines {
  list-style: none;
  padding: 0;
  margin: 8px 0 0;
  font-size: 12px;
  color: #444;
}
.slot-lines li {
  margin: 2px 0;
}
.cg-controls {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.slot-odds {
  width: 100%;
  border-collapse: collapse;
  margin-top: 12px;
  font-size: 12px;
}
.slot-odds th,
.slot-odds td {
  border: 1px solid #ddd;
  padding: 4px 8px;
  text-align: left;
}
.slot-odds .chip {
  display: inline-block;
  min-width: 34px;
  text-align: center;
  padding: 1px 4px;
  margin-right: 6px;
  border-radius: 4px;
  font-weight: bold;
}
</style>
