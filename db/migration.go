package db

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed schema/*.sql
var embedMigrations embed.FS

// Migrate はデータベースに対してマイグレーションを実行します。
func Migrate(conn *sql.DB) error {
	// 外部キー制約を有効化
	_, err := conn.Exec(`PRAGMA foreign_keys = ON;`)
	if err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// goose の設定
	goose.SetBaseFS(embedMigrations)

	// SQLite 用に goose を設定
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	// マイグレーションを実行
	if err := goose.Up(conn, "schema"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
