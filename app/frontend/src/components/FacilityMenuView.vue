<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player, type ShopItem } from '../api';
import { PARAM_COLUMNS } from '../params';
import Toast from './Toast.vue';
import { useToast, buildEffectLines } from '../toast';

// ジム等、メニューを選んで利用する施設の汎用ビュー。
const props = defineProps<{
  player: Player;
  facility: string;
  title: string;
  lead: string;
  useLabel: string;
}>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const menu = ref<ShopItem[]>([]);
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);
const { toast, showToast, closeToast } = useToast();

const intervalLabel = (m: number) => (m > 0 ? `${m}分` : '-');

onMounted(async () => {
  try {
    menu.value = await api.facilityMenu(props.facility);
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
    kind.value = 'error';
  }
});

async function use(item: ShopItem) {
  busy.value = true;
  const before = props.player;
  try {
    const after = await api.facilityUse(props.player.id, props.facility, item.id);
    emit('update', after);
    showToast({
      variant: 'item',
      title: item.name,
      lines: buildEffectLines(before, after),
      icon: 'item',
    });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '利用できませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page fac-menu-page">
    <Toast :toast="toast" @close="closeToast" />
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="fac-header">
      <div class="lead">
        {{ lead }}<br />
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
        ／ 身体パワー：{{ player.status.energy }} / {{ player.status.energy_max }}
      </div>
      <div class="title">{{ title }}</div>
    </div>

    <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

    <div class="panel-white table-scroll">
      <table class="menu-table">
        <thead>
          <tr>
            <th class="l">名前</th>
            <th></th>
            <th>値段</th>
            <th v-for="c in PARAM_COLUMNS" :key="c.key" class="p">{{ c.label }}</th>
            <th>間</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in menu" :key="item.id" :data-test="`menu-${item.id}`">
            <td class="l">{{ item.name }}</td>
            <td class="use"><button class="btn" :disabled="busy" @click="use(item)">{{ useLabel }}</button></td>
            <td class="price">{{ yen(item.price) }}円</td>
            <td
              v-for="c in PARAM_COLUMNS"
              :key="c.key"
              class="p"
              :class="{ up: (item.params[c.key] ?? 0) > 0, down: (item.params[c.key] ?? 0) < 0 }"
            >
              {{ item.params[c.key] ?? 0 }}
            </td>
            <td class="interval">{{ intervalLabel(item.interval_min) }}</td>
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
.fac-menu-page {
  background-color: #ffcc33;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.fac-header {
  display: flex;
  margin-bottom: 8px;
}
.fac-header .lead {
  flex: 1 1 auto;
  background: #fff;
  border: 1px solid #999;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.fac-header .title {
  flex: 0 0 160px;
  background: #333;
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
  background: #ffff99;
  color: #663300;
  padding: 2px 4px;
  border: 1px solid #e0e080;
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
.menu-table td.p.down {
  color: #c00;
  font-weight: bold;
  background: #ffecec;
}
.menu-table td.use {
  width: 56px;
}
</style>
