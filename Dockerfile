# ビルドステージ
FROM golang:1.22-alpine AS builder

# ビルドに必要なパッケージのインストール
RUN apk add --no-cache git gcc musl-dev

# 作業ディレクトリの設定
WORKDIR /app

# 依存関係をコピーしてダウンロード
COPY go.mod go.sum ./
RUN go mod download

# ソースコードをコピー
COPY . .

# アプリケーションをビルド
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o sougen .

# 実行ステージ
FROM alpine:3.19

# SQLite3と証明書をインストール
RUN apk add --no-cache sqlite ca-certificates tzdata

# 作業ディレクトリの作成
WORKDIR /app

# ビルドステージからバイナリをコピー
COPY --from=builder /app/sougen /app/

# データディレクトリを作成
RUN mkdir -p /app/data && chmod 777 /app/data

# 環境変数の設定
ENV SOUGEN_DATA_DIR=/app/data
ENV PORT=8080

# ユーザーを作成して権限を設定
RUN addgroup -g 1000 -S sougen && \
    adduser -u 1000 -S sougen -G sougen
RUN chown -R sougen:sougen /app

# sougenユーザーに切り替え
USER sougen

# ポートの公開
EXPOSE 8080

# アプリケーションの実行
CMD ["/app/sougen"]
