<script setup lang="ts">
import type { BuildingExterior } from '../api';

// 家の外装を画像一覧から選ぶピッカー(建築・建て替え共用)。
// プルダウンでは画像が選ぶまで分からないため、画像を並べてクリック選択する。
// 2マス(2x1)の外装は横長のカードで並べ、建てられないときは理由を出して選ばせない。
const props = defineProps<{
  exteriors: BuildingExterior[];
  modelValue: string;
  /** 選べない外装の理由を返す(空文字なら選べる)。省略時はすべて選べる。 */
  disabledReason?: (e: BuildingExterior) => string;
}>();
const emit = defineEmits<{ 'update:modelValue': [key: string] }>();

const reasonFor = (e: BuildingExterior) => props.disabledReason?.(e) ?? '';
const spanOf = (e: BuildingExterior) => (e.span > 1 ? e.span : 1);

function pick(e: BuildingExterior) {
  if (reasonFor(e)) return;
  emit('update:modelValue', e.key);
}
</script>

<template>
  <div class="ext-grid">
    <button
      v-for="e in exteriors"
      :key="e.key"
      type="button"
      class="ext-card"
      :class="{ selected: e.key === modelValue, locked: !!reasonFor(e), wide: spanOf(e) > 1 }"
      :disabled="!!reasonFor(e)"
      :title="reasonFor(e) ? `${e.key}（${e.price}万）／${reasonFor(e)}` : `${e.key}（${e.price}万）`"
      @click="pick(e)"
    >
      <img :src="`/img/svg/${e.key}.svg`" :alt="e.key" />
      <span class="ext-price">{{ e.price }}万</span>
      <span v-if="spanOf(e) > 1" class="ext-badge">{{ reasonFor(e) ? '許可証' : '2マス' }}</span>
    </button>
  </div>
</template>

<style scoped>
.ext-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: flex-start;
}
.ext-card {
  position: relative;
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
.ext-card:hover:not(:disabled) {
  border-color: #8ab;
  background: #f0f6fb;
}
.ext-card.selected {
  border-color: #cc7a00;
  background: #fff3df;
  font-weight: bold;
}
/* 2マスの外装。カード幅を倍にして、実際の大きさが分かるようにする。 */
.ext-card.wide {
  width: 92px;
}
.ext-card.wide img {
  width: 64px;
}
/* 建築許可証を持っていない外装。存在は見せて目標にしてもらう。 */
.ext-card.locked {
  cursor: not-allowed;
  opacity: 0.55;
  filter: grayscale(0.7);
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
.ext-badge {
  position: absolute;
  top: -6px;
  right: -4px;
  padding: 0 4px;
  border-radius: 7px;
  background: #cc7a00;
  color: #fff;
  font-size: 9px;
  line-height: 14px;
  white-space: nowrap;
}
.ext-card.locked .ext-badge {
  background: #888;
}
</style>
