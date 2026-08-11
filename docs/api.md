# API リファレンス

REST API はすべて `/api/v1` 以下にあります。リクエスト・レスポンスとも JSON
(`Content-Type: application/json; charset=utf-8`)です。

## 目次

- [共通事項](#共通事項)
- [認証(MiAuth)](#認証miauth)
- [プレイヤー](#プレイヤー)
- [街](#街)
- [仕事・店・アイテム](#仕事店アイテム)
- [施設](#施設)
- [銀行](#銀行)
- [株・競馬](#株競馬)
- [カジノ](#カジノ)
- [家・会社](#家会社)
- [交流(メール・あいさつ・掲示板)](#交流メールあいさつ掲示板)
- [役場(ニュース・ランキング)](#役場ニュースランキング)
- [イベント(釣り・ギフト・特典・ビンゴ)](#イベント釣りギフト特典ビンゴ)
- [Misskey連携](#misskey連携)
- [管理者](#管理者)

## 共通事項

### 認可

認可は経路のパターンごとに1か所(`internal/httpapi/middleware.go`)で判定します。
表の「認可」列は次の意味です。

| 区分 | 条件 |
| --- | --- |
| 公開 | ログイン不要 |
| ログイン | ログインが必要。対象は自分でなくてよい |
| 本人 | `{id}` が自分自身であること。管理者は**GETのみ**他人の分を読める |
| 管理者 | ログイン + `admin` ロール |

### `{id}` は常に「操作する人」

`/players/{id}/...` の `{id}` は**リクエストを出した本人**です。他人の家や会社を
対象にする操作でも `{id}` は自分のままで、対象は `house_id` などの
パラメータで指定します。

```
GET /api/v1/players/1/building/bbs?house_id=7   # 1番の住民が7番の家の掲示板を見る
```

例外は `GET /players/{id}/profile`・`/news`・`/misskey` で、これらは
「その住民について見る」経路です。

### エラー

エラーは HTTP ステータスと次の形で返ります。

```json
{ "error": "所持金が足りません。" }
```

| ステータス | 意味 |
| --- | --- |
| 400 | リクエストの形式が不正 |
| 401 | 未ログイン |
| 403 | 権限不足(他人の操作・管理者専用) |
| 404 | 対象が存在しない |
| 409 | 状態が競合している(すでにログイン済み等) |
| 422 | ゲームルール上できない(所持金不足・クールタイム中など) |
| 502 | 外部インスタンスへの問い合わせに失敗 |

### 冪等キー

お金や持ち物が動く操作は、リクエストボディに `idempotency_key`(任意の文字列)を
付けられます。同じキーで再送しても二重に実行されず、最初の結果が返ります。
通信が切れたときの再送を安全にするためのものです。

```json
{ "item_id": 12, "sets": 1, "idempotency_key": "e1f2a3..." }
```

### セッション

ログインすると `town_session` cookie(HttpOnly / SameSite=Lax)が発行されます。
サーバーはトークンのSHA-256しか保存しません。有効期限は30日で、**使うたびに
30日先へ延長**されます。

---

## 認証(MiAuth)

Misskeyの[MiAuth](https://misskey-hub.net/docs/for-developers/api/token/miauth/)で
ログインします。アプリの事前登録は不要です。

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 公開 | POST | `/auth/start` | インスタンスを検証し、承認画面のURLを返す。body: `{instance, origin}` |
| 公開 | POST | `/auth/callback` | 承認済みセッションをログインに引き換える。body: `{session}` |
| 公開 | GET | `/auth/me` | ログイン中のプレイヤーを返す。未ログインなら401 |
| 公開 | POST | `/auth/logout` | セッションを破棄する |

流れ:

1. `POST /auth/start` に自分のインスタンス(例 `misskey.io`)を渡す
2. 返ってきた `url` へブラウザを飛ばす(インスタンスの承認画面)
3. 承認すると `callback` に `?session=...` 付きで戻る
4. その `session` を `POST /auth/callback` へ渡すとcookieが発行される

要求する権限は `write:following` だけです(ゲーム内から他の住民をフォローするため)。
プロフィールの取得は認証不要のAPIで行うため、閲覧の権限は求めません。

インスタンスは管理者がブラックリスト/ホワイトリスト方式で制限できます
(`/admin/instances`)。

## プレイヤー

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 公開 | GET | `/health` | ヘルスチェック |
| 公開 | GET | `/players` | 住民名鑑(公開情報のみ) |
| 公開 | GET | `/participants` | 直近20分に活動した住民 |
| 公開 | GET | `/players/{id}/profile` | その住民の公開プロフィール(所持金・身元は含まない) |
| 本人 | GET | `/players/{id}` | 自分の全ステータス(所持金は台帳から算出) |
| 本人 | POST | `/players/{id}/attendance/checkin` | 今日の出席を記録(街の読み込み時に呼ぶ) |
| 公開 | GET | `/attendance` | 出席表と出席率ランキング |
| 本人 | POST | `/players/{id}/events/roll` | ランダムイベントの抽選(街の読み込み時に呼ぶ) |
| 本人 | GET | `/players/{id}/character` | 対戦キャラクター(未作成ならnull) |
| 本人 | POST | `/players/{id}/character` | キャラクターを作る |
| 本人 | POST | `/players/{id}/character/grow` | キャラクターを育てる |
| 本人 | POST | `/players/{id}/character/battle` | 対戦する |
| 公開 | GET | `/cleague` | キャラクターリーグの順位 |

## 街

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 公開 | GET | `/townmap` | 街マップの施設配置(全街ぶん) |
| 公開 | GET | `/townassets` | マップの背景レイヤー |
| 公開 | GET | `/towns` | 街の一覧(名前・地価) |
| 公開 | GET | `/houses` | 建っている家の一覧(マップ描画用) |
| 公開 | GET | `/assets/{name}` | アップロードされた画像を配信 |
| 本人 | GET | `/players/{id}/houses` | 家の一覧(自分の家かどうかの印付き) |
| 本人 | POST | `/players/{id}/move` | 徒歩/バスで別の街へ移動。運賃と移動時間がかかる |
| 本人 | POST | `/players/{id}/warp` | ワープ(高額・即時) |

## 仕事・店・アイテム

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 公開 | GET | `/jobs` | 就ける職業の一覧 |
| 公開 | GET | `/items` | 店の商品カタログ |
| 本人 | POST | `/players/{id}/work` | アルバイト。給料・経験値・体重減少などを返す |
| 本人 | POST | `/players/{id}/job` | 転職(職業安定所) |
| 本人 | POST | `/players/{id}/buy` | 商品を買う。body: `{item_id, sets}` |
| 本人 | POST | `/players/{id}/use` | 持ち物を使う。body: `{item_id}` |

## 施設

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 公開 | GET | `/facilities/{facility}/menu` | 施設のメニュー(`syokudou` / `gym` / `kyushitu` / `school` など) |
| 本人 | POST | `/players/{id}/facilities/{facility}/use` | メニューを実行する(ジムのトレーニング等) |
| 本人 | POST | `/players/{id}/eat` | 食堂で食事する |
| 本人 | POST | `/players/{id}/school/attend` | 学校で受講する(頭脳パラメータ上昇・1ゲーム日1回) |
| 本人 | POST | `/players/{id}/hospital/treat` | 病院で治療する。料金はサーバー側で決まる |
| 本人 | POST | `/players/{id}/onsen/bathe` | 温泉に入る(パワー回復が加速する) |
| 本人 | POST | `/players/{id}/onsen/tick` | 入浴中の回復を現在時刻まで進める(画面のポーリング用) |
| 本人 | POST | `/players/{id}/onsen/leave` | 温泉から出る |

## 銀行

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 本人 | POST | `/players/{id}/bank/deposit` | 普通口座へ預ける |
| 本人 | POST | `/players/{id}/bank/withdraw` | 普通口座から引き出す |
| 本人 | GET | `/players/{id}/bank/statement` | 入出金明細。`?account=super` でスーパー定期 |
| 本人 | GET | `/players/{id}/bank/transfer` | 振込先の候補と上限(相手ごとに本日の残り) |
| 本人 | POST | `/players/{id}/bank/transfer` | 他の住民へ振り込む(`to_id`。上限超過分は減額され手元に残る) |
| 本人 | POST | `/players/{id}/bank/super/deposit` | スーパー定期に預ける |
| 本人 | POST | `/players/{id}/bank/super/cancel` | スーパー定期を解約する |
| 本人 | GET | `/players/{id}/bank/loan/quote` | ローンの見積もり |
| 本人 | POST | `/players/{id}/bank/loan/borrow` | 借りる |
| 本人 | POST | `/players/{id}/bank/loan/repay` | 返す |

## 株・競馬

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 公開 | GET | `/stocks` | 現在の株価と値動きのログ |
| 本人 | GET | `/players/{id}/stocks` | 保有株(損益つき)と売買履歴 |
| 本人 | POST | `/players/{id}/stocks/buy` | 買う |
| 本人 | POST | `/players/{id}/stocks/sell` | 売る |
| 本人 | POST | `/players/{id}/stocks/settle` | 決済する |
| 本人 | GET | `/players/{id}/keiba` | 出走表と収支ランキング |
| 本人 | POST | `/players/{id}/keiba/bet` | 馬券を買ってレースを走らせ、結果を返す |

## カジノ

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 本人 | POST | `/players/{id}/casino/{game}/play` | 1回プレイする(サイコロ・スロット・くじ等) |
| 本人 | GET | `/players/{id}/scratch/{game}` | スクラッチの状態 |
| 本人 | POST | `/players/{id}/scratch/{game}/open` | スクラッチを削る |
| 本人 | GET | `/players/{id}/blackjack` | ブラックジャックの状態 |
| 本人 | POST | `/players/{id}/blackjack/start` | 開始(レート指定) |
| 本人 | POST | `/players/{id}/blackjack/hit` | ヒット |
| 本人 | POST | `/players/{id}/blackjack/stand` | スタンド |
| 本人 | GET | `/players/{id}/poker` | ポーカーの状態 |
| 本人 | POST | `/players/{id}/poker/buy` | コインを買う |
| 本人 | POST | `/players/{id}/poker/deal` | 配る |
| 本人 | POST | `/players/{id}/poker/draw` | 引き直す。body: `{hold}` |
| 本人 | POST | `/players/{id}/poker/cashout` | コインを換金する |
| 本人 | GET | `/players/{id}/loto6` | ロト6の状態 |
| 本人 | POST | `/players/{id}/loto6/buy` | 買う |

## 家・会社

家に関する操作は `house_id` で対象を指定します(`{id}` は操作する本人)。

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 本人 | GET | `/players/{id}/building` | 建設会社の画面(街と地価、外装・内装の一覧) |
| 本人 | POST | `/players/{id}/building/build` | 家を建てる |
| 本人 | POST | `/players/{id}/building/rebuild` | 建て替える |
| 本人 | POST | `/players/{id}/building/sell` | 取り壊して地価ぶんを受け取る |
| 本人 | POST | `/players/{id}/building/comment` | 家のマウスオーバーコメントを設定 |
| 本人 | POST | `/players/{id}/building/contents` | 家のコンテンツ枠(掲示板・店など)を設定 |
| 本人 | POST | `/players/{id}/building/saisen` | さい銭を入れる |
| 本人 | POST | `/players/{id}/building/shop/open` | 店を開く・設定する |
| 本人 | GET | `/players/{id}/building/orosi` | 卸問屋の品揃え |
| 本人 | POST | `/players/{id}/building/shiire` | 仕入れる |
| 本人 | GET | `/players/{id}/building/shop` | 店の陳列(訪問者向け) |
| 本人 | POST | `/players/{id}/building/shop/buy` | 店で買う |
| 本人 | GET | `/players/{id}/building/shop/stock` | 自分の店の在庫(価格設定用) |
| 本人 | POST | `/players/{id}/building/shop/price` | 品ごとの売値を設定 |
| 本人 | GET | `/players/{id}/building/yami` | 持ち物販売店(闇市)の棚 |
| 本人 | GET | `/players/{id}/building/yami/inventory` | 出品できる自分の持ち物 |
| 本人 | POST | `/players/{id}/building/yami/list` | 闇市に出品する |
| 本人 | POST | `/players/{id}/building/yami/buy` | 闇市で買う |
| 本人 | GET | `/players/{id}/building/company` | 会社(運営/株式会社)の画面 |
| 本人 | POST | `/players/{id}/building/company/staff` | 社員を雇う |
| 本人 | POST | `/players/{id}/building/company/educate` | 社員教育(自分のパラメータを移す) |
| 本人 | POST | `/players/{id}/building/company/seizou` | 独自商品を製造する(オーナーのみ・1日1回) |
| 本人 | POST | `/players/{id}/building/company/approve` | 入会/退会希望を承認する(オーナーのみ) |
| 本人 | POST | `/players/{id}/building/company/kick` | 社員を退会させる(オーナーのみ) |

## 交流(メール・あいさつ・掲示板)

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 本人 | GET | `/players/{id}/mail` | 受信箱(開くと既読になる) |
| 本人 | GET | `/players/{id}/mail/unread` | 未読数だけ(既読にしない) |
| 本人 | POST | `/players/{id}/mail/send` | 送る |
| 本人 | DELETE | `/players/{id}/mail/{msgId}` | 消す |
| 本人 | PUT | `/players/{id}/mail/{msgId}/save` | 保存する/しない |
| 公開 | GET | `/greetings` | 最近のあいさつ。`?limit=` |
| 公開 | GET | `/greetings/stream` | あいさつのSSE配信(投稿・削除のたびに最新一覧をpush) |
| 本人 | POST | `/players/{id}/greetings` | あいさつを投稿する |
| 本人 | GET | `/players/{id}/building/bbs` | 家の掲示板を読む。`?house_id=` |
| 本人 | POST | `/players/{id}/building/bbs/post` | 家の掲示板に書く |
| 本人 | POST | `/players/{id}/building/bbs/delete` | 家の掲示板の記事を消す |
| 本人 | POST | `/players/{id}/building/company/bbs` | 会社BBSに書く(入会/退会希望もここから) |
| 本人 | POST | `/players/{id}/building/company/bbs/delete` | 会社BBSの記事を消す(オーナーのみ) |

投稿本文にはカスタム絵文字のショートコード `:name@host:` を書けます
(1投稿20個まで)。詳しくは[Misskey連携](#misskey連携)を参照してください。

## 役場(ニュース・ランキング)

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 公開 | GET | `/news` | 街のニュース。`?limit=` |
| 公開 | GET | `/players/{id}/news` | その住民の出来事の履歴 |
| 公開 | GET | `/ranking` | ランキング1種。`?key=` `?limit=` `?self=` |
| 公開 | GET | `/ranking/keys` | 選べるランキングの一覧 |

## イベント(釣り・ギフト・特典・ビンゴ)

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| 本人 | GET | `/players/{id}/fishing` | 釣りの状態(進行中の勝負、または使える餌) |
| 本人 | POST | `/players/{id}/fishing/start` | 餌を1つ使って始める |
| 本人 | POST | `/players/{id}/fishing/pick` | カードを1枚めくる |
| 本人 | GET | `/players/{id}/gifts` | ギフト屋の状態(手持ちのギフトと変換できる持ち物) |
| 本人 | POST | `/players/{id}/gifts/convert` | 手数料を払って持ち物をギフトに変える |
| 本人 | POST | `/players/{id}/serial/redeem` | シリアルコードを使う(特典交換所) |
| 本人 | GET | `/players/{id}/bingo` | 開催中のビンゴと自分のカード |
| 本人 | POST | `/players/{id}/bingo/card` | カードをもう1枚もらう |
| 本人 | POST | `/players/{id}/bingo/claim` | 揃ったカードを申告して賞金を受け取る |

## Misskey連携

| 認可 | Method | Path | 内容 |
| --- | --- | --- | --- |
| ログイン | GET | `/players/{id}/misskey` | **その住民の**Misskeyプロフィールと、自分との関係(フォロー状態) |
| ログイン | POST | `/misskey/follow` | フォローする。body: `{target_id}` |
| ログイン | POST | `/misskey/unfollow` | フォローを外す。body: `{target_id}` |
| ログイン | GET | `/emojis` | 絵文字ピッカー用の一覧。`?host=` 省略時は自分のインスタンス |
| ログイン | POST | `/emojis/resolve` | 絵文字1件が使えるか判定する。body: `{host, name}` |
| 公開 | GET | `/emojis/used` | 投稿を描画するための `:name@host:` → URL 辞書 |

フォローは**実行者のインスタンス上で実行者のトークン**を使うため、パスに
プレイヤーIDを取りません(常にログイン中の本人が主語です)。

絵文字は次の3つをすべて満たすものだけ使えます。判定は選ばれた時点で1件だけ行い、
以後キャッシュします。

- `localOnly` が false(連合させない設定でない)
- `isSensitive` が false
- ライセンスが設定されている(「どこから取り込んだか」の記載だけでは不可)

`/emojis/resolve` は使えない場合も200で返し、理由を示します。

```json
{ "allowed": false, "reason": "no_license", "message": "この絵文字は…" }
```

`reason` は `no_license` / `sensitive` / `local_only` / `not_found` のいずれかです。

## 管理者

すべて `admin` ロールが必要です。

| Method | Path | 内容 |
| --- | --- | --- |
| GET / PUT | `/admin/settings` | ゲーム設定(初期所持金・回復間隔など) |
| GET / POST | `/admin/items` | アイテムの一覧・作成 |
| PUT / DELETE | `/admin/items/{id}` | アイテムの更新・削除 |
| GET / POST | `/admin/jobs` | 職業の一覧・作成 |
| PUT / DELETE | `/admin/jobs/{id}` | 職業の更新・削除 |
| POST | `/admin/simulate` | 効果を仮の状態に当てて結果と経済への影響を試算する |
| GET | `/admin/players` | 住民の一覧(MisskeyのIDつき) |
| PUT / DELETE | `/admin/players/{id}` | 住民の編集・論理削除 |
| PUT | `/admin/townmap` | マップの施設配置を差し替える |
| GET | `/admin/townmap/houses` | 家が建っているマス(編集時のロック用) |
| GET / PUT | `/admin/townmap/presets` | 施設プリセット |
| PUT | `/admin/towns` | 街の一覧(名前・地価)を差し替える。街番号は並び順 |
| GET / POST | `/admin/assets` | 画像の一覧・アップロード |
| DELETE | `/admin/assets/{name}` | 画像の削除(配置中は拒否) |
| PUT | `/admin/townassets` | 背景レイヤーの配置を差し替える |
| GET / POST | `/admin/events` | ランダムイベントの一覧・作成 |
| PUT / DELETE | `/admin/events/{eid}` | ランダムイベントの更新・削除 |
| GET / POST | `/admin/serials` | シリアルコードの一覧・発行 |
| PUT / DELETE | `/admin/serials/{sid}` | シリアルコードの更新・削除 |
| GET | `/admin/serials/{sid}/uses` | そのコードを誰が使ったか |
| POST | `/admin/bingo` | ビンゴ大会を開催する |
| DELETE | `/admin/greetings/{gid}` | あいさつを削除する(モデレーション) |
| GET | `/admin/instances` | インスタンスの許可/拒否リストと現在の方針 |
| PUT | `/admin/instances` | ルールを追加・置き換える |
| DELETE | `/admin/instances/{host}` | ルールを削除する |
