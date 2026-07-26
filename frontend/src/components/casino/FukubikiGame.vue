<script setup lang="ts">
import { ref } from 'vue';
import { api, type Player } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

// カード枚数はサーバの福引き(fukubikiCards)に合わせて3枚。
const cardCount = 3;
const cards = Array.from({ length: cardCount }, (_, i) => i + 1);

const busy = ref(false);
const message = ref('');
const played = ref(false);
const selected = ref(0);

interface CardInfo {
  card: number;
  rank: string;
}
interface FukubikiDetail {
  card: number;
  rank: string;
  prize: string;
  cards: CardInfo[];
}
const result = ref<FukubikiDetail | null>(null);

const rankLabel = (rank: string) =>
  rank === 'atari' ? '当たり' : rank === 'nami' ? '並' : 'はずれ';

// 公開後の各カードの正体(ランク)を返す。未プレイ時は空文字。
function cardRank(card: number): string {
  return result.value?.cards.find((c) => c.card === card)?.rank ?? '';
}

async function play(card: number) {
  if (busy.value || played.value) return;
  busy.value = true;
  message.value = '';
  selected.value = card;
  try {
    // 福引きは無料のため掛け金は0で呼ぶ。
    const res = await api.casinoPlay(props.player.id, 'fukubiki', 0, { card });
    emit('update', res.player);
    result.value = res.detail as unknown as FukubikiDetail;
    played.value = true;
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
    selected.value = 0;
  } finally {
    busy.value = false;
  }
}

function reset() {
  result.value = null;
  played.value = false;
  selected.value = 0;
  message.value = '';
}
</script>

<template>
  <div class="cg">
    <h3 class="cg-title">福引き</h3>
    <p class="cg-lead">
      3枚のカードから1枚を選ぶ無料の福引き。選んだカードの当たり・並・はずれに応じた景品(アイテム)がもらえる。
    </p>

    <div
      v-if="result"
      class="cg-result"
      :class="cardRank(result.card)"
      data-test="result"
    >
      <span class="band">{{ rankLabel(result.rank) }}</span>
      <span class="outcome">景品：{{ result.prize }} を手に入れた！</span>
    </div>

    <div class="cg-cards">
      <button
        v-for="c in cards"
        :key="c"
        class="card"
        :class="[played ? cardRank(c) : '', { picked: selected === c }]"
        :disabled="busy || played"
        :data-test="`card-${c}`"
        @click="play(c)"
      >
        <span class="face">{{ played ? rankLabel(cardRank(c)) : '?' }}</span>
        <span class="no">No.{{ c }}</span>
      </button>
    </div>

    <div v-if="played" class="cg-controls">
      <button class="btn" :disabled="busy" data-test="again" @click="reset">もう一度引く</button>
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
.cg-result .band {
  font-weight: bold;
  margin-right: 10px;
}
.cg-result .outcome {
  font-weight: bold;
}
.cg-result.atari {
  background: #eaffea;
  color: #067a06;
}
.cg-result.nami {
  background: #fff6e0;
  color: #a06a00;
}
.cg-result.hazure {
  background: #ffecec;
  color: #cc2200;
}
.cg-cards {
  display: flex;
  gap: 10px;
  justify-content: center;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.card {
  width: 96px;
  height: 128px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border: 2px solid #7a5cff;
  border-radius: 8px;
  background: #f4f1ff;
  color: #6a2fb5;
  cursor: pointer;
  transition: transform 0.08s ease;
}
.card:hover:enabled {
  transform: translateY(-3px);
}
.card:disabled {
  cursor: default;
}
.card .face {
  font-size: 22px;
  font-weight: bold;
}
.card .no {
  font-size: 12px;
  color: #888;
}
.card.picked {
  outline: 3px solid #ffd700;
  outline-offset: 2px;
}
.card.atari {
  background: #eaffea;
  border-color: #067a06;
  color: #067a06;
}
.card.nami {
  background: #fff6e0;
  border-color: #a06a00;
  color: #a06a00;
}
.card.hazure {
  background: #ffecec;
  border-color: #cc2200;
  color: #cc2200;
}
.cg-controls {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
</style>
