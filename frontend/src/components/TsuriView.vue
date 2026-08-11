<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player, type FishingState } from '../api';
import { showToast, notifyError, errorText } from '../toast';

// 釣りゲーム(レガシー tsuri.cgi)。餌を1つ消費してカードを引き、当たりを引くと魚が釣れる。
// 「引いてる」カードを引くと続行でき、引くほど良い魚になる。外すと逃げられて終了。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const state = ref<FishingState | null>(null);
const selectedBait = ref<number | null>(null);
const busy = ref(false);
// 直前の結果(演出用)。引いたカードの位置と結末を覚えておく。
const lastPick = ref<{ card: number; outcome: string } | null>(null);

async function load() {
  try {
    state.value = await api.fishing(props.player.id);
    if (selectedBait.value === null && state.value.baits.length) {
      selectedBait.value = state.value.baits[0].item_id;
    }
  } catch (e) {
    notifyError('釣り場を読み込めませんでした', e);
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

async function start() {
  if (selectedBait.value === null) return;
  busy.value = true;
  lastPick.value = null;
  try {
    emit('update', await api.fishingStart(props.player.id, selectedBait.value));
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function pick(card: number) {
  busy.value = true;
  try {
    const res = await api.fishingPick(props.player.id, card);
    emit('update', res.player);
    lastPick.value = { card, outcome: res.result.outcome };
    if (res.result.outcome === 'win') {
      showToast({
        variant: 'event-good',
        title: `${res.result.fish}を釣り上げた！`,
        lines: ['持ち物に追加されました。'],
        icon: 'item',
      });
    } else if (res.result.outcome === 'lose') {
      showToast({ variant: 'event-bad', title: '逃がしてしまいました。', lines: [], icon: 'item' });
    }
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page tsuri-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="tsuri-header">
      <div class="lead">
        釣りゲームです。餌を1つ使ってカードを引きます。<br />
        「引いてる」を引くと続けられ、粘るほど大物になります。外すと逃げられます。
      </div>
      <div class="title">釣り</div>
    </div>

    <div class="panel-white">
      <!-- 準備: 餌を選ぶ -->
      <template v-if="state && !state.active">
        <div v-if="state.at_limit" class="note warn">
          持ち物が上限なので釣りができません。何か使うか売ってから来てください。
        </div>
        <div v-else-if="!state.baits.length" class="note warn">
          釣り餌を持っていません。デパートで「釣りの餌(初)」を買ってきてください。
        </div>
        <div v-else class="prep">
          <span class="lbl">◆どの餌を使いますか？</span>
          <select v-model="selectedBait" data-test="bait">
            <option v-for="b in state.baits" :key="b.item_id" :value="b.item_id">
              {{ b.name }}（残り{{ b.uses }}）
            </option>
          </select>
          <button class="btn primary" :disabled="busy" data-test="start" @click="start">
            釣りをする
          </button>
        </div>
      </template>

      <!-- 進行中: カードを引く -->
      <template v-else-if="state && state.active">
        <div class="note">カードを1枚選んでください。</div>
        <div class="cards" data-test="cards">
          <button
            v-for="n in state.cards"
            :key="n"
            class="card"
            :disabled="busy"
            :data-test="`card-${n}`"
            @click="pick(n)"
          >
            <span class="qmark">?</span>
          </button>
        </div>
      </template>

      <div v-if="lastPick" class="result" :class="lastPick.outcome" data-test="result">
        {{
          lastPick.outcome === 'win'
            ? '釣り上げた！'
            : lastPick.outcome === 'continue'
              ? '引いてる…！ もう1枚引けます'
              : '逃がしてしまいました'
        }}
      </div>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.tsuri-page {
  background-color: #cfe4f0;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.tsuri-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.tsuri-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.tsuri-header .title {
  flex: 0 0 140px;
  background: #2b6ca3;
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
}
.note {
  font-size: 12px;
  color: #333;
  margin-bottom: 8px;
}
.note.warn {
  color: #cc3300;
}
.prep .lbl {
  color: #006699;
  margin-right: 6px;
  font-size: 12px;
}
.prep select {
  margin-right: 8px;
}
.cards {
  display: flex;
  width: 100%;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: center;
  margin: 10px 0;
}
.card {
  width: 78px;
  height: 96px;
  border: 2px solid #2b6ca3;
  border-radius: 6px;
  background: linear-gradient(160deg, #5aa7dd, #2b6ca3);
  color: #fff;
  font-size: 30px;
  font-weight: bold;
  cursor: pointer;
}
.card:hover:not(:disabled) {
  filter: brightness(1.12);
}
.card:disabled {
  opacity: 0.6;
  cursor: default;
}
.result {
  margin-top: 10px;
  text-align: center;
  font-weight: bold;
  padding: 6px;
  border-radius: 4px;
}
.result.win {
  color: #067a06;
  background: #eaffea;
}
.result.continue {
  color: #b36b00;
  background: #fff6e0;
}
.result.lose {
  color: #cc3300;
  background: #ffecec;
}
@media (max-width: 700px) {
  .tsuri-header .title {
    flex: 0 0 88px;
    font-size: 14px;
  }
  .card {
    width: 62px;
    height: 78px;
    font-size: 24px;
  }
}
</style>
