// Package store は、データの永続化機能を提供します。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stsysd/sougen/db"
	"github.com/stsysd/sougen/model"
)

// ListProjectsParams はプロジェクト一覧取得のパラメータです。
type ListProjectsParams struct {
	Pagination      *model.Pagination
	CursorUpdatedAt *time.Time // Cursor position: updated_at (nil if no cursor)
	CursorName      *string    // Cursor position: name (nil if no cursor)
}

// ListRecordsParams はレコード一覧取得のパラメータです。
type ListRecordsParams struct {
	ProjectID       int64
	From            time.Time
	To              time.Time
	Pagination      *model.Pagination
	Tags            []string
	CursorTimestamp *time.Time // Cursor position: timestamp (nil if no cursor)
	CursorID        *int64     // Cursor position: ID (nil if no cursor)
}

// ListAllRecordsParams は全レコード取得のパラメータです（ページネーションなし）。
type ListAllRecordsParams struct {
	ProjectID int64
	From      time.Time
	To        time.Time
	Tags      []string
}

// Store はレコードとプロジェクトの永続化を行うインターフェースです。
type Store interface {
	// Record operations
	// CreateRecord は新しいレコードを作成します。
	CreateRecord(ctx context.Context, record *model.Record) error
	// GetRecord は指定されたIDのレコードを取得します。
	GetRecord(ctx context.Context, id int64) (*model.Record, error)
	// UpdateRecord は指定されたIDのレコードを更新します。
	UpdateRecord(ctx context.Context, record *model.Record) error
	// DeleteRecord は指定されたIDのレコードを削除します。
	DeleteRecord(ctx context.Context, id int64) error
	// DeleteRecordsUntil は指定日時より前のレコードを削除します。
	DeleteRecordsUntil(ctx context.Context, projectID int64, until time.Time) (int, error)
	// ListRecords は指定されたパラメータに基づいてレコードを取得します。
	ListRecords(ctx context.Context, params *ListRecordsParams) ([]*model.Record, error)
	// ListAllRecords は指定されたパラメータに基づいて全てのレコードをイテレータで返します（ページネーションなし）。
	// イテレータはレコードとエラーのペアを返します。エラーが発生した場合、エラーが返され処理が終了します。
	ListAllRecords(ctx context.Context, params *ListAllRecordsParams) iter.Seq2[*model.Record, error]

	// Project operations
	// CreateProject は新しいプロジェクトを作成します。
	CreateProject(ctx context.Context, project *model.Project) error
	// GetProject は指定されたIDのプロジェクトを取得します。
	GetProject(ctx context.Context, id int64) (*model.Project, error)
	// UpdateProject は指定されたプロジェクトを更新します。
	UpdateProject(ctx context.Context, project *model.Project) error
	// DeleteProject は指定されたプロジェクトIDのすべてのレコードとプロジェクトを削除します。
	DeleteProject(ctx context.Context, projectID int64) error
	// ListProjects は指定されたパラメータに基づいてプロジェクトを取得します。
	ListProjects(ctx context.Context, params *ListProjectsParams) ([]*model.Project, error)
	// GetProjectTags は指定されたプロジェクトIDのタグ一覧を取得します。
	GetProjectTags(ctx context.Context, projectID int64) ([]string, error)

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
// Note: スキーマ定義はgooseマイグレーションに移行しました。
// この関数は既存のDBとの互換性のために残していますが、新規DBの場合はgooseを使用してください。
func initTables(conn *sql.DB) error {
	// 外部キー制約を有効化
	_, err := conn.Exec(`PRAGMA foreign_keys = ON;`)
	if err != nil {
		return err
	}

	// テーブルの作成
	_, err = conn.Exec(`
		-- Projects table
		CREATE TABLE IF NOT EXISTS projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		-- Records table
		CREATE TABLE IF NOT EXISTS records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			value INTEGER NOT NULL,
			timestamp TEXT NOT NULL,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		);

		-- Tags table
		CREATE TABLE IF NOT EXISTS tags (
			record_id INTEGER NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY (record_id, tag),
			FOREIGN KEY (record_id) REFERENCES records(id) ON DELETE CASCADE
		);

		-- Indexes
		CREATE INDEX IF NOT EXISTS idx_records_project_id_timestamp
		ON records(project_id, timestamp);

		CREATE INDEX IF NOT EXISTS idx_tags_record_id ON tags(record_id);
		CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag);
		CREATE INDEX IF NOT EXISTS idx_projects_updated_at ON projects(updated_at);
		CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);
	`)
	return err
}

// CreateRecord は新しいレコードをデータベースに保存します。
func (s *SQLiteStore) CreateRecord(ctx context.Context, record *model.Record) error {
	// バリデーション
	if err := record.Validate(); err != nil {
		return err
	}

	// 日時をRFC3339形式に統一して保存
	formattedTime := record.Timestamp.Format(time.RFC3339)

	// sqlcで生成されたクエリを使用（IDは自動生成）
	ret, err := s.queries.CreateRecord(ctx, db.CreateRecordParams{
		ProjectID: record.ProjectID,
		Value:     int64(record.Value),
		Timestamp: formattedTime,
	})
	if err != nil {
		return err
	}

	id, err := ret.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}
	record.ID = id

	// タグを個別に挿入
	for _, tag := range record.Tags {
		err = s.queries.CreateRecordTag(ctx, db.CreateRecordTagParams{
			RecordID: id,
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
		ProjectID: record.ProjectID,
		Value:     int64(record.Value),
		Timestamp: formattedTime,
		ID:        record.ID,
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
	err = queriesWithTx.DeleteRecordTags(ctx, record.ID)
	if err != nil {
		return fmt.Errorf("failed to delete existing tags: %w", err)
	}

	// 新しいタグを個別に挿入
	for _, tag := range record.Tags {
		err = queriesWithTx.CreateRecordTag(ctx, db.CreateRecordTagParams{
			RecordID: record.ID,
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
func (s *SQLiteStore) GetRecord(ctx context.Context, id int64) (*model.Record, error) {
	// sqlcで生成されたクエリを使用
	dbRecord, err := s.queries.GetRecord(ctx, id)
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

	// タグを取得
	tags, err := s.queries.GetRecordTags(ctx, dbRecord.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get record tags: %w", err)
	}

	// レコードの作成
	return model.LoadRecord(dbRecord.ID, timestamp, dbRecord.ProjectID, int(dbRecord.Value), tags)
}

// ListRecords は指定されたプロジェクトの、指定した期間内のレコードを取得します。
func (s *SQLiteStore) ListRecords(ctx context.Context, params *ListRecordsParams) ([]*model.Record, error) {
	// 日付の範囲を丸一日に設定（秒以下の精度を取り除く）
	fromDate := time.Date(params.From.Year(), params.From.Month(), params.From.Day(), 0, 0, 0, 0, params.From.Location())
	fromStr := fromDate.Format(time.RFC3339)

	toDate := time.Date(params.To.Year(), params.To.Month(), params.To.Day(), 23, 59, 59, 999999999, params.To.Location())
	toStr := toDate.Format(time.RFC3339)

	limit := int64(params.Pagination.Limit())

	// カーソルベースのページネーションパラメータ
	var cursorID int64
	var cursorTimestamp string
	var cursorColumn any
	if params.CursorTimestamp != nil && params.CursorID != nil {
		// カーソルが指定されている場合、パラメータから直接取得
		cursorID = *params.CursorID
		cursorTimestamp = params.CursorTimestamp.Format(time.RFC3339)
		cursorColumn = 1 // 非NULL値を設定してSQLの "? IS NULL" をFALSEにする
	} else {
		// カーソルが指定されていない場合は NULL
		cursorColumn = nil
		cursorTimestamp = ""
		cursorID = 0
	}

	var records []*model.Record

	if len(params.Tags) == 0 {
		// タグフィルタなし
		dbRecords, err := s.queries.ListRecords(ctx, db.ListRecordsParams{
			Timestamp:   fromStr,
			Timestamp_2: toStr,
			ProjectID:   params.ProjectID,
			Column4:     cursorColumn,
			Timestamp_3: cursorTimestamp,
			Timestamp_4: cursorTimestamp,
			ID:          cursorID,
			Limit:       limit,
		})
		if err != nil {
			return nil, err
		}

		for _, dbRecord := range dbRecords {
			timestamp, err := time.Parse(time.RFC3339, dbRecord.Timestamp)
			if err != nil {
				return nil, fmt.Errorf("failed to parse record date: %w", err)
			}

			var tags []string
			if tagsStr, ok := dbRecord.Tags.(string); ok && tagsStr != "" {
				tags = strings.Split(tagsStr, " ")
			}

			record, err := model.LoadRecord(dbRecord.ID, timestamp, dbRecord.ProjectID, int(dbRecord.Value), tags)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
	} else {
		// タグフィルタあり
		dbRecords, err := s.queries.ListRecordsWithTags(ctx, db.ListRecordsWithTagsParams{
			Timestamp:   fromStr,
			Timestamp_2: toStr,
			ProjectID:   params.ProjectID,
			Tags:        params.Tags,
			Column5:     cursorColumn,
			Timestamp_3: cursorTimestamp,
			Timestamp_4: cursorTimestamp,
			ID:          cursorID,
			Column9:     int64(len(params.Tags)),
			Limit:       limit,
		})
		if err != nil {
			return nil, err
		}

		for _, dbRecord := range dbRecords {
			timestamp, err := time.Parse(time.RFC3339, dbRecord.Timestamp)
			if err != nil {
				return nil, fmt.Errorf("failed to parse record date: %w", err)
			}

			var recordTags []string
			if tagsStr, ok := dbRecord.AllTags.(string); ok && tagsStr != "" {
				recordTags = strings.Split(tagsStr, " ")
			}

			record, err := model.LoadRecord(dbRecord.ID, timestamp, dbRecord.ProjectID, int(dbRecord.Value), recordTags)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
	}

	return records, nil
}

// ListAllRecords は指定されたパラメータに基づいて全てのレコードをイテレータで返します。
// ページネーションを使用して段階的にレコードを取得し、メモリ効率的に処理します。
func (s *SQLiteStore) ListAllRecords(ctx context.Context, params *ListAllRecordsParams) iter.Seq2[*model.Record, error] {
	return func(yield func(*model.Record, error) bool) {
		const pageSize = 1000
		var cursorTimestamp *time.Time
		var cursorID *int64

		for {
			pagination := model.NewPaginationWithValues(pageSize, nil)

			listParams := &ListRecordsParams{
				ProjectID:       params.ProjectID,
				From:            params.From,
				To:              params.To,
				Pagination:      pagination,
				Tags:            params.Tags,
				CursorTimestamp: cursorTimestamp,
				CursorID:        cursorID,
			}

			records, err := s.ListRecords(ctx, listParams)
			if err != nil {
				// エラーが発生した場合、エラーをyieldして終了
				yield(nil, err)
				return
			}

			// 各レコードをyield
			for _, record := range records {
				if !yield(record, nil) {
					// yieldがfalseを返したら早期終了
					return
				}
			}

			// 取得したレコード数がページサイズより少ない場合、これ以上レコードがない
			if len(records) < pageSize {
				break
			}

			// 次のページのためのカーソルを設定（新しいkeyset pagination形式）
			lastRecord := records[len(records)-1]
			cursorTimestamp = &lastRecord.Timestamp
			cursorID = &lastRecord.ID
		}
	}
}

// Close はデータベース接続を閉じます。
func (s *SQLiteStore) Close() error {
	return s.conn.Close()
}

// DeleteRecord は指定されたIDのレコードを削除します。
func (s *SQLiteStore) DeleteRecord(ctx context.Context, id int64) error {
	// sqlcで生成されたクエリを使用
	result, err := s.queries.DeleteRecord(ctx, id)
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

// DeleteProject は指定されたプロジェクトを削除します。
func (s *SQLiteStore) DeleteProject(ctx context.Context, projectID int64) error {
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

	// プロジェクトを削除（ON DELETE CASCADEにより関連レコードも自動削除される）
	err = queriesWithTx.DeleteProject(ctx, projectID)
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
func (s *SQLiteStore) DeleteRecordsUntil(ctx context.Context, projectID int64, until time.Time) (int, error) {
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
	if projectID == 0 {
		// 特定のプロジェクト指定がない場合は全プロジェクトから削除
		result, err = queriesWithTx.DeleteRecordsUntil(ctx, untilStr)
	} else {
		// 特定プロジェクトのレコードを削除
		result, err = queriesWithTx.DeleteRecordsUntilByProject(ctx, db.DeleteRecordsUntilByProjectParams{
			ProjectID: projectID,
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
	ret, err := s.queries.CreateProject(ctx, db.CreateProjectParams{
		Name:        project.Name,
		Description: project.Description,
		CreatedAt:   createdAtStr,
		UpdatedAt:   updatedAtStr,
	})
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}
	id, err := ret.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}

	project.ID = id
	return nil
}

// GetProject は指定されたIDのプロジェクトを取得します。
func (s *SQLiteStore) GetProject(ctx context.Context, id int64) (*model.Project, error) {
	// sqlcで生成されたクエリを使用
	dbProject, err := s.queries.GetProject(ctx, id)
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
	return model.LoadProject(dbProject.ID, dbProject.Name, dbProject.Description, createdAt, updatedAt)
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
		ID:          project.ID,
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

// ListProjects はすべてのプロジェクトを取得します。
func (s *SQLiteStore) ListProjects(ctx context.Context, params *ListProjectsParams) ([]*model.Project, error) {
	limit := int64(params.Pagination.Limit())

	// カーソルベースのページネーションパラメータ
	var cursorName string
	var cursorUpdatedAt string
	var cursorColumn any
	if params.CursorUpdatedAt != nil && params.CursorName != nil {
		// カーソルが指定されている場合、パラメータから直接取得
		cursorName = *params.CursorName
		cursorUpdatedAt = params.CursorUpdatedAt.Format(time.RFC3339)
		cursorColumn = 1 // 非NULL値を設定してSQLの "? IS NULL" をFALSEにする
	} else {
		// カーソルが指定されていない場合は NULL
		cursorColumn = nil
		cursorUpdatedAt = ""
		cursorName = ""
	}

	// sqlcで生成されたクエリを使用
	dbProjects, err := s.queries.ListProjects(ctx, db.ListProjectsParams{
		Column1:     cursorColumn,
		UpdatedAt:   cursorUpdatedAt,
		UpdatedAt_2: cursorUpdatedAt,
		Name:        cursorName,
		Limit:       limit,
	})
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
		project, err := model.LoadProject(dbProject.ID, dbProject.Name, dbProject.Description, createdAt, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to load project: %w", err)
		}
		projects = append(projects, project)
	}

	return projects, nil
}

// GetProjectTags は指定されたプロジェクトIDのタグ一覧を取得します。
func (s *SQLiteStore) GetProjectTags(ctx context.Context, projectID int64) ([]string, error) {
	// sqlcで生成されたクエリを使用
	tags, err := s.queries.GetProjectTags(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get project tags: %w", err)
	}

	return tags, nil
}
