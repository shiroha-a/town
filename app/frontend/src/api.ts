// Thin fetch wrapper around the backend REST API. In dev, /api is proxied to
// the Go backend by Vite (see vite.config.ts).

export interface ItemStack {
  item_id: number;
  name: string;
  category: string;
  quantity: number;
  remaining_uses: number;
  sets: number;
  durability_unit: string; // 'use'(回) or 'day'(日)
  money: number;
  params: Record<string, number>;
  interval_min: number;
  calorie_g: number; // 摂取カロリー(食べると体重+calorie_g g)
  // クールタイム中の再使用可能時刻(ISO8601)。使用可能ならnull。
  next_available_at: string | null;
}

export interface Params {
  kokugo: number;
  suugaku: number;
  rika: number;
  syakai: number;
  eigo: number;
  ongaku: number;
  bijutsu: number;
  looks: number;
  tairyoku: number;
  kenkou: number;
  speed: number;
  power: number;
  wanryoku: number;
  kyakuryoku: number;
  love: number;
  omoshirosa: number;
}

export interface Player {
  id: number;
  instance_host: string;
  remote_user_id: string;
  display_name: string;
  roles: string[];
  money: number;
  savings: number;
  super_savings: number;
  loan_daily: number;
  loan_count: number;
  current_town: number;
  status: {
    energy: number;
    energy_max: number;
    nou_energy: number;
    nou_energy_max: number;
    job: string;
    job_level: number;
    job_exp: number;
    job_kaisuu: number;
    mastered_jobs: string[];
    satiety: number;
    height_cm: number;
    weight_g: number;
    bmi: number;
    body_type: string;
    disease_index: number;
    disease_name: string;
    condition: string;
    work_available_at: string | null;
    energy_full_at: string | null;
    nou_energy_full_at: string | null;
    onsen_multiplier: number;
  };
  params: Params;
  items: ItemStack[];
  item_kind_limit: number; // 所持できるアイテムの種類上限(0=無制限)
  // サーバの現在時刻(ISO8601)。クライアント時計のずれを補正しカウントダウンに使う。
  server_now: string;
}

export interface JobOption {
  id: number;
  name: string;
  pay: number;
  salary: number;
  rank: number;
  require_master: string;
  requirements: Record<string, number>;
  work_params: Record<string, number>;
  energy_cost: number;
  nou_energy_cost: number;
  pay_interval: number;
}

export interface ShopItem {
  id: number;
  name: string;
  category: string;
  price: number;
  money: number;
  params: Record<string, number>;
  interval_min: number;
  durability: number;
  durability_unit: string; // 'use'(回) or 'day'(日)
  power_multiplier: number; // 温泉の回復速度倍率(0=温泉ではない)
  calorie_g: number; // 摂取カロリー(食べると体重+calorie_g g)
  stock: number; // 本日の店頭在庫(-1=無制限)
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  headers?: Record<string, string>,
): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    method,
    headers: { ...(body ? { 'Content-Type': 'application/json' } : {}), ...(headers ?? {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    throw new ApiError(res.status, data?.error ?? `HTTP ${res.status}`);
  }
  return data as T;
}

// クライアント側で冪等キーを生成する(同一操作の二重送信対策)。
// crypto.randomUUIDはセキュアコンテキスト(HTTPS/localhost)限定のため、
// HTTP経由の非localhostアクセスでも動くようフォールバックを持つ。
export function newIdempotencyKey(): string {
  const c = globalThis.crypto;
  if (c && typeof c.randomUUID === 'function') {
    return c.randomUUID();
  }
  return `k-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}

// 住民名鑑(公開一覧)の1件。
export interface PublicSummary {
  id: number;
  display_name: string;
  job: string;
  job_level: number;
  created_at: string; // 入居日
}
// 公開プロフィール(お金/身元などの非公開項目は含まない)。
export interface PublicProfile {
  id: number;
  display_name: string;
  created_at: string;
  status: Player['status'];
  params: Params;
}

// 役場: 街のニュース/住民の出来事の1件。
export interface NewsEntry {
  id: number;
  kind: string; // 入居/就職/家/イベント/当選
  actor_id: number | null;
  actor_name: string;
  message: string;
  good: boolean | null; // イベントの良悪。null=中立
  at: string;
}
// 役場: ランキング種別。
export interface RankingKey {
  key: string;
  label: string;
  unit: string;
}
export interface RankingEntry {
  rank: number;
  id: number;
  display_name: string;
  job: string;
  job_level: number;
  value: number;
}
export interface RankingResult extends RankingKey {
  entries: RankingEntry[];
  self: RankingEntry | null; // 圏外のときだけ入る自分の行
}

// 仕事1回の結果サマリ(給料・昇給・ボーナス・経験値)。
export interface WorkResult {
  exp_gained: number;
  new_level: number;
  leveled_up: boolean;
  this_salary: number;
  pay: number;
  pay_every: number;
  bonus: number;
  work_bonus: number;
  weight_loss_g: number;
  mastered: string[];
}
export type WorkResponse = Player & { work_result: WorkResult };

// 口座の入出金明細1行(普通/スーパー定期は別々に取得する)。
export interface StatementEntry {
  at: string;
  label: string;
  amount: number;
  balance: number;
}

// ミニゲーム(カジノ)1プレイの結果。detailはゲーム別の結果詳細。
export interface CasinoPlayResult {
  player: Player;
  payout: number;
  win: boolean;
  detail: Record<string, unknown>;
}

// スクラッチのカード1枚。valuesは開封済みセルのindex->値(未開封は含まない)。
export interface ScratchCard {
  index: number;
  values: Record<number, number>;
  opened: number;
  finished: boolean;
  atari: number;
}
export interface ScratchState {
  game: string;
  cols: number;
  cells: number;
  atari_max: number;
  open_max: number;
  cards: ScratchCard[];
}
export interface ScratchOpenResult {
  player: Player;
  value: number;
  win: boolean;
  bonus: boolean;
  prize: number;
  state: ScratchState;
}

// ブラックジャックの盤面。進行中は親(oya)の1枚目のみ公開しoya_hidden枚が伏せ。
export interface BJState {
  active: boolean;
  rate: number;
  ply: number[];
  ply_score: number;
  oya: number[];
  oya_score: number;
  oya_hidden: number;
  phase: string; // 'playing' | 'over'
  result: string; // 'win' | 'lose' | 'push'
  payout: number;
}

// ポーカーの状態。phase: none(未購入)/ready(配札前)/dealt(交換前)。
export interface PokerState {
  active: boolean;
  points: number;
  hand: number[];
  phase: string;
  result: number; // 直近の役(-1=未判定)
  result_name: string;
  gain: number; // 直近のポイント増減
}

// ロト6の状態(自分の当日の購入券・上限・直近抽選)。
export interface Loto6Ticket {
  numbers: number[];
}
export interface Loto6DrawInfo {
  date: string;
  winning: number[];
}
export interface Loto6State {
  my_tickets: Loto6Ticket[];
  today_count: number;
  daily_limit: number;
  cost: number;
  last_draw: Loto6DrawInfo | null;
}

// ローンの返済プラン1件(返済回数・利率・日額・総返済額)。
export interface LoanPlanQuote {
  count: number;
  rate: number;
  daily: number;
  total: number;
}
// ローンの借入見積り(借入可能額と各返済プラン)。
export interface LoanQuote {
  limit: number;
  has_loan: boolean;
  plans: LoanPlanQuote[];
}

// 効果エンジンのop(add_param / add_money)。
export interface EffectOp {
  op: 'add_param' | 'add_money';
  param?: string;
  amount: number;
}
// 条件(param_gte)。
export interface Condition {
  pred: 'param_gte';
  param: string;
  value: number;
}
export interface AdminItem {
  id: number;
  name: string;
  category: string;
  price: number;
  effect: EffectOp[];
  enabled: boolean;
  stock_master: number | null; // 標準在庫数(null=無制限)
}
export interface AdminJob {
  id: number;
  name: string;
  requirements: Condition[];
  effect: EffectOp[];
  salary: number;
  pay_interval: number;
  bonus_rate: number;
  raise_rate: number;
  rank: number;
  require_master: string;
  body_cost: number;
  nou_cost: number;
  enabled: boolean;
}
// 職業の作成/更新ペイロード。
export interface JobPayload {
  name: string;
  requirements: Condition[];
  effect: EffectOp[];
  salary: number;
  pay_interval: number;
  bonus_rate: number;
  raise_rate: number;
  rank: number;
  require_master: string;
  body_cost: number;
  nou_cost: number;
  enabled: boolean;
}
export interface SimResult {
  plan: {
    money_delta: number;
    params: { name: string; old_value: number; new_value: number }[];
  };
  warnings: string[];
}

export interface AdminPlayerSummary {
  id: number;
  display_name: string;
  roles: string[];
  money: number;
  job: string;
  job_level: number;
}
// プレイヤーの管理者編集ペイロード。
export interface AdminPlayerPayload {
  display_name: string;
  money: number;
  is_admin: boolean;
  params: Params;
  energy: number;
  nou_energy: number;
  satiety: number;
  job: string;
  job_level: number;
  job_exp: number;
  disease_index: number;
  height_cm: number;
  weight_g: number;
}

export interface GameSettings {
  initial_money: number;
  daily_interest_permille: number;
  energy_recovery_sec: number;
  nou_recovery_sec: number;
  satiety_decay_sec: number;
  condition_eval_interval_min: number;
  work_interval_min: number;
  debug_no_cooldown: boolean;
  depart_daily_count: number;
  syokudou_daily_count: number;
  item_kind_limit: number;
  stock_adjust: number;
  move_maigo_enabled: boolean;
  move_walk_secs: number;
  move_bus_secs: number;
  towns: TownConfig[]; // 街の一覧(round-trip用。編集は専用エディタ)
}

// 街設定(名前・地価・隠し町)。街番号は並び順(0始まり)。
export interface TownConfig {
  name: string;
  land_price: number;
  hidden: boolean; // ワープ不可の隠し町
}

// 街(番号付き)。GET /towns の戻り値。
export interface Town {
  no: number;
  name: string;
  land_price: number;
  hidden: boolean;
}

export interface TownFacility {
  key: string;
  img: string;
  alt: string;
  town: number;
  col: number;
  row: number;
  dest: number; // 移動施設(key=walk/bus)の行き先の街
  ready: boolean;
}

// 背景アセット(装飾レイヤー)。機能を持たず、施設レイヤーの下にセル単位で敷く。
export interface TownAsset {
  img: string;
  town: number;
  col: number;
  row: number;
}

// 施設プリセット(管理画面): 画像・表示名・遷移先を保存したテンプレート。
// D&Dでマップに配置すると施設になる。
export interface FacilityPreset {
  key: string;
  img: string;
  alt: string;
  dest: number;
}

// カスタムイベントの発生条件(すべて満たすプレイヤーにだけ発生)。
export interface EventCond {
  pred: string; // money_gte/money_lte/param_gte/param_lte/has_item/job_is
  param?: string;
  value?: number;
  item_id?: number;
  job?: string;
}

// カスタムイベント(管理画面): 組み込みのランダムイベントプールに合流する。
// 金額は[money_min, money_max]の一様乱数、disease_setは病気指数の直接代入。
export interface AdminEvent {
  id: number;
  name: string;
  message: string;
  good: boolean;
  money_min: number;
  money_max: number;
  params: Record<string, number>;
  disease_set: number | null;
  weight_g: number;
  weight: number;
  enabled: boolean;
  conditions: EventCond[];
}

// 街移動の結果。徒歩/自転車の能力上昇、乗り物、事故、迷子などを含む。
export interface MoveResult {
  arrived_town: number;
  means: string;
  vehicle: string; // 使った乗り物名(徒歩なら空)
  fare: number;
  travel_secs: number; // 移動時間(秒)。到着までのカウントダウンに使う
  stat_gains: Record<string, number>;
  accident: boolean;
  accident_item: string;
  lost: boolean;
}
export type MoveResp = Player & { move_result: MoveResult };

// 家の店の購入結果(ご近所キャッシュバック等)。
export interface BuyResult {
  total: number;
  cashback: number;
  paid: number;
  method: string; // 'cash'/'credit'
}
export type BuyResp = Player & { buy_result: BuyResult };

// ワープ料金(円)。バックエンド action.WarpFee と一致させること。
export const WARP_FEE = 100000;

// 背景アセット画像のURLを解決する。'u:'接頭辞はアップロード画像(DB配信)、
// それ以外は組み込みのpublic/img/svg/*.svg(GIFからSVG化済み)。
export function assetUrl(img: string): string {
  return img.startsWith('u:') ? `/api/v1/assets/${encodeURIComponent(img.slice(2))}` : `/img/svg/${img}.svg`;
}

export interface StockPrice {
  symbol: string;
  price: number;
}
export interface StockHolding {
  symbol: string;
  price: number;
  shares: number;
  value: number;
  cost_total: number;
  avg_cost: number;
  unrealized: number;
  inv_total: number;
  ret_total: number;
  net: number;
}
export interface StocksResp {
  prices: StockPrice[];
  event_log: string[];
}
export interface PlayerStocksResp {
  holdings: StockHolding[];
  history: string[];
}

export interface KeibaHorse {
  name: string;
  img: string;
  odds: number;
}
export interface KeibaRankEntry {
  name: string;
  profit: number;
  invested: number;
  won: number;
}
export interface KeibaRaceResp {
  race_id: number;
  lineup: KeibaHorse[];
  ranking: KeibaRankEntry[];
}
export interface KeibaResult {
  winner_index: number;
  winner_name: string;
  winner_odds: number;
  payout: number;
  invested: number;
  steps: number[][];
  lineup: KeibaHorse[];
}
export interface KeibaBetResp {
  player: Player;
  result: KeibaResult;
}

export interface MailMessage {
  id: number;
  direction: string;
  counterpart_id: number | null;
  counterpart_name: string;
  body: string;
  sent_at: string;
  saved: boolean;
  unread: boolean;
}
export interface MailContact {
  id: number;
  name: string;
}
export interface Mailbox {
  received: MailMessage[];
  sent: MailMessage[];
  address_book: MailContact[];
  unread: number;
}

export interface Greeting {
  id: number;
  user_id: number | null;
  user_name: string;
  category: string;
  body: string;
  color: string;
  janken: string;
  posted_at: string;
}
export interface GreetResult {
  reward: number;
  jackpot: boolean;
  janken: string;
  janken_pc: string;
  fine: boolean;
}
export interface GreetResp {
  player: Player;
  result: GreetResult;
}

export interface AttendanceMember {
  id: number;
  name: string;
  cells: string[]; // present | absent | blank(dates と同順)
}
export interface AttendanceRank {
  name: string;
  present: number;
  days: number;
  rate: number;
}
export interface AttendanceBoard {
  dates: string[];
  members: AttendanceMember[];
  ranking: AttendanceRank[];
}

export interface EventOutcome {
  name: string;
  message: string;
  good: boolean;
  money_delta: number;
  params: Record<string, number> | null;
  disease_delta: number;
  weight_g: number;
  special: string;
}
export interface EventRollResp {
  player: Player;
  event: EventOutcome | null;
}

export interface Character {
  owner_id: number;
  name: string;
  abilities: Record<string, number>;
  sintai: number;
  zunou: number;
  wins: number;
  losses: number;
  draws: number;
}
export interface CLeagueRank {
  owner_id: number;
  owner_name: string;
  char_name: string;
  wins: number;
  losses: number;
  draws: number;
  sintai: number;
  zunou: number;
}
export interface BattleRound {
  ability: string;
  comment: string;
  a_score: number;
  b_score: number;
  winner: string;
}
export interface BattleResult {
  winner: string;
  rounds: BattleRound[];
}
export interface BattleResp {
  player: Player;
  result: BattleResult;
}

// 建設会社(建築系)
export interface BuildingTown {
  no: number;
  name: string;
  land_price: number;
  hidden: boolean; // 隠し町(建築対象外)
}
export interface BuildingExterior {
  key: string;
  price: number;
}
export interface BuildingInterior {
  rank: number;
  name: string;
  multiplier: number; // 費用倍率(D=1..A=4。建築/建て替え費に乗算)
  slots: number;
}
// 家のコンテンツ枠。内装ランクで枠数が決まり(A=4..D=1)、設定した枠だけが訪問者に見える。
export interface HouseContent {
  slot: number;
  kind: string; // 'bbs'=通常掲示板 / 'shop'=お店 / 'nushi'=家主板 / 'url'=独自URL
  title: string;
  url: string; // kind='url' の埋め込みURL
  comment: string; // タイトル下コメント(リード文)
}
export interface HouseCell {
  id: number;
  town: number;
  row: number;
  col: number;
  exterior: string;
  setumei: string;
  owner_name: string;
  own: boolean;
  tuika: number; // 0=家のみ/1=運営/2=株式会社/3=持ち物販売店
  contents: HouseContent[];
}
export interface MyHouse {
  id: number;
  town: number;
  row: number;
  col: number;
  exterior: string;
  setumei: string;
  interior_rank: number;
  tuika: number; // 0=家のみ/1=運営/2=株式会社/3=持ち物販売店
  slots: number;
  built_at: string;
  has_shop: boolean;
  shop_title: string;
  shop_kind: string;
  shop_markup: number;
  contents: HouseContent[];
}
export interface PlotCell {
  town: number;
  row: number;
  col: number;
}
// 2軒目以降の追加種別(レガシー@housu_tuika2)。
export interface BuildingTuika {
  no: number; // 0=家のみ/1=運営/2=株式会社/3=持ち物販売店
  name: string;
  fee: number; // 万円
  shinsa: boolean; // 能力審査(総資産1億+全パラ1万)が必要
}
export interface BuildingState {
  towns: BuildingTown[];
  exteriors: BuildingExterior[];
  interiors: BuildingInterior[];
  tuikas: BuildingTuika[];
  shinsa_ok: boolean;
  plots: PlotCell[];
  houses: HouseCell[];
  my_houses: MyHouse[];
  shop_kinds: string[];
  house_count: number;
  mochiie_max: number;
  cols: number;
  rows: number;
}
export interface OrosiItem {
  item_id: number;
  name: string;
  category: string;
  buy_price: number;
  in_stock: number;
}
export interface OrosiState {
  syubetu: string;
  markup: number;
  savings: number;
  stock_kinds: number;
  max_kinds: number;
  max_stock: number;
  items: OrosiItem[];
}
export interface HouseShopItem {
  item_id: number;
  name: string;
  category: string;
  price: number;
  stock: number;
  money: number;
  params: Record<string, number>;
  calorie_g: number;
  durability: number;
  durability_unit: string; // 'use'(回)/'day'(日)
  interval_min: number;
  body_cost: number;
  nou_cost: number;
  owned: number; // 自分の所持残数(未所持0)
}
export interface HouseShopView {
  has_shop: boolean;
  title: string;
  syubetu: string;
  owner_name: string;
  own: boolean;
  items: HouseShopItem[];
}
export interface BbsPost {
  id: number;
  kind: string;
  author_id: number;
  author_name: string;
  author_job: string; // 投稿時の職業(（職業）表示用)
  title: string; // 家主板(nushi)の記事タイトル
  body: string;
  thread_no: number; // 親記事のNO.x(レスは0)
  parent_no: number; // レス先スレッドNO(親記事は0)
  created_at: string;
}
export interface BbsPostResult {
  reward: number;
  bonus: boolean;
}
export type PostBbsResp = Player & { bbs_result: BbsPostResult };
// 持ち物販売店(闇市)の1品(1行=1品、単品スナップショット)。
export interface YamiItem {
  listing_id: number;
  item_id: number;
  name: string;
  category: string;
  price: number;
  uses: number; // この1品の残り耐久
  zokusei: number; // 1=倉庫品(家主のみ表示)
  money: number;
  params: Record<string, number>;
  calorie_g: number;
  durability_unit: string;
  interval_min: number;
  body_cost: number;
  nou_cost: number;
}
export interface YamiView {
  is_yami: boolean;
  owner_name: string;
  own: boolean;
  max_items: number;
  items: YamiItem[];
}
export interface YamiInventoryItem {
  item_id: number;
  name: string;
  category: string;
  quantity: number;
  uses: number;
  durability_unit: string;
  default_price: number;
}
export interface YamiBuyResult {
  name: string;
  paid: number;
  method: string;
  own: boolean;
}
export type YamiBuyResp = Player & { yami_result: YamiBuyResult };
// 運営/株式会社の社員(職と仕送り額は社員パラメータから導出)。
export interface CompanyStaff {
  id: number;
  idx: number;
  params: Record<string, number>;
  job: string;
  sougou: number;
  income: number;
  edu_log: string;
  can_edu_at: string; // 次に教育できる時刻(空=今すぐ可)
}
export interface CompanyOfficer {
  player_id: number;
  name: string;
}
// 会社BBSの記事。statusは入退会ワークフロー(in/out/m_ryoukai/taikai)。
export interface CompanyBbsPost {
  id: number;
  no: number;
  author_id: number;
  author_name: string;
  body: string;
  status: string;
  created_at: string;
}
// 製造フォーム(株式会社オーナーのみ): 原料max(社員パラ最大値/10)と食料。
export interface CompanyMaterials {
  maxima: Record<string, number>;
  syoku: number;
  staff_count: number;
  made_today: boolean;
  has_shop: boolean;
  shop_syubetu: string;
}
export interface CompanyView {
  is_company: boolean;
  kind: number; // 1=運営 2=株式会社
  owner_name: string;
  own: boolean;
  officer: boolean;
  officers: CompanyOfficer[];
  staff_max: number;
  total_income: number;
  staff: CompanyStaff[];
  bbs_open: CompanyBbsPost[];
  bbs_member: CompanyBbsPost[];
  materials: CompanyMaterials | null;
  edu_efficiency: number;
  edu_fee_point: number;
  edu_interval_min: number;
}
export interface SeizouResult {
  name: string;
  zaiko: number;
  taikyuu: number;
  price: number;
}
export type SeizouResp = Player & { seizou_result: SeizouResult };
export interface StaffEduResult {
  param_name: string;
  gained: number;
  fee: number;
}
export type EducateResp = Player & { edu_result: StaffEduResult };
export interface ShopStockItem {
  item_id: number;
  name: string;
  category: string;
  buy_price: number;
  sell_price: number | null;
  shelf: number;
  stock: number;
  max_price: number;
}
export interface ShopStockView {
  has_shop: boolean;
  markup: number;
  items: ShopStockItem[];
}

function adminHeaders(actingId: number): Record<string, string> {
  return { 'X-Acting-Player-Id': String(actingId) };
}

export const api = {
  register: (instanceHost: string, remoteUserId: string, displayName: string) =>
    request<Player>('POST', '/players', {
      instance_host: instanceHost,
      remote_user_id: remoteUserId,
      display_name: displayName,
    }),
  getPlayer: (id: number) => request<Player>('GET', `/players/${id}`),
  listPlayers: () => request<PublicSummary[]>('GET', '/players'),
  playerProfile: (id: number) => request<PublicProfile>('GET', `/players/${id}/profile`),
  // 役場: 街のニュース(街全体)と住民ごとの出来事。
  townNews: (limit = 100) => request<NewsEntry[]>('GET', `/news?limit=${limit}`),
  playerNews: (id: number, limit = 50) => request<NewsEntry[]>('GET', `/players/${id}/news?limit=${limit}`),
  rankingKeys: () => request<RankingKey[]>('GET', '/ranking/keys'),
  ranking: (key: string, self: number) => request<RankingResult>('GET', `/ranking?key=${key}&self=${self}`),
  townMap: () => request<TownFacility[]>('GET', '/townmap'),
  townAssets: () => request<TownAsset[]>('GET', '/townassets'),
  towns: () => request<Town[]>('GET', '/towns'),
  // 全街の家(メイン画面のグリッド描画用)。ownは呼び出し元プレイヤー基準。
  houses: (id: number) => request<HouseCell[]>('GET', `/players/${id}/houses`),
  // 街移動(徒歩/バス)。行き先の街と手段を送る。移動結果(能力上昇等)を含む。
  moveTown: (id: number, dest: number, means: 'walk' | 'bus') =>
    request<MoveResp>('POST', `/players/${id}/move`, {
      dest,
      means,
      idempotency_key: newIdempotencyKey(),
    }),
  // ワープ(高額・即時)。行き先の街へ瞬間移動する。
  warp: (id: number, dest: number) =>
    request<Player>('POST', `/players/${id}/warp`, {
      dest,
      idempotency_key: newIdempotencyKey(),
    }),
  stocks: () => request<StocksResp>('GET', '/stocks'),
  playerStocks: (id: number) => request<PlayerStocksResp>('GET', `/players/${id}/stocks`),
  stockBuy: (id: number, symbol: string, quantity: number) =>
    request<Player>('POST', `/players/${id}/stocks/buy`, {
      symbol,
      quantity,
      idempotency_key: newIdempotencyKey(),
    }),
  stockSell: (id: number, symbol: string, quantity: number) =>
    request<Player>('POST', `/players/${id}/stocks/sell`, {
      symbol,
      quantity,
      idempotency_key: newIdempotencyKey(),
    }),
  stockSettle: (id: number) =>
    request<Player>('POST', `/players/${id}/stocks/settle`, {
      idempotency_key: newIdempotencyKey(),
    }),
  getMail: (id: number) => request<Mailbox>('GET', `/players/${id}/mail`),
  getMailUnread: (id: number) => request<{ unread: number }>('GET', `/players/${id}/mail/unread`),
  mailSend: (id: number, recipientId: number, body: string) =>
    request<{ ok: boolean }>('POST', `/players/${id}/mail/send`, { recipient_id: recipientId, body }),
  mailDelete: (id: number, msgId: number) =>
    request<{ ok: boolean }>('DELETE', `/players/${id}/mail/${msgId}`),
  mailSave: (id: number, msgId: number, saved: boolean) =>
    request<{ ok: boolean }>('PUT', `/players/${id}/mail/${msgId}/save`, { saved }),
  greetings: (limit?: number) =>
    request<Greeting[]>('GET', `/greetings${limit ? `?limit=${limit}` : ''}`),
  postGreeting: (id: number, category: string, body: string, color: string, janken: string) =>
    request<GreetResp>('POST', `/players/${id}/greetings`, {
      category,
      body,
      color,
      janken,
      idempotency_key: newIdempotencyKey(),
    }),
  getCharacter: (id: number) => request<Character | null>('GET', `/players/${id}/character`),
  cleague: () => request<CLeagueRank[]>('GET', '/cleague'),
  setCharacterName: (id: number, name: string) =>
    request<Player>('POST', `/players/${id}/character`, { name, idempotency_key: newIdempotencyKey() }),
  growCharacter: (id: number, inputs: Record<string, number>) =>
    request<Player>('POST', `/players/${id}/character/grow`, { inputs, idempotency_key: newIdempotencyKey() }),
  battle: (id: number, opponentId: number) =>
    request<BattleResp>('POST', `/players/${id}/character/battle`, {
      opponent_id: opponentId,
      idempotency_key: newIdempotencyKey(),
    }),
  attendanceBoard: () => request<AttendanceBoard>('GET', '/attendance'),
  attendanceCheckin: (id: number) =>
    request<{ recorded: boolean }>('POST', `/players/${id}/attendance/checkin`),
  eventRoll: (id: number) =>
    request<EventRollResp>('POST', `/players/${id}/events/roll`, {
      idempotency_key: newIdempotencyKey(),
    }),
  keibaRace: (id: number) => request<KeibaRaceResp>('GET', `/players/${id}/keiba`),
  keibaBet: (id: number, raceId: number, bets: number[]) =>
    request<KeibaBetResp>('POST', `/players/${id}/keiba/bet`, {
      race_id: raceId,
      bets,
      idempotency_key: newIdempotencyKey(),
    }),
  shopItems: () => request<ShopItem[]>('GET', '/items'),
  facilityMenu: (facility: string) => request<ShopItem[]>('GET', `/facilities/${facility}/menu`),
  eat: (id: number, foodId: number) =>
    request<Player>('POST', `/players/${id}/eat`, {
      food_id: foodId,
      idempotency_key: newIdempotencyKey(),
    }),
  schoolAttend: (id: number, courseId: number) =>
    request<Player>('POST', `/players/${id}/school/attend`, {
      course_id: courseId,
      idempotency_key: newIdempotencyKey(),
    }),
  facilityUse: (id: number, facility: string, menuId: number) =>
    request<Player>('POST', `/players/${id}/facilities/${facility}/use`, {
      menu_id: menuId,
      idempotency_key: newIdempotencyKey(),
    }),
  jobs: () => request<JobOption[]>('GET', '/jobs'),
  changeJob: (id: number, jobName: string) =>
    request<Player>('POST', `/players/${id}/job`, {
      job_name: jobName,
      idempotency_key: newIdempotencyKey(),
    }),
  work: (id: number) =>
    request<WorkResponse>('POST', `/players/${id}/work`, { idempotency_key: newIdempotencyKey() }),
  buy: (id: number, itemId: number, facility = '') =>
    request<Player>('POST', `/players/${id}/buy`, {
      item_id: itemId,
      facility,
      idempotency_key: newIdempotencyKey(),
    }),
  use: (id: number, itemId: number) =>
    request<Player>('POST', `/players/${id}/use`, {
      item_id: itemId,
      idempotency_key: newIdempotencyKey(),
    }),
  deposit: (id: number, amount: number) =>
    request<Player>('POST', `/players/${id}/bank/deposit`, {
      amount,
      idempotency_key: newIdempotencyKey(),
    }),
  withdraw: (id: number, amount: number) =>
    request<Player>('POST', `/players/${id}/bank/withdraw`, {
      amount,
      idempotency_key: newIdempotencyKey(),
    }),
  bankStatement: (id: number, account: 'normal' | 'super' = 'normal') =>
    request<StatementEntry[]>(
      'GET',
      `/players/${id}/bank/statement${account === 'super' ? '?account=super' : ''}`,
    ),
  transfer: (id: number, toName: string, amount: number) =>
    request<Player>('POST', `/players/${id}/bank/transfer`, {
      to_name: toName,
      amount,
      idempotency_key: newIdempotencyKey(),
    }),
  superDeposit: (id: number, amount: number) =>
    request<Player>('POST', `/players/${id}/bank/super/deposit`, {
      amount,
      idempotency_key: newIdempotencyKey(),
    }),
  superCancel: (id: number, amount: number, all: boolean) =>
    request<Player>('POST', `/players/${id}/bank/super/cancel`, {
      amount,
      all,
      idempotency_key: newIdempotencyKey(),
    }),
  loanQuote: (id: number) => request<LoanQuote>('GET', `/players/${id}/bank/loan/quote`),
  loanBorrow: (id: number, count: number) =>
    request<Player>('POST', `/players/${id}/bank/loan/borrow`, {
      count,
      idempotency_key: newIdempotencyKey(),
    }),
  loanRepay: (id: number) =>
    request<Player>('POST', `/players/${id}/bank/loan/repay`, {
      idempotency_key: newIdempotencyKey(),
    }),
  casinoPlay: (id: number, game: string, bet: number, params: unknown) =>
    request<CasinoPlayResult>('POST', `/players/${id}/casino/${game}/play`, {
      bet,
      params,
      idempotency_key: newIdempotencyKey(),
    }),
  scratchState: (id: number, game: string) => request<ScratchState>('GET', `/players/${id}/scratch/${game}`),
  scratchOpen: (id: number, game: string, card: number, cell: number) =>
    request<ScratchOpenResult>('POST', `/players/${id}/scratch/${game}/open`, {
      card,
      cell,
      idempotency_key: newIdempotencyKey(),
    }),
  bjState: (id: number) => request<BJState>('GET', `/players/${id}/blackjack`),
  bjStart: (id: number, rate: number) =>
    request<BJState>('POST', `/players/${id}/blackjack/start`, { rate, idempotency_key: newIdempotencyKey() }),
  bjHit: (id: number) =>
    request<BJState>('POST', `/players/${id}/blackjack/hit`, { idempotency_key: newIdempotencyKey() }),
  bjStand: (id: number) =>
    request<BJState>('POST', `/players/${id}/blackjack/stand`, { idempotency_key: newIdempotencyKey() }),
  pokerState: (id: number) => request<PokerState>('GET', `/players/${id}/poker`),
  pokerBuy: (id: number) =>
    request<PokerState>('POST', `/players/${id}/poker/buy`, { idempotency_key: newIdempotencyKey() }),
  pokerDeal: (id: number) =>
    request<PokerState>('POST', `/players/${id}/poker/deal`, { idempotency_key: newIdempotencyKey() }),
  pokerDraw: (id: number, hold: number[]) =>
    request<PokerState>('POST', `/players/${id}/poker/draw`, { hold, idempotency_key: newIdempotencyKey() }),
  pokerCashout: (id: number) =>
    request<PokerState>('POST', `/players/${id}/poker/cashout`, { idempotency_key: newIdempotencyKey() }),
  loto6State: (id: number) => request<Loto6State>('GET', `/players/${id}/loto6`),
  loto6Buy: (id: number, numbers: number[]) =>
    request<Loto6State>('POST', `/players/${id}/loto6/buy`, { numbers, idempotency_key: newIdempotencyKey() }),
  hospitalTreat: (id: number) =>
    request<Player>('POST', `/players/${id}/hospital/treat`, {
      idempotency_key: newIdempotencyKey(),
    }),
  onsenBathe: (id: number, bathId: number) =>
    request<Player>('POST', `/players/${id}/onsen/bathe`, {
      bath_id: bathId,
      idempotency_key: newIdempotencyKey(),
    }),
  onsenLeave: (id: number) =>
    request<Player>('POST', `/players/${id}/onsen/leave`, {
      idempotency_key: newIdempotencyKey(),
    }),
  onsenTick: (id: number) =>
    request<Player>('POST', `/players/${id}/onsen/tick`, {
      idempotency_key: newIdempotencyKey(),
    }),

  building: (id: number) => request<BuildingState>('GET', `/players/${id}/building`),
  buildHouse: (
    id: number,
    town: number,
    row: number,
    col: number,
    exterior: string,
    interiorRank: number,
    tuika = 0,
  ) =>
    request<Player>('POST', `/players/${id}/building/build`, {
      town,
      row,
      col,
      exterior,
      interior_rank: interiorRank,
      tuika,
      idempotency_key: newIdempotencyKey(),
    }),
  sellHouse: (id: number, houseId: number) =>
    request<Player>('POST', `/players/${id}/building/sell`, {
      house_id: houseId,
      idempotency_key: newIdempotencyKey(),
    }),
  rebuildHouse: (id: number, houseId: number, exterior: string, interiorRank: number) =>
    request<Player>('POST', `/players/${id}/building/rebuild`, {
      house_id: houseId,
      exterior,
      interior_rank: interiorRank,
      idempotency_key: newIdempotencyKey(),
    }),
  setHouseComment: (id: number, houseId: number, setumei: string) =>
    request<Player>('POST', `/players/${id}/building/comment`, {
      house_id: houseId,
      setumei,
      idempotency_key: newIdempotencyKey(),
    }),
  // 家のコンテンツ枠を設定(全置き換え)。kind空は非公開。
  setHouseContents: (id: number, houseId: number, contents: HouseContent[]) =>
    request<Player>('POST', `/players/${id}/building/contents`, {
      house_id: houseId,
      contents,
      idempotency_key: newIdempotencyKey(),
    }),
  saisen: (id: number, houseId: number, amount: number) =>
    request<Player>('POST', `/players/${id}/building/saisen`, {
      house_id: houseId,
      amount,
      idempotency_key: newIdempotencyKey(),
    }),
  openHouseShop: (id: number, houseId: number, title: string, syubetu: string, markup: number) =>
    request<Player>('POST', `/players/${id}/building/shop/open`, {
      house_id: houseId,
      title,
      syubetu,
      markup,
      idempotency_key: newIdempotencyKey(),
    }),
  orosi: (id: number, houseId: number) =>
    request<OrosiState>('GET', `/players/${id}/building/orosi?house_id=${houseId}`),
  shiire: (id: number, houseId: number, itemId: number, qty: number) =>
    request<Player>('POST', `/players/${id}/building/shiire`, {
      house_id: houseId,
      item_id: itemId,
      qty,
      idempotency_key: newIdempotencyKey(),
    }),
  houseShop: (id: number, houseId: number) =>
    request<HouseShopView>('GET', `/players/${id}/building/shop?house_id=${houseId}`),
  buyFromHouseShop: (id: number, houseId: number, itemId: number, qty: number, payMethod = 'cash') =>
    request<BuyResp>('POST', `/players/${id}/building/shop/buy`, {
      house_id: houseId,
      item_id: itemId,
      qty,
      pay_method: payMethod,
      idempotency_key: newIdempotencyKey(),
    }),
  houseBbs: (id: number, houseId: number) =>
    request<BbsPost[]>('GET', `/players/${id}/building/bbs?house_id=${houseId}`),
  postBbs: (id: number, houseId: number, kind: string, body: string, title = '', parentNo = 0) =>
    request<PostBbsResp>('POST', `/players/${id}/building/bbs/post`, {
      house_id: houseId,
      kind,
      title,
      body,
      parent_no: parentNo,
      idempotency_key: newIdempotencyKey(),
    }),
  deleteBbs: (
    id: number,
    houseId: number,
    kind: string,
    opts: { articleNo?: number; threadNo?: number; all?: boolean },
  ) =>
    request<Player>('POST', `/players/${id}/building/bbs/delete`, {
      house_id: houseId,
      kind,
      article_no: opts.articleNo ?? 0,
      thread_no: opts.threadNo ?? 0,
      all: opts.all ?? false,
      idempotency_key: newIdempotencyKey(),
    }),
  yamiShop: (id: number, houseId: number) =>
    request<YamiView>('GET', `/players/${id}/building/yami?house_id=${houseId}`),
  yamiInventory: (id: number) =>
    request<YamiInventoryItem[]>('GET', `/players/${id}/building/yami/inventory`),
  yamiList: (id: number, houseId: number, itemId: number, price: number, warehouse: boolean) =>
    request<Player>('POST', `/players/${id}/building/yami/list`, {
      house_id: houseId,
      item_id: itemId,
      price,
      warehouse,
      idempotency_key: newIdempotencyKey(),
    }),
  yamiBuy: (id: number, houseId: number, listingId: number, payMethod = 'cash') =>
    request<YamiBuyResp>('POST', `/players/${id}/building/yami/buy`, {
      house_id: houseId,
      listing_id: listingId,
      pay_method: payMethod,
      idempotency_key: newIdempotencyKey(),
    }),
  companyView: (id: number, houseId: number) =>
    request<CompanyView>('GET', `/players/${id}/building/company?house_id=${houseId}`),
  companyStaffAdd: (id: number, houseId: number) =>
    request<Player>('POST', `/players/${id}/building/company/staff`, {
      house_id: houseId,
      idempotency_key: newIdempotencyKey(),
    }),
  companyEducate: (id: number, houseId: number, staffId: number, param: string, amount: number, payMethod = 'cash') =>
    request<EducateResp>('POST', `/players/${id}/building/company/educate`, {
      house_id: houseId,
      staff_id: staffId,
      param,
      amount,
      pay_method: payMethod,
      idempotency_key: newIdempotencyKey(),
    }),
  companyBbsPost: (id: number, houseId: number, board: string, body: string, wantJoin = false, wantLeave = false) =>
    request<Player>('POST', `/players/${id}/building/company/bbs`, {
      house_id: houseId,
      board,
      body,
      want_join: wantJoin,
      want_leave: wantLeave,
      idempotency_key: newIdempotencyKey(),
    }),
  companyApprove: (id: number, houseId: number, postId: number) =>
    request<Player>('POST', `/players/${id}/building/company/approve`, {
      house_id: houseId,
      post_id: postId,
      idempotency_key: newIdempotencyKey(),
    }),
  companyKick: (id: number, houseId: number, officerId: number) =>
    request<Player>('POST', `/players/${id}/building/company/kick`, {
      house_id: houseId,
      officer_id: officerId,
      idempotency_key: newIdempotencyKey(),
    }),
  companyBbsDelete: (id: number, houseId: number, board: string, no: number) =>
    request<Player>('POST', `/players/${id}/building/company/bbs/delete`, {
      house_id: houseId,
      board,
      no,
      idempotency_key: newIdempotencyKey(),
    }),
  companySeizou: (
    id: number,
    houseId: number,
    input: {
      name: string;
      params: Record<string, number>;
      cal: number;
      kankaku: number;
      zaiko: number;
      taikyuu: number;
      price: number;
    },
  ) =>
    request<SeizouResp>('POST', `/players/${id}/building/company/seizou`, {
      house_id: houseId,
      input,
      idempotency_key: newIdempotencyKey(),
    }),
  participants: () => request<{ id: number; display_name: string }[]>('GET', '/participants'),
  houseShopStock: (id: number, houseId: number) =>
    request<ShopStockView>('GET', `/players/${id}/building/shop/stock?house_id=${houseId}`),
  setHouseShopPrice: (id: number, houseId: number, itemId: number, sellPrice: number) =>
    request<Player>('POST', `/players/${id}/building/shop/price`, {
      house_id: houseId,
      item_id: itemId,
      sell_price: sellPrice,
      idempotency_key: newIdempotencyKey(),
    }),

  // 管理者API(X-Acting-Player-Idヘッダ + adminロール)。
  adminListItems: (actingId: number) =>
    request<AdminItem[]>('GET', '/admin/items', undefined, adminHeaders(actingId)),
  adminCreateItem: (
    actingId: number,
    item: { name: string; category: string; price: number; effect: EffectOp[]; stock_master: number | null },
  ) => request<AdminItem>('POST', '/admin/items', item, adminHeaders(actingId)),
  adminUpdateItem: (
    actingId: number,
    id: number,
    item: {
      name: string;
      category: string;
      price: number;
      effect: EffectOp[];
      enabled: boolean;
      stock_master: number | null;
    },
  ) => request<AdminItem>('PUT', `/admin/items/${id}`, item, adminHeaders(actingId)),
  adminDeleteItem: (actingId: number, id: number) =>
    request<{ deleted: boolean }>('DELETE', `/admin/items/${id}`, undefined, adminHeaders(actingId)),
  adminListJobs: (actingId: number) =>
    request<AdminJob[]>('GET', '/admin/jobs', undefined, adminHeaders(actingId)),
  adminCreateJob: (actingId: number, job: JobPayload) =>
    request<AdminJob>('POST', '/admin/jobs', job, adminHeaders(actingId)),
  adminUpdateJob: (actingId: number, id: number, job: JobPayload) =>
    request<AdminJob>('PUT', `/admin/jobs/${id}`, job, adminHeaders(actingId)),
  adminDeleteJob: (actingId: number, id: number) =>
    request<{ deleted: boolean }>('DELETE', `/admin/jobs/${id}`, undefined, adminHeaders(actingId)),
  adminSimulate: (
    actingId: number,
    effect: EffectOp[],
    state: { money: number; params: Record<string, { value: number; max: number }> },
  ) => request<SimResult>('POST', '/admin/simulate', { effect, state }, adminHeaders(actingId)),
  adminListPlayers: (actingId: number) =>
    request<AdminPlayerSummary[]>('GET', '/admin/players', undefined, adminHeaders(actingId)),
  adminUpdatePlayer: (actingId: number, id: number, payload: AdminPlayerPayload) =>
    request<Player>('PUT', `/admin/players/${id}`, payload, adminHeaders(actingId)),
  adminDeletePlayer: (actingId: number, id: number) =>
    request<{ deleted: boolean }>('DELETE', `/admin/players/${id}`, undefined, adminHeaders(actingId)),
  adminDeleteGreeting: (actingId: number, id: number) =>
    request<{ deleted: boolean }>('DELETE', `/admin/greetings/${id}`, undefined, adminHeaders(actingId)),
  adminGetSettings: (actingId: number) =>
    request<GameSettings>('GET', '/admin/settings', undefined, adminHeaders(actingId)),
  adminUpdateSettings: (actingId: number, settings: GameSettings) =>
    request<GameSettings>('PUT', '/admin/settings', settings, adminHeaders(actingId)),
  adminUpdateTownMap: (actingId: number, facilities: TownFacility[]) =>
    request<TownFacility[]>('PUT', '/admin/townmap', facilities, adminHeaders(actingId)),
  adminUpdateTownAssets: (actingId: number, assets: TownAsset[]) =>
    request<TownAsset[]>('PUT', '/admin/townassets', assets, adminHeaders(actingId)),
  // 施設プリセット(画像・表示名・遷移先の保存済みテンプレート)。
  adminFacilityPresets: (actingId: number) =>
    request<FacilityPreset[]>('GET', '/admin/townmap/presets', undefined, adminHeaders(actingId)),
  adminUpdateFacilityPresets: (actingId: number, presets: FacilityPreset[]) =>
    request<FacilityPreset[]>('PUT', '/admin/townmap/presets', presets, adminHeaders(actingId)),
  // カスタムイベント(ランダムイベントの追加/編集/削除)。
  adminListEvents: (actingId: number) =>
    request<AdminEvent[]>('GET', '/admin/events', undefined, adminHeaders(actingId)),
  adminCreateEvent: (actingId: number, e: Omit<AdminEvent, 'id'>) =>
    request<AdminEvent>('POST', '/admin/events', e, adminHeaders(actingId)),
  adminUpdateEvent: (actingId: number, e: AdminEvent) =>
    request<AdminEvent>('PUT', `/admin/events/${e.id}`, e, adminHeaders(actingId)),
  adminDeleteEvent: (actingId: number, id: number) =>
    request<{ deleted: boolean }>('DELETE', `/admin/events/${id}`, undefined, adminHeaders(actingId)),
  // 家が建っているマス(施設エディタでロックするため)。
  adminHouseCells: (actingId: number) =>
    request<PlotCell[]>('GET', '/admin/townmap/houses', undefined, adminHeaders(actingId)),
  // アップロード済み画像名の一覧(背景アセットのパレット用)。
  adminListAssets: (actingId: number) =>
    request<string[]>('GET', '/admin/assets', undefined, adminHeaders(actingId)),
  // 背景アセット画像をアップロード(base64)。nameはURLスラッグ。
  adminUploadAsset: (actingId: number, name: string, mime: string, data: string) =>
    request<{ name: string }>('POST', '/admin/assets', { name, mime, data }, adminHeaders(actingId)),
  // アップロード画像を削除(配置中は422)。
  adminDeleteAsset: (actingId: number, name: string) =>
    request<{ ok: boolean }>('DELETE', `/admin/assets/${encodeURIComponent(name)}`, undefined, adminHeaders(actingId)),
  // 街の一覧(名前・地価)を更新。街番号は並び順で決まる。
  adminUpdateTowns: (actingId: number, towns: TownConfig[]) =>
    request<Town[]>('PUT', '/admin/towns', towns, adminHeaders(actingId)),
};
