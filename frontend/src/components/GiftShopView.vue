<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player, type GiftShopState } from '../api';
import Toast from './Toast.vue';
import { useToast } from '../toast';

// ギフト屋(レガシー gifutoya.cgi)。手数料を払って持ち物を贈答用の「ギフト」に変える。
// ギフトは自分では使えず、メールに添付して他の住民に贈る。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const state = ref<GiftShopState | null>(null);
const selectedItem = ref<number | null>(null);
const uses = ref(1);
const busy = ref(false);
const message = ref('');
const { toast, showToast, closeToast } = useToast();

async function load() {
  try {
    state.value = await api.giftShop(props.player.id);
    if (selectedItem.value === null && state.value.convertibles.length) {
      selectedItem.value = state.value.convertibles[0].item_id;
    }
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  }
}
onMounted(load);

async function convert() {
  if (selectedItem.value === null) return;
  busy.value = true;
  const name = state.value?.convertibles.find((c) => c.item_id === selectedItem.value)?.name ?? '';
  try {
    emit('update', await api.giftConvert(props.player.id, selectedItem.value, uses.value));
    showToast({
      variant: 'item',
      title: `${name}をギフトにした`,
      lines: [`手数料${yen(state.value?.fee ?? 0)}円を支払いました。`],
      icon: 'item',
    });
    await load();
  } catch (e) {
    showToast({
      variant: 'error',
      title: 'ギフトにできませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page gift-page">
    <Toast :toast="toast" @close="closeToast" />
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="gift-header">
      <div class="lead">
        ギフト屋です。持ち物を贈り物用の「ギフト」に変えられます（手数料{{ yen(state?.fee ?? 0) }}円）。<br />
        ギフトは自分では使えません。メール画面から他の住民に贈ってください。<br />
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
      </div>
      <div class="title">ギフト屋</div>
    </div>

    <div v-if="message" class="message error">{{ message }}</div>

    <div class="panel-white">
      <h3 class="sec">■ギフトに変える</h3>
      <div v-if="!state?.convertibles.length" class="note warn">変換できる持ち物がありません。</div>
      <div v-else class="row">
        <select v-model="selectedItem" data-test="convert-item">
          <option v-for="c in state.convertibles" :key="c.item_id" :value="c.item_id">
            {{ c.name }}（残り{{ c.uses }}）
          </option>
        </select>
        <label class="uses">個数<input type="number" v-model.number="uses" min="1" data-test="convert-uses" /></label>
        <button class="btn primary" :disabled="busy" data-test="convert" @click="convert">ギフトにする</button>
      </div>
      <p class="note">※個数を残り以上にすると、まとめて全部ギフトになります。</p>

      <h3 class="sec">■持っているギフト</h3>
      <div v-if="!state?.gifts.length" class="note">まだギフトはありません。</div>
      <table v-else class="gift-table" data-test="gift-list">
        <thead>
          <tr><th class="l">品名</th><th>残り</th></tr>
        </thead>
        <tbody>
          <tr v-for="g in state.gifts" :key="g.id">
            <td class="l">{{ g.name }}</td>
            <td>{{ g.uses }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.gift-page {
  background-color: #f6e2e2;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.gift-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.gift-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.gift-header .title {
  flex: 0 0 140px;
  background: #c0392b;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.money {
  color: #cc3300;
  font-weight: bold;
}
.panel-white {
  background: #fff;
  border: 1px solid #999;
  padding: 12px;
}
.sec {
  color: #c0392b;
  font-size: 13px;
  margin: 14px 0 6px;
}
.sec:first-child {
  margin-top: 0;
}
.note {
  font-size: 12px;
  color: #666;
  margin: 4px 0;
}
.note.warn {
  color: #cc3300;
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.uses input {
  width: 60px;
  margin-left: 4px;
}
.gift-table {
  border-collapse: collapse;
  font-size: 12px;
  min-width: 260px;
}
.gift-table th {
  background: #f7dcdc;
  color: #8a2b20;
  padding: 3px 8px;
  border: 1px solid #e6c0c0;
}
.gift-table td {
  padding: 3px 8px;
  border-bottom: 1px solid #eee;
  text-align: center;
}
.gift-table th.l,
.gift-table td.l {
  text-align: left;
}
@media (max-width: 700px) {
  .gift-header .title {
    flex: 0 0 88px;
    font-size: 14px;
  }
}
</style>
