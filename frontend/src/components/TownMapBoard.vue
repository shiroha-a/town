<script setup lang="ts">
import { assetUrl, type TownFacility, type TownAsset, type HouseCell } from '../api';

// 街マップの盤面。街トップ(操作あり)と入口(操作なし)で同じ絵を使うために
// 描画だけを切り出したもの。マス目のCSSは style.css に共通で置いてある。
const props = defineProps<{
  facilities: TownFacility[];
  assets: TownAsset[];
  houses: HouseCell[];
  town: number;
  skyColor: string;
  /** 施設・家・空き地を押せるようにするか(入口では false)。 */
  interactive?: boolean;
}>();

const emit = defineEmits<{
  facility: [f: TownFacility];
  house: [h: HouseCell];
  akichi: [pos: { col: number; row: number }];
}>();

const cols = Array.from({ length: 16 }, (_, i) => i + 1);
const rows = 'ABCDEFGHIJKL'.split('');

// 空き地(akichi)は施設アイコンとしては描画せず、空き地マスとして別扱いする。
const facilityAt = (col: number, row: number) =>
  props.facilities.find(
    (f) => f.key !== 'akichi' && f.town === props.town && f.col === col && f.row === row,
  );
const akichiAt = (col: number, row: number) =>
  props.facilities.some(
    (f) => f.key === 'akichi' && f.town === props.town && f.col === col && f.row === row,
  );
const houseAt = (col: number, row: number) =>
  props.houses.find((h) => h.town === props.town && h.col === col && h.row === row);
// 2マスの家に覆われている右隣のマス。原点マスの家が上にはみ出して描かれるので、
// このマスには何も置かない(背景アセットだけ敷く)。
const coveredAt = (col: number, row: number) =>
  props.houses.some(
    (h) =>
      h.town === props.town &&
      h.row === row &&
      (h.span_w ?? 1) > 1 &&
      h.col < col &&
      col < h.col + (h.span_w ?? 1),
  );
// 背景アセット(装飾レイヤー)。1マスに複数重ねられる(配列順=重ね順)。
const assetsAt = (col: number, row: number) =>
  props.assets.filter((a) => a.town === props.town && a.col === col && a.row === row);

// 家のツールチップ。家主が設定したマウスオーバーコメント(setumei)も出す。
const houseTitle = (h: HouseCell) =>
  h.setumei ? `${h.owner_name}さんの家\n「${h.setumei}」` : `${h.owner_name}さんの家`;

// 2マスの家はセルからはみ出させる。グリッドの列幅は1frなので、幅はマス数の
// 百分率で指定する(セル自体は正方形のまま=右隣のマスに重なる)。
const wideStyle = (h: HouseCell) =>
  (h.span_w ?? 1) > 1 ? { width: `${(h.span_w ?? 1) * 100}%` } : undefined;
</script>

<template>
  <div class="townmap-grid" :style="{ backgroundColor: skyColor }">
    <div class="th corner"></div>
    <div v-for="c in cols" :key="'h' + c" class="th">{{ c }}</div>
    <template v-for="(r, ri) in rows" :key="r">
      <div class="th">{{ r }}</div>
      <div v-for="c in cols" :key="r + '-' + c" class="tcell">
        <img
          v-for="(a, ai) in assetsAt(c, ri)"
          :key="'bg' + ai"
          class="cell-bg"
          :src="assetUrl(a.img)"
          alt=""
        />
        <!-- 空き地(家が建っていないakichiマス)。 -->
        <template
          v-if="akichiAt(c, ri) && !houseAt(c, ri) && !coveredAt(c, ri) && !facilityAt(c, ri)"
        >
          <button
            v-if="interactive"
            v-touch-label
            class="facility akichi-btn"
            :title="`${r}${c}（空き地）クリックで建築`"
            @click="emit('akichi', { col: c, row: ri })"
          >
            <img class="akichi-img" src="/img/svg/akiti.svg" alt="空き地" />
          </button>
          <span v-else class="facility akichi-btn">
            <img class="akichi-img" src="/img/svg/akiti.svg" alt="空き地" />
          </span>
        </template>
        <!-- 家。2マスの家は原点マスから右へはみ出して描く(house-wide)。 -->
        <template v-else-if="houseAt(c, ri)">
          <button
            v-if="interactive"
            v-touch-label
            class="facility house-cell"
            :class="{ 'house-wide': (houseAt(c, ri)!.span_w ?? 1) > 1 }"
            :style="wideStyle(houseAt(c, ri)!)"
            :title="houseTitle(houseAt(c, ri)!)"
            @click="emit('house', houseAt(c, ri)!)"
          >
            <img :src="`/img/svg/${houseAt(c, ri)!.exterior}.svg`" :alt="houseTitle(houseAt(c, ri)!)" />
          </button>
          <span
            v-else
            class="facility house-cell"
            :class="{ 'house-wide': (houseAt(c, ri)!.span_w ?? 1) > 1 }"
            :style="wideStyle(houseAt(c, ri)!)"
            :title="houseTitle(houseAt(c, ri)!)"
          >
            <img :src="`/img/svg/${houseAt(c, ri)!.exterior}.svg`" :alt="houseTitle(houseAt(c, ri)!)" />
          </span>
        </template>
        <template v-if="facilityAt(c, ri)">
          <button
            v-if="interactive"
            v-touch-label
            class="facility"
            :title="facilityAt(c, ri)!.alt"
            @click="emit('facility', facilityAt(c, ri)!)"
          >
            <img :src="`/img/svg/${facilityAt(c, ri)!.img}.svg`" :alt="facilityAt(c, ri)!.alt" />
          </button>
          <span v-else class="facility" :title="facilityAt(c, ri)!.alt">
            <img :src="`/img/svg/${facilityAt(c, ri)!.img}.svg`" :alt="facilityAt(c, ri)!.alt" />
          </span>
        </template>
      </div>
    </template>
  </div>
</template>
