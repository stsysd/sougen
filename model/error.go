// Package model は、アプリケーションのデータモデル定義を提供します。
package model

import "errors"

// センチネルエラー - リソースが見つからない場合
var (
	ErrRecordNotFound  = errors.New("record not found")
	ErrProjectNotFound = errors.New("project not found")
)

// ValidationError はバリデーションエラーを表す型
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// NewValidationError はValidationErrorを生成するヘルパー関数
func NewValidationError(msg string) error {
	return &ValidationError{Message: msg}
}
