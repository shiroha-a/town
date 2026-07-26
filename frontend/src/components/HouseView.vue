<script setup lang="ts">
import CommandIcon from './CommandIcon.vue';
import EmojiPicker from './EmojiPicker.vue';
import RichText from './RichText.vue';
import { ref, computed, onMounted } from 'vue';
import {
  api,
  type Player,
  type Town,
  type HouseCell,
  type HouseContent,
  type HouseShopView,
  type HouseShopItem,
  type BbsPost,
} from '../api';
import Toast from './Toast.vue';
import YamiShop from './YamiShop.vue';
import CompanyPanel from './CompanyPanel.vue';
import { useToast } from '../toast';

// 家訪問(レガシー original_house.cgi houmon)。レガシーのレイアウトを再現:
// 上部1行[街に戻る|コンテンツボタン|さい銭箱] + 中央寄せのコンテンツボックス。
// 背景色・囲み枠・文字色はレガシーの既定スタイルに合わせる
// (bbs=#ffcc66/点線青枠, 家主板=#99cc99/点線緑枠, 店=#ffcc33/白テーブル, URL=白)。
const props = defineProps<{ player: Player; houseId: number }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const busy = ref(false);
const message = ref('');
const { toast, showToast, closeToast } = useToast();

const house = ref<HouseCell | null>(null);
const allHouses = ref<HouseCell[]>([]);
const townList = ref<Town[]>([]);

// コンテンツ枠(枠順)。ボタンとして描画し、selectedSlotの枠を表示する。
const contents = computed<HouseContent[]>(() => house.value?.contents ?? []);
const selectedSlot = ref<number | null>(null);
const current = computed<HouseContent | null>(
  () => contents.value.find((c) => c.slot === selectedSlot.value) ?? null,
);
function kindLabel(kind: string): string {
  return { bbs: '掲示板', shop: 'お店', nushi: '家主板', url: 'リンク' }[kind] ?? kind;
}
function tabLabel(c: HouseContent): string {
  return c.title || kindLabel(c.kind);
}
function selectTab(c: HouseContent) {
  selectedSlot.value = c.slot;
  bbsPage.value = 0;
  nushiPage.value = 0;
}
// ページ背景色(レガシー既定: bbs=#ffcc66 / gentei=#99cc99 / omise=#ffcc33 / dokuzi=白)。
const pageBg = computed(() => {
  switch (current.value?.kind) {
    case 'nushi':
      return '#99cc99';
    case 'shop':
      return '#ffcc33';
    case 'url':
      return '#ffffff';
    default:
      return '#ffcc66';
  }
});

onMounted(async () => {
  try {
    const [houses, towns] = await Promise.all([api.houses(props.player.id), api.towns()]);
    townList.value = towns;
    allHouses.value = houses;
    house.value = houses.find((h) => h.id === props.houseId) ?? null;
    if (!house.value) {
      message.value = 'その家は見つかりませんでした。';
      return;
    }
    // 追加種別の家はコンテンツ枠を持たず、専用画面(運営/株式会社/闇市)になる。
    if (house.value.tuika !== 0) {
      return;
    }
    // レガシー忠実: 公開コンテンツが無い家には入れない。
    if (contents.value.length === 0) {
      message.value = 'まだ人に見せられる家では無いようです。';
      house.value = null;
      return;
    }
    // 一番上の枠が初期表示。
    selectedSlot.value = contents.value[0].slot;
    loadShop();
    loadBbs();
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  }
});

const fmtDate = (iso: string) => {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}/${p(d.getMonth() + 1)}/${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
};

// さい銭(レガシー同様、自分の家でも表示・可能)。
const saisenChoices = [100, 500, 1000, 2000, 5000, 10000];
const saisenAmount = ref(100);
async function doSaisen() {
  if (!house.value) return;
  busy.value = true;
  const amt = saisenAmount.value;
  try {
    const after = await api.saisen(props.player.id, house.value.id, amt);
    emit('update', after);
    showToast({
      variant: 'item',
      title: 'さい銭しました',
      lines: [`${house.value.owner_name}さんに ${yen(amt)}円 をさい銭しました`],
      icon: 'item',
    });
  } catch (e) {
    showToast({
      variant: 'error',
      title: 'さい銭できませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}

// お店(レガシーomise): radioで商品を選び、下部の個数(1〜4)+支払い方法で購入。
const shop = ref<HouseShopView | null>(null);
const selectedItemId = ref<number | null>(null);
const buyQtySel = ref(1);
const qtyChoices = [1, 2, 3, 4];
const payMethod = ref<'cash' | 'credit'>('cash');
const hasCreditCard = computed(() =>
  props.player.items.some((it) => it.enables_credit && it.remaining_uses > 0),
);
const neighborDiscount = computed(
  () => allHouses.value.some((h) => h.own && h.town === house.value?.town) && !house.value?.own,
);
// 商品テーブルのパラメータ列(レガシーの 国..面。アダルト系は対象外)。
const PARAM_COLS: { key: string; label: string }[] = [
  { key: 'kokugo', label: '国' },
  { key: 'suugaku', label: '数' },
  { key: 'rika', label: '理' },
  { key: 'syakai', label: '社' },
  { key: 'eigo', label: '英' },
  { key: 'ongaku', label: '音' },
  { key: 'bijutsu', label: '美' },
  { key: 'looks', label: 'ル' },
  { key: 'tairyoku', label: '体' },
  { key: 'kenkou', label: '健' },
  { key: 'speed', label: 'ス' },
  { key: 'power', label: 'パ' },
  { key: 'wanryoku', label: '腕' },
  { key: 'kyakuryoku', label: '脚' },
  { key: 'love', label: 'L' },
  { key: 'omoshirosa', label: '面' },
];
const paramVal = (it: HouseShopItem, key: string) => it.params[key] || '';
// 種別(カテゴリ)ごとにまとめて「▼種別」行を挟む(レガシーの表示順)。
const shopGroups = computed(() => {
  const groups: { category: string; items: HouseShopItem[] }[] = [];
  for (const it of shop.value?.items ?? []) {
    const g = groups.find((x) => x.category === it.category);
    if (g) g.items.push(it);
    else groups.push({ category: it.category, items: [it] });
  }
  return groups;
});
async function loadShop() {
  shop.value = null;
  try {
    shop.value = await api.houseShop(props.player.id, props.houseId);
  } catch {
    shop.value = null;
  }
}
async function doBuy() {
  if (!house.value || selectedItemId.value === null) return;
  const it = shop.value?.items.find((x) => x.item_id === selectedItemId.value);
  if (!it) return;
  busy.value = true;
  try {
    const after = await api.buyFromHouseShop(props.player.id, house.value.id, it.item_id, buyQtySel.value, payMethod.value);
    emit('update', after);
    shop.value = await api.houseShop(props.player.id, house.value.id);
    const br = after.buy_result;
    const lines = [`${it.name} を${buyQtySel.value}個（${br.method === 'credit' ? 'クレジット・普通口座' : '現金'} ${yen(br.paid)}円）`];
    if (br.cashback > 0) lines.push(`ご近所キャッシュバック ${yen(br.cashback)}円引き`);
    showToast({ variant: 'item', title: '買いました', lines, icon: 'item' });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '購入できませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}

// 掲示板(レガシーnormal_bbs: スレッド形式・投稿でお金・記事no指定削除 /
// 家主板gentei: ブログ風記事5件ページング・投稿は家の設定から)。
const bbs = ref<BbsPost[]>([]);
const bbsBody = ref('');
const replyBodies = ref<Record<number, string>>({});
const normalPosts = computed(() => bbs.value.filter((p) => p.kind === 'normal'));
const nushiPosts = computed(() => bbs.value.filter((p) => p.kind === 'nushi'));
// スレッド構成: 親記事(thread_no)ごとにレス(parent_no)をまとめ、
// スレッド内の最新投稿が新しい順に並べる(レスでスレッドが最上部へ浮上)。
interface BbsThread {
  parent: BbsPost;
  replies: BbsPost[];
}
const bbsThreads = computed<BbsThread[]>(() => {
  const map = new Map<number, BbsThread>();
  for (const p of normalPosts.value) {
    if (p.thread_no > 0) map.set(p.thread_no, { parent: p, replies: [] });
  }
  for (const p of normalPosts.value) {
    if (p.parent_no > 0) map.get(p.parent_no)?.replies.push(p);
  }
  const arr = [...map.values()];
  for (const t of arr) t.replies.sort((a, b) => a.id - b.id);
  const lastId = (t: BbsThread) => Math.max(t.parent.id, ...t.replies.map((r) => r.id));
  arr.sort((a, b) => lastId(b) - lastId(a));
  return arr;
});
const BBS_PER_PAGE = 10;
const NUSHI_PER_PAGE = 5; // レガシー gentei_kensuu 既定
const bbsPage = ref(0);
const nushiPage = ref(0);
const bbsPageThreads = computed(() =>
  bbsThreads.value.slice(bbsPage.value * BBS_PER_PAGE, (bbsPage.value + 1) * BBS_PER_PAGE),
);
const nushiPagePosts = computed(() =>
  nushiPosts.value.slice(nushiPage.value * NUSHI_PER_PAGE, (nushiPage.value + 1) * NUSHI_PER_PAGE),
);
const bbsMaxPage = computed(() => Math.max(0, Math.ceil(bbsThreads.value.length / BBS_PER_PAGE) - 1));
const nushiMaxPage = computed(() => Math.max(0, Math.ceil(nushiPosts.value.length / NUSHI_PER_PAGE) - 1));
// 本文中のURLをリンク化するためのトークン分割(レガシーの自動リンク)。
// 絵文字ピッカー。開いた入力欄を覚えておき、選ばれたショートコードを差し込む。
const emojiFor = ref<string | null>(null);
function insertEmoji(code: string) {
  const target = emojiFor.value;
  emojiFor.value = null;
  if (target === 'bbs') bbsBody.value = bbsBody.value + code;
  else if (target?.startsWith('reply:')) {
    const no = Number(target.slice(6));
    replyBodies.value[no] = (replyBodies.value[no] ?? '') + code;
  }
}

async function loadBbs() {
  bbs.value = [];
  try {
    bbs.value = await api.houseBbs(props.player.id, props.houseId);
  } catch {
    bbs.value = [];
  }
}
async function doPostBbs(body: string, parentNo = 0) {
  if (!house.value || !body.trim()) return;
  busy.value = true;
  try {
    const after = await api.postBbs(props.player.id, house.value.id, 'normal', body, '', parentNo);
    emit('update', after);
    if (parentNo > 0) replyBodies.value[parentNo] = '';
    else bbsBody.value = '';
    bbs.value = await api.houseBbs(props.player.id, house.value.id);
    const r = after.bbs_result;
    showToast({
      variant: 'item',
      title: '投稿しました',
      lines: [
        r.bonus
          ? `●ボーナスです！${yen(r.reward)}円のお金をゲットしました。`
          : `●${yen(r.reward)}円のお金を得ました。`,
      ],
      icon: 'item',
    });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '書き込めませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}
// 記事no./親記事no./全記事削除(レガシーbbs_delete)と家主板の記事no.削除(gentei_delete)。
const delArticleNo = ref('');
const delThreadNo = ref('');
const delNushiNo = ref('');
async function doDeleteBbs(kind: string, opts: { articleNo?: number; threadNo?: number; all?: boolean }) {
  if (!house.value) return;
  if (opts.all && !window.confirm('全記事を削除します。よろしいですか？')) return;
  busy.value = true;
  try {
    const after = await api.deleteBbs(props.player.id, house.value.id, kind, opts);
    emit('update', after);
    bbs.value = await api.houseBbs(props.player.id, house.value.id);
    delArticleNo.value = '';
    delThreadNo.value = '';
    delNushiNo.value = '';
    showToast({ variant: 'item', title: '記事を削除しました。', lines: [], icon: 'item' });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '削除できませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="house-page facility-page" :style="{ backgroundColor: house ? pageBg : '#ffcc66' }">
    <Toast :toast="toast" @close="closeToast" />

    <!-- 訪問不可(コンテンツ未公開)や読み込みエラー -->
    <div v-if="message" class="err-panel">
      <div class="err">{{ message }}</div>
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>

    <template v-if="house">
      <!-- 上部1行(レガシー): 街に戻る + コンテンツボタン + さい銭箱(右寄せ) -->
      <div class="visit-bar">
        <button class="btn" @click="emit('back')">街に戻る</button>
        <button
          v-for="c in contents"
          :key="c.slot"
          class="btn tabbtn"
          :class="{ active: c.slot === selectedSlot }"
          @click="selectTab(c)"
        >
          {{ tabLabel(c) }}
        </button>
        <span class="bar-spacer"></span>
        <span class="saisen-box">
          <select v-model.number="saisenAmount">
            <option v-for="a in saisenChoices" :key="a" :value="a">{{ yen(a) }}円</option>
          </select>
          <button class="btn" :disabled="busy" @click="doSaisen">さい銭する</button>
        </span>
      </div>

      <!-- 追加種別の家: 持ち物販売店=闇市 / 運営・株式会社=社員教育。 -->
      <YamiShop v-if="house.tuika === 3" :player="player" :house-id="house.id" @update="emit('update', $event)" />
      <CompanyPanel
        v-else-if="house.tuika === 1 || house.tuika === 2"
        :player="player"
        :house-id="house.id"
        @update="emit('update', $event)"
      />

      <!-- 通常掲示板(レガシーbbs1: #ffffcc・青点線枠・スレッド形式) -->
      <template v-if="current && current.kind === 'bbs'">
        <div class="cbox bbs-box">
          <div class="bbs-title">{{ tabLabel(current) }}</div>
          <div v-if="current.comment" class="bbs-lead">{{ current.comment }}</div>
          <div class="bbs-form">
            <textarea v-model="bbsBody" rows="4" class="bbs-area"></textarea>
            <div class="btn-row">
              <button class="btn" :disabled="busy" @click="doPostBbs(bbsBody)">新規投稿</button>
              <button class="btn emoji-btn" title="絵文字を入れる" @click="emojiFor = 'bbs'">
                <CommandIcon name="emoji" />
              </button>
            </div>
          </div>
          <div class="bbs-posts">
            <div v-if="bbsThreads.length === 0" class="bbs-empty">まだ書き込みはありません。</div>
            <div v-for="t in bbsPageThreads" :key="t.parent.thread_no" class="bbs-thread">
              <hr class="thread-hr" />
              <div class="thread-no">NO.{{ t.parent.thread_no }}</div>
              <div>
                <span class="bbs-author">{{ t.parent.author_name }}<span v-if="t.parent.author_job" class="bbs-job">（{{ t.parent.author_job }}）</span></span>：<span class="bbs-body-inline"><RichText :text="t.parent.body" /></span>（{{ fmtDate(t.parent.created_at) }}）<span class="bbs-no">記事no.{{ t.parent.id }}</span>
              </div>
              <div class="reply-form">
                <textarea v-model="replyBodies[t.parent.thread_no]" rows="2" class="reply-area"></textarea>
                <span class="btn-row">
                  <button
                    class="btn emoji-btn"
                    title="絵文字を入れる"
                    @click="emojiFor = `reply:${t.parent.thread_no}`"
                  >
                    <CommandIcon name="emoji" />
                  </button>
                  <button class="btn" :disabled="busy" @click="doPostBbs(replyBodies[t.parent.thread_no] ?? '', t.parent.thread_no)">レス</button>
                </span>
              </div>
              <div v-for="p in t.replies" :key="p.id" class="bbs-reply">
                <span class="bbs-author">{{ p.author_name }}<span v-if="p.author_job" class="bbs-job">（{{ p.author_job }}）</span></span>：<span class="bbs-body-inline"><RichText :text="p.body" /></span>（{{ fmtDate(p.created_at) }}）<span class="bbs-no">記事no.{{ p.id }}</span>
              </div>
            </div>
          </div>
          <div v-if="bbsMaxPage > 0" class="pager">
            <button class="btn" :disabled="bbsPage <= 0" @click="bbsPage--">BACK</button>
            <button class="btn" :disabled="bbsPage >= bbsMaxPage" @click="bbsPage++">NEXT</button>
          </div>
        </div>
        <!-- 記事削除フォーム(レガシー: 記事no./親記事no./全記事削除) -->
        <div class="del-form">
          <div>
            記事no.
            <input v-model="delArticleNo" size="8" class="del-input" />
            <button class="btn" :disabled="busy" @click="doDeleteBbs('normal', { articleNo: Number(delArticleNo) || 0 })">削除</button>
            親記事no.
            <input v-model="delThreadNo" size="8" class="del-input" />
            <button class="btn" :disabled="busy" @click="doDeleteBbs('normal', { threadNo: Number(delThreadNo) || 0 })">削除</button>
            <button class="btn" :disabled="busy" @click="doDeleteBbs('normal', { all: true })">全記事削除</button>
          </div>
          <br />
          ※ゲーム管理者のみ「記事no.」を指定して記事を削除することができます。<br />
          ※投稿することでお金を得ることができますが、無意味な発言、荒らし行為など不適切な投稿があった場合、減金、ホストの公開、アクセス拒否などのペナルティがありますのでご注意ください。
        </div>
      </template>

      <!-- 家主板(レガシーgentei: #ffffcc・緑点線枠・記事形式。投稿は家の設定から) -->
      <template v-else-if="current && current.kind === 'nushi'">
        <div class="cbox nushi-box">
          <div class="nushi-title">{{ tabLabel(current) }}</div>
          <div v-if="current.comment" class="nushi-lead">{{ current.comment }}</div>
          <div class="bbs-posts">
            <div v-if="nushiPosts.length === 0" class="bbs-empty">まだ記事はありません。</div>
            <div v-for="p in nushiPagePosts" :key="p.id" class="nushi-article">
              <div class="nushi-daimei">{{ p.title || '(無題)' }}</div>
              <div class="nushi-body">{{ p.body }}（{{ fmtDate(p.created_at) }}）<span class="bbs-no">記事no.{{ p.id }}</span></div>
            </div>
          </div>
          <div v-if="nushiMaxPage > 0" class="pager">
            <button class="btn" :disabled="nushiPage <= 0" @click="nushiPage--">BACK</button>
            <button class="btn" :disabled="nushiPage >= nushiMaxPage" @click="nushiPage++">NEXT</button>
          </div>
        </div>
        <div class="del-form">
          <div>
            記事no.
            <input v-model="delNushiNo" size="8" class="del-input" />
            <button class="btn" :disabled="busy" @click="doDeleteBbs('nushi', { articleNo: Number(delNushiNo) || 0 })">削除</button>
          </div>
          ※記事を書いた本人とゲーム管理者のみ「記事no.」を指定して記事を削除することができます。
        </div>
      </template>

      <!-- お店(レガシーomise: #ffcc33地・白タイトル行・全パラメータ列の商品表) -->
      <template v-else-if="current && current.kind === 'shop'">
        <template v-if="shop && shop.has_shop">
          <table class="omise-head">
            <tr>
              <td class="omise-title">{{ tabLabel(current) }}</td>
              <td class="omise-lead">{{ current.comment || shop.title }}（{{ shop.syubetu }}）</td>
            </tr>
          </table>
          <div v-if="shop.own" class="omise-note">※自分のお店で商品を買うことはできません。</div>
          <div v-if="shop.items.length === 0" class="omise-note">売り切れです。</div>
          <div v-else class="omise-scroll">
            <table class="omise-table">
              <tr>
                <td class="hanrei" :colspan="PARAM_COLS.length + 8">
                  凡例：(国)＝国語up値、(数)＝数学up値、(理)＝理科up値、(社)＝社会up値、(英)＝英語up値、(音)＝音楽up値、(美)＝美術up値、（ル）=ルックスup値、（体）=体力up値、（健）=健康up値、（ス）=スピードup値、（パ）=パワーup値、（腕）=腕力up値、（脚）=脚力up値、（L）=LOVEup値、（面）=面白さup値。青字は所持中(残数)。
                </td>
              </tr>
              <tr class="koumoku">
                <td>商品</td>
                <td>価格</td>
                <td>在庫</td>
                <td v-for="pc in PARAM_COLS" :key="pc.key">{{ pc.label }}</td>
                <td>カロリー</td>
                <td>耐久</td>
                <td>使用<br />間隔</td>
                <td>身体<br />消費</td>
                <td>頭脳<br />消費</td>
              </tr>
              <template v-for="g in shopGroups" :key="g.category">
                <tr class="syubetu-row">
                  <td :colspan="PARAM_COLS.length + 8">▼{{ g.category }}</td>
                </tr>
                <tr v-for="it in g.items" :key="it.item_id" class="syouhin-row">
                  <td class="hinmoku">
                    <input
                      v-if="!shop.own"
                      v-model.number="selectedItemId"
                      type="radio"
                      :value="it.item_id"
                      name="syo_hinmoku"
                    />
                    <span :class="{ motteru: it.owned > 0 }">{{ it.name }}<template v-if="it.owned > 0">({{ it.owned }})</template></span>
                  </td>
                  <td class="r">{{ yen(it.price) }}円</td>
                  <td class="r">{{ it.stock }}</td>
                  <td v-for="pc in PARAM_COLS" :key="pc.key">{{ paramVal(it, pc.key) }}</td>
                  <td class="r">{{ it.calorie_g || '' }}</td>
                  <td>{{ it.durability }}{{ it.durability_unit === 'day' ? '日' : '回' }}</td>
                  <td>{{ it.interval_min }}分</td>
                  <td>{{ it.body_cost || '' }}</td>
                  <td>{{ it.nou_cost || '' }}</td>
                </tr>
              </template>
              <tr v-if="!shop.own">
                <td :colspan="PARAM_COLS.length + 8" class="buy-row">
                  個数
                  <select v-model.number="buyQtySel">
                    <option v-for="q in qtyChoices" :key="q" :value="q">{{ q }}</option>
                  </select>
                  支払い方法
                  <select v-model="payMethod">
                    <option value="cash">現金</option>
                    <option value="credit" :disabled="!hasCreditCard">クレジット（普通口座）</option>
                  </select>
                  <button class="btn" :disabled="busy || selectedItemId === null" @click="doBuy">購入する</button>
                  <span v-if="neighborDiscount" class="neighbor-note">※この街の住人は単価10%引き</span>
                </td>
              </tr>
            </table>
          </div>
        </template>
        <div v-else class="omise-note">お店は準備中です。</div>
      </template>

      <!-- 独自URL(レガシーdokuzi: 白地・IFRAME 800x400) -->
      <div v-else-if="current && current.kind === 'url'" class="dokuzi-wrap">
        <div class="dokuzi-title">{{ tabLabel(current) }}</div>
        <div v-if="current.comment" class="dokuzi-lead">{{ current.comment }}</div>
        <iframe
          :src="current.url"
          class="dokuzi-frame"
          sandbox="allow-scripts allow-forms allow-popups"
          referrerpolicy="no-referrer"
        ></iframe>
      </div>

      <!-- 画面下部の退出ボタン(レガシーhooter: 家を出る) -->
      <div class="ie-deru">
        <button class="btn" @click="emit('back')">家を出る</button>
      </div>
    </template>
  </div>
  <EmojiPicker v-if="emojiFor" @pick="insertEmoji" @close="emojiFor = null" />
</template>

<style scoped>
/* 絵文字ボタン。アイコンのみで、隣のテキストボタンと高さを揃えるために
   align-items: stretch の行に入れる(文字の行高に依存させない)。 */
.btn-row {
  display: inline-flex;
  gap: 6px;
  align-items: stretch;
}
.emoji-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 8px;
}
.emoji-btn :deep(.cmd-icon) {
  width: 15px;
  height: 15px;
}
/* bodyのmargin(5px)を打ち消し、レガシーのbody背景色のように全面を塗る。 */
.house-page {
  padding: 6px;
  margin: -5px;
  min-height: 100vh;
  box-sizing: border-box;
}
.err-panel {
  background: #fff;
  border: 1px solid #999;
  padding: 10px;
  max-width: 480px;
  margin: 20px auto;
  text-align: center;
}
.err-panel .err {
  color: #a33;
  font-size: 14px;
  margin-bottom: 8px;
}
/* 上部1行: 街に戻る + コンテンツボタン + さい銭箱(右) */
.visit-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 5px;
  padding: 5px;
  max-width: 820px;
  margin: 0 auto;
}
.bar-spacer {
  flex: 1 1 auto;
}
.btn.tabbtn.active {
  background: #666;
  color: #fff;
}
.saisen-box {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: #000;
}
/* 中央寄せのコンテンツボックス(bbs/gentei共通の囲みテーブル) */
.cbox {
  margin: 10px auto 0;
  padding: 14px;
  font-size: 11px;
  line-height: 16px;
  color: #666666;
  background-color: #ffffcc;
}
.bbs-box {
  width: 500px;
  max-width: 96%;
  border: 4px dotted #336699;
}
.nushi-box {
  width: 520px;
  max-width: 96%;
  border: 4px dotted #339966;
}
.bbs-title {
  font-size: 16px;
  color: #666666;
  line-height: 180%;
  text-align: center;
}
.bbs-lead {
  font-size: 11px;
  line-height: 16px;
  color: #336699;
  white-space: pre-line;
}
.nushi-title {
  font-size: 20px;
  color: #339966;
  line-height: 150%;
  text-align: center;
}
.nushi-lead {
  font-size: 11px;
  color: #ff6600;
  line-height: 160%;
  white-space: pre-line;
}
.bbs-form {
  margin: 8px 0;
}
.bbs-area {
  width: 100%;
  box-sizing: border-box;
  font-size: 12px;
  padding: 3px;
  resize: vertical;
}
.bbs-posts {
  margin-top: 8px;
}
.thread-hr {
  border: none;
  border-top: 1px solid #999;
  margin: 10px 0;
}
.thread-no {
  font-size: 15px;
  color: #336699;
}
.bbs-author {
  font-size: 11px;
  color: #ff6600;
}
.bbs-job {
  font-size: 9px;
}
.bbs-body-inline {
  white-space: pre-line;
  color: #666;
}
.bbs-no {
  font-size: 9px;
  color: #999;
}
.reply-form {
  display: flex;
  align-items: flex-end;
  gap: 4px;
  margin: 4px 0;
}
.reply-area {
  width: 70%;
  font-size: 12px;
  padding: 2px;
  resize: vertical;
}
.bbs-reply {
  margin: 2px 0;
}
.nushi-article {
  margin-bottom: 6px;
}
.nushi-daimei {
  font-size: 14px;
  color: #445555;
  line-height: 200%;
  font-weight: bold;
}
.nushi-body {
  white-space: pre-line;
  color: #555;
}
.bbs-empty {
  color: #999;
}
/* 記事削除フォーム(コンテンツボックス下・10px #444) */
.del-form {
  width: 500px;
  max-width: 96%;
  margin: 8px auto 0;
  font-size: 10px;
  color: #444444;
  text-align: center;
}
.del-input {
  width: 70px;
  font-size: 11px;
  padding: 1px 3px;
}
/* 画面下部の退出ボタン */
.ie-deru {
  text-align: center;
  margin: 24px 0 12px;
}
.pager {
  display: flex;
  justify-content: center;
  gap: 8px;
  margin-top: 8px;
}
/* お店(omise) */
.omise-head {
  width: 100%;
  max-width: 820px;
  margin: 8px auto 0;
  border-collapse: collapse;
  font-size: 11px;
  line-height: 18px;
  color: #666666;
  background-color: #ffffff;
  border: 1px solid #666666;
}
.omise-head td {
  padding: 10px;
}
.omise-title {
  font-size: 18px;
  color: #ff6600;
  line-height: 160%;
  white-space: nowrap;
}
.omise-lead {
  font-size: 11px;
  line-height: 16px;
  color: #000000;
  width: 65%;
}
.omise-note {
  max-width: 820px;
  margin: 6px auto;
  font-size: 11px;
  color: #663300;
}
.omise-scroll {
  max-width: 820px;
  margin: 8px auto 0;
  overflow-x: auto;
}
.omise-table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 1px;
  font-size: 10px;
  color: #336699;
  background-color: #ffffff;
  border: 1px solid #666666;
}
.omise-table td {
  padding: 4px 5px;
  text-align: center;
  white-space: nowrap;
}
.omise-table td.r {
  text-align: right;
}
.omise-table .hanrei {
  text-align: left;
  white-space: normal;
  font-size: 10px;
  color: #336699;
}
.omise-table .koumoku td {
  font-size: 11px;
  color: #000000;
  background-color: #ffcc66;
}
.omise-table .syubetu-row td {
  background-color: #ffff88;
  text-align: left;
  color: #333;
}
.omise-table .syouhin-row td {
  font-size: 11px;
  color: #333333;
  background-color: #ffffaa;
}
.omise-table .hinmoku {
  text-align: left;
}
.motteru {
  color: #0000ff;
}
.buy-row {
  text-align: left !important;
  background: #fff;
  font-size: 11px;
  color: #333;
}
.neighbor-note {
  color: #2a7a2a;
  font-weight: bold;
  margin-left: 8px;
}
/* 独自URL(dokuzi) */
.dokuzi-wrap {
  max-width: 820px;
  margin: 10px auto 0;
  text-align: center;
}
.dokuzi-title {
  font-size: 16px;
  color: #666666;
  line-height: 160%;
}
.dokuzi-lead {
  font-size: 11px;
  line-height: 16px;
  color: #336699;
  white-space: pre-line;
}
.dokuzi-frame {
  width: 100%;
  max-width: 800px;
  height: 400px;
  border: 1px solid #ccc;
  background: #fff;
  margin-top: 6px;
}
</style>
