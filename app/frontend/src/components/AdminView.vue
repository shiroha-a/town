<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import {
  api,
  assetUrl,
  type Player,
  type EffectOp,
  type Condition,
  type AdminItem,
  type AdminJob,
  type JobPayload,
  type SimResult,
  type AdminPlayerSummary,
  type AdminPlayerPayload,
  type GameSettings,
  type TownFacility,
  type TownAsset,
  type FacilityPreset,
  type PlotCell,
  type Town,
} from '../api';
import { PARAM_FULL } from '../params';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ back: [] }>();

const isAdmin = computed(() => props.player.roles.includes('admin'));

// 各セクションの開閉。既定は折りたたみ(false)。
const open = reactive({ item: false, job: false, user: false, settings: false, towns: false, map: false, events: false });

// 効果/条件で対象にできるパラメータ。
const PARAM_OPTIONS = [
  'energy',
  'nou_energy',
  'satiety',
  'kokugo',
  'suugaku',
  'rika',
  'syakai',
  'eigo',
  'ongaku',
  'bijutsu',
  'looks',
  'tairyoku',
  'kenkou',
  'speed',
  'power',
  'wanryoku',
  'kyakuryoku',
  'love',
  'omoshirosa',
];

const item = reactive<{
  name: string;
  category: string;
  price: number;
  effect: EffectOp[];
  stock_master: number | null;
}>({
  name: '',
  category: '',
  price: 0,
  effect: [],
  stock_master: null,
});
function emptyJob(): JobPayload {
  return {
    name: '',
    requirements: [],
    effect: [],
    salary: 1000,
    pay_interval: 1,
    bonus_rate: 0,
    raise_rate: 0,
    rank: 1,
    require_master: '',
    body_cost: 1,
    nou_cost: 0,
    enabled: true,
  };
}
const job = reactive<JobPayload>(emptyJob());

const sim = ref<SimResult | null>(null);
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);
const items = ref<AdminItem[]>([]);
const jobs = ref<AdminJob[]>([]);

function addOp(list: EffectOp[]) {
  list.push({ op: 'add_param', param: 'tairyoku', amount: 1 });
}
function addReq(list: Condition[]) {
  list.push({ pred: 'param_gte', param: 'tairyoku', value: 10 });
}
function fail(e: unknown) {
  message.value = e instanceof Error ? e.message : String(e);
  kind.value = 'error';
}

const players = ref<AdminPlayerSummary[]>([]);
const settings = ref<GameSettings | null>(null);
async function refresh() {
  if (!isAdmin.value) return;
  try {
    items.value = await api.adminListItems(props.player.id);
    jobs.value = await api.adminListJobs(props.player.id);
    players.value = await api.adminListPlayers(props.player.id);
    settings.value = await api.adminGetSettings(props.player.id);
    townmap.value = await api.townMap();
    assets.value = await api.townAssets();
    houseCells.value = await api.adminHouseCells(props.player.id);
    uploadedAssets.value = await api.adminListAssets(props.player.id);
    facPresets.value = await api.adminFacilityPresets(props.player.id);
    adminEvents.value = await api.adminListEvents(props.player.id);
    townList.value = await api.towns();
    syncTownDraft();
    selectedIdx.value = null;
  } catch (e) {
    fail(e);
  }
}
onMounted(refresh);

// タウンマップ編集(ビジュアルエディタ)。グリッドは16列(1-16) × 12行(A-L, 0始まり)。
const MAP_COLS = 16;
const MAP_ROWS = 12;
const mapCols = Array.from({ length: MAP_COLS }, (_, i) => i + 1);
const mapRows = 'ABCDEFGHIJKL'.split('');
const townmap = ref<TownFacility[]>([]);
const selectedIdx = ref<number | null>(null);

// 遷移先(key)のプリセット。実装済みルート + 準備中ビュー。
const KEY_PRESETS: { key: string; label: string }[] = [
  { key: 'depart', label: 'デパート' },
  { key: 'bank', label: '銀行' },
  { key: 'syokudou', label: '食堂' },
  { key: 'gym', label: 'ジム' },
  { key: 'onsen', label: '温泉' },
  { key: 'hospital', label: '病院' },
  { key: 'jobchange', label: '職業安定所' },
  { key: 'yakuba', label: '役場' },
  { key: 'item', label: 'アイテム' },
  { key: 'kabu', label: '株取引場(準備中)' },
  { key: 'keiba', label: '競馬場(準備中)' },
  { key: 'kentiku', label: '建設会社' },
  { key: 'casino', label: 'ゲームセンター' },
  { key: 'prof', label: 'プロフィール(準備中)' },
  { key: 'mail', label: 'メール(準備中)' },
  { key: 'doukyo', label: 'キャラ作成(準備中)' },
  { key: 'aisatu', label: 'あいさつ(準備中)' },
  { key: 'walk', label: '街移動(徒歩)' },
  { key: 'bus', label: '街移動(バス・500円)' },
  { key: 'akichi', label: '空き地(建築可能マス)' },
];
// 移動施設の遷移先key(選択時に行き先セレクタを出す)。
const MOVE_KEYS = ['walk', 'bus'];
// 施設用に用意されているgif(public/img)。
const IMG_PRESETS = [
  'depart', 'bank', 'syokudou', 'gym', 'onsen', 'hospital', 'work', 'yakuba', 'kabu', 'keiba', 'kentiku', 'game', 'prof', 'mail', 'mati_link', 'bus', 'akiti',
];

// 施設レイヤーで編集中の街(0..4)。施設はマルチ街化済み。
const facilityTown = ref(0);
const mapFacilityAt = (col: number, rowIdx: number, town = 0) =>
  townmap.value.findIndex((f) => f.town === town && f.col === col && f.row === rowIdx);
// 家が建っているマス。ここは編集不可(空き地を外すと家が孤立し不整合になる)。
const houseCells = ref<PlotCell[]>([]);
function houseCellAt(col: number, rowIdx: number): boolean {
  return houseCells.value.some(
    (h) => h.town === facilityTown.value && h.col === col && h.row === rowIdx,
  );
}
const selectedFacility = computed(() =>
  selectedIdx.value === null ? null : (townmap.value[selectedIdx.value] ?? null),
);

function clickCell(col: number, rowIdx: number) {
  // 家が建っているマスは編集不可(選択も移動先にもできない)。
  if (houseCellAt(col, rowIdx)) return;
  // 一括配置モード: 選択中のプリセットをクリックしたマスへ連続配置する。
  if (bulkPlace.value && bulkPresetIdx.value !== null) {
    placePresetAt(bulkPresetIdx.value, col, rowIdx);
    return;
  }
  const idx = mapFacilityAt(col, rowIdx, facilityTown.value);
  if (idx >= 0) {
    // 施設セル: 選択(同じものを再クリックで選択解除)。
    selectedIdx.value = selectedIdx.value === idx ? null : idx;
    return;
  }
  // 空セル: 選択中の施設をそこへ移動(占有セルへは移動できない=1セル1施設)。
  if (selectedIdx.value !== null) {
    townmap.value[selectedIdx.value].col = col;
    townmap.value[selectedIdx.value].row = rowIdx;
  }
}

// 一括配置モード: オンにするとプリセットをクリックで選択し、
// セルをクリックするたびに同じプリセットを連続配置できる。
const bulkPlace = ref(false);
const bulkPresetIdx = ref<number | null>(null);
function clickPresetChip(i: number) {
  if (!bulkPlace.value) return;
  bulkPresetIdx.value = bulkPresetIdx.value === i ? null : i;
}
// プリセットを指定セルへ配置する(占有セルは属性を上書き)。D&Dと一括配置で共用。
function placePresetAt(i: number, col: number, rowIdx: number): number {
  const p = allFacPresets.value[i];
  if (!p || houseCellAt(col, rowIdx)) return -1;
  const targetIdx = mapFacilityAt(col, rowIdx, facilityTown.value);
  if (targetIdx >= 0) {
    const f = townmap.value[targetIdx];
    f.key = p.key;
    f.img = p.img;
    f.alt = p.alt;
    f.dest = p.dest;
    f.ready = true;
    return targetIdx;
  }
  townmap.value.push({
    key: p.key,
    img: p.img,
    alt: p.alt,
    town: facilityTown.value,
    col,
    row: rowIdx,
    dest: p.dest,
    ready: true,
  });
  return townmap.value.length - 1;
}

// 標準施設の組み込みプリセット(townmap.Defaultと同じ内容+移動施設/空き地)。
// 常にパレットに並び、削除はできない。徒歩/バスは行き先の街ごとに1チップずつ
// 展開する(destの設定なしでそのまま配置できる)。
const STD_FAC_BASE: FacilityPreset[] = [
  { key: 'kabu', img: 'kabu', alt: '株取引場', dest: 0 },
  { key: 'depart', img: 'depart', alt: '中央デパート', dest: 0 },
  { key: 'bank', img: 'bank', alt: '銀行', dest: 0 },
  { key: 'syokudou', img: 'syokudou', alt: 'セントラル食堂', dest: 0 },
  { key: 'gym', img: 'gym', alt: 'ジム', dest: 0 },
  { key: 'keiba', img: 'keiba', alt: '競馬場', dest: 0 },
  { key: 'jobchange', img: 'work', alt: '職業安定所', dest: 0 },
  { key: 'onsen', img: 'onsen', alt: '温泉', dest: 0 },
  { key: 'hospital', img: 'hospital', alt: '中央病院', dest: 0 },
  { key: 'school', img: 'school', alt: '学校', dest: 0 },
  { key: 'kyushitu', img: 'school', alt: '教室', dest: 0 },
  { key: 'kentiku', img: 'kentiku', alt: '建設会社', dest: 0 },
  { key: 'casino', img: 'game', alt: 'ゲームセンター', dest: 0 },
  { key: 'hanbai', img: 'hanbai', alt: '自動販売機', dest: 0 },
  { key: 'yakuba', img: 'yakuba', alt: '役場（住民名鑑）', dest: 0 },
  { key: 'prof', img: 'prof', alt: 'プロフィール', dest: 0 },
  { key: 'akichi', img: 'akiti', alt: '空き地', dest: 0 },
];
const stdFacPresets = computed<FacilityPreset[]>(() => [
  ...STD_FAC_BASE,
  ...plotTowns.value.flatMap((t) => [
    { key: 'walk', img: 'mati_link', alt: `徒歩→${t.name}`, dest: t.no },
    { key: 'bus', img: 'bus', alt: `バス→${t.name}`, dest: t.no },
  ]),
]);

// 施設プリセット(画像・表示名・遷移先を保存したテンプレート)。パレットから
// D&Dで配置できる。プリセット自体の追加/削除は即サーバへ保存する。
const facPresets = ref<FacilityPreset[]>([]);
// パレット全体 = 標準(削除不可) + カスタム(保存済み)。D&Dはこの通し番号を使う。
const allFacPresets = computed(() => [...stdFacPresets.value, ...facPresets.value]);
const presetDraft = ref<FacilityPreset>({ key: 'depart', img: 'depart', alt: '', dest: 0 });
const presetFormOpen = ref(false);
async function savePreset() {
  if (!presetDraft.value.alt.trim()) {
    message.value = 'プリセットの表示名を入力してください。';
    kind.value = 'error';
    return;
  }
  busy.value = true;
  try {
    facPresets.value = await api.adminUpdateFacilityPresets(props.player.id, [
      ...facPresets.value,
      { ...presetDraft.value, alt: presetDraft.value.alt.trim() },
    ]);
    presetFormOpen.value = false;
    presetDraft.value = { key: 'depart', img: 'depart', alt: '', dest: 0 };
    message.value = '施設プリセットを保存しました。';
    kind.value = 'ok';
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}
async function deletePreset(i: number) {
  if (!confirm(`プリセット「${facPresets.value[i]?.alt}」を削除しますか?`)) return;
  busy.value = true;
  try {
    const next = facPresets.value.filter((_, j) => j !== i);
    facPresets.value = await api.adminUpdateFacilityPresets(props.player.id, next);
    message.value = '施設プリセットを削除しました。';
    kind.value = 'ok';
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

// ドラッグ&ドロップで施設を配置する。タイル移動は占有セルで位置を入れ替え、
// プリセットは空セルへ新規配置(占有セルは属性を上書き)する。
type FacDrag = { kind: 'tile'; idx: number } | { kind: 'preset'; i: number };
const facDrag = ref<FacDrag | null>(null);
const dragging = computed(() => (facDrag.value?.kind === 'tile' ? facDrag.value.idx : null));
function onDragStart(idx: number) {
  if (idx < 0) return;
  facDrag.value = { kind: 'tile', idx };
  selectedIdx.value = idx;
}
function onPresetDragStart(i: number) {
  facDrag.value = { kind: 'preset', i };
}
function onDragEnd() {
  facDrag.value = null;
}
function onDrop(col: number, rowIdx: number) {
  const d = facDrag.value;
  facDrag.value = null;
  if (!d) return;
  // 家が建っているマスへは配置できない(空き地を外すと不整合)。
  if (houseCellAt(col, rowIdx)) return;
  const targetIdx = mapFacilityAt(col, rowIdx, facilityTown.value);
  if (d.kind === 'preset') {
    const placed = placePresetAt(d.i, col, rowIdx);
    if (placed >= 0) selectedIdx.value = placed;
    return;
  }
  const src = townmap.value[d.idx];
  if (targetIdx >= 0 && targetIdx !== d.idx) {
    // 移動先に別の施設があれば位置を入れ替える。
    const tgt = townmap.value[targetIdx];
    tgt.col = src.col;
    tgt.row = src.row;
  }
  src.col = col;
  src.row = rowIdx;
}

function firstFreeCell(): { col: number; row: number } | null {
  for (let r = 0; r < MAP_ROWS; r++) {
    for (let c = 1; c <= MAP_COLS; c++) {
      if (mapFacilityAt(c, r, facilityTown.value) < 0) return { col: c, row: r };
    }
  }
  return null;
}
function addFacility() {
  const cell = firstFreeCell();
  if (!cell) {
    message.value = 'マップに空きセルがありません。';
    kind.value = 'error';
    return;
  }
  townmap.value.push({
    key: 'depart',
    img: 'depart',
    alt: '新規施設',
    town: facilityTown.value,
    col: cell.col,
    row: cell.row,
    dest: 0,
    ready: true,
  });
  selectedIdx.value = townmap.value.length - 1;
}
function deleteFacility() {
  if (selectedIdx.value === null) return;
  townmap.value.splice(selectedIdx.value, 1);
  selectedIdx.value = null;
}
async function saveTownMap() {
  busy.value = true;
  message.value = '';
  try {
    townmap.value = await api.adminUpdateTownMap(props.player.id, townmap.value);
    selectedIdx.value = null;
    message.value = 'タウンマップを更新しました。';
    kind.value = 'ok';
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

// 背景アセット配置レイヤー。施設とは別に、装飾用の背景画像をセル単位で置く。
const assets = ref<TownAsset[]>([]);
// 編集中のレイヤー('facility'=施設(空き地含む) / 'asset'=背景)。
const mapLayer = ref<'facility' | 'asset'>('facility');
// 背景アセットのパレット(組み込みのSVG地形素材)。
const BG_PRESETS = [
  'kusa', 'tuti', 'sima', 'umi', 'oki',
  'michi_yoko', 'michi_tate', 'michi_kado1', 'michi_kado2', 'michi_kado3', 'michi_kado4',
  'michi_t1', 'michi_t2', 'michi_t3', 'michi_t4', 'michi_juji',
  'tree1', 'tree2', 'tree3', 'tree4', 'yama',
];
// アップロードされた画像名(背景に追加できる)。'u:'接頭辞でimg値に使う。
const uploadedAssets = ref<string[]>([]);
// パレット = 組み込み + アップロード('u:'接頭辞)。
const bgPalette = computed(() => [...BG_PRESETS, ...uploadedAssets.value.map((n) => `u:${n}`)]);
// 選択中の「筆」(パレットで選んだ背景アセット)。
const assetBrush = ref<string>(BG_PRESETS[0]);
// 背景レイヤーで編集中の街(0..4)。背景も街ごとに配置できる。
const assetTown = ref(0);

// 1マスに重ねられる背景レイヤー数の上限(サーバーのMaxAssetLayersと合わせる)。
const MAX_BG_LAYERS = 3;
// 消しゴム筆(最上層を1枚ずつ剥がす)。パレットの特殊スウォッチ。
const BG_ERASER = '__eraser__';

// 指定マスのアセットindex一覧(配列順=重ね順、末尾が最上層)。
function assetIdxsAt(col: number, rowIdx: number): number[] {
  const out: number[] = [];
  assets.value.forEach((a, i) => {
    if (a.town === assetTown.value && a.col === col && a.row === rowIdx) out.push(i);
  });
  return out;
}
function assetImgsAt(col: number, rowIdx: number): string[] {
  return assetIdxsAt(col, rowIdx).map((i) => assets.value[i].img);
}
// 指定した街の背景アセット画像(施設レイヤーで背景を薄く参照表示するのに使う)。
function assetImgsForTown(col: number, rowIdx: number, town: number): string[] {
  return assets.value.filter((x) => x.town === town && x.col === col && x.row === rowIdx).map((x) => x.img);
}
// マスをクリックで背景を配置(常に連続配置)。最上層と同じ筆なら除去、
// 消しゴムなら最上層を剥がし、それ以外は最上層に積む(上限あり)。
function paintAsset(col: number, rowIdx: number) {
  const idxs = assetIdxsAt(col, rowIdx);
  const topIdx = idxs.length ? idxs[idxs.length - 1] : -1;
  if (assetBrush.value === BG_ERASER) {
    if (topIdx >= 0) assets.value.splice(topIdx, 1);
    return;
  }
  if (topIdx >= 0 && assets.value[topIdx].img === assetBrush.value) {
    assets.value.splice(topIdx, 1);
    return;
  }
  if (idxs.length >= MAX_BG_LAYERS) {
    message.value = `1マスに置ける背景は${MAX_BG_LAYERS}層までです。`;
    kind.value = 'error';
    return;
  }
  assets.value.push({ img: assetBrush.value, town: assetTown.value, col, row: rowIdx });
}
async function saveTownAssets() {
  busy.value = true;
  message.value = '';
  try {
    assets.value = await api.adminUpdateTownAssets(props.player.id, assets.value);
    message.value = '背景レイヤーを更新しました。';
    kind.value = 'ok';
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

// 背景アセットの画像アップロード。ファイル名からスラッグを作り、base64で送る。
function slugFromFilename(fn: string): string {
  const base = fn.replace(/\.[^.]+$/, '');
  const slug = base.replace(/[^a-zA-Z0-9_-]/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '');
  return slug.slice(0, 40) || 'asset';
}
async function onUploadAsset(e: Event) {
  const input = e.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  busy.value = true;
  message.value = '';
  try {
    // ファイルをbase64(本体のみ)に変換する。
    const dataUrl: string = await new Promise((resolve, reject) => {
      const rd = new FileReader();
      rd.onload = () => resolve(String(rd.result));
      rd.onerror = () => reject(rd.error);
      rd.readAsDataURL(file);
    });
    const b64 = dataUrl.split(',')[1] ?? '';
    const name = slugFromFilename(file.name);
    const res = await api.adminUploadAsset(props.player.id, name, file.type, b64);
    uploadedAssets.value = await api.adminListAssets(props.player.id);
    assetBrush.value = `u:${res.name}`; // アップロードした素材を筆に選択
    message.value = `背景アセット「${res.name}」を追加しました。`;
    kind.value = 'ok';
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
    input.value = ''; // 同じファイルを再選択できるようにクリア
  }
}
// アップロード画像を削除する('u:name'形式のパレット項目のみ)。
async function deleteUploadedAsset(img: string) {
  if (!img.startsWith('u:')) return;
  const name = img.slice(2);
  if (!confirm(`背景アセット「${name}」を削除しますか?`)) return;
  busy.value = true;
  message.value = '';
  try {
    await api.adminDeleteAsset(props.player.id, name);
    uploadedAssets.value = await api.adminListAssets(props.player.id);
    if (assetBrush.value === img) assetBrush.value = BG_PRESETS[0]; // 筆が消えたら組み込みに戻す
    message.value = `背景アセット「${name}」を削除しました。`;
    kind.value = 'ok';
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

// 背景レイヤーのドラッグ&ドロップ。パレットからの新規配置と、置いたタイルの移動に対応。
type BgDrag = { kind: 'palette'; img: string } | { kind: 'tile'; col: number; row: number };
const bgDrag = ref<BgDrag | null>(null);
function onBgPaletteDragStart(img: string) {
  assetBrush.value = img; // ドラッグ元の素材を筆にも反映
  bgDrag.value = { kind: 'palette', img };
}
function onBgTileDragStart(col: number, rowIdx: number) {
  bgDrag.value = { kind: 'tile', col, row: rowIdx };
}
function onBgDragEnd() {
  bgDrag.value = null;
}
function onBgDrop(col: number, rowIdx: number) {
  const d = bgDrag.value;
  bgDrag.value = null;
  if (!d) return;
  const tgtCount = assetIdxsAt(col, rowIdx).length;
  if (d.kind === 'palette') {
    // パレットからドロップ: そのマスの最上層に積む。
    if (d.img === BG_ERASER) return;
    if (tgtCount >= MAX_BG_LAYERS) {
      message.value = `1マスに置ける背景は${MAX_BG_LAYERS}層までです。`;
      kind.value = 'error';
      return;
    }
    assets.value.push({ img: d.img, town: assetTown.value, col, row: rowIdx });
    return;
  }
  // 置いたタイルの移動: 移動元の最上層を剥がし、移動先の最上層に積む。
  if (d.col === col && d.row === rowIdx) return;
  const srcIdxs = assetIdxsAt(d.col, d.row);
  if (!srcIdxs.length) return;
  if (tgtCount >= MAX_BG_LAYERS) {
    message.value = `1マスに置ける背景は${MAX_BG_LAYERS}層までです。`;
    kind.value = 'error';
    return;
  }
  const [moved] = assets.value.splice(srcIdxs[srcIdxs.length - 1], 1);
  moved.col = col;
  moved.row = rowIdx;
  assets.value.push(moved);
}

// カスタムイベント管理(ランダムイベントの追加/編集/削除)。
const adminEvents = ref<import('../api').AdminEvent[]>([]);
const EV_PARAM_OPTIONS = [
  'kokugo', 'suugaku', 'rika', 'syakai', 'eigo', 'ongaku', 'bijutsu', 'looks',
  'tairyoku', 'kenkou', 'speed', 'power', 'wanryoku', 'kyakuryoku', 'love', 'omoshirosa',
  'energy', 'nou_energy', 'satiety',
];
const EV_DISEASES: { label: string; value: number | null }[] = [
  { label: 'なし', value: null },
  { label: '風邪ぎみ(-8)', value: -8 },
  { label: '風邪(-15)', value: -15 },
  { label: '下痢(-18)', value: -18 },
  { label: '肺炎(-30)', value: -30 },
  { label: '結核(-50)', value: -50 },
  { label: '脳腫瘍(-80)', value: -80 },
  { label: '癌(-120)', value: -120 },
];
// 発生条件の編集行。predごとに使うフィールドが変わる。
const EV_COND_PREDS: { value: string; label: string }[] = [
  { value: 'money_gte', label: '所持金が◯円以上' },
  { value: 'money_lte', label: '所持金が◯円以下' },
  { value: 'param_gte', label: 'パラメータが◯以上' },
  { value: 'param_lte', label: 'パラメータが◯以下' },
  { value: 'has_item', label: 'アイテムを所持' },
  { value: 'job_is', label: '職業が' },
];
function emptyEvent(): import('../api').AdminEvent {
  return {
    id: 0, name: '', message: '', good: true, money_min: 0, money_max: 0,
    params: {}, disease_set: null, weight_g: 0, weight: 1, enabled: true, conditions: [],
  };
}
const evForm = ref(emptyEvent());
const evParamRows = ref<{ key: string; value: number }[]>([]);
const evCondRows = ref<import('../api').EventCond[]>([]);
function evEdit(e: import('../api').AdminEvent) {
  evForm.value = { ...e, params: { ...e.params } };
  evParamRows.value = Object.entries(e.params).map(([key, value]) => ({ key, value }));
  evCondRows.value = (e.conditions ?? []).map((c) => ({ ...c }));
}
function evReset() {
  evForm.value = emptyEvent();
  evParamRows.value = [];
  evCondRows.value = [];
}
async function evSave() {
  busy.value = true;
  message.value = '';
  try {
    const params: Record<string, number> = {};
    for (const r of evParamRows.value) {
      if (r.key && r.value) params[r.key] = r.value;
    }
    const payload = { ...evForm.value, params, conditions: evCondRows.value };
    if (payload.id > 0) await api.adminUpdateEvent(props.player.id, payload);
    else await api.adminCreateEvent(props.player.id, payload);
    adminEvents.value = await api.adminListEvents(props.player.id);
    evReset();
    message.value = 'イベントを保存しました。';
    kind.value = 'ok';
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}
async function evDelete() {
  if (evForm.value.id <= 0) return;
  if (!confirm(`イベント「${evForm.value.name}」を削除しますか?`)) return;
  busy.value = true;
  try {
    await api.adminDeleteEvent(props.player.id, evForm.value.id);
    adminEvents.value = await api.adminListEvents(props.player.id);
    evReset();
    message.value = 'イベントを削除しました。';
    kind.value = 'ok';
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

// 街の一覧(管理画面で設定可能。名前・地価)。マップ編集の街セレクタや街エディタで使う。
const townList = ref<Town[]>([]);
// マップ編集の街セレクタ用(no+name)。街は設定で可変。
const plotTowns = computed(() => townList.value.map((t) => ({ no: t.no, name: t.name })));

// 街エディタの編集用ドラフト(名前・地価・隠し町)。保存で adminUpdateTowns。
const townDraft = ref<{ name: string; land_price: number; hidden: boolean }[]>([]);
function syncTownDraft() {
  townDraft.value = townList.value.map((t) => ({
    name: t.name,
    land_price: t.land_price,
    hidden: t.hidden,
  }));
}
function addTown() {
  townDraft.value.push({ name: '新しい街', land_price: 250, hidden: false });
}
function removeTown(i: number) {
  townDraft.value.splice(i, 1);
}
async function saveTowns() {
  busy.value = true;
  message.value = '';
  try {
    await api.adminUpdateTowns(props.player.id, townDraft.value);
    townList.value = await api.towns();
    syncTownDraft();
    message.value = '街の設定を更新しました。';
    kind.value = 'ok';
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

// サーバー設定(数値項目)の入力欄メタデータ。ラベルと簡単な補足を持つ。
const SETTINGS_FIELDS: { key: keyof GameSettings; label: string; hint?: string }[] = [
  { key: 'initial_money', label: '初期所持金', hint: '新規登録時に付与される金額(円)' },
  { key: 'daily_interest_permille', label: '日次利息', hint: '貯金に対する1日あたりの利息(‰/千分率)' },
  { key: 'energy_recovery_sec', label: '身体P回復間隔', hint: '身体パワーが1回復する秒数' },
  { key: 'nou_recovery_sec', label: '頭脳P回復間隔', hint: '頭脳パワーが1回復する秒数' },
  { key: 'satiety_decay_sec', label: '満腹度減少間隔', hint: '満腹度が1減少する秒数' },
  { key: 'condition_eval_interval_min', label: '病気評価間隔', hint: '病気指数を再評価する間隔(分)' },
  { key: 'work_interval_min', label: '仕事間隔', hint: '連続して働けるようになるまでの分数' },
  { key: 'depart_daily_count', label: 'デパート日次件数', hint: '0で全件(日次ローテ無効)' },
  { key: 'syokudou_daily_count', label: '食堂日次件数', hint: '0で全件(日次ローテ無効)' },
  { key: 'item_kind_limit', label: '所持アイテム種類上限', hint: '0で無制限(旧TOWN 25品目)' },
  { key: 'stock_adjust', label: '店頭在庫倍率', hint: '実在庫=ceil(標準在庫÷倍率)。大きいほど品薄' },
  { key: 'move_walk_secs', label: '徒歩の移動時間', hint: '街移動(徒歩)にかかる秒数。0以下で既定10秒' },
  { key: 'move_bus_secs', label: 'バスの移動時間', hint: '街移動(バス)にかかる秒数。0以下で既定5秒' },
];
async function saveSettings() {
  if (!settings.value) return;
  busy.value = true;
  message.value = '';
  try {
    settings.value = await api.adminUpdateSettings(props.player.id, settings.value);
    message.value = 'サーバー設定を更新しました。';
    kind.value = 'ok';
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

// プレイヤー編集(頭脳/身体/その他の各パラメータ)。
const DETAIL_PARAMS: { key: keyof Player['params']; label: string }[] = [
  { key: 'kokugo', label: '国語' },
  { key: 'suugaku', label: '数学' },
  { key: 'rika', label: '理科' },
  { key: 'syakai', label: '社会' },
  { key: 'eigo', label: '英語' },
  { key: 'ongaku', label: '音楽' },
  { key: 'bijutsu', label: '美術' },
  { key: 'looks', label: 'ルックス' },
  { key: 'tairyoku', label: '体力' },
  { key: 'kenkou', label: '健康' },
  { key: 'speed', label: 'スピード' },
  { key: 'power', label: 'パワー' },
  { key: 'wanryoku', label: '腕力' },
  { key: 'kyakuryoku', label: '脚力' },
  { key: 'love', label: 'LOVE' },
  { key: 'omoshirosa', label: '面白さ' },
];
const editingPlayer = ref<(AdminPlayerPayload & { id: number }) | null>(null);
// 職業の選択肢: 学生(初期職) + content_jobs。編集中プレイヤーの現職も必ず含める。
const jobOptions = computed(() => {
  const set = new Set<string>(['学生', ...jobs.value.map((j) => j.name)]);
  if (editingPlayer.value?.job) set.add(editingPlayer.value.job);
  return [...set];
});
async function openEditPlayer(id: number) {
  message.value = '';
  try {
    const p = await api.getPlayer(id);
    editingPlayer.value = {
      id: p.id,
      display_name: p.display_name,
      money: p.money,
      is_admin: p.roles.includes('admin'),
      params: { ...p.params },
      energy: p.status.energy,
      nou_energy: p.status.nou_energy,
      satiety: p.status.satiety,
      job: p.status.job,
      job_level: p.status.job_level,
      job_exp: p.status.job_exp,
      disease_index: p.status.disease_index,
      height_cm: p.status.height_cm,
      weight_g: p.status.weight_g,
    };
  } catch (e) {
    fail(e);
  }
}
function closeEditPlayer() {
  editingPlayer.value = null;
}
async function savePlayer() {
  if (!editingPlayer.value) return;
  busy.value = true;
  message.value = '';
  try {
    const { id, ...payload } = editingPlayer.value;
    await api.adminUpdatePlayer(props.player.id, id, payload);
    message.value = `ユーザー「${payload.display_name}」を更新しました。`;
    kind.value = 'ok';
    closeEditPlayer();
    await refresh();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}
async function deletePlayer() {
  if (!editingPlayer.value) return;
  if (!window.confirm(`ユーザー「${editingPlayer.value.display_name}」を論理削除しますか?`)) return;
  busy.value = true;
  message.value = '';
  try {
    await api.adminDeletePlayer(props.player.id, editingPlayer.value.id);
    message.value = 'ユーザーを論理削除しました。';
    kind.value = 'ok';
    closeEditPlayer();
    await refresh();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function simulate() {
  busy.value = true;
  message.value = '';
  try {
    // 仮想的な標準state(お金10万・全パラメータ10/上限999)で試算する。
    const params = Object.fromEntries(PARAM_OPTIONS.map((p) => [p, { value: 10, max: 999 }]));
    sim.value = await api.adminSimulate(props.player.id, item.effect, { money: 100000, params });
  } catch (e) {
    sim.value = null;
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function createItem() {
  busy.value = true;
  message.value = '';
  try {
    await api.adminCreateItem(props.player.id, {
      name: item.name,
      category: item.category,
      price: item.price,
      effect: item.effect,
      stock_master: item.stock_master,
    });
    message.value = `アイテム「${item.name}」を作成しました。`;
    kind.value = 'ok';
    item.name = '';
    item.category = '';
    item.price = 0;
    item.effect = [];
    item.stock_master = null;
    await refresh();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function createJob() {
  busy.value = true;
  message.value = '';
  try {
    await api.adminCreateJob(props.player.id, { ...job });
    message.value = `職業「${job.name}」を作成しました。`;
    kind.value = 'ok';
    Object.assign(job, emptyJob());
    await refresh();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

// 一覧クリックで開く職業編集(詳細)。
const editingJob = ref<AdminJob | null>(null);
function openEditJob(j: AdminJob) {
  editingJob.value = {
    ...j,
    requirements: j.requirements.map((r) => ({ ...r })),
    effect: j.effect.map((o) => ({ ...o })),
  };
}
function closeEditJob() {
  editingJob.value = null;
}
async function saveJob() {
  if (!editingJob.value) return;
  busy.value = true;
  message.value = '';
  try {
    const e = editingJob.value;
    await api.adminUpdateJob(props.player.id, e.id, {
      name: e.name,
      requirements: e.requirements,
      effect: e.effect,
      salary: e.salary,
      pay_interval: e.pay_interval,
      bonus_rate: e.bonus_rate,
      raise_rate: e.raise_rate,
      rank: e.rank,
      require_master: e.require_master,
      body_cost: e.body_cost,
      nou_cost: e.nou_cost,
      enabled: e.enabled,
    });
    message.value = `職業「${e.name}」を更新しました。`;
    kind.value = 'ok';
    closeEditJob();
    await refresh();
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
  }
}
async function deleteJob() {
  if (!editingJob.value) return;
  if (!window.confirm(`職業「${editingJob.value.name}」を削除しますか?`)) return;
  busy.value = true;
  message.value = '';
  try {
    await api.adminDeleteJob(props.player.id, editingJob.value.id);
    message.value = '職業を削除しました。';
    kind.value = 'ok';
    closeEditJob();
    await refresh();
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
  }
}

// 一覧クリックで開くアイテム編集(詳細)。編集対象は作業用コピー。
const editing = ref<AdminItem | null>(null);
function openEdit(it: AdminItem) {
  editing.value = { ...it, effect: it.effect.map((o) => ({ ...o })) };
}
function closeEdit() {
  editing.value = null;
}
async function saveEdit() {
  if (!editing.value) return;
  busy.value = true;
  message.value = '';
  try {
    await api.adminUpdateItem(props.player.id, editing.value.id, {
      name: editing.value.name,
      category: editing.value.category,
      price: editing.value.price,
      effect: editing.value.effect,
      enabled: editing.value.enabled,
      stock_master: editing.value.stock_master,
    });
    message.value = `アイテム「${editing.value.name}」を更新しました。`;
    kind.value = 'ok';
    closeEdit();
    await refresh();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}
async function deleteEdit() {
  if (!editing.value) return;
  if (!window.confirm(`アイテム「${editing.value.name}」を削除しますか?`)) return;
  busy.value = true;
  message.value = '';
  try {
    await api.adminDeleteItem(props.player.id, editing.value.id);
    message.value = 'アイテムを削除しました。';
    kind.value = 'ok';
    closeEdit();
    await refresh();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page admin-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>
    <div class="admin-header">
      <div class="lead">管理者用のコンテンツ作成画面です。アイテム・職業の追加と効果の試算ができます。</div>
      <div class="title">管理者</div>
    </div>

    <div v-if="!isAdmin" class="message error">この画面は管理者のみ利用できます。</div>

    <template v-else>
      <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

      <div class="admin-sections">
        <!-- ユーザー -->
        <section class="fold">
          <button class="fold-head" @click="open.user = !open.user">
            <span class="caret">{{ open.user ? '▼' : '▶' }}</span> ユーザー（{{ players.length }}）
          </button>
          <div v-if="open.user" class="fold-body">
            <section class="panel">
              <h3>ユーザー一覧<span class="hint"> ※行をクリックで確認/編集</span></h3>
              <div class="table-scroll">
                <table class="list-table">
                  <thead><tr><th>ID</th><th class="l">名前</th><th>職業</th><th>Lv</th><th>所持金</th><th>権限</th></tr></thead>
                  <tbody>
                    <tr v-for="u in players" :key="u.id" class="clickable" @click="openEditPlayer(u.id)">
                      <td>{{ u.id }}</td><td class="l">{{ u.display_name }}</td><td>{{ u.job }}</td>
                      <td>{{ u.job_level }}</td><td class="r">{{ u.money.toLocaleString('ja-JP') }}円</td>
                      <td>{{ u.roles.includes('admin') ? '管理者' : '' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </section>
          </div>
        </section>

        <!-- アイテム -->
        <section class="fold">
          <button class="fold-head" @click="open.item = !open.item">
            <span class="caret">{{ open.item ? '▼' : '▶' }}</span> アイテム（{{ items.length }}）
          </button>
          <div v-if="open.item" class="fold-body">
            <section class="panel">
              <h3>アイテム作成</h3>
              <label>品名<input v-model="item.name" placeholder="例: 特製栄養ドリンク" /></label>
              <label>カテゴリ<input v-model="item.category" placeholder="例: ドリンク" /></label>
              <label>値段<input type="number" v-model.number="item.price" /></label>
              <label>標準在庫数<input type="number" v-model.number="item.stock_master" placeholder="空=無制限" /></label>
              <div class="ops">
                <div class="ops-head">使用効果</div>
                <div v-for="(op, i) in item.effect" :key="i" class="op-row">
                  <select v-model="op.op">
                    <option value="add_param">パラメータ</option>
                    <option value="add_money">お金</option>
                  </select>
                  <select v-if="op.op === 'add_param'" v-model="op.param">
                    <option v-for="p in PARAM_OPTIONS" :key="p" :value="p">{{ PARAM_FULL[p] ?? p }}</option>
                  </select>
                  <input type="number" v-model.number="op.amount" />
                  <button class="btn mini" @click="item.effect.splice(i, 1)">×</button>
                </div>
                <button class="btn mini" @click="addOp(item.effect)">＋効果を追加</button>
              </div>
              <div class="actions">
                <button class="btn" :disabled="busy" @click="simulate">効果を試算</button>
                <button class="btn primary" :disabled="busy || !item.name" @click="createItem">作成</button>
              </div>
              <div v-if="sim" class="sim-box">
                <div class="ops-head">試算結果</div>
                <div v-if="sim.plan.money_delta !== 0">お金: {{ sim.plan.money_delta > 0 ? '+' : '' }}{{ sim.plan.money_delta }}円</div>
                <div v-for="pc in sim.plan.params" :key="pc.name">{{ pc.name }}: {{ pc.old_value }} → {{ pc.new_value }}</div>
                <div v-if="!sim.plan.params.length && sim.plan.money_delta === 0" class="muted">変化なし</div>
                <div v-for="(w, i) in sim.warnings" :key="i" class="warn">⚠ {{ w }}</div>
              </div>
            </section>
            <section class="panel">
              <h3>既存アイテム（{{ items.length }}）<span class="hint"> ※行をクリックで編集</span></h3>
              <div class="table-scroll">
                <table class="list-table">
                  <thead><tr><th>ID</th><th class="l">品名</th><th>カテゴリ</th><th>値段</th><th>有効</th></tr></thead>
                  <tbody>
                    <tr v-for="it in items" :key="it.id" class="clickable" @click="openEdit(it)">
                      <td>{{ it.id }}</td><td class="l">{{ it.name }}</td><td>{{ it.category }}</td>
                      <td class="r">{{ it.price }}</td><td :class="{ off: !it.enabled }">{{ it.enabled ? '○' : '×' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </section>
          </div>
        </section>

        <!-- 職業 -->
        <section class="fold">
          <button class="fold-head" @click="open.job = !open.job">
            <span class="caret">{{ open.job ? '▼' : '▶' }}</span> 職業（{{ jobs.length }}）
          </button>
          <div v-if="open.job" class="fold-body">
            <section class="panel">
              <h3>職業作成</h3>
              <label>職業名<input v-model="job.name" placeholder="例: 見習い店員" /></label>
              <div class="econ-grid">
                <label>給料<input type="number" v-model.number="job.salary" /></label>
                <label>支払間隔<input type="number" v-model.number="job.pay_interval" /></label>
                <label>ボーナス%<input type="number" v-model.number="job.bonus_rate" /></label>
                <label>昇給%<input type="number" v-model.number="job.raise_rate" /></label>
                <label>ランク<input type="number" v-model.number="job.rank" /></label>
                <label>身体消費<input type="number" v-model.number="job.body_cost" /></label>
                <label>頭脳消費<input type="number" v-model.number="job.nou_cost" /></label>
                <label class="wide2">前提マスター職<input v-model="job.require_master" placeholder="なし" /></label>
              </div>
              <div class="ops">
                <div class="ops-head">就くための必要条件(以上)</div>
                <div v-for="(req, i) in job.requirements" :key="i" class="op-row">
                  <select v-model="req.param">
                    <option v-for="p in PARAM_OPTIONS" :key="p" :value="p">{{ PARAM_FULL[p] ?? p }}</option>
                  </select>
                  <span class="ge">≧</span>
                  <input type="number" v-model.number="req.value" />
                  <button class="btn mini" @click="job.requirements.splice(i, 1)">×</button>
                </div>
                <button class="btn mini" @click="addReq(job.requirements)">＋条件を追加</button>
              </div>
              <div class="ops">
                <div class="ops-head">働いたときの効果</div>
                <div v-for="(op, i) in job.effect" :key="i" class="op-row">
                  <select v-model="op.op">
                    <option value="add_param">パラメータ</option>
                    <option value="add_money">お金</option>
                  </select>
                  <select v-if="op.op === 'add_param'" v-model="op.param">
                    <option v-for="p in PARAM_OPTIONS" :key="p" :value="p">{{ PARAM_FULL[p] ?? p }}</option>
                  </select>
                  <input type="number" v-model.number="op.amount" />
                  <button class="btn mini" @click="job.effect.splice(i, 1)">×</button>
                </div>
                <button class="btn mini" @click="addOp(job.effect)">＋効果を追加</button>
              </div>
              <div class="actions">
                <button class="btn primary" :disabled="busy || !job.name" @click="createJob">作成</button>
              </div>
            </section>
            <section class="panel">
              <h3>既存職業（{{ jobs.length }}）<span class="hint"> ※行をクリックで編集</span></h3>
              <div class="table-scroll">
                <table class="list-table">
                  <thead><tr><th>ID</th><th class="l">職業名</th><th>給料</th><th>ランク</th><th>前提職</th><th>有効</th></tr></thead>
                  <tbody>
                    <tr v-for="j in jobs" :key="j.id" class="clickable" @click="openEditJob(j)">
                      <td>{{ j.id }}</td><td class="l">{{ j.name }}</td><td class="r">{{ j.salary }}</td>
                      <td>{{ j.rank }}</td><td class="l">{{ j.require_master }}</td>
                      <td :class="{ off: !j.enabled }">{{ j.enabled ? '○' : '×' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </section>
          </div>
        </section>

        <!-- カスタムイベント -->
        <section class="fold">
          <button class="fold-head" @click="open.events = !open.events">
            <span class="caret">{{ open.events ? '▼' : '▶' }}</span> イベント（{{ adminEvents.length }}）
          </button>
          <div v-if="open.events" class="fold-body">
            <section class="panel">
              <h3>
                {{ evForm.id > 0 ? `イベント編集 #${evForm.id}` : 'イベント作成' }}
                <span class="hint"> ※組み込みイベントと同じ抽選(発生率1/12)に合流します</span>
              </h3>
              <label>名前<input v-model="evForm.name" placeholder="例: 落とし穴" /></label>
              <label>メッセージ<input v-model="evForm.message" class="wide" placeholder="例: 落とし穴に落ちて{money}円落としました。" /></label>
              <span class="hint">※プレースホルダー: {money}=実際の増減額 {name}=プレイヤー名 {job}=職業 {town}=今いる街</span>
              <label class="chk"><input type="checkbox" v-model="evForm.good" /> 良いイベント（トーストの色）</label>
              <label>お金(最小)<input type="number" v-model.number="evForm.money_min" /></label>
              <label>お金(最大)<input type="number" v-model.number="evForm.money_max" /></label>
              <span class="hint">※増減額は最小〜最大の一様乱数。マイナスで支払い。固定額は同値に</span>
              <div class="ops">
                <div class="ops-head">パラメータ増減</div>
                <div v-for="(r, i) in evParamRows" :key="i" class="op-row">
                  <select v-model="r.key">
                    <option v-for="p in EV_PARAM_OPTIONS" :key="p" :value="p">{{ PARAM_FULL[p] ?? p }}</option>
                  </select>
                  <input type="number" v-model.number="r.value" />
                  <button class="btn mini" @click="evParamRows.splice(i, 1)">×</button>
                </div>
                <button class="btn mini" @click="evParamRows.push({ key: 'kokugo', value: 1 })">＋パラメータを追加</button>
              </div>
              <div class="ops">
                <div class="ops-head">発生条件（すべて満たすプレイヤーにだけ発生。空=全員）</div>
                <div v-for="(c, i) in evCondRows" :key="i" class="op-row">
                  <select v-model="c.pred">
                    <option v-for="p in EV_COND_PREDS" :key="p.value" :value="p.value">{{ p.label }}</option>
                  </select>
                  <select v-if="c.pred === 'param_gte' || c.pred === 'param_lte'" v-model="c.param">
                    <option v-for="p in EV_PARAM_OPTIONS" :key="p" :value="p">{{ PARAM_FULL[p] ?? p }}</option>
                  </select>
                  <input
                    v-if="c.pred !== 'has_item' && c.pred !== 'job_is'"
                    type="number"
                    v-model.number="c.value"
                    placeholder="値"
                  />
                  <select v-if="c.pred === 'has_item'" v-model.number="c.item_id">
                    <option v-for="it in items" :key="it.id" :value="it.id">{{ it.name }}</option>
                  </select>
                  <select v-if="c.pred === 'job_is'" v-model="c.job">
                    <option v-for="j in jobs" :key="j.id" :value="j.name">{{ j.name }}</option>
                  </select>
                  <button class="btn mini" @click="evCondRows.splice(i, 1)">×</button>
                </div>
                <button class="btn mini" @click="evCondRows.push({ pred: 'money_gte', param: 'kokugo', value: 0 })">
                  ＋条件を追加
                </button>
              </div>
              <label>病気にする
                <select v-model="evForm.disease_set">
                  <option v-for="d in EV_DISEASES" :key="String(d.value)" :value="d.value">{{ d.label }}</option>
                </select>
              </label>
              <label>体重増減(g)<input type="number" v-model.number="evForm.weight_g" /></label>
              <label>抽選の重み<input type="number" v-model.number="evForm.weight" min="1" max="100" /></label>
              <span class="hint">※組み込みイベントは各1。2にすると2倍出やすい</span>
              <label class="chk"><input type="checkbox" v-model="evForm.enabled" /> 有効</label>
              <div class="actions">
                <button class="btn primary" :disabled="busy || !evForm.name || !evForm.message" @click="evSave">
                  {{ evForm.id > 0 ? '更新' : '作成' }}
                </button>
                <button v-if="evForm.id > 0" class="btn danger" :disabled="busy" @click="evDelete">削除</button>
                <button v-if="evForm.id > 0" class="btn" @click="evReset">新規作成に戻る</button>
              </div>
            </section>
            <section class="panel">
              <h3>既存イベント（{{ adminEvents.length }}）<span class="hint"> ※行をクリックで編集</span></h3>
              <div class="table-scroll">
                <table class="list-table">
                  <thead><tr><th>ID</th><th class="l">名前</th><th class="l">メッセージ</th><th>お金</th><th>重み</th><th>有効</th></tr></thead>
                  <tbody>
                    <tr v-for="e in adminEvents" :key="e.id" class="clickable" @click="evEdit(e)">
                      <td>{{ e.id }}</td>
                      <td class="l">{{ e.name }}</td>
                      <td class="l">{{ e.message }}</td>
                      <td class="r">{{ e.money_min === e.money_max ? e.money_min : `${e.money_min}〜${e.money_max}` }}</td>
                      <td class="r">{{ e.weight }}</td>
                      <td :class="{ off: !e.enabled }">{{ e.enabled ? '○' : '×' }}</td>
                    </tr>
                    <tr v-if="!adminEvents.length"><td colspan="6" class="muted">まだカスタムイベントがありません。</td></tr>
                  </tbody>
                </table>
              </div>
            </section>
          </div>
        </section>

        <!-- サーバー設定 -->
        <section class="fold">
          <button class="fold-head" @click="open.settings = !open.settings">
            <span class="caret">{{ open.settings ? '▼' : '▶' }}</span> サーバー設定
          </button>
          <div v-if="open.settings" class="fold-body">
            <section class="panel">
              <h3>ゲーム設定<span class="hint"> ※変更は即時反映(ワーカーは次tickで反映)</span></h3>
              <div v-if="settings" class="settings-grid">
                <label v-for="f in SETTINGS_FIELDS" :key="f.key" class="setting">
                  <span class="setting-label">{{ f.label }}</span>
                  <input type="number" v-model.number="settings[f.key] as number" />
                  <span v-if="f.hint" class="setting-hint">{{ f.hint }}</span>
                </label>
                <label class="setting chk-setting">
                  <span class="setting-label">デバッグ: 間隔ゼロ</span>
                  <span class="chk-line"><input type="checkbox" v-model="settings.debug_no_cooldown" /> 仕事/使用/食事などの間隔制限を無視</span>
                </label>
                <label class="setting chk-setting">
                  <span class="setting-label">街移動: 迷子</span>
                  <span class="chk-line"><input type="checkbox" v-model="settings.move_maigo_enabled" /> 徒歩移動で迷子(ダウンタウンへ)を有効化</span>
                </label>
              </div>
              <div class="actions">
                <button class="btn primary" :disabled="busy || !settings" @click="saveSettings">保存</button>
                <button class="btn" :disabled="busy" @click="refresh">再読込</button>
              </div>
            </section>
          </div>
        </section>

        <!-- 街(名前・地価) -->
        <section class="fold">
          <button class="fold-head" @click="open.towns = !open.towns">
            <span class="caret">{{ open.towns ? '▼' : '▶' }}</span> 街（{{ townDraft.length }}）
          </button>
          <div v-if="open.towns" class="fold-body">
            <section class="panel">
              <h3>
                街の設定<span class="hint">
                  ※上から順に街番号0,1,2…。名前と地価(万円)を編集。地価は建築費に使われる。「隠し」はワープで行けない隠し町。最大{{ 12 }}街。</span
                >
              </h3>
              <table class="town-edit">
                <thead>
                  <tr><th>#</th><th>名前</th><th>地価(万)</th><th>隠し</th><th></th></tr>
                </thead>
                <tbody>
                  <tr v-for="(t, i) in townDraft" :key="i">
                    <td>{{ i }}</td>
                    <td><input v-model="t.name" /></td>
                    <td><input type="number" v-model.number="t.land_price" min="0" /></td>
                    <td class="chk-cell"><input type="checkbox" v-model="t.hidden" title="ワープで行けない隠し町" /></td>
                    <td><button class="btn danger mini" :disabled="townDraft.length <= 1" @click="removeTown(i)">削除</button></td>
                  </tr>
                </tbody>
              </table>
              <div class="actions">
                <button class="btn" :disabled="townDraft.length >= 12" @click="addTown">＋街を追加</button>
                <button class="btn primary" :disabled="busy" @click="saveTowns">保存</button>
                <button class="btn" :disabled="busy" @click="refresh">再読込</button>
              </div>
            </section>
          </div>
        </section>

        <!-- タウンマップ -->
        <section class="fold">
          <button class="fold-head" @click="open.map = !open.map">
            <span class="caret">{{ open.map ? '▼' : '▶' }}</span> タウンマップ（{{ townmap.length }}）
          </button>
          <div v-if="open.map" class="fold-body">
            <!-- レイヤー切替: 機能付き施設層 / 背景アセット層 / 空き地(建設会社)層 -->
            <div class="layer-tabs">
              <button :class="{ active: mapLayer === 'facility' }" @click="mapLayer = 'facility'">
                施設レイヤー（{{ townmap.length }}）
              </button>
              <button :class="{ active: mapLayer === 'asset' }" @click="mapLayer = 'asset'">
                背景レイヤー（{{ assets.length }}）
              </button>
            </div>

            <section v-if="mapLayer === 'facility'" class="panel">
              <h3>
                マップ編集<span class="hint">
                  ※街を選び、施設をドラッグ&ドロップで移動(占有セルへは入れ替え)。クリックで選択→空きセルクリックでも移動可。プリセットはドラッグで配置</span
                >
              </h3>

              <!-- 施設プリセットパレット: 標準施設(削除不可)+保存済みテンプレートをD&Dで配置する -->
              <div class="fac-palette">
                <span class="pal-label">プリセット:</span>
                <span
                  v-for="(p, i) in allFacPresets"
                  :key="i"
                  class="fac-chip"
                  :class="{ std: i < stdFacPresets.length, bulksel: bulkPlace && bulkPresetIdx === i }"
                  draggable="true"
                  :title="`${p.alt}（${p.key}${MOVE_KEYS.includes(p.key) ? '→' + (plotTowns.find((t) => t.no === p.dest)?.name ?? p.dest) : ''}）`"
                  @click="clickPresetChip(i)"
                  @dragstart="onPresetDragStart(i)"
                  @dragend="onDragEnd"
                >
                  <img :src="`/img/svg/${p.img}.svg`" width="20" height="20" alt="" draggable="false" />
                  {{ p.alt }}
                  <button
                    v-if="i >= stdFacPresets.length"
                    class="chip-del"
                    title="プリセットを削除"
                    @click.stop="deletePreset(i - stdFacPresets.length)"
                  >×</button>
                </span>
                <button class="btn mini" @click="presetFormOpen = !presetFormOpen">
                  {{ presetFormOpen ? 'キャンセル' : '＋プリセット追加' }}
                </button>
              </div>
              <label class="chk bulk-toggle">
                <input type="checkbox" v-model="bulkPlace" />
                一括配置モード（プリセットをクリックで選択し、セルをクリックで連続配置）
              </label>
              <div v-if="presetFormOpen" class="preset-form">
                <label>表示名<input v-model="presetDraft.alt" maxlength="40" placeholder="例: 中央デパート" /></label>
                <label>遷移先
                  <select v-model="presetDraft.key">
                    <option v-for="k in KEY_PRESETS" :key="k.key" :value="k.key">{{ k.label }}</option>
                  </select>
                </label>
                <label v-if="MOVE_KEYS.includes(presetDraft.key)">行き先の街
                  <select v-model.number="presetDraft.dest">
                    <option v-for="t in plotTowns" :key="t.no" :value="t.no">{{ t.name }}</option>
                  </select>
                </label>
                <label>画像
                  <select v-model="presetDraft.img">
                    <option v-for="im in IMG_PRESETS" :key="im" :value="im">{{ im }}</option>
                  </select>
                </label>
                <img :src="`/img/svg/${presetDraft.img}.svg`" width="24" height="24" alt="" />
                <button class="btn primary mini" :disabled="busy" @click="savePreset">保存</button>
              </div>
              <div class="plot-towns">
                <button
                  v-for="t in plotTowns"
                  :key="t.no"
                  class="ptab"
                  :class="{ active: facilityTown === t.no }"
                  @click="
                    facilityTown = t.no;
                    selectedIdx = null;
                  "
                >
                  {{ t.name }}
                </button>
              </div>
              <div class="map-editor">
                <div class="map-scroll">
                  <div class="map-grid">
                    <div class="corner"></div>
                    <div v-for="c in mapCols" :key="'h' + c" class="colhead">{{ c }}</div>
                    <template v-for="(r, ri) in mapRows" :key="r">
                      <div class="rowhead">{{ r }}</div>
                      <div
                        v-for="c in mapCols"
                        :key="r + '-' + c"
                        class="cell"
                        :class="{
                          occ: mapFacilityAt(c, ri, facilityTown) >= 0,
                          locked: houseCellAt(c, ri),
                          sel:
                            !houseCellAt(c, ri) &&
                            mapFacilityAt(c, ri, facilityTown) >= 0 &&
                            mapFacilityAt(c, ri, facilityTown) === selectedIdx,
                          movable:
                            !houseCellAt(c, ri) &&
                            mapFacilityAt(c, ri, facilityTown) < 0 &&
                            (selectedIdx !== null || dragging !== null),
                          dragsrc:
                            mapFacilityAt(c, ri, facilityTown) >= 0 &&
                            mapFacilityAt(c, ri, facilityTown) === dragging,
                        }"
                        :title="
                          houseCellAt(c, ri)
                            ? '家が建っているため編集できません'
                            : mapFacilityAt(c, ri, facilityTown) >= 0
                              ? townmap[mapFacilityAt(c, ri, facilityTown)].alt
                              : ''
                        "
                        @click="clickCell(c, ri)"
                        @dragover.prevent
                        @drop="onDrop(c, ri)"
                      >
                        <img
                          v-for="(im, li) in assetImgsForTown(c, ri, facilityTown)"
                          :key="'br' + li"
                          class="bg-ref"
                          :src="assetUrl(im)"
                          alt=""
                          draggable="false"
                        />
                        <img
                          v-if="mapFacilityAt(c, ri, facilityTown) >= 0"
                          class="fac-icon"
                          :src="`/img/svg/${townmap[mapFacilityAt(c, ri, facilityTown)].img}.svg`"
                          width="24"
                          height="24"
                          :alt="townmap[mapFacilityAt(c, ri, facilityTown)].alt"
                          :draggable="!houseCellAt(c, ri)"
                          @dragstart="onDragStart(mapFacilityAt(c, ri, facilityTown))"
                          @dragend="onDragEnd"
                        />
                        <span v-if="houseCellAt(c, ri)" class="lock-badge" title="家が建っているため編集できません">🔒</span>
                      </div>
                    </template>
                  </div>
                </div>

                <div class="map-side">
                  <div v-if="selectedFacility" class="sel-panel">
                    <div class="ops-head">選択中の施設</div>
                    <label>表示名<input v-model="selectedFacility.alt" /></label>
                    <label>遷移先
                      <select v-model="selectedFacility.key">
                        <option v-for="k in KEY_PRESETS" :key="k.key" :value="k.key">{{ k.label }}</option>
                      </select>
                    </label>
                    <label v-if="MOVE_KEYS.includes(selectedFacility.key)">行き先の街
                      <select v-model.number="selectedFacility.dest">
                        <option v-for="t in plotTowns" :key="t.no" :value="t.no">{{ t.name }}</option>
                      </select>
                    </label>
                    <label>画像
                      <select v-model="selectedFacility.img">
                        <option v-for="im in IMG_PRESETS" :key="im" :value="im">{{ im }}</option>
                      </select>
                    </label>
                    <label class="chk"><input type="checkbox" v-model="selectedFacility.ready" /> 有効（オフで準備中=クリック不可）</label>
                    <div class="sel-prev">
                      位置: {{ mapRows[selectedFacility.row] }}{{ selectedFacility.col }}
                      <img :src="`/img/svg/${selectedFacility.img}.svg`" width="28" height="28" alt="" />
                    </div>
                    <button class="btn danger mini" @click="deleteFacility">この施設を削除</button>
                  </div>
                  <div v-else class="sel-empty muted">
                    施設をクリックすると編集できます。<br />
                    「＋施設を追加」で新しい施設を配置できます。
                  </div>
                </div>
              </div>
              <div class="actions">
                <button class="btn" @click="addFacility">＋施設を追加</button>
                <button class="btn primary" :disabled="busy" @click="saveTownMap">保存</button>
                <button class="btn" :disabled="busy" @click="refresh">再読込</button>
              </div>
            </section>

            <!-- 背景アセット配置レイヤー -->
            <section v-else-if="mapLayer === 'asset'" class="panel">
              <h3>
                背景アセット配置<span class="hint">
                  ※街を選び、パレットで素材を選んでマスをクリックで連続配置（常に一括配置）。1マスに最大{{ MAX_BG_LAYERS }}層まで重ね置き可（後から置いたものが上）。最上層と同じ素材を再クリック、または消しゴムで最上層を除去。施設は右下に薄く参照表示（編集不可）</span
                >
              </h3>
              <div class="plot-towns">
                <button
                  v-for="t in plotTowns"
                  :key="t.no"
                  class="ptab"
                  :class="{ active: assetTown === t.no }"
                  @click="assetTown = t.no"
                >
                  {{ t.name }}
                </button>
              </div>
              <div class="bg-palette">
                <div class="bg-swatch-wrap">
                  <button
                    :class="['bg-swatch', 'eraser', { active: assetBrush === BG_ERASER }]"
                    title="消しゴム（クリックで最上層を除去）"
                    @click="assetBrush = BG_ERASER"
                  >
                    消
                  </button>
                </div>
                <div v-for="a in bgPalette" :key="a" class="bg-swatch-wrap">
                  <button
                    :class="['bg-swatch', { active: assetBrush === a }]"
                    :title="a"
                    draggable="true"
                    @click="assetBrush = a"
                    @dragstart="onBgPaletteDragStart(a)"
                    @dragend="onBgDragEnd"
                  >
                    <img :src="assetUrl(a)" width="24" height="24" :alt="a" draggable="false" />
                  </button>
                  <button
                    v-if="a.startsWith('u:')"
                    class="bg-del"
                    title="このアップロード画像を削除"
                    :disabled="busy"
                    @click="deleteUploadedAsset(a)"
                  >
                    ×
                  </button>
                </div>
                <label class="bg-upload" title="背景アセットを画像から追加">
                  ＋画像を追加
                  <input type="file" accept="image/png,image/gif,image/jpeg,image/webp" :disabled="busy" @change="onUploadAsset" />
                </label>
              </div>
              <div class="map-scroll">
                <div class="map-grid">
                  <div class="corner"></div>
                  <div v-for="c in mapCols" :key="'ah' + c" class="colhead">{{ c }}</div>
                  <template v-for="(r, ri) in mapRows" :key="'ar' + r">
                    <div class="rowhead">{{ r }}</div>
                    <div
                      v-for="c in mapCols"
                      :key="'a' + r + '-' + c"
                      class="cell bgcell"
                      :class="{
                        occ: assetImgsAt(c, ri).length > 0,
                        dragsrc: bgDrag?.kind === 'tile' && bgDrag.col === c && bgDrag.row === ri,
                      }"
                      :title="`${r}${c}${assetImgsAt(c, ri).length ? ' : ' + assetImgsAt(c, ri).join(' / ') : ''}`"
                      @click="paintAsset(c, ri)"
                      @dragover.prevent
                      @drop="onBgDrop(c, ri)"
                    >
                      <img
                        v-for="(im, li) in assetImgsAt(c, ri)"
                        :key="'t' + li"
                        class="bg-tile"
                        :src="assetUrl(im)"
                        :alt="im"
                        :draggable="li === assetImgsAt(c, ri).length - 1"
                        @dragstart="li === assetImgsAt(c, ri).length - 1 ? onBgTileDragStart(c, ri) : undefined"
                        @dragend="onBgDragEnd"
                      />
                      <span v-if="assetImgsAt(c, ri).length > 1" class="layer-badge">{{ assetImgsAt(c, ri).length }}</span>
                      <img
                        v-if="mapFacilityAt(c, ri, assetTown) >= 0"
                        class="fac-ref"
                        :src="`/img/svg/${townmap[mapFacilityAt(c, ri, assetTown)].img}.svg`"
                        alt=""
                        draggable="false"
                      />
                    </div>
                  </template>
                </div>
              </div>
              <div class="actions">
                <button class="btn primary" :disabled="busy" @click="saveTownAssets">保存</button>
                <button class="btn" :disabled="busy" @click="refresh">再読込</button>
              </div>
            </section>

          </div>
        </section>
      </div>
    </template>

    <!-- アイテム編集(詳細)モーダル -->
    <div v-if="editing" class="modal-overlay" @click.self="closeEdit">
      <div class="modal">
        <h3>アイテム編集（ID {{ editing.id }}）</h3>
        <label>品名<input v-model="editing.name" /></label>
        <label>カテゴリ<input v-model="editing.category" /></label>
        <label>値段<input type="number" v-model.number="editing.price" /></label>
        <label>標準在庫数<input type="number" v-model.number="editing.stock_master" placeholder="空=無制限" /></label>
        <label class="chk"><input type="checkbox" v-model="editing.enabled" /> 有効（オフで無効化）</label>
        <div class="ops">
          <div class="ops-head">使用効果</div>
          <div v-for="(op, i) in editing.effect" :key="i" class="op-row">
            <select v-model="op.op">
              <option value="add_param">パラメータ</option>
              <option value="add_money">お金</option>
            </select>
            <select v-if="op.op === 'add_param'" v-model="op.param">
              <option v-for="p in PARAM_OPTIONS" :key="p" :value="p">{{ PARAM_FULL[p] ?? p }}</option>
            </select>
            <input type="number" v-model.number="op.amount" />
            <button class="btn mini" @click="editing.effect.splice(i, 1)">×</button>
          </div>
          <button class="btn mini" @click="addOp(editing.effect)">＋効果を追加</button>
        </div>
        <div class="actions">
          <button class="btn primary" :disabled="busy" @click="saveEdit">保存</button>
          <button class="btn danger" :disabled="busy" @click="deleteEdit">削除</button>
          <button class="btn" :disabled="busy" @click="closeEdit">キャンセル</button>
        </div>
      </div>
    </div>

    <!-- 職業編集(詳細)モーダル -->
    <div v-if="editingJob" class="modal-overlay" @click.self="closeEditJob">
      <div class="modal">
        <h3>職業編集（ID {{ editingJob.id }}）</h3>
        <label>職業名<input v-model="editingJob.name" /></label>
        <label class="chk"><input type="checkbox" v-model="editingJob.enabled" /> 有効（オフで無効化）</label>
        <div class="econ-grid">
          <label>給料<input type="number" v-model.number="editingJob.salary" /></label>
          <label>支払間隔<input type="number" v-model.number="editingJob.pay_interval" /></label>
          <label>ボーナス%<input type="number" v-model.number="editingJob.bonus_rate" /></label>
          <label>昇給%<input type="number" v-model.number="editingJob.raise_rate" /></label>
          <label>ランク<input type="number" v-model.number="editingJob.rank" /></label>
          <label>身体消費<input type="number" v-model.number="editingJob.body_cost" /></label>
          <label>頭脳消費<input type="number" v-model.number="editingJob.nou_cost" /></label>
          <label class="wide2">前提マスター職<input v-model="editingJob.require_master" placeholder="なし" /></label>
        </div>
        <div class="ops">
          <div class="ops-head">就くための必要条件(以上)</div>
          <div v-for="(req, i) in editingJob.requirements" :key="i" class="op-row">
            <select v-model="req.param">
              <option v-for="p in PARAM_OPTIONS" :key="p" :value="p">{{ PARAM_FULL[p] ?? p }}</option>
            </select>
            <span class="ge">≧</span>
            <input type="number" v-model.number="req.value" />
            <button class="btn mini" @click="editingJob.requirements.splice(i, 1)">×</button>
          </div>
          <button class="btn mini" @click="addReq(editingJob.requirements)">＋条件を追加</button>
        </div>
        <div class="ops">
          <div class="ops-head">働いたときの効果</div>
          <div v-for="(op, i) in editingJob.effect" :key="i" class="op-row">
            <select v-model="op.op">
              <option value="add_param">パラメータ</option>
              <option value="add_money">お金</option>
            </select>
            <select v-if="op.op === 'add_param'" v-model="op.param">
              <option v-for="p in PARAM_OPTIONS" :key="p" :value="p">{{ PARAM_FULL[p] ?? p }}</option>
            </select>
            <input type="number" v-model.number="op.amount" />
            <button class="btn mini" @click="editingJob.effect.splice(i, 1)">×</button>
          </div>
          <button class="btn mini" @click="addOp(editingJob.effect)">＋効果を追加</button>
        </div>
        <div class="actions">
          <button class="btn primary" :disabled="busy" @click="saveJob">保存</button>
          <button class="btn danger" :disabled="busy" @click="deleteJob">削除</button>
          <button class="btn" :disabled="busy" @click="closeEditJob">キャンセル</button>
        </div>
      </div>
    </div>

    <!-- ユーザー編集(詳細)モーダル -->
    <div v-if="editingPlayer" class="modal-overlay" @click.self="closeEditPlayer">
      <div class="modal wide-modal">
        <h3>ユーザー編集（ID {{ editingPlayer.id }}）</h3>
        <label>名前<input v-model="editingPlayer.display_name" /></label>
        <label class="chk"><input type="checkbox" v-model="editingPlayer.is_admin" /> 管理者権限</label>
        <div class="econ-grid">
          <label>所持金<input type="number" v-model.number="editingPlayer.money" /></label>
          <label>職業
            <select v-model="editingPlayer.job">
              <option v-for="name in jobOptions" :key="name" :value="name">{{ name }}</option>
            </select>
          </label>
          <label>職Lv<input type="number" v-model.number="editingPlayer.job_level" /></label>
          <label>職経験値<input type="number" v-model.number="editingPlayer.job_exp" /></label>
          <label>身体P<input type="number" v-model.number="editingPlayer.energy" /></label>
          <label>頭脳P<input type="number" v-model.number="editingPlayer.nou_energy" /></label>
          <label>満腹度<input type="number" v-model.number="editingPlayer.satiety" /></label>
          <label>病気指数<input type="number" v-model.number="editingPlayer.disease_index" /></label>
          <label>身長cm<input type="number" v-model.number="editingPlayer.height_cm" /></label>
          <label>体重g<input type="number" v-model.number="editingPlayer.weight_g" /></label>
        </div>
        <div class="ops">
          <div class="ops-head">パラメータ</div>
          <div class="param-edit">
            <label v-for="p in DETAIL_PARAMS" :key="p.key">
              <span>{{ p.label }}</span>
              <input type="number" v-model.number="editingPlayer.params[p.key]" />
            </label>
          </div>
        </div>
        <div class="actions">
          <button class="btn primary" :disabled="busy" @click="savePlayer">保存</button>
          <button class="btn danger" :disabled="busy" @click="deletePlayer">論理削除</button>
          <button class="btn" :disabled="busy" @click="closeEditPlayer">キャンセル</button>
        </div>
      </div>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.admin-page {
  background-color: #dfe6ee;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.admin-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.admin-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  color: #333;
}
.admin-header .title {
  flex: 0 0 130px;
  background: #445566;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.admin-sections {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.fold {
  border: 1px solid #99a;
  background: #fff;
}
.fold-head {
  width: 100%;
  text-align: left;
  background: #445566;
  color: #fff;
  border: 0;
  padding: 8px 12px;
  font-size: 14px;
  font-weight: bold;
  cursor: pointer;
}
.fold-head:hover {
  background: #33475a;
}
.fold-head .caret {
  display: inline-block;
  width: 14px;
  color: #cde;
}
.fold-body {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: flex-start;
  padding: 8px;
}
.fold-body .panel {
  flex: 1 1 320px;
  border: 1px solid #ccd;
}
.sim-box {
  margin-top: 8px;
  border: 1px solid #e0e4ea;
  padding: 6px;
  font-size: 12px;
  line-height: 1.6;
}
.sim-box .muted {
  color: #999;
}
.panel {
  flex: 1 1 300px;
  background: #fff;
  border: 1px solid #99a;
  padding: 10px 12px;
}
.panel.wide {
  flex: 1 1 100%;
}
.panel h3 {
  margin: 0 0 8px;
  font-size: 14px;
  color: #334;
  border-bottom: 1px solid #dde;
  padding-bottom: 4px;
}
.panel label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  margin-bottom: 6px;
  color: #445;
}
.panel label input {
  flex: 1 1 auto;
  padding: 2px 4px;
}
.econ-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 2px 8px;
  margin: 4px 0 6px;
}
.econ-grid label {
  margin-bottom: 0;
}
.econ-grid label input[type='number'] {
  width: 70px;
}
.econ-grid .wide2 {
  grid-column: span 2;
}
.settings-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px 12px;
  margin: 4px 0 8px;
}
.settings-grid .setting {
  display: flex;
  flex-direction: column;
  margin-bottom: 0;
}
.settings-grid .setting-label {
  font-size: 12px;
  color: #334;
  font-weight: bold;
}
.settings-grid .setting input[type='number'] {
  width: 110px;
}
.settings-grid .setting-hint {
  font-size: 10px;
  color: #889;
  margin-top: 1px;
}
.settings-grid .chk-setting {
  grid-column: span 2;
}
.settings-grid .chk-line {
  font-size: 12px;
  color: #445;
  display: flex;
  align-items: center;
  gap: 4px;
}
/* マップを上、選択パネルを下に縦積み。マップは固定サイズのセルで確実に描画し、
   万一パネルより広い場合のみ横スクロール(はみ出しは防ぐ)。 */
.map-editor {
  display: block;
}
.map-scroll {
  overflow-x: auto;
  margin-bottom: 8px;
}
.map-grid {
  display: grid;
  grid-template-columns: 18px repeat(16, 24px);
  grid-auto-rows: 24px;
  gap: 1px;
  background: #c8cfd8;
  border: 1px solid #c8cfd8;
  width: max-content;
}
.map-grid .corner,
.map-grid .colhead,
.map-grid .rowhead {
  /* style.cssのグローバル .colhead/.rowhead(旧マップ用: width 32px/14px)が
     漏れて列がずれるため、明示的にトラック幅へリセットする。 */
  width: auto;
  height: auto;
  background: #e7ecf2;
  font-size: 9px;
  color: #667;
  display: flex;
  align-items: center;
  justify-content: center;
}
.map-grid .cell {
  background: #eef2f7;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  position: relative;
}
/* 施設レイヤーで背景アセットを薄く参照表示(施設アイコンは前面)。 */
.map-grid .cell .bg-ref {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  opacity: 0.4;
  pointer-events: none;
}
.map-grid .cell .fac-icon {
  position: relative;
  z-index: 1;
}
/* 家が建っているマスは編集不可(赤系背景+錠前)。 */
.map-grid .cell.locked {
  background: #f2dede;
  cursor: not-allowed;
}
.map-grid .cell .lock-badge {
  position: absolute;
  right: 0;
  bottom: 0;
  font-size: 9px;
  line-height: 1;
  z-index: 2;
  pointer-events: none;
}
.map-grid .cell.occ {
  background: #fff;
}
.map-grid .cell.plot {
  background: #cdeeb0;
}
.plot-towns {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 6px;
}
.plot-towns .ptab {
  background: #eef3e8;
  border: 1px solid #99a;
  padding: 3px 8px;
  font-size: 12px;
  cursor: pointer;
}
.plot-towns .ptab.active {
  background: #4a7a2a;
  color: #fff;
  font-weight: bold;
}
/* 施設プリセットパレット(D&Dで配置) */
.fac-palette {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  margin: 6px 0;
  font-size: 12px;
}
.fac-palette .pal-label {
  color: #556;
  font-weight: bold;
}
.fac-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: #fff;
  border: 1px solid #9ab;
  border-radius: 5px;
  padding: 2px 4px 2px 6px;
  cursor: grab;
}
/* 一括配置モードで選択中のプリセット。 */
.fac-chip.bulksel {
  outline: 2px solid #e67e22;
  background: #fff3e0;
}
.bulk-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  width: fit-content;
  margin: 4px 0 8px;
  font-size: 12px;
  color: #555;
  cursor: pointer;
  font-size: 12px;
}
.fac-chip:active {
  cursor: grabbing;
}
/* 標準施設(組み込み・削除不可)は淡色で区別 */
.fac-chip.std {
  background: #f2f6fa;
  border-color: #b8c4d0;
}
.chip-del {
  border: 0;
  background: none;
  color: #c66;
  cursor: pointer;
  font-weight: bold;
  padding: 0 2px;
}
.preset-form {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  background: #f4f7f0;
  border: 1px solid #ccd;
  padding: 6px 8px;
  margin-bottom: 6px;
  font-size: 12px;
}
.preset-form label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.map-grid .cell.movable {
  background: #e3f0ff;
}
.map-grid .cell.movable:hover {
  background: #cfe4ff;
}
.map-grid .cell.sel {
  outline: 2px solid #ff6600;
  outline-offset: -2px;
  background: #fff3e8;
  z-index: 1;
}
/* ドラッグ中の元セルは半透明にする。 */
.map-grid .cell.dragsrc {
  opacity: 0.4;
}
.map-grid .cell img {
  cursor: grab;
  display: block;
  width: 20px;
  height: 20px;
}
/* レイヤー切替タブ */
.layer-tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 8px;
  /* fold-body(flex-wrap)の中で全幅を占め、マップ編集パネルを下段(タブの下)に送る。 */
  flex-basis: 100%;
}
.town-edit {
  border-collapse: collapse;
  margin-bottom: 8px;
}
.town-edit th,
.town-edit td {
  border: 1px solid #dfe3ea;
  padding: 3px 6px;
  font-size: 13px;
}
.town-edit input {
  font-size: 13px;
  padding: 2px 4px;
}
.town-edit input[type='number'] {
  width: 80px;
}
.layer-tabs button {
  background: #e2e8f0;
  border: 1px solid #99a;
  padding: 5px 12px;
  font-size: 13px;
  cursor: pointer;
}
.layer-tabs button.active {
  background: #336699;
  color: #fff;
  font-weight: bold;
}
/* 背景アセットのパレット */
.bg-palette {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 8px;
}
.bg-swatch {
  border: 2px solid #ccc;
  background: #fff;
  padding: 2px;
  line-height: 0;
  cursor: pointer;
}
.bg-swatch.active {
  border-color: #ff6600;
}
/* 消しゴムスウォッチ(最上層を除去する特殊筆)。 */
.bg-swatch.eraser {
  width: 32px;
  height: 32px;
  line-height: 1;
  font-size: 12px;
  color: #a33;
  font-weight: bold;
}
/* 2層以上重なっているマスの層数バッジ。 */
.map-grid .cell.bgcell .layer-badge {
  position: absolute;
  left: 1px;
  top: 1px;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  font-size: 9px;
  line-height: 1;
  padding: 1px 3px;
  border-radius: 3px;
  pointer-events: none;
}
.bg-swatch img {
  display: block;
  width: 24px;
  height: 24px;
  object-fit: cover;
}
/* アップロード画像のスウォッチ + 削除ボタン。 */
.bg-swatch-wrap {
  position: relative;
  line-height: 0;
}
.bg-del {
  position: absolute;
  top: -6px;
  right: -6px;
  width: 15px;
  height: 15px;
  padding: 0;
  border-radius: 50%;
  border: 1px solid #b33;
  background: #cc3333;
  color: #fff;
  font-size: 11px;
  line-height: 13px;
  cursor: pointer;
}
.bg-del:disabled {
  opacity: 0.5;
}
/* 背景アセットのアップロードボタン(パレット末尾)。 */
.bg-upload {
  display: inline-flex;
  align-items: center;
  border: 1px dashed #99a;
  background: #eef2f7;
  padding: 4px 8px;
  font-size: 11px;
  color: #445;
  cursor: pointer;
}
.bg-upload input {
  display: none;
}
/* 背景エディタのセル: タイルを全面に敷き、施設は右下に薄く参照表示する。 */
.map-grid .cell.bgcell {
  position: relative;
}
.map-grid .cell.bgcell .bg-tile {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  cursor: grab;
}
.map-grid .cell.bgcell .fac-ref {
  position: absolute;
  right: 0;
  bottom: 0;
  width: 12px;
  height: 12px;
  opacity: 0.55;
  cursor: pointer;
}
.map-side {
  width: 100%;
  max-width: 460px;
}
.sel-panel {
  border: 1px solid #e0e4ea;
  padding: 8px;
  background: #fafbfc;
}
.sel-panel label {
  display: block;
  margin-bottom: 4px;
  font-size: 12px;
}
.sel-panel label input[type='text'],
.sel-panel label input:not([type]),
.sel-panel label select {
  width: 100%;
  box-sizing: border-box;
}
.sel-prev {
  font-size: 12px;
  color: #445;
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 6px 0;
}
.sel-empty {
  border: 1px dashed #d0d5dd;
  padding: 10px;
  font-size: 12px;
  line-height: 1.6;
}
.ops {
  border: 1px solid #e0e4ea;
  padding: 6px;
  margin: 6px 0;
}
.ops-head {
  font-size: 11px;
  color: #667;
  margin-bottom: 4px;
}
.op-row {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-bottom: 4px;
}
.op-row select,
.op-row input {
  padding: 1px 3px;
  font-size: 12px;
}
.op-row input[type='number'] {
  width: 70px;
}
.ge {
  color: #667;
}
.actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
.btn.mini {
  padding: 1px 6px;
  font-size: 11px;
}
.btn.primary {
  background: #336699;
  color: #fff;
  border-color: #224466;
}
.sim {
  font-size: 13px;
  line-height: 1.7;
}
.sim .muted {
  color: #999;
}
.warn {
  color: #cc5500;
  font-size: 12px;
  margin-top: 4px;
}
.table-scroll {
  overflow-x: auto;
  max-height: 240px;
  overflow-y: auto;
  margin-bottom: 10px;
}
.list-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.list-table th {
  background: #e2e8f0;
  color: #234;
  padding: 2px 6px;
  border: 1px solid #cdd;
  position: sticky;
  top: 0;
}
.list-table td {
  padding: 2px 6px;
  border: 1px solid #eee;
  text-align: center;
}
.list-table th.l,
.list-table td.l {
  text-align: left;
}
.list-table td.r {
  text-align: right;
}
.list-table tr.clickable {
  cursor: pointer;
}
.list-table tr.clickable:hover td {
  background: #eef4fb;
}
.list-table td.off {
  color: #cc3300;
}
.hint {
  font-size: 11px;
  color: #889;
  font-weight: normal;
}
.chk {
  font-size: 12px;
}
.btn.danger {
  background: #cc3333;
  color: #fff;
  border-color: #992222;
}
/* 編集モーダル */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.4);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}
.modal {
  background: #fff;
  border: 1px solid #667;
  border-radius: 4px;
  padding: 14px 16px;
  width: 380px;
  max-width: 92vw;
  max-height: 88vh;
  overflow-y: auto;
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.3);
}
.modal.wide-modal {
  width: 460px;
}
.modal h3 {
  margin: 0 0 10px;
  font-size: 14px;
  color: #334;
  border-bottom: 1px solid #dde;
  padding-bottom: 5px;
}
.param-edit {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 2px 8px;
}
.param-edit label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  font-size: 12px;
  margin: 0;
}
.param-edit label input {
  width: 70px;
}
</style>
