package store

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stsysd/sougen/model"
)

// testMigration はテスト用のシンプルなマイグレーション関数です。
func testMigration(conn *sql.DB) error {
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

func setupTestStore(t *testing.T) (*SQLiteStore, func()) {
	// テスト用の一時ディレクトリを作成
	tempDir, err := os.MkdirTemp("", "sougen-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// テスト用のSQLiteストアを初期化
	store, err := NewSQLiteStore(tempDir, testMigration)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create test store: %v", err)
	}

	// クリーンアップ関数を返す
	cleanup := func() {
		store.Close()
		os.RemoveAll(tempDir)
	}

	return store, cleanup
}

func TestCreateAndGetRecord(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	project, err := model.NewProject("exercise", "Exercise project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// テストデータ
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local)
	record, err := model.NewRecord(timestamp, project.ID, 1, []string{"test"})
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// レコードを作成
	err = store.CreateRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// 作成したレコードを取得
	retrievedRecord, err := store.GetRecord(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("Failed to get record: %v", err)
	}

	// 取得したレコードが元のレコードと一致することを確認
	if !retrievedRecord.ID.Equals(record.ID) {
		t.Errorf("Expected ID %d, got %d", record.ID.ToInt64(), retrievedRecord.ID.ToInt64())
	}

	if !retrievedRecord.Timestamp.Equal(record.Timestamp) {
		t.Errorf("Expected Timestamp %v, got %v", record.Timestamp, retrievedRecord.Timestamp)
	}

	if !retrievedRecord.ProjectID.Equals(record.ProjectID) {
		t.Errorf("Expected ProjectID %d, got %d", record.ProjectID.ToInt64(), retrievedRecord.ProjectID.ToInt64())
	}

	if retrievedRecord.Value != record.Value {
		t.Errorf("Expected Value %d, got %d", record.Value, retrievedRecord.Value)
	}
}

func TestGetNonExistentRecord(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 存在しない ID でレコードを取得
	nonExistentID := model.NewHexID(99999)
	_, err := store.GetRecord(context.Background(), nonExistentID)
	if err == nil {
		t.Error("Expected error when getting non-existent record, got nil")
	}

	// エラーメッセージが期待どおりであることを確認
	if err != nil && err.Error() != "record not found" {
		t.Errorf("Expected 'record not found' error, got '%v'", err)
	}
}

func TestCreateInvalidRecord(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 無効なレコード（日時なし）
	invalidRecord := &model.Record{
		ProjectID: model.NewHexID(1),
		Value:     1,
	}

	// レコードの作成が失敗することを確認
	err := store.CreateRecord(context.Background(), invalidRecord)
	if err == nil {
		t.Error("Expected validation error when creating invalid record, got nil")
	}
}

func TestDeleteRecord(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	project, err := model.NewProject("exercise", "Exercise project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// テストデータの作成
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local)
	record, err := model.NewRecord(timestamp, project.ID, 1, []string{"test"})
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// レコードを保存
	err = store.CreateRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// レコードを削除
	err = store.DeleteRecord(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("Failed to delete record: %v", err)
	}

	// 削除したレコードが存在しないことを確認
	_, err = store.GetRecord(context.Background(), record.ID)
	if err == nil {
		t.Error("Expected error when getting deleted record, got nil")
	}

	if err != nil && err.Error() != "record not found" {
		t.Errorf("Expected 'record not found' error, got '%v'", err)
	}

	// 存在しないレコードの削除を試みる
	err = store.DeleteRecord(context.Background(), model.NewHexID(99999))
	if err == nil {
		t.Error("Expected error when deleting non-existent record, got nil")
	}

	if err != nil && err.Error() != "record not found" {
		t.Errorf("Expected 'record not found' error, got '%v'", err)
	}
}

func TestListRecords(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	readingProject, err := model.NewProject("reading", "Reading project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), readingProject)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	otherProject, err := model.NewProject("other-project", "Other project")
	if err != nil {
		t.Fatalf("Failed to create other project model: %v", err)
	}
	err = store.CreateProject(context.Background(), otherProject)
	if err != nil {
		t.Fatalf("Failed to create other project: %v", err)
	}

	// タイムゾーンを一致させるためにLocalタイムゾーンを使用
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	tomorrow := now.AddDate(0, 0, 1)
	lastWeek := now.AddDate(0, 0, -7)
	nextWeek := now.AddDate(0, 0, 7)

	// テスト用レコードを作成
	for i := range 5 {
		// 1日ずつずらしたレコードを作成
		timestamp := yesterday.AddDate(0, 0, i)
		record, err := model.NewRecord(timestamp, readingProject.ID, i+1, nil)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}

		err = store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// 異なるプロジェクトのレコードも作成（リストに含まれないことを確認）
	otherRecord, err := model.NewRecord(now, otherProject.ID, 10, nil)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}
	err = store.CreateRecord(context.Background(), otherRecord)
	if err != nil {
		t.Fatalf("Failed to store record: %v", err)
	}

	// テストケース
	tests := []struct {
		name        string
		from        time.Time
		to          time.Time
		expected    int // 期待されるレコード数
		description string
	}{
		{
			name:        "All records",
			from:        lastWeek,
			to:          nextWeek,
			expected:    5,
			description: "期間内の全レコードが取得できること",
		},
		{
			name:        "Partial records",
			from:        yesterday,
			to:          yesterday.AddDate(0, 0, 2),
			expected:    3,
			description: "期間を限定した場合に正しいレコード数が取得できること",
		},
		{
			name:        "Future only",
			from:        tomorrow,
			to:          nextWeek,
			expected:    3,
			description: "明日以降のレコードのみ取得できること",
		},
		{
			name:        "Past only",
			from:        lastWeek,
			to:          yesterday,
			expected:    1,
			description: "昨日までのレコードのみ取得できること",
		},
		{
			name:        "No records",
			from:        lastWeek.AddDate(0, 0, -10),
			to:          lastWeek.AddDate(0, 0, -5),
			expected:    0,
			description: "期間内にレコードがない場合は空配列が返ること",
		},
	}

	// テストの実行
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pagination, _ := model.NewPagination("100", "")
			result, err := store.ListRecords(context.Background(), &ListRecordsParams{
				ProjectID:  readingProject.ID,
				From:       tc.from,
				To:         tc.to,
				Pagination: pagination,
				Tags:       []string{},
			})
			if err != nil {
				t.Fatalf("Failed to list records: %v", err)
			}

			if len(result) != tc.expected {
				t.Errorf("%s: Expected %d records, got %d", tc.description, tc.expected, len(result))
			}

			// 取得したレコードが期間内にあることを確認
			for _, r := range result {
				if !r.ProjectID.Equals(readingProject.ID) {
					t.Errorf("Expected project ID %d, got %d", readingProject.ID.ToInt64(), r.ProjectID.ToInt64())
				}

				// 取得したレコードの日付を年月日のみで比較
				rYear, rMonth, rDay := r.Timestamp.Date()
				fromYear, fromMonth, fromDay := tc.from.Date()
				toYear, toMonth, toDay := tc.to.Date()

				rDate := time.Date(rYear, rMonth, rDay, 0, 0, 0, 0, r.Timestamp.Location())
				fromDate := time.Date(fromYear, fromMonth, fromDay, 0, 0, 0, 0, tc.from.Location())
				toDate := time.Date(toYear, toMonth, toDay, 0, 0, 0, 0, tc.to.Location())

				if rDate.Before(fromDate) || rDate.After(toDate) {
					t.Errorf("Record date %v is outside range %v to %v", rDate, fromDate, toDate)
				}
			}
		})
	}
}

func TestDeleteProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	project1, err := model.NewProject("project1", "Project 1")
	if err != nil {
		t.Fatalf("Failed to create project1 model: %v", err)
	}
	err = store.CreateProject(context.Background(), project1)
	if err != nil {
		t.Fatalf("Failed to create project1: %v", err)
	}

	project2, err := model.NewProject("project2", "Project 2")
	if err != nil {
		t.Fatalf("Failed to create project2 model: %v", err)
	}
	err = store.CreateProject(context.Background(), project2)
	if err != nil {
		t.Fatalf("Failed to create project2: %v", err)
	}

	now := time.Now()

	// プロジェクト1用のレコードを3つ作成
	for i := range 3 {
		record, err := model.NewRecord(now.AddDate(0, 0, i), project1.ID, i+1, nil)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
		if err := store.CreateRecord(context.Background(), record); err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// プロジェクト2用のレコードを2つ作成
	for i := range 2 {
		record, err := model.NewRecord(now.AddDate(0, 0, i), project2.ID, i+10, nil)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
		if err := store.CreateRecord(context.Background(), record); err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// プロジェクト1のレコード数を確認
	pagination, _ := model.NewPagination("100", "")
	project1Records, err := store.ListRecords(context.Background(), &ListRecordsParams{
		ProjectID:  project1.ID,
		From:       time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Pagination: pagination,
		Tags:       []string{},
	})
	if err != nil {
		t.Fatalf("Failed to list project1 records: %v", err)
	}
	if len(project1Records) != 3 {
		t.Errorf("Expected 3 records for project1, got %d", len(project1Records))
	}

	// プロジェクト1を削除
	err = store.DeleteProject(context.Background(), project1.ID)
	if err != nil {
		t.Fatalf("Failed to delete project: %v", err)
	}

	// プロジェクト1のレコードが存在しなくなっていることを確認
	project1RecordsAfter, err := store.ListRecords(context.Background(), &ListRecordsParams{
		ProjectID:  project1.ID,
		From:       time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Pagination: pagination,
		Tags:       []string{},
	})
	if err != nil {
		t.Fatalf("Failed to list project1 records after deletion: %v", err)
	}
	if len(project1RecordsAfter) != 0 {
		t.Errorf("Expected 0 records for project1 after deletion, got %d", len(project1RecordsAfter))
	}

	// プロジェクト1のエンティティが削除されていることを確認
	_, err = store.GetProject(context.Background(), project1.ID)
	if err == nil {
		t.Errorf("Expected error when getting deleted project, got nil")
	}

	// プロジェクト2のレコードが残っていることを確認
	project2Records, err := store.ListRecords(context.Background(), &ListRecordsParams{
		ProjectID:  project2.ID,
		From:       time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Pagination: pagination,
		Tags:       []string{},
	})
	if err != nil {
		t.Fatalf("Failed to list project2 records: %v", err)
	}
	if len(project2Records) != 2 {
		t.Errorf("Expected 2 records for project2, got %d", len(project2Records))
	}

	// プロジェクト2のエンティティが残っていることを確認
	_, err = store.GetProject(context.Background(), project2.ID)
	if err != nil {
		t.Errorf("Expected project2 to still exist, got error: %v", err)
	}

	// 存在しないプロジェクトを削除してもエラーにならないことを確認
	err = store.DeleteProject(context.Background(), model.NewHexID(99999))
	if err != nil {
		t.Errorf("Expected no error when deleting non-existent project, got: %v", err)
	}
}

// TestListRecordsWithTags はタグフィルタでのレコード取得のテスト
func TestListRecordsWithTags(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	project, err := model.NewProject("test-project", "Test project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)

	// 異なるタグを持つレコードを作成
	record1, _ := model.NewRecord(baseTime, project.ID, 1, []string{"work", "urgent"})
	record2, _ := model.NewRecord(baseTime.Add(1*time.Hour), project.ID, 2, []string{"personal", "hobby"})
	record3, _ := model.NewRecord(baseTime.Add(2*time.Hour), project.ID, 3, []string{"work", "meeting"})
	record4, _ := model.NewRecord(baseTime.Add(3*time.Hour), project.ID, 4, []string{"personal", "urgent"})

	// レコードを保存
	for _, record := range []*model.Record{record1, record2, record3, record4} {
		err := store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
	}

	// テストケース
	tests := []struct {
		name          string
		tags          []string
		expectedCount int
		expectedIDs   []int64
	}{
		{
			name:          "Filter by work tag",
			tags:          []string{"work"},
			expectedCount: 2,
			expectedIDs:   []int64{record1.ID.ToInt64(), record3.ID.ToInt64()},
		},
		{
			name:          "Filter by personal tag",
			tags:          []string{"personal"},
			expectedCount: 2,
			expectedIDs:   []int64{record2.ID.ToInt64(), record4.ID.ToInt64()},
		},
		{
			name:          "Filter by urgent tag",
			tags:          []string{"urgent"},
			expectedCount: 2,
			expectedIDs:   []int64{record1.ID.ToInt64(), record4.ID.ToInt64()},
		},
		{
			name:          "Filter by multiple tags (AND - both work and urgent)",
			tags:          []string{"work", "urgent"},
			expectedCount: 1,
			expectedIDs:   []int64{record1.ID.ToInt64()},
		},
		{
			name:          "Filter by multiple tags (AND - both personal and urgent)",
			tags:          []string{"personal", "urgent"},
			expectedCount: 1,
			expectedIDs:   []int64{record4.ID.ToInt64()},
		},
		{
			name:          "Filter by multiple tags (AND - work and meeting)",
			tags:          []string{"work", "meeting"},
			expectedCount: 1,
			expectedIDs:   []int64{record3.ID.ToInt64()},
		},
		{
			name:          "Filter by non-existent tag",
			tags:          []string{"nonexistent"},
			expectedCount: 0,
			expectedIDs:   []int64{},
		},
		{
			name:          "Filter by multiple tags where no record has all (AND)",
			tags:          []string{"urgent", "hobby"},
			expectedCount: 0,
			expectedIDs:   []int64{},
		},
		{
			name:          "Filter by empty tags",
			tags:          []string{},
			expectedCount: 4,
			expectedIDs:   []int64{record1.ID.ToInt64(), record2.ID.ToInt64(), record3.ID.ToInt64(), record4.ID.ToInt64()},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 日付範囲を設定（全レコードが含まれる範囲）
			fromTime := baseTime.Add(-1 * time.Hour)
			toTime := baseTime.Add(5 * time.Hour)

			// タグフィルタでレコードを取得
			pagination, _ := model.NewPagination("100", "")
			records, err := store.ListRecords(context.Background(), &ListRecordsParams{
				ProjectID:  project.ID,
				From:       fromTime,
				To:         toTime,
				Pagination: pagination,
				Tags:       tc.tags,
			})
			if err != nil {
				t.Fatalf("Failed to list records with tags: %v", err)
			}

			// 件数チェック
			if len(records) != tc.expectedCount {
				t.Errorf("Expected %d records, got %d", tc.expectedCount, len(records))
			}

			// IDが期待されるものと一致するかチェック
			actualIDs := make(map[int64]bool)
			for _, record := range records {
				actualIDs[record.ID.ToInt64()] = true
			}

			for _, expectedID := range tc.expectedIDs {
				if !actualIDs[expectedID] {
					t.Errorf("Expected record with ID %d not found in results", expectedID)
				}
			}

			// 取得されたレコードが期待される全てのタグを持っているかチェック (AND条件)
			for _, record := range records {
				if len(tc.tags) == 0 {
					continue // タグ指定がない場合はスキップ
				}
				// 全てのフィルタタグがレコードに含まれているか確認
				for _, filterTag := range tc.tags {
					if !slices.Contains(record.Tags, filterTag) {
						t.Errorf("Record %d does not have required tag '%s' from filter tags %v", record.ID.ToInt64(), filterTag, tc.tags)
					}
				}
			}
		})
	}
}

// TestListRecordsWithTagsEmptyResult は空の結果のテスト
func TestListRecordsWithTagsEmptyResult(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	project, err := model.NewProject("empty-project", "Empty project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)

	// タグなしのレコードを作成
	record, _ := model.NewRecord(baseTime, project.ID, 1, []string{})
	err = store.CreateRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// 存在しないタグでフィルタ
	fromTime := baseTime.Add(-1 * time.Hour)
	toTime := baseTime.Add(1 * time.Hour)
	pagination, _ := model.NewPagination("100", "")
	records, err := store.ListRecords(context.Background(), &ListRecordsParams{
		ProjectID:  project.ID,
		From:       fromTime,
		To:         toTime,
		Pagination: pagination,
		Tags:       []string{"nonexistent"},
	})
	if err != nil {
		t.Fatalf("Failed to list records with tags: %v", err)
	}

	if len(records) != 0 {
		t.Errorf("Expected 0 records, got %d", len(records))
	}
}

// TestListRecordsDateRange は日付範囲フィルタのテスト
func TestListRecordsDateRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	project, err := model.NewProject("date-range-project", "Date range project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)

	// 異なる日時のレコードを作成
	record1, _ := model.NewRecord(baseTime, project.ID, 1, []string{"work"})
	record2, _ := model.NewRecord(baseTime.Add(24*time.Hour), project.ID, 2, []string{"work"})
	record3, _ := model.NewRecord(baseTime.Add(48*time.Hour), project.ID, 3, []string{"work"})

	for _, record := range []*model.Record{record1, record2, record3} {
		err := store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
	}

	// 最初の2日分のみを取得
	fromTime := baseTime.Add(-1 * time.Hour)
	toTime := baseTime.Add(25 * time.Hour)
	pagination, _ := model.NewPagination("100", "")
	records, err := store.ListRecords(context.Background(), &ListRecordsParams{
		ProjectID:  project.ID,
		From:       fromTime,
		To:         toTime,
		Pagination: pagination,
		Tags:       []string{"work"},
	})
	if err != nil {
		t.Fatalf("Failed to list records with tags: %v", err)
	}

	// 2件取得されることを確認
	if len(records) != 2 {
		t.Errorf("Expected 2 records, got %d", len(records))
	}

	// 正しいレコードが取得されることを確認
	expectedIDs := map[int64]bool{record1.ID.ToInt64(): true, record2.ID.ToInt64(): true}
	for _, record := range records {
		if !expectedIDs[record.ID.ToInt64()] {
			t.Errorf("Unexpected record ID %d in results", record.ID.ToInt64())
		}
	}
}

// TestCreateProject はプロジェクト作成機能をテストします。
func TestCreateProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトの作成
	project, err := model.NewProject("test-project", "Test project description")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}

	// データベースに保存
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// プロジェクトを取得して確認
	retrievedProject, err := store.GetProject(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("Failed to get project: %v", err)
	}

	// 内容の確認
	// DBで自動生成されたIDは有効な値であるべき
	if !retrievedProject.ID.IsValid() {
		t.Errorf("Expected retrieved project ID to be valid (auto-generated)")
	}
	// CreateProjectは元のprojectオブジェクトのIDも更新する
	if !project.ID.IsValid() {
		t.Errorf("Expected original project ID to be updated with auto-generated ID")
	}
	if retrievedProject.Name != project.Name {
		t.Errorf("Expected name %s, got %s", project.Name, retrievedProject.Name)
	}
	if retrievedProject.Description != project.Description {
		t.Errorf("Expected description %s, got %s", project.Description, retrievedProject.Description)
	}
	if retrievedProject.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if retrievedProject.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

// TestGetNonExistentProject は存在しないプロジェクトの取得をテストします。
func TestGetNonExistentProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 存在しないプロジェクトを取得
	_, err := store.GetProject(context.Background(), model.NewHexID(99999))
	if err == nil {
		t.Error("Expected error when getting non-existent project, got nil")
	}
}

// TestCreateDuplicateProject は重複プロジェクト作成をテストします。
func TestCreateDuplicateProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 最初のプロジェクトを作成
	project1, err := model.NewProject("duplicate", "First project")
	if err != nil {
		t.Fatalf("Failed to create first project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project1)
	if err != nil {
		t.Fatalf("Failed to create first project: %v", err)
	}

	// 同じ名前のプロジェクトを作成（失敗するはず）
	project2, err := model.NewProject("duplicate", "Second project")
	if err != nil {
		t.Fatalf("Failed to create second project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project2)
	if err == nil {
		t.Error("Expected error when creating duplicate project, got nil")
	}
}

// TestUpdateProject はプロジェクト更新機能をテストします。
func TestUpdateProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを作成
	project, err := model.NewProject("update-test", "Original description")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// 説明を更新
	originalUpdatedAt := project.UpdatedAt

	// 秒単位で時間差を確保してより明確な時間差を作る
	time.Sleep(1 * time.Second)
	project.Description = "Updated description"
	project.UpdatedAt = time.Now()

	// 更新を保存
	err = store.UpdateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to update project: %v", err)
	}

	// 更新されたプロジェクトを再取得
	updatedProject, err := store.GetProject(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("Failed to get updated project: %v", err)
	}

	// 更新内容を確認
	if updatedProject.Description != "Updated description" {
		t.Errorf("Expected description 'Updated description', got %s", updatedProject.Description)
	}

	// 時間比較を秒単位で行う
	if !updatedProject.UpdatedAt.Truncate(time.Second).After(originalUpdatedAt.Truncate(time.Second)) {
		t.Errorf("UpdatedAt should be after original time. Original: %v, Updated: %v", originalUpdatedAt, updatedProject.UpdatedAt)
	}
}

// TestUpdateNonExistentProject は存在しないプロジェクトの更新をテストします。
func TestUpdateNonExistentProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 存在しないプロジェクトを更新
	project, err := model.NewProject("non-existent", "Description")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}

	err = store.UpdateProject(context.Background(), project)
	if err == nil {
		t.Error("Expected error when updating non-existent project, got nil")
	}
}

// TestDeleteProjectEntity はプロジェクトエンティティ削除機能をテストします。
func TestDeleteProjectEntity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを作成
	project, err := model.NewProject("delete-test", "Test project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// プロジェクトエンティティを削除
	err = store.DeleteProject(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("Failed to delete project entity: %v", err)
	}

	// プロジェクトが削除されたことを確認
	_, err = store.GetProject(context.Background(), project.ID)
	if err == nil {
		t.Error("Expected error when getting deleted project, got nil")
	}
}

// TestListProjects はプロジェクト一覧取得機能をテストします。
func TestListProjects(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 複数のプロジェクトを作成
	projects := []struct {
		name        string
		description string
	}{
		{"project-a", "Project A description"},
		{"project-b", "Project B description"},
		{"project-c", "Project C description"},
	}

	for _, p := range projects {
		project, err := model.NewProject(p.name, p.description)
		if err != nil {
			t.Fatalf("Failed to create project model for %s: %v", p.name, err)
		}
		err = store.CreateProject(context.Background(), project)
		if err != nil {
			t.Fatalf("Failed to create project %s: %v", p.name, err)
		}
		time.Sleep(1 * time.Millisecond) // UpdatedAtの順序を確実にするため
	}

	// プロジェクト一覧を取得
	pagination, _ := model.NewPagination("100", "")
	params := &ListProjectsParams{Pagination: pagination}
	retrievedProjects, err := store.ListProjects(context.Background(), params)
	if err != nil {
		t.Fatalf("Failed to list projects: %v", err)
	}

	// 期待されるプロジェクト数を確認
	if len(retrievedProjects) != len(projects) {
		t.Errorf("Expected %d projects, got %d", len(projects), len(retrievedProjects))
	}

	// プロジェクトがUpdatedAtの降順でソートされていることを確認
	for i := 1; i < len(retrievedProjects); i++ {
		if retrievedProjects[i-1].UpdatedAt.Before(retrievedProjects[i].UpdatedAt) {
			t.Error("Projects should be sorted by UpdatedAt in descending order")
		}
	}

	// 各プロジェクトが存在することを確認
	projectNames := make(map[string]bool)
	for _, p := range retrievedProjects {
		projectNames[p.Name] = true
	}
	for _, expected := range projects {
		if !projectNames[expected.name] {
			t.Errorf("Expected project %s not found in list", expected.name)
		}
	}
}

// TestListEmptyProjects は空のプロジェクト一覧取得をテストします。
func TestListEmptyProjects(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクト一覧を取得（空のはず）
	pagination, _ := model.NewPagination("100", "")
	params := &ListProjectsParams{Pagination: pagination}
	projects, err := store.ListProjects(context.Background(), params)
	if err != nil {
		t.Fatalf("Failed to list projects: %v", err)
	}

	if len(projects) != 0 {
		t.Errorf("Expected 0 projects, got %d", len(projects))
	}
}

// TestRecordProjectReferentialIntegrity はレコード作成時のプロジェクト参照整合性をテストします。
func TestRecordProjectReferentialIntegrity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 存在しないプロジェクトIDでレコードを作成しようとする
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local)
	record, err := model.NewRecord(timestamp, model.NewHexID(99999), 1, []string{"test"})
	if err != nil {
		t.Fatalf("Failed to create record model: %v", err)
	}

	// レコード作成は外部キー制約により失敗するはず
	err = store.CreateRecord(context.Background(), record)
	if err == nil {
		t.Error("Expected error when creating record with non-existent project, got nil")
	}

	// プロジェクトを作成
	project, err := model.NewProject("existing-project", "Test project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// 今度は正しいプロジェクトIDでレコード作成が成功するはず
	record2, err := model.NewRecord(timestamp, project.ID, 1, []string{"test"})
	if err != nil {
		t.Fatalf("Failed to create second record model: %v", err)
	}

	err = store.CreateRecord(context.Background(), record2)
	if err != nil {
		t.Fatalf("Failed to create record with existing project: %v", err)
	}
}

// TestProjectDeletionWithOrphanedRecords はプロジェクト削除後の孤立レコードをテストします。
func TestProjectDeletionWithOrphanedRecords(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを作成
	project, err := model.NewProject("test-project", "Test project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// プロジェクトに関連するレコードを作成
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local)
	record, err := model.NewRecord(timestamp, project.ID, 1, []string{"test"})
	if err != nil {
		t.Fatalf("Failed to create record model: %v", err)
	}
	err = store.CreateRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// プロジェクトエンティティを削除
	err = store.DeleteProject(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("Failed to delete project entity: %v", err)
	}

	// ON DELETE CASCADEにより、プロジェクト削除時にレコードも自動削除される
	pagination, _ := model.NewPagination("100", "")
	records, err := store.ListRecords(context.Background(), &ListRecordsParams{
		ProjectID:  project.ID,
		From:       timestamp.Add(-1 * time.Hour),
		To:         timestamp.Add(1 * time.Hour),
		Pagination: pagination,
		Tags:       []string{},
	})
	if err != nil {
		t.Fatalf("Failed to list records: %v", err)
	}

	// ON DELETE CASCADEでレコードも削除されているはず
	if len(records) != 0 {
		t.Errorf("Expected 0 records after project deletion (CASCADE), got %d", len(records))
	}

	// レコードを直接取得してもnot foundエラーになるはず
	_, err = store.GetRecord(context.Background(), record.ID)
	if err == nil {
		t.Error("Expected error (not found) for deleted record, got nil")
	}
}

// TestUpdateRecordWithInvalidProject は存在しないプロジェクトへのレコード更新をテストします。
func TestUpdateRecordWithInvalidProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 2つのプロジェクトを作成
	project1, err := model.NewProject("project1", "Project 1")
	if err != nil {
		t.Fatalf("Failed to create project1 model: %v", err)
	}
	err = store.CreateProject(context.Background(), project1)
	if err != nil {
		t.Fatalf("Failed to create project1: %v", err)
	}

	project2, err := model.NewProject("project2", "Project 2")
	if err != nil {
		t.Fatalf("Failed to create project2 model: %v", err)
	}
	err = store.CreateProject(context.Background(), project2)
	if err != nil {
		t.Fatalf("Failed to create project2: %v", err)
	}

	// project1にレコードを作成
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local)
	record, err := model.NewRecord(timestamp, project1.ID, 1, []string{"test"})
	if err != nil {
		t.Fatalf("Failed to create record model: %v", err)
	}
	err = store.CreateRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// project2を削除
	err = store.DeleteProject(context.Background(), project2.ID)
	if err != nil {
		t.Fatalf("Failed to delete project2: %v", err)
	}

	// レコードを存在しないproject2に更新しようとする（失敗するはず）
	record.ProjectID = project2.ID
	err = store.UpdateRecord(context.Background(), record)
	if err == nil {
		t.Error("Expected error when updating record to non-existent project, got nil")
	}
	// Foreign key constraint error expected
	if !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Errorf("Expected foreign key constraint error, got: %v", err)
	}
}

// TestGetProjectTags はプロジェクトのタグ一覧取得機能をテストします。
func TestGetProjectTags(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを作成
	project, err := model.NewProject("tag-test", "Tag test project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// 異なるタグを持つレコードを作成
	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	record1, _ := model.NewRecord(baseTime, project.ID, 1, []string{"work", "important"})
	record2, _ := model.NewRecord(baseTime.Add(1*time.Hour), project.ID, 2, []string{"personal", "urgent"})
	record3, _ := model.NewRecord(baseTime.Add(2*time.Hour), project.ID, 3, []string{"work", "meeting"})
	record4, _ := model.NewRecord(baseTime.Add(3*time.Hour), project.ID, 4, []string{}) // タグなし

	// レコードを保存
	for _, record := range []*model.Record{record1, record2, record3, record4} {
		err := store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
	}

	// プロジェクトのタグ一覧を取得
	tags, err := store.GetProjectTags(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("Failed to get project tags: %v", err)
	}

	// 期待されるタグが含まれているかチェック
	expectedTags := []string{"work", "important", "personal", "urgent", "meeting"}

	if len(tags) != len(expectedTags) {
		t.Errorf("Expected %d tags, got %d", len(expectedTags), len(tags))
	}

	// タグが期待されるものと一致するかチェック
	for _, expectedTag := range expectedTags {
		if !slices.Contains(tags, expectedTag) {
			t.Errorf("Expected tag '%s' not found in response", expectedTag)
		}
	}
}

// TestGetProjectTagsNonExistentProject は存在しないプロジェクトのタグ取得をテストします。
func TestGetProjectTagsNonExistentProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 存在しないプロジェクトのタグを取得
	tags, err := store.GetProjectTags(context.Background(), model.NewHexID(99999))
	if err != nil {
		t.Errorf("Expected no error when getting tags for non-existent project, got: %v", err)
	}
  if len(tags) != 0 {
    t.Errorf("Expected 0 tags for non-existent project, got %d", len(tags))
  }
}

// TestGetProjectTagsEmptyProject はタグを持たないプロジェクトのタグ取得をテストします。
func TestGetProjectTagsEmptyProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを作成（レコードなし）
	project, err := model.NewProject("empty-tags", "Empty tags project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// タグを持たないレコードを作成
	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	record, _ := model.NewRecord(baseTime, project.ID, 1, []string{})
	err = store.CreateRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// プロジェクトのタグ一覧を取得（空配列が返されるはず）
	tags, err := store.GetProjectTags(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("Failed to get project tags: %v", err)
	}

	if len(tags) != 0 {
		t.Errorf("Expected 0 tags for empty project, got %d", len(tags))
	}
}

// TestGetProjectTagsWithMultipleRecords は複数レコードからのタグ重複排除をテストします。
func TestGetProjectTagsWithMultipleRecords(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを作成
	project, err := model.NewProject("duplicate-tags", "Duplicate tags project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// 重複するタグを持つレコードを作成
	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	record1, _ := model.NewRecord(baseTime, project.ID, 1, []string{"work", "important"})
	record2, _ := model.NewRecord(baseTime.Add(1*time.Hour), project.ID, 2, []string{"work", "urgent"})       // workが重複
	record3, _ := model.NewRecord(baseTime.Add(2*time.Hour), project.ID, 3, []string{"important", "meeting"}) // importantが重複

	// レコードを保存
	for _, record := range []*model.Record{record1, record2, record3} {
		err := store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
	}

	// プロジェクトのタグ一覧を取得
	tags, err := store.GetProjectTags(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("Failed to get project tags: %v", err)
	}

	// 重複が排除されてユニークなタグのみが返されることを確認
	expectedTags := []string{"work", "important", "urgent", "meeting"}

	if len(tags) != len(expectedTags) {
		t.Errorf("Expected %d unique tags, got %d", len(expectedTags), len(tags))
	}

	// タグが期待されるものと一致するかチェック
	for _, expectedTag := range expectedTags {
		if !slices.Contains(tags, expectedTag) {
			t.Errorf("Expected tag '%s' not found in response", expectedTag)
		}
	}

	// 重複がないことを確認
	tagSet := make(map[string]bool)
	for _, tag := range tags {
		tagSet[tag] = true
	}
	if len(tags) != len(tagSet) {
		t.Error("Duplicate tags found in response")
	}
}

// TestListRecordsWithCursorPagination tests cursor-based pagination for records
func TestListRecordsWithCursorPagination(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	project, err := model.NewProject("exercise", "Exercise tracking")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// テスト用に10件のレコードを作成
	baseTime := time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 10; i++ {
		recordTime := baseTime.Add(time.Duration(i) * time.Hour)
		record, err := model.NewRecord(recordTime, project.ID, i+1, nil)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
		err = store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// データベースから全レコードを取得して期待値の配列を作成（timestamp DESC順）
	allRecordsParams := &ListRecordsParams{
		ProjectID:  project.ID,
		From:       baseTime.AddDate(0, 0, -1),
		To:         baseTime.AddDate(0, 0, 1),
		Pagination: model.NewPaginationWithValues(100, nil),
	}
	allRecords, err := store.ListRecords(context.Background(), allRecordsParams)
	if err != nil {
		t.Fatalf("Failed to get all records: %v", err)
	}
	if len(allRecords) != 10 {
		t.Fatalf("Expected 10 records, got %d", len(allRecords))
	}

	// ケース1: 最初のページを取得（limit=3, cursorなし）
	t.Run("First page without cursor", func(t *testing.T) {
		params := &ListRecordsParams{
			ProjectID:  project.ID,
			From:       baseTime.AddDate(0, 0, -1),
			To:         baseTime.AddDate(0, 0, 1),
			Pagination: model.NewPaginationWithValues(3, nil),
		}

		records, err := store.ListRecords(context.Background(), params)
		if err != nil {
			t.Fatalf("Failed to list records: %v", err)
		}

		if len(records) != 3 {
			t.Errorf("Expected 3 records, got %d", len(records))
		}

		// 最初の3件が取得されているか確認（降順なので最新の3件）
		for i := 0; i < 3; i++ {
			if !records[i].ID.Equals(allRecords[i].ID) {
				t.Errorf("Record at index %d has incorrect ID. Expected %d, got %d",
					i, allRecords[i].ID.ToInt64(), records[i].ID.ToInt64())
			}
		}
	})

	// ケース2: カーソルを使って2ページ目を取得（limit=3）
	// これがバグを検知するための重要なテスト
	t.Run("Second page with cursor", func(t *testing.T) {
		// 3番目のレコード（allRecords[2]）の後から取得
		cursorRecord := allRecords[2]
		cursorTimestamp := cursorRecord.Timestamp
		cursorID := cursorRecord.ID

		params := &ListRecordsParams{
			ProjectID:       project.ID,
			From:            baseTime.AddDate(0, 0, -1),
			To:              baseTime.AddDate(0, 0, 1),
			Pagination:      model.NewPaginationWithValues(3, nil),
			CursorTimestamp: &cursorTimestamp,
			CursorID:        &cursorID,
		}

		records, err := store.ListRecords(context.Background(), params)
		if err != nil {
			t.Fatalf("Failed to list records with cursor: %v", err)
		}

		if len(records) != 3 {
			t.Errorf("Expected 3 records on second page, got %d", len(records))
		}

		// 重要: 2ページ目は1ページ目と異なるレコードであるべき
		// カーソルの次のレコード（allRecords[3], [4], [5]）が返されるべき
		for i := 0; i < 3; i++ {
			expectedIndex := i + 3
			if !records[i].ID.Equals(allRecords[expectedIndex].ID) {
				t.Errorf("Record at index %d on second page has incorrect ID. Expected %d (from allRecords[%d]), got %d",
					i, allRecords[expectedIndex].ID.ToInt64(), expectedIndex, records[i].ID.ToInt64())
			}
		}

		// バグがある場合: 1ページ目と同じレコードが返される
		// このチェックでバグを明示的に検出
		if len(records) > 0 && records[0].ID.Equals(allRecords[0].ID) {
			t.Error("BUG DETECTED: Second page returned same records as first page. Cursor is not working!")
		}
	})

	// ケース3: 最後のレコードをカーソルにした場合、空配列が返される
	t.Run("Last record as cursor", func(t *testing.T) {
		lastRecord := allRecords[len(allRecords)-1]
		cursorTimestamp := lastRecord.Timestamp
		cursorID := lastRecord.ID

		params := &ListRecordsParams{
			ProjectID:       project.ID,
			From:            baseTime.AddDate(0, 0, -1),
			To:              baseTime.AddDate(0, 0, 1),
			Pagination:      model.NewPaginationWithValues(5, nil),
			CursorTimestamp: &cursorTimestamp,
			CursorID:        &cursorID,
		}

		records, err := store.ListRecords(context.Background(), params)
		if err != nil {
			t.Fatalf("Failed to list records with cursor: %v", err)
		}

		if len(records) != 0 {
			t.Errorf("Expected 0 records after last record, got %d", len(records))
		}
	})
}

// TestListProjectsWithCursorPagination tests cursor-based pagination for projects
func TestListProjectsWithCursorPagination(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// テスト用に10件のプロジェクトを作成
	for i := 0; i < 10; i++ {
		projectName := strings.Repeat("a", i+1) // a, aa, aaa, ... (アルファベット順)
		project, err := model.NewProject(projectName, "Test project "+projectName)
		if err != nil {
			t.Fatalf("Failed to create project model: %v", err)
		}
		err = store.CreateProject(context.Background(), project)
		if err != nil {
			t.Fatalf("Failed to create project: %v", err)
		}

		// 少し時間をずらして updated_at を異なる値にする
		time.Sleep(2 * time.Millisecond)
	}

	// データベースから全プロジェクトを取得して期待値の配列を作成（updated_at DESC, name順）
	allProjectsParams := &ListProjectsParams{
		Pagination: model.NewPaginationWithValues(100, nil),
	}
	allProjects, err := store.ListProjects(context.Background(), allProjectsParams)
	if err != nil {
		t.Fatalf("Failed to get all projects: %v", err)
	}
	if len(allProjects) != 10 {
		t.Fatalf("Expected 10 projects, got %d", len(allProjects))
	}

	// ケース1: 最初のページを取得（limit=3, cursorなし）
	t.Run("First page without cursor", func(t *testing.T) {
		params := &ListProjectsParams{
			Pagination: model.NewPaginationWithValues(3, nil),
		}

		projects, err := store.ListProjects(context.Background(), params)
		if err != nil {
			t.Fatalf("Failed to list projects: %v", err)
		}

		if len(projects) != 3 {
			t.Errorf("Expected 3 projects, got %d", len(projects))
		}

		// 最初の3件が取得されているか確認
		for i := 0; i < 3; i++ {
			if projects[i].Name != allProjects[i].Name {
				t.Errorf("Project at index %d has incorrect name. Expected %s, got %s",
					i, allProjects[i].Name, projects[i].Name)
			}
		}
	})

	// ケース2: カーソルを使って2ページ目を取得（limit=3）
	t.Run("Second page with cursor", func(t *testing.T) {
		// 3番目のプロジェクト（allProjects[2]）の後から取得
		cursorProject := allProjects[2]
		cursorUpdatedAt := cursorProject.UpdatedAt
		cursorName := cursorProject.Name

		params := &ListProjectsParams{
			Pagination:      model.NewPaginationWithValues(3, nil),
			CursorUpdatedAt: &cursorUpdatedAt,
			CursorName:      &cursorName,
		}

		projects, err := store.ListProjects(context.Background(), params)
		if err != nil {
			t.Fatalf("Failed to list projects with cursor: %v", err)
		}

		if len(projects) != 3 {
			t.Errorf("Expected 3 projects on second page, got %d", len(projects))
		}

		// 2ページ目は1ページ目と異なるプロジェクトであるべき
		for i := 0; i < 3; i++ {
			expectedIndex := i + 3
			if !projects[i].ID.Equals(allProjects[expectedIndex].ID) {
				t.Errorf("Project at index %d on second page has incorrect ID. Expected %d (from allProjects[%d]), got %d",
					i, allProjects[expectedIndex].ID.ToInt64(), expectedIndex, projects[i].ID.ToInt64())
			}
		}

		// バグがある場合: 1ページ目と同じプロジェクトが返される
		if len(projects) > 0 && projects[0].ID.Equals(allProjects[0].ID) {
			t.Error("BUG DETECTED: Second page returned same projects as first page. Cursor is not working!")
		}
	})
}

// TestListAllRecordsWithPagination tests that ListAllRecords correctly handles
// pagination across multiple pages using the new cursor format
func TestListAllRecordsWithPagination(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	project, err := model.NewProject("large-project", "Large project for pagination test")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// テスト用に2500件のレコードを作成（ListAllRecordsのpageSize=1000を超える）
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	expectedCount := 2500

	for i := 0; i < expectedCount; i++ {
		recordTime := baseTime.Add(time.Duration(i) * time.Minute)
		record, err := model.NewRecord(recordTime, project.ID, i+1, nil)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
		err = store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// ListAllRecordsを使って全レコードを取得
	params := &ListAllRecordsParams{
		ProjectID: project.ID,
		From:      baseTime.AddDate(0, 0, -1),
		To:        baseTime.AddDate(0, 0, 2),
		Tags:      nil,
	}

	var retrievedRecords []*model.Record
	var seenIDs = make(map[int64]bool)

	for record, err := range store.ListAllRecords(context.Background(), params) {
		if err != nil {
			t.Fatalf("Error during iteration: %v", err)
		}

		// 重複チェック（カーソルが機能していない場合、同じレコードが繰り返される）
		recordID := record.ID.ToInt64()
		if seenIDs[recordID] {
			t.Errorf("DUPLICATE DETECTED: Record ID %d appeared more than once. Cursor pagination is broken!", recordID)
		}
		seenIDs[recordID] = true

		retrievedRecords = append(retrievedRecords, record)

		// 安全のため、無限ループを防ぐ
		if len(retrievedRecords) > expectedCount+100 {
			t.Fatalf("Retrieved too many records (%d). Possible infinite loop due to broken cursor!", len(retrievedRecords))
		}
	}

	// 期待されるレコード数が取得できたか確認
	if len(retrievedRecords) != expectedCount {
		t.Errorf("Expected %d records, got %d", expectedCount, len(retrievedRecords))
	}

	// 重複がないことを再確認
	if len(seenIDs) != expectedCount {
		t.Errorf("Expected %d unique record IDs, got %d. Duplicates detected!", expectedCount, len(seenIDs))
	}

	t.Logf("Successfully retrieved %d unique records across multiple pages", len(retrievedRecords))
}
