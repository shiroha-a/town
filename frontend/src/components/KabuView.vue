<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue';
import { api, type Player, type StockHolding } from '../api';

// 株取引場: A〜E株の売買。株価は全プレイヤー共有でworkerが変動させる。手数料なし。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');

const holdings = ref<StockHolding[]>([]);
const eventLog = ref<string[]>([]);
const history = ref<string[]>([]);
const message = ref('');
const kind = ref<'ok' | 'error'>('ok');
const busy = ref(false);

// 銘柄ごとの購入/売却の入力株数。
const buyQty = reactive<Record<string, number>>({});
const sellQty = reactive<Record<string, number>>({});

async function load() {
  try {
    const [s, ps] = await Promise.all([api.stocks(), api.playerStocks(props.player.id)]);
    eventLog.value = s.event_log;
    holdings.value = ps.holdings;
    history.value = ps.history;
    for (const h of ps.holdings) {
      if (buyQty[h.symbol] === undefined) buyQty[h.symbol] = 1;
      if (sellQty[h.symbol] === undefined) sellQty[h.symbol] = h.shares;
    }
  } catch (e) {
    fail(e);
  }
}
onMounted(load);

function fail(e: unknown) {
  message.value = e instanceof Error ? e.message : String(e);
  kind.value = 'error';
}

async function buy(h: StockHolding) {
  const qty = buyQty[h.symbol] ?? 0;
  if (qty <= 0) return;
  busy.value = true;
  message.value = '';
  try {
    emit('update', await api.stockBuy(props.player.id, h.symbol, qty));
    message.value = `${h.symbol}株を${qty}株購入しました。`;
    kind.value = 'ok';
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function sell(h: StockHolding) {
  const qty = sellQty[h.symbol] ?? 0;
  if (qty <= 0) return;
  busy.value = true;
  message.value = '';
  try {
    emit('update', await api.stockSell(props.player.id, h.symbol, qty));
    message.value = `${h.symbol}株を${qty}株売却しました。`;
    kind.value = 'ok';
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function settle() {
  if (!window.confirm('全ての持ち株を現在の株価で精算します。よろしいですか?')) return;
  busy.value = true;
  message.value = '';
  try {
    emit('update', await api.stockSettle(props.player.id));
    message.value = '精算しました。';
    kind.value = 'ok';
    await load();
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page kabu-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="fac-header">
      <div class="lead">
        ++ 株取引 ++ 株の買い付けは200株までです。手数料はかかりません。<br />
        ●{{ player.display_name }}さんの所持金：<span class="money">{{ yen(player.money) }}円</span>
      </div>
      <div class="title">株取引場</div>
    </div>

    <div v-if="message" :class="['message', kind]" data-test="message">{{ message }}</div>

    <div class="panel-white table-scroll">
      <table class="kabu-table">
        <thead>
          <tr>
            <th>銘柄</th>
            <th>株価</th>
            <th>保有</th>
            <th>時価</th>
            <th>平均単価</th>
            <th>含み損益</th>
            <th>購入</th>
            <th>売却</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="h in holdings" :key="h.symbol" :data-test="`stock-${h.symbol}`">
            <td class="sym">{{ h.symbol }}株</td>
            <td class="price">{{ yen(h.price) }}円</td>
            <td>{{ h.shares }}株</td>
            <td class="price">{{ yen(h.value) }}円</td>
            <td>{{ h.shares > 0 ? yen(h.avg_cost) + '円' : '-' }}</td>
            <td :class="{ up: h.unrealized > 0, down: h.unrealized < 0 }">
              {{ h.shares > 0 ? (h.unrealized > 0 ? '+' : '') + yen(h.unrealized) + '円' : '-' }}
            </td>
            <td class="act">
              <input type="number" min="1" :max="200 - h.shares" v-model.number="buyQty[h.symbol]" />
              <button class="btn mini" :disabled="busy" @click="buy(h)">購入</button>
            </td>
            <td class="act">
              <input type="number" min="0" :max="h.shares" v-model.number="sellQty[h.symbol]" />
              <button class="btn mini" :disabled="busy || h.shares <= 0" @click="sell(h)">売却</button>
            </td>
          </tr>
        </tbody>
      </table>
      <div class="settle-row">
        <button class="btn danger" :disabled="busy" @click="settle">全部精算する</button>
        <span class="hint">持ち株を現在の株価で全て売却して精算します。</span>
      </div>
    </div>

    <div class="logs">
      <div class="log-col">
        <h4>株価動向</h4>
        <ul>
          <li v-for="(m, i) in eventLog" :key="i">{{ m }}</li>
          <li v-if="!eventLog.length" class="muted">まだ動きがありません。</li>
        </ul>
      </div>
      <div class="log-col">
        <h4>売買記録</h4>
        <ul>
          <li v-for="(m, i) in history" :key="i">{{ m }}</li>
          <li v-if="!history.length" class="muted">まだ取引がありません。</li>
        </ul>
      </div>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.kabu-page {
  background-color: #999999;
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
  border: 1px solid #666;
  padding: 8px 12px;
  font-size: 12px;
  color: #333;
  line-height: 1.6;
}
.fac-header .title {
  flex: 0 0 160px;
  background: #333;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid #666;
}
.money {
  color: #cc3300;
  font-weight: bold;
}
.panel-white {
  background: #fff;
  border: 1px solid #666;
  padding: 8px;
}
.table-scroll {
  overflow-x: auto;
}
.kabu-table {
  border-collapse: collapse;
  font-size: 12px;
  white-space: nowrap;
  width: 100%;
}
.kabu-table th {
  background: #eee;
  color: #333;
  padding: 3px 6px;
  border: 1px solid #ddd;
}
.kabu-table td {
  padding: 3px 6px;
  border: 1px solid #eee;
  text-align: center;
}
.kabu-table td.sym {
  font-weight: bold;
}
.kabu-table td.price {
  text-align: right;
  color: #036;
}
.kabu-table td.up {
  color: #060;
  font-weight: bold;
}
.kabu-table td.down {
  color: #c00;
  font-weight: bold;
}
.kabu-table td.act input {
  width: 48px;
  margin-right: 4px;
}
.settle-row {
  margin-top: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.settle-row .hint {
  font-size: 11px;
  color: #667;
}
.btn.danger {
  background: #cc3333;
  color: #fff;
  border-color: #992222;
}
.btn.mini {
  padding: 1px 6px;
  font-size: 11px;
}
.logs {
  display: flex;
  gap: 8px;
  margin-top: 8px;
  flex-wrap: wrap;
}
.log-col {
  flex: 1 1 240px;
  background: #fff;
  border: 1px solid #666;
  padding: 8px;
  min-width: 220px;
}
.log-col h4 {
  margin: 0 0 4px;
  font-size: 13px;
  color: #333;
}
.log-col ul {
  margin: 0;
  padding-left: 16px;
  font-size: 11px;
  line-height: 1.7;
  max-height: 200px;
  overflow-y: auto;
}
.log-col .muted {
  color: #999;
  list-style: none;
  margin-left: -16px;
}
</style>
