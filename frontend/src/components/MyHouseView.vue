<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import {
  api,
  type Player,
  type BuildingState,
  type MyHouse,
  type OrosiState,
  type OrosiItem,
  type ShopStockView,
  type ShopStockItem,
} from '../api';
import Toast from './Toast.vue';
import ExteriorPicker from './ExteriorPicker.vue';
import { useToast } from '../toast';

// 家の設定(レガシー original_house.cgi my_house_settei)。コマンドバーの
// 「家の設定」から開く。店設定・基本設定(コメント)・コンテンツ選択・
// 建て替え・売却を1画面に集約する(レガシーの画面構成順)。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const busy = ref(false);
const message = ref('');
const { toast, showToast, closeToast } = useToast();

const state = ref<BuildingState | null>(null);
const selectedHouseId = ref<number | null>(null);
const house = computed<MyHouse | null>(
  () => state.value?.my_houses.find((h) => h.id === selectedHouseId.value) ?? null,
);
const townName = (no: number) => state.value?.towns.find((t) => t.no === no)?.name ?? '';
const rowLabel = (row: number) => String.fromCharCode(65 + row);

async function refresh() {
  state.value = await api.building(props.player.id);
  if (selectedHouseId.value === null && state.value.my_houses.length > 0) {
    selectedHouseId.value = state.value.my_houses[0].id;
  }
  syncDrafts();
}
onMounted(async () => {
  try {
    await refresh();
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  }
});

// --- 基本設定: マウスオーバーコメント(40字) ---
const commentDraft = ref('');
// --- コンテンツ選択(枠数=内装ランク) ---
const CONTENT_KINDS = [
  { value: '', label: '公開しない' },
  { value: 'bbs', label: '通常の掲示板' },
  { value: 'shop', label: 'お店' },
  { value: 'url', label: '独自URL' },
  { value: 'nushi', label: '家主のみ書ける掲示板' },
];
const contentDraft = ref<{ kind: string; title: string; url: string; comment: string }[]>([]);
// --- 店設定 ---
const shopDraft = ref<{ title: string; syubetu: string; markup: number }>({
  title: '',
  syubetu: '',
  markup: 2,
});

function syncDrafts() {
  const h = house.value;
  if (!h) return;
  commentDraft.value = h.setumei ?? '';
  const rows: { kind: string; title: string; url: string; comment: string }[] = [];
  for (let s = 0; s < h.slots; s++) {
    const c = h.contents.find((x) => x.slot === s);
    rows.push({ kind: c?.kind ?? '', title: c?.title ?? '', url: c?.url ?? '', comment: c?.comment ?? '' });
  }
  contentDraft.value = rows;
  shopDraft.value = {
    title: h.shop_title || '',
    syubetu: h.shop_kind || state.value?.shop_kinds[0] || '',
    markup: h.shop_markup || 2,
  };
  // 建て替えドラフトも現状に合わせる。
  rebuildExterior.value = h.exterior;
  rebuildInterior.value = h.interior_rank;
  closeOrosi();
  closePrice();
}
function selectHouse(h: MyHouse) {
  selectedHouseId.value = h.id;
  syncDrafts();
}

// 追加種別(2軒目以降)の表示名。
const TUIKA_NAMES: Record<number, string> = { 1: '運営', 2: '株式会社', 3: '持ち物販売店' };

// 店設定フォームを出すか: コンテンツ枠に「お店」があるか、既に店がある家。
const shopConfigured = computed(
  () => (house.value?.contents.some((c) => c.kind === 'shop') ?? false) || (house.value?.has_shop ?? false),
);
// 家主掲示板の投稿フォームを出すか(レガシーは家の設定側から投稿する)。
const nushiConfigured = computed(() => house.value?.contents.some((c) => c.kind === 'nushi') ?? false);
const nushiTitle = ref('');
const nushiBody = ref('');
async function postNushi() {
  const h = house.value;
  if (!h || !nushiBody.value.trim()) return;
  await run(async () => {
    const after = await api.postBbs(props.player.id, h.id, 'nushi', nushiBody.value, nushiTitle.value);
    emit('update', after);
    nushiTitle.value = '';
    nushiBody.value = '';
    showToast({ variant: 'item', title: '投稿しました。', lines: [], icon: 'item' });
  });
}

async function run(fn: () => Promise<void>) {
  busy.value = true;
  try {
    await fn();
  } catch (e) {
    showToast({
      variant: 'error',
      title: 'エラー',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}

async function saveComment() {
  const h = house.value;
  if (!h) return;
  await run(async () => {
    const after = await api.setHouseComment(props.player.id, h.id, commentDraft.value);
    emit('update', after);
    await refresh();
    showToast({ variant: 'item', title: 'コメントを保存しました', lines: [], icon: 'item' });
  });
}

async function saveContents() {
  const h = house.value;
  if (!h) return;
  await run(async () => {
    const contents = contentDraft.value.map((r, s) => ({ slot: s, kind: r.kind, title: r.title, url: r.url, comment: r.comment }));
    const after = await api.setHouseContents(props.player.id, h.id, contents);
    emit('update', after);
    await refresh();
    showToast({ variant: 'item', title: 'コンテンツを保存しました', lines: [], icon: 'item' });
  });
}

async function saveShop() {
  const h = house.value;
  if (!h) return;
  await run(async () => {
    const after = await api.openHouseShop(
      props.player.id,
      h.id,
      shopDraft.value.title,
      shopDraft.value.syubetu,
      shopDraft.value.markup,
    );
    emit('update', after);
    await refresh();
    showToast({
      variant: 'item',
      title: '店を設定しました',
      lines: [`${shopDraft.value.syubetu}の店（掛け率${shopDraft.value.markup}倍）`],
      icon: 'item',
    });
  });
}

// --- 卸問屋(仕入れ) ---
const orosiState = ref<OrosiState | null>(null);
const shiireQty = ref<Record<number, number>>({});
async function openOrosi() {
  const h = house.value;
  if (!h) return;
  await run(async () => {
    orosiState.value = await api.orosi(props.player.id, h.id);
  });
}
function closeOrosi() {
  orosiState.value = null;
}
async function doShiire(it: OrosiItem) {
  const h = house.value;
  if (!h) return;
  const qty = shiireQty.value[it.item_id] || 1;
  await run(async () => {
    const after = await api.shiire(props.player.id, h.id, it.item_id, qty);
    emit('update', after);
    orosiState.value = await api.orosi(props.player.id, h.id);
    showToast({ variant: 'item', title: '仕入れました', lines: [`${it.name} を${qty}個`], icon: 'item' });
  });
}

// --- 商品リスト・個別価格設定(my_syouhin) ---
const priceStock = ref<ShopStockView | null>(null);
const priceDraft = ref<Record<number, number>>({});
async function openPrice() {
  const h = house.value;
  if (!h) return;
  await run(async () => {
    priceStock.value = await api.houseShopStock(props.player.id, h.id);
    const d: Record<number, number> = {};
    for (const it of priceStock.value.items) d[it.item_id] = it.shelf;
    priceDraft.value = d;
  });
}
function closePrice() {
  priceStock.value = null;
}
async function savePrice(it: ShopStockItem) {
  const h = house.value;
  if (!h) return;
  const price = priceDraft.value[it.item_id] ?? it.shelf;
  await run(async () => {
    const after = await api.setHouseShopPrice(props.player.id, h.id, it.item_id, price);
    emit('update', after);
    priceStock.value = await api.houseShopStock(props.player.id, h.id);
    showToast({ variant: 'item', title: '価格を設定しました', lines: [`${it.name}: ${yen(price)}円`], icon: 'item' });
  });
}

// --- 家の外観、内装の変更(建て替え) ---
const rebuildOpen = ref(false);
const rebuildExterior = ref('');
const rebuildInterior = ref(0);
const rebuildCost = computed(() => {
  const ext = state.value?.exteriors.find((e) => e.key === rebuildExterior.value);
  const inte = state.value?.interiors.find((i) => i.rank === rebuildInterior.value);
  if (!ext || !inte) return 0;
  return ext.price * inte.multiplier * 10000;
});
async function doRebuild() {
  const h = house.value;
  if (!h) return;
  await run(async () => {
    const after = await api.rebuildHouse(props.player.id, h.id, rebuildExterior.value, rebuildInterior.value);
    emit('update', after);
    await refresh();
    rebuildOpen.value = false;
    showToast({ variant: 'item', title: '家を建て替えた', lines: [`費用 ${yen(rebuildCost.value)}円(現金)`], icon: 'item' });
  });
}

// --- 家の売却 ---
const sellRefund = computed(() => {
  const h = house.value;
  if (!h) return 0;
  const t = state.value?.towns.find((x) => x.no === h.town);
  return t ? t.land_price * 10000 : 0;
});
async function doSell() {
  const h = house.value;
  if (!h) return;
  const ok = window.confirm(
    `${townName(h.town)}／${rowLabel(h.row)}${h.col}の家を売却しますか？\n地価分 ${yen(sellRefund.value)}円が現金で戻ります(外装・内装費は戻りません)。`,
  );
  if (!ok) return;
  await run(async () => {
    const after = await api.sellHouse(props.player.id, h.id);
    emit('update', after);
    selectedHouseId.value = null;
    await refresh();
    showToast({ variant: 'item', title: '家を売却しました', lines: [], icon: 'item' });
  });
}
</script>

<template>
  <div class="myhouse-page facility-page">
    <Toast :toast="toast" @close="closeToast" />
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <!-- タイトルバー(レガシー: 自分の家設定) -->
    <div class="settei-title">自分の家設定</div>

    <div v-if="message" class="err">{{ message }}</div>

    <div v-if="state && state.my_houses.length === 0" class="panel-white">
      まだ家を持っていません。建設会社で家を建てると、ここで設定できます。
    </div>

    <template v-if="house">
      <!-- 家が複数あるときの切替 -->
      <div v-if="state && state.my_houses.length > 1" class="house-tabs">
        <button
          v-for="h in state.my_houses"
          :key="h.id"
          class="tab"
          :class="{ active: h.id === selectedHouseId }"
          @click="selectHouse(h)"
        >
          {{ townName(h.town) }}／{{ rowLabel(h.row) }}{{ h.col }}
        </button>
      </div>

      <div class="house-summary panel-white">
        <img :src="`/img/svg/${house.exterior}.svg`" :alt="house.exterior" />
        <div>
          <div class="hs-loc">{{ townName(house.town) }}／{{ rowLabel(house.row) }}{{ house.col }}</div>
          <div v-if="house.tuika !== 0" class="hs-sub">外装 {{ house.exterior }}・種別 {{ TUIKA_NAMES[house.tuika] ?? '?' }}</div>
          <div v-else class="hs-sub">外装 {{ house.exterior }}・内装{{ ['A','B','C','D'][house.interior_rank] ?? '?' }}ランク（コンテンツ枠{{ house.slots }}）</div>
        </div>
      </div>

      <!-- 公開中コンテンツの詳細設定: お店(レガシー omise_settei相当) -->
      <div v-if="shopConfigured" class="panel-white sec">
        <div class="sec-head">■お店の設定</div>
        <div class="row-line">
          <label class="fld">店名<input v-model="shopDraft.title" maxlength="50" class="inp" /></label>
          <label class="fld">種類
            <select v-model="shopDraft.syubetu">
              <option v-for="k in state!.shop_kinds" :key="k" :value="k">{{ k }}</option>
            </select>
          </label>
          <label class="fld">販売掛け率
            <input v-model.number="shopDraft.markup" type="number" step="0.1" min="0.3" max="3" class="inp-num" />倍
          </label>
          <button class="btn mini primary-btn" :disabled="busy" @click="saveShop">
            {{ house.has_shop ? '店設定を保存' : '店を開く' }}
          </button>
        </div>
        <div class="note">掛け率は0.3超〜3倍まで。スーパーは全種類を扱えますが仕入れ値が1.5倍になります。種類を変えると在庫は消えます。</div>
        <div v-if="house.has_shop" class="row-line">
          <button class="btn mini" :disabled="busy" @click="openOrosi">卸問屋で仕入れる</button>
          <button class="btn mini" :disabled="busy" @click="openPrice">商品リスト・価格設定</button>
        </div>

        <!-- 卸問屋 -->
        <div v-if="orosiState" class="sub-panel">
          <div class="sub-head">
            <span class="sub-title">卸問屋（{{ orosiState.syubetu }}）</span>
            <span class="sub-info">普通口座 {{ yen(orosiState.savings) }}円／在庫種類 {{ orosiState.stock_kinds }}／{{ orosiState.max_kinds }}</span>
            <button class="btn mini" @click="closeOrosi">閉じる</button>
          </div>
          <div v-if="orosiState.items.length === 0" class="empty">仕入れられる商品がありません。</div>
          <div v-else class="scroll">
            <table class="tbl">
              <thead>
                <tr><th class="l">品名</th><th>種類</th><th>仕入れ値</th><th>店在庫</th><th>数量</th><th></th></tr>
              </thead>
              <tbody>
                <tr v-for="it in orosiState.items" :key="it.item_id">
                  <td class="l">{{ it.name }}</td>
                  <td>{{ it.category }}</td>
                  <td class="price">{{ yen(it.buy_price) }}円</td>
                  <td :class="{ full: it.in_stock >= orosiState.max_stock }">{{ it.in_stock }}/{{ orosiState.max_stock }}</td>
                  <td><input v-model.number="shiireQty[it.item_id]" type="number" min="1" :max="orosiState.max_stock" class="inp-num" /></td>
                  <td><button class="btn mini" :disabled="busy || it.in_stock >= orosiState.max_stock" @click="doShiire(it)">仕入れる</button></td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- 商品リスト・価格設定 -->
        <div v-if="priceStock && priceStock.has_shop" class="sub-panel">
          <div class="sub-head">
            <span class="sub-title">商品リスト・価格設定</span>
            <span class="sub-info">掛け率{{ priceStock.markup }}倍／0円で掛け率に戻す。販売価格は仕入れ値の3倍まで</span>
            <button class="btn mini" @click="closePrice">閉じる</button>
          </div>
          <div v-if="priceStock.items.length === 0" class="empty">在庫がありません。まず仕入れてください。</div>
          <div v-else class="scroll">
            <table class="tbl">
              <thead>
                <tr><th class="l">品名</th><th>仕入れ値</th><th>上限(×3)</th><th>店頭価格</th><th>新価格</th><th></th></tr>
              </thead>
              <tbody>
                <tr v-for="it in priceStock.items" :key="it.item_id">
                  <td class="l">{{ it.name }}</td>
                  <td class="price">{{ yen(it.buy_price) }}円</td>
                  <td>{{ yen(it.max_price) }}円</td>
                  <td class="price">{{ yen(it.shelf) }}円{{ it.sell_price === null ? '(掛率)' : '' }}</td>
                  <td><input v-model.number="priceDraft[it.item_id]" type="number" min="0" :max="it.max_price" class="inp-num wide" /></td>
                  <td><button class="btn mini" :disabled="busy" @click="savePrice(it)">設定</button></td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <!-- 基本設定(レガシー: ■基本設定) -->
      <div class="panel-white sec">
        <div class="sec-head">■基本設定</div>
        <div class="note">●街で家にマウスがのった時に表示されるコメント（40字以内）</div>
        <div class="row-line">
          <input v-model="commentDraft" maxlength="40" class="inp grow" />
          <button class="btn mini" :disabled="busy" @click="saveComment">OK</button>
        </div>
      </div>

      <!-- コンテンツ選択(レガシー: ●コンテンツ選択)。追加種別の家は機能固定で枠なし。 -->
      <div v-if="house.tuika !== 0" class="panel-white sec">
        <div class="sec-head">●{{ TUIKA_NAMES[house.tuika] ?? '追加種別' }}</div>
        <div class="note">この家は{{ TUIKA_NAMES[house.tuika] ?? '追加種別' }}です。コンテンツ枠はありません。</div>
      </div>
      <div v-else class="panel-white sec">
        <div class="sec-head">●コンテンツ選択</div>
        <div class="note">
          設置するコンテンツを選択してください。後で変更も可能です。タイトルは訪問画面のボタンに表示されます。<br />
          <template v-if="house.slots > 1">ここで一番上にあるコンテンツが家に入ったとき最初に表示されます。</template>
        </div>
        <div v-for="(row, s) in contentDraft" :key="s" class="slot-row">
          <span class="slot-no">○{{ s + 1 }}つめのコンテンツ</span>
          <select v-model="row.kind">
            <option v-for="k in CONTENT_KINDS" :key="k.value" :value="k.value">{{ k.label }}</option>
          </select>
          <label v-if="row.kind" class="fld">タイトル<input v-model="row.title" maxlength="20" class="inp" /></label>
          <input v-if="row.kind" v-model="row.comment" maxlength="100" class="inp lead" placeholder="タイトル下コメント(省略可)" />
          <input v-if="row.kind === 'url'" v-model="row.url" class="inp url" placeholder="https://…(埋め込むURL)" />
        </div>
        <button class="btn mini primary-btn" :disabled="busy" @click="saveContents">決定</button>
      </div>

      <!-- 家主掲示板への投稿(レガシー: gentei_settei ■投稿) -->
      <div v-if="nushiConfigured" class="panel-white sec">
        <div class="sec-head">■家主掲示板への投稿</div>
        <div class="note">家主掲示板の記事はここから投稿します（最新50件まで保存）。</div>
        <label class="fld">タイトル<input v-model="nushiTitle" maxlength="40" class="inp" /></label>
        <div><textarea v-model="nushiBody" rows="4" class="nushi-area"></textarea></div>
        <button class="btn mini primary-btn" :disabled="busy" @click="postNushi">投稿</button>
      </div>

      <!-- 家の外観、内装の変更(レガシー: house_change 選択画面) -->
      <div class="panel-white sec">
        <div class="sec-head">●家の外観、内装（コンテンツ枠数）の変更</div>
        <div class="note">建て替え費用は「外装×内装ランク倍率」×10000円を現金から支払います（地価は不要）。</div>
        <button v-if="!rebuildOpen" class="btn mini" :disabled="busy" @click="rebuildOpen = true">選択画面へ</button>
        <template v-else>
          <div class="rebuild-ext">
            <div class="fld">外装</div>
            <ExteriorPicker v-model="rebuildExterior" :exteriors="state!.exteriors" />
          </div>
          <div class="row-line">
            <label class="fld">内装
              <select v-model.number="rebuildInterior">
                <option v-for="i in state!.interiors" :key="i.rank" :value="i.rank">{{ i.name }}（費用×{{ i.multiplier }}・枠{{ i.slots }}）</option>
              </select>
            </label>
            <span class="cost">費用 {{ yen(rebuildCost) }}円（現金）</span>
            <button class="btn mini primary-btn" :disabled="busy" @click="doRebuild">建て替える</button>
            <button class="btn mini" :disabled="busy" @click="rebuildOpen = false">やめる</button>
          </div>
        </template>
      </div>

      <!-- 家の売却(レガシー: ●家の売却) -->
      <div class="panel-white sec">
        <div class="sec-head">●家の売却</div>
        <div class="note">
          家の場所を変更したい場合は、一度家を売却してから再度購入してください。売却で得られるのは土地の価格（{{ yen(sellRefund) }}円）だけです。
        </div>
        <button class="btn mini danger" :disabled="busy" @click="doSell">家の売却</button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.myhouse-page {
  background-color: #d8e8c8;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.btn.mini {
  font-size: 11px;
  padding: 2px 8px;
}
.btn.danger {
  background: #cc3333;
  color: #fff;
}
.primary-btn {
  background: #4a7a2a;
  color: #fff;
}
.settei-title {
  background: #336699;
  color: #fff;
  text-align: center;
  font-size: 13px;
  padding: 6px;
  border: 1px solid #999;
  max-width: 640px;
}
.err {
  color: #a33;
  font-size: 13px;
  margin-top: 6px;
}
.panel-white {
  background: #fff;
  border: 1px solid #999;
  padding: 8px;
  margin-top: 8px;
  max-width: 640px;
}
.house-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 8px;
}
.house-tabs .tab {
  background: #eef3e8;
  border: 1px solid #99a;
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
}
.house-tabs .tab.active {
  background: #336699;
  color: #fff;
  font-weight: bold;
}
.house-summary {
  display: flex;
  align-items: center;
  gap: 10px;
}
.house-summary img {
  width: 36px;
  height: 36px;
  object-fit: contain;
}
.hs-loc {
  font-weight: bold;
  color: #345;
}
.hs-sub {
  font-size: 11px;
  color: #789;
}
.sec .sec-head {
  font-weight: bold;
  color: #336699;
  margin-bottom: 6px;
  font-size: 13px;
}
.note {
  font-size: 11px;
  color: #778;
  margin-bottom: 6px;
  line-height: 1.6;
}
.row-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 6px;
}
.fld {
  font-size: 12px;
  color: #445;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.inp {
  font-size: 12px;
  padding: 2px 4px;
}
.inp.grow {
  flex: 1 1 auto;
}
.inp.url {
  min-width: 220px;
}
.inp.lead {
  min-width: 200px;
}
.inp-num {
  width: 60px;
  font-size: 12px;
  padding: 2px;
}
.nushi-area {
  width: 100%;
  max-width: 480px;
  box-sizing: border-box;
  font-size: 12px;
  padding: 3px;
  margin: 4px 0;
  resize: vertical;
}
.rebuild-ext {
  margin: 6px 0;
}
.rebuild-ext .fld {
  margin-bottom: 4px;
}
.inp-num.wide {
  width: 80px;
}
.slot-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 4px;
}
.slot-row .slot-no {
  font-size: 12px;
  color: #4a7a2a;
  flex: 0 0 auto;
}
.slot-row select {
  font-size: 12px;
}
.cost {
  font-size: 12px;
  color: #cc3300;
  font-weight: bold;
}
.sub-panel {
  border: 1px dashed #bcd;
  padding: 6px;
  margin-top: 6px;
}
.sub-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 4px;
}
.sub-title {
  font-weight: bold;
  color: #7a4a00;
  font-size: 13px;
}
.sub-info {
  font-size: 11px;
  color: #889;
  flex: 1 1 auto;
}
.empty {
  font-size: 12px;
  color: #888;
}
.scroll {
  overflow-x: auto;
}
.tbl {
  border-collapse: collapse;
  font-size: 12px;
  white-space: nowrap;
  width: 100%;
}
.tbl th {
  background: #f0e0c0;
  color: #634;
  padding: 3px 6px;
  border: 1px solid #dc9;
}
.tbl td {
  padding: 3px 6px;
  border: 1px solid #eee;
  text-align: center;
}
.tbl th.l,
.tbl td.l {
  text-align: left;
}
.tbl td.price {
  color: #cc3300;
  text-align: right;
}
.tbl td.full {
  color: #cc0000;
  font-weight: bold;
}
</style>
