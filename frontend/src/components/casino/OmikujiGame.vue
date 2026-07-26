<script setup lang="ts">
import { computed, ref } from 'vue';
import { api, type Player } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');

// お賽銭の選択肢(saisengaku 1..7)。1..3は「ちょろまかし」で所持金が増える。
const saisenOptions = [
  { value: 1, label: '100,000円ちょろまかす' },
  { value: 2, label: '10,000円ちょろまかす' },
  { value: 3, label: '金は払わぬ' },
  { value: 4, label: '100円' },
  { value: 5, label: '1,000円' },
  { value: 6, label: '10,000円' },
  { value: 7, label: '100,000円' },
];
// 占う項目(kibou)。学問/健康/恋愛はステータス、金運は金銭に作用する。
const kibouOptions = [
  { value: 'gaku', label: '学問' },
  { value: 'kenn', label: '健康' },
  { value: 'renn', label: '恋愛' },
  { value: 'kinn', label: '金運' },
];

const saisengaku = ref(4); // 既定は100円(レガシーのselected)。
const kibou = ref('gaku');
const busy = ref(false);
const message = ref('');

interface OmikujiFortune {
  kibou: string;
  unsei: number;
  name: string;
}
interface OmikujiChange {
  param: string;
  label: string;
  amount: number;
}
interface OmikujiDetail {
  saisengaku: number;
  saisen_money: number;
  kinn_money: number;
  money_delta: number;
  kibou: string;
  kibou_name: string;
  fortunes: OmikujiFortune[];
  overall: OmikujiFortune;
  result: OmikujiFortune;
  changes: OmikujiChange[] | null;
  hamaya: boolean;
  message: string;
}

const result = ref<OmikujiDetail | null>(null);

// 運勢名を項目キーから引く表(表示用)。
const kibouLabel: Record<string, string> = {
  gaku: '学問',
  kenn: '健康',
  renn: '恋愛',
  kinn: '金運',
};

const changes = computed(() => result.value?.changes ?? []);

// 運勢の良し悪しで結果ボックスの配色を決める。超大吉/吉以上=吉、大凶/凶=凶、末吉=中立。
const resultClass = computed(() => {
  const u = result.value?.result.unsei ?? 0;
  if (u === 1) return 'super';
  if (u >= 5) return 'win';
  if (u === 2 || u === 3) return 'lose';
  return 'neutral';
});

async function draw() {
  if (busy.value) return;
  busy.value = true;
  message.value = '';
  try {
    const res = await api.casinoPlay(props.player.id, 'omikuji', 0, {
      saisengaku: saisengaku.value,
      kibou: kibou.value,
    });
    emit('update', res.player);
    result.value = res.detail as unknown as OmikujiDetail;
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="cg">
    <h3 class="cg-title">おみくじ</h3>
    <p class="cg-lead">
      お賽銭を納めて運勢を占う。占う項目の運勢に応じてステータスが増減(金運は所持金が増減)。賽銭をちょろまかすと所持金は増えるが凶が出やすい。
    </p>

    <div v-if="result" class="cg-result" :class="resultClass" data-test="result">
      <div class="omi-grid">
        <div v-for="f in result.fortunes" :key="f.kibou" class="omi-cell">
          <span class="omi-label">{{ kibouLabel[f.kibou] }}</span>
          <span class="omi-unsei" :class="`u${f.unsei}`">{{ f.name }}</span>
        </div>
      </div>
      <div class="omi-overall">全体運：<b>{{ result.overall.name }}</b></div>

      <div class="omi-main">
        <span class="band">{{ result.kibou_name }} → {{ result.result.name }}</span>
        <p class="omi-message">{{ result.message }}</p>
      </div>

      <ul v-if="changes.length" class="omi-changes">
        <li v-for="c in changes" :key="c.param">
          {{ c.label }} {{ c.amount >= 0 ? '+' : '' }}{{ c.amount }}
        </li>
      </ul>

      <div class="omi-money">
        <span>お賽銭：{{ result.saisen_money >= 0 ? '+' : '' }}{{ yen(result.saisen_money) }}円</span>
        <span v-if="result.kibou === 'kinn' && result.kinn_money !== 0">
          ／金運：{{ result.kinn_money >= 0 ? '+' : '' }}{{ yen(result.kinn_money) }}円
        </span>
        <span class="omi-money-total">
          ／所持金 {{ result.money_delta >= 0 ? '+' : '' }}{{ yen(result.money_delta) }}円
        </span>
      </div>
    </div>

    <div class="cg-controls">
      <label
        >お賽銭：
        <select v-model.number="saisengaku" data-test="saisen">
          <option v-for="o in saisenOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
      </label>
      <label
        >占う項目：
        <select v-model="kibou" data-test="kibou">
          <option v-for="o in kibouOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
      </label>
      <button class="btn" :disabled="busy" data-test="draw" @click="draw">おみくじを引く</button>
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
.cg-result.win {
  background: #eaffea;
  color: #067a06;
}
.cg-result.lose {
  background: #ffecec;
  color: #cc2200;
}
.cg-result.neutral {
  background: #f2f2f2;
  color: #555;
}
.cg-result.super {
  background: #fff7d6;
  color: #b8860b;
}
.omi-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 6px;
  max-width: 320px;
  margin: 0 auto 8px;
}
.omi-cell {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  background: rgba(255, 255, 255, 0.6);
  border-radius: 6px;
  padding: 6px 2px;
}
.omi-label {
  font-size: 11px;
  color: #666;
}
.omi-unsei {
  font-size: 15px;
  font-weight: bold;
}
/* 運勢別の色(超大吉=金, 大凶/凶=赤, 大吉=緑) */
.omi-unsei.u1 {
  color: #b8860b;
}
.omi-unsei.u2,
.omi-unsei.u3 {
  color: #cc2200;
}
.omi-unsei.u4 {
  color: #777;
}
.omi-unsei.u5,
.omi-unsei.u6,
.omi-unsei.u7 {
  color: #2a7d2a;
}
.omi-unsei.u8 {
  color: #067a06;
}
.omi-overall {
  font-size: 13px;
  margin-bottom: 8px;
}
.omi-main .band {
  font-size: 18px;
  font-weight: bold;
}
.omi-message {
  margin: 6px 0 0;
  font-size: 13px;
}
.omi-changes {
  list-style: none;
  padding: 0;
  margin: 8px 0 0;
  font-size: 13px;
  font-weight: bold;
}
.omi-changes li {
  margin: 2px 0;
}
.omi-money {
  margin-top: 10px;
  font-size: 12px;
}
.omi-money-total {
  font-weight: bold;
}
.cg-controls {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
</style>
