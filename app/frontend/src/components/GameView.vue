<script setup lang="ts">
import { ref, computed, type Component } from 'vue';
import { type Player } from '../api';
import SaikoroGame from './casino/SaikoroGame.vue';
import LotoGame from './casino/LotoGame.vue';
import SlotGame from './casino/SlotGame.vue';
import KujiGame from './casino/KujiGame.vue';
import DonutsGame from './casino/DonutsGame.vue';
import OmikujiGame from './casino/OmikujiGame.vue';
import OtakaraGame from './casino/OtakaraGame.vue';
import FukubikiGame from './casino/FukubikiGame.vue';
import ScratchGame from './casino/ScratchGame.vue';
import BlackjackGame from './casino/BlackjackGame.vue';
import PokerGame from './casino/PokerGame.vue';
import Loto6Game from './casino/Loto6Game.vue';

defineProps<{ player: Player }>();
const emit = defineEmits<{ update: [player: Player]; back: [] }>();

// ゲーム一覧。新しいゲームはこの配列と gameComponents の両方に登録する。
// props は選択時にコンポーネントへ渡す追加プロパティ(スクラッチのgame種別など)。
const games: { key: string; name: string; desc: string; props?: Record<string, unknown> }[] = [
  { key: 'saikoro', name: 'サイコロ', desc: '2つのサイコロの合計が偶数か奇数かを当てる(1:1)' },
  { key: 'loto', name: 'ロト', desc: '6桁の数字を予想し、位置ごとの一致桁数で配当(最大×1000)' },
  { key: 'slot', name: 'スロット', desc: '8ラインスロット、絵柄を揃えて最大×7777' },
  { key: 'kuji', name: 'くじ', desc: '2択ダブルアップ、連勝で配当が2倍ずつ増える' },
  { key: 'donuts', name: 'ドーナツ', desc: 'Hi&Lo、前のカードより大きいか小さいかを当てる' },
  { key: 'omikuji', name: 'おみくじ', desc: 'お賽銭と占う項目を選び、運勢でステータス・金運が変わる' },
  { key: 'otakara', name: 'お宝', desc: '宝箱を選んで代金を払い、アイテムやステータスを得る' },
  { key: 'fukubiki', name: '福引き', desc: 'カードを選んで景品が当たる(無料)' },
  { key: 'scratch', name: 'スクラッチ', desc: '1日5枚、3マス開けて当たりを狙う(無料)', props: { game: 'scratch' } },
  { key: 'sukuratti', name: 'スクラッチ2', desc: '3x3の9マス版スクラッチ(無料)', props: { game: 'sukuratti' } },
  { key: 'blackjack', name: 'ブラックジャック', desc: '21に近づけてディーラーに勝つ(配当1:1)' },
  { key: 'poker', name: 'ポーカー', desc: '5カードドロー、役でポイントを増やして換金' },
  { key: 'loto6', name: 'ロト6', desc: '1〜36から6個選んで購入、毎日抽選で銀行に賞金' },
];
const gameComponents: Record<string, Component> = {
  saikoro: SaikoroGame,
  loto: LotoGame,
  slot: SlotGame,
  kuji: KujiGame,
  donuts: DonutsGame,
  omikuji: OmikujiGame,
  otakara: OtakaraGame,
  fukubiki: FukubikiGame,
  scratch: ScratchGame,
  sukuratti: ScratchGame,
  blackjack: BlackjackGame,
  poker: PokerGame,
  loto6: Loto6Game,
};

const yen = (n: number) => n.toLocaleString('ja-JP');
const selected = ref<string | null>(null);
const current = computed(() => (selected.value ? gameComponents[selected.value] : null));
const currentProps = computed(() => games.find((g) => g.key === selected.value)?.props ?? {});
</script>

<template>
  <div class="casino-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>
    <div class="casino-header">
      <div class="title">ゲームセンター</div>
      <div class="money">所持金：{{ yen(player.money) }}円</div>
    </div>

    <template v-if="!selected">
      <div class="game-menu">
        <button v-for="g in games" :key="g.key" class="game-card" @click="selected = g.key">
          <div class="gname">{{ g.name }}</div>
          <div class="gdesc">{{ g.desc }}</div>
        </button>
      </div>
    </template>
    <template v-else>
      <button class="btn menu-back" @click="selected = null">← ゲーム選択にもどる</button>
      <component :is="current" :player="player" v-bind="currentProps" @update="emit('update', $event)" />
    </template>
  </div>
</template>

<style scoped>
.casino-page {
  background: #1a0a2a;
  min-height: 80vh;
  padding: 6px;
}
.btn.back {
  margin-bottom: 6px;
}
.casino-header {
  text-align: center;
  margin-bottom: 14px;
}
.casino-header .title {
  display: inline-block;
  font-size: 26px;
  font-weight: bold;
  letter-spacing: 8px;
  color: #ffd700;
  text-shadow:
    0 0 8px #ff8c00,
    0 0 2px #fff;
  padding: 6px 24px;
  border: 2px solid #ffd700;
  border-radius: 8px;
  background: linear-gradient(180deg, #33104d, #1a0a2a);
}
.casino-header .money {
  margin-top: 6px;
  color: #ffe;
  font-size: 13px;
}
.game-menu {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 10px;
  max-width: 760px;
  margin: 0 auto;
}
.game-card {
  text-align: left;
  padding: 12px 14px;
  border: 1px solid #7a5cff;
  border-radius: 8px;
  background: linear-gradient(180deg, #2a1650, #1c0e38);
  color: #fff;
  cursor: pointer;
}
.game-card:hover {
  border-color: #ffd700;
  background: linear-gradient(180deg, #3a1f6b, #24123f);
}
.game-card .gname {
  font-size: 16px;
  font-weight: bold;
  color: #ffd700;
  margin-bottom: 4px;
}
.game-card .gdesc {
  font-size: 12px;
  color: #cbb8ff;
}
.menu-back {
  margin-bottom: 10px;
}
</style>
