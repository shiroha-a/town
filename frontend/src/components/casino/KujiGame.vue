<script setup lang="ts">
import { ref, computed } from 'vue';
import { api, type Player } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const bets = [500, 1000, 5000, 10000, 50000, 100000, 500000, 1000000];
const bet = ref(bets[0]);
const stage = ref(0); // 現在の連勝段数(dabulu)。0=未プレイ。
const pot = ref(0); // 現在の段数で精算した場合の受取額。
const busy = ref(false);
const message = ref('');

interface KujiDetail {
  action: string;
  card: number;
  choice: number;
  win: boolean;
  stage: number;
  bairitu: number;
  syoukin: number;
}
const result = ref<(KujiDetail & { payout: number; net: number }) | null>(null);

const inStreak = computed(() => stage.value >= 1);
const resultClass = computed(() => {
  if (!result.value) return '';
  if (result.value.action === 'settle') return 'win';
  return result.value.win ? 'win' : 'lose';
});

async function draw(choice: 1 | 2) {
  if (busy.value) return;
  busy.value = true;
  message.value = '';
  try {
    const res = await api.casinoPlay(props.player.id, 'kuji', bet.value, {
      stage: stage.value,
      choice,
      cashout: false,
    });
    emit('update', res.player);
    const d = res.detail as unknown as KujiDetail;
    stage.value = d.win ? d.stage : 0;
    pot.value = d.win ? d.syoukin : 0;
    result.value = { ...d, payout: res.payout, net: res.payout - bet.value };
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}

async function settle() {
  if (busy.value || stage.value < 1) return;
  busy.value = true;
  message.value = '';
  try {
    const res = await api.casinoPlay(props.player.id, 'kuji', bet.value, {
      stage: stage.value,
      cashout: true,
    });
    emit('update', res.player);
    const d = res.detail as unknown as KujiDetail;
    stage.value = 0;
    pot.value = 0;
    result.value = { ...d, payout: res.payout, net: res.payout - bet.value };
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="cg">
    <h3 class="cg-title">くじ</h3>
    <p class="cg-lead">
      2枚のカードから当たりを選ぶ2択。当たれば賞金が倍々(2^n)に育つダブルアップ。精算する前に外すと掛け金は没収。
    </p>

    <div v-if="result" class="cg-result" :class="resultClass" data-test="result">
      <template v-if="result.action === 'settle'">
        <span class="band">精算</span>
        <span class="outcome">
          倍率×{{ result.bairitu }}／受取 {{ yen(result.payout) }}円（+{{ yen(result.net) }}円）
        </span>
      </template>
      <template v-else>
        <span class="card">引いたカード：{{ result.card }}（予想：{{ result.choice }}）</span>
        <span v-if="result.win" class="outcome">
          当たり！{{ result.stage }}連勝／現在の賞金 {{ yen(result.syoukin) }}円
        </span>
        <span v-else class="outcome">はずれ… 掛け金 {{ yen(bet) }}円を失いました</span>
      </template>
    </div>

    <div class="cg-controls">
      <label
        >掛け金：
        <select v-model.number="bet" data-test="bet" :disabled="inStreak || busy">
          <option v-for="b in bets" :key="b" :value="b">{{ yen(b) }}円</option>
        </select>
      </label>
    </div>

    <div v-if="inStreak" class="streak">
      <div class="streak-info">{{ stage }}連勝中／いま精算すると {{ yen(pot) }}円</div>
      <div class="cg-controls">
        <button class="btn" :disabled="busy" data-test="up1" @click="draw(1)">カード1でダブルアップ</button>
        <button class="btn" :disabled="busy" data-test="up2" @click="draw(2)">カード2でダブルアップ</button>
        <button class="btn settle" :disabled="busy" data-test="settle" @click="settle">
          精算する（{{ yen(pot) }}円）
        </button>
      </div>
    </div>
    <div v-else class="cg-controls">
      <button class="btn" :disabled="busy" data-test="draw1" @click="draw(1)">カード1を引く</button>
      <button class="btn" :disabled="busy" data-test="draw2" @click="draw(2)">カード2を引く</button>
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
.cg-result .card {
  font-size: 16px;
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
  margin-bottom: 8px;
}
.streak {
  border-top: 1px dashed #cbb8ff;
  padding-top: 10px;
  margin-top: 4px;
}
.streak-info {
  font-size: 13px;
  font-weight: bold;
  color: #6a2fb5;
  margin-bottom: 8px;
}
.btn.settle {
  background: #ffd700;
  color: #6a2fb5;
}
</style>
