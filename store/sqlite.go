// Package store は、データの永続化機能を提供します。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stsysd/sougen/internal/db"
	"github.com/stsysd/sougen/model"
)

// RecordStore はレコードの保存と取得を行うインターフェースです。
type RecordStore interface {
	// CreateRecord は新しいレコードを作成します。
	CreateRecord(record *model.Record) error
	// GetRecord は指定されたIDのレコードを取得します。
	GetRecord(id uuid.UUID) (*model.Record, error)
	// DeleteRecord は指定されたIDのレコードを削除します。
	DeleteRecord(id uuid.UUID) error
	// DeleteProject は指定されたプロジェクトのすべてのレコードを削除します。
	DeleteProject(projectName string) error
	// DeleteRecordsUntil は指定日時より前のレコードを削除します。
	DeleteRecordsUntil(project string, until time.Time) (int, error)
	// ListRecords は指定されたプロジェクトの、指定した期間内のレコードを取得します。
	ListRecords(project string, from, to time.Time) ([]*model.Record, error)
	// GetProjectInfo は指定されたプロジェクトの情報を取得します。
	GetProjectInfo(projectName string) (*model.ProjectInfo, error)
	// Close はストアの接続を閉じます。
	Close() error
}

// SQLiteStore はSQLiteを使用したRecordStoreの実装です。
type SQLiteStore struct {
	conn    *sql.DB
	queries *db.Queries
}

// NewSQLiteStore は新しいSQLiteStoreを作成します。
func NewSQLiteStore(dataDir string) (*SQLiteStore, error) {
	// データディレクトリの作成（存在しない場合）
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// SQLiteデータベースファイルのパス
	dbPath := filepath.Join(dataDir, "sougen.db")

	// SQLiteデータベースへの接続
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite database: %w", err)
	}

	// テーブルの初期化
	if err := initTables(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to initialize database tables: %w", err)
	}

	return &SQLiteStore{
		conn:    conn,
		queries: db.New(conn),
	}, nil
}

// initTables はデータベーステーブルを初期化します。
func initTables(conn *sql.DB) error {
	// recordsテーブルの作成
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS records (
			id TEXT PRIMARY KEY,
			project TEXT NOT NULL,
			value INTEGER NOT NULL,
			done_at TEXT NOT NULL
		);
		
		CREATE INDEX IF NOT EXISTS idx_records_project_done_at 
		ON records(project, done_at);
	`)
	return err
}

// CreateRecord は新しいレコードをデータベースに保存します。
func (s *SQLiteStore) CreateRecord(record *model.Record) error {
	// バリデーション
	if err := record.Validate(); err != nil {
		return err
	}

	// 日時をRFC3339形式に統一して保存
	formattedTime := record.DoneAt.Format(time.RFC3339)

	// sqlcで生成されたクエリを使用
	return s.queries.CreateRecord(context.Background(), db.CreateRecordParams{
		ID:      record.ID.String(),
		Project: record.Project,
		Value:   int64(record.Value),
		DoneAt:  formattedTime,
	})
}

// GetRecord は指定されたIDのレコードを取得します。
func (s *SQLiteStore) GetRecord(id uuid.UUID) (*model.Record, error) {
	// sqlcで生成されたクエリを使用
	dbRecord, err := s.queries.GetRecord(context.Background(), id.String())
	if err == sql.ErrNoRows {
		return nil, errors.New("record not found")
	}
	if err != nil {
		return nil, err
	}

	// 文字列から時間に変換
	doneAt, err := time.Parse(time.RFC3339, dbRecord.DoneAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse record date: %w", err)
	}

	// UUIDの解析
	recordID, err := uuid.Parse(dbRecord.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid UUID in database: %w", err)
	}

	// レコードの作成
	return model.LoadRecord(recordID, doneAt, dbRecord.Project, int(dbRecord.Value))
}

// ListRecords は指定されたプロジェクトの、指定した期間内のレコードを取得します。
func (s *SQLiteStore) ListRecords(project string, from, to time.Time) ([]*model.Record, error) {
	// 日付の範囲を丸一日に設定（秒以下の精度を取り除く）
	// fromは日付の始まりに設定
	fromDate := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	fromStr := fromDate.Format(time.RFC3339)

	// toは日付の終わりに設定（次の日の0時の直前）
	toDate := time.Date(to.Year(), to.Month(), to.Day(), 23, 59, 59, 999999999, to.Location())
	toStr := toDate.Format(time.RFC3339)

	// sqlcで生成されたクエリを使用
	dbRecords, err := s.queries.ListRecords(context.Background(), db.ListRecordsParams{
		DoneAt:   fromStr,
		DoneAt_2: toStr,
		Project:  project,
	})
	if err != nil {
		return nil, err
	}

	// 結果の変換
	var records []*model.Record
	for _, dbRecord := range dbRecords {
		// 文字列から時間に変換
		doneAt, err := time.Parse(time.RFC3339, dbRecord.DoneAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse record date: %w", err)
		}

		// UUID の解析
		id, err := uuid.Parse(dbRecord.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID in database: %w", err)
		}

		// レコードの作成
		record, err := model.LoadRecord(id, doneAt, dbRecord.Project, int(dbRecord.Value))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, nil
}

// Close はデータベース接続を閉じます。
func (s *SQLiteStore) Close() error {
	return s.conn.Close()
}

// DeleteRecord は指定されたIDのレコードを削除します。
func (s *SQLiteStore) DeleteRecord(id uuid.UUID) error {
	// sqlcで生成されたクエリを使用
	result, err := s.queries.DeleteRecord(context.Background(), id.String())
	if err != nil {
		return err
	}

	// 削除された行数を確認
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// レコードが見つからない場合
	if rowsAffected == 0 {
		return errors.New("record not found")
	}

	return nil
}

// GetProjectInfo は指定されたプロジェクトの情報を取得します。
func (s *SQLiteStore) GetProjectInfo(projectName string) (*model.ProjectInfo, error) {
	// sqlcで生成されたクエリを使用
	projectInfo, err := s.queries.GetProjectInfo(context.Background(), projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to query project info: %w", err)
	}

	// レコードがない場合はエラーを返す
	if projectInfo.RecordCount == 0 {
		return nil, sql.ErrNoRows
	}

	// 日時のパース
	var firstRecordAt, lastRecordAt time.Time

	if firstRecordAtStr, ok := projectInfo.FirstRecordAt.(string); ok && firstRecordAtStr != "" {
		firstRecordAt, err = time.Parse(time.RFC3339, firstRecordAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse first record date: %w", err)
		}
	}

	if lastRecordAtStr, ok := projectInfo.LastRecordAt.(string); ok && lastRecordAtStr != "" {
		lastRecordAt, err = time.Parse(time.RFC3339, lastRecordAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse last record date: %w", err)
		}
	}

	// interface{}をintに変換
	totalValue := 0
	if tv, ok := projectInfo.TotalValue.(int64); ok {
		totalValue = int(tv)
	}

	// ProjectInfoオブジェクトの作成
	return model.NewProjectInfo(
		projectName,
		int(projectInfo.RecordCount),
		totalValue,
		firstRecordAt,
		lastRecordAt,
	), nil
}

// DeleteProject は指定されたプロジェクトのすべてのレコードを削除します。
func (s *SQLiteStore) DeleteProject(projectName string) error {
	// トランザクションの開始
	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// トランザクションをロールバックするための遅延関数
	defer func() {
		if tx != nil {
			tx.Rollback() // 成功した場合は既にnilになっているためエラーは無視
		}
	}()

	// sqlcで生成されたクエリを使用（トランザクション内で）
	queriesWithTx := s.queries.WithTx(tx)
	err = queriesWithTx.DeleteProject(context.Background(), projectName)
	if err != nil {
		return fmt.Errorf("failed to delete project records: %w", err)
	}

	// トランザクションのコミット
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	tx = nil // コミットが成功したのでnilにして遅延関数でのロールバックを防ぐ

	return nil
}

// DeleteRecordsUntil は指定日時より前のレコードを削除します。
func (s *SQLiteStore) DeleteRecordsUntil(project string, until time.Time) (int, error) {
	// トランザクションの開始
	tx, err := s.conn.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// トランザクションをロールバックするための遅延関数
	defer func() {
		if tx != nil {
			tx.Rollback() // 成功した場合は既にnilになっているためエラーは無視
		}
	}()

	// 日時を文字列に変換
	untilStr := until.Format(time.RFC3339)

	// sqlcで生成されたクエリを使用（トランザクション内で）
	queriesWithTx := s.queries.WithTx(tx)
	var result sql.Result
	if project == "" {
		// 特定のプロジェクト指定がない場合は全プロジェクトから削除
		result, err = queriesWithTx.DeleteRecordsUntil(context.Background(), untilStr)
	} else {
		// 特定プロジェクトのレコードを削除
		result, err = queriesWithTx.DeleteRecordsUntilByProject(context.Background(), db.DeleteRecordsUntilByProjectParams{
			Project: project,
			DoneAt:  untilStr,
		})
	}

	if err != nil {
		return 0, fmt.Errorf("failed to delete records until specified date: %w", err)
	}

	// 削除された行数を取得
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	// トランザクションのコミット
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}
	tx = nil // コミットが成功したのでnilにして遅延関数でのロールバックを防ぐ

	return int(rowsAffected), nil
}
