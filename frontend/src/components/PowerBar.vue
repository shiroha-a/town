<script setup lang="ts">
import { computed } from 'vue';

// トップ画面と温泉の入浴画面で共有する身体/頭脳パワーの表示。
// 値・最大値・満タンまでの残り時間ラベルを受け取り、残量バーを描画する。
const props = defineProps<{
  label: string;
  value: number;
  max: number;
  fullRemain: string | null;
}>();

// パワーバーは残量%(0-100)。色は旧town_maker準拠: >59%青 / >19%黄 / それ以下赤。
const pct = computed(() => {
  if (props.max <= 0) return 0;
  return Math.min(100, Math.max(0, Math.round((props.value / props.max) * 100)));
});
const color = computed(() => {
  if (pct.value > 59) return 'blue';
  if (pct.value > 19) return 'yellow';
  return 'red';
});
</script>

<template>
  <div class="honbun2">
    <span class="honbun2">{{ label }}</span>：{{ value }} （MAX値：{{ max }}）
    <span v-if="fullRemain" class="recover-timer">満タンまで{{ fullRemain }}</span><br />
    <span class="powerbar">
      <span class="bar-fill" :class="color" :style="{ width: pct + '%' }"></span>
    </span>
  </div>
</template>
