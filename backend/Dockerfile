FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/town ./cmd/town

FROM alpine:3.20
# Asia/Tokyo等のタイムゾーン解決に tzdata が必要
RUN apk add --no-cache tzdata ca-certificates
WORKDIR /app
COPY --from=build /out/town /app/town
COPY default.yml /app/default.yml
ENTRYPOINT ["/app/town"]
