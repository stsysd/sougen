// Package model は、アプリケーションのデータモデル定義を提供します。
package model

import (
	"errors"
	"strings"
	"time"
)

// Record は日々のアクティビティデータを表すモデルです。
type Record struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"project_id"` // プロジェクトID
	Value     int       `json:"value"`      // 記録値
	Timestamp time.Time `json:"timestamp"`  // アクティビティの日時
	Tags      []string  `json:"tags"`       // タグ一覧
}

// NewRecord はRecordの新しいインスタンスを作成します。
// IDはデータベース側で自動生成されるため、0を設定します。
func NewRecord(timestamp time.Time, projectID int64, value int, tags []string) (*Record, error) {
	if tags == nil {
		tags = []string{}
	}
	rec := &Record{
		ID:        -1, // DBのAUTOINCREMENTで自動生成
		ProjectID: projectID,
		Value:     value,
		Timestamp: timestamp,
		Tags:      tags,
	}
	if err := rec.Validate(); err != nil {
		return nil, err
	}
	return rec, nil
}

// LoadRecord は既存のRecordインスタンスを作成します。
func LoadRecord(id int64, timestamp time.Time, projectID int64, value int, tags []string) (*Record, error) {
	// LoadRecordはDBから読み込んだレコード用なので、IDは必須
	if id <= 0 {
		return nil, errors.New("id is required for loaded record")
	}

	if tags == nil {
		tags = []string{}
	}
	rec := &Record{
		ID:        id,
		ProjectID: projectID,
		Value:     value,
		Timestamp: timestamp,
		Tags:      tags,
	}
	err := rec.Validate()
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// Validate はレコードのデータバリデーションを行います。
func (r *Record) Validate() error {
	// 日時の検証
	if r.Timestamp.IsZero() {
		return errors.New("timestamp is required")
	}

	// プロジェクトIDの検証
	if r.ProjectID <= 0 {
		return errors.New("project_id is required")
	}

	// タグの検証
	for _, tag := range r.Tags {
		if tag == "" {
			return errors.New("tag cannot be empty")
		}
		// スペースは区切り文字として使用するため禁止
		if strings.Contains(tag, " ") {
			return errors.New("tag cannot contain spaces")
		}
	}

	return nil
}
