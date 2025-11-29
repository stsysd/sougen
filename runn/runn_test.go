package runn

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/k1LoW/runn"
	"github.com/stsysd/sougen/api"
	"github.com/stsysd/sougen/config"
	"github.com/stsysd/sougen/db"
	"github.com/stsysd/sougen/store"
)

func TestRouter(t *testing.T) {
  os.Setenv("SOUGEN_API_KEY", "test-token")
	os.Setenv("SOUGEN_DATA_DIR", "./testdata")

  if err := os.RemoveAll("./testdata"); err != nil {
    t.Fatalf("Failed to clean test data dir: %v", err)
  }

	// 設定の読み込み
	cfg := config.NewConfig()

	// SQLiteストアの初期化（マイグレーション関数を渡す）
	sqliteStore, err := store.NewSQLiteStore(cfg.DataDir, db.Migrate)
	if err != nil {
		t.Fatalf("Failed to initialize SQLite store: %v", err)
	}
	defer sqliteStore.Close()

	// サーバーインスタンスの作成
	server := api.NewServer(sqliteStore, cfg)

	ctx := context.Background()
	ts := httptest.NewServer(server)
	t.Cleanup(func() {
		ts.Close()
	})
	opts := []runn.Option{
		runn.T(t),
		runn.Runner("req", ts.URL),
    runn.Var("api_key", "test-token"),
	}
	o, err := runn.Load("./**/*.yml", opts...)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.RunN(ctx); err != nil {
		t.Fatal(err)
	}
}
