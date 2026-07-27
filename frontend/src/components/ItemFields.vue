<script setup lang="ts">
import type { AdminItemInput } from '../api';
import ToggleSwitch from './ToggleSwitch.vue';

// アイテムの設定項目(使用効果を除く)。作成フォームと編集モーダルで同じものを
// 出したいので切り出した。渡されたオブジェクトを直接書き換える。
defineProps<{ item: AdminItemInput; edit?: boolean }>();

// 扱う場所。空とhanbaiは買うと持ち物になり、それ以外はその場で消費するメニュー。
const FACILITIES = [
  { value: '', label: '店（デパート）' },
  { value: 'hanbai', label: '自動販売機' },
  { value: 'syokudou', label: '食堂' },
  { value: 'gym', label: 'ジム' },
  { value: 'onsen', label: '温泉' },
  { value: 'school', label: '学校' },
  { value: 'kyushitu', label: '教室' },
];
</script>

<template>
  <label>品名<input v-model="item.name" placeholder="例: 特製栄養ドリンク" /></label>
  <label>カテゴリ<input v-model="item.category" placeholder="例: ドリンク" /></label>
  <label>
    扱う場所
    <select v-model="item.facility">
      <option v-for="f in FACILITIES" :key="f.value" :value="f.value">{{ f.label }}</option>
    </select>
  </label>
  <div class="hint">
    店と自動販売機の品は買うと持ち物になります。それ以外はその場で消費するメニューで、持ち物には入りません。
    <template v-if="edit">
      <b>持っている人がいる品で変えると、その持ち物が宙に浮きます。</b>
    </template>
  </div>
  <label>値段<input type="number" v-model.number="item.price" /></label>
  <label>
    標準在庫数<input type="number" v-model.number="item.stock_master" placeholder="空=無制限" />
  </label>

  <div class="fields-head">持ち物としての扱い</div>
  <div class="field-row">
    <label class="narrow"
      >耐久<input type="number" min="1" v-model.number="item.durability"
    /></label>
    <label class="narrow">
      単位
      <select v-model="item.durability_unit">
        <option value="use">回（使うと減る）</option>
        <option value="day">日（日数で減る）</option>
      </select>
    </label>
    <label class="narrow">
      所持上限<input type="number" min="1" v-model.number="item.max_sets" />
    </label>
    <label class="narrow">
      使用間隔(分)<input type="number" min="0" v-model.number="item.use_interval_min" />
    </label>
  </div>

  <div class="fields-head">飲食・消費</div>
  <div class="field-row">
    <label class="narrow">
      カロリー(g)<input type="number" min="0" v-model.number="item.calorie_g" />
    </label>
    <label class="narrow">
      身体パワー消費<input type="number" min="0" v-model.number="item.body_cost" />
    </label>
    <label class="narrow">
      頭脳パワー消費<input type="number" min="0" v-model.number="item.nou_cost" />
    </label>
  </div>
  <ToggleSwitch v-model="item.fills_satiety" label="食べると満腹になる" />
  <div class="hint">
    カテゴリが「食料品」「ファーストフード」の品は、オフでも満腹になります（設定もれの保険）。
  </div>

  <div class="fields-head">特殊な扱い</div>
  <div class="field-row">
    <label class="narrow">
      温泉の回復倍率<input type="number" min="0" v-model.number="item.power_multiplier" />
    </label>
    <label class="narrow">
      建築許可証の幅<input type="number" min="0" v-model.number="item.build_span" />
    </label>
  </div>
  <div class="hint">
    温泉の回復倍率は温泉のお風呂だけで使います（0＝温泉ではない）。建築許可証の幅は0が通常のアイテムで、2にすると2マスの家を建てられる許可証になります。
  </div>
  <ToggleSwitch v-model="item.enables_credit" label="持っているとクレジット払いができる" />
  <ToggleSwitch v-model="item.is_gift" label="ギフト屋で包んだ状態の品" />

  <div class="fields-head">公開の設定</div>
  <ToggleSwitch v-if="edit" v-model="item.enabled" label="有効（オフで無効化）" />
  <ToggleSwitch v-model="item.shop_listed" label="店に並べる（オフで販売しない）" />
  <div class="hint">
    オフにすると店の品揃えと卸問屋から外れ、買えなくなります。シリアルコードやイベントでは配れます。
  </div>
  <ToggleSwitch v-model="item.usable" label="使える（オフで持つだけの品）" />
  <div class="hint">
    建築許可証・乗り物・カード類のように、持っていること自体が意味を持つ品はオフにします。使っても何も起きず耐久だけ減るためです。
  </div>
</template>

<style scoped>
/* 親(AdminView)のスタイルはscopedで届かないので、フォームの見た目はここで持つ。 */
label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  margin-bottom: 6px;
  color: #445;
}
label input,
label select {
  flex: 1 1 auto;
  min-width: 0;
  padding: 2px 4px;
}
.hint {
  font-size: 11px;
  color: #889;
  line-height: 1.5;
  margin: -2px 0 6px;
}
.hint b {
  color: #a33;
}
.fields-head {
  margin: 10px 0 2px;
  font-size: 11px;
  font-weight: bold;
  color: #667;
  border-bottom: 1px solid #dde;
  padding-bottom: 2px;
}
.field-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 10px;
}
/* 横並びの入力欄。ラベルは折り返さず、幅が足りなければ入力欄側が縮み、
   それでも足りなければ行を折り返す(「耐 久」のような分断を防ぐ)。 */
.field-row .narrow {
  flex: 1 1 150px;
  min-width: 0;
  white-space: nowrap;
}
.field-row .narrow input,
.field-row .narrow select {
  flex: 1 1 40px;
}
</style>
