package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stsysd/sougen/model"
)

// TestCreateRecordWithTemplate はテンプレートパラメータを使ったレコード作成をテストします。
func TestCreateRecordWithTemplate(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "template-test"

	// テストケース
	tests := []struct {
		name           string
		template       string
		requestBody    string
		expectedStatus int
		expectedValue  int
		expectedTimestamp string
	}{
		{
			name:     "GitHub webhook style template",
			template: `{"timestamp": "{{.pushed_at}}", "value": {{len .commits}}}`,
			requestBody: `{
				"pushed_at": "2025-01-01T12:00:00Z",
				"commits": [{"id":"1"}, {"id":"2"}, {"id":"3"}]
			}`,
			expectedStatus: http.StatusCreated,
			expectedValue:  3,
			expectedTimestamp: "2025-01-01T12:00:00Z",
		},
		{
			name:     "Simple counter template",
			template: `{"value": {{.count}}, "timestamp": "{{.timestamp}}"}`,
			requestBody: `{
				"count": 5,
				"timestamp": "2025-02-01T15:30:00Z"
			}`,
			expectedStatus: http.StatusCreated,
			expectedValue:  5,
			expectedTimestamp: "2025-02-01T15:30:00Z",
		},
		{
			name:     "Default value template",
			template: `{"value": {{if .value}}{{.value}}{{else}}1{{end}}}`,
			requestBody: `{
				"other_field": "test"
			}`,
			expectedStatus: http.StatusCreated,
			expectedValue:  1,
		},
		{
			name:     "Complex nested data template",
			template: `{"timestamp": "{{.event.timestamp}}", "value": {{.event.data.count}}}`,
			requestBody: `{
				"event": {
					"timestamp": "2025-03-01T10:00:00Z",
					"data": {
						"count": 7
					}
				}
			}`,
			expectedStatus: http.StatusCreated,
			expectedValue:  7,
			expectedTimestamp: "2025-03-01T10:00:00Z",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// リクエストの作成
			baseURL := fmt.Sprintf("/api/v0/p/%s/r", projectName)
			params := url.Values{}
			params.Set("template", tc.template)
			fullURL := baseURL + "?" + params.Encode()
			req := httptest.NewRequest(http.MethodPost, fullURL, strings.NewReader(tc.requestBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", testAPIToken)

			// レスポンスレコーダーの作成
			w := httptest.NewRecorder()

			// ハンドラの実行
			server.ServeHTTP(w, req)

			// レスポンスのステータスコードを確認
			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tc.expectedStatus, w.Code)
				t.Logf("Response body: %s", w.Body.String())
				return
			}

			// 成功の場合、レスポンスボディをデコード
			if tc.expectedStatus == http.StatusCreated {
				var responseRecord model.Record
				if err := json.NewDecoder(w.Body).Decode(&responseRecord); err != nil {
					t.Fatalf("Failed to decode response body: %v", err)
				}

				// 値の確認
				if responseRecord.Value != tc.expectedValue {
					t.Errorf("Expected Value %d, got %d", tc.expectedValue, responseRecord.Value)
				}

				// プロジェクト名の確認
				if responseRecord.Project != projectName {
					t.Errorf("Expected Project %s, got %s", projectName, responseRecord.Project)
				}

				// Timestampの確認（指定されている場合）
				if tc.expectedTimestamp != "" {
					timestampStr := responseRecord.Timestamp.Format(time.RFC3339)
					if timestampStr != tc.expectedTimestamp {
						t.Errorf("Expected Timestamp %s, got %s", tc.expectedTimestamp, timestampStr)
					}
				}
			}
		})
	}
}

// TestCreateRecordWithInvalidTemplate は無効なテンプレートのテストです。
func TestCreateRecordWithInvalidTemplate(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "invalid-template-test"

	// テストケース
	tests := []struct {
		name           string
		template       string
		requestBody    string
		expectedStatus int
	}{
		{
			name:           "Invalid template syntax",
			template:       `{"value": {{.invalid_syntax}`,
			requestBody:    `{"test": "data"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Template execution error",
			template:       `{"value": {{.nonexistent.field}}}`,
			requestBody:    `{"test": "data"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid JSON output from template",
			template:       `{"value": invalid_json}`,
			requestBody:    `{"test": "data"}`,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// リクエストの作成
			baseURL := fmt.Sprintf("/api/v0/p/%s/r", projectName)
			params := url.Values{}
			params.Set("template", tc.template)
			fullURL := baseURL + "?" + params.Encode()
			req := httptest.NewRequest(http.MethodPost, fullURL, strings.NewReader(tc.requestBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", testAPIToken)

			// レスポンスレコーダーの作成
			w := httptest.NewRecorder()

			// ハンドラの実行
			server.ServeHTTP(w, req)

			// レスポンスのステータスコードを確認
			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tc.expectedStatus, w.Code)
				t.Logf("Response body: %s", w.Body.String())
			}
		})
	}
}

// TestCreateRecordWithTemplateNoBody はテンプレートパラメータがある場合でもボディがない場合のテストです。
func TestCreateRecordWithTemplateNoBody(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "template-no-body-test"

	// テンプレートパラメータ付きで空のボディのリクエスト
	template := `{"value": 1}`
	baseURL := fmt.Sprintf("/api/v0/p/%s/r", projectName)
	params := url.Values{}
	params.Set("template", template)
	fullURL := baseURL + "?" + params.Encode()
	req := httptest.NewRequest(http.MethodPost, fullURL, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// テスト時刻を記録
	beforeTime := time.Now()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// テスト終了時刻を記録
	afterTime := time.Now()

	// レスポンスのステータスコードを確認
	if w.Code != http.StatusCreated {
		t.Errorf("Expected status code %d, got %d", http.StatusCreated, w.Code)
		t.Logf("Response body: %s", w.Body.String())
		return
	}

	// レスポンスボディをデコード
	var responseRecord model.Record
	if err := json.NewDecoder(w.Body).Decode(&responseRecord); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	// 値の確認（テンプレートで指定した値）
	if responseRecord.Value != 1 {
		t.Errorf("Expected Value 1, got %d", responseRecord.Value)
	}

	// プロジェクト名の確認
	if responseRecord.Project != projectName {
		t.Errorf("Expected Project %s, got %s", projectName, responseRecord.Project)
	}

	// Timestampが現在時刻付近であることを確認（テンプレートで指定されていないため現在時刻が設定される）
	if responseRecord.Timestamp.Before(beforeTime) || responseRecord.Timestamp.After(afterTime) {
		t.Errorf("Expected Timestamp to be between %v and %v, got %v",
			beforeTime, afterTime, responseRecord.Timestamp)
	}
}

// TestTransformRequestBody はtransformRequestBody関数の直接テストです。
func TestTransformRequestBody(t *testing.T) {
	server := &Server{}

	tests := []struct {
		name           string
		template       string
		inputJSON      string
		expectedJSON   string
		expectError    bool
	}{
		{
			name:         "Simple field extraction",
			template:     `{"value": {{.count}}}`,
			inputJSON:    `{"count": 5}`,
			expectedJSON: `{"value": 5}`,
			expectError:  false,
		},
		{
			name:         "String field extraction",
			template:     `{"timestamp": "{{.timestamp}}"}`,
			inputJSON:    `{"timestamp": "2025-01-01T12:00:00Z"}`,
			expectedJSON: `{"timestamp": "2025-01-01T12:00:00Z"}`,
			expectError:  false,
		},
		{
			name:         "Array length calculation",
			template:     `{"value": {{len .items}}}`,
			inputJSON:    `{"items": [1, 2, 3, 4]}`,
			expectedJSON: `{"value": 4}`,
			expectError:  false,
		},
		{
			name:         "Nested field access",
			template:     `{"value": {{.data.count}}}`,
			inputJSON:    `{"data": {"count": 10}}`,
			expectedJSON: `{"value": 10}`,
			expectError:  false,
		},
		{
			name:        "Invalid template syntax",
			template:    `{"value": {{.invalid}`,
			inputJSON:   `{"test": 1}`,
			expectError: true,
		},
		{
			name:        "Invalid JSON input",
			template:    `{"value": {{.count}}}`,
			inputJSON:   `{invalid json}`,
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := server.transformRequestBody(bytes.NewReader([]byte(tc.inputJSON)), tc.template)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// JSONの内容を比較するため、パースして比較
			var expectedMap, resultMap map[string]interface{}
			if err := json.Unmarshal([]byte(tc.expectedJSON), &expectedMap); err != nil {
				t.Fatalf("Failed to parse expected JSON: %v", err)
			}
			if err := json.Unmarshal([]byte(result), &resultMap); err != nil {
				t.Fatalf("Failed to parse result JSON: %v", err)
			}

			// 値の比較
			for key, expectedValue := range expectedMap {
				if resultValue, ok := resultMap[key]; !ok {
					t.Errorf("Missing key %s in result", key)
				} else if resultValue != expectedValue {
					t.Errorf("Expected %s=%v, got %v", key, expectedValue, resultValue)
				}
			}
		})
	}
}
