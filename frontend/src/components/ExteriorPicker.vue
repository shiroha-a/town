<script setup lang="ts">
import type { BuildingExterior } from '../api';

// 家の外装を画像一覧から選ぶピッカー(建築・建て替え共用)。
// プルダウンでは画像が選ぶまで分からないため、画像を並べてクリック選択する。
defineProps<{ exteriors: BuildingExterior[]; modelValue: string }>();
const emit = defineEmits<{ 'update:modelValue': [key: string] }>();
</script>

<template>
  <div class="ext-grid">
    <button
      v-for="e in exteriors"
      :key="e.key"
      type="button"
      class="ext-card"
      :class="{ selected: e.key === modelValue }"
      :title="`${e.key}（${e.price}万）`"
      @click="emit('update:modelValue', e.key)"
    >
      <img :src="`/img/svg/${e.key}.svg`" :alt="e.key" />
      <span class="ext-price">{{ e.price }}万</span>
    </button>
  </div>
</template>

<style scoped>
.ext-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.ext-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  width: 56px;
  padding: 4px 2px;
  border: 2px solid #ccc;
  border-radius: 4px;
  background: #fff;
  cursor: pointer;
  font-size: 10px;
  color: #555;
}
.ext-card:hover {
  border-color: #8ab;
  background: #f0f6fb;
}
.ext-card.selected {
  border-color: #cc7a00;
  background: #fff3df;
  font-weight: bold;
}
.ext-card img {
  width: 32px;
  height: 32px;
  object-fit: contain;
  image-rendering: pixelated;
}
.ext-price {
  white-space: nowrap;
}
</style>
