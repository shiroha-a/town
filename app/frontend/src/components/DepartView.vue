<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, type Player, type ShopItem } from '../api';
import { PARAM_COLUMNS, PARAM_COLUMNS_MAIN, PARAM_COLUMNS_POWER } from '../params';
import Toast from './Toast.vue';
import { useToast, buildEffectLines } from '../toast';

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
    items.value = await api.shopItems();
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
    kind.value = 'error';
  }
});

const intervalLabel = (m: number) => (m > 0 ? `${m}分` : '-');

// カテゴリ別にグループ化(レガシーのカテゴリ見出し付き表を再現)
const grouped = computed(() => {
  const g = new Map<string, ShopItem[]>();
  for (const it of items.value) {
    const c = it.category || 'その他';
    if (!g.has(c)) g.set(c, []);
    g.get(c)!.push(it);
  }
  return [...g.entries()];
});

async function buy(it: ShopItem) {
  busy.value = true;
  message.value = '';
  const before = props.player;
  try {
    const after = await api.buy(props.player.id, it.id);
    emit('update', after);
    items.value = await api.shopItems(); // 購入後の在庫数を反映する
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
  <div class="facility-page depart-page">
    <Toast :toast="toast" @close="closeToast" />
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="depart-header">
      <div class="lead">
        デパートです。品揃えは毎日変わります。種類は豊富ですが値段は高めです。<br />
        また一度に持てる所有物の限度は{{ player.item_kind_limit > 0 ? `${player.item_kind_limit}品目` : '無制限' }}です。<br />
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
      </div>
      <div class="title">デパート</div>
    </div>

    <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

    <div class="panel-white">
      <div class="table-scroll">
        <table class="depart-table">
          <thead>
            <tr>
              <th class="l">品名</th>
              <th>在庫</th>
              <th></th>
              <th>価格</th>
              <th>耐久</th>
              <th v-for="c in PARAM_COLUMNS_MAIN" :key="c.key" class="p">{{ c.label }}</th>
              <th>ｶﾛﾘｰ</th>
              <th v-for="c in PARAM_COLUMNS_POWER" :key="c.key" class="p">{{ c.label }}</th>
              <th>間隔</th>
            </tr>
          </thead>
          <template v-for="[cat, list] in grouped" :key="cat">
            <tbody>
              <tr class="cat-row">
                <td :colspan="PARAM_COLUMNS.length + 7">●{{ cat }}</td>
              </tr>
              <tr v-for="it in list" :key="it.id" :data-test="`shop-${it.id}`">
                <td class="l">{{ it.name }}</td>
                <td class="stock" :class="{ soldout: it.stock === 0 }">
                  {{ it.stock < 0 ? '-' : it.stock === 0 ? '売切' : it.stock }}
                </td>
                <td class="buy">
                  <button class="btn" :disabled="busy || it.stock === 0" @click="buy(it)">
                    {{ it.stock === 0 ? '売切' : '買う' }}
                  </button>
                </td>
                <td class="price">{{ yen(it.price) }}円</td>
                <td class="dura">{{ it.durability }}{{ it.durability_unit === 'day' ? '日' : '回' }}</td>
                <td v-for="c in PARAM_COLUMNS_MAIN" :key="c.key" class="p" :class="{ up: (it.params[c.key] ?? 0) > 0 }">
                  {{ it.params[c.key] ?? 0 }}
                </td>
                <td class="cal">{{ it.calorie_g > 0 ? it.calorie_g : '-' }}</td>
                <td v-for="c in PARAM_COLUMNS_POWER" :key="c.key" class="p" :class="{ up: (it.params[c.key] ?? 0) > 0 }">
                  {{ it.params[c.key] ?? 0 }}
                </td>
                <td class="interval">{{ intervalLabel(it.interval_min) }}</td>
              </tr>
            </tbody>
          </template>
        </table>
      </div>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.depart-page {
  background-color: #ffffcc;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.depart-header {
  display: flex;
  align-items: stretch;
  margin-bottom: 8px;
}
.depart-header .lead {
  flex: 1 1 auto;
  background: #fff;
  border: 1px solid #999;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.depart-header .title {
  flex: 0 0 130px;
  background: #339933;
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
.depart-table {
  border-collapse: collapse;
  font-size: 11px;
  white-space: nowrap;
}
.depart-table th {
  background: #ffcc66;
  color: #663300;
  padding: 2px 4px;
  border: 1px solid #e0c080;
  position: sticky;
  top: 0;
}
.depart-table th.l,
.depart-table td.l {
  text-align: left;
}
.depart-table td {
  padding: 2px 4px;
  border: 1px solid #eee;
  text-align: center;
  background: #fffef0;
}
.depart-table th.p,
.depart-table td.p {
  width: 20px;
  color: #999;
}
.depart-table td.p.up {
  color: #060;
  font-weight: bold;
  background: #eaffea;
}
.depart-table td.price {
  color: #cc3300;
  font-weight: bold;
  text-align: right;
}
.depart-table tr.cat-row td {
  background: #ccff99;
  color: #060;
  font-weight: bold;
  text-align: left;
  border-top: 2px solid #339933;
}
.depart-table td.buy {
  width: 44px;
}
.depart-table td.stock {
  color: #333;
}
.depart-table td.stock.soldout {
  color: #cc0000;
  font-weight: bold;
}
</style>
