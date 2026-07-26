# ビルドコンテキストはリポジトリのルート(画面もこのイメージへ同梱するため)。
#   docker build -f backend/Dockerfile .

# 画面(Vue)をビルドする。公開環境ではViteの開発サーバを使わず、
# ここで作った静的ファイルをAPIと同じプロセスから配る。
FROM node:22-alpine AS web
WORKDIR /w
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --include=dev
COPY frontend/ ./
RUN npm run build

FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o /out/town ./cmd/town

FROM alpine:3.20
# Asia/Tokyo等のタイムゾーン解決に tzdata が必要
RUN apk add --no-cache tzdata ca-certificates
WORKDIR /app
COPY --from=build /out/town /app/town
COPY --from=web /w/dist /app/web
# 設定ファイルは同梱しない。コンテナは環境変数(TOWN_DATABASE_URL 等)で設定する。
ENTRYPOINT ["/app/town"]
