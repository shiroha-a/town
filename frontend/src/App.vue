<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue';
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
import ProfileView from './components/ProfileView.vue';
import TsuriView from './components/TsuriView.vue';
import GiftShopView from './components/GiftShopView.vue';
import TokutenView from './components/TokutenView.vue';
import BingoView from './components/BingoView.vue';
import AdminView from './components/AdminView.vue';
import PlaceholderView from './components/PlaceholderView.vue';

const player = ref<Player | null>(null);
const view = ref('town');

// ログイン状態はHttpOnly cookieのセッションで持つ(MiAuth)。
// 起動時に /auth/me で復元し、未ログインならログイン画面を出す。
const booting = ref(true);

onMounted(async () => {
  try {
    player.value = await api.authMe();
  } catch {
    player.value = null;
  } finally {
    booting.value = false;
  }
});

function onLogin(p: Player) {
  player.value = p;
  view.value = 'town';
}
function onUpdate(p: Player) {
  player.value = p;
}
// ログアウト直後だけログイン画面に後始末の案内を出す。ホストは直前の
// プレイヤーから取る(このブラウザでログインしていないと入力欄は空のため)。
const loggedOut = ref(false);
const loggedOutHost = ref('');

async function onLogout() {
  // MiAuthはログインのたびにMisskey側で新しいアクセストークンを発行する。
  // i/revoke-token は secure:true でアクセストークンから呼べないため、
  // こちらから古いトークンを消せない。無駄なログアウトを減らすために断りを入れる。
  const ok = window.confirm(
    'ログアウトしますか？\n次にログインすると、Misskey側で新しいアクセストークンが発行されます。',
  );
  if (!ok) return;
  loggedOutHost.value = player.value?.instance_host ?? '';
  try {
    await api.authLogout();
  } catch {
    // 失敗してもクライアント側は未ログイン扱いにする。
  }
  player.value = null;
  view.value = 'town';
  loggedOut.value = true;
}
// お試しプレイ(ゲスト)の残り時間。1分ごとに更新し、切れたらログイン画面へ戻す。
const now = ref(Date.now());
let guestTimer: number | undefined;
const guestRemainMin = computed(() => {
  const at = player.value?.guest_expires_at;
  if (!player.value?.is_guest || !at) return null;
  const left = new Date(at).getTime() - now.value;
  return left > 0 ? Math.ceil(left / 60000) : 0;
});

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
//
// 間隔はサーバーの回復間隔(1ポイントあたりの秒数)に合わせる。固定値だと、
// 回復が遅い設定では無駄に叩き、速い設定では表示が追いつかないため。
// 設定を変えても次回の予約から効くよう、都度読み直す。
const POLL_MIN_SEC = 5;
const POLL_FALLBACK_SEC = 10;
let pollTimer: number | undefined;

function pollIntervalMs(): number {
  const st = player.value?.status;
  const secs = [st?.energy_recovery_sec, st?.nou_recovery_sec].filter(
    (v): v is number => typeof v === 'number' && v > 0,
  );
  const sec = secs.length ? Math.min(...secs) : POLL_FALLBACK_SEC;
  return Math.max(POLL_MIN_SEC, sec) * 1000;
}

function schedulePoll() {
  pollTimer = window.setTimeout(async () => {
    if (player.value && view.value === 'town') {
      try {
        player.value = await api.getPlayer(player.value.id);
      } catch {
        // 一時的な失敗は無視して次回に任せる。
      }
    }
    schedulePoll();
  }, pollIntervalMs());
}
onMounted(schedulePoll);
onMounted(() => {
  guestTimer = window.setInterval(() => {
    now.value = Date.now();
    // 期限切れのゲストはサーバー側で消えるため、画面も入口へ戻す。
    if (guestRemainMin.value === 0) player.value = null;
  }, 30000);
});
onUnmounted(() => {
  if (pollTimer !== undefined) window.clearTimeout(pollTimer);
  if (guestTimer !== undefined) window.clearInterval(guestTimer);
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
  tsuri: '釣りゲーム',
  gifutoya: 'ギフト屋',
  tokuten: '特典交換所',
  bingo: 'ビンゴ会場',
};
</script>

<template>
  <template v-if="booting">
    <h1 class="town-title">Ｔｏｗｎ</h1>
    <div class="booting">読み込み中…</div>
  </template>
  <template v-else-if="!player">
    <LoginView :logged-out="loggedOut" :logged-out-host="loggedOutHost" @login="onLogin" />
  </template>
  <template v-else>
    <div v-if="player.is_guest" class="guest-bar">
      お試しプレイ中<span v-if="guestRemainMin !== null">（残り約{{ guestRemainMin }}分）</span>
      — データは保存されません。家の建築・銀行・あいさつ・メールは使えません。
    </div>
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
    <TsuriView v-else-if="view === 'tsuri'" :player="player" @update="onUpdate" @back="back" />
    <GiftShopView v-else-if="view === 'gifutoya'" :player="player" @update="onUpdate" @back="back" />
    <TokutenView v-else-if="view === 'tokuten'" :player="player" @update="onUpdate" @back="back" />
    <BingoView v-else-if="view === 'bingo'" :player="player" @update="onUpdate" @back="back" />
    <YakubaView v-else-if="view === 'yakuba'" :player="player" @back="back" />
    <ProfileView v-else-if="view === 'prof'" :player="player" @back="back" />
    <AdminView v-else-if="view === 'admin'" :player="player" @back="back" />
    <PlaceholderView v-else :title="facilityTitles[view] ?? view" @back="back" />
  </template>

  <div class="footer">
    <div class="copyright">&copy; 2026 shiroha-a</div>
    <div class="report">
      <a href="https://github.com/shiroha-a/town/issues" target="_blank" rel="noopener noreferrer">
        要望・不具合報告
      </a>
    </div>
  </div>
</template>

<style scoped>
.guest-bar {
  background: #fff3d4;
  border: 1px solid #e0c98a;
  color: #7a5f18;
  font-size: 12px;
  line-height: 1.6;
  padding: 5px 10px;
  margin-bottom: 6px;
  text-align: center;
}
.booting {
  text-align: center;
  color: #666;
  font-size: 14px;
  padding: 40px 0;
}
</style>
