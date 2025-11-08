// Package store は、データの永続化機能を提供します。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stsysd/sougen/db"
	"github.com/stsysd/sougen/model"
)

// RecordStore はレコードの保存と取得を行うインターフェースです。
type RecordStore interface {
	// CreateRecord は新しいレコードを作成します。
	CreateRecord(ctx context.Context, record *model.Record) error
	// GetRecord は指定されたIDのレコードを取得します。
	GetRecord(ctx context.Context, id uuid.UUID) (*model.Record, error)
	// UpdateRecord は指定されたIDのレコードを更新します。
	UpdateRecord(ctx context.Context, record *model.Record) error
	// DeleteRecord は指定されたIDのレコードを削除します。
	DeleteRecord(ctx context.Context, id uuid.UUID) error
	// DeleteProject は指定されたプロジェクトのすべてのレコードを削除します。
	DeleteProject(ctx context.Context, projectName string) error
	// DeleteRecordsUntil は指定日時より前のレコードを削除します。
	DeleteRecordsUntil(ctx context.Context, project string, until time.Time) (int, error)
	// ListRecords は指定されたプロジェクトの、指定した期間内のレコードを取得します。
	ListRecords(ctx context.Context, project string, from, to time.Time, sortOrder *model.SortOrder) ([]*model.Record, error)
	// ListRecordsWithTags は指定されたプロジェクトの、指定した期間内の、指定されたタグを持つレコードを取得します。
	ListRecordsWithTags(ctx context.Context, project string, from, to time.Time, tags []string, sortOrder *model.SortOrder) ([]*model.Record, error)
	// Close はストアの接続を閉じます。
	Close() error
}

// ProjectStore はプロジェクトの保存と取得を行うインターフェースです。
type ProjectStore interface {
	// CreateProject は新しいプロジェクトを作成します。
	CreateProject(ctx context.Context, project *model.Project) error
	// GetProject は指定された名前のプロジェクトを取得します。
	GetProject(ctx context.Context, name string) (*model.Project, error)
	// UpdateProject は指定されたプロジェクトを更新します。
	UpdateProject(ctx context.Context, project *model.Project) error
	// DeleteProjectEntity はプロジェクトエンティティのみを削除します（レコードは削除しません）。
	DeleteProjectEntity(ctx context.Context, name string) error
	// ListProjects はすべてのプロジェクトを取得します。
	ListProjects(ctx context.Context) ([]*model.Project, error)
	// GetProjectTags は指定されたプロジェクトのタグ一覧を取得します。
	GetProjectTags(ctx context.Context, projectName string) ([]string, error)
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
	// テーブルの作成（外部キー制約なし）
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS projects (
			name TEXT PRIMARY KEY,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		
		CREATE TABLE IF NOT EXISTS records (
			id TEXT PRIMARY KEY,
			project TEXT NOT NULL,
			value INTEGER NOT NULL,
			timestamp TEXT NOT NULL
		);
		
		CREATE TABLE IF NOT EXISTS tags (
			record_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY (record_id, tag)
		);
		
		CREATE INDEX IF NOT EXISTS idx_records_project_timestamp 
		ON records(project, timestamp);
		
		CREATE INDEX IF NOT EXISTS idx_tags_record_id ON tags(record_id);
		CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag);
		CREATE INDEX IF NOT EXISTS idx_projects_updated_at ON projects(updated_at);
	`)
	return err
}

// CreateRecord は新しいレコードをデータベースに保存します。
func (s *SQLiteStore) CreateRecord(ctx context.Context, record *model.Record) error {
	// バリデーション
	if err := record.Validate(); err != nil {
		return err
	}

	// プロジェクトの存在確認（アプリケーションレベルでの整合性チェック）
	_, err := s.GetProject(ctx, record.Project)
	if err != nil {
		return fmt.Errorf("project not found: %s", record.Project)
	}

	// 日時をRFC3339形式に統一して保存
	formattedTime := record.Timestamp.Format(time.RFC3339)

	// sqlcで生成されたクエリを使用
	err = s.queries.CreateRecord(ctx, db.CreateRecordParams{
		ID:        record.ID.String(),
		Project:   record.Project,
		Value:     int64(record.Value),
		Timestamp: formattedTime,
	})
	if err != nil {
		return err
	}

	// タグを個別に挿入
	for _, tag := range record.Tags {
		err = s.queries.CreateRecordTag(ctx, db.CreateRecordTagParams{
			RecordID: record.ID.String(),
			Tag:      tag,
		})
		if err != nil {
			return fmt.Errorf("failed to create tag %s: %w", tag, err)
		}
	}

	return nil
}

// UpdateRecord は指定されたIDのレコードを更新します。
func (s *SQLiteStore) UpdateRecord(ctx context.Context, record *model.Record) error {
	// バリデーション
	if err := record.Validate(); err != nil {
		return err
	}

	// プロジェクトの存在確認（アプリケーションレベルでの整合性チェック）
	_, err := s.GetProject(ctx, record.Project)
	if err != nil {
		return fmt.Errorf("project not found: %s", record.Project)
	}

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

	// 日時をRFC3339形式に統一して更新
	formattedTime := record.Timestamp.Format(time.RFC3339)

	// sqlcで生成されたクエリを使用（トランザクション内で）
	queriesWithTx := s.queries.WithTx(tx)

	// レコードの基本情報を更新
	result, err := queriesWithTx.UpdateRecord(ctx, db.UpdateRecordParams{
		Project:   record.Project,
		Value:     int64(record.Value),
		Timestamp: formattedTime,
		ID:        record.ID.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to update record: %w", err)
	}

	// 更新された行数を確認
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// レコードが見つからない場合
	if rowsAffected == 0 {
		return errors.New("record not found")
	}

	// 既存のタグを削除
	err = queriesWithTx.DeleteRecordTags(ctx, record.ID.String())
	if err != nil {
		return fmt.Errorf("failed to delete existing tags: %w", err)
	}

	// 新しいタグを個別に挿入
	for _, tag := range record.Tags {
		err = queriesWithTx.CreateRecordTag(ctx, db.CreateRecordTagParams{
			RecordID: record.ID.String(),
			Tag:      tag,
		})
		if err != nil {
			return fmt.Errorf("failed to create tag %s: %w", tag, err)
		}
	}

	// トランザクションのコミット
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	tx = nil // コミットが成功したのでnilにして遅延関数でのロールバックを防ぐ

	return nil
}

// GetRecord は指定されたIDのレコードを取得します。
func (s *SQLiteStore) GetRecord(ctx context.Context, id uuid.UUID) (*model.Record, error) {
	// sqlcで生成されたクエリを使用
	dbRecord, err := s.queries.GetRecord(ctx, id.String())
	if err == sql.ErrNoRows {
		return nil, errors.New("record not found")
	}
	if err != nil {
		return nil, err
	}

	// 文字列から時間に変換
	timestamp, err := time.Parse(time.RFC3339, dbRecord.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse record date: %w", err)
	}

	// UUIDの解析
	recordID, err := uuid.Parse(dbRecord.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid UUID in database: %w", err)
	}

	// タグを取得
	tags, err := s.queries.GetRecordTags(ctx, dbRecord.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get record tags: %w", err)
	}

	// レコードの作成
	return model.LoadRecord(recordID, timestamp, dbRecord.Project, int(dbRecord.Value), tags)
}

// ListRecords は指定されたプロジェクトの、指定した期間内のレコードを取得します。
func (s *SQLiteStore) ListRecords(ctx context.Context, project string, from, to time.Time, sortOrder *model.SortOrder) ([]*model.Record, error) {
	// 日付の範囲を丸一日に設定（秒以下の精度を取り除く）
	// fromは日付の始まりに設定
	fromDate := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	fromStr := fromDate.Format(time.RFC3339)

	// toは日付の終わりに設定（次の日の0時の直前）
	toDate := time.Date(to.Year(), to.Month(), to.Day(), 23, 59, 59, 999999999, to.Location())
	toStr := toDate.Format(time.RFC3339)

	// sqlcで生成されたクエリを使用
	dbRecords, err := s.queries.ListRecords(ctx, db.ListRecordsParams{
		Timestamp:   fromStr,
		Timestamp_2: toStr,
		Project:     project,
	})
	if err != nil {
		return nil, err
	}

	// 結果の変換
	var records []*model.Record
	for _, dbRecord := range dbRecords {
		// 文字列から時間に変換
		timestamp, err := time.Parse(time.RFC3339, dbRecord.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse record date: %w", err)
		}

		// UUID の解析
		id, err := uuid.Parse(dbRecord.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID in database: %w", err)
		}

		// タグを GROUP_CONCAT の結果から分割
		var tags []string
		if tagsStr, ok := dbRecord.Tags.(string); ok && tagsStr != "" {
			tags = strings.Split(tagsStr, " ")
		}

		// レコードの作成
		record, err := model.LoadRecord(id, timestamp, dbRecord.Project, int(dbRecord.Value), tags)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	// 降順の場合は結果を逆順にする
	if sortOrder.IsDesc() {
		for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
			records[i], records[j] = records[j], records[i]
		}
	}

	return records, nil
}

// ListRecordsWithTags は指定されたプロジェクトの、指定した期間内の、指定されたタグを持つレコードを取得します。
func (s *SQLiteStore) ListRecordsWithTags(ctx context.Context, project string, from, to time.Time, tags []string, sortOrder *model.SortOrder) ([]*model.Record, error) {
	// タグが指定されていない場合は通常のListRecordsを呼び出す
	if len(tags) == 0 {
		return s.ListRecords(ctx, project, from, to, sortOrder)
	}

	// 日付の範囲を丸一日に設定（秒以下の精度を取り除く）
	// fromは日付の始まりに設定
	fromDate := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	fromStr := fromDate.Format(time.RFC3339)

	// toは日付の終わりに設定（次の日の0時の直前）
	toDate := time.Date(to.Year(), to.Month(), to.Day(), 23, 59, 59, 999999999, to.Location())
	toStr := toDate.Format(time.RFC3339)

	// sqlcで生成されたクエリを使用
	dbRecords, err := s.queries.ListRecordsWithTags(ctx, db.ListRecordsWithTagsParams{
		Timestamp:   fromStr,
		Timestamp_2: toStr,
		Project:     project,
		Tags:        tags,
		Column5:     int64(len(tags)), // HAVINGクエリ用：タグの数
	})
	if err != nil {
		return nil, err
	}

	// 結果の変換
	var records []*model.Record
	for _, dbRecord := range dbRecords {
		// 文字列から時間に変換
		timestamp, err := time.Parse(time.RFC3339, dbRecord.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse record date: %w", err)
		}

		// UUID の解析
		id, err := uuid.Parse(dbRecord.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID in database: %w", err)
		}

		// タグを GROUP_CONCAT の結果から分割
		var recordTags []string
		if tagsStr, ok := dbRecord.AllTags.(string); ok && tagsStr != "" {
			recordTags = strings.Split(tagsStr, " ")
		}

		// レコードの作成
		record, err := model.LoadRecord(id, timestamp, dbRecord.Project, int(dbRecord.Value), recordTags)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	// 降順の場合は結果を逆順にする
	if sortOrder.IsDesc() {
		for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
			records[i], records[j] = records[j], records[i]
		}
	}

	return records, nil
}

// Close はデータベース接続を閉じます。
func (s *SQLiteStore) Close() error {
	return s.conn.Close()
}

// DeleteRecord は指定されたIDのレコードを削除します。
func (s *SQLiteStore) DeleteRecord(ctx context.Context, id uuid.UUID) error {
	// sqlcで生成されたクエリを使用
	result, err := s.queries.DeleteRecord(ctx, id.String())
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

// DeleteProject は指定されたプロジェクトのすべてのレコードを削除します。
func (s *SQLiteStore) DeleteProject(ctx context.Context, projectName string) error {
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

	// レコードを削除
	err = queriesWithTx.DeleteProject(context.Background(), projectName)
	if err != nil {
		return fmt.Errorf("failed to delete project records: %w", err)
	}

	// プロジェクトエンティティを削除
	err = queriesWithTx.DeleteProjectEntity(context.Background(), projectName)
	if err != nil {
		return fmt.Errorf("failed to delete project entity: %w", err)
	}

	// トランザクションのコミット
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	tx = nil // コミットが成功したのでnilにして遅延関数でのロールバックを防ぐ

	return nil
}

// DeleteRecordsUntil は指定日時より前のレコードを削除します。
func (s *SQLiteStore) DeleteRecordsUntil(ctx context.Context, project string, until time.Time) (int, error) {
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
		result, err = queriesWithTx.DeleteRecordsUntil(ctx, untilStr)
	} else {
		// 特定プロジェクトのレコードを削除
		result, err = queriesWithTx.DeleteRecordsUntilByProject(ctx, db.DeleteRecordsUntilByProjectParams{
			Project:   project,
			Timestamp: untilStr,
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

// CreateProject は新しいプロジェクトをデータベースに保存します。
func (s *SQLiteStore) CreateProject(ctx context.Context, project *model.Project) error {
	// バリデーション
	if err := project.Validate(); err != nil {
		return err
	}

	// 日時をRFC3339形式に統一して保存
	createdAtStr := project.CreatedAt.Format(time.RFC3339)
	updatedAtStr := project.UpdatedAt.Format(time.RFC3339)

	// sqlcで生成されたクエリを使用
	err := s.queries.CreateProject(ctx, db.CreateProjectParams{
		Name:        project.Name,
		Description: project.Description,
		CreatedAt:   createdAtStr,
		UpdatedAt:   updatedAtStr,
	})
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}

	return nil
}

// GetProject は指定された名前のプロジェクトを取得します。
func (s *SQLiteStore) GetProject(ctx context.Context, name string) (*model.Project, error) {
	// sqlcで生成されたクエリを使用
	dbProject, err := s.queries.GetProject(ctx, name)
	if err == sql.ErrNoRows {
		return nil, errors.New("project not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	// 文字列から時間に変換
	createdAt, err := time.Parse(time.RFC3339, dbProject.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339, dbProject.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at: %w", err)
	}

	// プロジェクトの作成
	return model.LoadProject(dbProject.Name, dbProject.Description, createdAt, updatedAt)
}

// UpdateProject は指定されたプロジェクトを更新します。
func (s *SQLiteStore) UpdateProject(ctx context.Context, project *model.Project) error {
	// バリデーション
	if err := project.Validate(); err != nil {
		return err
	}

	// 日時をRFC3339形式に統一して保存
	updatedAtStr := project.UpdatedAt.Format(time.RFC3339)

	// sqlcで生成されたクエリを使用
	result, err := s.queries.UpdateProject(ctx, db.UpdateProjectParams{
		Description: project.Description,
		UpdatedAt:   updatedAtStr,
		Name:        project.Name,
	})
	if err != nil {
		return fmt.Errorf("failed to update project: %w", err)
	}

	// 更新された行数を確認
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// プロジェクトが見つからない場合
	if rowsAffected == 0 {
		return errors.New("project not found")
	}

	return nil
}

// DeleteProjectEntity はプロジェクトエンティティのみを削除します（レコードは削除しません）。
func (s *SQLiteStore) DeleteProjectEntity(ctx context.Context, name string) error {
	// sqlcで生成されたクエリを使用
	err := s.queries.DeleteProjectEntity(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to delete project entity: %w", err)
	}

	return nil
}

// ListProjects はすべてのプロジェクトを取得します。
func (s *SQLiteStore) ListProjects(ctx context.Context) ([]*model.Project, error) {
	// sqlcで生成されたクエリを使用
	dbProjects, err := s.queries.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}

	// 結果の変換
	var projects []*model.Project
	for _, dbProject := range dbProjects {
		// 文字列から時間に変換
		createdAt, err := time.Parse(time.RFC3339, dbProject.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at: %w", err)
		}

		updatedAt, err := time.Parse(time.RFC3339, dbProject.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse updated_at: %w", err)
		}

		// プロジェクトの作成
		project, err := model.LoadProject(dbProject.Name, dbProject.Description, createdAt, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to load project: %w", err)
		}
		projects = append(projects, project)
	}

	return projects, nil
}

// GetProjectTags は指定されたプロジェクトのタグ一覧を取得します。
func (s *SQLiteStore) GetProjectTags(ctx context.Context, projectName string) ([]string, error) {
	// プロジェクトの存在確認
	_, err := s.GetProject(ctx, projectName)
	if err != nil {
		return nil, err
	}

	// sqlcで生成されたクエリを使用
	tags, err := s.queries.GetProjectTags(ctx, projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to get project tags: %w", err)
	}

	return tags, nil
}
