# town

2000年代のCGIゲーム「TOWN ver.1.40」を Go + Vue 3 + PostgreSQL + Redis で作り直したものです。

街に住民として参加し、働いて稼ぎ、買い物や食事で体調を整え、家を建て、掲示板やあいさつで
他の住民と交流する — というブラウザゲームです。

## これは何か

オリジナルはPerl CGI(Shift_JIS・ファイルベース)で書かれた個人制作のゲームです。
本リポジトリはその**挙動を仕様として読み解いた上での再実装**であり、
オリジナルのコードは一切含みません(`.gitignore`で除外しています)。

ゲームバランス(給料・物価・パラメータの上がり方・確率など)はオリジナルの値を踏襲し、
UIも当時の雰囲気を保っています。一方、以下は現代的な作りに置き換えています。

- お金は**複式の台帳**で管理し、残高は台帳から算出する(不整合が起きない)
- 時間進行(日次処理・株価・利息)は**専用のworker**が担当する
- 掲示板やプロフィールに**生のHTMLを保存しない**(XSS対策)

## 構成

| 層 | 使っているもの |
| --- | --- |
| バックエンド | Go 1.26 / `net/http` (Go 1.22 ServeMux) / pgx / goose |
| フロントエンド | Vue 3 + TypeScript + Vite (SPA) |
| データ | PostgreSQL 16 (永続) / Redis 7 (揮発・リーダー選出) |

```
├── backend/          # Go。1バイナリ2モード(web / worker)
│   ├── cmd/town/
│   └── internal/     # httpapi, player, action, content, ledger, worker, ...
├── frontend/         # Vue 3 SPA
└── deploy/
    └── compose.yaml  # postgres + redis + web + worker + frontend
```

## 街にあるもの

デパート・自動販売機・セントラル食堂・銀行・株取引場・競馬場・ゲームセンター(カジノ各種)・
職業安定所・中央病院・温泉・スポーツクラブ・学校・教室・建設会社・役場(住民名鑑/街のニュース/
ランキング)・釣りゲーム・ギフト屋・特典交換所・ビンゴ会場・プロフィール、
そして住民が建てた家(掲示板・お店・独自コンテンツを置ける)。

## 動かす

```sh
cp deploy/compose.override.yaml.example deploy/compose.override.yaml   # 公開アドレスと鍵を書く
docker compose -f deploy/compose.yaml -f deploy/compose.override.yaml up -d --build
```

| URL | 内容 |
| --- | --- |
| http://localhost:5173 | ゲーム画面(Vite開発サーバ) |
| http://localhost:8090 | REST API |

PostgreSQLは`55432`、Redisは`56379`で公開しています(ホストの標準ポートとぶつからないように)。
DBのマイグレーションはwebの起動時に自動で適用されます。

バックエンドだけホストで動かす場合:

```sh
docker compose -f deploy/compose.yaml up -d postgres redis
cd backend
cp default.yml.example default.yml   # 設定ファイルは任意(環境変数だけでも動く)
go run ./cmd/town web      # REST API
go run ./cmd/town worker   # 時間進行(別ターミナル)
```

## ドキュメント

- [docs/architecture.md](docs/architecture.md) — プロジェクト構成と設計上の決めごと
- [docs/api.md](docs/api.md) — APIリファレンス(全162エンドポイント)

## テスト

```sh
cd backend
TOWN_TEST_DATABASE_URL="postgres://town:town@localhost:55432/town_test?sslmode=disable" \
  go test ./...
```

フロントエンドの型チェック:

```sh
cd frontend && npx vue-tsc --noEmit
```

## オリジナルについて

TOWN ver.1.40 — Copyright (c) 2003-2004 brassiere (<http://brassiere.jp/>)

長く遊ばれてきた作品で、本リポジトリはその設計と作り込みに多くを学んでいます。
繰り返しになりますが、オリジナルのソースコードは本リポジトリに含まれていません。

## ライセンス

MIT License — [LICENSE](LICENSE) を参照してください。
