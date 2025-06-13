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
	Timestamp  time.Time `json:"timestamp"` // アクティビティの日時
	Tags    []string  `json:"tags"`     // タグ一覧
}

// NewRecord はRecordの新しいインスタンスを作成し、UUIDと作成時間を設定します。
func NewRecord(timestamp time.Time, project string, value int, tags []string) (*Record, error) {
	if tags == nil {
		tags = []string{}
	}
	rec := &Record{
		ID:      uuid.New(),
		Project: project,
		Value:   value,
		Timestamp:  timestamp,
		Tags:    tags,
	}
	if err := rec.Validate(); err != nil {
		return nil, err
	}
	return rec, nil
}

// LoadRecord はRecordの新しいインスタンスを作成し、UUIDと作成時間を設定します。
func LoadRecord(id uuid.UUID, timestamp time.Time, project string, value int, tags []string) (*Record, error) {
	if tags == nil {
		tags = []string{}
	}
	rec := &Record{
		ID:      id,
		Project: project,
		Value:   value,
		Timestamp:  timestamp,
		Tags:    tags,
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
	if r.Timestamp.IsZero() {
		return errors.New("timestamp is required")
	}

	// カテゴリーの検証
	if r.Project == "" {
		return errors.New("project is required")
	}

	return nil
}
