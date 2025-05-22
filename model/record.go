// Package model は、アプリケーションのデータモデル定義を提供します。
package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Record は日々のアクティビティデータを表すモデルです。
type Record struct {
	ID      uuid.UUID `json:"id"`
	Project string    `json:"project"` // アクティビティのカテゴリー
	Value   int       `json:"value"`   // 記録値
	DoneAt  time.Time `json:"done_at"` // アクティビティの日時
}

// NewRecord はRecordの新しいインスタンスを作成し、UUIDと作成時間を設定します。
func NewRecord(doneAt time.Time, project string, value int) (*Record, error) {
	rec := &Record{
		ID:      uuid.New(),
		Project: project,
		Value:   value,
		DoneAt:  doneAt,
	}
	if err := rec.Validate(); err != nil {
		return nil, err
	}
	return rec, nil
}

// LoadRecord はRecordの新しいインスタンスを作成し、UUIDと作成時間を設定します。
func LoadRecord(id uuid.UUID, doneAt time.Time, project string, value int) (*Record, error) {
	rec := &Record{
		ID:      id,
		Project: project,
		Value:   value,
		DoneAt:  doneAt,
	}
	err := rec.Validate()
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// Validate はレコードのデータバリデーションを行います。
func (r *Record) Validate() error {
	// IDの検証
	if r.ID == uuid.Nil {
		return errors.New("id is required")
	}

	// 日時の検証
	if r.DoneAt.IsZero() {
		return errors.New("done_at is required")
	}

	// カテゴリーの検証
	if r.Project == "" {
		return errors.New("project is required")
	}

	return nil
}
