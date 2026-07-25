<script setup lang="ts">
import { ref } from 'vue';
import { api, type Player } from '../../api';

const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player] }>();

const yen = (n: number) => n.toLocaleString('ja-JP');

// 宝箱の一覧(banはotakara.goのotakaraBoxesと対応)。安い順に並べる。
const boxes = [
  { key: 'copper', name: '銅の箱', cost: 500 },
  { key: 'silver', name: '銀の箱', cost: 1000 },
  { key: 'special', name: 'スペシャル', cost: 500000 },
  { key: 'gold', name: '金の箱', cost: 1000000 },
] as const;

const busy = ref(false);
const message = ref('');

interface ParamDelta {
  param: string;
  amount: number;
}
interface OtakaraDetail {
  box: string;
  cost: number;
  prize: string;
  kind: string; // "item" | "param" | "money"
  item?: string;
  params?: ParamDelta[];
  money?: number;
}

const result = ref<(OtakaraDetail & { win: boolean; net: number }) | null>(null);

// 16スキルの表示名(api.tsのParamsと対応)。
const paramLabels: Record<string, string> = {
  kokugo: '国語',
  suugaku: '数学',
  rika: '理科',
  syakai: '社会',
  eigo: '英語',
  ongaku: '音楽',
  bijutsu: '美術',
  looks: 'ルックス',
  tairyoku: '体力',
  kenkou: '健康',
  speed: 'スピード',
  power: 'パワー',
  wanryoku: '腕力',
  kyakuryoku: '脚力',
  love: 'ラブ',
  omoshirosa: '面白さ',
};

const boxLabels: Record<string, string> = {
  copper: '銅の箱',
  silver: '銀の箱',
  special: 'スペシャル',
  gold: '金の箱',
};

async function play(box: { key: string; cost: number }) {
  if (busy.value) return;
  busy.value = true;
  message.value = '';
  try {
    const res = await api.casinoPlay(props.player.id, 'otakara', box.cost, { box: box.key });
    emit('update', res.player);
    const d = res.detail as unknown as OtakaraDetail;
    result.value = { ...d, win: res.win, net: res.payout - box.cost };
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="cg">
    <h3 class="cg-title">お宝</h3>
    <p class="cg-lead">
      宝箱を選んで箱代を払うと、その箱に応じた宝(アイテム・ステータス・お金)が1つ手に入る。
    </p>

    <div v-if="result" class="cg-result" :class="result.win ? 'win' : 'lose'" data-test="result">
      <div class="ota-box">{{ boxLabels[result.box] ?? result.box }}を開けた！</div>
      <div class="ota-prize">{{ result.prize }}</div>
      <div class="ota-detail">
        <template v-if="result.kind === 'item'">
          アイテム「{{ result.item }}」を手に入れた！
        </template>
        <template v-else-if="result.kind === 'param'">
          <span v-for="(pd, i) in result.params" :key="i" class="ota-stat">
            {{ paramLabels[pd.param] ?? pd.param }} {{ pd.amount >= 0 ? '+' : '' }}{{ pd.amount }}
          </span>
        </template>
        <template v-else> {{ yen(result.money ?? 0) }}円が入っていた！ </template>
      </div>
      <div class="ota-net">収支 {{ result.net >= 0 ? '+' : '' }}{{ yen(result.net) }}円</div>
    </div>

    <div class="cg-controls">
      <button
        v-for="b in boxes"
        :key="b.key"
        class="btn ota-btn"
        :class="`box-${b.key}`"
        :disabled="busy || props.player.money < b.cost"
        :data-test="b.key"
        @click="play(b)"
      >
        <span class="ota-name">{{ b.name }}</span>
        <span class="ota-cost">{{ yen(b.cost) }}円</span>
      </button>
    </div>

    <p class="cg-note">
      スペシャルは全ての宝から抽選される(安い宝も高級な宝も等確率)。
    </p>

    <div v-if="message" class="message error">{{ message }}</div>
  </div>
</template>

<style scoped>
.cg {
  max-width: 560px;
  margin: 0 auto;
  background: #fff;
  border: 1px solid #7a5cff;
  border-radius: 8px;
  padding: 14px 16px;
}
.cg-title {
  margin: 0 0 4px;
  color: #6a2fb5;
}
.cg-lead {
  font-size: 12px;
  color: #555;
  margin: 0 0 12px;
}
.cg-result {
  text-align: center;
  padding: 14px;
  border-radius: 6px;
  margin-bottom: 12px;
  font-size: 15px;
}
.cg-result.win {
  background: #eaffea;
  color: #067a06;
}
.cg-result.lose {
  background: #ffecec;
  color: #cc2200;
}
.ota-box {
  font-size: 13px;
  margin-bottom: 4px;
}
.ota-prize {
  font-size: 20px;
  font-weight: bold;
  margin-bottom: 6px;
}
.ota-detail {
  margin-bottom: 6px;
}
.ota-stat {
  display: inline-block;
  margin: 0 6px;
  font-weight: bold;
}
.ota-net {
  font-weight: bold;
}
.cg-controls {
  display: flex;
  gap: 8px;
  align-items: stretch;
  flex-wrap: wrap;
}
.ota-btn {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  padding: 8px 12px;
  border: 1px solid #7a5cff;
  border-radius: 6px;
  background: #f6f3ff;
  color: #4a2a8a;
  cursor: pointer;
  min-width: 96px;
}
.ota-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.ota-name {
  font-weight: bold;
}
.ota-cost {
  font-size: 11px;
  color: #666;
}
.box-copper {
  border-color: #b08d57;
}
.box-silver {
  border-color: #9aa0a6;
}
.box-gold {
  border-color: #d4af37;
}
.box-special {
  border-color: #e0457b;
}
.cg-note {
  font-size: 11px;
  color: #777;
  margin: 10px 0 0;
}
.message.error {
  margin-top: 10px;
  color: #cc2200;
  font-size: 13px;
}
</style>
