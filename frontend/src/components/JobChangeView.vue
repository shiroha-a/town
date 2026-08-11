<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, type Player, type JobOption } from '../api';
import { PARAM_COLUMNS } from '../params';
import { notifyOk, notifyError } from '../toast';

// 身体/頭脳パワーは「必要値」ではなく「1回働くと消費する量」として専用列に出すため、
// 必要パラメータ列からは除外する。
const REQ_COLUMNS = PARAM_COLUMNS.filter((c) => c.key !== 'energy' && c.key !== 'nou_energy');

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const jobs = ref<JobOption[]>([]);
const busy = ref(false);
const yen = (n: number) => n.toLocaleString('ja-JP');

onMounted(async () => {
  try {
    jobs.value = await api.jobs();
  } catch (e) {
    notifyError('求人を読み込めませんでした', e, 'go_work');
  }
});

// 前提マスター職を満たしているか(なければ常にtrue)。
function masterOk(job: JobOption): boolean {
  return (
    job.require_master === '' || props.player.status.mastered_jobs.includes(job.require_master)
  );
}

// 前提職をまだマスターしておらず就けない職業は、一覧から除外する。
const visibleJobs = computed(() => jobs.value.filter(masterOk));

// 体格の条件。パラメータのように列を作ると横に伸びるので、職業名の下に文で出す。
// 満たしていないものは色を変えて、なぜ就けないのかが分かるようにする。
interface Limit {
  text: string;
  met: boolean;
}
function limits(job: JobOption): Limit[] {
  const st = props.player.status;
  const out: Limit[] = [];
  if (job.bmi_min > 0 && job.bmi_max > 0) {
    out.push({
      text: `BMI ${job.bmi_min}〜${job.bmi_max}`,
      met: st.bmi >= job.bmi_min && st.bmi <= job.bmi_max,
    });
  } else if (job.bmi_min > 0) {
    out.push({ text: `BMI ${job.bmi_min}以上`, met: st.bmi >= job.bmi_min });
  } else if (job.bmi_max > 0) {
    out.push({ text: `BMI ${job.bmi_max}以下`, met: st.bmi <= job.bmi_max });
  }
  if (job.height_min > 0) {
    out.push({ text: `身長${job.height_min}cm以上`, met: st.height_cm >= job.height_min });
  }
  return out;
}

// プレイヤーの現在値を取得(学力・能力はplayer.paramsに入っている)。
const playerParam = (key: string): number =>
  (props.player.params as unknown as Record<string, number>)[key] ?? 0;
// 必要値があり、現在値がそれに満たない(=不足)か。
function lacking(job: JobOption, key: string): boolean {
  const need = job.requirements[key] ?? 0;
  return need > 0 && playerParam(key) < need;
}
// 必要値があり、それを満たしているか。
function meets(job: JobOption, key: string): boolean {
  const need = job.requirements[key] ?? 0;
  return need > 0 && playerParam(key) >= need;
}

async function take(job: JobOption) {
  busy.value = true;
  try {
    emit('update', await api.changeJob(props.player.id, job.name));
    notifyOk(`${job.name}に転職しました`, ['街の仕事ボタンが使えるようになりました。'], 'go_work');
  } catch (e) {
    notifyError('転職できませんでした', e, 'go_work');
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page job-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>
    <div class="job-header">
      <div class="lead">
        必要な条件を満たしていればその職業に就くことができます。<br />
        なりたい職業めざして勉強・トレーニングに励みましょう。<br />
        ※転職をすると経験値・出勤回数は0に戻ります。<br />
        現在の職業：<b>{{ player.status.job }}</b>
      </div>
      <div class="title">職業安定所</div>
    </div>

    <div class="panel-white">
      <div class="cap">
        必要パラメータ(不足は赤・達成は緑で表示)／ 職業名の横は体格の条件(満たしていないものは赤)／
        身P・頭P消費(1回働くと消費するパワー)／ ボーナス(レベルアップ時に給料の何倍が出るか)／
        給料の下は昇給(レベル1ごとに基本給が何%増えるか)
      </div>
      <div class="table-scroll sticky-table">
        <table class="job-table">
          <thead>
            <tr>
              <th class="l">職業</th>
              <th v-for="c in REQ_COLUMNS" :key="c.key" class="p">{{ c.label }}</th>
              <th class="cost">身P<br />消費</th>
              <th class="cost">頭P<br />消費</th>
              <th>給料</th>
              <th class="bonus">ボー<br />ナス</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="job in visibleJobs" :key="job.id" :data-test="`job-${job.id}`">
              <td class="l">
                {{ job.name }}
                <span v-if="job.require_master" class="req-master" :class="{ met: masterOk(job) }">
                  （要「{{ job.require_master }}」マスター）
                </span>
                <span
                  v-for="(lim, i) in limits(job)"
                  :key="i"
                  class="limit"
                  :class="{ ng: !lim.met }"
                >
                  {{ lim.text }}
                </span>
              </td>
              <td
                v-for="c in REQ_COLUMNS"
                :key="c.key"
                class="p"
                :class="{ lack: lacking(job, c.key), met: meets(job, c.key) }"
              >
                {{ job.requirements[c.key] ?? 0 }}
              </td>
              <td class="cost">{{ job.energy_cost }}</td>
              <td class="cost">{{ job.nou_energy_cost }}</td>
              <td class="right money">
                {{ yen(job.pay) }}円
                <span v-if="job.pay_interval > 1" class="interval"
                  >{{ job.pay_interval }}回ごと支給</span
                >
                <span v-if="job.raise_rate > 0" class="interval">昇給{{ job.raise_rate }}%/Lv</span>
              </td>
              <td class="bonus">
                <span v-if="job.bonus_rate > 0">×{{ job.bonus_rate }}</span>
              </td>
              <td class="right">
                <button
                  class="btn"
                  :disabled="busy || player.status.job === job.name || !masterOk(job)"
                  @click="take(job)"
                >
                  {{ player.status.job === job.name ? '就業中' : '就く' }}
                </button>
              </td>
            </tr>
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
.job-page {
  background-color: #669966;
  /* 旧shop_bak.gifのCSS再現: 6px周期の1pxライン */
  background-image: repeating-linear-gradient(
    180deg,
    transparent 0 2px,
    #cccccc 2px 3px,
    transparent 3px 6px
  );
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.job-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.job-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  color: #333;
  line-height: 1.6;
}
.job-header .title {
  flex: 0 0 140px;
  background: #336633;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.panel-white {
  background: #fff;
  border: 1px solid #333;
  padding: 8px;
}
.cap {
  font-size: 11px;
  color: #cc0000;
  margin-bottom: 4px;
}
.table-scroll {
  --stick-line: #b0d0b0;
  --stick-bg: #fff;
  overflow-x: auto;
}
.job-table {
  border-collapse: collapse;
  font-size: 11px;
  white-space: nowrap;
}
.job-table th {
  background: #cfe8cf;
  color: #063;
  padding: 2px 4px;
  border: 1px solid #b0d0b0;
}
.job-table td {
  padding: 2px 4px;
  border: 1px solid #eee;
  text-align: center;
}
.job-table th.l,
.job-table td.l {
  text-align: left;
}
.job-table td.right {
  text-align: right;
}
.job-table th.p,
.job-table td.p {
  width: 20px;
  color: #bbb;
}
.job-table td.p.lack {
  color: #cc0000;
  font-weight: bold;
  background: #ffd5d5;
}
.job-table td.p.met {
  color: #067a06;
  font-weight: bold;
  background: #e6f5e6;
}
.job-table th.cost {
  background: #ffe0cc;
  color: #cc5500;
  font-size: 10px;
  line-height: 1.1;
}
.job-table td.cost {
  color: #cc5500;
  font-weight: bold;
  background: #fff4ec;
}
.money {
  color: #cc3300;
  font-weight: bold;
  font-size: 12px;
}
/* ボーナスは職業ごとの差が大きく比べたい値なので、給料の隣に列として出す。 */
.job-table th.bonus {
  background: #ffeccc;
  color: #a05a00;
  font-size: 10px;
  line-height: 1.1;
}
.job-table td.bonus {
  color: #cc6600;
  font-weight: bold;
  background: #fffaf2;
}
/* 給料の下に出す補足(支給間隔・昇給)。表の本文と同じ大きさにする。
   9pxだと読めなかった。 */
.interval {
  display: block;
  font-size: 11px;
  line-height: 1.5;
  color: #555;
  font-weight: normal;
  white-space: nowrap;
}
/* 体格の条件。満たしていれば控えめに、足りなければ赤で目立たせる。 */
.limit {
  display: inline-block;
  margin-left: 6px;
  padding: 0 5px;
  border-radius: 8px;
  background: #eef1e8;
  color: #556;
  font-size: 11px;
  white-space: nowrap;
}
.limit.ng {
  background: #ffe8e8;
  color: #c33;
  font-weight: bold;
}
.req-master {
  font-size: 10px;
  color: #cc0000;
}
.req-master.met {
  color: #067a06;
}
</style>
