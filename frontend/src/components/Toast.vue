<script setup lang="ts">
import CommandIcon from './CommandIcon.vue';
import type { ToastData } from '../toast';

// 画面上部トーストの表示コンポーネント。中身はtoast.tsが1つだけ持ち、
// これを描画するのはApp.vueだけ(画面ごとには置かない)。
// スタイル(.toast等)はstyle.cssにグローバル定義されている。
defineProps<{ toast: ToastData | null }>();
const emit = defineEmits<{ close: [] }>();
</script>

<template>
  <transition name="wt">
    <div
      v-if="toast"
      class="toast"
      :class="toast.variant"
      role="status"
      data-test="toast"
      @click="emit('close')"
    >
      <span class="toast-icon"><CommandIcon :name="toast.icon" /></span>
      <div class="toast-body">
        <div class="toast-title">{{ toast.title }}</div>
        <div v-for="(l, i) in toast.lines" :key="i" class="toast-line">{{ l }}</div>
      </div>
    </div>
  </transition>
</template>
