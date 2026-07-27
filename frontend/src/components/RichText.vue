<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { tokenize, loadEmojiDict, emojiDict } from '../emoji';

// 投稿本文の表示。URLはリンクに、:name@host: はカスタム絵文字の画像にする。
// 本文に生HTMLは入っていないので、ここでの変換がすべて(レガシーとの最大の違い)。
const props = defineProps<{
  text: string;
  /** プロフィールのように、その文章専用の shortcode->url 辞書を持つ場合に渡す。 */
  emojis?: Record<string, string>;
}>();

// emojiDict を参照して、辞書が後から読み込まれたら再計算されるようにする。
const tokens = computed(() => {
  void emojiDict.value;
  return tokenize(props.text, props.emojis);
});

onMounted(() => {
  void loadEmojiDict();
});
</script>

<template>
  <span class="rich"
    ><template v-for="(t, i) in tokens" :key="i"
      ><a v-if="t.kind === 'link'" :href="t.v" target="_blank" rel="noopener noreferrer">{{
        t.v
      }}</a
      ><img
        v-else-if="t.kind === 'emoji'"
        class="custom-emoji"
        :src="t.url"
        :alt="t.v"
        :title="t.license ? `${t.v}\nライセンス: ${t.license}` : t.v"
        loading="lazy"
      /><template v-else>{{ t.v }}</template></template
    ></span
  >
</template>

<style scoped>
.rich {
  white-space: pre-wrap;
  word-break: break-word;
}
.custom-emoji {
  height: 1.6em;
  width: auto;
  vertical-align: -0.4em;
  object-fit: contain;
}
</style>
