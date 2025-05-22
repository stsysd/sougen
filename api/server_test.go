// Package api はsougenのAPIサーバー実装を提供します。
package api

import (
	"bytes"
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

func (m *MockRecordStore) CreateRecord(record *model.Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	m.records[record.ID.String()] = record
	return nil
}

func (m *MockRecordStore) GetRecord(id uuid.UUID) (*model.Record, error) {
	record, exists := m.records[id.String()]
	if !exists {
		return nil, fmt.Errorf("record not found")
	}
	return record, nil
}

func (m *MockRecordStore) DeleteRecord(id uuid.UUID) error {
	_, exists := m.records[id.String()]
	if !exists {
		return fmt.Errorf("record not found")
	}
	delete(m.records, id.String())
	return nil
}

func (m *MockRecordStore) ListRecords(project string, from, to time.Time) ([]*model.Record, error) {
	var records []*model.Record

	for _, r := range m.records {
		if r.Project == project && !r.DoneAt.Before(from) && !r.DoneAt.After(to) {
			records = append(records, r)
		}
	}

	// DoneAtの昇順にソート（SQLiteの実装と同様に）
	sort.Slice(records, func(i, j int) bool {
		return records[i].DoneAt.Before(records[j].DoneAt)
	})

	return records, nil
}

func (m *MockRecordStore) Close() error {
	return nil
}

func (m *MockRecordStore) DeleteProject(projectName string) error {
	// 指定されたプロジェクトのレコードをすべて削除
	for id, record := range m.records {
		if record.Project == projectName {
			delete(m.records, id)
		}
	}

	return nil
}

func (m *MockRecordStore) GetProjectInfo(projectName string) (*model.ProjectInfo, error) {
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
				firstRecordAt = r.DoneAt
				lastRecordAt = r.DoneAt
				hasRecords = true
			} else {
				// 日時の比較
				if r.DoneAt.Before(firstRecordAt) {
					firstRecordAt = r.DoneAt
				}
				if r.DoneAt.After(lastRecordAt) {
					lastRecordAt = r.DoneAt
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
		"done_at": "2025-05-21T14:30:00Z",
		"value":   1,
	}
	reqBytes, _ := json.Marshal(reqBody)

	// リクエストの作成 - 新しいURLパスを使用
	req := httptest.NewRequest(http.MethodPost, "/v0/p/"+projectName+"/r", bytes.NewBuffer(reqBytes))
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

	// DoneAt は日時型なので、フォーマット文字列を使った比較が必要
	expectedDoneAt := reqBody["done_at"].(string)
	doneAtStr := responseRecord.DoneAt.Format(time.RFC3339)
	if doneAtStr != expectedDoneAt {
		t.Errorf("Expected DoneAt %s, got %s", expectedDoneAt, doneAtStr)
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

func TestCreateRecordWithoutDoneAt(t *testing.T) {
	// done_atフィールドが省略された場合に現在時刻が自動設定されることをテスト

	// モックストアの準備
	mockStore := NewMockRecordStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "exercise"

	// done_atを省略したテストリクエストデータ
	reqBody := map[string]interface{}{
		"value": 1,
		// done_atは意図的に省略
	}
	reqBytes, _ := json.Marshal(reqBody)

	// リクエストの作成
	req := httptest.NewRequest(http.MethodPost, "/v0/p/"+projectName+"/r", bytes.NewBuffer(reqBytes))
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

	// doneAtが現在時刻付近であることを確認（省略時に現在時刻が設定される）
	if responseRecord.DoneAt.Before(beforeTime) || responseRecord.DoneAt.After(afterTime) {
		t.Errorf("Expected DoneAt to be between %v and %v, got %v",
			beforeTime, afterTime, responseRecord.DoneAt)
	}

	t.Logf("自動設定されたDoneAt: %v", responseRecord.DoneAt)

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

func TestGetRecordEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockRecordStore()

	// プロジェクト名
	projectName := "exercise"

	// テスト用のレコードを作成
	doneAt := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(doneAt, projectName, 1)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(testRecord)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成 - 新しいURLパスを使用
	req := httptest.NewRequest(http.MethodGet, "/v0/p/"+projectName+"/r/"+testRecord.ID.String(), nil)
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

	if !responseRecord.DoneAt.Equal(testRecord.DoneAt) {
		t.Errorf("Expected DoneAt %v, got %v", testRecord.DoneAt, responseRecord.DoneAt)
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
	req := httptest.NewRequest(http.MethodGet, "/v0/p/"+projectName+"/r/"+nonExistentID, nil)
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
	doneAt := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(doneAt, correctProject, 1)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(testRecord)

	server := NewServer(mockStore, newTestConfig())

	// 間違ったプロジェクト名でリクエスト
	req := httptest.NewRequest(http.MethodGet, "/v0/p/"+wrongProject+"/r/"+testRecord.ID.String(), nil)
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
	doneAt := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(doneAt, projectName, 1)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(testRecord)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成
	req := httptest.NewRequest(http.MethodDelete, "/v0/p/"+projectName+"/r/"+testRecord.ID.String(), nil)
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
	_, err = mockStore.GetRecord(testRecord.ID)
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
	req := httptest.NewRequest(http.MethodDelete, "/v0/p/"+projectName+"/r/"+nonExistentID, nil)
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
	doneAt := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(doneAt, correctProject, 1)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(testRecord)

	server := NewServer(mockStore, newTestConfig())

	// 間違ったプロジェクト名でリクエスト
	req := httptest.NewRequest(http.MethodDelete, "/v0/p/"+wrongProject+"/r/"+testRecord.ID.String(), nil)
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
	_, err = mockStore.GetRecord(testRecord.ID)
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
	doneAt1 := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	record1, _ := model.NewRecord(doneAt1, projectName, 3)
	mockStore.CreateRecord(record1)

	// 同じ日の別の時間のレコード
	doneAt2 := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	record2, _ := model.NewRecord(doneAt2, projectName, 2)
	mockStore.CreateRecord(record2)

	// 別の日のレコード
	doneAt3 := time.Date(2025, 5, 22, 9, 0, 0, 0, time.UTC)
	record3, _ := model.NewRecord(doneAt3, projectName, 1)
	mockStore.CreateRecord(record3)

	// 別プロジェクトのレコード (グラフに含まれないはず)
	doneAt4 := time.Date(2025, 5, 22, 10, 0, 0, 0, time.UTC)
	record4, _ := model.NewRecord(doneAt4, "another_project", 5)
	mockStore.CreateRecord(record4)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成 (日付範囲指定あり)
	fromDate := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(2025, 5, 31, 23, 59, 59, 0, time.UTC)
	url := fmt.Sprintf("/v0/p/%s/graph?from=%s&to=%s",
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
	url := fmt.Sprintf("/v0/p/%s/graph?from=%s&to=%s",
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
	doneAt1 := time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC)
	record1, _ := model.NewRecord(doneAt1, projectName, 1)
	mockStore.CreateRecord(record1)

	doneAt2 := time.Date(2025, 5, 21, 12, 0, 0, 0, time.UTC)
	record2, _ := model.NewRecord(doneAt2, projectName, 2)
	mockStore.CreateRecord(record2)

	doneAt3 := time.Date(2025, 5, 22, 14, 0, 0, 0, time.UTC)
	record3, _ := model.NewRecord(doneAt3, projectName, 3)
	mockStore.CreateRecord(record3)

	// 別のプロジェクトのレコード（取得されないはず）
	doneAt4 := time.Date(2025, 5, 23, 16, 0, 0, 0, time.UTC)
	record4, _ := model.NewRecord(doneAt4, "another-project", 4)
	mockStore.CreateRecord(record4)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成 - 日付範囲を指定
	fromDate := time.Date(2025, 5, 15, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(2025, 5, 25, 23, 59, 59, 0, time.UTC)
	url := fmt.Sprintf("/v0/p/%s/r?from=%s&to=%s",
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
		mockStore.CreateRecord(record)
		allRecords = append(allRecords, record)
	}

	server := NewServer(mockStore, newTestConfig())

	// ケース1: limit=3, offset=0 で最初の3件を取得
	t.Run("First Page", func(t *testing.T) {
		url := fmt.Sprintf("/v0/p/%s/r?limit=3&offset=0", projectName)
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
		url := fmt.Sprintf("/v0/p/%s/r?limit=4&offset=3", projectName)
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
		url := fmt.Sprintf("/v0/p/%s/r?limit=5&offset=20", projectName)
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
	doneAt := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)

	// プロジェクト "test" にレコードを追加
	rec1, err := model.NewRecord(doneAt, "test", 10)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec1.ID.String()] = rec1

	rec2, err := model.NewRecord(doneAt.Add(1*time.Hour), "test", 15)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec2.ID.String()] = rec2

	// 別プロジェクトのレコードも追加
	rec3, err := model.NewRecord(doneAt, "another", 20)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec3.ID.String()] = rec3

	server := NewServer(store, newTestConfig())

	// テスト対象のエンドポイントを呼び出す
	req := httptest.NewRequest("GET", "/v0/p/test", nil)
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
	req := httptest.NewRequest("GET", "/v0/p/non-existent", nil)
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
	doneAt := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)

	// プロジェクト "test" にレコードを追加
	rec1, err := model.NewRecord(doneAt, "test", 10)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec1.ID.String()] = rec1

	rec2, err := model.NewRecord(doneAt.Add(1*time.Hour), "test", 15)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec2.ID.String()] = rec2

	// 別プロジェクトのレコードも追加
	rec3, err := model.NewRecord(doneAt, "another", 20)
	if err != nil {
		t.Fatal(err)
	}
	store.records[rec3.ID.String()] = rec3

	server := NewServer(store, newTestConfig())

	// テスト対象のエンドポイントを呼び出す
	req := httptest.NewRequest("DELETE", "/v0/p/test", nil)
	req.Header.Set("X-API-Key", testAPIToken)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	// レスポンスを検証
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status NoContent, got %v", rec.Code)
	}

	// プロジェクトが削除されたことを確認
	_, err = store.GetProjectInfo("test")
	if err == nil {
		t.Errorf("Project should have been deleted, but still exists")
	}

	// 他のプロジェクトのレコードが削除されていないことを確認
	_, err = store.GetRecord(rec3.ID)
	if err != nil {
		t.Errorf("Record from other project should not be deleted")
	}
}

// TestDeleteNonExistentProject は存在しないプロジェクトの削除テスト
func TestDeleteNonExistentProject(t *testing.T) {
	store := NewMockRecordStore()
	server := NewServer(store, newTestConfig())

	// テスト対象のエンドポイントを呼び出す - 存在しないプロジェクト名
	req := httptest.NewRequest("DELETE", "/v0/p/non-existent", nil)
	req.Header.Set("X-API-Key", testAPIToken)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	// レスポンスを検証
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status NoContent, got %v", rec.Code)
	}
}
