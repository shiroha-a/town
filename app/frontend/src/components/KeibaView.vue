<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue';
import { api, type Player, type KeibaHorse, type KeibaRankEntry, type KeibaResult } from '../api';

// 競馬場: 6頭立てレース。1枚500円、最大2頭・合計200枚まで。払戻し=オッズ×枚数×500。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const TICKET_OPTIONS = [0, 1, 2, 3, 5, 10, 20, 30, 50, 100, 150, 200];
const GOAL = 910;

const raceId = ref(0);
const lineup = ref<KeibaHorse[]>([]);
const ranking = ref<KeibaRankEntry[]>([]);
const tickets = reactive<number[]>([]);
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);

const mode = ref<'bet' | 'racing' | 'result'>('bet');
const result = ref<KeibaResult | null>(null);
const positions = ref<number[]>([]); // 0-100% 各馬の現在位置
let animTimer: number | undefined;

const totalTickets = computed(() => tickets.reduce((a, b) => a + b, 0));
const horsesBet = computed(() => tickets.filter((t) => t > 0).length);
const totalCost = computed(() => totalTickets.value * 500);

async function loadRace() {
  try {
    const r = await api.keibaRace(props.player.id);
    raceId.value = r.race_id;
    lineup.value = r.lineup;
    ranking.value = r.ranking;
    tickets.splice(0, tickets.length, ...r.lineup.map(() => 0));
    mode.value = 'bet';
    result.value = null;
  } catch (e) {
    fail(e);
  }
}
onMounted(loadRace);
onUnmounted(() => {
  if (animTimer !== undefined) window.clearInterval(animTimer);
});

function fail(e: unknown) {
  message.value = e instanceof Error ? e.message : String(e);
  kind.value = 'error';
}

async function startRace() {
  if (totalTickets.value === 0) {
    message.value = '購入枚数を入力してください。';
    kind.value = 'error';
    return;
  }
  if (horsesBet.value > 2) {
    message.value = '賭けられるのは2頭までです。';
    kind.value = 'error';
    return;
  }
  busy.value = true;
  message.value = '';
  try {
    const res = await api.keibaBet(props.player.id, raceId.value, [...tickets]);
    emit('update', res.player);
    result.value = res.result;
    animate(res.result);
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

// 歩幅列に従って各馬を左から右へ進めるアニメーション。終了後に結果を表示する。
function animate(res: KeibaResult) {
  mode.value = 'racing';
  const cum = res.steps.map((arr) => {
    let s = 0;
    return arr.map((step) => (s += step));
  });
  const ticks = Math.max(...res.steps.map((a) => a.length));
  positions.value = res.steps.map(() => 0);
  let t = 0;
  if (animTimer !== undefined) window.clearInterval(animTimer);
  animTimer = window.setInterval(() => {
    positions.value = cum.map((c) => {
      const d = c[Math.min(t, c.length - 1)] ?? 0;
      return Math.min(100, (d / GOAL) * 100);
    });
    t++;
    if (t > ticks) {
      window.clearInterval(animTimer);
      animTimer = undefined;
      mode.value = 'result';
    }
  }, 90);
}

function retry() {
  loadRace();
}
</script>

<template>
  <div class="facility-page keiba-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="fac-header">
      <div class="lead">
        馬券は1枚500円です。2頭まで、合計200枚まで賭けられます。90日プレイしないとランキングから外れます。<br />
        ●{{ player.display_name }}さんの持ち金：<span class="money">{{ yen(player.money) }}円</span>
      </div>
      <div class="title">競馬場</div>
    </div>

    <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

    <div class="keiba-body">
      <div class="main-col">
        <!-- 出走表(賭け画面) -->
        <div v-if="mode === 'bet'" class="panel-white">
          <h3>出走表<span class="hint"> 賭け {{ horsesBet }}/2頭 ・ {{ totalTickets }}枚 ・ {{ yen(totalCost) }}円</span></h3>
          <table class="uma-table">
            <thead>
              <tr><th>枠</th><th class="l">馬名</th><th>オッズ</th><th>購入枚数</th></tr>
            </thead>
            <tbody>
              <tr v-for="(h, i) in lineup" :key="i" :data-test="`horse-${i}`">
                <td>{{ i + 1 }}</td>
                <td class="l"><img :src="`/img/svg/uma/${h.img}.svg`" class="uma-ico" :alt="h.name" />{{ h.name }}</td>
                <td class="odds">{{ h.odds }}倍</td>
                <td>
                  <select v-model.number="tickets[i]">
                    <option v-for="n in TICKET_OPTIONS" :key="n" :value="n">{{ n === 0 ? '-' : n + '枚' }}</option>
                  </select>
                </td>
              </tr>
            </tbody>
          </table>
          <div class="actions">
            <button class="btn primary" :disabled="busy" @click="startRace">レース開始</button>
          </div>
        </div>

        <!-- レース(アニメ) / 結果 -->
        <div v-else class="panel-white">
          <h3>レース</h3>
          <div class="track">
            <div v-for="(h, i) in (result?.lineup ?? [])" :key="i" class="lane">
              <span class="lane-no">{{ i + 1 }}</span>
              <div class="lane-track">
                <img
                  :src="`/img/svg/uma/${h.img}.svg`"
                  class="uma-run"
                  :style="{ left: (positions[i] ?? 0) + '%' }"
                  :alt="h.name"
                />
              </div>
              <span class="goal">🏁</span>
            </div>
          </div>
          <div v-if="mode === 'result' && result" class="race-result">
            <div class="winner">{{ result.winner_index + 1 }}枠 {{ result.winner_name }} ゴール！</div>
            <div>購入金額: {{ yen(result.invested) }}円</div>
            <div :class="{ up: result.payout > 0 }">
              獲得金額: {{ result.payout > 0 ? yen(result.payout) + '円' : '残念ながら配当はありません' }}
            </div>
            <button class="btn primary" @click="retry">再挑戦</button>
          </div>
        </div>
      </div>

      <!-- ギャンブル王ランキング -->
      <div class="rank-col panel-white">
        <h3>ギャンブル王ベスト10</h3>
        <table class="rank-table">
          <thead><tr><th>#</th><th class="l">名前</th><th>儲け</th></tr></thead>
          <tbody>
            <tr v-for="(e, i) in ranking" :key="i" :class="{ me: e.name === player.display_name }">
              <td>{{ i + 1 }}</td>
              <td class="l">{{ e.name }}</td>
              <td :class="{ up: e.profit > 0, down: e.profit < 0 }">{{ yen(e.profit) }}円</td>
            </tr>
            <tr v-if="!ranking.length"><td colspan="3" class="muted">まだ記録がありません。</td></tr>
          </tbody>
        </table>
      </div>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.keiba-page {
  background-color: #99cc66;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.fac-header {
  display: flex;
  margin-bottom: 8px;
}
.fac-header .lead {
  flex: 1 1 auto;
  background: #fff;
  border: 1px solid #669933;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.fac-header .title {
  flex: 0 0 160px;
  background: #336600;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid #669933;
}
.money {
  color: #cc3300;
  font-weight: bold;
}
.keiba-body {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  flex-wrap: wrap;
}
.main-col {
  flex: 1 1 360px;
  min-width: 300px;
}
.rank-col {
  flex: 1 1 200px;
  min-width: 200px;
}
.panel-white {
  background: #fff;
  border: 1px solid #669933;
  padding: 8px;
}
.panel-white h3 {
  margin: 0 0 6px;
  font-size: 14px;
  color: #336600;
}
.hint {
  font-size: 11px;
  color: #667;
  font-weight: normal;
}
.uma-table,
.rank-table {
  border-collapse: collapse;
  width: 100%;
  font-size: 12px;
}
.uma-table th,
.rank-table th {
  background: #e5f0d8;
  color: #336600;
  padding: 3px 6px;
  border: 1px solid #cde0b8;
}
.uma-table td,
.rank-table td {
  padding: 3px 6px;
  border: 1px solid #eee;
  text-align: center;
}
.uma-table td.l,
.rank-table td.l,
.uma-table th.l,
.rank-table th.l {
  text-align: left;
}
.uma-ico {
  width: 24px;
  height: 18px;
  vertical-align: middle;
  margin-right: 4px;
}
.uma-table td.odds {
  color: #cc3300;
  font-weight: bold;
}
.up {
  color: #060;
  font-weight: bold;
}
.down {
  color: #c00;
  font-weight: bold;
}
.track {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.lane {
  display: flex;
  align-items: center;
  gap: 4px;
}
.lane-no {
  width: 16px;
  font-size: 11px;
  color: #667;
}
.lane-track {
  position: relative;
  flex: 1 1 auto;
  height: 22px;
  background: linear-gradient(#d8e8c0, #c8dcac);
  border: 1px solid #bcd0a0;
  overflow: hidden;
}
.uma-run {
  position: absolute;
  top: 1px;
  width: 26px;
  height: 20px;
  transition: left 0.09s linear;
}
.goal {
  font-size: 14px;
}
.race-result {
  margin-top: 8px;
  padding: 8px;
  background: #f5faef;
  border: 1px solid #cde0b8;
  font-size: 13px;
  line-height: 1.7;
}
.race-result .winner {
  font-weight: bold;
  font-size: 15px;
  color: #336600;
}
.rank-table tr.me {
  background: #fff3e8;
}
.rank-table .muted {
  color: #999;
}
.actions {
  margin-top: 8px;
}
.btn.primary {
  background: #336699;
  color: #fff;
  border-color: #224466;
}
</style>
