<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { api, type Player, type ShopItem } from '../api';
import PowerBar from './PowerBar.vue';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const baths = ref<ShopItem[]>([]);
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);

// 画面フェーズ。入浴ボタンを押すと結果画面(bathing)に移る。
const phase = ref<'select' | 'bathing'>('select');
const lastBath = ref<ShopItem | null>(null);
// 入浴開始時のパワーを基準に、この入浴での累積回復量をリアルタイム表示する。
const baseEnergy = ref(0);
const baseNou = ref(0);
const gainedEnergy = computed(() => Math.max(0, props.player.status.energy - baseEnergy.value));
const gainedNou = computed(() => Math.max(0, props.player.status.nou_energy - baseNou.value));

// サーバ時刻基準の1秒クロック。満タンまでの残り時間をリアルタイム表示するため。
const skewMs = ref(0);
function syncSkew() {
  const serverNow = new Date(props.player.server_now).getTime();
  if (!Number.isNaN(serverNow)) skewMs.value = serverNow - Date.now();
}
syncSkew();
watch(() => props.player.server_now, syncSkew);
const nowMs = ref(Date.now());
let timer: number | undefined;
// 入浴中はサーバから最新パワーをポーリングし、倍率回復が進む様子を表示する。
let pollTimer: number | undefined;

onMounted(async () => {
  timer = window.setInterval(() => {
    nowMs.value = Date.now();
  }, 1000);
  try {
    baths.value = await api.facilityMenu('onsen');
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
    kind.value = 'error';
  }
});
onUnmounted(() => {
  if (timer !== undefined) window.clearInterval(timer);
  stopPolling();
  // 入浴中に画面を離れたら通常速度へ戻す(念のため)。
  if (phase.value === 'bathing') {
    api.onsenLeave(props.player.id).catch(() => {});
  }
});

const serverCorrectedNow = computed(() => nowMs.value + skewMs.value);
// パワーが満タンになる時刻までの残り時間(満タン中はnull)。時/分/秒で表示する。
function fullRemain(fullAt: string | null): string | null {
  if (!fullAt) return null;
  const target = new Date(fullAt).getTime();
  const remain = target - serverCorrectedNow.value;
  if (Number.isNaN(target) || remain <= 0) return null;
  const sec = Math.ceil(remain / 1000);
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (h > 0) return `${h}時間${String(m).padStart(2, '0')}分`;
  if (m > 0) return `${m}分${String(s).padStart(2, '0')}秒`;
  return `${s}秒`;
}
const energyFullRemain = computed(() => fullRemain(props.player.status.energy_full_at));
const nouFullRemain = computed(() => fullRemain(props.player.status.nou_energy_full_at));

// 両パワーとも満タンか。
const isFull = computed(
  () =>
    props.player.status.energy >= props.player.status.energy_max &&
    props.player.status.nou_energy >= props.player.status.nou_energy_max,
);

function startPolling() {
  stopPolling();
  pollTimer = window.setInterval(async () => {
    try {
      // onsenTickは「その時点まで回復を確定して」返すため、workerの粗いtickを
      // 待たずに2秒ごとパワーが増えていく。
      const p = await api.onsenTick(props.player.id);
      emit('update', p);
      if (p.status.energy >= p.status.energy_max && p.status.nou_energy >= p.status.nou_energy_max) {
        stopPolling(); // 満タンになったら回復は止まる
      }
    } catch {
      // 一時的な失敗は無視し、次回のポーリングで追従する。
    }
  }, 2000);
}
function stopPolling() {
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer);
    pollTimer = undefined;
  }
}

async function bathe(bath: ShopItem) {
  busy.value = true;
  message.value = '';
  // 入浴開始時のパワーを基準にし、以降の回復量を差分で表示する。
  baseEnergy.value = props.player.status.energy;
  baseNou.value = props.player.status.nou_energy;
  try {
    const updated = await api.onsenBathe(props.player.id, bath.id);
    emit('update', updated);
    lastBath.value = bath;
    phase.value = 'bathing';
    startPolling();
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
    kind.value = 'error';
  } finally {
    busy.value = false;
  }
}

// 入浴を終える(回復倍率を通常に戻す)。
async function leaveOnsen() {
  stopPolling();
  try {
    emit('update', await api.onsenLeave(props.player.id));
  } catch {
    // 失敗しても画面遷移は行う(次アクセス時に整合する)。
  }
}

// 別の風呂に入るため選択画面へ戻る。
async function backToSelect() {
  await leaveOnsen();
  phase.value = 'select';
  message.value = '';
}

// 街へ戻る(入浴中なら通常速度に戻してから)。
async function backToTown() {
  if (phase.value === 'bathing') await leaveOnsen();
  emit('back');
}
</script>

<template>
  <div class="facility-page onsen-page">
    <button class="btn back" @click="backToTown">街に戻る</button>

    <!-- 入浴後の画面 -->
    <template v-if="phase === 'bathing'">
      <h2 class="onsen-title">入浴中</h2>
      <div class="onsen-scene">
        <img src="/img/svg/onsen.svg" alt="温泉" class="onsen-img" />
      </div>
      <div class="bathe-result">
        <p class="lead">
          {{ lastBath?.name }}（回復倍率×{{ lastBath?.power_multiplier }}）に入っています。<br />
          <span v-if="!isFull">通常の{{ lastBath?.power_multiplier }}倍の速さでパワーが回復中です。</span>
          <span v-else>パワーは満タンになりました。</span>
        </p>
        <PowerBar
          label="身体パワー"
          :value="player.status.energy"
          :max="player.status.energy_max"
          :full-remain="energyFullRemain"
        />
        <PowerBar
          label="頭脳パワー"
          :value="player.status.nou_energy"
          :max="player.status.nou_energy_max"
          :full-remain="nouFullRemain"
        />
        <p class="gain-note">この入浴で 身体+{{ gainedEnergy }} ／ 頭脳+{{ gainedNou }} 回復しました。</p>
      </div>
      <div class="bathe-actions">
        <button class="btn" :disabled="busy" @click="backToSelect">別の風呂に入る</button>
        <button class="btn" @click="backToTown">街に戻る</button>
      </div>
    </template>

    <!-- 風呂の選択画面 -->
    <template v-else>
      <h2 class="onsen-title">風呂の選択</h2>
      <div class="onsen-sub">
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
        ／ 身体パワー {{ player.status.energy }}/{{ player.status.energy_max }}
        ・頭脳パワー {{ player.status.nou_energy }}/{{ player.status.nou_energy_max }}
      </div>

      <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

      <div class="onsen-note">
        風呂は自然回復を「回復倍率」ぶん加速します。入浴中はその速さで回復し続け、満タンになるか街に戻ると終了します。
      </div>

      <div class="bath-box">
        <table class="bath-table">
          <thead>
            <tr>
              <th class="l">風呂</th>
              <th>料金</th>
              <th>回復倍率</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="bath in baths" :key="bath.id" :data-test="`bath-${bath.id}`">
              <td class="l">{{ bath.name }}</td>
              <td class="price">{{ yen(bath.price) }}円</td>
              <td class="mult">×{{ bath.power_multiplier }}</td>
              <td>
                <button class="btn" :disabled="busy" @click="bathe(bath)">入る</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div style="text-align: center; margin-top: 8px">
        <button class="btn" @click="backToTown">街へ戻る</button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.onsen-page {
  background-color: #336699;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.onsen-title {
  text-align: center;
  color: #fff;
  font-size: 18px;
  margin: 10px 0 6px;
}
.onsen-sub {
  text-align: center;
  color: #fff;
  font-size: 12px;
  margin-bottom: 10px;
}
.money {
  color: #ffff66;
  font-weight: bold;
}
.bath-box {
  background: #ffffcc;
  border: 1px solid #666;
  max-width: 460px;
  margin: 0 auto;
  padding: 10px 14px;
}
.bath-table {
  width: 100%;
  border-collapse: collapse;
}
.bath-table th {
  padding: 4px 8px;
  background: #f0e6b0;
  color: #663300;
  border-bottom: 1px solid #ccb;
  font-size: 12px;
}
.bath-table th.l {
  text-align: left;
}
.bath-table td {
  padding: 4px 8px;
  text-align: center;
}
.bath-table td.l {
  text-align: left;
  font-weight: bold;
}
.bath-table .price {
  color: #cc3300;
  font-weight: bold;
  text-align: right;
}
.bath-table .mult {
  color: #0066aa;
  font-weight: bold;
}
.onsen-note {
  max-width: 460px;
  margin: 0 auto 8px;
  color: #ffffcc;
  font-size: 11px;
  line-height: 1.5;
  text-align: center;
}
.onsen-scene {
  text-align: center;
  margin: 8px 0;
}
.onsen-img {
  max-width: 90%;
  max-height: 220px;
  border: 3px solid #fff;
  border-radius: 4px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
}
.bathe-result {
  background: #ffffcc;
  border: 1px solid #666;
  max-width: 460px;
  margin: 0 auto;
  padding: 12px 16px;
}
.bathe-result .lead {
  text-align: center;
  color: #663300;
  font-size: 13px;
  line-height: 1.6;
  margin-bottom: 10px;
}
.gain-note {
  text-align: center;
  color: #cc3300;
  font-size: 12px;
  margin-top: 10px;
}
.bathe-actions {
  margin-top: 12px;
  display: flex;
  gap: 10px;
  justify-content: center;
}
</style>
