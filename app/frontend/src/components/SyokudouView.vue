<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player, type ShopItem } from '../api';
import { PARAM_COLUMNS_MAIN, PARAM_COLUMNS_POWER, satietyLabel } from '../params';
import Toast from './Toast.vue';
import { useToast, buildEffectLines } from '../toast';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const menu = ref<ShopItem[]>([]);
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);
const { toast, showToast, closeToast } = useToast();

onMounted(async () => {
  try {
    menu.value = await api.facilityMenu('syokudou');
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
    kind.value = 'error';
  }
});

async function eat(food: ShopItem) {
  busy.value = true;
  const before = props.player;
  try {
    const after = await api.eat(props.player.id, food.id);
    emit('update', after);
    menu.value = await api.facilityMenu('syokudou'); // 食後の在庫数を反映する
    showToast({
      variant: 'item',
      title: `${food.name}を食べた`,
      lines: buildEffectLines(before, after),
      icon: 'item',
    });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '食べられませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page syokudou-page">
    <Toast :toast="toast" @close="closeToast" />
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="syokudou-header">
      <div class="lead">
        セントラル食堂です。メニューは毎日変わります。<br />
        満腹のときは食事できません(お腹が空くと食べられます)。<br />
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
        ／ 空腹度：{{ satietyLabel(player.status.satiety) }}
      </div>
      <div class="title">食　堂</div>
    </div>

    <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

    <div class="panel-white table-scroll">
      <table class="menu-table">
        <thead>
          <tr>
            <th class="l">メニュー</th>
            <th>在庫</th>
            <th></th>
            <th>値段</th>
            <th v-for="c in PARAM_COLUMNS_MAIN" :key="c.key" class="p">{{ c.label }}</th>
            <th>ｶﾛﾘｰ</th>
            <th v-for="c in PARAM_COLUMNS_POWER" :key="c.key" class="p">{{ c.label }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="food in menu" :key="food.id" :data-test="`food-${food.id}`">
            <td class="l">{{ food.name }}</td>
            <td class="stock" :class="{ soldout: food.stock === 0 }">
              {{ food.stock < 0 ? '-' : food.stock === 0 ? '売切' : food.stock }}
            </td>
            <td class="eat">
              <button class="btn" :disabled="busy || food.stock === 0" @click="eat(food)">
                {{ food.stock === 0 ? '売切' : '食べる' }}
              </button>
            </td>
            <td class="price">{{ yen(food.price) }}円</td>
            <td v-for="c in PARAM_COLUMNS_MAIN" :key="c.key" class="p" :class="{ up: (food.params[c.key] ?? 0) > 0 }">
              {{ food.params[c.key] ?? 0 }}
            </td>
            <td class="cal">{{ food.calorie_g > 0 ? food.calorie_g : '-' }}</td>
            <td v-for="c in PARAM_COLUMNS_POWER" :key="c.key" class="p" :class="{ up: (food.params[c.key] ?? 0) > 0 }">
              {{ food.params[c.key] ?? 0 }}
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
.syokudou-page {
  background-color: #ccff66;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.syokudou-header {
  display: flex;
  margin-bottom: 8px;
}
.syokudou-header .lead {
  flex: 1 1 auto;
  background: #fff;
  border: 1px solid #999;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.syokudou-header .title {
  flex: 0 0 130px;
  background: #333;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  letter-spacing: 4px;
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
.menu-table td.eat {
  width: 56px;
}
.menu-table td.stock {
  color: #333;
}
.menu-table td.stock.soldout {
  color: #cc0000;
  font-weight: bold;
}
</style>
