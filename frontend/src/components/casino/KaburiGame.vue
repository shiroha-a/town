<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { api, type Player, type KaburiState } from '../../api';
import { notifyError } from '../../toast';

// カード引き: 街でひとつの卓を全員で共有する。場から1枚引き、前の人が引いた
// カード(伏せ札)とかぶったら負け。引くのはサーバーなので、伏せ札を知っていても
// 有利にはならない(選ぶ操作は無い)。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');

const state = ref<KaburiState | null>(null);
const bet = ref(10000);
const busy = ref(false);
const result = ref<{ card: number; hidden: number; win: boolean; net: number } | null>(null);

const payout = computed(() =>
  state.value ? Math.floor((bet.value * state.value.payout_percent) / 100) : 0,
);
const fmtTime = (iso: string) => {
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${p(d.getHours())}:${p(d.getMinutes())}`;
};

async function load() {
  try {
    const st = await api.kaburiState(props.player.id);
    state.value = st;
    if (!st.bets.includes(bet.value)) bet.value = st.bets[0];
  } catch (e) {
    notifyError('カード引きの場を読み込めませんでした', e);
  }
}
onMounted(load);

async function draw() {
  if (busy.value) return;
  busy.value = true;
  const stake = bet.value;
  try {
    const res = await api.kaburiPlay(props.player.id, stake);
    emit('update', res.player);
    state.value = res.state;
    result.value = {
      card: res.card,
      hidden: res.hidden,
      win: res.win,
      net: res.payout - stake,
    };
  } catch (e) {
    notifyError('カードを引けませんでした', e);
    await load();
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="cg">
    <h3 class="cg-title">カード引き</h3>
    <p class="cg-lead">
      場から1枚引くだけ。場のカードのうち1枚は<b>前の人が引いたカード</b>で伏せられていて、それを引いてしまったら負け(掛け金は没収)。
      かぶらなければ配当がもらえ、引いたカードが次の人の伏せ札になる。
      通るたびに場のカードが1枚ずつ減り(最少3枚)、かぶりやすくなるかわりに配当が上がる。かぶりが出ると場は10枚に戻る。
    </p>

    <div v-if="state" class="cg-table">
      <span
        >場のカード：<b>{{ state.size }}</b> 枚（かぶる確率
        {{ Math.round((100 / state.size) * 10) / 10 }}%）</span
      >
      <span
        >連鎖：<b>{{ state.streak }}</b> 人</span
      >
      <span
        >配当：掛け金の<b>{{ state.payout_percent }}%</b>（+{{ yen(payout) }}円）</span
      >
    </div>

    <div v-if="result" class="cg-result" :class="result.win ? 'win' : 'lose'" data-test="result">
      <span class="cards"
        >あなた <b>{{ result.card }}</b> ／ 前の人 <b>{{ result.hidden }}</b></span
      >
      <span class="outcome">
        <template v-if="result.win">セーフ！ +{{ yen(result.net) }}円</template>
        <template v-else>かぶり！ {{ yen(result.net) }}円</template>
      </span>
    </div>

    <!-- 場のカード。引くのはサーバーなので、ここは「何枚から引くか」の目安。
         直前に引いたカードだけ表にして見せる。 -->
    <div v-if="state" class="cards-row">
      <div
        v-for="c in state.cards"
        :key="c"
        class="card"
        :class="{ drawn: result?.card === c }"
        data-test="card"
      >
        {{ result?.card === c ? c : '?' }}
      </div>
    </div>

    <div class="cg-controls">
      <label
        >掛け金：
        <select v-model.number="bet" data-test="bet">
          <option v-for="b in state?.bets ?? []" :key="b" :value="b">{{ yen(b) }}円</option>
        </select>
      </label>
      <button class="btn" :disabled="busy" data-test="play" @click="draw">カードを引く</button>
    </div>

    <div v-if="state?.recent.length" class="recent">
      <div class="recent-head">直近の勝負</div>
      <div v-for="(e, i) in state.recent" :key="i" class="recent-row" :class="{ lose: !e.win }">
        <span class="t">{{ fmtTime(e.at) }}</span>
        <span class="n">{{ e.name }}</span>
        <span class="s">{{ e.size }}枚</span>
        <span class="r">{{ e.win ? 'セーフ' : 'かぶり' }}</span>
      </div>
    </div>
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
  line-height: 1.6;
}
.cg-table {
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
  justify-content: center;
  font-size: 13px;
  background: #f5f2ff;
  border: 1px solid #ddd4ff;
  border-radius: 6px;
  padding: 8px;
  margin-bottom: 10px;
}
.cg-result {
  text-align: center;
  padding: 12px;
  border-radius: 6px;
  margin-bottom: 12px;
  font-size: 15px;
}
.cg-result .cards {
  margin-right: 10px;
}
.cg-result .outcome {
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
.cards-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  justify-content: center;
  margin-bottom: 12px;
}
.card {
  width: 40px;
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  font-weight: bold;
  color: #aab;
  background: linear-gradient(135deg, #efeaff 0%, #dcd2ff 100%);
  border: 2px solid #bbb;
  border-radius: 6px;
}
/* 直前に引いたカードだけ表にする。 */
.card.drawn {
  background: #fff;
  border-color: #6a2fb5;
  color: #6a2fb5;
  transform: translateY(-4px);
}
.cg-controls {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  justify-content: center;
}
.recent {
  margin-top: 14px;
  border-top: 1px solid #eee;
  padding-top: 8px;
}
.recent-head {
  font-size: 12px;
  color: #666;
  margin-bottom: 4px;
}
.recent-row {
  display: flex;
  gap: 8px;
  font-size: 12px;
  color: #067a06;
  padding: 1px 0;
}
.recent-row.lose {
  color: #cc2200;
}
.recent-row .t {
  color: #999;
}
.recent-row .n {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #333;
}
</style>
