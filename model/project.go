// Package model は、アプリケーションのデータモデル定義を提供します。
package model

import (
	"errors"
	"time"
)

// Project はプロジェクトエンティティを表すモデルです。
type Project struct {
	Name        string    `json:"name"`        // プロジェクト名
	Description string    `json:"description"` // プロジェクトの説明
	CreatedAt   time.Time `json:"created_at"`  // 作成日時
	UpdatedAt   time.Time `json:"updated_at"`  // 更新日時
}

// NewProject は新しいProjectインスタンスを作成します。
func NewProject(name, description string) (*Project, error) {
	now := time.Now()
	p := &Project{
		Name:        name,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// LoadProject は既存のProjectインスタンスを作成します。
func LoadProject(name, description string, createdAt, updatedAt time.Time) (*Project, error) {
	p := &Project{
		Name:        name,
		Description: description,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// Validate はプロジェクトのデータバリデーションを行います。
func (p *Project) Validate() error {
	if p.Name == "" {
		return errors.New("name is required")
	}
	if p.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	if p.UpdatedAt.IsZero() {
		return errors.New("updated_at is required")
	}
	return nil
}
