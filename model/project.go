// Package model は、アプリケーションのデータモデル定義を提供します。
package model

import (
	"time"
)

// ProjectInfo はプロジェクト情報を表すモデルです。
type ProjectInfo struct {
	Name          string    `json:"name"`            // プロジェクト名
	RecordCount   int       `json:"record_count"`    // レコード数
	TotalValue    int       `json:"total_value"`     // 合計値
	FirstRecordAt time.Time `json:"first_record_at"` // 最初のレコードの日時
	LastRecordAt  time.Time `json:"last_record_at"`  // 最新のレコードの日時
}

// NewProjectInfo は新しいProjectInfoインスタンスを作成します。
func NewProjectInfo(name string, recordCount, totalValue int, firstRecordAt, lastRecordAt time.Time) *ProjectInfo {
	return &ProjectInfo{
		Name:          name,
		RecordCount:   recordCount,
		TotalValue:    totalValue,
		FirstRecordAt: firstRecordAt,
		LastRecordAt:  lastRecordAt,
	}
}
