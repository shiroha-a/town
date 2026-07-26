<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, type Player, type YamiView, type YamiInventoryItem } from '../api';
import Toast from './Toast.vue';
import { useToast } from '../toast';

// 持ち物販売店=闇市(レガシーmotimono_hanbai/mothimonohanbai.cgi)。
// 売り場: 1行=1品、種別ごとにまとめ、買うボタン+支払い方法。家主には倉庫品も表示。
// 家主は「売る/預ける」で自分の持ち物から出品する(任意価格、既定=単価×残耐久)。
const props = defineProps<{ player: Player; houseId: number }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');
const busy = ref(false);
const { toast, showToast, closeToast } = useToast();

const view = ref<YamiView | null>(null);
const mode = ref<'shop' | 'list'>('shop');
const inventory = ref<YamiInventoryItem[]>([]);
const priceDrafts = ref<Record<number, string>>({});
const payMethods = ref<Record<number, string>>({});

const hasCreditCard = computed(() =>
  props.player.items.some((it) => it.enables_credit && it.remaining_uses > 0),
);

// 商品テーブルのパラメータ列(レガシー闇市の 国..面)。
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
const paramVal = (params: Record<string, number>, key: string) => params[key] || '';

// 種別(カテゴリ)ごとにまとめて「▼種別」行を挟む。
const groups = computed(() => {
  const out: { category: string; items: NonNullable<typeof view.value>['items'] }[] = [];
  for (const it of view.value?.items ?? []) {
    const g = out.find((x) => x.category === it.category);
    if (g) g.items.push(it);
    else out.push({ category: it.category, items: [it] });
  }
  return out;
});

async function reload() {
  view.value = await api.yamiShop(props.player.id, props.houseId);
  for (const it of view.value.items) {
    if (!payMethods.value[it.listing_id]) payMethods.value[it.listing_id] = 'cash';
  }
}
onMounted(reload);

async function buy(listingId: number) {
  busy.value = true;
  try {
    const after = await api.yamiBuy(
      props.player.id,
      props.houseId,
      listingId,
      payMethods.value[listingId] ?? 'cash',
    );
    emit('update', after);
    await reload();
    const r = after.yami_result;
    showToast({
      variant: 'item',
      title: r.own ? '回収しました' : '買いました',
      lines: [
        r.own
          ? `${r.name} を回収しました（手数料500円）`
          : `${r.name}（${r.method === 'credit' ? 'クレジット・普通口座' : '現金'} ${yen(r.paid)}円）`,
      ],
      icon: 'item',
    });
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

async function openList() {
  inventory.value = await api.yamiInventory(props.player.id);
  priceDrafts.value = {};
  mode.value = 'list';
}

async function listItem(it: YamiInventoryItem, warehouse: boolean) {
  busy.value = true;
  try {
    const price = Number(priceDrafts.value[it.item_id] ?? '') || 0;
    const after = await api.yamiList(props.player.id, props.houseId, it.item_id, price, warehouse);
    emit('update', after);
    inventory.value = await api.yamiInventory(props.player.id);
    await reload();
    showToast({
      variant: 'item',
      title: warehouse ? '預けました' : '販売に出しました',
      lines: [it.name],
      icon: 'item',
    });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '出品できませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div v-if="view" class="yami">
    <Toast :toast="toast" @close="closeToast" />
    <!-- タイトル行(レガシー: 説明+闇市の黒箱) -->
    <table class="yami-head">
      <tr>
        <td class="yami-desc">
          闇市です。品揃えは家の持ち主が変えます。1品ずつの取り扱いで、価格は持ち主が決めます。
          <div class="money-line">
            ●{{ player.display_name }}さんの所持金：{{ yen(player.money) }}円
          </div>
        </td>
        <td class="yami-label">闇市</td>
      </tr>
    </table>

    <!-- 売り場 -->
    <template v-if="mode === 'shop'">
      <div class="yami-scroll">
        <table class="yami-table">
          <tr>
            <td class="hanrei" :colspan="PARAM_COLS.length + 8">
              凡例：(国)＝国語up値、(数)＝数学up値、(理)＝理科up値、(社)＝社会up値、(英)＝英語up値、(音)＝音楽up値、(美)＝美術up値、（ル）=ルックスup値、（体）=体力up値、（健）=健康up値、（ス）=スピードup値、（パ）=パワーup値、（腕）=腕力up値、（脚）=脚力up値、（L）=LOVEup値、（面）=面白さup値。
              <template v-if="view.own"
                >【倉庫品】は訪問者に表示されません。回収は「買う」(手数料500円)です。</template
              >
            </td>
          </tr>
          <tr class="koumoku">
            <td>商品</td>
            <td>残り</td>
            <td>値段</td>
            <td v-for="pc in PARAM_COLS" :key="pc.key">{{ pc.label }}</td>
            <td>カロリー</td>
            <td>使用<br />間隔</td>
            <td>身体<br />消費</td>
            <td>頭脳<br />消費</td>
            <td></td>
          </tr>
          <template v-for="g in groups" :key="g.category">
            <tr class="syubetu-row">
              <td :colspan="PARAM_COLS.length + 8">▼{{ g.category }}</td>
            </tr>
            <tr v-for="it in g.items" :key="it.listing_id" class="item-row">
              <td class="hinmoku">
                <span v-if="it.zokusei === 1" class="souko">【倉庫品】</span>{{ it.name }}
              </td>
              <td>{{ it.uses }}{{ it.durability_unit === 'day' ? '日' : '回' }}</td>
              <td class="r">{{ yen(it.price) }}円</td>
              <td v-for="pc in PARAM_COLS" :key="pc.key">{{ paramVal(it.params, pc.key) }}</td>
              <td class="r">{{ it.calorie_g || '' }}</td>
              <td>{{ it.interval_min }}分</td>
              <td>{{ it.body_cost || '' }}</td>
              <td>{{ it.nou_cost || '' }}</td>
              <td class="buy-cell">
                <select v-if="!view.own" v-model="payMethods[it.listing_id]">
                  <option value="cash">現金</option>
                  <option value="credit" :disabled="!hasCreditCard">クレジット</option>
                </select>
                <button class="btn" :disabled="busy" @click="buy(it.listing_id)">
                  {{ view.own ? '回収' : '買う' }}
                </button>
              </td>
            </tr>
          </template>
          <tr v-if="view.items.length === 0">
            <td :colspan="PARAM_COLS.length + 8" class="empty">
              現在売り出し中の商品はありません。
            </td>
          </tr>
        </table>
      </div>
      <div v-if="view.own" class="owner-bar">
        <button class="btn" :disabled="busy" @click="openList">売る/預ける</button>
        <span class="slot-note">{{ view.items.length }}/{{ view.max_items }}枠</span>
      </div>
    </template>

    <!-- 出品画面(家主のみ: 自分の持ち物から選んで販売/倉庫へ) -->
    <template v-else>
      <div class="yami-scroll">
        <table class="yami-table">
          <tr class="koumoku">
            <td>商品</td>
            <td>所持</td>
            <td>残り</td>
            <td>既定価格</td>
            <td>価格(空欄=既定)</td>
            <td></td>
          </tr>
          <tr v-for="it in inventory" :key="it.item_id" class="item-row">
            <td class="hinmoku">{{ it.name }}</td>
            <td>{{ it.quantity }}</td>
            <td>{{ it.uses }}{{ it.durability_unit === 'day' ? '日' : '回' }}</td>
            <td class="r">{{ yen(it.default_price) }}円</td>
            <td><input v-model="priceDrafts[it.item_id]" class="price-inp" placeholder="円" /></td>
            <td class="buy-cell">
              <button class="btn" :disabled="busy" @click="listItem(it, false)">販売する</button>
              <button class="btn" :disabled="busy" @click="listItem(it, true)">預ける</button>
            </td>
          </tr>
          <tr v-if="inventory.length === 0">
            <td colspan="6" class="empty">現在所有しているアイテムはありません。</td>
          </tr>
        </table>
      </div>
      <div class="owner-bar">
        <button class="btn" :disabled="busy" @click="mode = 'shop'">売り場に戻る</button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.yami {
  max-width: 900px;
  margin: 8px auto 0;
}
.yami-head {
  width: 100%;
  border-collapse: collapse;
  background: #fff;
  border: 1px solid #666;
  font-size: 11px;
  color: #333;
}
.yami-head td {
  padding: 10px;
}
.yami-label {
  background: #555555;
  color: #fff;
  text-align: center;
  width: 20%;
  font-size: 16px;
}
.money-line {
  margin-top: 4px;
  color: #663300;
}
.yami-scroll {
  margin-top: 8px;
  overflow-x: auto;
}
.yami-table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 1px;
  font-size: 11px;
  color: #333;
  background: #fff;
  border: 1px solid #666;
}
.yami-table td {
  padding: 4px 5px;
  text-align: center;
  white-space: nowrap;
}
.yami-table td.r {
  text-align: right;
}
.yami-table .hanrei {
  text-align: left;
  white-space: normal;
  font-size: 10px;
  color: #336699;
}
.yami-table .koumoku td {
  background: #ccff33;
  color: #000;
}
.yami-table .syubetu-row td {
  background: #ffff66;
  text-align: left;
}
.yami-table .item-row td {
  background: #ccff99;
}
.yami-table .hinmoku {
  text-align: left;
}
.souko {
  color: #996600;
  font-weight: bold;
}
.buy-cell {
  display: flex;
  gap: 4px;
  justify-content: center;
  align-items: center;
}
.empty {
  color: #777;
}
.owner-bar {
  margin-top: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.slot-note {
  font-size: 11px;
  color: #555;
}
.price-inp {
  width: 90px;
  font-size: 11px;
  padding: 2px;
}
</style>
