// Package store は、データの永続化機能を提供します。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
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
	db *sql.DB
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
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite database: %w", err)
	}

	// テーブルの初期化
	if err := initTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database tables: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// initTables はデータベーステーブルを初期化します。
func initTables(db *sql.DB) error {
	// recordsテーブルの作成
	_, err := db.Exec(`
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

	// レコードの挿入
	_, err := s.db.Exec(
		`INSERT INTO records 
		(id, project, value, done_at)
		VALUES (?, ?, ?, ?)`,
		record.ID.String(),
		record.Project,
		record.Value,
		formattedTime,
	)

	return err
}

// GetRecord は指定されたIDのレコードを取得します。
func (s *SQLiteStore) GetRecord(id uuid.UUID) (*model.Record, error) {
	row := s.db.QueryRow(
		`SELECT project, value, done_at FROM records WHERE id = ?`,
		id.String(),
	)

	var (
		project   string
		value     int
		doneAtStr string
	)

	err := row.Scan(
		&project,
		&value,
		&doneAtStr,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("record not found")
	}
	if err != nil {
		return nil, err
	}

	// 文字列から時間に変換
	doneAt, err := time.Parse(time.RFC3339, doneAtStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse record date: %w", err)
	}

	// レコードの作成
	record, err := model.LoadRecord(id, doneAt, project, value)
	if err != nil {
		return nil, err
	}

	return record, nil
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

	// SQLクエリの実行
	rows, err := s.db.Query(
		`SELECT id, value, done_at FROM records 
		WHERE project = ? AND done_at BETWEEN ? AND ? 
		ORDER BY done_at`,
		project, fromStr, toStr,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 結果の処理
	var records []*model.Record
	for rows.Next() {
		var (
			idStr     string
			value     int
			doneAtStr string
		)
		if err := rows.Scan(&idStr, &value, &doneAtStr); err != nil {
			return nil, err
		}

		// 文字列から時間に変換
		doneAt, err := time.Parse(time.RFC3339, doneAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse record date: %w", err)
		}

		// UUID の解析
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID in database: %w", err)
		}

		// レコードの作成
		record, err := model.LoadRecord(id, doneAt, project, value)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	// エラーチェック
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

// Close はデータベース接続を閉じます。
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// DeleteRecord は指定されたIDのレコードを削除します。
func (s *SQLiteStore) DeleteRecord(id uuid.UUID) error {
	result, err := s.db.Exec(
		`DELETE FROM records WHERE id = ?`,
		id.String(),
	)
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
	// プロジェクトの統計情報を取得するSQL
	query := `
		SELECT 
			COUNT(*) as record_count,
			COALESCE(SUM(value), 0) as total_value,
			MIN(done_at) as first_record_at,
			MAX(done_at) as last_record_at
		FROM records
		WHERE project = ?
	`

	var recordCount int
	var totalValue int
	var firstRecordAtStr, lastRecordAtStr sql.NullString

	err := s.db.QueryRow(query, projectName).Scan(&recordCount, &totalValue, &firstRecordAtStr, &lastRecordAtStr)
	if err != nil {
		return nil, fmt.Errorf("failed to query project info: %w", err)
	}

	// レコードがない場合はエラーを返す
	if recordCount == 0 {
		return nil, sql.ErrNoRows
	}

	// 日時のパース
	var firstRecordAt, lastRecordAt time.Time

	if firstRecordAtStr.Valid {
		firstRecordAt, err = time.Parse(time.RFC3339, firstRecordAtStr.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse first record date: %w", err)
		}
	}

	if lastRecordAtStr.Valid {
		lastRecordAt, err = time.Parse(time.RFC3339, lastRecordAtStr.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse last record date: %w", err)
		}
	}

	// ProjectInfoオブジェクトの作成
	projectInfo := model.NewProjectInfo(
		projectName,
		recordCount,
		totalValue,
		firstRecordAt,
		lastRecordAt,
	)

	return projectInfo, nil
}

// DeleteProject は指定されたプロジェクトのすべてのレコードを削除します。
func (s *SQLiteStore) DeleteProject(projectName string) error {
	// トランザクションの開始
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// トランザクションをロールバックするための遅延関数
	defer func() {
		if tx != nil {
			tx.Rollback() // 成功した場合は既にnilになっているためエラーは無視
		}
	}()

	// プロジェクトの全レコードを削除
	_, err = tx.Exec(
		`DELETE FROM records WHERE project = ?`,
		projectName,
	)
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
	tx, err := s.db.Begin()
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

	var result sql.Result
	if project == "" {
		// 特定のプロジェクト指定がない場合は全プロジェクトから削除
		result, err = tx.Exec(
			`DELETE FROM records WHERE done_at < ?`,
			untilStr,
		)
	} else {
		// 特定プロジェクトのレコードを削除
		result, err = tx.Exec(
			`DELETE FROM records WHERE project = ? AND done_at < ?`,
			project, untilStr,
		)
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
