<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, type Player, type UserSettings, type PushPrefs } from '../api';
import { pushSupported, permission, subscribe, unsubscribe, deviceSubscribed } from '../notify';
import { unregisterServiceWorker } from '../pwa';
import ToggleSwitch from './ToggleSwitch.vue';
import Toast from './Toast.vue';
import { useToast } from '../toast';

// 住民が自分で変えられる設定。管理者が街全体を変える「ゲーム設定」とは別物で、
// ここは自分のアカウントの話だけを置く。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: []; retired: [] }>();

const form = ref<UserSettings | null>(null);
// 完了はトーストで知らせる(他の画面と同じ扱い)。エラーは画面に残す。
const { toast, showToast, closeToast } = useToast();
const error = ref('');
const busy = ref(false);

// 退会は取り消せないので、名前を打ち込ませてから実行する。
const retireOpen = ref(false);
const retireConfirm = ref('');

// --- 通知(Web Push) ---
// 既定は全部オフ。どれかをオンにしたときに初めて端末を登録し、ブラウザの許可を求める。
const push = ref<PushPrefs | null>(null);
const pushBusy = ref(false);
const canPush = pushSupported();
const denied = computed(() => permission() === 'denied');

// 「この端末が登録済みか」は端末ごとの話。サーバーが返す subscribed は
// 「この人がどれかの端末で登録済みか」なので別物として持つ。混同すると、
// 1台目で登録したあと2台目が自分を登録しないまま「オン」に見えてしまう。
const thisDevice = ref(false);

/** この端末で通知を受け取るかを切り替える。許可を求めるのはここだけ。 */
async function toggleDevice() {
  if (pushBusy.value) return;
  pushBusy.value = true;
  error.value = '';
  try {
    if (thisDevice.value) {
      await unsubscribe(props.player.id);
      thisDevice.value = false;
      showToast({
        variant: 'item',
        title: 'この端末への通知を止めました',
        lines: [],
        icon: 'usersettings',
      });
    } else {
      const ok = await subscribe(props.player.id);
      if (!ok) {
        error.value = 'ブラウザで通知が許可されませんでした。';
        return;
      }
      thisDevice.value = true;
      showToast({
        variant: 'item',
        title: 'この端末で通知を受け取ります',
        lines: [],
        icon: 'usersettings',
      });
    }
    await loadPush();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    pushBusy.value = false;
  }
}

async function loadPush() {
  try {
    push.value = await api.pushPrefs(props.player.id);
  } catch {
    push.value = null;
  }
  thisDevice.value = await deviceSubscribed();
}

/** 何を知らせるかを切り替える。こちらは人ごとの設定で、登録済みの全端末に効く。 */
async function togglePush(key: 'mail' | 'energy' | 'work') {
  if (!push.value || pushBusy.value) return;
  pushBusy.value = true;
  error.value = '';
  const next = { ...push.value, [key]: !push.value[key] };
  try {
    push.value = await api.updatePushPrefs(props.player.id, {
      mail: next.mail,
      energy: next.energy,
      work: next.work,
    });
    showToast({
      variant: 'item',
      title: '通知の設定を保存しました',
      lines: [],
      icon: 'usersettings',
    });
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    pushBusy.value = false;
  }
}

async function load() {
  void loadPush();
  try {
    form.value = await api.userSettings(props.player.id);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}
onMounted(load);

async function save() {
  if (!form.value || busy.value) return;
  busy.value = true;
  error.value = '';
  try {
    form.value = await api.updateUserSettings(props.player.id, form.value);
    showToast({ variant: 'item', title: '保存しました', lines: [], icon: 'usersettings' });
    emit('update', await api.getPlayer(props.player.id));
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}

function useMisskeyName() {
  if (form.value?.misskey_name) form.value.display_name = form.value.misskey_name;
}

// 手元に溜めた画像・プログラムを捨てて読み込み直す。古いものを掴んだまま直せない
// のが一番困るので、逃げ道として置く(画面まで来られないときは /reset.html)。
async function clearCaches() {
  if (busy.value) return;
  busy.value = true;
  error.value = '';
  try {
    await unregisterServiceWorker();
  } catch {
    // 消せなかったぶんは諦めて読み込み直す。掴んでいるものが減れば直ることがある。
  }
  location.reload();
}

async function refreshMisskey() {
  if (busy.value) return;
  busy.value = true;
  error.value = '';
  try {
    await api.refreshMisskeyProfile(props.player.id);
    await load();
    showToast({
      variant: 'item',
      title: 'Misskeyの情報を取り直しました',
      lines: [],
      icon: 'usersettings',
    });
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}

async function retire() {
  if (busy.value) return;
  busy.value = true;
  error.value = '';
  try {
    await api.retire(props.player.id, retireConfirm.value);
    emit('retired');
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page us-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>
    <div class="us-header">
      <div class="lead">
        あなたのアカウントの設定です。<br />
        街全体の設定ではありません。
      </div>
      <div class="title">ユーザー設定</div>
    </div>

    <Toast :toast="toast" @close="closeToast" />
    <div v-if="error" class="message error">{{ error }}</div>

    <div v-if="form" class="us-body">
      <section class="us-sec">
        <div class="us-head">街での名前</div>
        <div class="us-row">
          <input v-model="form.display_name" maxlength="20" class="us-name" />
          <button v-if="form.misskey_name" class="btn" @click="useMisskeyName">
            Misskeyの名前にする
          </button>
        </div>
        <div class="us-hint">
          名鑑・掲示板・あいさつに出る名前です(20文字まで)。
          <span v-if="form.misskey_name">Misskeyでの名前: {{ form.misskey_name }}</span>
        </div>
      </section>

      <section class="us-sec">
        <div class="us-head">プロフィールの公開</div>
        <ToggleSwitch
          v-model="form.profile_public"
          label="Misskeyの情報を街のプロフィールに載せる"
        />
        <div class="us-hint">
          アイコン・自己紹介・フォロー数などを、他の住民が見られるようになります。
          オフのままなら誰にも見えません(自分では見られます)。
        </div>
      </section>

      <section class="us-sec">
        <div class="us-head">通知</div>
        <template v-if="!canPush">
          <div class="us-hint">
            このブラウザは通知に対応していません。iPhone/iPadでは、
            <b>ホーム画面に追加</b>してから開くと使えます。
          </div>
        </template>
        <template v-else-if="denied">
          <div class="us-hint">
            通知がブラウザ側で拒否されています。サイトの設定から許可し直してください。
          </div>
        </template>
        <template v-else-if="push">
          <ToggleSwitch
            :model-value="thisDevice"
            label="この端末で通知を受け取る"
            @update:model-value="toggleDevice"
          />
          <div class="us-hint">
            通知は<b>端末ごとに登録</b>が要ります。スマホとPCの両方で受け取るなら、それぞれで開いてオンにしてください。
          </div>
          <div class="us-head sub">何を知らせるか</div>
          <ToggleSwitch
            :model-value="push.mail"
            label="メールが届いたら知らせる"
            @update:model-value="togglePush('mail')"
          />
          <ToggleSwitch
            :model-value="push.energy"
            label="身体パワーが満タンになったら知らせる"
            @update:model-value="togglePush('energy')"
          />
          <ToggleSwitch
            :model-value="push.work"
            label="仕事に行けるようになったら知らせる"
            @update:model-value="togglePush('work')"
          />
          <div class="us-hint">
            <template v-if="!thisDevice">
              <b>この端末は未登録です。</b>上のトグルをオンにすると届くようになります。
            </template>
            <template v-else>アプリを閉じていても届きます。</template>
            種類の設定はアカウント共通で、登録した全ての端末に効きます。
          </div>
        </template>
      </section>

      <section class="us-sec">
        <div class="us-head">Misskeyの情報</div>
        <button class="btn" :disabled="busy" @click="refreshMisskey">いま取り直す</button>
        <div class="us-hint">
          プロフィールは6時間ごとに取り直します。Misskey側で変えた直後に反映したいときに使ってください。
        </div>
      </section>

      <section class="us-sec">
        <div class="us-head">表示の不具合</div>
        <button class="btn" :disabled="busy" @click="clearCaches">
          キャッシュを破棄して読み込み直す
        </button>
        <div class="us-hint">
          更新したはずの画面や画像が古いまま出るときに使ってください。手元に溜めた画像や
          プログラムを捨てて取り直します。ログインは外れません。
        </div>
      </section>

      <div class="us-actions">
        <button class="btn primary" :disabled="busy" @click="save">保存</button>
      </div>

      <section class="us-sec danger">
        <div class="us-head">退会</div>
        <div class="us-hint">
          住民をやめて、自分のデータ(持ち物・家・お店・掲示板の書き込み・メール)を消します。
          <b>取り消せません。</b>所持金と貯金は街へ戻ります。
        </div>
        <button v-if="!retireOpen" class="btn danger-btn" @click="retireOpen = true">
          退会する
        </button>
        <div v-else class="retire-form">
          <div class="us-hint">
            確認のため、街での名前「{{ form.display_name }}」を入力してください。
          </div>
          <div class="us-row">
            <input v-model="retireConfirm" class="us-name" placeholder="街での名前" />
            <button class="btn danger-btn" :disabled="busy || !retireConfirm" @click="retire">
              退会を確定する
            </button>
            <button class="btn" @click="retireOpen = false">やめる</button>
          </div>
        </div>
      </section>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.us-page {
  background-color: #e8dcc0;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.us-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.us-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  color: #333;
  line-height: 1.6;
}
.us-header .title {
  flex: 0 0 140px;
  background: #997a44;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.us-body {
  background: #fff;
  border: 1px solid #999;
  padding: 12px 14px;
  max-width: 640px;
}
.us-sec {
  padding-bottom: 12px;
  margin-bottom: 12px;
  border-bottom: 1px dotted #ccc;
}
.us-head.sub {
  margin-top: 10px;
  font-size: 12px;
  color: #778;
}
.us-head {
  font-size: 13px;
  font-weight: bold;
  color: #663300;
  margin-bottom: 6px;
}
.us-row {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.us-name {
  font-size: 14px;
  padding: 4px 6px;
  flex: 0 1 220px;
}
.us-hint {
  font-size: 11px;
  color: #888;
  line-height: 1.7;
  margin-top: 4px;
}
.us-actions {
  margin-bottom: 12px;
}
.us-sec.danger {
  border: 1px solid #e0b4b4;
  background: #fff8f8;
  padding: 10px;
  border-radius: 3px;
}
.us-sec.danger .us-head {
  color: #a33;
}
.btn.danger-btn {
  border-color: #c99;
  color: #a33;
}
.retire-form {
  margin-top: 6px;
}
.message {
  margin-bottom: 8px;
}
@media (max-width: 700px) {
  .us-header .title {
    flex-basis: 96px;
    font-size: 14px;
  }
}
</style>
