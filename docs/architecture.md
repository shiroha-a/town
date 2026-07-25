# プロジェクト構成

## 全体像

```
ブラウザ ──▶ frontend (Vue 3 SPA / Vite)
                │  /api を web へプロキシ(同一オリジンなのでCORS不要)
                ▼
             web (Go)  ──▶ PostgreSQL  … 永続データすべて
                │         └▶ Redis      … 揮発(リーダーロック等)
                │
             worker (Go) ──▶ 同じDB     … 時間進行(利息・株価・病気・回収など)
```

`web` と `worker` は**同じバイナリの別モード**です(`town web` / `town worker`)。
どちらも起動時に同じ設定を読み、その時点でDBマイグレーションを適用します
(どちらが先に立ち上がっても同じ結果になります)。

## ディレクトリ

```
backend/
├── cmd/town/          # エントリーポイント(web / worker の2モード)
├── default.yml        # 既定設定(環境変数で上書き)
├── Dockerfile
└── internal/          # 下の表を参照
frontend/
├── src/
│   ├── App.vue        # 画面の切り替え(ルーターは使わない)
│   ├── api.ts         # APIクライアント(サーバーとの唯一の窓口)
│   ├── emoji.ts       # カスタム絵文字の辞書とテキストの分解
│   ├── components/    # 画面と部品(51ファイル。うち12がカジノ)
│   └── style.css      # 全体のスタイル(レガシーの見た目を再現)
└── vite.config.ts
deploy/
├── compose.yaml       # postgres + redis + web + worker + frontend
└── initdb/            # テスト用DBの作成
docs/
├── api.md             # APIリファレンス
└── architecture.md    # この文書
```

## バックエンドのパッケージ

HTTPハンドラ(`httpapi`)は薄く、ゲームのルールは各ドメインのパッケージに置いています。
お金が動く操作は必ず `ledger` を通ります。

| パッケージ | 役割 |
| --- | --- |
| `app` | 依存の組み立てとモードの起動 |
| `config` | 設定の読み込み(YAML + 環境変数) |
| `db` | 接続プールとマイグレーション(goose、埋め込み) |
| `rediscli` | Redisクライアント(揮発用途のみ) |
| **`ledger`** | **複式・追記専用の金銭台帳。残高はここから算出する** |
| `player` | プレイヤーの登録とステータスの読み出し |
| `action` | プレイヤーの行動(仕事・購入・使用…)をトランザクションで実行 |
| `effects` | データ駆動の効果/条件スキーマ(JSON)の解釈と適用 |
| `condition` | 体調・体型・パワー上限などサーバー権威の導出 |
| `content` | 管理者が編集するコンテンツ(アイテム・職業・イベント・家) |
| `settings` | ゲーム設定(既定値・検証・DB永続化)。管理画面から編集する |
| `townmap` | 街マップの配置(施設・背景・プリセット) |
| `building` | 建設会社の参照データと費用計算 |
| `jobrule` | 職業と給料の共通ルール |
| `bank` | 利息の付与 |
| `stock` | 株取引場(5銘柄・共有価格) |
| `keiba` | 競馬 |
| `casino` | 単発のカジノゲーム8種(サイコロ・スロット・くじ等。純粋関数として実装) |
| `fishing` `bingo` `serial` | 釣り / ビンゴ / シリアルコード(特典) |
| `event` | ランダムイベント |
| `mail` `greeting` `attendance` | メール / あいさつ / 足あと |
| `news` `ranking` | 街のニュース / 役場のランキング |
| `cleague` | 対戦キャラクターのリーグ |
| `gametime` | 「ゲーム日」の計算(境界時刻でロールオーバー) |
| `rng` | サーバー権威の乱数 |
| `miauth` | MiAuthのフローとMisskey APIクライアント |
| `session` | ログインセッション(cookie) |
| `profile` | Misskeyプロフィールの取得とリモートフォロー |
| `emoji` | カスタム絵文字の取得と使用可否の判定 |
| `httpapi` | REST API。認可は `middleware.go` の1か所 |
| `worker` | 時間進行のジョブ |
| `integration` | API結合テスト(実DBを使う) |

## 設計上の決めごと

### お金は複式の台帳で持つ

`players` に残高カラムはありません。すべての金銭移動は `ledger_tx` と
`ledger_entry` に**追記**され、残高は合計から求めます。1つの取引の借方と貸方は
必ず0に合計されるため、「世界に存在するお金の総量」が監査できる不変条件になります。

バグで所持金がおかしくなっても、どの取引が原因かを台帳から追えます。

### 行動は1トランザクション + 冪等キー

お金や持ち物が動く操作は `action.runAction` を通ります。

1. 冪等キーを確保する(同じキーが既にあれば何もせず成功を返す)
2. プレイヤーの状態を読む
3. 実際の処理を行う

すべて同じトランザクション内なので、途中で失敗すれば何も起きません。
通信断でクライアントが再送しても二重には実行されません。

### 効果はデータで書く

アイテム・職業・イベントの効果は、Goのコードではなく**JSONのスキーマ**で表現します
(`effects`)。管理者が管理画面から編集できる一方、**管理者の入力も信用しない**方針で、
未知の演算子やパラメータは読み込み時に拒否します。任意コードは一切実行しません。

```json
{ "ops": [{ "op": "add_param", "param": "tairyoku", "value": 2 },
          { "op": "add_weight_g", "value": 300 }] }
```

### 時間の進み方

「ゲーム日」は実時刻を境界時刻(既定 AM5:00)だけ戻した日付です(`gametime`)。
日次のリセットはこの日付が変わったときに走ります。

時間進行は `worker` が担当します。複数起動しても**Redisのリーダーロック**
(`SETNX`)を取れた1つだけが実行し、各ジョブは同じゲーム日に二度走らないよう
`worker_jobs` で冪等にしています。

### 認可は1か所

162本のエンドポイントの認可を、ハンドラごとではなく `httpapi/middleware.go` の
1か所で判定します。マッチしたルートパターンから「公開 / ログイン / 本人 / 管理者」を
振り分けます。

`/players/{id}/…` と `/admin/…` は**パスの形だけで区分が決まる**ので、
新しい経路を足しても認可の書き忘れが起きません。一方それ以外の形の経路
(例 `/misskey/follow`)は既定で公開になるため、ログインを要するものは
`middleware.go` のリストに明示的に足す必要があります。

詳しくは [api.md の認可](api.md#認可) を参照してください。

### 判定はサーバーが持つ

体調・BMI・パワーの上限・給料・確率など、ゲームの結果を左右する計算は
すべてサーバー側にあります(`condition` / `jobrule` / `rng`)。
クライアントから送られるのは「何をしたいか」だけです。

### 生のHTMLを保存しない

掲示板・あいさつ・プロフィールに保存するのは**テキストだけ**です。
カスタム絵文字も `:name@host:` というショートコードで保存し、
表示時に画像へ置き換えます。レガシーは投稿本文に `<img>` タグを直接
埋めていましたが、その方式は採っていません。

## フロントエンド

Vue 3 + TypeScript のSPAです。ルーターは使わず、`App.vue` が `view` の値で
画面を切り替えます(レガシーが1画面1CGIだった構造に近く、画面数が固定のため)。

- `api.ts` — サーバーとの唯一の窓口。`credentials: 'same-origin'` でcookieを送る
- `components/` — 施設ごとの画面(BankView, DepartView, …)と共通部品
  - `TownMapBoard` — 街マップの盤面。街トップ(操作あり)と入口(操作なし)で共用
  - `RichText` — 投稿本文の描画(URL と `:name@host:` を変換)
  - `EmojiPicker` — 絵文字ピッカー(カテゴリ絞り込み + 検索)
  - `CommandIcon` — コマンドバーのアイコン(Feather Icons由来のSVG)
  - `casino/` — カジノの各ゲーム(単発8種 + スクラッチ・ブラックジャック・ポーカー・ロト6)
- `style.css` — レガシーの配色・枠線を再現した全体スタイル

## データ

主なテーブルは次のとおりです(全体は `backend/internal/db/migrations/` の90本を参照)。

| 分類 | テーブル |
| --- | --- |
| お金 | `ledger_tx` `ledger_entry` `transfer_log` `player_loans` |
| プレイヤー | `players` `player_status` `player_roles` `player_items` `status_history` |
| コンテンツ | `content_items` `content_jobs` `content_events` `app_settings` |
| 街 | `town_map` `uploaded_images` `player_houses` `house_*` `company_*` `yami_items` |
| ゲーム | `player_stock` `stock_price` `keiba_race` `player_poker` `player_blackjack` `bingo_*` `player_fishing` |
| 交流 | `messages` `greetings` `house_bbs` `company_bbs` `attendance` `town_news` |
| 認証 | `sessions` `auth_sessions` `instance_rules` |
| Misskey | `misskey_profiles` `misskey_emojis` `misskey_emoji_rejects` `misskey_resolved_users` |
| 運用 | `action_log` `worker_jobs` `goose_db_version` |

マイグレーションはバイナリに埋め込まれ、起動時に自動で適用されます。

## 設定

設定は**置き場所を役割で分けています**。

| どこ | 何を持つか | 変更方法 |
| --- | --- | --- |
| `backend/default.yml` | インフラのみ(DB/Redis/待ち受けポート/公開アドレス/worker周期) | ファイル + 環境変数。反映は再起動 |
| `deploy/compose.override.yaml` | 環境ごとの値と秘密(公開アドレス・暗号鍵・ポート) | `.example` をコピーして編集。コミットしない |
| DB `app_settings` | ゲームの設定すべて(初期所持金・回復間隔・クールタイム・街の一覧など) | **ゲーム内の管理者メニュー**。基本は即時反映 |

ゲームの値を設定ファイルに置かないのは、運営中に触りたくなるのはほぼゲーム側で、
そのたびに再デプロイするのは現実的でないためです。初回起動時は
`settings.Defaults()`(レガシー準拠の値)をDBへシードします。

タイムゾーンと日付の切り替わり時刻も管理画面から変えられますが、起動時に読むため
**反映には再起動が要ります**。壊れた値が保存されないよう、保存時に検証します
(不正なタイムゾーン・範囲外の時刻は400で拒否)。

| 環境変数 | 用途 |
| --- | --- |
| `TOWN_CONFIG` | 設定ファイルのパス(既定 `default.yml`) |
| `TOWN_HTTP_ADDR` | APIの待ち受けアドレス |
| `TOWN_DATABASE_URL` | PostgreSQLの接続先 |
| `TOWN_REDIS_ADDR` | Redisの接続先 |
| `TOWN_BASE_URL` | 公開アドレス。MiAuthのコールバックURLをここから組み立てる |
| `TOWN_EXTRA_ORIGINS` | base_url以外の経路(Tailscale等)を足す。`all` で全許可(テスト専用) |
| `TOWN_APP_NAME` | Misskeyの承認画面に出るアプリ名 |
| `TOWN_TOKEN_KEY` | Misskeyアクセストークンの暗号化キー(未設定ならトークンを保存しない) |
| `TOWN_COOKIE_SECURE` | `1` でcookieにSecure属性を付ける(HTTPS運用時) |
| `TOWN_RNG_SEED` | 乱数のシードを固定する(開発・テスト用。既定0=時刻ベース) |
| `TOWN_TEST_DATABASE_URL` | 結合テスト用のDB |

## テスト

```sh
cd backend
TOWN_TEST_DATABASE_URL="postgres://town:town@localhost:55432/town_test?sslmode=disable" \
  go test ./...
```

`internal/integration` は実際のPostgreSQLに対してAPIを叩く結合テストです。
各テストの冒頭でワールドを初期化するため、順番に依存しません。
純粋なロジック(効果エンジン・カジノ・条件計算など)は各パッケージの単体テストで
DBなしに検証しています。
