<script setup lang="ts">
defineProps<{ title: string }>();
const emit = defineEmits<{ back: [] }>();
</script>

<template>
  <div class="facility-page ph-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>
    <div class="ph-header">
      <div class="lead">この施設はリライトで順次実装します(レガシー画面をキャプチャ済み)。</div>
      <div class="title">{{ title }}</div>
    </div>
    <div class="panel-white">
      <p class="muted">準備中です。</p>
    </div>
    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.ph-page {
  background-color: #eef1e6;
  padding: 6px;
  min-height: 60vh;
}
.btn.back {
  margin-bottom: 6px;
}
.ph-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #999;
}
.ph-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  color: #333;
}
.ph-header .title {
  flex: 0 0 140px;
  background: #888;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.panel-white {
  background: #fff;
  border: 1px solid #999;
  padding: 12px;
}
</style>
