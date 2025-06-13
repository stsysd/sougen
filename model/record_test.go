package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewRecord(t *testing.T) {
	// テストデータ
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local)
	project := "exercise"
	value := 1

	// レコードを生成
	record, err := NewRecord(timestamp, project, value)
	if err != nil {
		t.Fatalf("Failed to create record: %v", err)
	}

	// IDが生成されていることを確認
	if record.ID == uuid.Nil {
		t.Error("Expected ID to be generated, got Nil UUID")
	}

	// 各フィールドが正しく設定されていることを確認
	if !record.Timestamp.Equal(timestamp) {
		t.Errorf("Expected Timestamp to be %v, got %v", timestamp, record.Timestamp)
	}

	if record.Project != project {
		t.Errorf("Expected project to be %s, got %s", project, record.Project)
	}

	if record.Value != value {
		t.Errorf("Expected Value to be %d, got %d", value, record.Value)
	}
}

func TestValidate(t *testing.T) {
	// 有効なレコード
	if _, err := LoadRecord(
		uuid.New(),
		time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local),
		"exercise",
		1,
	); err != nil {
		t.Fatalf("Failed to create valid record: %v", err)
	}

	// 無効なレコード（IDが空）
	if _, err := LoadRecord(
		uuid.Nil,
		time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local),
		"exercise",
		1,
	); err == nil {
		t.Error("Expected error for empty ID, got nil")
	}
}
