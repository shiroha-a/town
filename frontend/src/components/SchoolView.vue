<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player, type ShopItem } from '../api';
import Toast from './Toast.vue';
import { useToast, buildEffectLines } from '../toast';

// 学校: 頭脳科目を1日1回だけ大きく伸ばす施設。頭脳パワー(nou_energy)と現金を消費する。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');

// 学校で扱う頭脳7科目。
const SUBJECTS: { key: string; label: string }[] = [
  { key: 'kokugo', label: '国' },
  { key: 'suugaku', label: '数' },
  { key: 'rika', label: '理' },
  { key: 'syakai', label: '社' },
  { key: 'eigo', label: '英' },
  { key: 'ongaku', label: '音' },
  { key: 'bijutsu', label: '美' },
];

const menu = ref<ShopItem[]>([]);
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);
const { toast, showToast, closeToast } = useToast();

// 頭脳消費は effect の nou_energy 負値。ジムの身体消費と同じくマイナス表記で見せる。
const brainCost = (item: ShopItem) => Math.min(0, item.params['nou_energy'] ?? 0);

onMounted(async () => {
  try {
    menu.value = await api.facilityMenu('school');
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
    kind.value = 'error';
  }
});

async function attend(item: ShopItem) {
  busy.value = true;
  const before = props.player;
  try {
    const after = await api.schoolAttend(props.player.id, item.id);
    emit('update', after);
    showToast({
      variant: 'item',
      title: `${item.name}を受講した`,
      lines: buildEffectLines(before, after),
      icon: 'item',
    });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '受講できませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page school-page">
    <Toast :toast="toast" @close="closeToast" />
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="fac-header">
      <div class="lead">
        今日も頑張って勉強しましょう。受講できるのは1日1回です。<br />
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
        ／ 頭脳パワー：{{ player.status.nou_energy }} / {{ player.status.nou_energy_max }}
      </div>
      <div class="title">学校</div>
    </div>

    <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

    <div class="panel-white table-scroll">
      <table class="menu-table">
        <thead>
          <tr>
            <th class="l">講座名</th>
            <th></th>
            <th v-for="s in SUBJECTS" :key="s.key" class="p">{{ s.label }}</th>
            <th>金額</th>
            <th>頭</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in menu" :key="item.id" :data-test="`course-${item.id}`">
            <td class="l">{{ item.name }}</td>
            <td class="use"><button class="btn" :disabled="busy" @click="attend(item)">受講する</button></td>
            <td v-for="s in SUBJECTS" :key="s.key" class="p" :class="{ up: (item.params[s.key] ?? 0) > 0 }">
              {{ item.params[s.key] ?? 0 }}
            </td>
            <td class="price">{{ yen(item.price) }}円</td>
            <td class="cost" :class="{ down: brainCost(item) < 0 }">{{ brainCost(item) }}</td>
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
.school-page {
  background-color: #cfe3cf;
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
  background: #336633;
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
  background: #ddeedd;
  color: #336633;
  padding: 2px 6px;
  border: 1px solid #bcd0bc;
}
.menu-table td {
  padding: 2px 6px;
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
.menu-table td.cost {
  color: #06c;
}
.menu-table td.cost.down {
  color: #c00;
  font-weight: bold;
  background: #ffecec;
}
.menu-table td.use {
  width: 56px;
}
</style>
