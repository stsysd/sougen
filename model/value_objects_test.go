package model

import (
	"encoding/base64"
	"testing"
	"time"
)

// Test helper functions and constants
var (
	testZeroTime time.Time
	testHour     = time.Hour
)

func testTime() time.Time {
	return time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
}

func testParseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

func testEncodeBase64(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

// TestNewPagination tests the NewPagination function
func TestNewPagination(t *testing.T) {
	tests := []struct {
		name        string
		limitStr    string
		cursorStr   string
		expectError bool
		expectedLimit int
		description string
	}{
		{
			name:          "Valid limit and cursor",
			limitStr:      "50",
			cursorStr:     "test-cursor",
			expectError:   false,
			expectedLimit: 50,
			description:   "正常なlimitとcursorで成功すること",
		},
		{
			name:          "Default limit with empty strings",
			limitStr:      "",
			cursorStr:     "",
			expectError:   false,
			expectedLimit: 100,
			description:   "空文字列の場合、デフォルトのlimit=100が設定されること",
		},
		{
			name:          "Valid limit without cursor",
			limitStr:      "25",
			cursorStr:     "",
			expectError:   false,
			expectedLimit: 25,
			description:   "cursorなしでも正常に動作すること",
		},
		{
			name:          "Limit exceeds maximum",
			limitStr:      "2000",
			cursorStr:     "",
			expectError:   false,
			expectedLimit: 1000,
			description:   "limitが1000を超える場合、1000に制限されること",
		},
		{
			name:          "Invalid limit (non-numeric)",
			limitStr:      "abc",
			cursorStr:     "",
			expectError:   true,
			expectedLimit: 0,
			description:   "limitが数値でない場合、エラーになること",
		},
		{
			name:          "Invalid limit (negative)",
			limitStr:      "-10",
			cursorStr:     "",
			expectError:   true,
			expectedLimit: 0,
			description:   "limitが負の数の場合、エラーになること",
		},
		{
			name:          "Invalid limit (zero)",
			limitStr:      "0",
			cursorStr:     "",
			expectError:   true,
			expectedLimit: 0,
			description:   "limitが0の場合、エラーになること",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pagination, err := NewPagination(tt.limitStr, tt.cursorStr)

			if tt.expectError {
				if err == nil {
					t.Errorf("%s: expected error but got nil", tt.description)
				}
				return
			}

			if err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
				return
			}

			if pagination.Limit() != tt.expectedLimit {
				t.Errorf("%s: expected limit %d, got %d", tt.description, tt.expectedLimit, pagination.Limit())
			}

			// cursorのチェック
			if tt.cursorStr == "" {
				if pagination.Cursor() != nil {
					t.Errorf("%s: expected nil cursor, got %v", tt.description, pagination.Cursor())
				}
			} else {
				if pagination.Cursor() == nil {
					t.Errorf("%s: expected cursor %s, got nil", tt.description, tt.cursorStr)
				} else if *pagination.Cursor() != tt.cursorStr {
					t.Errorf("%s: expected cursor %s, got %s", tt.description, tt.cursorStr, *pagination.Cursor())
				}
			}
		})
	}
}

// TestNewPaginationWithValues tests the NewPaginationWithValues function
func TestNewPaginationWithValues(t *testing.T) {
	cursor := "test-cursor"
	pagination := NewPaginationWithValues(50, &cursor)

	if pagination.Limit() != 50 {
		t.Errorf("Expected limit 50, got %d", pagination.Limit())
	}

	if pagination.Cursor() == nil {
		t.Error("Expected cursor to be set, got nil")
	} else if *pagination.Cursor() != cursor {
		t.Errorf("Expected cursor %s, got %s", cursor, *pagination.Cursor())
	}

	// nil cursorのテスト
	paginationNilCursor := NewPaginationWithValues(100, nil)
	if paginationNilCursor.Cursor() != nil {
		t.Errorf("Expected nil cursor, got %v", paginationNilCursor.Cursor())
	}
}

// TestEncodeDecodeProjectCursor tests ProjectCursor encoding and decoding with ID
func TestEncodeDecodeProjectCursor(t *testing.T) {
	updatedAt := testTime()
	id := "550e8400-e29b-41d4-a716-446655440000" // Valid UUID string

	// Encode cursor
	encoded := EncodeProjectCursor(updatedAt, id)
	if encoded == "" {
		t.Error("Expected non-empty encoded cursor")
	}

	// Decode cursor
	decoded, err := DecodeProjectCursor(encoded)
	if err != nil {
		t.Fatalf("Failed to decode cursor: %v", err)
	}

	// Verify updatedAt
	decodedUpdatedAt, err := testParseTime(decoded.UpdatedAt)
	if err != nil {
		t.Fatalf("Failed to parse decoded updatedAt: %v", err)
	}
	if !decodedUpdatedAt.Equal(updatedAt) {
		t.Errorf("Expected updatedAt %v, got %v", updatedAt, decodedUpdatedAt)
	}

	// Verify ID
	if decoded.ID != id {
		t.Errorf("Expected ID %s, got %s", id, decoded.ID)
	}
}

// TestDecodeInvalidProjectCursor tests decoding invalid project cursors
func TestDecodeInvalidProjectCursor(t *testing.T) {
	tests := []struct {
		name        string
		encoded     string
		expectError bool
		description string
	}{
		{
			name:        "Empty string",
			encoded:     "",
			expectError: false, // Empty string returns nil, not error
			description: "空文字列の場合はnilを返すこと",
		},
		{
			name:        "Invalid base64",
			encoded:     "not-valid-base64!@#",
			expectError: true,
			description: "不正なbase64文字列の場合はエラーになること",
		},
		{
			name:        "Invalid JSON",
			encoded:     testEncodeBase64("not valid json"),
			expectError: true,
			description: "不正なJSON文字列の場合はエラーになること",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := DecodeProjectCursor(tt.encoded)

			if tt.expectError {
				if err == nil {
					t.Errorf("%s: expected error but got nil", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("%s: unexpected error: %v", tt.description, err)
				}
				if tt.encoded == "" && decoded != nil {
					t.Errorf("%s: expected nil for empty string, got %v", tt.description, decoded)
				}
			}
		})
	}
}
