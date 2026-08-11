<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { api, type Player, type ItemStack } from '../api';
import { PARAM_COLUMNS, PARAM_COLUMNS_MAIN, PARAM_COLUMNS_POWER } from '../params';
import { showToast, buildEffectLines, errorText } from '../toast';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

const busy = ref(false);

// カテゴリ別にグループ化(デパートと同じカテゴリ見出し付き表を再現)
const grouped = computed(() => {
  const g = new Map<string, ItemStack[]>();
  for (const it of props.player.items) {
    const c = it.category || 'その他';
    if (!g.has(c)) g.set(c, []);
    g.get(c)!.push(it);
  }
  return [...g.entries()];
});

// サーバ時刻とクライアント時計のずれ(ms)。カウントダウンをサーバ基準に補正し、
// 端末時計がずれていても残り時間が正しく表示されるようにする。
const skewMs = ref(0);
function syncSkew() {
  const serverNow = new Date(props.player.server_now).getTime();
  if (!Number.isNaN(serverNow)) {
    skewMs.value = serverNow - Date.now();
  }
}
syncSkew();
watch(() => props.player.server_now, syncSkew);

// 1秒ごとに進むクロック。カウントダウン表示の再計算トリガとして使う。
const nowMs = ref(Date.now());
let timer: number | undefined;
onMounted(() => {
  timer = window.setInterval(() => {
    nowMs.value = Date.now();
  }, 1000);
});
onUnmounted(() => {
  if (timer !== undefined) window.clearInterval(timer);
});

// サーバ基準の現在時刻(ms)。
const serverCorrectedNow = computed(() => nowMs.value + skewMs.value);

interface Cooldown {
  active: boolean; // クールタイム中か
  label: string; // 表示文字列
  soon: boolean; // 残り3分未満(もうすぐ使用可能)
}

const SOON_MS = 3 * 60 * 1000;

function computeCooldown(it: ItemStack, now: number): Cooldown {
  if (!it.next_available_at) {
    return { active: false, label: 'OK', soon: false };
  }
  const target = new Date(it.next_available_at).getTime();
  const remain = target - now;
  if (Number.isNaN(target) || remain <= 0) {
    return { active: false, label: 'OK', soon: false };
  }
  const totalSec = Math.ceil(remain / 1000);
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  const label = m > 0 ? `あと${m}分${String(s).padStart(2, '0')}秒` : `あと${s}秒`;
  return { active: true, label, soon: remain < SOON_MS };
}

// item_id -> クールタイム状態。nowMs更新のたびに再計算される。
const cooldowns = computed<Record<number, Cooldown>>(() => {
  const now = serverCorrectedNow.value;
  const map: Record<number, Cooldown> = {};
  for (const it of props.player.items) {
    map[it.item_id] = computeCooldown(it, now);
  }
  return map;
});

// 一括使用の対象。使える品(持つだけの品でない・クールタイムが明けている)のうち、
// 満腹度が回復する品は外す。判定はサーバー側と同じ条件だが、こちらは点数を出すため
// だけのもので、実際に何が使えたかは応答で分かる。
const bulkTargets = computed(() =>
  props.player.items.filter(
    (it) => it.usable && !it.fills_satiety && !cooldowns.value[it.item_id]?.active,
  ),
);
// まとめて耐久を減らす操作なので、誤爆しないよう確認を1段挟む。
const confirmBulk = ref(false);

async function useAll() {
  confirmBulk.value = false;
  busy.value = true;
  const before = props.player;
  try {
    const after = await api.useAll(props.player.id);
    emit('update', after);
    const r = after.use_all_result;
    const lines = buildEffectLines(before, after);
    // 使えなかったのはパワー不足の品だけ(クールタイム中の品はそもそも対象外なので
    // ここには出てこない)。どれがなぜ使えなかったかを1行ずつ出す。
    for (const s of r.skipped) lines.push(`${s.name}：${s.reason}`);
    showToast({
      variant: 'item',
      title: r.used.length > 0 ? `${r.used.length}点をまとめて使った` : '使えたものはありません',
      lines,
      icon: 'item',
    });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '使えませんでした',
      lines: [errorText(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}

async function use(it: ItemStack) {
  // クールタイム中はボタンをグレーアウトしているが、二重の安全策として弾く。
  if (cooldowns.value[it.item_id]?.active) return;
  // 持つだけの品(建築許可証・乗り物・カード類)は使えない。
  if (!it.usable) return;
  busy.value = true;
  const before = props.player;
  try {
    const after = await api.use(props.player.id, it.item_id);
    emit('update', after);
    showToast({
      variant: 'item',
      title: `${it.name}を使った`,
      lines: buildEffectLines(before, after),
      icon: 'item',
    });
  } catch (e) {
    showToast({
      variant: 'error',
      title: '使えませんでした',
      lines: [errorText(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="facility-page item-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>
    <div class="item-header">
      <div class="lead">
        持っているアイテムを使うことができます。<br />
        ●身体パワー：<span class="pw"
          >{{ player.status.energy }}/{{ player.status.energy_max }}</span
        ><br />
        ●頭脳パワー：<span class="pw"
          >{{ player.status.nou_energy }}/{{ player.status.nou_energy_max }}</span
        >
      </div>
      <div class="title">アイテム使用</div>
    </div>

    <div class="panel-white">
      <div v-if="player.items.length > 0" class="bulk">
        <template v-if="!confirmBulk">
          <button
            class="btn"
            :disabled="busy || bulkTargets.length === 0"
            data-test="use-all"
            @click="confirmBulk = true"
          >
            まとめて使う（{{ bulkTargets.length }}点）
          </button>
          <span class="bulk-hint">
            いま使える持ち物を1回ずつ使います。食べ物（満腹度が回復する品）は使いません。
          </span>
        </template>
        <template v-else>
          <span class="bulk-ask">{{ bulkTargets.length }}点を1回ずつ使います。</span>
          <button class="btn primary" :disabled="busy" data-test="use-all-go" @click="useAll()">
            使う
          </button>
          <button class="btn" :disabled="busy" @click="confirmBulk = false">やめる</button>
        </template>
      </div>
      <p v-if="player.items.length === 0" class="muted">持ち物はありません。</p>
      <div v-else class="table-scroll sticky-table">
        <table class="item-table">
          <thead>
            <tr>
              <th class="l">品名</th>
              <th>使用可</th>
              <th></th>
              <th>残り</th>
              <th v-for="c in PARAM_COLUMNS_MAIN" :key="c.key" class="p">{{ c.label }}</th>
              <th>ｶﾛﾘｰ</th>
              <th v-for="c in PARAM_COLUMNS_POWER" :key="c.key" class="p">{{ c.label }}</th>
              <th>間隔</th>
            </tr>
          </thead>
          <template v-for="[cat, list] in grouped" :key="cat">
            <tbody>
              <tr class="cat-row">
                <td :colspan="PARAM_COLUMNS.length + 6">
                  <span class="cat-name">●{{ cat }}</span>
                </td>
              </tr>
              <template v-for="it in list" :key="it.item_id">
                <tr :data-test="`item-${it.item_id}`">
                  <!-- 効果行がある品は、品名/使用可/使うを2行にまたがせてどの品の効果か分かるようにする
                     (レガシー depart.cgi の rowspan=2 と同じ)。 -->
                  <td class="l" :rowspan="it.special ? 2 : 1">○{{ it.name }}</td>
                  <td
                    class="cooldown"
                    :class="{
                      ok: it.usable && !cooldowns[it.item_id].active,
                      soon: it.usable && cooldowns[it.item_id].active && cooldowns[it.item_id].soon,
                      wait:
                        it.usable && cooldowns[it.item_id].active && !cooldowns[it.item_id].soon,
                      hold: !it.usable,
                    }"
                    :data-test="`cooldown-${it.item_id}`"
                    :rowspan="it.special ? 2 : 1"
                  >
                    {{ it.usable ? cooldowns[it.item_id].label : '－' }}
                  </td>
                  <td :rowspan="it.special ? 2 : 1">
                    <button
                      v-if="it.usable"
                      class="btn"
                      :disabled="busy || cooldowns[it.item_id].active"
                      :data-test="`use-${it.item_id}`"
                      @click="use(it)"
                    >
                      使う
                    </button>
                  </td>
                  <td>{{ it.remaining_uses }}{{ it.durability_unit === 'day' ? '日' : '回' }}</td>
                  <td
                    v-for="c in PARAM_COLUMNS_MAIN"
                    :key="c.key"
                    class="p"
                    :class="{ up: (it.params[c.key] ?? 0) > 0 }"
                  >
                    {{ it.params[c.key] ?? 0 }}
                  </td>
                  <td>{{ it.calorie_g > 0 ? it.calorie_g : '-' }}</td>
                  <td
                    v-for="c in PARAM_COLUMNS_POWER"
                    :key="c.key"
                    class="p"
                    :class="{ up: (it.params[c.key] ?? 0) > 0 }"
                  >
                    {{ it.params[c.key] ?? 0 }}
                  </td>
                  <td class="interval">{{ it.interval_min > 0 ? `${it.interval_min}分` : '-' }}</td>
                </tr>
                <!-- 特殊効果(体重/身長/病気)は列に収まらないので、レガシー depart.cgi の
                   備考行と同じく1行下にcolspanで出す。 -->
                <tr v-if="it.special" class="special-row" :data-test="`special-${it.item_id}`">
                  <td :colspan="PARAM_COLUMNS.length + 3">
                    <span class="cat-name">【 効果 】{{ it.special }}</span>
                  </td>
                </tr>
              </template>
            </tbody>
          </template>
        </table>
      </div>
    </div>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
/* 一括使用の行。表の上に置く。 */
.bulk {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px 10px;
  margin-bottom: 8px;
}
.bulk-hint {
  font-size: 11px;
  color: #666;
  line-height: 1.5;
}
.bulk-ask {
  font-size: 12px;
  color: #a0308c;
}
.item-page {
  background-color: #ffcc66;
  /* 旧command_bak.gifのCSS再現: 6px周期の1pxライン */
  background-image: repeating-linear-gradient(
    180deg,
    transparent 0 2px,
    #ffcc33 2px 3px,
    transparent 3px 6px
  );
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.item-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.item-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  color: #333;
  line-height: 1.6;
}
.item-header .pw {
  color: #cc3300;
  font-weight: bold;
}
.item-header .title {
  flex: 0 0 140px;
  background: #cc9933;
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
  padding: 12px;
}
.table-scroll {
  --stick-line: #e0c080;
  --stick-bg: #fff;
  overflow-x: auto;
}
.item-table {
  border-collapse: collapse;
  font-size: 11px;
  white-space: nowrap;
}
.item-table th {
  background: #ffe0a3;
  color: #663300;
  padding: 2px 4px;
  border: 1px solid #e0c080;
}
.item-table td {
  padding: 2px 4px;
  border: 1px solid #eee;
  text-align: center;
}
.item-table th.l,
.item-table td.l {
  text-align: left;
}
.item-table th.p,
.item-table td.p {
  width: 20px;
  color: #999;
}
.item-table td.p.up {
  color: #060;
  font-weight: bold;
  background: #eaffea;
}
.item-table tr.cat-row td {
  background: #ffedcc;
  color: #995500;
  font-weight: bold;
  text-align: left;
  border-top: 2px solid #cc9933;
}
.item-table td.cooldown {
  font-weight: bold;
}
.item-table td.cooldown.ok {
  color: #060;
}
/* 持っていること自体が意味を持つ品(建築許可証・乗り物・カード類)。
   使っても何も起きず耐久だけ減るので「使う」ボタンを出さない。 */
.item-table td.cooldown.hold {
  color: #aaa;
  font-weight: normal;
}
.item-table td.cooldown.soon {
  color: #0a7d2c;
  background: #eaffea;
}
.item-table td.cooldown.wait {
  color: #a0308c;
  background: #fbeaf6;
}
.btn:disabled {
  background: #ccc;
  color: #888;
  border-color: #bbb;
  cursor: not-allowed;
  opacity: 0.7;
}
.item-table tr.special-row td {
  background: #fff6e0;
  color: #995500;
  text-align: left;
  /* 本行(11px)より一段小さくし、行間も詰めて注記として従属させる */
  font-size: 10px;
  line-height: 1.25;
  padding: 0 8px 1px;
}
</style>
