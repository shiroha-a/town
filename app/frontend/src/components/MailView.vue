<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player, type MailMessage, type PublicSummary, type Gift } from '../api';

// メール: 住人あての1対1メッセージ。受信箱・送信箱を1画面にまとめて表示する。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ back: [] }>();

const received = ref<MailMessage[]>([]);
const sent = ref<MailMessage[]>([]);
const players = ref<PublicSummary[]>([]);
const recipientId = ref<number | ''>('');
// 贈り物の添付(ギフト屋で作ったギフトを1個送る)。
const gifts = ref<Gift[]>([]);
const giftId = ref<number | ''>('');
const body = ref('');
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);

const fmtDate = (iso: string) => new Date(iso).toLocaleString('ja-JP', { hour12: false });

async function load() {
  try {
    const mb = await api.getMail(props.player.id);
    received.value = mb.received;
    sent.value = mb.sent;
    gifts.value = (await api.giftShop(props.player.id)).gifts;
  } catch (e) {
    fail(e);
  }
}
onMounted(async () => {
  try {
    players.value = (await api.listPlayers()).filter((p) => p.id !== props.player.id);
  } catch {
    players.value = [];
  }
  await load();
});

function fail(e: unknown) {
  message.value = e instanceof Error ? e.message : String(e);
  kind.value = 'error';
}

async function send() {
  if (recipientId.value === '' || !body.value.trim()) {
    message.value = '宛先とメッセージを入力してください。';
    kind.value = 'error';
    return;
  }
  busy.value = true;
  message.value = '';
  try {
    await api.mailSend(props.player.id, recipientId.value, body.value, giftId.value === '' ? 0 : giftId.value);
    giftId.value = '';
    message.value = 'メッセージを送信しました。';
    kind.value = 'ok';
    body.value = '';
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function toggleSave(m: MailMessage) {
  busy.value = true;
  try {
    await api.mailSave(props.player.id, m.id, !m.saved);
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function del(m: MailMessage) {
  if (!window.confirm('このメッセージを削除しますか?')) return;
  busy.value = true;
  try {
    await api.mailDelete(props.player.id, m.id);
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page mail-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="fac-header">
      <div class="lead">
        住人あてにメッセージを送れます。保存できるのは受信箱・送信箱あわせて50件までです。<br />
        1日に送信できるのは30通までです。
      </div>
      <div class="title">メール</div>
    </div>

    <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

    <!-- 送信フォーム -->
    <div class="panel-white send-form">
      <h3>メッセージを送る</h3>
      <label class="row">
        <span class="lbl">宛先</span>
        <select v-model="recipientId">
          <option value="">選んでください</option>
          <option v-for="p in players" :key="p.id" :value="p.id">{{ p.display_name }}</option>
        </select>
      </label>
      <label class="row">
        <span class="lbl">本文</span>
        <textarea v-model="body" rows="3" placeholder="メッセージを入力"></textarea>
      </label>
      <label class="row" v-if="gifts.length">
        <span class="lbl">贈り物</span>
        <select v-model="giftId" data-test="gift-select">
          <option value="">添付しない</option>
          <option v-for="g in gifts" :key="g.id" :value="g.id">{{ g.name }}（残り{{ g.uses }}）</option>
        </select>
      </label>
      <div class="actions">
        <button class="btn primary" :disabled="busy" @click="send">送信</button>
      </div>
    </div>

    <div class="boxes">
      <!-- 受信箱 -->
      <div class="panel-white box">
        <h3>受信箱（{{ received.length }}）</h3>
        <div v-if="!received.length" class="muted">まだ受信したメッセージはありません。</div>
        <div v-for="m in received" :key="m.id" class="mail" :class="{ unread: m.unread }" :data-test="`recv-${m.id}`">
          <div class="mail-head">
            <span class="from">{{ m.counterpart_name }}さんより</span>
            <span class="date">{{ fmtDate(m.sent_at) }}</span>
            <span v-if="m.unread" class="badge">新着</span>
            <span v-if="m.saved" class="badge saved">保存</span>
          </div>
          <div class="mail-body">{{ m.body }}</div>
          <div v-if="m.gift_item_name" class="gift-note">🎁 {{ m.gift_item_name }} が贈られてきました。</div>
          <div class="mail-act">
            <button class="btn mini" :disabled="busy" @click="toggleSave(m)">{{ m.saved ? '保存解除' : '保存する' }}</button>
            <button class="btn mini danger" :disabled="busy" @click="del(m)">削除する</button>
          </div>
        </div>
      </div>

      <!-- 送信箱 -->
      <div class="panel-white box">
        <h3>送信箱（{{ sent.length }}）</h3>
        <div v-if="!sent.length" class="muted">まだ送信したメッセージはありません。</div>
        <div v-for="m in sent" :key="m.id" class="mail" :data-test="`sent-${m.id}`">
          <div class="mail-head">
            <span class="from">{{ m.counterpart_name }}さんへ</span>
            <span class="date">{{ fmtDate(m.sent_at) }}</span>
            <span v-if="m.saved" class="badge saved">保存</span>
          </div>
          <div class="mail-body">{{ m.body }}</div>
          <div v-if="m.gift_item_name" class="gift-note">🎁 {{ m.gift_item_name }} を贈りました。</div>
          <div class="mail-act">
            <button class="btn mini" :disabled="busy" @click="toggleSave(m)">{{ m.saved ? '保存解除' : '保存する' }}</button>
            <button class="btn mini danger" :disabled="busy" @click="del(m)">削除する</button>
          </div>
        </div>
      </div>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.mail-page {
  background-color: #e8e0f0;
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
  border: 1px solid #99a;
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
  border: 1px solid #99a;
}
.panel-white {
  background: #fff;
  border: 1px solid #99a;
  padding: 10px;
  margin-bottom: 8px;
}
.panel-white h3 {
  margin: 0 0 8px;
  font-size: 14px;
  color: #442266;
}
.send-form .row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 8px;
}
.send-form .lbl {
  flex: 0 0 40px;
  font-size: 12px;
  color: #445;
  padding-top: 4px;
}
.send-form select,
.send-form textarea {
  flex: 1 1 auto;
  box-sizing: border-box;
}
.boxes {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  flex-wrap: wrap;
}
.box {
  flex: 1 1 300px;
  min-width: 280px;
}
.muted {
  color: #999;
  font-size: 12px;
}
.mail {
  border: 1px solid #ddd;
  padding: 6px 8px;
  margin-bottom: 6px;
  font-size: 12px;
}
.mail.unread {
  background: #fff6e8;
  border-color: #f0c890;
}
.mail-head {
  display: flex;
  align-items: center;
  gap: 6px;
  color: #667;
  margin-bottom: 3px;
}
.mail-head .from {
  font-weight: bold;
  color: #442266;
}
.mail-head .date {
  font-size: 10px;
  color: #99a;
}
.badge {
  font-size: 10px;
  background: #cc3300;
  color: #fff;
  padding: 0 4px;
  border-radius: 2px;
}
.badge.saved {
  background: #669933;
}
.mail-body {
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.5;
  color: #333;
}
.mail-act {
  margin-top: 4px;
  display: flex;
  gap: 4px;
}
.btn.mini {
  padding: 1px 6px;
  font-size: 11px;
}
.btn.danger {
  background: #cc3333;
  color: #fff;
  border-color: #992222;
}
.btn.primary {
  background: #663399;
  color: #fff;
  border-color: #442266;
}
.gift-note {
  margin-top: 4px;
  font-size: 12px;
  color: #b36b00;
  background: #fff6e0;
  border-radius: 4px;
  padding: 2px 6px;
}
</style>
