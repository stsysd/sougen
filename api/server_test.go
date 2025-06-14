// Package api はsougenのAPIサーバー実装を提供します。
package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stsysd/sougen/config"
	"github.com/stsysd/sougen/model"
)

// テスト用の定数
const testAPIToken = "test-api-token"

// テスト用の設定を生成するヘルパー関数
func newTestConfig() *config.Config {
	return &config.Config{
		DataDir:  "./testdata",
		Port:     "8080",
		APIToken: testAPIToken,
	}
}

// モックストア: テスト用のRecordStoreの実装
type MockRecordStore struct {
	records map[string]*model.Record
}

func NewMockRecordStore() *MockRecordStore {
	return &MockRecordStore{
		records: make(map[string]*model.Record),
	}
}

func (m *MockRecordStore) CreateRecord(ctx context.Context, record *model.Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	m.records[record.ID.String()] = record
	return nil
}

func (m *MockRecordStore) GetRecord(ctx context.Context, id uuid.UUID) (*model.Record, error) {
	record, exists := m.records[id.String()]
	if !exists {
		return nil, fmt.Errorf("record not found")
	}
	return record, nil
}

func (m *MockRecordStore) DeleteRecord(ctx context.Context, id uuid.UUID) error {
	_, exists := m.records[id.String()]
	if !exists {
		return fmt.Errorf("record not found")
	}
	delete(m.records, id.String())
	return nil
}

func (m *MockRecordStore) ListRecords(ctx context.Context, project string, from, to time.Time) ([]*model.Record, error) {
	var records []*model.Record

	for _, r := range m.records {
		if r.Project == project && !r.Timestamp.Before(from) && !r.Timestamp.After(to) {
			records = append(records, r)
		}
	}

	// Timestampの昇順にソート（SQLiteの実装と同様に）
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})

	return records, nil
}

func (m *MockRecordStore) Close() error {
	return nil
}

func (m *MockRecordStore) DeleteProject(ctx context.Context, projectName string) error {
	// 指定されたプロジェクトのレコードをすべて削除
	for id, record := range m.records {
		if record.Project == projectName {
			delete(m.records, id)
		}
	}

	return nil
}

func (m *MockRecordStore) DeleteRecordsUntil(ctx context.Context, project string, until time.Time) (int, error) {
	count := 0
	// 条件に一致するレコードをIDリストに収集
	var idsToDelete []string

	for id, record := range m.records {
		// プロジェクト指定がない、または一致するプロジェクトかつ指定日時より前
		if (project == "" || record.Project == project) && record.Timestamp.Before(until) {
			idsToDelete = append(idsToDelete, id)
		}
	}

	// 収集したIDのレコードを削除
	for _, id := range idsToDelete {
		delete(m.records, id)
		count++
	}

	return count, nil
}

func (m *MockRecordStore) GetProjectInfo(ctx context.Context, projectName string) (*model.ProjectInfo, error) {
	var recordCount int
	var totalValue int
	var firstRecordAt, lastRecordAt time.Time

	// 初期化
	hasRecords := false

	// プロジェクトのレコードを探索
	for _, r := range m.records {
		if r.Project == projectName {
			if !hasRecords {
				// 1件目のレコード
				firstRecordAt = r.Timestamp
				lastRecordAt = r.Timestamp
				hasRecords = true
			} else {
				// 日時の比較
				if r.Timestamp.Before(firstRecordAt) {
					firstRecordAt = r.Timestamp
				}
				if r.Timestamp.After(lastRecordAt) {
					lastRecordAt = r.Timestamp
				}
			}

			recordCount++
			totalValue += r.Value
		}
	}

	// レコードがない場合
	if !hasRecords {
		return nil, sql.ErrNoRows
	}

	// ProjectInfoオブジェクトの作成
	return model.NewProjectInfo(
		projectName,
		recordCount,
		totalValue,
		firstRecordAt,
		lastRecordAt,
	), nil
}

func TestCreateRecordEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "exercise"

	// テストリクエストデータ
	reqBody := map[string]interface{}{
		"timestamp": "2025-05-21T14:30:00Z",
		"value":     1,
	}
	reqBytes, _ := json.Marshal(reqBody)

	// リクエストの作成 - 新しいURLパスを使用
	req := httptest.NewRequest(http.MethodPost, "/api/v0/p/"+projectName+"/r", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認
	if w.Code != http.StatusCreated {
		t.Errorf("Expected status code %d, got %d", http.StatusCreated, w.Code)
		return
	}

	// レスポンスボディをデコード
	var responseRecord model.Record
	if err := json.NewDecoder(w.Body).Decode(&responseRecord); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	// レスポンスの各フィールドを確認
	t.Logf("Response record: %+v", responseRecord)
	t.Logf("Request body: %+v", reqBody)

	// Timestamp は日時型なので、フォーマット文字列を使った比較が必要
	expectedTimestamp := reqBody["timestamp"].(string)
	timestampStr := responseRecord.Timestamp.Format(time.RFC3339)
	if timestampStr != expectedTimestamp {
		t.Errorf("Expected Timestamp %s, got %s", expectedTimestamp, timestampStr)
	}

	// プロジェクト名の確認
	if responseRecord.Project != projectName {
		t.Errorf("Expected Project %s, got %s", projectName, responseRecord.Project)
	}

	expectedValue := reqBody["value"]
	t.Logf("Expected value (raw): %v, type: %T", reqBody["value"], reqBody["value"])
	t.Logf("Expected value (converted): %d, Response value: %d", expectedValue, responseRecord.Value)

	if responseRecord.Value != expectedValue {
		t.Errorf("Expected Value %d, got %d", expectedValue, responseRecord.Value)
	}
}

func TestCreateRecordWithoutTimestamp(t *testing.T) {
	// timestampフィールドが省略された場合に現在時刻が自動設定されることをテスト

	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "exercise"

	// timestampを省略したテストリクエストデータ
	reqBody := map[string]interface{}{
		"value": 1,
		// timestampは意図的に省略
	}
	reqBytes, _ := json.Marshal(reqBody)

	// リクエストの作成
	req := httptest.NewRequest(http.MethodPost, "/api/v0/p/"+projectName+"/r", bytes.NewBuffer(reqBytes))
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

	// timestampが現在時刻付近であることを確認（省略時に現在時刻が設定される）
	if responseRecord.Timestamp.Before(beforeTime) || responseRecord.Timestamp.After(afterTime) {
		t.Errorf("Expected Timestamp to be between %v and %v, got %v",
			beforeTime, afterTime, responseRecord.Timestamp)
	}

	t.Logf("自動設定されたTimestamp: %v", responseRecord.Timestamp)

	// プロジェクト名の確認
	if responseRecord.Project != projectName {
		t.Errorf("Expected Project %s, got %s", projectName, responseRecord.Project)
	}

	// 値の確認
	expectedValue := reqBody["value"]
	if responseRecord.Value != expectedValue {
		t.Errorf("Expected Value %d, got %d", expectedValue, responseRecord.Value)
	}
}

func TestCreateRecordWithoutValue(t *testing.T) {
	// valueフィールドが省略された場合にデフォルト値1が設定されることをテスト

	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "exercise"

	// valueを省略したテストリクエストデータ
	reqBody := map[string]interface{}{
		"timestamp": "2025-05-21T14:30:00Z",
		// valueは意図的に省略
	}
	reqBytes, _ := json.Marshal(reqBody)

	// リクエストの作成
	req := httptest.NewRequest(http.MethodPost, "/api/v0/p/"+projectName+"/r", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

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

	// Timestamp の確認
	expectedTimestamp := reqBody["timestamp"].(string)
	timestampStr := responseRecord.Timestamp.Format(time.RFC3339)
	if timestampStr != expectedTimestamp {
		t.Errorf("Expected Timestamp %s, got %s", expectedTimestamp, timestampStr)
	}

	// プロジェクト名の確認
	if responseRecord.Project != projectName {
		t.Errorf("Expected Project %s, got %s", projectName, responseRecord.Project)
	}

	// デフォルト値の確認
	expectedValue := 1 // 省略時のデフォルト値
	if responseRecord.Value != expectedValue {
		t.Errorf("Expected default Value %d, got %d", expectedValue, responseRecord.Value)
	}
}

func TestCreateRecordWithEmptyBody(t *testing.T) {
	// リクエストボディが空の場合のテスト

	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "exercise"

	// 空のリクエストボディ
	req := httptest.NewRequest(http.MethodPost, "/api/v0/p/"+projectName+"/r", nil)
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

	// timestampが現在時刻付近であることを確認（空リクエストの場合も現在時刻が設定される）
	if responseRecord.Timestamp.Before(beforeTime) || responseRecord.Timestamp.After(afterTime) {
		t.Errorf("Expected Timestamp to be between %v and %v, got %v",
			beforeTime, afterTime, responseRecord.Timestamp)
	}

	// プロジェクト名の確認
	if responseRecord.Project != projectName {
		t.Errorf("Expected Project %s, got %s", projectName, responseRecord.Project)
	}

	// デフォルト値の確認
	expectedValue := 1 // 空リクエストの場合のデフォルト値
	if responseRecord.Value != expectedValue {
		t.Errorf("Expected default Value %d, got %d", expectedValue, responseRecord.Value)
	}
}

func TestGetRecordEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()

	// プロジェクト名
	projectName := "exercise"

	// テスト用のレコードを作成
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(timestamp, projectName, 1)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(context.Background(), testRecord)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成 - 新しいURLパスを使用
	req := httptest.NewRequest(http.MethodGet, "/api/v0/p/"+projectName+"/r/"+testRecord.ID.String(), nil)
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	// レスポンスボディをデコード
	var responseRecord model.Record
	if err := json.NewDecoder(w.Body).Decode(&responseRecord); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	// レスポンスの各フィールドを確認
	t.Logf("Test record: %+v", testRecord)
	t.Logf("Response record: %+v", responseRecord)

	if responseRecord.ID != testRecord.ID {
		t.Errorf("Expected ID %s, got %s", testRecord.ID, responseRecord.ID)
	}

	if !responseRecord.Timestamp.Equal(testRecord.Timestamp) {
		t.Errorf("Expected Timestamp %v, got %v", testRecord.Timestamp, responseRecord.Timestamp)
	}

	if responseRecord.Project != testRecord.Project {
		t.Errorf("Expected Project %s, got %s", testRecord.Project, responseRecord.Project)
	}

	t.Logf("Expected value: %d, Response value: %d", testRecord.Value, responseRecord.Value)
	if responseRecord.Value != testRecord.Value {
		t.Errorf("Expected Value %d, got %d", testRecord.Value, responseRecord.Value)
	}
}

func TestGetNonExistentRecordEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "exercise"

	// 存在しないIDでリクエスト
	nonExistentID := uuid.New().String()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/p/"+projectName+"/r/"+nonExistentID, nil)
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認（404が期待される）
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestGetRecordFromWrongProject(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()

	// 正しいプロジェクト名と異なるプロジェクト名
	correctProject := "exercise"
	wrongProject := "diet"

	// テスト用のレコードを作成
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(timestamp, correctProject, 1)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(context.Background(), testRecord)

	server := NewServer(mockStore, newTestConfig())

	// 間違ったプロジェクト名でリクエスト
	req := httptest.NewRequest(http.MethodGet, "/api/v0/p/"+wrongProject+"/r/"+testRecord.ID.String(), nil)
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認（404が期待される）
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestDeleteRecordEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()

	// プロジェクト名
	projectName := "exercise"

	// テスト用のレコードを作成
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(timestamp, projectName, 1)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(context.Background(), testRecord)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成
	req := httptest.NewRequest(http.MethodDelete, "/api/v0/p/"+projectName+"/r/"+testRecord.ID.String(), nil)
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認（204が期待される）
	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status code %d, got %d", http.StatusNoContent, w.Code)
	}

	// レコードが実際に削除されたことを確認
	_, err = mockStore.GetRecord(context.Background(), testRecord.ID)
	if err == nil {
		t.Error("Record should have been deleted, but it still exists")
	}
}

func TestDeleteNonExistentRecordEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "exercise"

	// 存在しないIDでリクエスト
	nonExistentID := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/api/v0/p/"+projectName+"/r/"+nonExistentID, nil)
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認（404が期待される）
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestDeleteRecordFromWrongProject(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()

	// 正しいプロジェクト名と異なるプロジェクト名
	correctProject := "exercise"
	wrongProject := "diet"

	// テスト用のレコードを作成
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(timestamp, correctProject, 1)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(context.Background(), testRecord)

	server := NewServer(mockStore, newTestConfig())

	// 間違ったプロジェクト名でリクエスト
	req := httptest.NewRequest(http.MethodDelete, "/api/v0/p/"+wrongProject+"/r/"+testRecord.ID.String(), nil)
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認（404が期待される）
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, w.Code)
	}

	// レコードがまだ存在することを確認
	_, err = mockStore.GetRecord(context.Background(), testRecord.ID)
	if err != nil {
		t.Errorf("Record should still exist, but got error: %v", err)
	}
}

func TestGetGraphEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()

	// プロジェクト名
	projectName := "exercise"

	// テスト用のレコードを作成 - 同じ日付で複数レコード
	timestamp1 := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	record1, _ := model.NewRecord(timestamp1, projectName, 3)
	mockStore.CreateRecord(context.Background(), record1)

	// 同じ日の別の時間のレコード
	timestamp2 := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	record2, _ := model.NewRecord(timestamp2, projectName, 2)
	mockStore.CreateRecord(context.Background(), record2)

	// 別の日のレコード
	timestamp3 := time.Date(2025, 5, 22, 9, 0, 0, 0, time.UTC)
	record3, _ := model.NewRecord(timestamp3, projectName, 1)
	mockStore.CreateRecord(context.Background(), record3)

	// 別プロジェクトのレコード (グラフに含まれないはず)
	timestamp4 := time.Date(2025, 5, 22, 10, 0, 0, 0, time.UTC)
	record4, _ := model.NewRecord(timestamp4, "another_project", 5)
	mockStore.CreateRecord(context.Background(), record4)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成 (日付範囲指定あり)
	fromDate := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(2025, 5, 31, 23, 59, 59, 0, time.UTC)
	url := fmt.Sprintf("/p/%s/graph?from=%s&to=%s",
		projectName,
		fromDate.Format(time.RFC3339),
		toDate.Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	// Content-Typeの確認
	contentType := w.Header().Get("Content-Type")
	if contentType != "image/svg+xml" {
		t.Errorf("Expected Content-Type image/svg+xml, got %s", contentType)
	}

	// SVG形式のレスポンスが返されていることを確認
	responseBody := w.Body.String()
	if !strings.HasPrefix(responseBody, "<svg") {
		t.Errorf("Response is not in SVG format: %s", responseBody)
	}

	// 5月21日のデータポイント (value=5) と5月22日のデータポイント (value=1) が含まれていることを確認
	if !strings.Contains(responseBody, `data-date="2025-05-21"`) || !strings.Contains(responseBody, `data-count="5"`) {
		t.Errorf("SVG doesn't contain expected data point for 2025-05-21 with count 5")
	}
	if !strings.Contains(responseBody, `data-date="2025-05-22"`) || !strings.Contains(responseBody, `data-count="1"`) {
		t.Errorf("SVG doesn't contain expected data point for 2025-05-22 with count 1")
	}

	// データがない日（例：5月10日）も0の値で含まれていることを確認
	if !strings.Contains(responseBody, `data-date="2025-05-10"`) || !strings.Contains(responseBody, `data-count="0"`) {
		t.Errorf("SVG doesn't contain expected data point for 2025-05-10 with count 0")
	}
}

func TestGetGraphEndpointWithoutData(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()

	// プロジェクト名
	projectName := "empty_project"

	server := NewServer(mockStore, newTestConfig())

	// 明示的に日付範囲を指定する（データなしだが日付範囲は有効）
	fromDate := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(2025, 5, 31, 23, 59, 59, 0, time.UTC)
	url := fmt.Sprintf("/p/%s/graph?from=%s&to=%s",
		projectName,
		fromDate.Format(time.RFC3339),
		toDate.Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	// Content-Typeの確認
	contentType := w.Header().Get("Content-Type")
	if contentType != "image/svg+xml" {
		t.Errorf("Expected Content-Type image/svg+xml, got %s", contentType)
	}

	// SVG形式のレスポンスが返されることを確認（空ではなく、日付範囲のすべての日付が含まれるSVG）
	responseBody := w.Body.String()
	if !strings.HasPrefix(responseBody, "<svg") {
		t.Errorf("Response is not in SVG format: %s", responseBody)
	}

	// 5月の日付が含まれ、値が0であることを確認
	if !strings.Contains(responseBody, `data-date="2025-05-15"`) || !strings.Contains(responseBody, `data-count="0"`) {
		t.Errorf("SVG doesn't contain expected data point for mid-May with count 0")
	}
}

func TestListRecordsEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()

	// プロジェクト名
	projectName := "exercise"

	// テスト用のレコードを複数作成
	timestamp1 := time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC)
	record1, _ := model.NewRecord(timestamp1, projectName, 1)
	mockStore.CreateRecord(context.Background(), record1)

	timestamp2 := time.Date(2025, 5, 21, 12, 0, 0, 0, time.UTC)
	record2, _ := model.NewRecord(timestamp2, projectName, 2)
	mockStore.CreateRecord(context.Background(), record2)

	timestamp3 := time.Date(2025, 5, 22, 14, 0, 0, 0, time.UTC)
	record3, _ := model.NewRecord(timestamp3, projectName, 3)
	mockStore.CreateRecord(context.Background(), record3)

	// 別のプロジェクトのレコード（取得されないはず）
	timestamp4 := time.Date(2025, 5, 23, 16, 0, 0, 0, time.UTC)
	record4, _ := model.NewRecord(timestamp4, "another-project", 4)
	mockStore.CreateRecord(context.Background(), record4)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成 - 日付範囲を指定
	fromDate := time.Date(2025, 5, 15, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(2025, 5, 25, 23, 59, 59, 0, time.UTC)
	url := fmt.Sprintf("/api/v0/p/%s/r?from=%s&to=%s",
		projectName,
		fromDate.Format(time.RFC3339),
		toDate.Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", testAPIToken)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		t.Logf("Response: %s", w.Body.String())
		return
	}

	// レスポンスボディをデコード
	var responseRecords []*model.Record
	if err := json.NewDecoder(w.Body).Decode(&responseRecords); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	// 正しいレコード数が返されていることを確認
	if len(responseRecords) != 3 {
		t.Errorf("Expected 3 records, got %d", len(responseRecords))
	}

	// レコードの内容を確認
	t.Logf("Response records: %+v", responseRecords)

	// recordsMap は検証のためにIDベースでレコードにアクセスできるマップ
	recordsMap := make(map[string]*model.Record)
	for _, r := range responseRecords {
		recordsMap[r.ID.String()] = r
	}

	// 期待されるレコードが含まれているか確認
	if _, ok := recordsMap[record1.ID.String()]; !ok {
		t.Errorf("Record 1 missing from response")
	}
	if _, ok := recordsMap[record2.ID.String()]; !ok {
		t.Errorf("Record 2 missing from response")
	}
	if _, ok := recordsMap[record3.ID.String()]; !ok {
		t.Errorf("Record 3 missing from response")
	}

	// 別のプロジェクトのレコードが含まれていないことを確認
	if _, ok := recordsMap[record4.ID.String()]; ok {
		t.Errorf("Record 4 from another project should not be included")
	}
}

func TestListRecordsWithPagination(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()

	// プロジェクト名
	projectName := "pagination-test"

	// テスト用に10件のレコードを作成
	var allRecords []*model.Record
	baseTime := time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 10; i++ {
		recordTime := baseTime.Add(time.Duration(i) * time.Hour)
		record, _ := model.NewRecord(recordTime, projectName, i+1)
		mockStore.CreateRecord(context.Background(), record)
		allRecords = append(allRecords, record)
	}

	server := NewServer(mockStore, newTestConfig())

	// ケース1: limit=3, offset=0 で最初の3件を取得
	t.Run("First Page", func(t *testing.T) {
		url := fmt.Sprintf("/api/v0/p/%s/r?limit=3&offset=0", projectName)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIToken)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var records []*model.Record
		if err := json.NewDecoder(w.Body).Decode(&records); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}

		if len(records) != 3 {
			t.Errorf("Expected 3 records, got %d", len(records))
		}

		// 最初の3件のレコードが正しいか確認
		for i := 0; i < 3; i++ {
			if records[i].ID != allRecords[i].ID {
				t.Errorf("Record at index %d has incorrect ID", i)
			}
		}
	})

	// ケース2: limit=4, offset=3 で次の4件を取得
	t.Run("Second Page", func(t *testing.T) {
		url := fmt.Sprintf("/api/v0/p/%s/r?limit=4&offset=3", projectName)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIToken)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var records []*model.Record
		if err := json.NewDecoder(w.Body).Decode(&records); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}

		if len(records) != 4 {
			t.Errorf("Expected 4 records, got %d", len(records))
		}

		// オフセット3から4件のレコードが正しいか確認
		for i := 0; i < 4; i++ {
			if records[i].ID != allRecords[i+3].ID {
				t.Errorf("Record at index %d has incorrect ID", i)
			}
		}
	})

	// ケース3: offset が範囲外の場合、空配列が返される
	t.Run("Out of Range Offset", func(t *testing.T) {
		url := fmt.Sprintf("/api/v0/p/%s/r?limit=5&offset=20", projectName)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIToken)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var records []*model.Record
		if err := json.NewDecoder(w.Body).Decode(&records); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}

		if len(records) != 0 {
			t.Errorf("Expected 0 records, got %d", len(records))
		}
	})
}

func TestGetProject(t *testing.T) {
	store := NewMockRecordStore()

	// テスト用データの作成
	timestamp := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)

	// プロジェクト "test" にレコードを追加
	rec1, err := model.NewRecord(timestamp, "test", 10)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec1.ID.String()] = rec1

	rec2, err := model.NewRecord(timestamp.Add(1*time.Hour), "test", 15)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec2.ID.String()] = rec2

	// 別プロジェクトのレコードも追加
	rec3, err := model.NewRecord(timestamp, "another", 20)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec3.ID.String()] = rec3

	server := NewServer(store, newTestConfig())

	// テスト対象のエンドポイントを呼び出す
	req := httptest.NewRequest("GET", "/api/v0/p/test", nil)
	req.Header.Set("X-API-Key", testAPIToken)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	// レスポンスを検証
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %v", rec.Code)
	}

	var result model.ProjectInfo
	err = json.NewDecoder(rec.Body).Decode(&result)
	if err != nil {
		t.Fatal(err)
	}

	// プロジェクト情報を検証
	if result.Name != "test" {
		t.Errorf("Expected project name 'test', got '%s'", result.Name)
	}

	if result.RecordCount != 2 {
		t.Errorf("Expected 2 records, got %d", result.RecordCount)
	}

	if result.TotalValue != 25 {
		t.Errorf("Expected total value 25, got %d", result.TotalValue)
	}
}

func TestGetProjectNotFound(t *testing.T) {
	store := NewMockRecordStore()
	server := NewServer(store, newTestConfig())

	// テスト対象のエンドポイントを呼び出す - 存在しないプロジェクト名
	req := httptest.NewRequest("GET", "/api/v0/p/non-existent", nil)
	req.Header.Set("X-API-Key", testAPIToken)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	// レスポンスを検証
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status NotFound, got %v", rec.Code)
	}
}

// TestDeleteProject はプロジェクト削除エンドポイントのテスト
func TestDeleteProject(t *testing.T) {
	store := NewMockRecordStore()

	// テスト用のレコードを作成
	timestamp := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)

	// プロジェクト "test" にレコードを追加
	rec1, err := model.NewRecord(timestamp, "test", 10)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec1.ID.String()] = rec1

	rec2, err := model.NewRecord(timestamp.Add(1*time.Hour), "test", 15)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec2.ID.String()] = rec2

	// 別プロジェクトのレコードも追加
	rec3, err := model.NewRecord(timestamp, "another", 20)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec3.ID.String()] = rec3

	server := NewServer(store, newTestConfig())

	// テスト対象のエンドポイントを呼び出す
	req := httptest.NewRequest("DELETE", "/api/v0/p/test", nil)
	req.Header.Set("X-API-Key", testAPIToken)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	// レスポンスを検証
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status NoContent, got %v", rec.Code)
	}

	// プロジェクトが削除されたことを確認
	_, err = store.GetProjectInfo(context.Background(), "test")
	if err == nil {
		t.Errorf("Project should have been deleted, but still exists")
	}

	// 他のプロジェクトのレコードが削除されていないことを確認
	_, err = store.GetRecord(context.Background(), rec3.ID)
	if err != nil {
		t.Errorf("Record from other project should not be deleted")
	}
}

// TestDeleteNonExistentProject は存在しないプロジェクトの削除テスト
func TestDeleteNonExistentProject(t *testing.T) {
	store := NewMockRecordStore()
	server := NewServer(store, newTestConfig())

	// テスト対象のエンドポイントを呼び出す - 存在しないプロジェクト名
	req := httptest.NewRequest("DELETE", "/api/v0/p/non-existent", nil)
	req.Header.Set("X-API-Key", testAPIToken)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	// レスポンスを検証
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status NoContent, got %v", rec.Code)
	}
}

// TestHandleGetGraph は指定プロジェクトのヒートマップグラフ生成・返却をテストします。
func TestHandleGetGraph(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "test-project"

	// テスト用のレコードを作成
	now := time.Now()
	record1, err := model.NewRecord(now.AddDate(0, 0, -7), projectName, 5)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(context.Background(), record1)

	// リクエストの作成
	req := httptest.NewRequest(http.MethodGet, "/p/"+projectName+"/graph", nil)
	req.Header.Set("X-API-Key", testAPIToken)
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		return
	}

	// SVG形式のレスポンスが返されたか確認
	contentType := w.Header().Get("Content-Type")
	if contentType != "image/svg+xml" {
		t.Errorf("Expected Content-Type 'image/svg+xml', got '%s'", contentType)
	}

	// レスポンスボディがSVG形式であるか簡易チェック
	body := w.Body.String()
	if !strings.Contains(body, "<svg") {
		t.Errorf("Response does not contain SVG content")
	}
}

// TestHandleGetGraphWithTrackParam はtrackパラメータを使ったアクセスカウンター機能をテストします。
func TestHandleGetGraphWithTrackParam(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "counter-test"

	// trackパラメータ付きのリクエストを作成
	req := httptest.NewRequest(http.MethodGet, "/p/"+projectName+"/graph?track", nil)
	req.Header.Set("X-API-Key", testAPIToken)
	w := httptest.NewRecorder()

	// 実行前のレコード数を記録
	countBefore := len(mockStore.records)

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		return
	}

	// レコードが1つ追加されたか確認
	countAfter := len(mockStore.records)
	if countAfter != countBefore+1 {
		t.Errorf("Expected %d records after tracking, got %d", countBefore+1, countAfter)
	}

	// 追加されたレコードの内容を確認
	var foundRecord *model.Record
	for _, record := range mockStore.records {
		if record.Project == projectName {
			foundRecord = record
			break
		}
	}

	if foundRecord == nil {
		t.Errorf("No record created for project %s", projectName)
		return
	}

	// レコードの値が1であることを確認
	if foundRecord.Value != 1 {
		t.Errorf("Expected record value to be 1, got %d", foundRecord.Value)
	}

	// レコードの日時が現在時刻に近いことを確認（前後5分以内）
	now := time.Now()
	fiveMinutesAgo := now.Add(-5 * time.Minute)
	fiveMinutesLater := now.Add(5 * time.Minute)

	if foundRecord.Timestamp.Before(fiveMinutesAgo) || foundRecord.Timestamp.After(fiveMinutesLater) {
		t.Errorf("Record timestamp is not within expected range: %v", foundRecord.Timestamp)
	}
}

// TestHandleGetGraphWithoutTrackParam はtrackパラメータなしの場合にレコードが作成されないことをテストします。
func TestHandleGetGraphWithoutTrackParam(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "no-counter-test"

	// trackパラメータなしのリクエストを作成
	req := httptest.NewRequest(http.MethodGet, "/p/"+projectName+"/graph", nil)
	req.Header.Set("X-API-Key", testAPIToken)
	w := httptest.NewRecorder()

	// 実行前のレコード数を記録
	countBefore := len(mockStore.records)

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レコード数が変わっていないことを確認
	countAfter := len(mockStore.records)
	if countAfter != countBefore {
		t.Errorf("Expected no new records, but got %d records (was: %d)", countAfter, countBefore)
	}
}

// TestHandleGetGraphSVGExtension はSVG拡張子付きのURLでグラフを取得できることをテストします。
func TestHandleGetGraphSVGExtension(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "svg-ext-test"

	// .svg拡張子付きのリクエストを作成
	req := httptest.NewRequest(http.MethodGet, "/p/"+projectName+"/graph.svg", nil)
	req.Header.Set("X-API-Key", testAPIToken)
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		return
	}

	// SVG形式のレスポンスが返されたか確認
	contentType := w.Header().Get("Content-Type")
	if contentType != "image/svg+xml" {
		t.Errorf("Expected Content-Type 'image/svg+xml', got '%s'", contentType)
	}
}

// TestBulkDeleteRecords はレコード一括削除APIのテスト
func TestBulkDeleteRecords(t *testing.T) {
	// プロジェクト名
	project1 := "project1"
	project2 := "project2"

	// テスト用の基準日時
	baseTime := time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC)

	// テストケース
	tests := []struct {
		name           string
		project        string
		until          time.Time
		expectedStatus int
		expectedCount  int
	}{
		{
			name:           "特定プロジェクトの一部レコード削除",
			project:        project1,
			until:          baseTime.AddDate(0, 0, 3), // 3日目までのレコードを削除
			expectedStatus: http.StatusOK,
			expectedCount:  3, // 3日分のレコードが削除される
		},
		{
			name:           "特定プロジェクトの全レコード削除",
			project:        project2,
			until:          baseTime.AddDate(1, 0, 0), // 十分後の日付
			expectedStatus: http.StatusOK,
			expectedCount:  3, // project2の全レコードが削除される
		},
		{
			name:           "全プロジェクトの一部レコード削除",
			project:        "",                        // プロジェクト指定なし
			until:          baseTime.AddDate(0, 0, 2), // 2日目までのレコードを削除
			expectedStatus: http.StatusOK,
			expectedCount:  4, // 両方のプロジェクトから2日分ずつ
		},
		{
			name:           "該当レコードなし",
			project:        project1,
			until:          baseTime.AddDate(0, 0, -1), // ベース時間より前（該当レコードなし）
			expectedStatus: http.StatusOK,
			expectedCount:  0, // 該当するレコードがない
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 各テストケースごとに新しいモックストアを作成
			mockStore := NewMockRecordStore()

			// project1のレコードを作成（5件）
			for i := 0; i < 5; i++ {
				recordTime := baseTime.AddDate(0, 0, i) // 1日ずつずらす
				record, _ := model.NewRecord(recordTime, project1, i+1)
				mockStore.CreateRecord(context.Background(), record)
			}

			// project2のレコードを作成（3件）
			for i := 0; i < 3; i++ {
				recordTime := baseTime.AddDate(0, 0, i) // 1日ずつずらす
				record, _ := model.NewRecord(recordTime, project2, i+10)
				mockStore.CreateRecord(context.Background(), record)
			}

			// 各テストケースごとに新しいサーバーも作成
			server := NewServer(mockStore, newTestConfig())

			// リクエストURLの組み立て
			url := "/api/v0/r?until=" + tc.until.Format(time.RFC3339)
			if tc.project != "" {
				url += "&project=" + tc.project
			}

			req := httptest.NewRequest(http.MethodDelete, url, nil)
			req.Header.Set("X-API-Key", testAPIToken)
			w := httptest.NewRecorder()

			// リクエスト実行
			server.ServeHTTP(w, req)

			// ステータスコードの確認
			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
				return
			}

			// レスポンスのパース
			var response map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			// 削除件数の確認
			deletedCount, ok := response["deleted_count"].(float64)
			if !ok {
				t.Fatalf("Expected deleted_count in response, got: %v", response)
			}

			if int(deletedCount) != tc.expectedCount {
				t.Errorf("Expected %d deleted records, got %d", tc.expectedCount, int(deletedCount))
			}
		})
	}
}

// TestBulkDeleteRecordsWithInvalidParams はレコード一括削除APIの不正パラメータのテスト
func TestBulkDeleteRecordsWithInvalidParams(t *testing.T) {
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// テストケース
	tests := []struct {
		name           string
		url            string
		expectedStatus int
	}{
		{
			name:           "until パラメータ不足",
			url:            "/api/v0/r",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "不正な until フォーマット",
			url:            "/api/v0/r?until=invalid-date",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, tc.url, nil)
			req.Header.Set("X-API-Key", testAPIToken)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
			}
		})
	}
}
