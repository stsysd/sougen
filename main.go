// Package main はアプリケーションのエントリーポイントを提供します。
package main

import (
	"log"

	"github.com/stsysd/sougen/api"
	"github.com/stsysd/sougen/config"
	"github.com/stsysd/sougen/store"
)

func main() {
	// 設定の読み込み
	cfg := config.NewConfig()

	// API認証トークンのチェック
	if cfg.APIToken == "" {
		log.Println("WARNING: No API token set (SOUGEN_API_TOKEN). API will not be secure!")
	}

	// SQLiteストアの初期化
	sqliteStore, err := store.NewSQLiteStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("Failed to initialize SQLite store: %v", err)
	}
	defer sqliteStore.Close()

	// サーバーインスタンスの作成
	server := api.NewServer(sqliteStore, cfg)

	// サーバーの起動
	log.Fatal(server.Run(":" + cfg.Port))
}
