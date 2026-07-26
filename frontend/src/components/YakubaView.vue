<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue';
import {
  api,
  type Player,
  type PublicSummary,
  type PublicProfile,
  type NewsEntry,
  type RankingKey,
  type RankingResult,
} from '../api';
import { satietyLabel } from '../params';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const message = ref('');

type Tab = 'roster' | 'news' | 'ranking';
const tab = ref<Tab>('roster');
const TABS: { key: Tab; label: string }[] = [
  { key: 'roster', label: '住民名鑑' },
  { key: 'news', label: '街のニュース' },
  { key: 'ranking', label: 'ランキング' },
];

function fail(e: unknown) {
  message.value = e instanceof Error ? e.message : String(e);
}

// 日時表示。ニュースは年をまたぐので月日+時刻まで。
const fmtDate = (iso: string) => {
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getMonth() + 1}/${d.getDate()} ${p(d.getHours())}:${p(d.getMinutes())}`;
};
const fmtDay = (iso: string) => {
  const d = new Date(iso);
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`;
};
// 入居からの経過日数(レガシー役場の「$tatta日経ちました」)。
const daysSince = (iso: string) => Math.floor((Date.now() - new Date(iso).getTime()) / 86400000);

// ── 住民名鑑 ────────────────────────────────────────────
const roster = ref<PublicSummary[]>([]);
const selectedId = ref(props.player.id);
const other = ref<PublicProfile | null>(null);
const sortMode = ref<'id' | 'new'>('id');

const sortedRoster = computed(() =>
  sortMode.value === 'new'
    ? [...roster.value].sort((a, b) => b.created_at.localeCompare(a.created_at))
    : roster.value,
);
const isSelf = computed(() => selectedId.value === props.player.id);
// 表示対象: 自分は player(全項目)、他人は取得した公開プロフィール。
const view = computed<Player | PublicProfile | null>(() => (isSelf.value ? props.player : other.value));
const weightKg = computed(() => (view.value ? (view.value.status.weight_g / 1000).toFixed(1) : '0'));
// 入居日は自分もroster側から引く(playerには含まれないため)。
const joinedAt = computed(() => roster.value.find((m) => m.id === selectedId.value)?.created_at ?? '');

const zunou = [
  { label: '国語', key: 'kokugo' },
  { label: '数学', key: 'suugaku' },
  { label: '理科', key: 'rika' },
  { label: '社会', key: 'syakai' },
  { label: '英語', key: 'eigo' },
  { label: '音楽', key: 'ongaku' },
  { label: '美術', key: 'bijutsu' },
] as const;
const shintai = [
  { label: 'ルックス', key: 'looks' },
  { label: '体力', key: 'tairyoku' },
  { label: '健康', key: 'kenkou' },
  { label: 'スピード', key: 'speed' },
  { label: 'パワー', key: 'power' },
  { label: '腕力', key: 'wanryoku' },
  { label: '脚力', key: 'kyakuryoku' },
] as const;
const others = [
  { label: 'LOVE', key: 'love' },
  { label: '面白さ', key: 'omoshirosa' },
] as const;

// 選択中の住民の出来事(レガシーの「個人イベント」。自分以外も見られる)。
const personalNews = ref<NewsEntry[]>([]);
async function loadPersonalNews(id: number) {
  personalNews.value = [];
  try {
    personalNews.value = await api.playerNews(id);
  } catch (e) {
    fail(e);
  }
}

async function select(id: number) {
  if (id === selectedId.value) return;
  selectedId.value = id;
  other.value = null;
  message.value = '';
  if (id !== props.player.id) {
    try {
      other.value = await api.playerProfile(id);
    } catch (e) {
      fail(e);
    }
  }
  await loadPersonalNews(id);
}

// ── 街のニュース ─────────────────────────────────────────
const news = ref<NewsEntry[]>([]);
const newsLoaded = ref(false);
async function loadNews() {
  try {
    news.value = await api.townNews();
    newsLoaded.value = true;
  } catch (e) {
    fail(e);
  }
}
// 種別ごとの色と記号。レガシー yakuba.cgi の $news_style / $news_kigou を踏襲。
const NEWS_STYLE: Record<string, { color: string; mark: string }> = {
  入居: { color: '#3366cc', mark: '◆' },
  就職: { color: '#009900', mark: '◎' },
  家: { color: '#990000', mark: '●' },
  当選: { color: '#cc6600', mark: '★' },
  転居: { color: '#666666', mark: '▲' },
};
const newsStyle = (n: NewsEntry) => {
  const s = NEWS_STYLE[n.kind];
  if (s) return s;
  // イベントは良悪で色分け(レガシーの地震=悪/運用=良に相当)。
  return n.good === false ? { color: '#990000', mark: '×' } : { color: '#006666', mark: '○' };
};

// ── ランキング ──────────────────────────────────────────
const rankKeys = ref<RankingKey[]>([]);
const rankKey = ref('assets');
const rank = ref<RankingResult | null>(null);
async function loadRanking() {
  try {
    if (!rankKeys.value.length) rankKeys.value = await api.rankingKeys();
    rank.value = await api.ranking(rankKey.value, props.player.id);
  } catch (e) {
    fail(e);
  }
}
const rankValue = (v: number) => `${yen(v)}${rank.value?.unit ?? ''}`;

watch(rankKey, loadRanking);
watch(tab, (t) => {
  message.value = '';
  if (t === 'news' && !newsLoaded.value) loadNews();
  if (t === 'ranking' && !rank.value) loadRanking();
});

onMounted(async () => {
  try {
    roster.value = await api.listPlayers();
  } catch (e) {
    fail(e);
  }
  await loadPersonalNews(selectedId.value);
});
</script>

<template>
  <div class="facility-page profile-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>
    <div class="profile-header">
      <div class="lead">
        役場です。住民名鑑・街のニュース・各種ランキングを見ることができます。<br />
        ●{{ player.display_name }}さんが街に来てから{{ joinedAt ? daysSince(joinedAt) : 0 }}日経ちました。
      </div>
      <div class="title">役　場</div>
    </div>

    <div class="tabs">
      <button
        v-for="t in TABS"
        :key="t.key"
        class="tab"
        :class="{ active: tab === t.key }"
        :data-test="`tab-${t.key}`"
        @click="tab = t.key"
      >
        {{ t.label }}
      </button>
    </div>

    <div v-if="message" class="message error">{{ message }}</div>

    <!-- 住民名鑑 -->
    <div v-if="tab === 'roster'" class="profile-layout">
      <div class="roster">
        <div class="roster-head">
          住民一覧({{ roster.length }}人)
          <select v-model="sortMode" class="rsort" title="並び替え">
            <option value="id">登録順</option>
            <option value="new">新着順</option>
          </select>
        </div>
        <button
          v-for="m in sortedRoster"
          :key="m.id"
          class="roster-item"
          :class="{ active: m.id === selectedId }"
          @click="select(m.id)"
        >
          {{ m.display_name }}<span class="rjob">（{{ m.job }} Lv{{ m.job_level }}）</span>
        </button>
      </div>

      <div class="card" v-if="view">
        <div class="pname">
          ●{{ view.display_name }}
          <span class="self-badge" v-if="isSelf">（あなた）</span>
        </div>
        <table class="pinfo">
          <tbody>
            <tr><th>職業</th><td>{{ view.status.job }}（レベル{{ view.status.job_level }} / 経験値{{ view.status.job_exp }} / 勤務{{ view.status.job_kaisuu }}回）</td></tr>
            <tr v-if="view.status.mastered_jobs.length"><th>マスター職</th><td>{{ view.status.mastered_jobs.join('、') }}</td></tr>
            <tr v-if="joinedAt"><th>入居日</th><td>{{ fmtDay(joinedAt) }}（{{ daysSince(joinedAt) }}日目）</td></tr>
            <tr>
              <th>コンディション</th>
              <td><span :class="{ sick: view.status.disease_name }">{{ view.status.condition }}</span></td>
            </tr>
            <tr><th>身長 / 体重</th><td>{{ view.status.height_cm }}cm / {{ weightKg }}kg</td></tr>
            <tr><th>体型</th><td>{{ view.status.body_type }}（BMI {{ view.status.bmi }}）</td></tr>
            <tr><th>身体パワー</th><td>{{ view.status.energy }} / {{ view.status.energy_max }}</td></tr>
            <tr><th>頭脳パワー</th><td>{{ view.status.nou_energy }} / {{ view.status.nou_energy_max }}</td></tr>
            <tr><th>空腹度</th><td>{{ satietyLabel(view.status.satiety) }}</td></tr>
            <tr v-if="isSelf"><th>持ち金 / 貯金</th><td class="money">{{ yen(player.money) }}円 / {{ yen(player.savings) }}円</td></tr>
          </tbody>
        </table>

        <div class="param-grid">
          <div class="param-col">
            <div class="phead">頭　脳</div>
            <div v-for="p in zunou" :key="p.key" class="prow">
              <span class="plabel">{{ p.label }}</span><span class="pval">{{ view.params[p.key] }}</span>
            </div>
          </div>
          <div class="param-col">
            <div class="phead">身　体</div>
            <div v-for="p in shintai" :key="p.key" class="prow">
              <span class="plabel">{{ p.label }}</span><span class="pval">{{ view.params[p.key] }}</span>
            </div>
          </div>
          <div class="param-col">
            <div class="phead">その他</div>
            <div v-for="p in others" :key="p.key" class="prow">
              <span class="plabel">{{ p.label }}</span><span class="pval">{{ view.params[p.key] }}</span>
            </div>
          </div>
        </div>

        <!-- その住民の出来事(レガシーの個人イベント。公開範囲を全住民に拡張) -->
        <div class="phead sec-head">最近の出来事</div>
        <div class="feed" data-test="personal-news">
          <div v-if="!personalNews.length" class="muted">記録は残っていません。</div>
          <div v-for="n in personalNews" :key="n.id" class="feed-row">
            <span class="fdate">{{ fmtDate(n.at) }}</span>
            <span class="fkind" :style="{ color: newsStyle(n).color }">{{ newsStyle(n).mark }}{{ n.kind }}</span>
            <span class="fbody">{{ n.message }}</span>
          </div>
        </div>
      </div>
      <div class="card" v-else>読み込み中…</div>
    </div>

    <!-- 街のニュース -->
    <div v-else-if="tab === 'news'" class="card wide">
      <div class="phead sec-head">◎最近の街のニュース</div>
      <div class="feed" data-test="town-news">
        <div v-if="!news.length" class="muted">まだニュースはありません。</div>
        <div v-for="n in news" :key="n.id" class="feed-row">
          <span class="fdate">{{ fmtDate(n.at) }}</span>
          <span class="fkind" :style="{ color: newsStyle(n).color }">{{ newsStyle(n).mark }}{{ n.kind }}</span>
          <span class="fbody" :style="{ color: newsStyle(n).color }">{{ n.message }}</span>
        </div>
      </div>
    </div>

    <!-- ランキング -->
    <div v-else class="card wide">
      <div class="rank-head">
        <select v-model="rankKey" data-test="rank-key">
          <option v-for="k in rankKeys" :key="k.key" :value="k.key">{{ k.label }}</option>
        </select>
        <span class="rank-note">上位{{ rank?.entries.length ?? 0 }}人</span>
      </div>
      <div class="table-scroll">
        <table class="rank-table" data-test="rank-table">
          <thead>
            <tr><th>順位</th><th class="l">名　前</th><th class="l job">職　業</th><th class="num">{{ rank?.label ?? '' }}</th></tr>
          </thead>
          <tbody>
            <tr v-if="!rank?.entries.length"><td colspan="4" class="muted">該当者なし。</td></tr>
            <tr v-for="e in rank?.entries ?? []" :key="e.id" :class="{ me: e.id === player.id }">
              <td class="rk">{{ e.rank }}</td>
              <td class="l">{{ e.display_name }}</td>
              <td class="l job">{{ e.job }} Lv{{ e.job_level }}</td>
              <td class="num">{{ rankValue(e.value) }}</td>
            </tr>
            <!-- 圏外の自分(レガシーには無い改善) -->
            <tr v-if="rank?.self" class="me out">
              <td class="rk">{{ rank.self.rank }}</td>
              <td class="l">{{ rank.self.display_name }}</td>
              <td class="l job">{{ rank.self.job }} Lv{{ rank.self.job_level }}</td>
              <td class="num">{{ rankValue(rank.self.value) }}</td>
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
.profile-page {
  background-color: #e8dcc0;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.profile-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.profile-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  color: #333;
  line-height: 1.6;
}
.profile-header .title {
  flex: 0 0 140px;
  background: #997a44;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 8px;
}
.tab {
  border: 1px solid #997a44;
  border-bottom: 0;
  background: #f0e6cf;
  color: #663300;
  font-size: 13px;
  padding: 5px 16px;
  cursor: pointer;
  border-radius: 4px 4px 0 0;
}
.tab.active {
  background: #997a44;
  color: #fff;
  font-weight: bold;
}
.profile-layout {
  display: flex;
  gap: 8px;
  align-items: flex-start;
}
.roster {
  flex: 0 0 180px;
  background: #fff;
  border: 1px solid #999;
  max-height: 70vh;
  overflow-y: auto;
}
.roster-head {
  background: #997a44;
  color: #fff;
  font-size: 12px;
  padding: 4px 8px;
  position: sticky;
  top: 0;
  display: flex;
  align-items: center;
  gap: 4px;
}
.rsort {
  margin-left: auto;
  font-size: 11px;
}
.roster-item {
  display: block;
  width: 100%;
  text-align: left;
  border: 0;
  border-bottom: 1px solid #eee;
  background: #fff;
  padding: 5px 8px;
  font-size: 12px;
  cursor: pointer;
}
.roster-item:hover {
  background: #f5eede;
}
.roster-item.active {
  background: #ffe9b0;
  font-weight: bold;
}
.rjob {
  color: #888;
  font-size: 11px;
}
.card {
  flex: 1 1 auto;
  background: #fff;
  border: 1px solid #999;
  padding: 12px;
  min-width: 0;
}
.card.wide {
  width: 100%;
  box-sizing: border-box;
}
.pname {
  font-size: 15px;
  font-weight: bold;
  color: #663300;
  margin-bottom: 8px;
}
.self-badge {
  color: #cc3300;
  font-size: 12px;
}
.pinfo {
  border-collapse: collapse;
  font-size: 13px;
  margin-bottom: 12px;
  width: 100%;
}
.pinfo th {
  text-align: left;
  background: #f0e6cf;
  color: #663300;
  padding: 3px 8px;
  border: 1px solid #e0d0a8;
  white-space: nowrap;
  width: 120px;
}
.pinfo td {
  padding: 3px 8px;
  border: 1px solid #eee;
}
.pinfo td.money {
  color: #cc3300;
  font-weight: bold;
}
.sick {
  color: #cc0033;
  font-weight: bold;
}
.param-grid {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.param-col {
  flex: 1 1 120px;
  border: 1px solid #ddd;
}
.phead {
  background: #cbb684;
  color: #3a2a10;
  text-align: center;
  font-size: 12px;
  font-weight: bold;
  padding: 2px;
}
.sec-head {
  margin-top: 12px;
  text-align: left;
  padding: 3px 8px;
}
.prow {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  padding: 2px 8px;
  border-bottom: 1px solid #f0f0f0;
}
.plabel {
  color: #555;
}
.pval {
  font-weight: bold;
  color: #333;
}
.muted {
  color: #999;
  font-size: 12px;
  padding: 6px 4px;
}
/* ニュース/出来事フィード */
.feed {
  border: 1px solid #eee;
  border-top: 0;
  max-height: 60vh;
  overflow-y: auto;
}
.feed-row {
  display: flex;
  gap: 8px;
  font-size: 12px;
  line-height: 1.7;
  padding: 3px 8px;
  border-bottom: 1px solid #f4f0e6;
}
.fdate {
  flex: 0 0 auto;
  color: #999;
  white-space: nowrap;
}
.fkind {
  flex: 0 0 auto;
  font-weight: bold;
  white-space: nowrap;
}
.fbody {
  flex: 1 1 auto;
  color: #333;
  word-break: break-word;
}
/* ランキング */
.rank-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.rank-note {
  font-size: 12px;
  color: #888;
}
.table-scroll {
  overflow-x: auto;
}
.rank-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.rank-table th {
  background: #f0e6cf;
  color: #663300;
  padding: 3px 8px;
  border: 1px solid #e0d0a8;
  white-space: nowrap;
}
.rank-table td {
  padding: 3px 8px;
  border-bottom: 1px solid #eee;
  text-align: center;
  white-space: nowrap;
}
.rank-table th.l,
.rank-table td.l {
  text-align: left;
}
.rank-table th.num,
.rank-table td.num {
  text-align: right;
}
.rank-table td.rk {
  color: #997a44;
  font-weight: bold;
}
.rank-table tr.me td {
  background: #ffe9b0;
  font-weight: bold;
}
.rank-table tr.out td {
  border-top: 2px dashed #cbb684;
}
/* モバイル: 名鑑を1カラムにする */
@media (max-width: 700px) {
  .profile-layout {
    flex-direction: column;
  }
  .roster {
    flex: 1 1 auto;
    width: 100%;
    box-sizing: border-box;
    max-height: 30vh;
  }
  .card {
    width: 100%;
    box-sizing: border-box;
  }
  .profile-header .title {
    flex: 0 0 88px;
    font-size: 14px;
  }
  .tab {
    flex: 1 1 0;
    padding: 5px 4px;
    font-size: 12px;
  }
  .feed-row {
    flex-wrap: wrap;
    gap: 4px;
  }
  /* 狭い画面では職業列を落とし、肝心の数値列を画面内に収める */
  .rank-table th.job,
  .rank-table td.job {
    display: none;
  }
}
</style>
