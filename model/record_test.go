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
	tags := []string{"test", "example"}
	record, err := NewRecord(timestamp, project, value, tags)
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

	// Tagsフィールドが正しく設定されていることを確認
	if len(record.Tags) != 2 || record.Tags[0] != "test" || record.Tags[1] != "example" {
		t.Errorf("Expected Tags to be %v, got %v", tags, record.Tags)
	}
}

func TestValidate(t *testing.T) {
	// 有効なレコード
	if _, err := LoadRecord(
		uuid.New(),
		time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local),
		"exercise",
		1,
		[]string{"valid"},
	); err != nil {
		t.Fatalf("Failed to create valid record: %v", err)
	}

	// 無効なレコード（IDが空）
	if _, err := LoadRecord(
		uuid.Nil,
		time.Date(2025, 5, 21, 14, 30, 0, 0, time.Local),
		"exercise",
		1,
		[]string{},
	); err == nil {
		t.Error("Expected error for empty ID, got nil")
	}
}

func TestNewDateRange(t *testing.T) {
	tests := []struct {
		name     string
		fromStr  string
		toStr    string
		wantErr  bool
		checkFn  func(*DateRange) bool
	}{
		{
			name:    "full datetime format",
			fromStr: "2025-01-01T10:30:45Z",
			toStr:   "2025-01-02T15:45:30Z",
			wantErr: false,
			checkFn: func(dr *DateRange) bool {
				// from should be normalized to 00:00:00
				expectedFrom := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
				// to should be normalized to 23:59:59.999999999
				expectedTo := time.Date(2025, 1, 2, 23, 59, 59, 999999999, time.UTC)
				return dr.From().Equal(expectedFrom) && dr.To().Equal(expectedTo)
			},
		},
		{
			name:    "date only format",
			fromStr: "2025-01-01",
			toStr:   "2025-01-02",
			wantErr: false,
			checkFn: func(dr *DateRange) bool {
				// from should be normalized to 00:00:00
				expectedFrom := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
				// to should be normalized to 23:59:59.999999999
				expectedTo := time.Date(2025, 1, 2, 23, 59, 59, 999999999, time.UTC)
				return dr.From().Equal(expectedFrom) && dr.To().Equal(expectedTo)
			},
		},
		{
			name:    "mixed format",
			fromStr: "2025-01-01",
			toStr:   "2025-01-02T12:30:45Z",
			wantErr: false,
			checkFn: func(dr *DateRange) bool {
				// Both should be normalized properly
				return dr.From().Hour() == 0 && dr.To().Hour() == 23
			},
		},
		{
			name:    "invalid format",
			fromStr: "invalid-date",
			toStr:   "2025-01-02",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dr, err := NewDateRange(tt.fromStr, tt.toStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDateRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checkFn != nil && !tt.checkFn(dr) {
				t.Errorf("NewDateRange() validation failed for %s", tt.name)
			}
		})
	}
}
