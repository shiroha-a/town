<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { api, type Player, type Character, type CLeagueRank, type BattleResult } from '../api';
import { PARAM_LABEL } from '../params';

// Cリーグ: 自分のパラメータとお金を注いでバトルキャラを育て、他プレイヤーのキャラと対戦する。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const ABILITIES = [
  'kokugo', 'suugaku', 'rika', 'syakai', 'eigo', 'ongaku', 'bijutsu',
  'looks', 'tairyoku', 'kenkou', 'speed', 'power', 'wanryoku', 'kyakuryoku', 'love', 'omoshirosa',
];
const label = (k: string) => PARAM_LABEL[k] ?? k;
const playerParam = (k: string) => (props.player.params as unknown as Record<string, number>)[k] ?? 0;

const character = ref<Character | null>(null);
const ranking = ref<CLeagueRank[]>([]);
const newName = ref('');
const grow = reactive<Record<string, number>>({});
const opponentId = ref<number | ''>('');
const battle = ref<BattleResult | null>(null);
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);

const growCost = computed(() => Object.values(grow).reduce((a, b) => a + (b || 0), 0) * 10000);
const opponents = computed(() => ranking.value.filter((r) => r.owner_id !== props.player.id));

async function load() {
  try {
    character.value = await api.getCharacter(props.player.id);
    ranking.value = await api.cleague();
  } catch (e) {
    fail(e);
  }
}
onMounted(load);

function fail(e: unknown) {
  message.value = e instanceof Error ? e.message : String(e);
  kind.value = 'error';
}

async function create() {
  if (!newName.value.trim()) return;
  busy.value = true;
  message.value = '';
  try {
    emit('update', await api.setCharacterName(props.player.id, newName.value));
    message.value = 'キャラを作成しました。';
    kind.value = 'ok';
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function doGrow() {
  const inputs: Record<string, number> = {};
  for (const k of ABILITIES) if (grow[k] > 0) inputs[k] = grow[k];
  if (!Object.keys(inputs).length) {
    message.value = '育成する能力を入力してください。';
    kind.value = 'error';
    return;
  }
  busy.value = true;
  message.value = '';
  try {
    emit('update', await api.growCharacter(props.player.id, inputs));
    message.value = 'キャラを育成しました。';
    kind.value = 'ok';
    for (const k of ABILITIES) grow[k] = 0;
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function doBattle() {
  if (opponentId.value === '') return;
  busy.value = true;
  message.value = '';
  battle.value = null;
  try {
    const res = await api.battle(props.player.id, opponentId.value);
    emit('update', res.player);
    battle.value = res.result;
    const w = res.result.winner;
    message.value = w === 'a' ? '勝利！' : w === 'b' ? '敗北…' : '引き分け';
    kind.value = w === 'b' ? 'error' : 'ok';
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page cl-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="fac-header">
      <div class="lead">
        自分のパラメータとお金を注いでキャラを育て、Cリーグで対戦しましょう。<br />
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
      </div>
      <div class="title">Cリーグ</div>
    </div>

    <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

    <!-- キャラ未作成 -->
    <div v-if="!character" class="panel-white">
      <h3>キャラクター作成</h3>
      <div class="create-row">
        <input v-model="newName" maxlength="30" placeholder="キャラ名" />
        <button class="btn primary" :disabled="busy" @click="create">作成する</button>
      </div>
    </div>

    <!-- キャラあり -->
    <template v-else>
      <div class="cl-body">
        <div class="main-col">
          <div class="panel-white char-card">
            <h3>{{ character.name }}</h3>
            <div class="derived">
              頭の良さ <b>{{ character.zunou }}</b> ／ 身体能力 <b>{{ character.sintai }}</b>
              ／ 戦績 {{ character.wins }}勝 {{ character.losses }}敗 {{ character.draws }}分
            </div>
          </div>

          <div class="panel-white">
            <h3>育成（本人の能力とお金を注入）<span class="hint"> 費用: {{ yen(growCost) }}円</span></h3>
            <div class="grow-grid">
              <label v-for="k in ABILITIES" :key="k" class="grow-cell">
                <span class="gl">{{ label(k) }}</span>
                <span class="cur">キャラ{{ character.abilities[k] ?? 0 }} / 本人{{ playerParam(k) }}</span>
                <input type="number" min="0" v-model.number="grow[k]" />
              </label>
            </div>
            <div class="actions">
              <button class="btn primary" :disabled="busy" @click="doGrow">育成する</button>
            </div>
          </div>

          <div class="panel-white">
            <h3>対戦</h3>
            <div class="battle-row">
              <select v-model="opponentId">
                <option value="">相手のキャラを選択</option>
                <option v-for="o in opponents" :key="o.owner_id" :value="o.owner_id">
                  {{ o.char_name }}（{{ o.owner_name }}）
                </option>
              </select>
              <button class="btn primary" :disabled="busy || opponentId === ''" @click="doBattle">対戦する</button>
            </div>
            <div v-if="battle" class="battle-log">
              <div v-for="(rd, i) in battle.rounds" :key="i" class="round" :class="rd.winner">
                第{{ i + 1 }}戦 [{{ label(rd.ability) }}] {{ rd.comment }}
                <span class="score">{{ rd.a_score }} vs {{ rd.b_score }}</span>
                <span class="rw">{{ rd.winner === 'a' ? '○勝' : rd.winner === 'b' ? '×負' : '△分' }}</span>
              </div>
            </div>
          </div>
        </div>

        <div class="rank-col panel-white">
          <h3>Cリーグ順位</h3>
          <table class="rank-table">
            <thead><tr><th>#</th><th class="l">キャラ</th><th>戦績</th></tr></thead>
            <tbody>
              <tr v-for="(r, i) in ranking" :key="i" :class="{ me: r.owner_id === player.id }">
                <td>{{ i + 1 }}</td>
                <td class="l">{{ r.char_name }}<span class="ro">({{ r.owner_name }})</span></td>
                <td>{{ r.wins }}-{{ r.losses }}-{{ r.draws }}</td>
              </tr>
              <tr v-if="!ranking.length"><td colspan="3" class="muted">まだキャラがいません。</td></tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.cl-page {
  background-color: #d8d0e8;
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
  border: 1px solid #a9c;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.fac-header .title {
  flex: 0 0 160px;
  background: #663399;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid #a9c;
}
.money {
  color: #cc3300;
  font-weight: bold;
}
.panel-white {
  background: #fff;
  border: 1px solid #a9c;
  padding: 10px;
  margin-bottom: 8px;
}
.panel-white h3 {
  margin: 0 0 8px;
  font-size: 14px;
  color: #442266;
}
.hint {
  font-size: 11px;
  color: #cc3300;
  font-weight: normal;
}
.create-row,
.battle-row {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.char-card .derived {
  font-size: 13px;
  color: #333;
}
.cl-body {
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
.grow-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px;
}
.grow-cell {
  display: flex;
  flex-direction: column;
  font-size: 11px;
  border: 1px solid #eee;
  padding: 3px 5px;
}
.grow-cell .gl {
  font-weight: bold;
  color: #442266;
}
.grow-cell .cur {
  color: #889;
  font-size: 10px;
}
.grow-cell input {
  width: 70px;
}
.battle-log {
  margin-top: 8px;
  font-size: 12px;
}
.battle-log .round {
  padding: 3px 5px;
  border-bottom: 1px solid #eee;
}
.battle-log .round.a {
  background: #eef7ee;
}
.battle-log .round.b {
  background: #f7eeee;
}
.battle-log .score {
  color: #667;
  margin: 0 6px;
}
.battle-log .rw {
  font-weight: bold;
}
.rank-table {
  border-collapse: collapse;
  width: 100%;
  font-size: 12px;
}
.rank-table th,
.rank-table td {
  border: 1px solid #eee;
  padding: 3px 6px;
  text-align: center;
}
.rank-table th {
  background: #eee6f5;
  color: #442266;
}
.rank-table td.l,
.rank-table th.l {
  text-align: left;
}
.rank-table .ro {
  color: #99a;
  font-size: 10px;
  margin-left: 3px;
}
.rank-table tr.me {
  background: #fff3e8;
}
.muted {
  color: #999;
}
.btn.primary {
  background: #663399;
  color: #fff;
  border-color: #442266;
}
</style>
