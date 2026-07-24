<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { api, type Player } from './api';
import LoginView from './components/LoginView.vue';
import TownView from './components/TownView.vue';
import GameView from './components/GameView.vue';
import DepartView from './components/DepartView.vue';
import BankView from './components/BankView.vue';
import ItemView from './components/ItemView.vue';
import JobChangeView from './components/JobChangeView.vue';
import SyokudouView from './components/SyokudouView.vue';
import FacilityMenuView from './components/FacilityMenuView.vue';
import HanbaiView from './components/HanbaiView.vue';
import KentikuView from './components/KentikuView.vue';
import HouseView from './components/HouseView.vue';
import MyHouseView from './components/MyHouseView.vue';
import OnsenView from './components/OnsenView.vue';
import HospitalView from './components/HospitalView.vue';
import SchoolView from './components/SchoolView.vue';
import KabuView from './components/KabuView.vue';
import KeibaView from './components/KeibaView.vue';
import MailView from './components/MailView.vue';
import AshiatoView from './components/AshiatoView.vue';
import CLeagueView from './components/CLeagueView.vue';
import YakubaView from './components/YakubaView.vue';
import AdminView from './components/AdminView.vue';
import PlaceholderView from './components/PlaceholderView.vue';

const player = ref<Player | null>(null);
const view = ref('town');

// 開発用の簡易セッション(MiAuth導入時に本認証へ置換)。
const STORAGE_KEY = 'town.playerId';

onMounted(async () => {
  const saved = localStorage.getItem(STORAGE_KEY);
  if (saved) {
    try {
      player.value = await api.getPlayer(Number(saved));
    } catch {
      localStorage.removeItem(STORAGE_KEY);
    }
  }
});

function onLogin(p: Player) {
  player.value = p;
  view.value = 'town';
  localStorage.setItem(STORAGE_KEY, String(p.id));
}
function onUpdate(p: Player) {
  player.value = p;
}
function onLogout() {
  player.value = null;
  view.value = 'town';
  localStorage.removeItem(STORAGE_KEY);
}
// 家訪問(view='house')で開く家のID。街の家クリックからnavigate経由で渡される。
const houseId = ref<number | null>(null);
// 建設会社(view='kentiku')の初期建築ターゲット。街マップの空き地クリックから渡される。
const kentikuTarget = ref<{ town: number; row: number; col: number } | null>(null);
function navigate(v: string, param?: number | { town: number; row: number; col: number }) {
  view.value = v;
  houseId.value = v === 'house' && typeof param === 'number' ? param : null;
  kentikuTarget.value = v === 'kentiku' && typeof param === 'object' ? param : null;
}
function back() {
  view.value = 'town';
}
async function reload() {
  if (player.value) player.value = await api.getPlayer(player.value.id);
}

// メイン画面では一定間隔でステータスを取り込み、パワー回復・コンディション・
// 就労可否などをリアルタイムに近い形で反映する(サブ画面では操作を妨げないため停止)。
let pollTimer: number | undefined;
onMounted(() => {
  pollTimer = window.setInterval(() => {
    if (player.value && view.value === 'town') {
      api
        .getPlayer(player.value.id)
        .then((p) => {
          player.value = p;
        })
        .catch(() => {});
    }
  }, 10000);
});
onUnmounted(() => {
  if (pollTimer !== undefined) window.clearInterval(pollTimer);
});

// 施設タイトル(準備中ビュー用)
const facilityTitles: Record<string, string> = {
  kabu: '株取引場',
  syokudou: 'セントラル食堂',
  gym: 'ジム',
  keiba: '競馬場',
  onsen: '温泉',
  kentiku: '建設会社',
  prof: 'プロフィール',
  mail: 'メール',
  doukyo: 'キャラ作成',
};
</script>

<template>
  <template v-if="!player">
    <h1 class="town-title">Ｔｏｗｎ</h1>
    <LoginView @login="onLogin" />
  </template>
  <template v-else>
    <TownView v-if="view === 'town'" :player="player" @navigate="navigate" @reload="reload" @logout="onLogout" />
    <GameView v-else-if="view === 'casino'" :player="player" @update="onUpdate" @back="back" />
    <DepartView v-else-if="view === 'depart'" :player="player" @update="onUpdate" @back="back" />
    <BankView v-else-if="view === 'bank'" :player="player" @update="onUpdate" @back="back" />
    <ItemView v-else-if="view === 'item'" :player="player" @update="onUpdate" @back="back" />
    <JobChangeView v-else-if="view === 'jobchange'" :player="player" @update="onUpdate" @back="back" />
    <SyokudouView v-else-if="view === 'syokudou'" :player="player" @update="onUpdate" @back="back" />
    <HanbaiView v-else-if="view === 'hanbai'" :player="player" @update="onUpdate" @back="back" />
    <KentikuView v-else-if="view === 'kentiku'" :player="player" :initial-target="kentikuTarget" @update="onUpdate" @back="back" />
    <HouseView v-else-if="view === 'house' && houseId" :player="player" :house-id="houseId" @update="onUpdate" @back="back" />
    <MyHouseView v-else-if="view === 'myhouse'" :player="player" @update="onUpdate" @back="back" />
    <FacilityMenuView
      v-else-if="view === 'gym'"
      :player="player"
      facility="gym"
      title="スポーツクラブ"
      lead="今日も張り切って体を鍛えましょう。"
      use-label="鍛える"
      @update="onUpdate"
      @back="back"
    />
    <FacilityMenuView
      v-else-if="view === 'kyushitu'"
      :player="player"
      facility="kyushitu"
      title="教室"
      lead="今日も張り切って鍛えましょう。"
      use-label="受講する"
      @update="onUpdate"
      @back="back"
    />
    <OnsenView v-else-if="view === 'onsen'" :player="player" @update="onUpdate" @back="back" />
    <HospitalView v-else-if="view === 'hospital'" :player="player" @update="onUpdate" @back="back" />
    <SchoolView v-else-if="view === 'school'" :player="player" @update="onUpdate" @back="back" />
    <KabuView v-else-if="view === 'kabu'" :player="player" @update="onUpdate" @back="back" />
    <KeibaView v-else-if="view === 'keiba'" :player="player" @update="onUpdate" @back="back" />
    <MailView v-else-if="view === 'mail'" :player="player" @back="back" />
    <AshiatoView v-else-if="view === 'ashiato'" :player="player" @back="back" />
    <CLeagueView v-else-if="view === 'doukyo'" :player="player" @update="onUpdate" @back="back" />
    <YakubaView v-else-if="view === 'yakuba'" :player="player" @back="back" />
    <AdminView v-else-if="view === 'admin'" :player="player" @back="back" />
    <PlaceholderView v-else :title="facilityTitles[view] ?? view" @back="back" />
  </template>
</template>
