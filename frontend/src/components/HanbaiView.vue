<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player, type ShopItem } from '../api';
import { PARAM_COLUMNS } from '../params';
import Toast from './Toast.vue';
import { useToast, buildEffectLines } from '../toast';

// 自動販売機: 日用品を毎日ランダム3品陳列する(レガシー hanbai1.cgi)。家システムに非依存。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const items = ref<ShopItem[]>([]);
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);
const { toast, showToast, closeToast } = useToast();

onMounted(async () => {
  try {
    items.value = await api.facilityMenu('hanbai');
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
    kind.value = 'error';
  }
});

async function buy(it: ShopItem) {
  busy.value = true;
  const before = props.player;
  try {
    const after = await api.buy(props.player.id, it.id, 'hanbai');
    emit('update', after);
    items.value = await api.facilityMenu('hanbai'); // 購入後の在庫を反映
    showToast({
      variant: 'item',
      title: `${it.name}を購入した`,
      lines: buildEffectLines(before, after),
      icon: 'item',
    });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '購入できませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page hanbai-page">
    <Toast :toast="toast" @close="closeToast" />
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="hanbai-header">
      <div class="lead">
        自動販売機です。日用品を売っています。品揃えは毎日変わります。<br />
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
      </div>
      <div class="title">自動販売機</div>
    </div>

    <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

    <div class="panel-white table-scroll">
      <table class="menu-table">
        <thead>
          <tr>
            <th class="l">品名</th>
            <th>価格</th>
            <th v-for="c in PARAM_COLUMNS" :key="c.key" class="p">{{ c.label }}</th>
            <th>在庫</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="it in items" :key="it.id" :data-test="`hanbai-${it.id}`">
            <td class="l">{{ it.name }}</td>
            <td class="price">{{ yen(it.price) }}円</td>
            <td v-for="c in PARAM_COLUMNS" :key="c.key" class="p" :class="{ up: (it.params[c.key] ?? 0) > 0 }">
              {{ it.params[c.key] ?? 0 }}
            </td>
            <td class="stock" :class="{ soldout: it.stock === 0 }">
              {{ it.stock < 0 ? '-' : it.stock === 0 ? '売切' : it.stock }}
            </td>
            <td class="buy">
              <button class="btn" :disabled="busy || it.stock === 0" @click="buy(it)">
                {{ it.stock === 0 ? '売切' : '買う' }}
              </button>
            </td>
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
.hanbai-page {
  background-color: #cce0ff;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.hanbai-header {
  display: flex;
  margin-bottom: 8px;
}
.hanbai-header .lead {
  flex: 1 1 auto;
  background: #fff;
  border: 1px solid #999;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.hanbai-header .title {
  flex: 0 0 130px;
  background: #336699;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid #999;
}
.money {
  color: #cc3300;
  font-weight: bold;
}
.panel-white {
  background: #fff;
  border: 1px solid #999;
  padding: 8px;
}
.table-scroll {
  overflow-x: auto;
}
.menu-table {
  border-collapse: collapse;
  font-size: 11px;
  white-space: nowrap;
}
.menu-table th {
  background: #cfe0f5;
  color: #234;
  padding: 2px 4px;
  border: 1px solid #a8c4e0;
}
.menu-table td {
  padding: 2px 4px;
  border: 1px solid #eee;
  text-align: center;
}
.menu-table th.l,
.menu-table td.l {
  text-align: left;
}
.menu-table td.price {
  color: #cc3300;
  font-weight: bold;
  text-align: right;
}
.menu-table th.p,
.menu-table td.p {
  width: 20px;
  color: #999;
}
.menu-table td.p.up {
  color: #060;
  font-weight: bold;
  background: #eaffea;
}
.menu-table td.stock {
  color: #333;
}
.menu-table td.stock.soldout {
  color: #cc0000;
  font-weight: bold;
}
.menu-table td.buy {
  width: 44px;
}
</style>
