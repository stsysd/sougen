package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stsysd/sougen/model"
)

func setupTestStore(t *testing.T) (*SQLiteStore, func()) {
	// テスト用の一時ディレクトリを作成
	tempDir, err := os.MkdirTemp("", "sougen-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// テスト用のSQLiteストアを初期化
	store, err := NewSQLiteStore(tempDir)
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
	record, err := model.NewRecord(timestamp, "exercise", 1, []string{"test"})
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
	if retrievedRecord.ID != record.ID {
		t.Errorf("Expected ID %s, got %s", record.ID, retrievedRecord.ID)
	}

	if !retrievedRecord.Timestamp.Equal(record.Timestamp) {
		t.Errorf("Expected Timestamp %v, got %v", record.Timestamp, retrievedRecord.Timestamp)
	}

	if retrievedRecord.Project != record.Project {
		t.Errorf("Expected Category %s, got %s", record.Project, retrievedRecord.Project)
	}

	if retrievedRecord.Value != record.Value {
		t.Errorf("Expected Value %d, got %d", record.Value, retrievedRecord.Value)
	}
}

func TestGetNonExistentRecord(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 存在しない妥当なUUIDでレコードを取得
	nonExistentID := uuid.New()
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
		Project: "exercise",
		Value:   1,
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
	record, err := model.NewRecord(timestamp, "exercise", 1, []string{"test"})
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
	err = store.DeleteRecord(context.Background(), uuid.New())
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
	projectModel, err := model.NewProject("reading", "Reading project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), projectModel)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	otherProjectModel, err := model.NewProject("other-project", "Other project")
	if err != nil {
		t.Fatalf("Failed to create other project model: %v", err)
	}
	err = store.CreateProject(context.Background(), otherProjectModel)
	if err != nil {
		t.Fatalf("Failed to create other project: %v", err)
	}

	// テスト用のプロジェクト名
	project := "reading"

	// タイムゾーンを一致させるためにLocalタイムゾーンを使用
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	tomorrow := now.AddDate(0, 0, 1)
	lastWeek := now.AddDate(0, 0, -7)
	nextWeek := now.AddDate(0, 0, 7)

	// テスト用レコードを作成
	for i := 0; i < 5; i++ {
		// 1日ずつずらしたレコードを作成
		timestamp := yesterday.AddDate(0, 0, i)
		record, err := model.NewRecord(timestamp, project, i+1, nil)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}

		err = store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// 異なるプロジェクトのレコードも作成（リストに含まれないことを確認）
	otherRecord, err := model.NewRecord(now, "other-project", 10, nil)
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
			result, err := store.ListRecords(context.Background(), project, tc.from, tc.to)
			if err != nil {
				t.Fatalf("Failed to list records: %v", err)
			}

			if len(result) != tc.expected {
				t.Errorf("%s: Expected %d records, got %d", tc.description, tc.expected, len(result))
			}

			// 取得したレコードが期間内にあることを確認
			for _, r := range result {
				if r.Project != project {
					t.Errorf("Expected project %s, got %s", project, r.Project)
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
	projectModel1, err := model.NewProject("project1", "Project 1")
	if err != nil {
		t.Fatalf("Failed to create project1 model: %v", err)
	}
	err = store.CreateProject(context.Background(), projectModel1)
	if err != nil {
		t.Fatalf("Failed to create project1: %v", err)
	}

	projectModel2, err := model.NewProject("project2", "Project 2")
	if err != nil {
		t.Fatalf("Failed to create project2 model: %v", err)
	}
	err = store.CreateProject(context.Background(), projectModel2)
	if err != nil {
		t.Fatalf("Failed to create project2: %v", err)
	}

	// テスト用のデータセットアップ
	project1 := "project1"
	project2 := "project2"
	now := time.Now()

	// プロジェクト1用のレコードを3つ作成
	for i := 0; i < 3; i++ {
		record, err := model.NewRecord(now.AddDate(0, 0, i), project1, i+1, nil)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
		if err := store.CreateRecord(context.Background(), record); err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// プロジェクト2用のレコードを2つ作成
	for i := 0; i < 2; i++ {
		record, err := model.NewRecord(now.AddDate(0, 0, i), project2, i+10, nil)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
		if err := store.CreateRecord(context.Background(), record); err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// プロジェクト1のレコード数を確認
	project1Records, err := store.ListRecords(context.Background(), project1, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Failed to list project1 records: %v", err)
	}
	if len(project1Records) != 3 {
		t.Errorf("Expected 3 records for project1, got %d", len(project1Records))
	}

	// プロジェクト1を削除
	err = store.DeleteProject(context.Background(), project1)
	if err != nil {
		t.Fatalf("Failed to delete project: %v", err)
	}

	// プロジェクト1のレコードが存在しなくなっていることを確認
	project1RecordsAfter, err := store.ListRecords(context.Background(), project1, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Failed to list project1 records after deletion: %v", err)
	}
	if len(project1RecordsAfter) != 0 {
		t.Errorf("Expected 0 records for project1 after deletion, got %d", len(project1RecordsAfter))
	}

	// プロジェクト2のレコードが残っていることを確認
	project2Records, err := store.ListRecords(context.Background(), project2, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Failed to list project2 records: %v", err)
	}
	if len(project2Records) != 2 {
		t.Errorf("Expected 2 records for project2, got %d", len(project2Records))
	}

	// 存在しないプロジェクトを削除しても問題ないことを確認
	err = store.DeleteProject(context.Background(), "non-existent-project")
	if err != nil {
		t.Errorf("Expected no error when deleting non-existent project, got %v", err)
	}
}

// TestListRecordsWithTags はタグフィルタでのレコード取得のテスト
func TestListRecordsWithTags(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	projectModel, err := model.NewProject("test-project", "Test project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), projectModel)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	project := "test-project"
	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)

	// 異なるタグを持つレコードを作成
	record1, _ := model.NewRecord(baseTime, project, 1, []string{"work", "urgent"})
	record2, _ := model.NewRecord(baseTime.Add(1*time.Hour), project, 2, []string{"personal", "hobby"})
	record3, _ := model.NewRecord(baseTime.Add(2*time.Hour), project, 3, []string{"work", "meeting"})
	record4, _ := model.NewRecord(baseTime.Add(3*time.Hour), project, 4, []string{"personal", "urgent"})

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
		expectedIDs   []uuid.UUID
	}{
		{
			name:          "Filter by work tag",
			tags:          []string{"work"},
			expectedCount: 2,
			expectedIDs:   []uuid.UUID{record1.ID, record3.ID},
		},
		{
			name:          "Filter by personal tag",
			tags:          []string{"personal"},
			expectedCount: 2,
			expectedIDs:   []uuid.UUID{record2.ID, record4.ID},
		},
		{
			name:          "Filter by urgent tag",
			tags:          []string{"urgent"},
			expectedCount: 2,
			expectedIDs:   []uuid.UUID{record1.ID, record4.ID},
		},
		{
			name:          "Filter by multiple tags (OR)",
			tags:          []string{"work", "hobby"},
			expectedCount: 3,
			expectedIDs:   []uuid.UUID{record1.ID, record2.ID, record3.ID},
		},
		{
			name:          "Filter by non-existent tag",
			tags:          []string{"nonexistent"},
			expectedCount: 0,
			expectedIDs:   []uuid.UUID{},
		},
		{
			name:          "Filter by multiple urgent,meeting (OR)",
			tags:          []string{"urgent", "meeting"},
			expectedCount: 3,
			expectedIDs:   []uuid.UUID{record1.ID, record3.ID, record4.ID},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 日付範囲を設定（全レコードが含まれる範囲）
			fromTime := baseTime.Add(-1 * time.Hour)
			toTime := baseTime.Add(5 * time.Hour)

			// タグフィルタでレコードを取得
			records, err := store.ListRecordsWithTags(context.Background(), project, fromTime, toTime, tc.tags)
			if err != nil {
				t.Fatalf("Failed to list records with tags: %v", err)
			}

			// 件数チェック
			if len(records) != tc.expectedCount {
				t.Errorf("Expected %d records, got %d", tc.expectedCount, len(records))
			}

			// IDが期待されるものと一致するかチェック
			actualIDs := make(map[uuid.UUID]bool)
			for _, record := range records {
				actualIDs[record.ID] = true
			}

			for _, expectedID := range tc.expectedIDs {
				if !actualIDs[expectedID] {
					t.Errorf("Expected record with ID %s not found in results", expectedID)
				}
			}

			// 取得されたレコードが期待されるタグを持っているかチェック
			for _, record := range records {
				hasMatchingTag := false
				for _, filterTag := range tc.tags {
					for _, recordTag := range record.Tags {
						if recordTag == filterTag {
							hasMatchingTag = true
							break
						}
					}
					if hasMatchingTag {
						break
					}
				}
				if !hasMatchingTag && len(tc.tags) > 0 {
					t.Errorf("Record %s does not have any of the filter tags %v, but was returned", record.ID, tc.tags)
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
	projectModel, err := model.NewProject("empty-project", "Empty project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), projectModel)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	project := "empty-project"
	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)

	// タグなしのレコードを作成
	record, _ := model.NewRecord(baseTime, project, 1, []string{})
	err = store.CreateRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// 存在しないタグでフィルタ
	fromTime := baseTime.Add(-1 * time.Hour)
	toTime := baseTime.Add(1 * time.Hour)
	records, err := store.ListRecordsWithTags(context.Background(), project, fromTime, toTime, []string{"nonexistent"})
	if err != nil {
		t.Fatalf("Failed to list records with tags: %v", err)
	}

	if len(records) != 0 {
		t.Errorf("Expected 0 records, got %d", len(records))
	}
}

// TestListRecordsWithTagsDateRange は日付範囲フィルタのテスト
func TestListRecordsWithTagsDateRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// プロジェクトを事前に作成
	projectModel, err := model.NewProject("date-range-project", "Date range project")
	if err != nil {
		t.Fatalf("Failed to create project model: %v", err)
	}
	err = store.CreateProject(context.Background(), projectModel)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	project := "date-range-project"
	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)

	// 異なる日時のレコードを作成
	record1, _ := model.NewRecord(baseTime, project, 1, []string{"work"})
	record2, _ := model.NewRecord(baseTime.Add(24*time.Hour), project, 2, []string{"work"})
	record3, _ := model.NewRecord(baseTime.Add(48*time.Hour), project, 3, []string{"work"})

	for _, record := range []*model.Record{record1, record2, record3} {
		err := store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
	}

	// 最初の2日分のみを取得
	fromTime := baseTime.Add(-1 * time.Hour)
	toTime := baseTime.Add(25 * time.Hour)
	records, err := store.ListRecordsWithTags(context.Background(), project, fromTime, toTime, []string{"work"})
	if err != nil {
		t.Fatalf("Failed to list records with tags: %v", err)
	}

	// 2件取得されることを確認
	if len(records) != 2 {
		t.Errorf("Expected 2 records, got %d", len(records))
	}

	// 正しいレコードが取得されることを確認
	expectedIDs := map[uuid.UUID]bool{record1.ID: true, record2.ID: true}
	for _, record := range records {
		if !expectedIDs[record.ID] {
			t.Errorf("Unexpected record ID %s in results", record.ID)
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
	retrievedProject, err := store.GetProject(context.Background(), "test-project")
	if err != nil {
		t.Fatalf("Failed to get project: %v", err)
	}

	// 内容の確認
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
	_, err := store.GetProject(context.Background(), "non-existent")
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

	// プロジェクトを取得
	retrievedProject, err := store.GetProject(context.Background(), "update-test")
	if err != nil {
		t.Fatalf("Failed to get project: %v", err)
	}

	// 説明を更新
	originalUpdatedAt := retrievedProject.UpdatedAt
	
	// 秒単位で時間差を確保してより明確な時間差を作る
	time.Sleep(1 * time.Second)
	retrievedProject.Description = "Updated description"
	retrievedProject.UpdatedAt = time.Now()

	// 更新を保存
	err = store.UpdateProject(context.Background(), retrievedProject)
	if err != nil {
		t.Fatalf("Failed to update project: %v", err)
	}

	// 更新されたプロジェクトを再取得
	updatedProject, err := store.GetProject(context.Background(), "update-test")
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
	err = store.DeleteProjectEntity(context.Background(), "delete-test")
	if err != nil {
		t.Fatalf("Failed to delete project entity: %v", err)
	}

	// プロジェクトが削除されたことを確認
	_, err = store.GetProject(context.Background(), "delete-test")
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
	retrievedProjects, err := store.ListProjects(context.Background())
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
	projects, err := store.ListProjects(context.Background())
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

	// 存在しないプロジェクトでレコードを作成しようとする
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local)
	record, err := model.NewRecord(timestamp, "non-existent-project", 1, []string{"test"})
	if err != nil {
		t.Fatalf("Failed to create record model: %v", err)
	}

	// レコード作成は失敗するはず
	err = store.CreateRecord(context.Background(), record)
	if err == nil {
		t.Error("Expected error when creating record with non-existent project, got nil")
	}
	if !strings.Contains(err.Error(), "project not found") {
		t.Errorf("Expected 'project not found' error, got: %v", err)
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

	// 今度は同じプロジェクト名でレコード作成が成功するはず
	record2, err := model.NewRecord(timestamp, "existing-project", 1, []string{"test"})
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
	record, err := model.NewRecord(timestamp, "test-project", 1, []string{"test"})
	if err != nil {
		t.Fatalf("Failed to create record model: %v", err)
	}
	err = store.CreateRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// プロジェクトエンティティを削除
	err = store.DeleteProjectEntity(context.Background(), "test-project")
	if err != nil {
		t.Fatalf("Failed to delete project entity: %v", err)
	}

	// 外部キー制約がないため、関連するレコードは残っている
	records, err := store.ListRecords(context.Background(), "test-project", 
		timestamp.Add(-1*time.Hour), timestamp.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to list records: %v", err)
	}

	if len(records) != 1 {
		t.Errorf("Expected 1 orphaned record after project deletion, got %d", len(records))
	}

	// レコードを直接取得しても見つかるはず（孤立状態）
	_, err = store.GetRecord(context.Background(), record.ID)
	if err != nil {
		t.Errorf("Expected to find orphaned record, but got error: %v", err)
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
	record, err := model.NewRecord(timestamp, "project1", 1, []string{"test"})
	if err != nil {
		t.Fatalf("Failed to create record model: %v", err)
	}
	err = store.CreateRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// project2を削除
	err = store.DeleteProjectEntity(context.Background(), "project2")
	if err != nil {
		t.Fatalf("Failed to delete project2: %v", err)
	}

	// レコードを存在しないproject2に更新しようとする（失敗するはず）
	record.Project = "project2"
	err = store.UpdateRecord(context.Background(), record)
	if err == nil {
		t.Error("Expected error when updating record to non-existent project, got nil")
	}
	if !strings.Contains(err.Error(), "project not found") {
		t.Errorf("Expected 'project not found' error, got: %v", err)
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
	record1, _ := model.NewRecord(baseTime, "tag-test", 1, []string{"work", "important"})
	record2, _ := model.NewRecord(baseTime.Add(1*time.Hour), "tag-test", 2, []string{"personal", "urgent"})
	record3, _ := model.NewRecord(baseTime.Add(2*time.Hour), "tag-test", 3, []string{"work", "meeting"})
	record4, _ := model.NewRecord(baseTime.Add(3*time.Hour), "tag-test", 4, []string{}) // タグなし

	// レコードを保存
	for _, record := range []*model.Record{record1, record2, record3, record4} {
		err := store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
	}

	// プロジェクトのタグ一覧を取得
	tags, err := store.GetProjectTags(context.Background(), "tag-test")
	if err != nil {
		t.Fatalf("Failed to get project tags: %v", err)
	}

	// 期待されるタグが含まれているかチェック
	expectedTags := []string{"work", "important", "personal", "urgent", "meeting"}
	
	if len(tags) != len(expectedTags) {
		t.Errorf("Expected %d tags, got %d", len(expectedTags), len(tags))
	}

	// タグが期待されるものと一致するかチェック
	tagSet := make(map[string]bool)
	for _, tag := range tags {
		tagSet[tag] = true
	}

	for _, expectedTag := range expectedTags {
		if !tagSet[expectedTag] {
			t.Errorf("Expected tag '%s' not found in response", expectedTag)
		}
	}
}

// TestGetProjectTagsNonExistentProject は存在しないプロジェクトのタグ取得をテストします。
func TestGetProjectTagsNonExistentProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// 存在しないプロジェクトのタグを取得（エラーになるはず）
	_, err := store.GetProjectTags(context.Background(), "non-existent")
	if err == nil {
		t.Error("Expected error when getting tags for non-existent project, got nil")
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
	record, _ := model.NewRecord(baseTime, "empty-tags", 1, []string{})
	err = store.CreateRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// プロジェクトのタグ一覧を取得（空配列が返されるはず）
	tags, err := store.GetProjectTags(context.Background(), "empty-tags")
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
	record1, _ := model.NewRecord(baseTime, "duplicate-tags", 1, []string{"work", "important"})
	record2, _ := model.NewRecord(baseTime.Add(1*time.Hour), "duplicate-tags", 2, []string{"work", "urgent"}) // workが重複
	record3, _ := model.NewRecord(baseTime.Add(2*time.Hour), "duplicate-tags", 3, []string{"important", "meeting"}) // importantが重複

	// レコードを保存
	for _, record := range []*model.Record{record1, record2, record3} {
		err := store.CreateRecord(context.Background(), record)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
	}

	// プロジェクトのタグ一覧を取得
	tags, err := store.GetProjectTags(context.Background(), "duplicate-tags")
	if err != nil {
		t.Fatalf("Failed to get project tags: %v", err)
	}

	// 重複が排除されてユニークなタグのみが返されることを確認
	expectedTags := []string{"work", "important", "urgent", "meeting"}
	
	if len(tags) != len(expectedTags) {
		t.Errorf("Expected %d unique tags, got %d", len(expectedTags), len(tags))
	}

	// タグが期待されるものと一致するかチェック
	tagSet := make(map[string]bool)
	for _, tag := range tags {
		tagSet[tag] = true
	}

	for _, expectedTag := range expectedTags {
		if !tagSet[expectedTag] {
			t.Errorf("Expected tag '%s' not found in response", expectedTag)
		}
	}

	// 重複がないことを確認
	if len(tags) != len(tagSet) {
		t.Error("Duplicate tags found in response")
	}
}
