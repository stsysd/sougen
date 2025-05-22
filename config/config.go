// Package config はアプリケーション設定を管理します。
package config

import (
	"os"
	"path/filepath"
)

// Config はアプリケーション全体の設定を保持します。
type Config struct {
	// データディレクトリのパス
	DataDir string

	// HTTPサーバーのポート
	Port string

	// API認証トークン
	APIToken string
}

// NewConfig は環境変数から設定を読み込み、Configインスタンスを生成します。
func NewConfig() *Config {
	// データディレクトリの設定
	dataDir := os.Getenv("SOUGEN_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(".", "data")
	}

	// ポートの設定
	port := os.Getenv("SOUGEN_SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	// API認証トークンの設定
	apiToken := os.Getenv("SOUGEN_API_TOKEN")
	if apiToken == "" {
		// デフォルトトークンは設定しない
		panic("SOUGEN_API_TOKEN is not set")
	}

	return &Config{
		DataDir:  dataDir,
		Port:     port,
		APIToken: apiToken,
	}
}
