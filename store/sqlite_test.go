package store

import (
	"database/sql"
	"os"
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

	// テストデータ
	doneAt := time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local)
	record, err := model.NewRecord(doneAt, "exercise", 1)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// レコードを作成
	err = store.CreateRecord(record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// 作成したレコードを取得
	retrievedRecord, err := store.GetRecord(record.ID)
	if err != nil {
		t.Fatalf("Failed to get record: %v", err)
	}

	// 取得したレコードが元のレコードと一致することを確認
	if retrievedRecord.ID != record.ID {
		t.Errorf("Expected ID %s, got %s", record.ID, retrievedRecord.ID)
	}

	if !retrievedRecord.DoneAt.Equal(record.DoneAt) {
		t.Errorf("Expected DoneAt %v, got %v", record.DoneAt, retrievedRecord.DoneAt)
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
	_, err := store.GetRecord(nonExistentID)
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
	err := store.CreateRecord(invalidRecord)
	if err == nil {
		t.Error("Expected validation error when creating invalid record, got nil")
	}
}

func TestDeleteRecord(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// テストデータの作成
	doneAt := time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local)
	record, err := model.NewRecord(doneAt, "exercise", 1)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// レコードを保存
	err = store.CreateRecord(record)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// レコードを削除
	err = store.DeleteRecord(record.ID)
	if err != nil {
		t.Fatalf("Failed to delete record: %v", err)
	}

	// 削除したレコードが存在しないことを確認
	_, err = store.GetRecord(record.ID)
	if err == nil {
		t.Error("Expected error when getting deleted record, got nil")
	}

	if err != nil && err.Error() != "record not found" {
		t.Errorf("Expected 'record not found' error, got '%v'", err)
	}

	// 存在しないレコードの削除を試みる
	err = store.DeleteRecord(uuid.New())
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
		doneAt := yesterday.AddDate(0, 0, i)
		record, err := model.NewRecord(doneAt, project, i+1)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}

		err = store.CreateRecord(record)
		if err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// 異なるプロジェクトのレコードも作成（リストに含まれないことを確認）
	otherRecord, err := model.NewRecord(now, "other-project", 10)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}
	err = store.CreateRecord(otherRecord)
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
			result, err := store.ListRecords(project, tc.from, tc.to)
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
				rYear, rMonth, rDay := r.DoneAt.Date()
				fromYear, fromMonth, fromDay := tc.from.Date()
				toYear, toMonth, toDay := tc.to.Date()

				rDate := time.Date(rYear, rMonth, rDay, 0, 0, 0, 0, r.DoneAt.Location())
				fromDate := time.Date(fromYear, fromMonth, fromDay, 0, 0, 0, 0, tc.from.Location())
				toDate := time.Date(toYear, toMonth, toDay, 0, 0, 0, 0, tc.to.Location())

				if rDate.Before(fromDate) || rDate.After(toDate) {
					t.Errorf("Record date %v is outside range %v to %v", rDate, fromDate, toDate)
				}
			}
		})
	}
}

func TestGetProjectInfo(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// テスト用のプロジェクト名とデータ
	project := "study"
	startDate := time.Date(2025, 1, 1, 12, 0, 0, 0, time.Local)

	// 3つのレコードを作成（合計値が6になるようにする）
	values := []int{1, 2, 3}
	var firstDate, lastDate time.Time

	for i, val := range values {
		doneAt := startDate.AddDate(0, 0, i) // 1日ずつずらす
		if i == 0 {
			firstDate = doneAt
		}
		if i == len(values)-1 {
			lastDate = doneAt
		}

		record, err := model.NewRecord(doneAt, project, val)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}

		if err := store.CreateRecord(record); err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// プロジェクト情報を取得
	info, err := store.GetProjectInfo(project)
	if err != nil {
		t.Fatalf("Failed to get project info: %v", err)
	}

	// 取得した情報が正しいことを確認
	if info.Name != project {
		t.Errorf("Expected project name %s, got %s", project, info.Name)
	}

	if info.RecordCount != len(values) {
		t.Errorf("Expected record count %d, got %d", len(values), info.RecordCount)
	}

	expectedTotal := 1 + 2 + 3
	if info.TotalValue != expectedTotal {
		t.Errorf("Expected total value %d, got %d", expectedTotal, info.TotalValue)
	}

	// 日付の精度によって誤差が出る可能性があるため、日付の部分だけ比較
	if !sameDay(info.FirstRecordAt, firstDate) {
		t.Errorf("Expected first record date close to %v, got %v", firstDate, info.FirstRecordAt)
	}

	if !sameDay(info.LastRecordAt, lastDate) {
		t.Errorf("Expected last record date close to %v, got %v", lastDate, info.LastRecordAt)
	}

	// 存在しないプロジェクト
	_, err = store.GetProjectInfo("non-existent-project")
	if err != sql.ErrNoRows {
		t.Errorf("Expected sql.ErrNoRows for non-existent project, got %v", err)
	}
}

// sameDay は2つの時刻が同じ日であることを確認します
func sameDay(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func TestDeleteProject(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// テスト用のデータセットアップ
	project1 := "project1"
	project2 := "project2"
	now := time.Now()

	// プロジェクト1用のレコードを3つ作成
	for i := 0; i < 3; i++ {
		record, err := model.NewRecord(now.AddDate(0, 0, i), project1, i+1)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
		if err := store.CreateRecord(record); err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// プロジェクト2用のレコードを2つ作成
	for i := 0; i < 2; i++ {
		record, err := model.NewRecord(now.AddDate(0, 0, i), project2, i+10)
		if err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}
		if err := store.CreateRecord(record); err != nil {
			t.Fatalf("Failed to store record: %v", err)
		}
	}

	// プロジェクト1のレコード数を確認
	project1Info, err := store.GetProjectInfo(project1)
	if err != nil {
		t.Fatalf("Failed to get project info: %v", err)
	}
	if project1Info.RecordCount != 3 {
		t.Errorf("Expected 3 records for project1, got %d", project1Info.RecordCount)
	}

	// プロジェクト1を削除
	err = store.DeleteProject(project1)
	if err != nil {
		t.Fatalf("Failed to delete project: %v", err)
	}

	// プロジェクト1が存在しなくなっていることを確認
	_, err = store.GetProjectInfo(project1)
	if err != sql.ErrNoRows {
		t.Errorf("Expected sql.ErrNoRows after deleting project1, got %v", err)
	}

	// プロジェクト2のレコードが残っていることを確認
	project2Info, err := store.GetProjectInfo(project2)
	if err != nil {
		t.Fatalf("Failed to get project2 info: %v", err)
	}
	if project2Info.RecordCount != 2 {
		t.Errorf("Expected 2 records for project2, got %d", project2Info.RecordCount)
	}

	// 存在しないプロジェクトを削除しても問題ないことを確認
	err = store.DeleteProject("non-existent-project")
	if err != nil {
		t.Errorf("Expected no error when deleting non-existent project, got %v", err)
	}
}
