// Package api はsougenのAPIサーバー実装を提供します。
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stsysd/sougen/config"
	"github.com/stsysd/sougen/model"
	"github.com/stsysd/sougen/store"
)

// テスト用の定数
const testAPIKey = "test-api-key"

// テスト用の設定を生成するヘルパー関数
func newTestConfig() *config.Config {
	return &config.Config{
		DataDir: "./testdata",
		Port:    "8080",
		APIKey:  testAPIKey,
	}
}

// モックストア: テスト用のRecordStoreの実装
type MockStore struct {
	records  map[int64]*model.Record
	projects map[int64]*model.Project
}

func NewMockStore() *MockStore {
	return &MockStore{
		records:  make(map[int64]*model.Record),
		projects: make(map[int64]*model.Project),
	}
}

func (m *MockStore) CreateRecord(ctx context.Context, record *model.Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	// IDを自動生成
	record.ID = model.NewHexID(int64(len(m.records) + 1))
	m.records[record.ID.ToInt64()] = record
	return nil
}

func (m *MockStore) GetRecord(ctx context.Context, id model.HexID) (*model.Record, error) {
	record, exists := m.records[id.ToInt64()]
	if !exists {
		return nil, fmt.Errorf("record not found")
	}
	return record, nil
}

func (m *MockStore) UpdateRecord(ctx context.Context, record *model.Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	_, exists := m.records[record.ID.ToInt64()]
	if !exists {
		return fmt.Errorf("record not found")
	}
	m.records[record.ID.ToInt64()] = record
	return nil
}

func (m *MockStore) DeleteRecord(ctx context.Context, id model.HexID) error {
	_, exists := m.records[id.ToInt64()]
	if !exists {
		return fmt.Errorf("record not found")
	}
	delete(m.records, id.ToInt64())
	return nil
}

func (m *MockStore) ListRecords(ctx context.Context, params *store.ListRecordsParams) ([]*model.Record, error) {
	var records []*model.Record

	for _, r := range m.records {
		// プロジェクトフィルタ
		if params.ProjectID.IsValid() && !r.ProjectID.Equals(params.ProjectID) {
			continue
		}

		// 日付範囲フィルタ（From/Toがゼロ値でない場合のみ）
		if !params.From.IsZero() && r.Timestamp.Before(params.From) {
			continue
		}
		if !params.To.IsZero() && r.Timestamp.After(params.To) {
			continue
		}

		// タグフィルタ
		if len(params.Tags) > 0 {
			tagMatch := false
			for _, filterTag := range params.Tags {
				if slices.Contains(r.Tags, filterTag) {
					tagMatch = true
					break
				}
			}
			if !tagMatch {
				continue
			}
		}

		records = append(records, r)
	}

	// Timestampの降順にソート（SQLiteの実装と同様に）
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.After(records[j].Timestamp)
	})

	// ページネーションを適用（cursor-based）
	limit := params.Pagination.Limit()

	// カーソルが指定されている場合、その位置を見つける
	startIndex := 0
	if params.CursorTimestamp != nil && params.CursorID != nil {
		for i, r := range records {
			// timestamp と ID の組み合わせで位置を特定
			if r.Timestamp.Equal(*params.CursorTimestamp) && r.ID.Equals(*params.CursorID) {
				startIndex = i + 1 // カーソルの次から開始
				break
			}
		}
	}

	if startIndex >= len(records) {
		return []*model.Record{}, nil
	}
	endIndex := min(startIndex+limit, len(records))

	return records[startIndex:endIndex], nil
}

func (m *MockStore) ListAllRecords(ctx context.Context, params *store.ListAllRecordsParams) iter.Seq2[*model.Record, error] {
	return func(yield func(*model.Record, error) bool) {
		var records []*model.Record

		for _, r := range m.records {
			if !r.ProjectID.Equals(params.ProjectID) || r.Timestamp.Before(params.From) || r.Timestamp.After(params.To) {
				continue
			}

			// タグフィルタ
			if len(params.Tags) > 0 {
				tagMatch := false
				for _, filterTag := range params.Tags {
					for _, recordTag := range r.Tags {
						if recordTag == filterTag {
							tagMatch = true
							break
						}
					}
					if tagMatch {
						break
					}
				}
				if !tagMatch {
					continue
				}
			}

			records = append(records, r)
		}

		// Timestampの降順にソート（SQLiteの実装と同様に）
		sort.Slice(records, func(i, j int) bool {
			return records[i].Timestamp.After(records[j].Timestamp)
		})

		// すべてのレコードをyield
		for _, record := range records {
			if !yield(record, nil) {
				return
			}
		}
	}
}

func (m *MockStore) Close() error {
	return nil
}

func (m *MockStore) DeleteProject(ctx context.Context, projectID model.HexID) error {
	// 指定されたプロジェクトのレコードをすべて削除
	for id, record := range m.records {
		if record.ProjectID.Equals(projectID) {
			delete(m.records, id)
		}
	}

	return nil
}

func (m *MockStore) DeleteRecordsUntil(ctx context.Context, projectID model.HexID, until time.Time) (int, error) {
	count := 0
	// 条件に一致するレコードをIDリストに収集
	var idsToDelete []int64

	for id, record := range m.records {
		// プロジェクト指定がない、または一致するプロジェクトかつ指定日時より前
		if (!projectID.IsValid() || record.ProjectID.Equals(projectID)) && record.Timestamp.Before(until) {
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

func (m *MockStore) CreateProject(ctx context.Context, project *model.Project) error {
	// IDを自動生成
	project.ID = model.NewHexID(int64(len(m.projects) + 1))
	m.projects[project.ID.ToInt64()] = project
	return nil
}

func (m *MockStore) GetProject(ctx context.Context, id model.HexID) (*model.Project, error) {
	project, exists := m.projects[id.ToInt64()]
	if !exists {
		return nil, errors.New("project not found")
	}
	return project, nil
}

func (m *MockStore) UpdateProject(ctx context.Context, project *model.Project) error {
	if _, exists := m.projects[project.ID.ToInt64()]; !exists {
		return errors.New("project not found")
	}
	m.projects[project.ID.ToInt64()] = project
	return nil
}

func (m *MockStore) DeleteProjectEntity(ctx context.Context, id int64) error {
	if _, exists := m.projects[id]; !exists {
		return errors.New("project not found")
	}
	delete(m.projects, id)
	return nil
}

func (m *MockStore) ListProjects(ctx context.Context, params *store.ListProjectsParams) ([]*model.Project, error) {
	var projects []*model.Project
	for _, project := range m.projects {
		projects = append(projects, project)
	}

	// updated_atの降順、nameの昇順にソート（SQLiteの実装と同様に）
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].UpdatedAt.Equal(projects[j].UpdatedAt) {
			return projects[i].Name < projects[j].Name
		}
		return projects[i].UpdatedAt.After(projects[j].UpdatedAt)
	})

	// カーソルベースのページネーションを適用
	startIndex := 0
	if cursor := params.Pagination.Cursor(); cursor != nil {
		// カーソルが指定されている場合、そのプロジェクトの次から開始
		for i, p := range projects {
			if p.Name == *cursor {
				startIndex = i + 1
				break
			}
		}
	}

	// startIndexから指定された件数を取得
	limit := params.Pagination.Limit()
	if startIndex >= len(projects) {
		return []*model.Project{}, nil
	}
	endIndex := min(startIndex+limit, len(projects))

	return projects[startIndex:endIndex], nil
}

func (m *MockStore) GetProjectTags(ctx context.Context, projectID model.HexID) ([]string, error) {
	// プロジェクトの存在確認
	if _, exists := m.projects[projectID.ToInt64()]; !exists {
		return nil, errors.New("project not found")
	}

	// プロジェクトのレコードからユニークなタグを収集
	tagSet := make(map[string]bool)
	for _, record := range m.records {
		if record.ProjectID.Equals(projectID) {
			for _, tag := range record.Tags {
				tagSet[tag] = true
			}
		}
	}

	// マップからスライスに変換
	var tags []string
	for tag := range tagSet {
		tags = append(tags, tag)
	}

	return tags, nil
}

func TestCreateRecordEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// テストリクエストデータ
	reqBody := map[string]any{
		"project_id": projectID,
		"timestamp":  "2025-05-21T14:30:00Z",
		"value":      1,
	}
	reqBytes, _ := json.Marshal(reqBody)

	// リクエストの作成 - 新しいURLパスを使用
	req := httptest.NewRequest(http.MethodPost, "/api/v0/r", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

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
	if !responseRecord.ProjectID.Equals(projectID) {
		t.Errorf("Expected Project %s, got %s", projectID, responseRecord.ProjectID)
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
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// timestampを省略したテストリクエストデータ
	reqBody := map[string]any{
		"project_id": projectID,
		"value":      1,
		// timestampは意図的に省略
	}
	reqBytes, _ := json.Marshal(reqBody)

	// リクエストの作成
	req := httptest.NewRequest(http.MethodPost, "/api/v0/r", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

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
	if !responseRecord.ProjectID.Equals(projectID) {
		t.Errorf("Expected Project %s, got %s", projectID, responseRecord.ProjectID)
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
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// valueを省略したテストリクエストデータ
	reqBody := map[string]any{
		"project_id": projectID,
		"timestamp":  "2025-05-21T14:30:00Z",
		// valueは意図的に省略
	}
	reqBytes, _ := json.Marshal(reqBody)

	// リクエストの作成
	req := httptest.NewRequest(http.MethodPost, "/api/v0/r", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

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
	if !responseRecord.ProjectID.Equals(projectID) {
		t.Errorf("Expected Project %s, got %s", projectID, responseRecord.ProjectID)
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
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// project_idのみのリクエストボディ（timestamp, valueは省略）
	reqBody := map[string]any{
		"project_id": projectID,
	}
	reqBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/r", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

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
	if !responseRecord.ProjectID.Equals(projectID) {
		t.Errorf("Expected Project %s, got %s", projectID, responseRecord.ProjectID)
	}

	// デフォルト値の確認
	expectedValue := 1 // 空リクエストの場合のデフォルト値
	if responseRecord.Value != expectedValue {
		t.Errorf("Expected default Value %d, got %d", expectedValue, responseRecord.Value)
	}
}

func TestCreateRecordWithNonExistentProject(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// 存在しないproject_idを指定してレコード作成を試みる
	nonExistentProjectID := model.NewHexID(9999)
	reqBody := map[string]any{
		"project_id": nonExistentProjectID,
		"timestamp":  "2025-05-21T14:30:00Z",
		"value":      1,
	}
	reqBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/r", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// 404 Not Foundが返ることを確認
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, w.Code)
		t.Logf("Response body: %s", w.Body.String())
	}

	// エラーメッセージが"Project not found"であることを確認
	responseBody := w.Body.String()
	if !strings.Contains(responseBody, "Project not found") {
		t.Errorf("Expected error message to contain 'Project not found', got: %s", responseBody)
	}
}

func TestGetRecordEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// テスト用のレコードを作成
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(timestamp, projectID, 1, nil)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(context.Background(), testRecord)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成 - 新しいURLパスを使用
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v0/r/%s", testRecord.ID), nil)
	req.Header.Set("X-API-Key", testAPIKey)

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

	if !responseRecord.ID.Equals(testRecord.ID) {
		t.Errorf("Expected ID %s, got %s", testRecord.ID, responseRecord.ID)
	}

	if !responseRecord.Timestamp.Equal(testRecord.Timestamp) {
		t.Errorf("Expected Timestamp %v, got %v", testRecord.Timestamp, responseRecord.Timestamp)
	}

	if !responseRecord.ProjectID.Equals(testRecord.ProjectID) {
		t.Errorf("Expected Project %s, got %s", testRecord.ProjectID, responseRecord.ProjectID)
	}

	t.Logf("Expected value: %d, Response value: %d", testRecord.Value, responseRecord.Value)
	if responseRecord.Value != testRecord.Value {
		t.Errorf("Expected Value %d, got %d", testRecord.Value, responseRecord.Value)
	}
}

func TestGetNonExistentRecordEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// 存在しないIDでリクエスト
	nonExistentID := model.NewHexID(9999)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v0/r/%s", nonExistentID), nil)
	req.Header.Set("X-API-Key", testAPIKey)

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
	mockStore := NewMockStore()

	// プロジェクト
	projectID := model.NewHexID(42)

	// テスト用のレコードを作成
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(timestamp, projectID, 1, nil)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(context.Background(), testRecord)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v0/r/%s", testRecord.ID), nil)
	req.Header.Set("X-API-Key", testAPIKey)

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
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// 存在しないIDでリクエスト
	nonExistentID := model.NewHexID(9999)
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v0/r/%s", nonExistentID), nil)
	req.Header.Set("X-API-Key", testAPIKey)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認（404が期待される）
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestUpdateRecordEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// テスト用のレコードを作成
	originalTimestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, err := model.NewRecord(originalTimestamp, projectID, 5, []string{"tag1", "tag2"})
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(context.Background(), testRecord)

	server := NewServer(mockStore, newTestConfig())

	// 更新データ
	newTimestamp := time.Date(2025, 5, 22, 10, 0, 0, 0, time.UTC)
	updateData := map[string]any{
		"timestamp": newTimestamp.Format(time.RFC3339),
		"value":     10,
		"tags":      []string{"updated-tag1", "updated-tag2", "updated-tag3"},
	}
	reqBytes, _ := json.Marshal(updateData)

	// リクエストの作成
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v0/r/%s", testRecord.ID), bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

	// レスポンスレコーダーの作成
	w := httptest.NewRecorder()

	// ハンドラの実行
	server.ServeHTTP(w, req)

	// レスポンスのステータスコードを確認
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		t.Logf("Response body: %s", w.Body.String())
		return
	}

	// レスポンスボディをデコード
	var responseRecord model.Record
	if err := json.NewDecoder(w.Body).Decode(&responseRecord); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	// 各フィールドが更新されていることを確認
	if !responseRecord.Timestamp.Equal(newTimestamp) {
		t.Errorf("Expected Timestamp %v, got %v", newTimestamp, responseRecord.Timestamp)
	}
	if responseRecord.Value != 10 {
		t.Errorf("Expected Value 10, got %d", responseRecord.Value)
	}
	if len(responseRecord.Tags) != 3 {
		t.Errorf("Expected 3 tags, got %d", len(responseRecord.Tags))
	}
	expectedTags := []string{"updated-tag1", "updated-tag2", "updated-tag3"}
	if !slices.Equal(responseRecord.Tags, expectedTags) {
		t.Errorf("Expected tags %v, got %v", expectedTags, responseRecord.Tags)
	}

	// レコードIDとプロジェクトIDは変わらないことを確認
	if !responseRecord.ID.Equals(testRecord.ID) {
		t.Errorf("Expected ID %s, got %s", testRecord.ID, responseRecord.ID)
	}
	if !responseRecord.ProjectID.Equals(projectID) {
		t.Errorf("Expected ProjectID %s, got %s", projectID, responseRecord.ProjectID)
	}
}

func TestUpdateRecordPartialFields(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	server := NewServer(mockStore, newTestConfig())

	t.Run("Update timestamp only", func(t *testing.T) {
		// テスト用のレコードを作成
		originalTimestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
		testRecord, _ := model.NewRecord(originalTimestamp, projectID, 5, []string{"tag1"})
		mockStore.CreateRecord(context.Background(), testRecord)

		// timestampのみ更新
		newTimestamp := time.Date(2025, 5, 22, 10, 0, 0, 0, time.UTC)
		updateData := map[string]any{
			"timestamp": newTimestamp.Format(time.RFC3339),
		}
		reqBytes, _ := json.Marshal(updateData)

		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v0/r/%s", testRecord.ID), bytes.NewBuffer(reqBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var responseRecord model.Record
		json.NewDecoder(w.Body).Decode(&responseRecord)

		// timestampのみ更新され、他のフィールドは保持されていることを確認
		if !responseRecord.Timestamp.Equal(newTimestamp) {
			t.Errorf("Expected Timestamp %v, got %v", newTimestamp, responseRecord.Timestamp)
		}
		if responseRecord.Value != 5 {
			t.Errorf("Expected Value 5 (unchanged), got %d", responseRecord.Value)
		}
		if !slices.Equal(responseRecord.Tags, []string{"tag1"}) {
			t.Errorf("Expected tags [tag1] (unchanged), got %v", responseRecord.Tags)
		}
	})

	t.Run("Update value only", func(t *testing.T) {
		// テスト用のレコードを作成
		originalTimestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
		testRecord, _ := model.NewRecord(originalTimestamp, projectID, 5, []string{"tag1"})
		mockStore.CreateRecord(context.Background(), testRecord)

		// valueのみ更新
		updateData := map[string]any{
			"value": 20,
		}
		reqBytes, _ := json.Marshal(updateData)

		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v0/r/%s", testRecord.ID), bytes.NewBuffer(reqBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var responseRecord model.Record
		json.NewDecoder(w.Body).Decode(&responseRecord)

		// valueのみ更新され、他のフィールドは保持されていることを確認
		if responseRecord.Value != 20 {
			t.Errorf("Expected Value 20, got %d", responseRecord.Value)
		}
		if !responseRecord.Timestamp.Equal(originalTimestamp) {
			t.Errorf("Expected Timestamp %v (unchanged), got %v", originalTimestamp, responseRecord.Timestamp)
		}
		if !slices.Equal(responseRecord.Tags, []string{"tag1"}) {
			t.Errorf("Expected tags [tag1] (unchanged), got %v", responseRecord.Tags)
		}
	})

	t.Run("Update tags only", func(t *testing.T) {
		// テスト用のレコードを作成
		originalTimestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
		testRecord, _ := model.NewRecord(originalTimestamp, projectID, 5, []string{"tag1"})
		mockStore.CreateRecord(context.Background(), testRecord)

		// tagsのみ更新
		updateData := map[string]any{
			"tags": []string{"new-tag1", "new-tag2"},
		}
		reqBytes, _ := json.Marshal(updateData)

		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v0/r/%s", testRecord.ID), bytes.NewBuffer(reqBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var responseRecord model.Record
		json.NewDecoder(w.Body).Decode(&responseRecord)

		// tagsのみ更新され、他のフィールドは保持されていることを確認
		if !slices.Equal(responseRecord.Tags, []string{"new-tag1", "new-tag2"}) {
			t.Errorf("Expected tags [new-tag1 new-tag2], got %v", responseRecord.Tags)
		}
		if responseRecord.Value != 5 {
			t.Errorf("Expected Value 5 (unchanged), got %d", responseRecord.Value)
		}
		if !responseRecord.Timestamp.Equal(originalTimestamp) {
			t.Errorf("Expected Timestamp %v (unchanged), got %v", originalTimestamp, responseRecord.Timestamp)
		}
	})

	t.Run("Clear tags with empty array", func(t *testing.T) {
		// テスト用のレコードを作成
		originalTimestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
		testRecord, _ := model.NewRecord(originalTimestamp, projectID, 5, []string{"tag1", "tag2"})
		mockStore.CreateRecord(context.Background(), testRecord)

		// 空配列でtagsをクリア
		updateData := map[string]any{
			"tags": []string{},
		}
		reqBytes, _ := json.Marshal(updateData)

		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v0/r/%s", testRecord.ID), bytes.NewBuffer(reqBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var responseRecord model.Record
		json.NewDecoder(w.Body).Decode(&responseRecord)

		// tagsが空になっていることを確認
		if len(responseRecord.Tags) != 0 {
			t.Errorf("Expected 0 tags, got %d: %v", len(responseRecord.Tags), responseRecord.Tags)
		}
	})
}

func TestUpdateNonExistentRecord(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// 存在しないrecord_idで更新を試みる
	nonExistentID := model.NewHexID(9999)
	updateData := map[string]any{
		"value": 10,
	}
	reqBytes, _ := json.Marshal(updateData)

	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v0/r/%s", nonExistentID), bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// 404 Not Foundが返ることを確認
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestUpdateRecordWithInvalidData(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// テスト用のレコードを作成
	timestamp := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	testRecord, _ := model.NewRecord(timestamp, projectID, 5, []string{})
	mockStore.CreateRecord(context.Background(), testRecord)

	server := NewServer(mockStore, newTestConfig())

	t.Run("Invalid timestamp format", func(t *testing.T) {
		updateData := map[string]any{
			"timestamp": "invalid-timestamp",
		}
		reqBytes, _ := json.Marshal(updateData)

		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v0/r/%s", testRecord.ID), bytes.NewBuffer(reqBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		// 400 Bad Requestが返ることを確認
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("Invalid record_id format", func(t *testing.T) {
		updateData := map[string]any{
			"value": 10,
		}
		reqBytes, _ := json.Marshal(updateData)

		// 不正なrecord_id（文字列）
		req := httptest.NewRequest(http.MethodPut, "/api/v0/r/invalid-id", bytes.NewBuffer(reqBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		// 400 Bad Requestが返ることを確認
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

func TestGetGraphEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// テスト用のレコードを作成 - 同じ日付で複数レコード
	timestamp1 := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	record1, _ := model.NewRecord(timestamp1, projectID, 3, nil)
	mockStore.CreateRecord(context.Background(), record1)

	// 同じ日の別の時間のレコード
	timestamp2 := time.Date(2025, 5, 21, 14, 30, 0, 0, time.UTC)
	record2, _ := model.NewRecord(timestamp2, projectID, 2, nil)
	mockStore.CreateRecord(context.Background(), record2)

	// 別の日のレコード
	timestamp3 := time.Date(2025, 5, 22, 9, 0, 0, 0, time.UTC)
	record3, _ := model.NewRecord(timestamp3, projectID, 1, nil)
	mockStore.CreateRecord(context.Background(), record3)

	// 別プロジェクトのレコード (グラフに含まれないはず)
	timestamp4 := time.Date(2025, 5, 22, 10, 0, 0, 0, time.UTC)
	record4, _ := model.NewRecord(timestamp4, model.NewHexID(43), 5, nil)
	mockStore.CreateRecord(context.Background(), record4)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成 (日付範囲指定あり)
	fromDate := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(2025, 5, 31, 23, 59, 59, 0, time.UTC)
	url := fmt.Sprintf("/p/%s/graph?from=%s&to=%s",
		projectID,
		fromDate.Format(time.RFC3339),
		toDate.Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", testAPIKey)

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
	if !strings.Contains(responseBody, `data-date="2025-05-21"`) || !strings.Contains(responseBody, `data-value="5"`) {
		t.Errorf("SVG doesn't contain expected data point for 2025-05-21 with count 5")
	}
	if !strings.Contains(responseBody, `data-date="2025-05-22"`) || !strings.Contains(responseBody, `data-value="1"`) {
		t.Errorf("SVG doesn't contain expected data point for 2025-05-22 with count 1")
	}

	// データがない日（例：5月10日）も0の値で含まれていることを確認
	if !strings.Contains(responseBody, `data-date="2025-05-10"`) || !strings.Contains(responseBody, `data-value="0"`) {
		t.Errorf("SVG doesn't contain expected data point for 2025-05-10 with count 0")
	}
}

func TestGetGraphEndpointWithoutData(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	server := NewServer(mockStore, newTestConfig())

	// 明示的に日付範囲を指定する（データなしだが日付範囲は有効）
	fromDate := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(2025, 5, 31, 23, 59, 59, 0, time.UTC)
	url := fmt.Sprintf("/p/%s/graph?from=%s&to=%s",
		projectID,
		fromDate.Format(time.RFC3339),
		toDate.Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", testAPIKey)

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
	if !strings.Contains(responseBody, `data-date="2025-05-15"`) || !strings.Contains(responseBody, `data-value="0"`) {
		t.Errorf("SVG doesn't contain expected data point for mid-May with count 0")
	}
}

func TestListRecordsEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// テスト用のレコードを複数作成
	timestamp1 := time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC)
	record1, _ := model.NewRecord(timestamp1, projectID, 1, nil)
	mockStore.CreateRecord(context.Background(), record1)

	timestamp2 := time.Date(2025, 5, 21, 12, 0, 0, 0, time.UTC)
	record2, _ := model.NewRecord(timestamp2, projectID, 2, nil)
	mockStore.CreateRecord(context.Background(), record2)

	timestamp3 := time.Date(2025, 5, 22, 14, 0, 0, 0, time.UTC)
	record3, _ := model.NewRecord(timestamp3, projectID, 3, nil)
	mockStore.CreateRecord(context.Background(), record3)

	// 別のプロジェクトのレコード（取得されないはず）
	timestamp4 := time.Date(2025, 5, 23, 16, 0, 0, 0, time.UTC)
	record4, _ := model.NewRecord(timestamp4, model.NewHexID(43), 4, nil)
	mockStore.CreateRecord(context.Background(), record4)

	server := NewServer(mockStore, newTestConfig())

	// リクエストの作成 - 日付範囲を指定
	fromDate := time.Date(2025, 5, 15, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(2025, 5, 25, 23, 59, 59, 0, time.UTC)
	url := fmt.Sprintf("/api/v0/r?project_id=%s&from=%s&to=%s",
		projectID,
		fromDate.Format(time.RFC3339),
		toDate.Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", testAPIKey)

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
	var response ListRecordsResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	// 正しいレコード数が返されていることを確認
	if len(response.Items) != 3 {
		t.Errorf("Expected 3 records, got %d", len(response.Items))
	}
	items := response.Items

	// レコードの内容を確認
	t.Logf("Response records: %+v", items)

	// recordsMap は検証のためにIDベースでレコードにアクセスできるマップ
	recordsMap := make(map[model.HexID]*model.Record)
	for _, r := range items {
		recordsMap[r.ID] = r
	}

	// 期待されるレコードが含まれているか確認
	if _, ok := recordsMap[record1.ID]; !ok {
		t.Errorf("Record 1 missing from response")
	}
	if _, ok := recordsMap[record2.ID]; !ok {
		t.Errorf("Record 2 missing from response")
	}
	if _, ok := recordsMap[record3.ID]; !ok {
		t.Errorf("Record 3 missing from response")
	}

	// 別のプロジェクトのレコードが含まれていないことを確認
	if _, ok := recordsMap[record4.ID]; ok {
		t.Errorf("Record 4 from another project should not be included")
	}
}

func TestListRecordsWithPagination(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project for pagination")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// テスト用に10件のレコードを作成（新しい順にallRecordsに格納）
	var allRecords []*model.Record
	baseTime := time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC)

	for i := 9; i >= 0; i-- {
		recordTime := baseTime.Add(time.Duration(i) * time.Hour)
		record, _ := model.NewRecord(recordTime, projectID, i+1, nil)
		mockStore.CreateRecord(context.Background(), record)
		allRecords = append(allRecords, record)
	}

	server := NewServer(mockStore, newTestConfig())

	// ケース1: limit=3 で最初の3件を取得（カーソルなし）
	t.Run("First Page", func(t *testing.T) {
		url := fmt.Sprintf("/api/v0/r?limit=3&project_id=%s", projectID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var response ListRecordsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}

		records := response.Items
		if len(records) != 3 {
			t.Errorf("Expected 3 records, got %d", len(records))
		} else {
			// 最初の3件のレコードが正しいか確認
			for i := range 3 {
				if !records[i].ID.Equals(allRecords[i].ID) {
					t.Errorf("Record at index %d has incorrect ID, expected %s, got %s", i, allRecords[i].ID, records[i].ID)
				}
			}
		}

		// cursorフィールドが存在することを確認（まだレコードが残っている）
		if response.Cursor == nil {
			t.Error("Expected cursor field to be present")
		}
	})

	// ケース2: limit=4, cursor={3番目のID} で次の4件を取得
	t.Run("Second Page with Cursor", func(t *testing.T) {
		// 3番目のレコード（allRecords[2]）をカーソルとして使用
		// Base64エンコードされたcursorを生成
		thirdRecord := allRecords[2]
		cursor := model.EncodeRecordCursor(
			thirdRecord.Timestamp,
			thirdRecord.ID,
			projectID,
			time.Time{}, // from
			time.Time{}, // to
			nil,         // tags
		)
		url := fmt.Sprintf("/api/v0/r?limit=4&project_id=%s&cursor=%s", projectID, cursor)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var response ListRecordsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}

		items := response.Items
		if len(items) != 4 {
			t.Errorf("Expected 4 records, got %d", len(items))
		} else {

			// カーソルの次のレコードから4件が返されることを確認
			for i := range 4 {
				expectedIndex := i + 3 // allRecords[3], [4], [5], [6]
				if !items[i].ID.Equals(allRecords[expectedIndex].ID) {
					t.Errorf("Record at index %d has incorrect ID, expected %s, got %s",
						i, allRecords[expectedIndex].ID, items[i].ID)
				}
			}
		}
	})

	// ケース3: 最後のレコードをカーソルにした場合、空配列が返される
	t.Run("Last Record as Cursor", func(t *testing.T) {
		// 最後のレコード（allRecords[9]）をカーソルとして使用
		// Base64エンコードされたcursorを生成
		lastRecord := allRecords[9]
		cursor := model.EncodeRecordCursor(
			lastRecord.Timestamp,
			lastRecord.ID,
			projectID,
			time.Time{}, // from
			time.Time{}, // to
			nil,         // tags
		)
		url := fmt.Sprintf("/api/v0/r?limit=5&project_id=%s&cursor=%s", projectID, cursor)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var response ListRecordsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}

		if len(response.Items) != 0 {
			t.Errorf("Expected 0 records, got %d", len(response.Items))
		}

		// cursorフィールドがないことを確認（次ページなし）
		if response.Cursor != nil {
			t.Errorf("Expected cursor field to be nil, got: %s", *response.Cursor)
		}
	})
}

// TestListRecordsWithInvalidPaginationParams tests pagination with invalid parameters
func TestListRecordsWithInvalidPaginationParams(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	projectID := model.NewHexID(42)

	// テスト用にレコードを1件作成
	baseTime := time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC)
	record, _ := model.NewRecord(baseTime, projectID, 1, nil)
	mockStore.CreateRecord(context.Background(), record)

	// ケース1: 無効なlimit（非数値）
	t.Run("Invalid limit (non-numeric)", func(t *testing.T) {
		url := fmt.Sprintf("/api/v0/r?limit=abc&project_id=%s", projectID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	// ケース2: 無効なlimit（負の数）
	t.Run("Invalid limit (negative)", func(t *testing.T) {
		url := fmt.Sprintf("/api/v0/r?limit=-10&project_id=%s", projectID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	// ケース3: 無効なlimit（ゼロ）
	t.Run("Invalid limit (zero)", func(t *testing.T) {
		url := fmt.Sprintf("/api/v0/r?limit=0&project_id=%s", projectID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	// ケース4: 不正な形式のcursor（Base64デコードできない文字列）
	t.Run("Non-existent cursor", func(t *testing.T) {
		// Base64エンコードされていない不正な形式のcursorを渡す
		// 新しい実装では、API層でcursorをデコードするため、
		// 不正な形式の場合は400 BadRequestが返される
		url := fmt.Sprintf("/api/v0/r?cursor=non-existent-id&project_id=%s", projectID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		// 不正な形式のcursorはBadRequestを返す
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

// TestDeleteProject はプロジェクト削除エンドポイントのテスト
func TestDeleteProject(t *testing.T) {
	mockStore := NewMockStore()

	projectID := model.NewHexID(42)

	// テスト用のレコードを作成
	timestamp := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)

	// プロジェクトにレコードを追加
	rec1, err := model.NewRecord(timestamp, projectID, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
  mockStore.CreateRecord(context.Background(), rec1)

	rec2, err := model.NewRecord(timestamp.Add(1*time.Hour), projectID, 15, nil)
	if err != nil {
		t.Fatal(err)
	}
  mockStore.CreateRecord(context.Background(), rec2)

	// 別プロジェクトのレコードも追加
	rec3, err := model.NewRecord(timestamp, model.NewHexID(43), 20, nil)
	if err != nil {
		t.Fatal(err)
	}
  mockStore.CreateRecord(context.Background(), rec3)

	server := NewServer(mockStore, newTestConfig())

	// テスト対象のエンドポイントを呼び出す
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v0/p/%s", projectID), nil)
	req.Header.Set("X-API-Key", testAPIKey)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	// レスポンスを検証
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status NoContent, got %v", rec.Code)
	}

	// プロジェクトのレコードが削除されたことを確認
	pagination, _ := model.NewPagination("100", "")
	testRecords, err := mockStore.ListRecords(context.Background(), &store.ListRecordsParams{
		ProjectID:  projectID,
		From:       time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Pagination: pagination,
		Tags:       []string{},
	})
	if err != nil {
		t.Fatalf("Failed to list test project records: %v", err)
	}
	if len(testRecords) != 0 {
		t.Errorf("Expected 0 records for test project after deletion, got %d", len(testRecords))
	}

	// 他のプロジェクトのレコードが削除されていないことを確認
	_, err = mockStore.GetRecord(context.Background(), rec3.ID)
	if err != nil {
		t.Errorf("Record from other project should not be deleted")
	}
}

// TestDeleteNonExistentProject は存在しないプロジェクトの削除テスト
func TestDeleteNonExistentProject(t *testing.T) {
	store := NewMockStore()
	server := NewServer(store, newTestConfig())

	// テスト対象のエンドポイントを呼び出す - 存在しないプロジェクトID
	nonExistentID := model.NewHexID(99999)
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v0/p/%s", nonExistentID), nil)
	req.Header.Set("X-API-Key", testAPIKey)
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
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project for graph")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// テスト用のレコードを作成
	now := time.Now()
	record1, err := model.NewRecord(now.AddDate(0, 0, -7), projectID, 5, nil)
	if err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}
	mockStore.CreateRecord(context.Background(), record1)

	// リクエストの作成
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/p/%s/graph", projectID), nil)
	req.Header.Set("X-API-Key", testAPIKey)
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
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// trackパラメータ付きのリクエストを作成
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/p/%s/graph?track&tags=good", projectID), nil)
	req.Header.Set("X-API-Key", testAPIKey)
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
		if record.ProjectID.Equals(projectID) {
			foundRecord = record
			break
		}
	}

	if foundRecord == nil {
		t.Errorf("No record created for project %s", projectID)
		return
	}

	// レコードの値が1であることを確認
	if foundRecord.Value != 1 {
		t.Errorf("Expected record value to be 1, got %d", foundRecord.Value)
	}

	if len(foundRecord.Tags) != 1 || foundRecord.Tags[0] != "good" {
		t.Errorf("Expected record to have tag 'good', got %v", foundRecord.Tags)
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
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト名
	projectName := "no-counter-test"

	// trackパラメータなしのリクエストを作成
	req := httptest.NewRequest(http.MethodGet, "/p/"+projectName+"/graph", nil)
	req.Header.Set("X-API-Key", testAPIKey)
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
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("svg-ext-test", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// .svg拡張子付きのリクエストを作成
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/p/%s/graph.svg", projectID), nil)
	req.Header.Set("X-API-Key", testAPIKey)
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
	project1 := model.NewHexID(42)
	project2 := model.NewHexID(43)

	// テスト用の基準日時
	baseTime := time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC)

	// テストケース
	tests := []struct {
		name           string
		project        model.HexID
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
			mockStore := NewMockStore()

			// project1のレコードを作成（5件）
			for i := range 5 {
				recordTime := baseTime.AddDate(0, 0, i) // 1日ずつずらす
				record, _ := model.NewRecord(recordTime, project1, i+1, nil)
				mockStore.CreateRecord(context.Background(), record)
			}

			// project2のレコードを作成（3件）
			for i := range 3 {
				recordTime := baseTime.AddDate(0, 0, i) // 1日ずつずらす
				record, _ := model.NewRecord(recordTime, project2, i+10, nil)
				mockStore.CreateRecord(context.Background(), record)
			}

			// 各テストケースごとに新しいサーバーも作成
			server := NewServer(mockStore, newTestConfig())

			// リクエストボディの作成
			reqBody := map[string]any{
				"project_id": tc.project,
				"until":      tc.until.Format(time.RFC3339),
			}
			reqBytes, _ := json.Marshal(reqBody)

			req := httptest.NewRequest(http.MethodPost, "/api/v0/bulk-deletion", bytes.NewBuffer(reqBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", testAPIKey)
			w := httptest.NewRecorder()

			// リクエスト実行
			server.ServeHTTP(w, req)

			// ステータスコードの確認
			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
				return
			}

			// レスポンスのパース
			var response map[string]any
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
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テストケース
	tests := []struct {
		name           string
		reqBody        map[string]any
		expectedStatus int
	}{
		{
			name: "until パラメータ不足",
			reqBody: map[string]any{
				"project_id": model.NewHexID(42),
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "不正な until フォーマット",
			reqBody: map[string]any{
				"project_id": model.NewHexID(42),
				"until":      "invalid-date",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reqBytes, _ := json.Marshal(tc.reqBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/bulk-deletion", bytes.NewBuffer(reqBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", testAPIKey)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
			}
		})
	}
}

// TestCreateRecordWithTags はタグ付きレコード作成のテスト
func TestCreateRecordWithTags(t *testing.T) {
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// リクエストボディ
	requestBody := map[string]any{
		"project_id": projectID,
		"timestamp":  "2025-05-21T14:30:00Z",
		"value":      5,
		"tags":       []string{"work", "important", "urgent"},
	}

	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/r", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// レスポンスの検証
	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var responseRecord model.Record
	if err := json.NewDecoder(w.Body).Decode(&responseRecord); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// タグが正しく設定されているかチェック
	expectedTags := []string{"work", "important", "urgent"}
	if len(responseRecord.Tags) != len(expectedTags) {
		t.Errorf("Expected %d tags, got %d", len(expectedTags), len(responseRecord.Tags))
	}

	for i, expectedTag := range expectedTags {
		if i >= len(responseRecord.Tags) || responseRecord.Tags[i] != expectedTag {
			t.Errorf("Expected tag[%d] to be %s, got %s", i, expectedTag, responseRecord.Tags[i])
		}
	}

	// ストアに正しく保存されているかチェック
	storedRecord, err := mockStore.GetRecord(context.Background(), responseRecord.ID)
	if err != nil {
		t.Fatalf("Failed to get stored record: %v", err)
	}

	if len(storedRecord.Tags) != len(expectedTags) {
		t.Errorf("Stored record: Expected %d tags, got %d", len(expectedTags), len(storedRecord.Tags))
	}
}

// TestCreateRecordWithEmptyTags は空タグ配列でのレコード作成のテスト
func TestCreateRecordWithEmptyTags(t *testing.T) {
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// リクエストボディ（空のタグ配列）
	requestBody := map[string]any{
		"project_id": projectID,
		"timestamp":  "2025-05-21T14:30:00Z",
		"value":      3,
		"tags":       []string{},
	}

	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/r", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var responseRecord model.Record
	if err := json.NewDecoder(w.Body).Decode(&responseRecord); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// 空のタグ配列が正しく処理されているかチェック
	if len(responseRecord.Tags) != 0 {
		t.Errorf("Expected 0 tags, got %d", len(responseRecord.Tags))
	}
}

// TestListRecordsWithTagsFilter はタグフィルタでのレコード取得のテスト
func TestListRecordsWithTagsFilter(t *testing.T) {
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	projectID := model.NewHexID(42)
	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)

	// 異なるタグを持つレコードを作成
	record1, _ := model.NewRecord(baseTime, projectID, 1, []string{"work", "urgent"})
	record2, _ := model.NewRecord(baseTime.Add(1*time.Hour), projectID, 2, []string{"personal", "hobby"})
	record3, _ := model.NewRecord(baseTime.Add(2*time.Hour), projectID, 3, []string{"work", "meeting"})
	record4, _ := model.NewRecord(baseTime.Add(3*time.Hour), projectID, 4, []string{"personal", "urgent"})

	mockStore.CreateRecord(context.Background(), record1)
	mockStore.CreateRecord(context.Background(), record2)
	mockStore.CreateRecord(context.Background(), record3)
	mockStore.CreateRecord(context.Background(), record4)

	tests := []struct {
		name          string
		tagsFilter    string
		expectedIDs   []model.HexID
		expectedCount int
	}{
		{
			name:          "Filter by work tag",
			tagsFilter:    "work",
			expectedIDs:   []model.HexID{record1.ID, record3.ID},
			expectedCount: 2,
		},
		{
			name:          "Filter by personal tag",
			tagsFilter:    "personal",
			expectedIDs:   []model.HexID{record2.ID, record4.ID},
			expectedCount: 2,
		},
		{
			name:          "Filter by urgent tag (OR)",
			tagsFilter:    "urgent",
			expectedIDs:   []model.HexID{record1.ID, record4.ID},
			expectedCount: 2,
		},
		{
			name:          "Filter by multiple tags (OR)",
			tagsFilter:    "work,hobby",
			expectedIDs:   []model.HexID{record1.ID, record2.ID, record3.ID},
			expectedCount: 3,
		},
		{
			name:          "Filter by non-existent tag",
			tagsFilter:    "nonexistent",
			expectedIDs:   []model.HexID{},
			expectedCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := fmt.Sprintf("/api/v0/r?project_id=%s&tags=%s", projectID, tc.tagsFilter)
			req := httptest.NewRequest(http.MethodGet, url, nil)
			req.Header.Set("X-API-Key", testAPIKey)

			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
			}

			var response ListRecordsResponse
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			items := response.Items
			if len(items) != tc.expectedCount {
				t.Errorf("Expected %d records, got %d", tc.expectedCount, len(items))
			}

			// IDが期待されるものと一致するかチェック
			actualIDs := make(map[model.HexID]bool)
			for _, item := range items {
				actualIDs[item.ID] = true
			}

			for _, expectedID := range tc.expectedIDs {
				if !actualIDs[expectedID] {
					t.Errorf("Expected record with ID %016x not found in results", expectedID)
				}
			}
		})
	}
}

// TestGetGraphWithTagsFilter はタグフィルタでのヒートマップ生成のテスト
func TestGetGraphWithTagsFilter(t *testing.T) {
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)

	// 異なるタグを持つレコードを作成
	record1, _ := model.NewRecord(baseTime, projectID, 5, []string{"work"})
	record2, _ := model.NewRecord(baseTime.Add(24*time.Hour), projectID, 3, []string{"personal"})
	record3, _ := model.NewRecord(baseTime.Add(48*time.Hour), projectID, 7, []string{"work"})

	mockStore.CreateRecord(context.Background(), record1)
	mockStore.CreateRecord(context.Background(), record2)
	mockStore.CreateRecord(context.Background(), record3)

	// workタグでフィルタしたヒートマップ生成
	url := fmt.Sprintf("/p/%s/graph.svg?tags=work", projectID)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "image/svg+xml" {
		t.Errorf("Expected Content-Type 'image/svg+xml', got '%s'", contentType)
	}

	svgContent := w.Body.String()
	if len(svgContent) == 0 {
		t.Error("Expected non-empty SVG content")
	}

	// SVGの基本構造チェック
	if !strings.Contains(svgContent, "<svg") {
		t.Error("Response does not contain SVG tag")
	}
}

// TestCreateProjectEndpoint はプロジェクト作成エンドポイントをテストします。
func TestCreateProjectEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テストデータ
	projectData := map[string]any{
		"name":        "test-project",
		"description": "Test project description",
	}

	// JSON形式でリクエストボディを作成
	requestBody, err := json.Marshal(projectData)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}

	// HTTPリクエストを作成
	req, err := http.NewRequest("POST", "/api/v0/p", bytes.NewBuffer(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

	// レスポンスレコーダーを作成
	w := httptest.NewRecorder()

	// サーバーでリクエストを処理
	server.ServeHTTP(w, req)

	// ステータスコードをチェック
	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, w.Code)
	}

	// レスポンスボディをパース
	var createdProject model.Project
	err = json.Unmarshal(w.Body.Bytes(), &createdProject)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// 作成されたプロジェクトの内容をチェック
	if createdProject.Name != "test-project" {
		t.Errorf("Expected name 'test-project', got %s", createdProject.Name)
	}
	if createdProject.Description != "Test project description" {
		t.Errorf("Expected description 'Test project description', got %s", createdProject.Description)
	}
}

// TestGetProjectEndpoint はプロジェクト取得エンドポイントをテストします。
func TestGetProjectEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test description")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// HTTPリクエストを作成
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v0/p/%s", projectID), nil)
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// ステータスコードをチェック
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// レスポンスボディをパース
	var retrievedProject model.Project
	err := json.Unmarshal(w.Body.Bytes(), &retrievedProject)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// プロジェクトの内容をチェック
	if retrievedProject.Name != "test-project" {
		t.Errorf("Expected name 'test-project', got %s", retrievedProject.Name)
	}
	if retrievedProject.Description != "Test description" {
		t.Errorf("Expected description 'Test description', got %s", retrievedProject.Description)
	}
}

// TestGetNonExistentProjectEndpoint は存在しないプロジェクト取得エンドポイントをテストします。
func TestGetNonExistentProjectEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// HTTPリクエストを作成 - 存在しないプロジェクトID
	nonExistentID := model.NewHexID(99999)
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v0/p/%s", nonExistentID), nil)
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// Not Foundステータスコードを期待
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// TestUpdateProjectEndpoint はプロジェクト更新エンドポイントをテストします。
func TestUpdateProjectEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("update-test", "Original description")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// 更新データ
	updateData := map[string]any{
		"description": "Updated description",
	}

	requestBody, _ := json.Marshal(updateData)
	req, _ := http.NewRequest("PUT", fmt.Sprintf("/api/v0/p/%s", projectID), bytes.NewBuffer(requestBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// ステータスコードをチェック
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// レスポンスボディをパース
	var updatedProject model.Project
	err := json.Unmarshal(w.Body.Bytes(), &updatedProject)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// 更新されたプロジェクトの内容をチェック
	if updatedProject.Description != "Updated description" {
		t.Errorf("Expected description 'Updated description', got %s", updatedProject.Description)
	}
}

// TestListProjectsEndpoint はプロジェクト一覧取得エンドポイントをテストします。
func TestListProjectsEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを複数作成
	project1, _ := model.NewProject("project-1", "Project 1")
	project2, _ := model.NewProject("project-2", "Project 2")
	mockStore.CreateProject(context.Background(), project1)
	mockStore.CreateProject(context.Background(), project2)

	// HTTPリクエストを作成
	req, _ := http.NewRequest("GET", "/api/v0/p", nil)
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// ステータスコードをチェック
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// レスポンスボディをパース
	var response ListProjectsResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// プロジェクト数をチェック
	if len(response.Items) != 2 {
		t.Errorf("Expected 2 projects, got %d", len(response.Items))
	}
}

// TestDeleteProjectEndpoint はプロジェクト削除エンドポイントをテストします。
func TestDeleteProjectEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトとレコードを作成
	project, _ := model.NewProject("delete-test", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	record, _ := model.NewRecord(time.Now(), projectID, 1, []string{})
	mockStore.CreateRecord(context.Background(), record)

	// HTTPリクエストを作成
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("/api/v0/p/%s", projectID), nil)
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// ステータスコードをチェック（現在の実装では204を返す）
	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d", http.StatusNoContent, w.Code)
	}

	// レコードが削除されたことを確認
	pagination, _ := model.NewPagination("100", "")
	records, _ := mockStore.ListRecords(context.Background(), &store.ListRecordsParams{
		ProjectID:  projectID,
		From:       time.Now().Add(-24 * time.Hour),
		To:         time.Now().Add(24 * time.Hour),
		Pagination: pagination,
		Tags:       []string{},
	})
	if len(records) != 0 {
		t.Errorf("Expected 0 records after project deletion, got %d", len(records))
	}
}

// TestGetProjectTagsEndpoint はプロジェクトタグ取得エンドポイントをテストします。
func TestGetProjectTagsEndpoint(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// 異なるタグを持つレコードを作成
	baseTime := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	record1, _ := model.NewRecord(baseTime, projectID, 1, []string{"work", "important"})
	record2, _ := model.NewRecord(baseTime.Add(1*time.Hour), projectID, 2, []string{"personal", "urgent"})
	record3, _ := model.NewRecord(baseTime.Add(2*time.Hour), projectID, 3, []string{"work", "meeting"})

	mockStore.CreateRecord(context.Background(), record1)
	mockStore.CreateRecord(context.Background(), record2)
	mockStore.CreateRecord(context.Background(), record3)

	// HTTPリクエストを作成
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v0/p/%s/t", projectID), nil)
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// ステータスコードをチェック
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		t.Logf("Response body: %s", w.Body.String())
		return
	}

	// レスポンスボディをパース
	var tags []string
	err := json.Unmarshal(w.Body.Bytes(), &tags)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// 期待されるタグが含まれているかチェック
	expectedTags := []string{"work", "important", "personal", "urgent", "meeting"}

	if len(tags) != len(expectedTags) {
		t.Errorf("Expected %d tags, got %d", len(expectedTags), len(tags))
	}

	// タグが期待されるものと一致するかチェック
	tagSet := make(map[string]bool)
	for _, tag := range tags {
		tagSet[tag] = true
	}

	for _, expectedTag := range expectedTags {
		if !tagSet[expectedTag] {
			t.Errorf("Expected tag '%s' not found in response", expectedTag)
		}
	}
}

// TestGetProjectTagsNonExistentProject は存在しないプロジェクトのタグ取得エンドポイントをテストします。
func TestGetProjectTagsNonExistentProject(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// HTTPリクエストを作成 - 存在しないプロジェクトID
	nonExistentID := model.NewHexID(9999)
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v0/p/%s/t", nonExistentID), nil)
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// Internal Server Errorステータスコードを期待（プロジェクトが存在しない場合）
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

// TestGetProjectTagsEmptyProject はタグを持たないプロジェクトのタグ取得エンドポイントをテストします。
func TestGetProjectTagsEmptyProject(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用プロジェクトを作成（レコードなし）
	project, _ := model.NewProject("empty-project", "Empty project")
	mockStore.CreateProject(context.Background(), project)
	projectID := project.ID

	// HTTPリクエストを作成
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v0/p/%s/t", projectID), nil)
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// ステータスコードをチェック
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// レスポンスボディをパース
	var tags []string
	err := json.Unmarshal(w.Body.Bytes(), &tags)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// 空の配列が返されることを確認
	if len(tags) != 0 {
		t.Errorf("Expected 0 tags for empty project, got %d", len(tags))
	}
}

func TestListProjectsWithPagination(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用に5件のプロジェクトを作成
	var allProjects []*model.Project
	for i := range 5 {
		projectName := fmt.Sprintf("project-%d", i)
		description := fmt.Sprintf("Project %d", i)
		project, _ := model.NewProject(projectName, description)
		mockStore.CreateProject(context.Background(), project)
		allProjects = append(allProjects, project)
	}

	// ケース1: limit=2 で最初の2件を取得（cursor指定なし）
	t.Run("First Page", func(t *testing.T) {
		url := "/api/v0/p?limit=2"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var response ListProjectsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}

		if len(response.Items) != 2 {
			t.Errorf("Expected 2 projects, got %d", len(response.Items))
		}

		// 次ページが存在することを確認
		if response.Cursor == nil {
			t.Error("Expected cursor, got nil")
		}
	})

	// ケース2: cursorを使って次の2件を取得
	t.Run("Second Page with Cursor", func(t *testing.T) {
		// 最初のページを取得してcursorを取得
		firstPageURL := "/api/v0/p?limit=2"
		firstReq := httptest.NewRequest(http.MethodGet, firstPageURL, nil)
		firstReq.Header.Set("X-API-Key", testAPIKey)
		firstW := httptest.NewRecorder()
		server.ServeHTTP(firstW, firstReq)

		var firstResponse ListProjectsResponse
		json.NewDecoder(firstW.Body).Decode(&firstResponse)

		if firstResponse.Cursor == nil {
			t.Fatal("Expected cursor in first response")
		}

		// 次ページを取得
		secondPageURL := fmt.Sprintf("/api/v0/p?limit=2&cursor=%s", *firstResponse.Cursor)
		req := httptest.NewRequest(http.MethodGet, secondPageURL, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var response ListProjectsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}

		if len(response.Items) != 2 {
			t.Errorf("Expected 2 projects, got %d", len(response.Items))
		}

		// 次ページが存在することを確認
		if response.Cursor == nil {
			t.Error("Expected cursor, got nil")
		}
	})

	// ケース3: 最後のページ（残り1件）
	t.Run("Last Page", func(t *testing.T) {
		url := "/api/v0/p?limit=10"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var response ListProjectsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}

		// 全5件が返される
		if len(response.Items) != 5 {
			t.Errorf("Expected 5 projects, got %d", len(response.Items))
		}

		// 次ページは存在しない
		if response.Cursor != nil {
			t.Errorf("Expected no cursor, got: %s", *response.Cursor)
		}
	})

	// ケース4: パラメータなしでデフォルトのlimitが適用される
	t.Run("Default Pagination", func(t *testing.T) {
		url := "/api/v0/p"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			return
		}

		var response ListProjectsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}

		// デフォルトのlimitは100なので、5件すべて返される
		if len(response.Items) != 5 {
			t.Errorf("Expected 5 projects, got %d", len(response.Items))
		}

		// 次ページは存在しない
		if response.Cursor != nil {
			t.Errorf("Expected no cursor, got: %s", *response.Cursor)
		}
	})
}

// TestListProjectsWithInvalidPaginationParams tests project pagination with invalid parameters
func TestListProjectsWithInvalidPaginationParams(t *testing.T) {
	// モックストアの準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// テスト用にプロジェクトを1件作成
	project, _ := model.NewProject("test-project", "Test project")
	mockStore.CreateProject(context.Background(), project)

	// ケース1: 無効なlimit（非数値）
	t.Run("Invalid limit (non-numeric)", func(t *testing.T) {
		url := "/api/v0/p?limit=invalid"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	// ケース2: 無効なlimit（負の数）
	t.Run("Invalid limit (negative)", func(t *testing.T) {
		url := "/api/v0/p?limit=-5"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	// ケース3: 無効なlimit（ゼロ）
	t.Run("Invalid limit (zero)", func(t *testing.T) {
		url := "/api/v0/p?limit=0"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	// ケース4: 不正な形式のcursor（Base64デコードできない文字列）
	t.Run("Non-existent cursor", func(t *testing.T) {
		// Base64エンコードされていない不正な形式のcursorを渡す
		// 新しい実装では、API層でcursorをデコードするため、
		// 不正な形式の場合は400 BadRequestが返される
		url := "/api/v0/p?cursor=non-existent-project"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		// 不正な形式のcursorはBadRequestを返す
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

// TestListRecordsEmptyResponse tests that empty record list returns [] instead of null
func TestListRecordsEmptyResponse(t *testing.T) {
	// 空のモックストアを準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクトIDを指定してリクエスト（レコードは存在しない）
	projectID := model.NewHexID(1)
	url := fmt.Sprintf("/api/v0/r?project_id=%s", projectID)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// ステータスコードの確認
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// レスポンスボディのパース
	var response ListRecordsResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// items フィールドが null でないことを確認
	if response.Items == nil {
		t.Error("items field is null, expected empty array")
	}

	// items が空配列であることを確認
	if len(response.Items) != 0 {
		t.Errorf("Expected empty array, got %d items", len(response.Items))
	}
}

// TestListProjectsEmptyResponse tests that empty project list returns [] instead of null
func TestListProjectsEmptyResponse(t *testing.T) {
	// 空のモックストアを準備
	mockStore := NewMockStore()
	server := NewServer(mockStore, newTestConfig())

	// プロジェクト一覧をリクエスト（プロジェクトは存在しない）
	req := httptest.NewRequest(http.MethodGet, "/api/v0/p", nil)
	req.Header.Set("X-API-Key", testAPIKey)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// ステータスコードの確認
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// レスポンスボディのパース
	var response ListProjectsResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// items フィールドが null でないことを確認
	if response.Items == nil {
		t.Error("items field is null, expected empty array")
	}

	// items が空配列であることを確認
	if len(response.Items) != 0 {
		t.Errorf("Expected empty array, got %d items", len(response.Items))
	}
}
