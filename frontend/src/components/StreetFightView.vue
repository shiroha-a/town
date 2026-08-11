<script setup lang="ts">
import { ref, computed, onUnmounted } from 'vue';
import { api, type Player, type FightResult } from '../api';
import { notifyError } from '../toast';
import { PARAM_LABEL } from '../params';

// ストリートファイト(レガシー game.cgi mode=battle)。通りすがりのモンスターと
// 殴り合い、勝てばお金と景品、負ければお金を奪われる。削られたパワーはそのまま
// 自分のパワーとして残るので、これ自体が回数の制限になっている。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const label = (k: string) => PARAM_LABEL[k] ?? k;

const result = ref<FightResult | null>(null);
const busy = ref(false);
// 何ターン目まで見せたか。1ターンずつ出して殴り合いに見せる。
const shown = ref(0);
let timer: number | undefined;

const finished = computed(() => result.value !== null && shown.value >= result.value.turns.length);
const visibleTurns = computed(() => result.value?.turns.slice(0, shown.value) ?? []);

// 表示中のパワー。まだ出していないターンの結果は見せない。
// 決着時のダメージは残量を超えるので、表示は0で止める(内部の値は負でよい)。
const last = computed(() => visibleTurns.value[visibleTurns.value.length - 1] ?? null);
const floor0 = (v: number) => Math.max(0, v);
const playerEnergy = computed(() =>
  floor0(last.value?.player_energy ?? props.player.status.energy),
);
const playerNou = computed(() => floor0(last.value?.player_nou ?? props.player.status.nou_energy));
const monsterEnergy = computed(() =>
  floor0(last.value?.monster_energy ?? result.value?.monster_energy_max ?? 0),
);
const monsterNou = computed(() =>
  floor0(last.value?.monster_nou ?? result.value?.monster_nou_max ?? 0),
);

function stopPlayback() {
  if (timer !== undefined) {
    clearInterval(timer);
    timer = undefined;
  }
}
onUnmounted(stopPlayback);

/** 残りのターンを一気に出す(長い戦いを待たなくてよいように)。 */
function skip() {
  stopPlayback();
  shown.value = result.value?.turns.length ?? 0;
}

async function fight() {
  if (busy.value) return;
  busy.value = true;
  stopPlayback();
  result.value = null;
  shown.value = 0;
  try {
    const res = await api.streetFight(props.player.id);
    result.value = res.result;
    emit('update', res.player);
    timer = window.setInterval(() => {
      if (!result.value || shown.value >= result.value.turns.length) {
        stopPlayback();
        return;
      }
      shown.value++;
    }, 700);
  } catch (e) {
    notifyError('戦えませんでした', e, 'battle');
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page sf-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="sf-header">
      <div class="lead">
        街を歩いていると、ケンカを売ってくる相手がいます。<br />
        勝てば相手からお金を奪えますが、負ければ奪われます。<br />
        戦いで減った身体パワー・頭脳パワーはそのまま残ります。<br />
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
      </div>
      <div class="title">ストリート<br />ファイト</div>
    </div>

    <div class="panel-white sf-start">
      <button class="btn primary" :disabled="busy" data-test="fight" @click="fight">
        {{ result ? 'もう一度出かける' : '出かける' }}
      </button>
      <span class="pw"
        >身体パワー {{ player.status.energy }}/{{ player.status.energy_max }} ・ 頭脳パワー
        {{ player.status.nou_energy }}/{{ player.status.nou_energy_max }}</span
      >
    </div>

    <template v-if="result">
      <!-- 対戦カード。左が自分、右が相手。 -->
      <div class="sf-card">
        <div class="side me">
          <div class="side-name">{{ player.display_name }}</div>
          <div class="pw-line">
            身体パワー：<b>{{ playerEnergy }}</b>
          </div>
          <div class="pw-line">
            頭脳パワー：<b>{{ playerNou }}</b>
          </div>
        </div>
        <div class="vs">
          <img :src="`/img/svg/${result.monster.icon}.svg`" :alt="result.monster.name" />
          <div class="vs-text">VS</div>
        </div>
        <div class="side foe">
          <div class="side-name">{{ result.monster.name }}（Lv{{ result.monster.level }}）</div>
          <div class="pw-line">
            身体パワー：<b>{{ monsterEnergy }}</b>
          </div>
          <div class="pw-line">
            頭脳パワー：<b>{{ monsterNou }}</b>
          </div>
        </div>
      </div>

      <div class="panel-white sf-log">
        <div
          v-for="(t, i) in visibleTurns"
          :key="i"
          class="turn"
          :class="t.attacker === 'player' ? 'mine' : 'theirs'"
        >
          <div class="atk">
            {{ t.attacker === 'player' ? player.display_name : result.monster.name }}の攻撃！（{{
              label(t.ability)
            }}）
          </div>
          <div class="line">{{ t.line }}</div>
          <div class="res" :class="{ nod: t.damage === 0 }">{{ t.text }}</div>
        </div>
        <div v-if="!finished" class="playing">
          <button class="btn mini" @click="skip">最後まで飛ばす</button>
        </div>
      </div>

      <div v-if="finished" class="panel-white sf-result" :class="result.outcome">
        <template v-if="result.outcome === 'win'">
          <div class="big">勝ちました！</div>
          <div>倒れている{{ result.monster.name }}から{{ yen(result.money) }}円を奪いました。</div>
          <div v-if="result.reward_name" class="drop">{{ result.reward_name }}を手に入れた。</div>
        </template>
        <template v-else-if="result.outcome === 'lose'">
          <div class="big">負けてしまいました。。</div>
          <div>
            ボロボロになった{{ player.display_name }}さんの財布から{{
              yen(-result.money)
            }}円を奪われました。
          </div>
        </template>
        <template v-else>
          <div class="big">決着がつきませんでした。。</div>
          <div>{{ result.monster.name }}とはにらみ合ったまま別れました。</div>
        </template>
      </div>
    </template>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.sf-page {
  background-color: #7a4b3a;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.sf-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.sf-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  color: #333;
  line-height: 1.6;
}
.sf-header .title {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 150px;
  background: #5c342a;
  color: #fff;
  font-size: 18px;
  font-weight: bold;
  text-align: center;
  line-height: 1.3;
}
.money {
  color: #cc3300;
  font-weight: bold;
}
/* 他の施設画面と同じ白パネル(各画面がそれぞれ持っている)。 */
.panel-white {
  background: #fff;
  border: 1px solid #99b;
  padding: 12px;
  margin-bottom: 8px;
}
.sf-start {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.sf-start .pw {
  font-size: 12px;
  color: #555;
}
/* 対戦カード: 左に自分、中央に相手のアイコン、右に相手。 */
.sf-card {
  display: flex;
  align-items: stretch;
  gap: 8px;
  margin-bottom: 8px;
}
.sf-card .side {
  flex: 1 1 0;
  background: #fff;
  border: 1px solid #333;
  padding: 8px 10px;
}
.sf-card .side.me {
  border-top: 4px solid #3d7fd1;
}
.sf-card .side.foe {
  border-top: 4px solid #cc3300;
  text-align: right;
}
.side-name {
  font-weight: bold;
  margin-bottom: 4px;
}
.pw-line {
  font-size: 13px;
  line-height: 1.7;
}
.vs {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: #ffd452;
}
.vs img {
  width: 40px;
  height: 40px;
}
.vs-text {
  font-weight: bold;
  font-size: 14px;
}
.sf-log {
  max-height: 420px;
  overflow-y: auto;
}
.turn {
  border-bottom: 1px dotted #ccc;
  padding: 6px 4px;
  font-size: 13px;
  line-height: 1.6;
}
.turn:last-child {
  border-bottom: none;
}
.turn .atk {
  font-weight: bold;
}
.turn.mine .atk {
  color: #2f6fc4;
}
.turn.theirs {
  text-align: right;
}
.turn.theirs .atk {
  color: #cc3300;
}
.turn .line {
  color: #333;
}
/* 効かなかった攻撃は灰色にして、当たった回と見分けられるようにする。 */
.turn .res.nod {
  color: #888;
}
.playing {
  text-align: center;
  padding: 6px 0;
}
.sf-result {
  text-align: center;
  line-height: 1.8;
}
.sf-result .big {
  font-size: 16px;
  font-weight: bold;
}
.sf-result.win .big {
  color: #339933;
}
.sf-result.lose .big {
  color: #ff3300;
}
.sf-result .drop {
  color: #a05a00;
  font-weight: bold;
}
</style>
