<script setup lang="ts">
import { computed, ref } from 'vue';
import { api, type Player } from '../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);

// 治療費は表示用。実際の徴収額はサーバが病名から権威的に決める。
const FEES: Record<string, number> = {
  風邪ぎみ: 18000,
  風邪: 28000,
  下痢: 32000,
  肺炎: 35000,
  結核: 48000,
  脳腫瘍: 64000,
  癌: 88000,
};
const HEALTHY_FEE = 10000;

const diseaseName = computed(() => props.player.status.disease_name);
const isSick = computed(() => diseaseName.value !== '');
const fee = computed(() => (isSick.value ? (FEES[diseaseName.value] ?? HEALTHY_FEE) : HEALTHY_FEE));

async function treat() {
  busy.value = true;
  message.value = '';
  try {
    emit('update', await api.hospitalTreat(props.player.id));
    message.value = isSick.value ? '治療しました。お大事に。' : '元気注射を打ちました。';
    kind.value = 'ok';
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
    kind.value = 'error';
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page hospital-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="hospital-header">
      <div class="lead">
        中央病院です。病気は早めに治療しましょう。<br />
        健康なときは「元気注射」で予防できます(病気指数がリセットされます)。<br />
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
      </div>
      <div class="title">病　院</div>
    </div>

    <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

    <div class="panel-white">
      <table class="diag-table">
        <tbody>
          <tr>
            <th>コンディション</th>
            <td :class="{ sick: isSick }" data-test="condition">{{ player.status.condition }}</td>
          </tr>
          <tr>
            <th>診断</th>
            <td data-test="diagnosis">
              <template v-if="isSick">
                <span class="sick">{{ diseaseName }}</span>にかかっています。
              </template>
              <template v-else>健康です。</template>
            </td>
          </tr>
          <tr>
            <th>病気指数</th>
            <td>{{ player.status.disease_index }}</td>
          </tr>
          <tr>
            <th>{{ isSick ? '治療費' : '元気注射' }}</th>
            <td class="fee">{{ yen(fee) }}円</td>
          </tr>
        </tbody>
      </table>

      <div class="treat-area">
        <button class="btn treat" :disabled="busy" data-test="treat" @click="treat">
          {{ isSick ? `治療する（${yen(fee)}円）` : `元気注射を打つ（${yen(fee)}円）` }}
        </button>
      </div>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.hospital-page {
  background-color: #e6f2ff;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.hospital-header {
  display: flex;
  margin-bottom: 8px;
}
.hospital-header .lead {
  flex: 1 1 auto;
  background: #fff;
  border: 1px solid #99b;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.hospital-header .title {
  flex: 0 0 130px;
  background: #336699;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  letter-spacing: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid #99b;
}
.money {
  color: #cc3300;
  font-weight: bold;
}
.panel-white {
  background: #fff;
  border: 1px solid #99b;
  padding: 12px;
}
.diag-table {
  border-collapse: collapse;
  font-size: 13px;
  margin: 0 auto 12px;
}
.diag-table th {
  background: #dbe8f5;
  color: #234;
  padding: 4px 10px;
  border: 1px solid #b8c8dc;
  text-align: right;
  white-space: nowrap;
}
.diag-table td {
  padding: 4px 12px;
  border: 1px solid #dde;
  text-align: left;
}
.diag-table td.sick,
.sick {
  color: #cc0033;
  font-weight: bold;
}
.diag-table td.fee {
  color: #cc3300;
  font-weight: bold;
}
.treat-area {
  text-align: center;
}
.btn.treat {
  font-size: 14px;
  padding: 6px 18px;
}
.btn:disabled {
  background: #ccc;
  color: #888;
  border-color: #bbb;
  cursor: not-allowed;
  opacity: 0.7;
}
</style>
