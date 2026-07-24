<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, assetUrl, type Player, type BuildingState, type TownFacility, type TownAsset } from '../api';
import Toast from './Toast.vue';
import ExteriorPicker from './ExteriorPicker.vue';
import { useToast } from '../toast';

// 建設会社(建築系フェーズ2a): 5つの街の空地に家を建てる。建築費は普通口座から
// 引き落とす。1軒目は(地価+外装)×内装倍率、2軒目以降は地価+外装×2。1人4軒まで。
// initialTargetは街マップの空き地クリックから渡される建築マス(隠し町も可)。
const props = defineProps<{
  player: Player;
  initialTarget?: { town: number; row: number; col: number } | null;
}>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const state = ref<BuildingState | null>(null);
const facilities = ref<TownFacility[]>([]); // 全街の施設(選択中の街ぶんを描画)
const assets = ref<TownAsset[]>([]); // 背景アセット(装飾レイヤー)
const message = ref('');
const busy = ref(false);
const { toast, showToast, closeToast } = useToast();

const selectedTown = ref(0);
const selectedCell = ref<{ row: number; col: number } | null>(null);
const selectedExterior = ref('');
const selectedInterior = ref(3); // 既定はD(最安)

// 建築対象の街: 隠し町はタブから選べない。ただし空き地クリック(initialTarget)で
// 開いた隠し町はそのまま表示・建築できる(今いる街に限りサーバーが許可する)。
const buildableTowns = computed(() => state.value?.towns.filter((t) => !t.hidden) ?? []);
// タブ表示用: 通常街+初期ターゲットで開いた隠し町(選択中のみ)。
const tabTowns = computed(() => {
  const towns = [...buildableTowns.value];
  const sel = state.value?.towns.find((t) => t.no === selectedTown.value);
  if (sel && sel.hidden) towns.push(sel);
  return towns;
});

async function refresh() {
  state.value = await api.building(props.player.id);
  if (!selectedExterior.value && state.value.exteriors.length > 0) {
    selectedExterior.value = state.value.exteriors[0].key;
  }
  // 選択中の街が隠し町(または消滅)なら先頭の通常街へ戻す。
  // 空き地クリックで開いた隠し町(initialTarget)は維持する。
  if (
    !buildableTowns.value.some((t) => t.no === selectedTown.value) &&
    selectedTown.value !== props.initialTarget?.town
  ) {
    selectedTown.value = buildableTowns.value[0]?.no ?? 0;
    selectedCell.value = null;
  }
}

onMounted(async () => {
  try {
    const [f] = await Promise.all([api.townMap(), refresh()]);
    facilities.value = f;
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  }
  // 街マップの空き地クリックから来た場合はそのマスを選択済みにする。
  const target = props.initialTarget;
  if (target && state.value?.towns.some((t) => t.no === target.town)) {
    selectedTown.value = target.town;
    selectedCell.value = { row: target.row, col: target.col };
  }
  // 背景アセット(装飾レイヤー)。取れなくてもグリッドは描画する。
  try {
    assets.value = await api.townAssets();
  } catch {
    assets.value = [];
  }
});

// グリッド範囲(row 0..rows-1 = A..L, col 1..cols)。
const rowRange = computed(() => Array.from({ length: state.value?.rows ?? 0 }, (_, i) => i));
const colRange = computed(() => Array.from({ length: state.value?.cols ?? 0 }, (_, i) => i + 1));
const gridStyle = computed(() => ({
  gridTemplateColumns: `repeat(${state.value?.cols ?? 0}, 30px)`,
}));
const rowLabel = (row: number) => String.fromCharCode(65 + row);

const isFirstHouse = computed(() => (state.value?.house_count ?? 0) === 0);
const townName = (no: number) => state.value?.towns.find((t) => t.no === no)?.name ?? '';
const townPlotCount = computed(
  () => state.value?.plots.filter((p) => p.town === selectedTown.value).length ?? 0,
);

// 選択中の街の施設セル(施設はマルチ街化済み)。空き地(akichi)は建築マスとして
// plotAtで別扱いするので、施設アイコンからは除外する。
function facilityAt(row: number, col: number): TownFacility | undefined {
  return facilities.value.find(
    (f) => f.key !== 'akichi' && f.town === selectedTown.value && f.row === row && f.col === col,
  );
}
function houseAt(row: number, col: number) {
  return state.value?.houses.find(
    (h) => h.town === selectedTown.value && h.row === row && h.col === col,
  );
}
// 管理者が空地に指定したマスか。
function plotAt(row: number, col: number): boolean {
  return (
    state.value?.plots.some(
      (p) => p.town === selectedTown.value && p.row === row && p.col === col,
    ) ?? false
  );
}
function cellClass(row: number, col: number) {
  const sel = selectedCell.value;
  const fac = !!facilityAt(row, col);
  const hou = !!houseAt(row, col);
  return {
    facility: fac,
    house: hou,
    own: houseAt(row, col)?.own ?? false,
    selected: !!sel && sel.row === row && sel.col === col,
    empty: plotAt(row, col) && !fac && !hou, // 建築可能な空地
  };
}
function cellImg(row: number, col: number): string | null {
  const f = facilityAt(row, col);
  if (f) return `/img/svg/${f.img}.svg`;
  const h = houseAt(row, col);
  if (h) return `/img/svg/${h.exterior}.svg`;
  // 空地は街マップと同じ空き地アイコンで示す。
  if (plotAt(row, col)) return '/img/svg/akiti.svg';
  return null;
}
// セルの背景アセット(選択中の街)。施設・家・空き地アイコンの下に敷く。
function assetImgAt(row: number, col: number): string | null {
  const a = assets.value.find(
    (x) => x.town === selectedTown.value && x.row === row && x.col === col,
  );
  return a ? assetUrl(a.img) : null;
}
function cellTitle(row: number, col: number): string {
  const f = facilityAt(row, col);
  if (f) return f.alt;
  const h = houseAt(row, col);
  if (h) return h.setumei ? `${h.owner_name}さんの家\n「${h.setumei}」` : `${h.owner_name}さんの家`;
  if (plotAt(row, col)) return `${rowLabel(row)}${col}（空地）`;
  return `${rowLabel(row)}${col}`;
}
function clickCell(row: number, col: number) {
  // 建築画面のグリッドは建てる場所を選ぶためのもの。家はクリックしても
  // 何もしない(訪問は街マップの家クリックから)。tooltipで家主名だけ分かる。
  if (houseAt(row, col)) return;
  // 空地に指定されたマス(施設・家なし)だけ建築選択できる。
  if (!plotAt(row, col) || facilityAt(row, col)) return;
  selectedCell.value = { row, col };
}
function selectTown(no: number) {
  selectedTown.value = no;
  selectedCell.value = null;
}

// 追加種別(2軒目以降)。同種は1軒まで、株式会社/持ち物販売店は能力審査が必要。
const selectedTuika = ref(0);
const ownedTuikas = computed(() => new Set((state.value?.my_houses ?? []).map((h) => h.tuika).filter((t) => t !== 0)));
function tuikaDisabled(t: { no: number; shinsa: boolean }): string {
  if (t.no !== 0 && ownedTuikas.value.has(t.no)) return 'すでに所有';
  if (t.shinsa && !state.value?.shinsa_ok) return '設営能力不足';
  return '';
}

// 建築費プレビュー(building.BuildCostと同じ式。単位:円)。
const cost = computed(() => {
  const s = state.value;
  if (!s) return 0;
  const town = s.towns.find((t) => t.no === selectedTown.value);
  const ext = s.exteriors.find((e) => e.key === selectedExterior.value);
  if (!town || !ext) return 0;
  let man = 0;
  if (isFirstHouse.value) {
    const inte = s.interiors.find((i) => i.rank === selectedInterior.value);
    if (!inte) return 0;
    man = (town.land_price + ext.price) * inte.multiplier;
  } else {
    const tk = s.tuikas.find((t) => t.no === selectedTuika.value);
    man = town.land_price + ext.price * 2 + (tk?.fee ?? 0);
  }
  return man * 10000;
});

async function build() {
  if (!selectedCell.value || !state.value) return;
  busy.value = true;
  const c = cost.value;
  try {
    const after = await api.buildHouse(
      props.player.id,
      selectedTown.value,
      selectedCell.value.row,
      selectedCell.value.col,
      selectedExterior.value,
      isFirstHouse.value ? selectedInterior.value : 0,
      isFirstHouse.value ? 0 : selectedTuika.value,
    );
    emit('update', after);
    await refresh();
    selectedCell.value = null;
    showToast({
      variant: 'item',
      title: '家を建てた',
      lines: [`建築費 ${yen(c)}円を普通口座から支払いました`],
      icon: 'item',
    });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '建てられませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}

</script>

<template>
  <div class="kentiku-page facility-page">
    <Toast :toast="toast" @close="closeToast" />
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="kentiku-header">
      <div class="lead">
        建設会社です。街の空地に家を建てられます。<br />
        1軒目は「（地価＋外装）×内装ランク倍率」、2軒目以降は「地価＋外装×2」の建築費が<b>普通口座</b>から引き落とされます（1人{{ state?.mochiie_max ?? 4 }}軒まで）。
      </div>
      <div class="title">建設会社</div>
    </div>

    <div v-if="message" class="message error" data-test="message">{{ message }}</div>

    <template v-if="state">
      <!-- 街タブ(隠し町は空き地クリックで開いた場合のみ表示) -->
      <div class="town-tabs">
        <button
          v-for="t in tabTowns"
          :key="t.no"
          class="tab"
          :class="{ active: selectedTown === t.no }"
          @click="selectTown(t.no)"
        >
          {{ t.name }}<span class="tika">地価{{ t.land_price }}万</span>
        </button>
      </div>

      <!-- 街グリッド(空地クリックで選択) -->
      <div class="grid-scroll">
        <div class="grid" :style="gridStyle">
          <template v-for="row in rowRange" :key="row">
            <div
              v-for="col in colRange"
              :key="`${row}-${col}`"
              class="cell"
              :class="cellClass(row, col)"
              :title="cellTitle(row, col)"
              @click="clickCell(row, col)"
            >
              <img v-if="assetImgAt(row, col)" class="cell-bg" :src="assetImgAt(row, col)!" alt="" />
              <img v-if="cellImg(row, col)" class="cell-fg" :src="cellImg(row, col)!" :alt="cellTitle(row, col)" />
            </div>
          </template>
        </div>
      </div>

      <!-- 建築フォーム -->
      <div v-if="selectedCell" class="build-form panel-white">
        <div class="row">
          <span class="lbl">建築位置</span>
          <span class="val">{{ townName(selectedTown) }}／{{ rowLabel(selectedCell.row) }}{{ selectedCell.col }}</span>
        </div>
        <div class="row ext-row">
          <span class="lbl">外装</span>
          <ExteriorPicker v-model="selectedExterior" :exteriors="state.exteriors" />
        </div>
        <div v-if="isFirstHouse" class="row">
          <span class="lbl">内装</span>
          <select v-model.number="selectedInterior" class="sel">
            <option v-for="i in state.interiors" :key="i.rank" :value="i.rank">
              {{ i.name }}（費用×{{ i.multiplier }}・枠{{ i.slots }}）
            </option>
          </select>
        </div>
        <div v-else class="row ext-row">
          <span class="lbl">種別</span>
          <div class="tuika-list">
            <label v-for="t in state.tuikas" :key="t.no" class="tuika-opt" :class="{ off: !!tuikaDisabled(t) }">
              <input v-model.number="selectedTuika" type="radio" :value="t.no" :disabled="!!tuikaDisabled(t)" />
              {{ t.name }}<template v-if="t.fee > 0">（+{{ t.fee }}万）</template>
              <span v-if="tuikaDisabled(t)" class="tuika-ng">{{ tuikaDisabled(t) }}</span>
            </label>
            <div class="note tuika-note">
              2軒目以降は内装を選べません。株式会社・持ち物販売店には能力審査（総資産1億円＋全パラメータ1万以上）があります。
            </div>
          </div>
        </div>
        <div class="row cost-row">
          <span class="lbl">建築費</span>
          <span class="cost">{{ yen(cost) }}円</span>
        </div>
        <div class="row">
          <button class="btn build-btn" :disabled="busy" @click="build">この場所に建てる</button>
        </div>
      </div>
      <div v-else class="hint">
        <template v-if="townPlotCount === 0">
          この街にはまだ空地が設定されていません（管理者が空地を設定すると建てられます）。
        </template>
        <template v-else>グリッドの空地（緑）をクリックして建築する場所を選んでください。</template>
      </div>

      <!-- 自分の家一覧(読み取り専用。設定はコマンドバーの「家の設定」から) -->
      <div class="my-houses panel-white">
        <div class="mh-head">自分の家（{{ state.house_count }}／{{ state.mochiie_max }}軒）</div>
        <div v-if="state.my_houses.length === 0" class="mh-empty">まだ家を持っていません。</div>
        <ul v-else class="mh-list">
          <li v-for="h in state.my_houses" :key="h.id" class="mh-item">
            <div class="mh-row">
              <img :src="`/img/svg/${h.exterior}.svg`" :alt="h.exterior" />
              <span class="mh-loc">{{ townName(h.town) }}／{{ rowLabel(h.row) }}{{ h.col }}</span>
              <span class="mh-ext">{{ h.exterior }}・内装{{ ['A','B','C','D'][h.interior_rank] ?? '?' }}ランク</span>
            </div>
          </li>
        </ul>
        <div class="mh-note">コメント・コンテンツ・店・建て替え・売却は、街のコマンドバー「家の設定」から行えます。</div>
      </div>
    </template>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.kentiku-page {
  background-color: #d8e8c8;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.kentiku-header {
  display: flex;
  margin-bottom: 8px;
}
.kentiku-header .lead {
  flex: 1 1 auto;
  background: #fff;
  border: 1px solid #999;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.kentiku-header .title {
  flex: 0 0 130px;
  background: #4a7a2a;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid #999;
}
.panel-white {
  background: #fff;
  border: 1px solid #999;
  padding: 8px;
  margin-top: 8px;
}
.town-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 6px;
}
.town-tabs .tab {
  background: #eef3e8;
  border: 1px solid #99a;
  padding: 4px 8px;
  font-size: 12px;
  cursor: pointer;
  color: #234;
}
.town-tabs .tab.active {
  background: #4a7a2a;
  color: #fff;
  font-weight: bold;
}
.town-tabs .tab .tika {
  margin-left: 4px;
  font-size: 10px;
  opacity: 0.8;
}
.grid-scroll {
  overflow-x: auto;
  background: #fff;
  border: 1px solid #999;
  padding: 6px;
  width: max-content;
  max-width: 100%;
  box-sizing: border-box;
}
.grid {
  display: grid;
  gap: 1px;
  background: #cfe0c0;
  width: max-content;
}
.cell {
  width: 30px;
  height: 30px;
  background: #e6e6e6;
  border: 1px solid #cfcfcf;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  position: relative;
}
/* 背景アセット(装飾レイヤー): セルいっぱいに敷き、施設・空き地アイコンの下に置く。 */
.cell .cell-bg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  pointer-events: none;
}
.cell .cell-fg {
  position: relative;
  z-index: 1;
}
.cell.empty {
  background: #d6f0c0;
  border-color: #a8d488;
  cursor: pointer;
}
.cell.empty:hover {
  background: #bfe6a0;
}
/* 空き地アイコンはうっすら表示(街マップと同じ見え方。選択ハイライトを透かす)。 */
.cell.empty .cell-fg {
  opacity: 0.6;
}
.cell.facility {
  background: #dfe6ee;
  cursor: not-allowed;
}
.cell.house {
  background: #fff6e0;
  cursor: default;
}
.cell.house.own {
  outline: 2px solid #cc7a00;
  outline-offset: -2px;
}
.cell.selected {
  background: #ffd27a;
  outline: 2px solid #cc3300;
  outline-offset: -2px;
}
.cell img {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
}
.build-form .row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 4px 0;
  font-size: 13px;
}
.build-form .lbl {
  flex: 0 0 64px;
  color: #456;
  font-weight: bold;
}
.build-form .sel {
  font-size: 13px;
  padding: 2px 4px;
}
.build-form .ext-row {
  align-items: flex-start;
}
.build-form .ext-row .lbl {
  padding-top: 6px;
}
.tuika-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.tuika-opt {
  font-size: 13px;
  display: flex;
  align-items: center;
  gap: 4px;
}
.tuika-opt.off {
  color: #aaa;
}
.tuika-ng {
  font-size: 11px;
  color: #c44;
}
.tuika-note {
  font-size: 11px;
}
.build-form .note {
  color: #888;
  font-size: 12px;
}
.build-form .cost-row .cost {
  color: #cc3300;
  font-weight: bold;
  font-size: 15px;
}
.build-btn {
  background: #4a7a2a;
  color: #fff;
  font-weight: bold;
}
.hint {
  margin-top: 8px;
  font-size: 12px;
  color: #567;
  text-align: center;
}
.visit-panel {
  margin-top: 8px;
}
.visit-head {
  display: flex;
  align-items: center;
  gap: 8px;
}
.visit-head img {
  width: 36px;
  height: 36px;
  object-fit: contain;
}
.visit-info {
  flex: 1 1 auto;
}
.visit-owner {
  font-weight: bold;
  color: #345;
}
.visit-loc {
  font-size: 11px;
  color: #789;
}
.visit-comment {
  margin-top: 6px;
  font-size: 13px;
  color: #446;
  background: #f4f8ec;
  border-left: 3px solid #a8d488;
  padding: 4px 8px;
}
.visit-note {
  margin-top: 6px;
  font-size: 12px;
  color: #888;
}
.saisen-box {
  margin-top: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding-top: 6px;
  border-top: 1px dashed #cde;
}
.saisen-label {
  font-weight: bold;
  color: #b5651d;
}
.saisen-btn {
  background: #b5651d;
  color: #fff;
  font-weight: bold;
}
.mh-comment {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
}
.mh-cinput {
  flex: 1 1 auto;
  font-size: 12px;
  padding: 2px 4px;
}
/* コンテンツ枠エディタ(内装ランクで枠数が決まる)。 */
.mh-contents {
  margin-top: 6px;
  border: 1px dashed #cfd8c0;
  padding: 6px;
}
.mh-contents-head {
  font-size: 12px;
  font-weight: bold;
  color: #4a7a2a;
  margin-bottom: 4px;
}
.mh-content-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}
.mh-content-row .slot-no {
  font-size: 11px;
  color: #667;
  flex: 0 0 auto;
}
.mh-content-row select {
  font-size: 12px;
}
.slot-title {
  max-width: 180px;
}
.slot-url {
  max-width: 240px;
}
.mh-contents-note {
  font-size: 11px;
  color: #889;
  margin-bottom: 4px;
}
.mh-shop {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
}
.shop-badge {
  font-size: 12px;
  color: #7a4a00;
  background: #fff3e0;
  border: 1px solid #e0c080;
  padding: 2px 6px;
  flex: 1 1 auto;
}
.shop-none {
  font-size: 12px;
  color: #999;
  flex: 1 1 auto;
}
.shop-form {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  padding-top: 6px;
  border-top: 1px dashed #cde0bc;
  font-size: 12px;
}
.markup-input {
  width: 56px;
  font-size: 12px;
  padding: 2px 4px;
}
.orosi-panel {
  margin-top: 8px;
}
.orosi-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 6px;
}
.orosi-title {
  font-weight: bold;
  color: #7a4a00;
  font-size: 14px;
}
.orosi-info {
  font-size: 12px;
  color: #567;
  flex: 1 1 auto;
}
.orosi-empty {
  font-size: 12px;
  color: #888;
}
.orosi-scroll {
  overflow-x: auto;
}
.orosi-table {
  border-collapse: collapse;
  font-size: 12px;
  white-space: nowrap;
  width: 100%;
}
.orosi-table th {
  background: #f0e0c0;
  color: #634;
  padding: 3px 6px;
  border: 1px solid #dc9;
}
.orosi-table td {
  padding: 3px 6px;
  border: 1px solid #eee;
  text-align: center;
}
.orosi-table th.l,
.orosi-table td.l {
  text-align: left;
}
.orosi-table td.price {
  color: #cc3300;
  text-align: right;
}
.orosi-table td.full {
  color: #cc0000;
  font-weight: bold;
}
.qty-input {
  width: 50px;
  font-size: 12px;
  padding: 2px;
}
.visit-shop {
  margin-top: 8px;
  padding-top: 6px;
  border-top: 1px dashed #cde;
}
.vs-title {
  font-weight: bold;
  color: #7a4a00;
  margin-bottom: 4px;
}
.visit-bbs {
  margin-top: 8px;
  padding-top: 6px;
  border-top: 1px dashed #cde;
}
.bbs-form {
  display: flex;
  gap: 6px;
  margin-bottom: 4px;
}
.bbs-input {
  flex: 1 1 auto;
  font-size: 12px;
  padding: 2px 4px;
}
.bbs-list {
  list-style: none;
  margin: 0 0 8px;
  padding: 0;
  font-size: 12px;
}
.bbs-list li {
  padding: 2px 0;
  border-bottom: 1px solid #eee;
  color: #445;
}
.bbs-author {
  font-weight: bold;
  color: #367;
}
.bbs-del {
  border: none;
  background: none;
  color: #c44;
  cursor: pointer;
  font-weight: bold;
}
.bbs-empty {
  color: #999;
}
.my-houses .mh-head {
  font-weight: bold;
  color: #345;
  margin-bottom: 6px;
  border-bottom: 1px solid #cde;
  padding-bottom: 3px;
}
.my-houses .mh-empty {
  font-size: 12px;
  color: #888;
}
.mh-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.mh-item {
  background: #f6faf0;
  border: 1px solid #cde0bc;
  padding: 4px 6px;
  font-size: 12px;
}
.mh-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.mh-row img {
  width: 28px;
  height: 28px;
  object-fit: contain;
}
.mh-spacer {
  flex: 1 1 auto;
}
.mh-rebuild {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  padding-top: 6px;
  border-top: 1px dashed #cde0bc;
}
.mh-field {
  display: flex;
  align-items: center;
  gap: 3px;
}
.mh-cost {
  color: #cc3300;
  font-weight: bold;
}
.btn.mini {
  padding: 2px 6px;
  font-size: 11px;
}
.btn.danger {
  background: #c44;
  color: #fff;
}
.mh-loc {
  font-weight: bold;
  color: #345;
}
.mh-ext {
  color: #888;
  font-size: 11px;
}
.mh-note {
  margin-top: 6px;
  font-size: 11px;
  color: #889;
}
.message.error {
  background: #ffecec;
  border: 1px solid #e0a0a0;
  color: #b00;
  padding: 6px 10px;
  font-size: 12px;
  margin-bottom: 6px;
}
</style>
