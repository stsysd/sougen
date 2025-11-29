// Package api はsougenのAPIサーバー実装を提供します。
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/stsysd/sougen/config"
	"github.com/stsysd/sougen/heatmap"
	"github.com/stsysd/sougen/model"
	"github.com/stsysd/sougen/store"
)

// Server はAPIサーバーの構造体です。
type Server struct {
	router *http.ServeMux
  store  store.Store
  config *config.Config
}

// ErrorResponse はエラーレスポンスの構造体です。
type ErrorResponse struct {
  Error string `json:"error"`
  Code  int    `json:"code"`
}

// writeJSONError はJSON形式でエラーレスポンスを返却します。
func writeJSONError(w http.ResponseWriter, message string, statusCode int) {
  w.Header().Set("Content-Type", "application/json")
  w.WriteHeader(statusCode)
  resp := ErrorResponse{
    Error: message,
    Code:  statusCode,
  }
  if err := json.NewEncoder(w).Encode(resp); err != nil {
    log.Printf("Error encoding error response: %v", err)
  }
}

// NewServer は新しいAPIサーバーインスタンスを生成します。
func NewServer(store store.Store, config *config.Config) *Server {
  s := &Server{
    router: http.NewServeMux(),
    store:  store,
    config: config,
  }
  s.routes()
  return s
}

// routes はAPIエンドポイントのルーティングを設定します。
func (s *Server) routes() {
  // ヘルスチェックエンドポイントは認証不要
  s.router.HandleFunc("GET /healthz", s.handleHealthCheck)

  // すべての保護されたエンドポイントをまずセキュアなルータに登録
  securedHandler := http.NewServeMux()

  // Project endpoints
  securedHandler.HandleFunc("GET /api/v0/p", s.handleListProjects)
  securedHandler.HandleFunc("POST /api/v0/p", s.handleCreateProject)
  securedHandler.HandleFunc("GET /api/v0/p/{project_id}", s.handleGetProject)
  securedHandler.HandleFunc("PUT /api/v0/p/{project_id}", s.handleUpdateProject)
  securedHandler.HandleFunc("DELETE /api/v0/p/{project_id}", s.handleDeleteProject)

  // Record endpoints
  securedHandler.HandleFunc("POST /api/v0/r", s.handleCreateRecord)
  securedHandler.HandleFunc("GET /api/v0/r", s.handleListRecords)
  securedHandler.HandleFunc("GET /api/v0/r/{record_id}", s.handleGetRecord)
  securedHandler.HandleFunc("PUT /api/v0/r/{record_id}", s.handleUpdateRecord)
  securedHandler.HandleFunc("DELETE /api/v0/r/{record_id}", s.handleDeleteRecord)

  securedHandler.HandleFunc("POST /api/v0/bulk-deletion", s.handleBulkDeleteRecords)

  // Tag endpoints
  securedHandler.HandleFunc("GET /api/v0/p/{project_id}/t", s.handleGetProjectTags)

  // 認証ミドルウェアを適用し、メインルータにマウント
  s.router.Handle("/api/", s.authMiddleware(securedHandler))

  // Graph endpoints - support both with and without .svg extension
  s.router.HandleFunc("GET /p/{project_id}/graph.svg", s.handleGetGraph)
  s.router.HandleFunc("GET /p/{project_id}/graph", s.handleGetGraph)
}

// ServeHTTP はServer構造体をhttp.Handlerとして実装します。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
  // routesに設定されたルーティングを使用する
  s.router.ServeHTTP(w, r)
}

// handleHealthCheck はヘルスチェックエンドポイントのハンドラーです。
func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
  w.Header().Set("Content-Type", "application/json")
  w.WriteHeader(http.StatusOK)
  resp := map[string]string{"status": "ok"}
  if err := json.NewEncoder(w).Encode(resp); err != nil {
    log.Printf("Error encoding response: %v", err)
  }
}

// CreateRecordParams represents parameters for creating a record.
type CreateRecordParams struct {
  ProjectID model.HexID
  Timestamp *model.Timestamp
  Value     *model.Value
  Tags      []string
}

// NewCreateRecordParams creates parameters for record creation from HTTP request.
func NewCreateRecordParams(r *http.Request) (*CreateRecordParams, error) {
  // Parse request body
  var requestBody struct {
    ProjectID model.HexID `json:"project_id"`
    Timestamp string      `json:"timestamp"`
    Value     *int        `json:"value"`
    Tags      []string    `json:"tags"`
  }

  if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
    return nil, fmt.Errorf("invalid request body: %w", err)
  }

  if !requestBody.ProjectID.IsValid() {
    return nil, fmt.Errorf("project_id is required")
  }

  timestamp, err := model.NewTimestamp(requestBody.Timestamp)
  if err != nil {
    return nil, err
  }

  value, err := model.NewValue(requestBody.Value)
  if err != nil {
    return nil, err
  }

  return &CreateRecordParams{
    ProjectID: requestBody.ProjectID,
    Timestamp: timestamp,
    Value:     value,
    Tags:      requestBody.Tags,
  }, nil
}

// handleCreateRecord はレコード作成エンドポイントのハンドラーです。
func (s *Server) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
  // パラメータを検証
  params, err := NewCreateRecordParams(r)
  if err != nil {
    writeJSONError(w, err.Error(), http.StatusBadRequest)
    return
  }

  // プロジェクトの存在確認
  _, err = s.store.GetProject(r.Context(), params.ProjectID)
  if err != nil {
    log.Printf("Error getting project: %v", err)
    writeJSONError(w, "Project not found", http.StatusNotFound)
    return
  }

  // 新しいレコードの作成
  record, err := model.NewRecord(params.Timestamp.Time(), params.ProjectID, params.Value.Int(), params.Tags)
  if err != nil {
    log.Printf("Error creating record: %v", err)
    writeJSONError(w, "Failed to create record", http.StatusBadRequest)
    return
  }

  // レコードの保存
  if err := s.store.CreateRecord(r.Context(), record); err != nil {
    log.Printf("Error creating record: %v", err)
    writeJSONError(w, "Failed to create record", http.StatusInternalServerError)
    return
  }

  // 成功レスポンスの返却
  w.Header().Set("Content-Type", "application/json")
  w.WriteHeader(http.StatusCreated)
  if err := json.NewEncoder(w).Encode(record); err != nil {
    log.Printf("Error encoding response: %v", err)
  }
}

// GetRecordParams represents parameters for getting a record.
type GetRecordParams struct {
  RecordID model.HexID
}

// NewGetRecordParams creates parameters for record retrieval from HTTP request.
func NewGetRecordParams(r *http.Request) (*GetRecordParams, error) {
  recordID, err := model.ParseHexID(r.PathValue("record_id"))
  if err != nil {
    return nil, err
  }

  return &GetRecordParams{
    RecordID: recordID,
  }, nil
}

// handleGetRecord は特定のIDのレコードを取得するハンドラーです。
func (s *Server) handleGetRecord(w http.ResponseWriter, r *http.Request) {
  // パラメータを検証
  params, err := NewGetRecordParams(r)
  if err != nil {
    writeJSONError(w, err.Error(), http.StatusBadRequest)
    return
  }

  // レコードの取得
  record, err := s.store.GetRecord(r.Context(), params.RecordID)
  if err != nil {
    if errors.Is(err, model.ErrRecordNotFound) {
      writeJSONError(w, "Record not found", http.StatusNotFound)
    } else {
      log.Printf("Error retrieving record: %v", err)
      writeJSONError(w, "Failed to retrieve record", http.StatusInternalServerError)
    }
    return
  }

  // レスポンスの返却
  w.Header().Set("Content-Type", "application/json")
  if err := json.NewEncoder(w).Encode(record); err != nil {
    log.Printf("Error encoding response: %v", err)
  }
}

// UpdateRecordParams represents parameters for updating a record.
type UpdateRecordParams struct {
  RecordID  model.HexID
  Timestamp *model.Timestamp
  Value     *model.Value
  Tags      []string
}

// NewUpdateRecordParams creates parameters for record update from HTTP request.
func NewUpdateRecordParams(r *http.Request) (*UpdateRecordParams, error) {
  recordID, err := model.ParseHexID(r.PathValue("record_id"))
  if err != nil {
    return nil, err
  }

  // Parse request body
  var requestBody struct {
    Timestamp *string  `json:"timestamp"`
    Value     *int     `json:"value"`
    Tags      []string `json:"tags"`
  }

  if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
    return nil, fmt.Errorf("invalid request body: %w", err)
  }

  var timestamp *model.Timestamp
  if requestBody.Timestamp != nil {
    timestamp, err = model.NewTimestamp(*requestBody.Timestamp)
    if err != nil {
      return nil, err
    }
  }

  var value *model.Value
  if requestBody.Value != nil {
    value, err = model.NewValue(requestBody.Value)
    if err != nil {
      return nil, err
    }
  }

  return &UpdateRecordParams{
    RecordID:  recordID,
    Timestamp: timestamp,
    Value:     value,
    Tags:      requestBody.Tags,
  }, nil
}

// handleUpdateRecord は特定のIDのレコードを更新するハンドラーです。
func (s *Server) handleUpdateRecord(w http.ResponseWriter, r *http.Request) {
  // パラメータを検証
  params, err := NewUpdateRecordParams(r)
  if err != nil {
    writeJSONError(w, err.Error(), http.StatusBadRequest)
    return
  }

  // 更新前にレコードが存在するか確認
  existingRecord, err := s.store.GetRecord(r.Context(), params.RecordID)
  if err != nil {
    if errors.Is(err, model.ErrRecordNotFound) {
      writeJSONError(w, "Record not found", http.StatusNotFound)
    } else {
      log.Printf("Error retrieving record: %v", err)
      writeJSONError(w, "Failed to retrieve record", http.StatusInternalServerError)
    }
    return
  }

  // 更新用のレコードを既存レコードをベースに作成
  updatedRecord := *existingRecord

  // timestampの更新（指定されている場合）
  if params.Timestamp != nil {
    updatedRecord.Timestamp = params.Timestamp.Time()
  }

  // valueの更新（指定されている場合）
  if params.Value != nil {
    updatedRecord.Value = params.Value.Int()
  }

  // tagsの更新（JSONで明示的に指定されている場合のみ）
  // nil の場合は既存のタグを保持、空配列の場合はタグをクリア
  if params.Tags != nil {
    updatedRecord.Tags = params.Tags
  }

  // レコードの更新
  if err := s.store.UpdateRecord(r.Context(), &updatedRecord); err != nil {
    if errors.Is(err, model.ErrRecordNotFound) {
      writeJSONError(w, "Record not found", http.StatusNotFound)
    } else {
      var validationErr *model.ValidationError
      if errors.As(err, &validationErr) {
        // バリデーションエラーの場合は400を返す
        writeJSONError(w, err.Error(), http.StatusBadRequest)
      } else {
        log.Printf("Error updating record: %v", err)
        writeJSONError(w, "Failed to update record", http.StatusInternalServerError)
      }
    }
    return
  }

  // 更新成功のレスポンスを返却
  w.Header().Set("Content-Type", "application/json")
  if err := json.NewEncoder(w).Encode(&updatedRecord); err != nil {
    log.Printf("Error encoding response: %v", err)
  }
}

// DeleteRecordParams represents parameters for deleting a record.
type DeleteRecordParams struct {
  RecordID model.HexID
}

// NewDeleteRecordParams creates parameters for record deletion from HTTP request.
func NewDeleteRecordParams(r *http.Request) (*DeleteRecordParams, error) {
  recordID, err := model.ParseHexID(r.PathValue("record_id"))
  if err != nil {
    return nil, err
  }

  return &DeleteRecordParams{
    RecordID: recordID,
  }, nil
}

// handleDeleteRecord は特定のIDのレコードを削除するハンドラーです。
func (s *Server) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
  // パラメータを検証
  params, err := NewDeleteRecordParams(r)
  if err != nil {
    writeJSONError(w, err.Error(), http.StatusBadRequest)
    return
  }

  // レコードの削除
  if err := s.store.DeleteRecord(r.Context(), params.RecordID); err != nil {
    if errors.Is(err, model.ErrRecordNotFound) {
      writeJSONError(w, "Record not found", http.StatusNotFound)
    } else {
      log.Printf("Error deleting record: %v", err)
      writeJSONError(w, "Failed to delete record", http.StatusInternalServerError)
    }
    return
  }

  // 削除成功のレスポンスを返す
  w.WriteHeader(http.StatusNoContent)
}

// GetGraphParams represents parameters for getting a graph.
type GetGraphParams struct {
  ProjectID model.HexID
  DateRange *model.DateRange
  Tags      *model.Tags
  Track     bool
}

// NewGetGraphParams creates parameters for graph generation from HTTP request.
func NewGetGraphParams(r *http.Request) (*GetGraphParams, error) {
  projectID, err := model.ParseHexID(r.PathValue("project_id"))
  if err != nil {
    return nil, fmt.Errorf("invalid project_id: %w", err)
  }

  query := r.URL.Query()

  dateRange, err := model.NewDateRange(query.Get("from"), query.Get("to"))
  if err != nil {
    return nil, err
  }

  tags := model.NewTags(query.Get("tags"))
  track := query.Has("track")

  return &GetGraphParams{
    ProjectID: projectID,
    DateRange: dateRange,
    Tags:      tags,
    Track:     track,
  }, nil
}

// handleGetGraph は指定プロジェクトのヒートマップグラフを生成・返却するハンドラーです。
func (s *Server) handleGetGraph(w http.ResponseWriter, r *http.Request) {
  // パラメータを検証
  params, err := NewGetGraphParams(r)
  if err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return
  }

  // アクセスカウンター機能: trackパラメータがある場合、レコードを自動作成
  if params.Track {
    // 新しいレコードの作成（現在時刻、値は1）
    record, err := model.NewRecord(time.Now(), params.ProjectID, 1, params.Tags.Values())
    if err != nil {
      log.Printf("Error creating access counter record: %v", err)
      // エラーが発生してもグラフ表示は続行するため、エラーレスポンスは返さない
    } else {
      // レコードの保存
      if err := s.store.CreateRecord(r.Context(), record); err != nil {
        log.Printf("Error saving access counter record: %v", err)
        // エラーが発生してもグラフ表示は続行
      }
    }
  }

  // プロジェクトを取得（グラフ生成時のタイトル用）
  project, err := s.store.GetProject(r.Context(), params.ProjectID)
  if err != nil {
    log.Printf("Error getting project: %v", err)
    http.Error(w, "Project not found", http.StatusNotFound)
    return
  }

  // レコードの取得と日付ごとの集計
  // イテレータを使用してメモリ効率的に全レコードを処理
  dateMap := make(map[string]int)

  storeParams := &store.ListAllRecordsParams{
    ProjectID: params.ProjectID,
    From:      params.DateRange.From(),
    To:        params.DateRange.To(),
    Tags:      params.Tags.Values(),
  }

  // イテレータで各レコードを順次処理
  for record, err := range s.store.ListAllRecords(r.Context(), storeParams) {
    if err != nil {
      log.Printf("Error retrieving records: %v", err)
      http.Error(w, "Failed to retrieve records", http.StatusInternalServerError)
      return
    }
    dateString := record.Timestamp.Local().Format("2006-01-02")
    dateMap[dateString] += record.Value
  }

  fromDate := params.DateRange.From()
  toDate := params.DateRange.To()

  // ヒートマップ用データの作成（範囲内のすべての日を含む）
  var data []heatmap.Data
  currentDate := fromDate
  for !currentDate.After(toDate) {
    dateString := currentDate.Format("2006-01-02")
    count := dateMap[dateString] // マップに存在しない場合は0を返す
    data = append(data, heatmap.Data{
      Date:  currentDate,
      Value: count,
    })
    currentDate = currentDate.AddDate(0, 0, 1) // 次の日に移動
  }

  // データがない場合（日付範囲が無効な場合のみ）
  if len(data) == 0 {
    svg := ""
    w.Header().Set("Content-Type", "image/svg+xml")
    w.Write([]byte(svg))
    return
  }

  // SVGの生成
  opts := &heatmap.Options{
    CellSize:    12,
    CellPadding: 2,
    FontSize:    10,
    FontFamily:  "sans-serif",
    Colors:      []string{"#ebedf0", "#9be9a8", "#40c463", "#30a14e", "#216e39"},
    ProjectName: project.Name,
  }

  // tagsがある場合はタイトルに含める
  if !params.Tags.IsEmpty() {
    opts.Tags = params.Tags.Values()
  }

  svg := heatmap.GenerateYearlyHeatmapSVG(data, opts)

  // レスポンスの返却
  w.Header().Set("Content-Type", "image/svg+xml")
  w.Write([]byte(svg))
}

// ListRecordsParams represents parameters for listing records.
type ListRecordsParams struct {
  ProjectID  *model.HexID
  DateRange  *model.DateRange
  Tags       *model.Tags
  Pagination *model.Pagination
}

// NewListRecordsParams creates parameters for record listing from HTTP request.
// If cursor is present, all filter parameters are restored from the cursor.
func NewListRecordsParams(r *http.Request) (*ListRecordsParams, error) {
  query := r.URL.Query()
  cursorStr := query.Get("cursor")

  // If cursor exists, restore all parameters from cursor
  if cursorStr != "" {
    cursor, err := model.DecodeRecordCursor(cursorStr)
    if err != nil {
      return nil, fmt.Errorf("invalid cursor: %w", err)
    }

    // Restore date range from cursor
    dateRange, err := model.NewDateRange(cursor.From, cursor.To)
    if err != nil {
      return nil, err
    }

    // Restore tags from cursor
    var tagsStr string
    if len(cursor.Tags) > 0 {
      tagsStr = strings.Join(cursor.Tags, ",")
    }
    tags := model.NewTags(tagsStr)

    // Create pagination with cursor
    pagination, err := model.NewPagination(query.Get("limit"), cursorStr)
    if err != nil {
      return nil, err
    }

    pid := cursor.ProjectID
    return &ListRecordsParams{
      ProjectID:  &pid,
      DateRange:  dateRange,
      Tags:       tags,
      Pagination: pagination,
    }, nil
  }

  // No cursor: use regular parameters from query
  projectIDStr := query.Get("project_id")
  if projectIDStr == "" {
    return nil, fmt.Errorf("project_id is required")
  }
  pid, err := model.ParseHexID(projectIDStr)
  if err != nil {
    return nil, fmt.Errorf("invalid project_id: %w", err)
  }

  dateRange, err := model.NewDateRange(query.Get("from"), query.Get("to"))
  if err != nil {
    return nil, err
  }

  tags := model.NewTags(query.Get("tags"))

  pagination, err := model.NewPagination(query.Get("limit"), "")
  if err != nil {
    return nil, err
  }

  return &ListRecordsParams{
    ProjectID:  &pid,
    DateRange:  dateRange,
    Tags:       tags,
    Pagination: pagination,
  }, nil
}

// ListRecordsResponse represents the paginated response for list records.
type ListRecordsResponse struct {
  Items  []*model.Record `json:"items"`
  Cursor *string         `json:"cursor,omitempty"`
}

// handleListRecords はプロジェクトに属するレコードの一覧を取得するハンドラーです。
func (s *Server) handleListRecords(w http.ResponseWriter, r *http.Request) {
  // パラメータを検証
  params, err := NewListRecordsParams(r)
  if err != nil {
    writeJSONError(w, err.Error(), http.StatusBadRequest)
    return
  }

  // Decode cursor if present to extract position information
  var cursorTimestamp *time.Time
  var cursorID *model.HexID
  if params.Pagination.Cursor() != nil {
    decodedCursor, err := model.DecodeRecordCursor(*params.Pagination.Cursor())
    if err != nil {
      writeJSONError(w, fmt.Sprintf("Invalid cursor: %v", err), http.StatusBadRequest)
      return
    }
    ts, err := time.Parse(time.RFC3339, decodedCursor.Timestamp)
    if err != nil {
      writeJSONError(w, "Invalid cursor timestamp", http.StatusBadRequest)
      return
    }
    cursorTimestamp = &ts
    cursorID = &decodedCursor.ID
  }

  // store.ListRecordsParams を作成
  // project_id is always present (validated in NewListRecordsParams)
  projectID := *params.ProjectID
  storeParams := &store.ListRecordsParams{
    ProjectID:       projectID,
    From:            params.DateRange.From(),
    To:              params.DateRange.To(),
    Pagination:      params.Pagination,
    Tags:            params.Tags.Values(),
    CursorTimestamp: cursorTimestamp,
    CursorID:        cursorID,
  }

  // レコードの取得（limit+1 件取得して次ページの有無を判定）
  originalLimit := params.Pagination.Limit()
  storeParams.Pagination = model.NewPaginationWithValues(originalLimit+1, params.Pagination.Cursor())

  records, err := s.store.ListRecords(r.Context(), storeParams)
  if err != nil {
    log.Printf("Error retrieving records: %v", err)
    writeJSONError(w, "Failed to retrieve records", http.StatusInternalServerError)
    return
  }

  // レスポンスの構築
  response := &ListRecordsResponse{
    Items: records,
  }
  // 空配列を返すためにnilチェック
  if response.Items == nil {
    response.Items = []*model.Record{}
  }

  // 次ページのカーソルを生成
  if len(records) > originalLimit {
    // limit+1 件取得できた場合、次ページが存在する
    response.Items = records[:originalLimit]
    lastRecord := records[originalLimit-1]

    // 次ページ用のカーソルをエンコード
    cursor := model.EncodeRecordCursor(
      lastRecord.Timestamp,
      lastRecord.ID,
      projectID,
      params.DateRange.From(),
      params.DateRange.To(),
      params.Tags.Values(),
    )
    response.Cursor = &cursor
  }

  // レスポンスの返却
  w.Header().Set("Content-Type", "application/json")
  if err := json.NewEncoder(w).Encode(response); err != nil {
    log.Printf("Error encoding response: %v", err)
  }
}

// GetProjectParams represents parameters for getting project info.
type GetProjectParams struct {
  ProjectID model.HexID
}

// NewGetProjectParams creates parameters for project retrieval from HTTP request.
func NewGetProjectParams(r *http.Request) (*GetProjectParams, error) {
  projectID, err := model.ParseHexID(r.PathValue("project_id"))
  if err != nil {
    return nil, fmt.Errorf("invalid project_id: %w", err)
  }

  return &GetProjectParams{
    ProjectID: projectID,
  }, nil
}

// handleGetProject はプロジェクト取得をハンドリングします。
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
  // パラメータを検証
  params, err := NewGetProjectParams(r)
  if err != nil {
    writeJSONError(w, err.Error(), http.StatusBadRequest)
    return
  }

  // プロジェクトの取得
  project, err := s.store.GetProject(r.Context(), params.ProjectID)
  if err != nil {
    if errors.Is(err, model.ErrProjectNotFound) {
      writeJSONError(w, fmt.Sprintf("Project with ID %s not found", params.ProjectID), http.StatusNotFound)
    } else {
      writeJSONError(w, fmt.Sprintf("Error retrieving project: %v", err), http.StatusInternalServerError)
    }
    return
  }

  // レスポンスの設定
  w.Header().Set("Content-Type", "application/json")
  w.WriteHeader(http.StatusOK)

  // JSONとしてレスポンスを返す
  if err := json.NewEncoder(w).Encode(project); err != nil {
    log.Printf("Error encoding response: %v", err)
  }
}

// ListProjectsParams はプロジェクト一覧取得のパラメータです。
type ListProjectsParams struct {
  Pagination *model.Pagination
}

// NewListProjectsParams はリクエストからプロジェクト一覧取得のパラメータを作成します。
func NewListProjectsParams(r *http.Request) (*ListProjectsParams, error) {
  query := r.URL.Query()

  pagination, err := model.NewPagination(query.Get("limit"), query.Get("cursor"))
  if err != nil {
    return nil, err
  }

  return &ListProjectsParams{
    Pagination: pagination,
  }, nil
}

// ListProjectsResponse はプロジェクト一覧取得のレスポンスです。
type ListProjectsResponse struct {
  Items  []*model.Project `json:"items"`
  Cursor *string          `json:"cursor,omitempty"`
}

// handleListProjects はプロジェクト一覧取得をハンドリングします。
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
  // パラメータを検証
  params, err := NewListProjectsParams(r)
  if err != nil {
    writeJSONError(w, err.Error(), http.StatusBadRequest)
    return
  }

  // Decode cursor if present to extract position information
  var cursorUpdatedAt *time.Time
  var cursorName *string
  if params.Pagination.Cursor() != nil {
    decodedCursor, err := model.DecodeProjectCursor(*params.Pagination.Cursor())
    if err != nil {
      writeJSONError(w, fmt.Sprintf("Invalid cursor: %v", err), http.StatusBadRequest)
      return
    }
    updatedAt, err := time.Parse(time.RFC3339, decodedCursor.UpdatedAt)
    if err != nil {
      writeJSONError(w, "Invalid cursor updated_at", http.StatusBadRequest)
      return
    }
    cursorUpdatedAt = &updatedAt
    cursorName = &decodedCursor.Name
  }

  // プロジェクトの取得（limit+1 件取得して次ページの有無を判定）
  originalLimit := params.Pagination.Limit()
  storeParams := &store.ListProjectsParams{
    Pagination:      model.NewPaginationWithValues(originalLimit+1, params.Pagination.Cursor()),
    CursorUpdatedAt: cursorUpdatedAt,
    CursorName:      cursorName,
  }

  projects, err := s.store.ListProjects(r.Context(), storeParams)
  if err != nil {
    writeJSONError(w, fmt.Sprintf("Error retrieving projects: %v", err), http.StatusInternalServerError)
    return
  }

  // レスポンスの構築
  response := &ListProjectsResponse{
    Items: projects,
  }
  // 空配列を返すためにnilチェック
  if response.Items == nil {
    response.Items = []*model.Project{}
  }

  // 次ページのカーソルを生成
  if len(projects) > originalLimit {
    // limit+1 件取得できた場合、次ページが存在する
    response.Items = projects[:originalLimit]
    lastProject := projects[originalLimit-1]

    // 次ページ用のカーソルをエンコード
    cursor := model.EncodeProjectCursor(
      lastProject.UpdatedAt,
      lastProject.Name,
    )
    response.Cursor = &cursor
  }

  // レスポンスの返却
  w.Header().Set("Content-Type", "application/json")
  if err := json.NewEncoder(w).Encode(response); err != nil {
    log.Printf("Error encoding response: %v", err)
  }
}

// handleCreateProject はプロジェクト作成をハンドリングします。
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
  // リクエストボディの読み取り
  body, err := io.ReadAll(r.Body)
  if err != nil {
    writeJSONError(w, "Failed to read request body", http.StatusBadRequest)
    return
  }

  // JSONのパース
  var projectData struct {
    Name        string `json:"name"`
    Description string `json:"description"`
  }
  if err := json.Unmarshal(body, &projectData); err != nil {
    writeJSONError(w, "Invalid JSON format", http.StatusBadRequest)
    return
  }

  // プロジェクトの作成
  project, err := model.NewProject(projectData.Name, projectData.Description)
  if err != nil {
    writeJSONError(w, fmt.Sprintf("Invalid project data: %v", err), http.StatusBadRequest)
    return
  }

  // データベースに保存
  if err := s.store.CreateProject(r.Context(), project); err != nil {
    writeJSONError(w, fmt.Sprintf("Failed to create project: %v", err), http.StatusInternalServerError)
    return
  }

  // レスポンスの設定
  w.Header().Set("Content-Type", "application/json")
  w.WriteHeader(http.StatusCreated)

  // 作成されたプロジェクトをJSONとして返す
  if err := json.NewEncoder(w).Encode(project); err != nil {
    log.Printf("Error encoding response: %v", err)
  }
}

// handleUpdateProject はプロジェクト更新をハンドリングします。
func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
  // URLからプロジェクトIDを取得
  projectID, err := model.ParseHexID(r.PathValue("project_id"))
  if err != nil {
    writeJSONError(w, "Invalid project_id", http.StatusBadRequest)
    return
  }

  // 既存プロジェクトの取得
  existingProject, err := s.store.GetProject(r.Context(), projectID)
  if err != nil {
    if errors.Is(err, model.ErrProjectNotFound) {
      writeJSONError(w, fmt.Sprintf("Project with ID %s not found", projectID), http.StatusNotFound)
    } else {
      writeJSONError(w, fmt.Sprintf("Error retrieving project: %v", err), http.StatusInternalServerError)
    }
    return
  }

  // リクエストボディの読み取り
  body, err := io.ReadAll(r.Body)
  if err != nil {
    writeJSONError(w, "Failed to read request body", http.StatusBadRequest)
    return
  }

  // JSONのパース（部分更新をサポートするためポインタ型を使用）
  var updateData struct {
    Name        *string `json:"name"`
    Description *string `json:"description"`
  }
  if err := json.Unmarshal(body, &updateData); err != nil {
    writeJSONError(w, "Invalid JSON format", http.StatusBadRequest)
    return
  }

  // プロジェクトの部分更新（指定されたフィールドのみ更新）
  if updateData.Name != nil {
    existingProject.Name = *updateData.Name
  }
  if updateData.Description != nil {
    existingProject.Description = *updateData.Description
  }
  existingProject.UpdatedAt = time.Now()

  // バリデーション
  if err := existingProject.Validate(); err != nil {
    writeJSONError(w, err.Error(), http.StatusBadRequest)
    return
  }

  // データベースに保存
  if err := s.store.UpdateProject(r.Context(), existingProject); err != nil {
    writeJSONError(w, fmt.Sprintf("Failed to update project: %v", err), http.StatusInternalServerError)
    return
  }

  // レスポンスの設定
  w.Header().Set("Content-Type", "application/json")
  w.WriteHeader(http.StatusOK)

  // 更新されたプロジェクトをJSONとして返す
  if err := json.NewEncoder(w).Encode(existingProject); err != nil {
    log.Printf("Error encoding response: %v", err)
  }
}

// DeleteProjectParams represents parameters for deleting a project.
type DeleteProjectParams struct {
  ProjectID model.HexID
}

// NewDeleteProjectParams creates parameters for project deletion from HTTP request.
func NewDeleteProjectParams(r *http.Request) (*DeleteProjectParams, error) {
  projectID, err := model.ParseHexID(r.PathValue("project_id"))
  if err != nil {
    return nil, fmt.Errorf("invalid project_id: %w", err)
  }

  return &DeleteProjectParams{
    ProjectID: projectID,
  }, nil
}

// handleDeleteProject はプロジェクト削除をハンドリングします。
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
  // パラメータを検証
  params, err := NewDeleteProjectParams(r)
  if err != nil {
    writeJSONError(w, err.Error(), http.StatusBadRequest)
    return
  }

  // プロジェクト削除の実行（べき等性：既に存在しない場合もエラーにしない）
	err = s.store.DeleteProject(r.Context(), params.ProjectID)
	if err != nil {
		// プロジェクトが存在しない場合は成功とみなす（べき等性）
		if errors.Is(err, model.ErrProjectNotFound) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// その他のエラーの場合は500を返す
		log.Printf("Error deleting project: %v", err)
		writeJSONError(w, fmt.Sprintf("Failed to delete project: %v", err), http.StatusInternalServerError)
		return
	}

	// 成功した場合は204 No Contentを返す
	w.WriteHeader(http.StatusNoContent)
}

// handleBulkDeleteRecords は条件に一致するレコードをまとめて削除するハンドラーです。
func (s *Server) handleBulkDeleteRecords(w http.ResponseWriter, r *http.Request) {
	// リクエストボディの読み取り
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// JSONのパース
	var deletionData struct {
		ProjectID model.HexID `json:"project_id"`
		Until     string      `json:"until"`
	}
	if err := json.Unmarshal(body, &deletionData); err != nil {
		writeJSONError(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	// untilパラメータの検証
	if deletionData.Until == "" {
		writeJSONError(w, "until parameter is required", http.StatusBadRequest)
		return
	}

	// タイムスタンプのパース
	timestamp, err := model.NewTimestamp(deletionData.Until)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// レコードの一括削除を実行
	count, err := s.store.DeleteRecordsUntil(r.Context(), deletionData.ProjectID, timestamp.Time())
	if err != nil {
		log.Printf("Error deleting records until specified date: %v", err)
		writeJSONError(w, "Failed to delete records", http.StatusInternalServerError)
		return
	}

	// 削除結果をJSONで返す
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]int{
		"deleted_count": count,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// GetProjectTagsParams represents parameters for getting project tags.
type GetProjectTagsParams struct {
	ProjectID model.HexID
}

// NewGetProjectTagsParams creates parameters for project tags retrieval from HTTP request.
func NewGetProjectTagsParams(r *http.Request) (*GetProjectTagsParams, error) {
	projectID, err := model.ParseHexID(r.PathValue("project_id"))
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	return &GetProjectTagsParams{
		ProjectID: projectID,
	}, nil
}

// handleGetProjectTags はプロジェクト内のタグ一覧を取得するハンドラーです。
func (s *Server) handleGetProjectTags(w http.ResponseWriter, r *http.Request) {
	// パラメータを検証
	params, err := NewGetProjectTagsParams(r)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// プロジェクトの存在確認
	_, err = s.store.GetProject(r.Context(), params.ProjectID)
	if err != nil {
		if errors.Is(err, model.ErrProjectNotFound) {
			writeJSONError(w, fmt.Sprintf("Project with ID %s not found", params.ProjectID), http.StatusNotFound)
		} else {
			writeJSONError(w, fmt.Sprintf("Error retrieving project: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// タグの取得
	tags, err := s.store.GetProjectTags(r.Context(), params.ProjectID)
	if err != nil {
		log.Printf("Error retrieving project tags: %v", err)
		writeJSONError(w, "Failed to retrieve project tags", http.StatusInternalServerError)
		return
	}

	// レスポンスの返却
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tags); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// Run はサーバーを指定されたアドレスで起動します。
func (s *Server) Run(addr string) error {
	log.Printf("Server starting on %s", addr)
	return http.ListenAndServe(addr, s)
}
