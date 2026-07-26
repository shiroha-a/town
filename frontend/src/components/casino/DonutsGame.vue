<script setup lang="ts">
import { computed, ref } from 'vue';
import { api, type Player } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');

// 掛け金は「累積枚数×基本額」で決まる。基本額は仕様どおり1万円固定。
const units = [10000];
const unit = ref(units[0]);

// ゲーム状態(サーバは保持せずクライアントで持ち回す): 前のカードと累積枚数。
const randCard = () => Math.floor(Math.random() * 5) + 1;
const prev = ref(randCard());
const count = ref(1);

const busy = ref(false);
const message = ref('');
const result = ref<{
  prev: number;
  card: number;
  choice: string;
  count: number;
  outcome: string;
  next_prev: number;
  next_count: number;
  win: boolean;
  net: number;
} | null>(null);

// 今回の掛け金 = 累積枚数 × 基本額。
const stake = computed(() => count.value * unit.value);

function newGame() {
  prev.value = randCard();
  count.value = 1;
  result.value = null;
  message.value = '';
}

async function play(choice: 'hi' | 'low') {
  busy.value = true;
  message.value = '';
  const bet = stake.value;
  try {
    const res = await api.casinoPlay(props.player.id, 'donuts', bet, {
      choice,
      prev: prev.value,
      count: count.value,
    });
    emit('update', res.player);
    const d = res.detail as {
      prev: number;
      card: number;
      choice: string;
      count: number;
      outcome: string;
      next_prev: number;
      next_count: number;
    };
    result.value = { ...d, win: res.win, net: res.payout - bet };
    // 次ラウンドの状態(前カード・累積枚数)を引き継ぐ。
    prev.value = d.next_prev;
    count.value = d.next_count;
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="cg">
    <h3 class="cg-title">ドーナツ</h3>
    <p class="cg-lead">
      1〜5のカードを引き、前のカードよりハイ(大)かロー(小)かを当てる。当たると累積枚数×1万円を得てカードが1枚積まれ、外れると同額を失いテーブルは1枚に戻る。同じ数字はセーフ(損得なし)で累積が2倍になり継続。
    </p>

    <div class="cg-table">
      <span class="prev">前のカード：<b>{{ prev }}</b></span>
      <span class="count">累積：<b>{{ count }}</b> 枚</span>
      <span class="stake">今回の掛け金：<b>{{ yen(stake) }}</b> 円</span>
    </div>

    <div
      v-if="result"
      class="cg-result"
      :class="result.outcome"
      data-test="result"
    >
      <span class="cards">{{ result.prev }} → <b>{{ result.card }}</b></span>
      <span class="band">{{ result.choice === 'hi' ? 'ハイ' : 'ロー' }}</span>
      <span class="outcome">
        <template v-if="result.outcome === 'win'">当たり！ +{{ yen(result.net) }}円</template>
        <template v-else-if="result.outcome === 'safe'">セーフ(同じ数字)！ 損得なし・累積2倍で継続</template>
        <template v-else>はずれ {{ yen(result.net) }}円</template>
      </span>
    </div>

    <div class="cg-controls">
      <label
        >1枚あたり：
        <select v-model.number="unit" data-test="bet">
          <option v-for="u in units" :key="u" :value="u">{{ yen(u) }}円</option>
        </select>
      </label>
      <button class="btn" :disabled="busy" data-test="hi" @click="play('hi')">ハイ(大)</button>
      <button class="btn" :disabled="busy" data-test="low" @click="play('low')">ロー(小)</button>
      <button class="btn ghost" :disabled="busy" data-test="reset" @click="newGame">新しく始める</button>
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
.cg-table {
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
  font-size: 13px;
  color: #333;
  margin-bottom: 12px;
}
.cg-table b {
  color: #6a2fb5;
}
.cg-result {
  text-align: center;
  padding: 12px;
  border-radius: 6px;
  margin-bottom: 12px;
  font-size: 15px;
}
.cg-result .cards {
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
.cg-result.safe {
  background: #fff6e0;
  color: #a06a00;
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
.cg-controls .btn.ghost {
  opacity: 0.85;
}
</style>
