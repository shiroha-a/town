<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, type Player, type PublicSummary, type NewsEntry } from '../api';

// MiAuthログイン。自分のMisskeyインスタンスを入力すると、そのインスタンスの
// 承認画面へ飛び、戻ってくるとログインが完了する(アプリの事前登録は不要)。
//
// 入口では街の様子(住民と最近の出来事)も見せる。どちらも公開APIなので
// ログイン前でも引ける。取れなくてもログインの邪魔はしない。
const props = defineProps<{ loggedOut?: boolean; loggedOutHost?: string }>();
const emit = defineEmits<{ login: [player: Player] }>();

const STORAGE_KEY = 'town.instance';

const instance = ref('');
const error = ref('');
const busy = ref(false);
// コールバックから戻った直後の引き換え中かどうか。
const exchanging = ref(false);

const roster = ref<PublicSummary[]>([]);
const news = ref<NewsEntry[]>([]);

onMounted(async () => {
  // 前回使ったインスタンスを覚えておく(入力の手間を減らすだけ)。
  instance.value = localStorage.getItem(STORAGE_KEY) ?? '';

  // /auth/callback?session=... で戻ってきたら引き換える。
  const url = new URL(window.location.href);
  const session = url.searchParams.get('session');
  if (session) {
    exchanging.value = true;
    try {
      const p = await api.authCallback(session);
      // URLからsessionを消してから通常画面へ(リロードで再送されないように)。
      window.history.replaceState({}, '', '/');
      emit('login', p);
      return;
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
      window.history.replaceState({}, '', '/');
    } finally {
      exchanging.value = false;
    }
  }
  void loadTown();
});

async function loadTown() {
  const [r, n] = await Promise.allSettled([api.listPlayers(), api.townNews(8)]);
  if (r.status === 'fulfilled') roster.value = r.value;
  if (n.status === 'fulfilled') news.value = n.value;
}

// 新しい住民から順に数人だけ出す。
const newcomers = computed(() =>
  [...roster.value].sort((a, b) => b.created_at.localeCompare(a.created_at)).slice(0, 6),
);

// ログアウト直後は、残ったアクセストークンを消せる場所を案内する。
const tokenSettingsURL = computed(() => {
  const host = props.loggedOutHost?.trim() || instance.value.trim();
  return host ? `https://${host}/settings/apps` : '';
});

function fmtDay(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

async function login() {
  if (!instance.value.trim()) {
    error.value = 'インスタンスを入力してください。';
    return;
  }
  error.value = '';
  busy.value = true;
  try {
    const res = await api.authStart(instance.value);
    localStorage.setItem(STORAGE_KEY, res.instance);
    // インスタンスの承認画面へ。承認するとcallbackへ戻ってくる。
    window.location.href = res.url;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    busy.value = false;
  }
}
</script>

<template>
  <div class="entrance">
    <div class="signboard">
      <div class="town-name">ＴＯＷＮ</div>
      <div class="tagline">働いて、買って、暮らす街</div>
    </div>

    <div v-if="exchanging" class="exchanging" data-test="exchanging">ログインしています…</div>

    <template v-else>
      <div class="cols">
        <!-- 入口 -->
        <div class="ent-gate">
          <div class="box-head">街に入る</div>
          <div class="box-body">
            <p class="lead">
              お使いのMisskeyインスタンスを入力してください。<br />
              承認画面が開き、許可すると街に入れます。
            </p>
            <div class="row">
              <span class="lbl">インスタンス</span>
              <input
                type="text"
                v-model="instance"
                placeholder="misskey.io"
                data-test="instance"
                autocapitalize="off"
                autocorrect="off"
                spellcheck="false"
                @keydown.enter="login"
              />
              <button class="btn primary" :disabled="busy" data-test="login" @click="login">
                {{ busy ? '接続中…' : 'ログイン' }}
              </button>
            </div>

            <p v-if="props.loggedOut" class="logged-out">
              ログアウトしました。<br />
              使わなくなったアクセストークンは
              <a v-if="tokenSettingsURL" :href="tokenSettingsURL" target="_blank" rel="noopener noreferrer">
                Misskeyの設定
              </a>
              <span v-else>Misskeyの設定</span>
              から削除できます（設定 → アプリ）。
            </p>
            <div v-if="error" class="message error" data-test="login-error">{{ error }}</div>

            <p class="note">
              ※初めての方はこの操作でそのまま登録されます。<br />
              ※ゲーム内から他の住民をフォローするための許可だけをお願いしています。
            </p>
          </div>
        </div>

        <!-- 街の様子 -->
        <div class="ent-now">
          <div class="box-head">この街のいま</div>
          <div class="box-body">
            <div class="stat">
              現在の住民 <b>{{ roster.length }}</b> 人
            </div>
            <ul v-if="newcomers.length" class="residents">
              <li v-for="m in newcomers" :key="m.id">
                <span class="rname">{{ m.display_name }}</span>
                <span class="rjob">（{{ m.job }}）</span>
              </li>
            </ul>
            <div v-else class="empty">まだ住民がいません。最初の住民になりませんか。</div>

            <div class="sub-head">最近の出来事</div>
            <ul v-if="news.length" class="news">
              <li v-for="n in news" :key="n.id">
                <span class="day">{{ fmtDay(n.at) }}</span>
                <span class="kind">{{ n.kind }}</span>
                {{ n.message }}
              </li>
            </ul>
            <div v-else class="empty">まだ何も起きていません。</div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.entrance {
  max-width: 860px;
  margin: 24px auto;
  padding: 0 8px;
}
/* 街の看板。レガシーの色(茶+クリーム)に合わせた立体的な見出し。 */
.signboard {
  background: linear-gradient(#a9884d, #8a6d38);
  border: 1px solid #5f4a20;
  border-radius: 4px;
  text-align: center;
  padding: 14px 10px 12px;
  box-shadow: 0 2px 0 #6b5527;
}
.town-name {
  font-size: 30px;
  font-weight: bold;
  letter-spacing: 6px;
  color: #fff8e6;
  text-shadow: 1px 1px 0 #6b5527, 2px 2px 3px rgba(0, 0, 0, 0.35);
}
.tagline {
  margin-top: 4px;
  font-size: 12px;
  color: #f2e6c8;
  letter-spacing: 2px;
}
.cols {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  margin-top: 12px;
}
.ent-gate {
  flex: 1 1 58%;
  min-width: 0;
}
.ent-now {
  flex: 1 1 42%;
  min-width: 0;
}
.box-head {
  background: #997a44;
  color: #fff;
  font-size: 13px;
  font-weight: bold;
  padding: 4px 10px;
  border: 1px solid #7a5f30;
  border-bottom: 0;
}
.box-body {
  background: #fff;
  border: 1px solid #999;
  padding: 12px 14px;
}
.lead {
  font-size: 13px;
  color: #333;
  line-height: 1.7;
  margin: 0 0 12px;
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.lbl {
  color: #006699;
  font-size: 13px;
}
.row input {
  flex: 1 1 180px;
  font-size: 15px;
  padding: 4px 6px;
}
.note {
  font-size: 11px;
  color: #888;
  line-height: 1.7;
  margin: 12px 0 0;
}
.logged-out {
  background: #f4f8ee;
  border: 1px solid #cfd9bd;
  font-size: 12px;
  line-height: 1.7;
  color: #445;
  padding: 8px 10px;
  margin: 12px 0 0;
}
.message.error {
  margin-top: 12px;
}
.exchanging {
  background: #fff;
  border: 1px solid #999;
  margin-top: 12px;
  font-size: 14px;
  color: #006699;
  padding: 24px 0;
  text-align: center;
}
.stat {
  font-size: 13px;
  color: #333;
  border-bottom: 1px dotted #ccc;
  padding-bottom: 6px;
}
.stat b {
  color: #cc3300;
  font-size: 16px;
  margin: 0 2px;
}
.residents {
  list-style: none;
  margin: 6px 0 0;
  padding: 0;
  font-size: 12px;
  line-height: 1.8;
}
.rname {
  color: #006699;
}
.rjob {
  color: #888;
  font-size: 11px;
}
.sub-head {
  margin-top: 12px;
  font-size: 12px;
  font-weight: bold;
  color: #663300;
  border-bottom: 1px dotted #ccc;
  padding-bottom: 3px;
}
.news {
  list-style: none;
  word-break: break-word;
  margin: 6px 0 0;
  padding: 0;
  font-size: 11px;
  line-height: 1.8;
  color: #333;
  max-height: 190px;
  overflow-y: auto;
}
.news .day {
  color: #999;
  margin-right: 4px;
}
.news .kind {
  background: #f0e6cf;
  border: 1px solid #ddd0ae;
  color: #663300;
  font-size: 10px;
  padding: 0 4px;
  margin-right: 4px;
  border-radius: 2px;
}
.empty {
  font-size: 12px;
  color: #999;
  padding: 8px 0;
}
@media (max-width: 700px) {
  .entrance {
    margin: 12px auto;
  }
  .cols {
    flex-direction: column;
  }
  .ent-gate,
  .ent-now {
    width: 100%;
  }
  .town-name {
    font-size: 24px;
    letter-spacing: 4px;
  }
  .row input {
    flex: 1 1 100%;
  }
}
</style>
