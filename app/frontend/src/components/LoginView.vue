<script setup lang="ts">
import { ref } from 'vue';
import { api, type Player } from '../api';

const emit = defineEmits<{ login: [player: Player] }>();

// 新規登録フォーム
const instanceHost = ref('misskey.example');
const remoteUserId = ref('');
const displayName = ref('');

// 既存プレイヤーをIDで再開(開発用)
const playerId = ref<number | null>(null);

const error = ref('');
const busy = ref(false);

async function register() {
  if (!remoteUserId.value) {
    error.value = 'ユーザーIDを入力してください。';
    return;
  }
  error.value = '';
  busy.value = true;
  try {
    const p = await api.register(instanceHost.value, remoteUserId.value, displayName.value);
    emit('login', p);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}

async function enterExisting() {
  if (!playerId.value) {
    error.value = 'プレイヤーIDを入力してください。';
    return;
  }
  error.value = '';
  busy.value = true;
  try {
    const p = await api.getPlayer(playerId.value);
    emit('login', p);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="login-panel">
    <h2>街に入る（新規登録・再開）</h2>
    <table class="grid">
      <tbody>
        <tr>
          <td>インスタンス</td>
          <td><input type="text" v-model="instanceHost" data-test="instance-host" /></td>
        </tr>
        <tr>
          <td>ユーザーID</td>
          <td><input type="text" v-model="remoteUserId" data-test="remote-user-id" /></td>
        </tr>
        <tr>
          <td>表示名</td>
          <td><input type="text" v-model="displayName" data-test="display-name" /></td>
        </tr>
      </tbody>
    </table>
    <div class="actions" style="margin-top: 8px">
      <button class="btn" :disabled="busy" data-test="register" @click="register">街へ入る</button>
    </div>
    <p class="muted">
      初回はそのまま新規登録されます。登録済みの方は、登録時と同じインスタンスとユーザーIDを入れると
      同じプレイヤーで再開できます（表示名は初回登録時のみ反映されます）。
    </p>
  </div>

  <div class="login-panel">
    <h2>プレイヤーIDで再開(開発用)</h2>
    <div class="actions">
      <input type="number" v-model.number="playerId" placeholder="プレイヤーID" data-test="player-id" />
      <button class="btn" :disabled="busy" @click="enterExisting">再開</button>
    </div>
    <p class="muted">MiAuth導入時にこのdevログインは本認証へ置き換えます。</p>
  </div>

  <div v-if="error" class="message error" data-test="error">{{ error }}</div>
</template>
