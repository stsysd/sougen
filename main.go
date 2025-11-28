// Package main はアプリケーションのエントリーポイントを提供します。
package main

import (
	"log"

	"github.com/stsysd/sougen/api"
	"github.com/stsysd/sougen/config"
	"github.com/stsysd/sougen/db"
	"github.com/stsysd/sougen/store"
)

func main() {
	// 設定の読み込み
	cfg := config.NewConfig()

	// SQLiteストアの初期化（マイグレーション関数を渡す）
	sqliteStore, err := store.NewSQLiteStore(cfg.DataDir, db.Migrate)
	if err != nil {
		log.Fatalf("Failed to initialize SQLite store: %v", err)
	}
	defer sqliteStore.Close()

	// サーバーインスタンスの作成
	server := api.NewServer(sqliteStore, cfg)

	// サーバーの起動
	log.Fatal(server.Run(":" + cfg.Port))
}
