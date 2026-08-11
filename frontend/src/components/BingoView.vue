<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player, type BingoState, type BingoCard } from '../api';
import { showToast, notifyError, errorText } from '../toast';

// ビンゴ会場(レガシー bingo.cgi)。街全体で数日かけて行う共有イベントで、
// 全員が同じ抽選番号を見る。2ライン揃うと上がりで、賞金は上がった順に決まる。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const state = ref<BingoState | null>(null);
const busy = ref(false);

async function load() {
  try {
    state.value = await api.bingo(props.player.id);
  } catch (e) {
    notifyError('ビンゴを読み込めませんでした', e);
  }
}
onMounted(load);

function fail(e: unknown) {
  showToast({
    variant: 'error',
    title: 'できませんでした',
    lines: [errorText(e)],
    icon: 'item',
  });
}

async function takeCard() {
  busy.value = true;
  try {
    emit('update', await api.bingoTakeCard(props.player.id));
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function claim(card: BingoCard) {
  busy.value = true;
  try {
    const res = await api.bingoClaim(props.player.id, card.id);
    emit('update', res.player);
    showToast({
      variant: 'event-good',
      title: `ビンゴ！ ${res.result.rank}着`,
      lines: [`賞金${yen(res.result.prize)}円が普通口座に入りました。`],
      icon: 'item',
    });
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page bingo-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="bingo-header">
      <div class="lead">
        ビンゴ会場です。街のみんなで同じ番号を見ながら進めます。<br />
        番号は毎日少しずつ公開されます。{{
          state?.lines_to_win ?? 2
        }}ライン揃うと上がりで、賞金は上がった順に決まります。
      </div>
      <div class="title">ビンゴ</div>
    </div>

    <div v-if="state && !state.active" class="panel-white">
      <p class="note">いまビンゴ大会は開かれていません。次回をお待ちください。</p>
    </div>

    <template v-else-if="state">
      <div class="panel-white">
        <div class="status">
          <span
            ><b>{{ state.day }}</b> / {{ state.days }}日目</span
          >
          <span
            >公開済み <b>{{ state.drawn.length }}</b> / {{ state.total }}個</span
          >
          <span
            >上がった人 <b>{{ state.finished_count }}</b
            >人</span
          >
          <span class="prize"
            >次の賞金 <b>{{ yen(state.next_prize) }}円</b></span
          >
        </div>
        <div class="drawn" data-test="drawn">
          <span v-for="n in state.drawn" :key="n" class="ball">{{ n }}</span>
          <span v-if="!state.drawn.length" class="note">まだ番号は出ていません。</span>
        </div>
      </div>

      <div class="panel-white">
        <div class="cards-head">
          <span>あなたのカード（{{ state.cards.length }} / {{ state.max_cards }}）</span>
          <button
            v-if="state.cards.length < state.max_cards"
            class="btn primary"
            :disabled="busy"
            data-test="take-card"
            @click="takeCard"
          >
            カードをもらう
          </button>
        </div>

        <div v-if="!state.cards.length" class="note">
          まだカードがありません。もらってから参加してください。
        </div>

        <div class="cards">
          <div v-for="c in state.cards" :key="c.id" class="card" :data-test="`card-${c.id}`">
            <div class="grid">
              <div
                v-for="(n, i) in c.numbers"
                :key="i"
                class="cell"
                :class="{ marked: c.marks[i], free: n === 0 }"
              >
                {{ n === 0 ? 'FREE' : n }}
              </div>
            </div>
            <div class="card-foot">
              <span class="lines">{{ c.lines }}ライン</span>
              <span v-if="c.rank" class="done">{{ c.rank }}着（{{ yen(c.prize) }}円）</span>
              <button
                v-else-if="c.can_win"
                class="btn primary"
                :disabled="busy"
                :data-test="`claim-${c.id}`"
                @click="claim(c)"
              >
                ビンゴ！
              </button>
            </div>
          </div>
        </div>
      </div>
    </template>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.bingo-page {
  background-color: #f7e9e7;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.bingo-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.bingo-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.bingo-header .title {
  flex: 0 0 140px;
  background: #c0392b;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.panel-white {
  background: #fff;
  border: 1px solid #999;
  padding: 12px;
  margin-bottom: 8px;
}
.note {
  font-size: 12px;
  color: #666;
}
.status {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  font-size: 12px;
  color: #555;
  margin-bottom: 8px;
}
.status b {
  color: #333;
  font-size: 14px;
}
.status .prize b {
  color: #cc3300;
}
.drawn {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.ball {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: #3d7fd1;
  color: #fff;
  font-size: 11px;
  font-weight: bold;
}
.cards-head {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 13px;
  font-weight: bold;
  color: #8a2b20;
  margin-bottom: 8px;
}
.cards {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}
.card {
  border: 2px solid #c0392b;
  border-radius: 6px;
  padding: 6px;
  background: #fffdfc;
}
.grid {
  display: grid;
  grid-template-columns: repeat(5, 34px);
  gap: 2px;
}
.cell {
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid #e0c0bc;
  border-radius: 3px;
  font-size: 12px;
  color: #333;
  background: #fff;
}
.cell.marked {
  background: #3d7fd1;
  border-color: #2b62a3;
  color: #fff;
  font-weight: bold;
}
.cell.free {
  font-size: 9px;
  background: #ffd452;
  border-color: #c99a17;
  color: #6b4a00;
}
.cell.free.marked {
  background: #ffd452;
  color: #6b4a00;
}
.card-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 6px;
  font-size: 12px;
}
.lines {
  color: #8a2b20;
  font-weight: bold;
}
.done {
  color: #067a06;
  font-weight: bold;
}
@media (max-width: 700px) {
  .bingo-header .title {
    flex: 0 0 88px;
    font-size: 14px;
  }
  .grid {
    grid-template-columns: repeat(5, 30px);
  }
  .cell {
    height: 27px;
    font-size: 11px;
  }
}
</style>
