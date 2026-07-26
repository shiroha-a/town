<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api, type Player, type AttendanceBoard } from '../api';

// 足あと(出席簿): 縦=住人、横=日付のマトリクス。来た日は足跡、来なかった日は×。
defineProps<{ player: Player }>();
const emit = defineEmits<{ back: [] }>();

const board = ref<AttendanceBoard | null>(null);
const message = ref('');

const cellMark = (c: string) => (c === 'present' ? '👣' : c === 'absent' ? '×' : '');

onMounted(async () => {
  try {
    board.value = await api.attendanceBoard();
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  }
});
</script>

<template>
  <div class="facility-page ashi-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="fac-header">
      <div class="lead">
        住人の来訪を日毎に記録した出席簿です。街を見るだけでその日の足跡が付きます。<br />
        👣=来た日 / ×=来なかった日
      </div>
      <div class="title">足あと帳</div>
    </div>

    <div v-if="message" class="message error">{{ message }}</div>

    <div v-if="board" class="ashi-body">
      <div class="panel-white table-scroll">
        <table class="ashi-table">
          <thead>
            <tr>
              <th class="name-col">住人</th>
              <th v-for="(d, i) in board.dates" :key="i">{{ d }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in board.members" :key="m.id" :class="{ me: m.id === player.id }">
              <td class="name-col">{{ m.name }}</td>
              <td v-for="(c, i) in m.cells" :key="i" :class="['cell', c]">{{ cellMark(c) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="panel-white rank">
        <h3>皆勤賞ランキング</h3>
        <table class="rank-table">
          <thead><tr><th>#</th><th class="l">住人</th><th>出席率</th><th>出席/日数</th></tr></thead>
          <tbody>
            <tr v-for="(r, i) in board.ranking" :key="i" :class="{ me: r.name === player.display_name }">
              <td>{{ i + 1 }}</td>
              <td class="l">{{ r.name }}</td>
              <td class="rate">{{ r.rate }}%</td>
              <td>{{ r.present }}/{{ r.days }}</td>
            </tr>
            <tr v-if="!board.ranking.length"><td colspan="4" class="muted">まだランキング対象者がいません。</td></tr>
          </tbody>
        </table>
      </div>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.ashi-page {
  background-color: #e0ecd8;
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
  border: 1px solid #9b9;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.fac-header .title {
  flex: 0 0 160px;
  background: #558855;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid #9b9;
}
.panel-white {
  background: #fff;
  border: 1px solid #9b9;
  padding: 8px;
  margin-bottom: 8px;
}
.panel-white h3 {
  margin: 0 0 6px;
  font-size: 14px;
  color: #336633;
}
.table-scroll {
  overflow-x: auto;
}
.ashi-table {
  border-collapse: collapse;
  font-size: 12px;
  white-space: nowrap;
}
.ashi-table th,
.ashi-table td {
  border: 1px solid #dde;
  padding: 2px 4px;
  text-align: center;
}
.ashi-table th {
  background: #eef4e8;
  color: #336633;
  font-size: 10px;
}
.ashi-table .name-col {
  text-align: left;
  position: sticky;
  left: 0;
  background: #fff;
  font-weight: bold;
}
.ashi-table th.name-col {
  background: #eef4e8;
}
.ashi-table tr.me .name-col {
  background: #fff3e8;
}
.cell.present {
  background: #eaffea;
}
.cell.absent {
  color: #c99;
}
.rank-table {
  border-collapse: collapse;
  width: 100%;
  font-size: 12px;
}
.rank-table th,
.rank-table td {
  border: 1px solid #eee;
  padding: 3px 6px;
  text-align: center;
}
.rank-table th {
  background: #eef4e8;
  color: #336633;
}
.rank-table td.l,
.rank-table th.l {
  text-align: left;
}
.rank-table td.rate {
  color: #336633;
  font-weight: bold;
}
.rank-table tr.me {
  background: #fff3e8;
}
.muted {
  color: #999;
}
</style>
