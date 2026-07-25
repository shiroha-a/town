<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player } from '../api';

// MiAuthログイン。自分のMisskeyインスタンスを入力すると、そのインスタンスの
// 承認画面へ飛び、戻ってくるとログインが完了する(アプリの事前登録は不要)。
const emit = defineEmits<{ login: [player: Player] }>();

const STORAGE_KEY = 'town.instance';

const instance = ref('');
const error = ref('');
const busy = ref(false);
// コールバックから戻った直後の引き換え中かどうか。
const exchanging = ref(false);

onMounted(async () => {
  // 前回使ったインスタンスを覚えておく(入力の手間を減らすだけ)。
  instance.value = localStorage.getItem(STORAGE_KEY) ?? '';

  // /auth/callback?session=... で戻ってきたら引き換える。
  const url = new URL(window.location.href);
  const session = url.searchParams.get('session');
  if (!session) return;
  exchanging.value = true;
  try {
    const p = await api.authCallback(session);
    // URLからsessionを消してから通常画面へ(リロードで再送されないように)。
    window.history.replaceState({}, '', '/');
    emit('login', p);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    window.history.replaceState({}, '', '/');
  } finally {
    exchanging.value = false;
  }
});

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
  <div class="login-panel">
    <h2>街に入る</h2>

    <div v-if="exchanging" class="exchanging" data-test="exchanging">ログインしています…</div>

    <template v-else>
      <p class="lead">
        お使いのMisskeyインスタンスを入力してください。<br />
        インスタンスの承認画面が開き、許可すると街に入れます。
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
      <p class="note">
        ※初めての方はこの操作でそのまま登録されます。<br />
        ※アカウント情報の閲覧と、ゲーム内からのフォローの許可をお願いしています。
      </p>
    </template>

    <div v-if="error" class="message error" data-test="login-error">{{ error }}</div>
  </div>
</template>

<style scoped>
.login-panel {
  max-width: 560px;
  margin: 40px auto;
  background: #fff;
  border: 1px solid #999;
  padding: 20px 24px;
}
h2 {
  margin: 0 0 12px;
  color: #663300;
  font-size: 18px;
}
.lead {
  font-size: 13px;
  color: #333;
  line-height: 1.7;
  margin: 0 0 14px;
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
  flex: 1 1 200px;
  font-size: 15px;
  padding: 4px 6px;
}
.note {
  font-size: 11px;
  color: #888;
  line-height: 1.7;
  margin: 12px 0 0;
}
.exchanging {
  font-size: 14px;
  color: #006699;
  padding: 20px 0;
  text-align: center;
}
.message.error {
  margin-top: 12px;
}
@media (max-width: 700px) {
  .login-panel {
    margin: 16px 8px;
    padding: 14px;
  }
  .row input {
    flex: 1 1 100%;
  }
}
</style>
