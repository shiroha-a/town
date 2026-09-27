<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue';
import { FEEDBACK_REACTIONS, type FeedbackReaction } from '../api';
import RichText from './RichText.vue';
import EmojiPicker from './EmojiPicker.vue';

// 目安箱の投稿・コメントの下に出すリアクションの列。付いているものを押すと
// 付け外し、「＋」から定番の絵文字かカスタム絵文字を選んで足す。
defineProps<{ reactions: FeedbackReaction[]; disabled: boolean }>();
const emit = defineEmits<{ toggle: [reaction: string] }>();

const menuOpen = ref(false);
const pickerOpen = ref(false);
const addWrap = ref<HTMLElement | null>(null);

// 定番のメニューは、外側を押したら閉じる。開いている間だけ文書全体を見張る。
// 「＋」自身はメニューと同じ枠の中なので、押し直しは開閉の切り替えとして効く。
function onOutside(e: PointerEvent) {
  if (addWrap.value && !addWrap.value.contains(e.target as Node)) menuOpen.value = false;
}
watch(menuOpen, (open) => {
  if (open) document.addEventListener('pointerdown', onOutside);
  else document.removeEventListener('pointerdown', onOutside);
});
onUnmounted(() => document.removeEventListener('pointerdown', onOutside));

function choose(reaction: string) {
  menuOpen.value = false;
  pickerOpen.value = false;
  emit('toggle', reaction);
}
function openPicker() {
  menuOpen.value = false;
  pickerOpen.value = true;
}
</script>

<template>
  <div class="rb">
    <button
      v-for="r in reactions"
      :key="r.reaction"
      class="rb-chip"
      :class="{ mine: r.reacted }"
      :disabled="disabled"
      :title="r.reacted ? 'もう一度押すと取り消します' : '同じリアクションを付ける'"
      data-test="reaction"
      @click="emit('toggle', r.reaction)"
    >
      <RichText :text="r.reaction" /><span class="rb-count">{{ r.count }}</span>
    </button>
    <span ref="addWrap" class="rb-add-wrap">
      <button
        class="rb-add"
        :disabled="disabled"
        title="リアクションを付ける"
        data-test="reaction-add"
        @click="menuOpen = !menuOpen"
      >
        ＋
      </button>
      <span v-if="menuOpen" class="rb-menu">
        <button
          v-for="e in FEEDBACK_REACTIONS"
          :key="e"
          class="rb-pick"
          :data-test="`reaction-pick-${e}`"
          @click="choose(e)"
        >
          {{ e }}
        </button>
        <button class="rb-custom" @click="openPicker">カスタム絵文字…</button>
      </span>
    </span>
    <EmojiPicker v-if="pickerOpen" @pick="choose" @close="pickerOpen = false" />
  </div>
</template>

<style scoped>
.rb {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  margin-top: 6px;
}
.rb-chip {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 1px 7px;
  border: 1px solid #ddd;
  border-radius: 12px;
  background: #f6f6f6;
  font-size: 13px;
  line-height: 1.6;
  cursor: pointer;
}
/* 自分が付けているものは色を変え、押すと外れることが分かるようにする。 */
.rb-chip.mine {
  background: #e8f1ff;
  border-color: #2f6fc4;
}
.rb-count {
  font-size: 12px;
  color: #555;
}
.rb-chip.mine .rb-count {
  color: #2f6fc4;
  font-weight: bold;
}
.rb-add-wrap {
  position: relative;
}
.rb-add {
  padding: 1px 8px;
  border: 1px dashed #bbb;
  border-radius: 12px;
  background: #fff;
  color: #777;
  font-size: 13px;
  line-height: 1.6;
  cursor: pointer;
}
.rb-menu {
  position: absolute;
  left: 0;
  top: calc(100% + 4px);
  z-index: 20;
  display: flex;
  flex-wrap: wrap;
  gap: 2px;
  width: 188px;
  padding: 6px;
  background: #fff;
  border: 1px solid #ccc;
  border-radius: 6px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
}
.rb-pick {
  width: 40px;
  height: 32px;
  border: none;
  background: none;
  font-size: 18px;
  cursor: pointer;
  border-radius: 4px;
}
.rb-pick:hover {
  background: #f0f0f0;
}
.rb-custom {
  width: 100%;
  margin-top: 2px;
  padding: 3px;
  border: 1px solid #ddd;
  border-radius: 4px;
  background: #fafafa;
  font-size: 12px;
  cursor: pointer;
}
</style>
