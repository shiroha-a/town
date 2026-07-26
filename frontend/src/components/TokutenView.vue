<script setup lang="ts">
import { ref } from 'vue';
import { api, type Player } from '../api';
import Toast from './Toast.vue';
import { useToast, buildEffectLines } from '../toast';

// 特典(レガシー tokuten.cgi)。管理者が発行したシリアルコードを入力すると、
// 設定された景品(アイテム/パラメータ/お金など)を1人1回だけ受け取れる。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const code = ref('');
const busy = ref(false);
const { toast, showToast, closeToast } = useToast();

async function redeem() {
  if (!code.value.trim()) return;
  busy.value = true;
  const before = props.player;
  try {
    const res = await api.redeemSerial(props.player.id, code.value);
    emit('update', res.player);
    const lines = buildEffectLines(before, res.player).filter((l) => l !== '変化なし');
    if (res.result.item_name) {
      lines.unshift(`${res.result.item_name} を${res.result.item_uses}個 受け取りました`);
    }
    if (res.result.message) lines.unshift(res.result.message);
    showToast({
      variant: 'event-good',
      title: '特典を受け取りました',
      lines: lines.length ? lines : ['受け取りました。'],
      icon: 'item',
    });
    code.value = '';
  } catch (e) {
    showToast({
      variant: 'error',
      title: '受け取れませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page tokuten-page">
    <Toast :toast="toast" @close="closeToast" />
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="tokuten-header">
      <div class="lead">
        特典交換所です。配布されたシリアルコードを入力すると景品を受け取れます。<br />
        1つのコードにつき1回だけ受け取れます。
      </div>
      <div class="title">特　典</div>
    </div>

    <div class="panel-white">
      <div class="row">
        <span class="lbl">◆シリアルコード</span>
        <input
          v-model="code"
          type="text"
          class="code"
          placeholder="XXXX-XXXX-XXXX"
          data-test="serial-code"
          @keydown.enter="redeem"
        />
        <button
          class="btn primary"
          :disabled="busy || !code.trim()"
          data-test="serial-redeem"
          @click="redeem"
        >
          受け取る
        </button>
      </div>
      <p class="note">※大文字・小文字とハイフンは区別しません。</p>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.tokuten-page {
  background-color: #efe6f5;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.tokuten-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.tokuten-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.tokuten-header .title {
  flex: 0 0 140px;
  background: #6b4a9e;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  letter-spacing: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.panel-white {
  background: #fff;
  border: 1px solid #999;
  padding: 12px;
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.lbl {
  color: #6b4a9e;
  font-size: 12px;
}
.code {
  width: 220px;
  font-family: ui-monospace, monospace;
  font-size: 14px;
  letter-spacing: 1px;
  text-transform: uppercase;
}
.note {
  font-size: 11px;
  color: #888;
  margin: 6px 0 0;
}
@media (max-width: 700px) {
  .tokuten-header .title {
    flex: 0 0 88px;
    font-size: 14px;
    letter-spacing: 2px;
  }
  .code {
    width: 100%;
  }
}
</style>
