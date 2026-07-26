<script setup lang="ts">
// オン/オフのトグル。チェックボックスの代わりに使う。
//
// 見た目だけを変えたスイッチで、中身は checkbox のまま。キーボード操作
// (Tab移動・スペースで切替)とスクリーンリーダーの読み上げを、ブラウザの
// 標準実装にそのまま任せられるため。
const model = defineModel<boolean>({ required: true });

defineProps<{
  /** スイッチの右に出す説明。 */
  label?: string;
  disabled?: boolean;
}>();
</script>

<template>
  <label class="toggle" :class="{ disabled }">
    <input v-model="model" type="checkbox" :disabled="disabled" />
    <span class="track"><span class="knob"></span></span>
    <span v-if="label" class="toggle-label">{{ label }}</span>
  </label>
</template>

<style scoped>
.toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  line-height: 1.4;
}
.toggle.disabled {
  cursor: default;
  opacity: 0.5;
}
/* 実体のcheckboxは隠すが、フォーカスは受け取れるようにする(消さない)。 */
.toggle input {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
}
.track {
  position: relative;
  flex: 0 0 auto;
  width: 34px;
  height: 18px;
  border-radius: 9px;
  background: #c9c4b4;
  border: 1px solid #a9a48f;
  transition: background 0.15s;
}
.knob {
  position: absolute;
  top: 1px;
  left: 1px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
  transition: transform 0.15s;
}
.toggle input:checked + .track {
  background: #7ba05b;
  border-color: #5f7f45;
}
.toggle input:checked + .track .knob {
  transform: translateX(16px);
}
.toggle input:focus-visible + .track {
  outline: 2px solid #cc6600;
  outline-offset: 1px;
}
.toggle-label {
  font-size: 12px;
  color: #333;
}
</style>
