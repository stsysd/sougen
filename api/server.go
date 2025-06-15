// Package api はsougenのAPIサーバー実装を提供します。
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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
	store  store.RecordStore
	config *config.Config
}

// NewServer は新しいAPIサーバーインスタンスを生成します。
func NewServer(recordStore store.RecordStore, config *config.Config) *Server {
	s := &Server{
		router: http.NewServeMux(),
		store:  recordStore,
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
	securedHandler.HandleFunc("GET /api/v0/p/{project_name}", s.handleGetProject)
	securedHandler.HandleFunc("PUT /api/v0/p/{project_name}", s.handleUpdateProject)
	securedHandler.HandleFunc("DELETE /api/v0/p/{project_name}", s.handleDeleteProject)
	
	// Record endpoints
	securedHandler.HandleFunc("POST /api/v0/p/{project_name}/r", s.handleCreateRecord)
	securedHandler.HandleFunc("GET /api/v0/p/{project_name}/r", s.handleListRecords)
	securedHandler.HandleFunc("GET /api/v0/p/{project_name}/r/{record_id}", s.handleGetRecord)
	securedHandler.HandleFunc("PUT /api/v0/p/{project_name}/r/{record_id}", s.handleUpdateRecord)
	securedHandler.HandleFunc("DELETE /api/v0/p/{project_name}/r/{record_id}", s.handleDeleteRecord)
	securedHandler.HandleFunc("DELETE /api/v0/r", s.handleBulkDeleteRecords)

	// 認証ミドルウェアを適用し、メインルータにマウント
	s.router.Handle("/api/", s.authMiddleware(securedHandler))

	// Graph endpoints - support both with and without .svg extension
	s.router.HandleFunc("GET /p/{project_name}/graph.svg", s.handleGetGraph)
	s.router.HandleFunc("GET /p/{project_name}/graph", s.handleGetGraph)
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
	ProjectName *model.ProjectName
	Timestamp   *model.Timestamp
	Value       *model.Value
	Tags        []string
}

// NewCreateRecordParams creates parameters for record creation from HTTP request.
func NewCreateRecordParams(r *http.Request) (*CreateRecordParams, error) {
	projectName, err := model.NewProjectName(r.PathValue("project_name"))
	if err != nil {
		return nil, err
	}

	// Parse request body
	var requestBody struct {
		Timestamp string   `json:"timestamp"`
		Value     *int     `json:"value"`
		Tags      []string `json:"tags"`
	}

	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			return nil, fmt.Errorf("invalid request body: %w", err)
		}
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
		ProjectName: projectName,
		Timestamp:   timestamp,
		Value:       value,
		Tags:        requestBody.Tags,
	}, nil
}

// handleCreateRecord はレコード作成エンドポイントのハンドラーです。
func (s *Server) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
	// パラメータを検証
	params, err := NewCreateRecordParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 新しいレコードの作成
	record, err := model.NewRecord(params.Timestamp.Time(), params.ProjectName.String(), params.Value.Int(), params.Tags)
	if err != nil {
		log.Printf("Error creating record: %v", err)
		http.Error(w, "Failed to create record", http.StatusBadRequest)
		return
	}

	// レコードの保存
	if err := s.store.CreateRecord(r.Context(), record); err != nil {
		log.Printf("Error creating record: %v", err)
		http.Error(w, "Failed to create record", http.StatusInternalServerError)
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
	ProjectName *model.ProjectName
	RecordID    *model.RecordID
}

// NewGetRecordParams creates parameters for record retrieval from HTTP request.
func NewGetRecordParams(r *http.Request) (*GetRecordParams, error) {
	projectName, err := model.NewProjectName(r.PathValue("project_name"))
	if err != nil {
		return nil, err
	}

	recordID, err := model.NewRecordID(r.PathValue("record_id"))
	if err != nil {
		return nil, err
	}

	return &GetRecordParams{
		ProjectName: projectName,
		RecordID:    recordID,
	}, nil
}

// handleGetRecord は特定のIDのレコードを取得するハンドラーです。
func (s *Server) handleGetRecord(w http.ResponseWriter, r *http.Request) {
	// パラメータを検証
	params, err := NewGetRecordParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// レコードの取得
	record, err := s.store.GetRecord(r.Context(), params.RecordID.UUID())
	if err != nil {
		if err.Error() == "record not found" {
			http.Error(w, "Record not found", http.StatusNotFound)
		} else {
			log.Printf("Error retrieving record: %v", err)
			http.Error(w, "Failed to retrieve record", http.StatusInternalServerError)
		}
		return
	}

	// レコードが指定されたプロジェクトのものかチェック
	if record.Project != params.ProjectName.String() {
		http.Error(w, "Record not found in this project", http.StatusNotFound)
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
	ProjectName *model.ProjectName
	RecordID    *model.RecordID
	Project     *string
	Timestamp   *model.Timestamp
	Value       *model.Value
	Tags        []string
}

// NewUpdateRecordParams creates parameters for record update from HTTP request.
func NewUpdateRecordParams(r *http.Request) (*UpdateRecordParams, error) {
	projectName, err := model.NewProjectName(r.PathValue("project_name"))
	if err != nil {
		return nil, err
	}

	recordID, err := model.NewRecordID(r.PathValue("record_id"))
	if err != nil {
		return nil, err
	}

	// Parse request body
	var requestBody struct {
		Project   *string  `json:"project"`
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
		ProjectName: projectName,
		RecordID:    recordID,
		Project:     requestBody.Project,
		Timestamp:   timestamp,
		Value:       value,
		Tags:        requestBody.Tags,
	}, nil
}

// handleUpdateRecord は特定のIDのレコードを更新するハンドラーです。
func (s *Server) handleUpdateRecord(w http.ResponseWriter, r *http.Request) {
	// パラメータを検証
	params, err := NewUpdateRecordParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 更新前にレコードが存在するかつ指定プロジェクトのものかを確認
	existingRecord, err := s.store.GetRecord(r.Context(), params.RecordID.UUID())
	if err != nil {
		if err.Error() == "record not found" {
			http.Error(w, "Record not found", http.StatusNotFound)
		} else {
			log.Printf("Error retrieving record: %v", err)
			http.Error(w, "Failed to retrieve record", http.StatusInternalServerError)
		}
		return
	}

	// レコードが指定されたプロジェクトのものかチェック
	if existingRecord.Project != params.ProjectName.String() {
		http.Error(w, "Record not found in this project", http.StatusNotFound)
		return
	}

	// 更新用のレコードを既存レコードをベースに作成
	updatedRecord := *existingRecord

	// projectの更新（指定されている場合）
	if params.Project != nil {
		updatedRecord.Project = *params.Project
	}

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
		if err.Error() == "record not found" {
			http.Error(w, "Record not found", http.StatusNotFound)
		} else {
			log.Printf("Error updating record: %v", err)
			http.Error(w, "Failed to update record", http.StatusInternalServerError)
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
	ProjectName *model.ProjectName
	RecordID    *model.RecordID
}

// NewDeleteRecordParams creates parameters for record deletion from HTTP request.
func NewDeleteRecordParams(r *http.Request) (*DeleteRecordParams, error) {
	projectName, err := model.NewProjectName(r.PathValue("project_name"))
	if err != nil {
		return nil, err
	}

	recordID, err := model.NewRecordID(r.PathValue("record_id"))
	if err != nil {
		return nil, err
	}

	return &DeleteRecordParams{
		ProjectName: projectName,
		RecordID:    recordID,
	}, nil
}

// handleDeleteRecord は特定のIDのレコードを削除するハンドラーです。
func (s *Server) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	// パラメータを検証
	params, err := NewDeleteRecordParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 削除前にレコードが存在するかつ指定プロジェクトのものかを確認
	record, err := s.store.GetRecord(r.Context(), params.RecordID.UUID())
	if err != nil {
		if err.Error() == "record not found" {
			http.Error(w, "Record not found", http.StatusNotFound)
		} else {
			log.Printf("Error retrieving record: %v", err)
			http.Error(w, "Failed to retrieve record", http.StatusInternalServerError)
		}
		return
	}

	// レコードが指定されたプロジェクトのものかチェック
	if record.Project != params.ProjectName.String() {
		http.Error(w, "Record not found in this project", http.StatusNotFound)
		return
	}

	// レコードの削除
	if err := s.store.DeleteRecord(r.Context(), params.RecordID.UUID()); err != nil {
		log.Printf("Error deleting record: %v", err)
		http.Error(w, "Failed to delete record", http.StatusInternalServerError)
		return
	}

	// 削除成功のレスポンスを返す
	w.WriteHeader(http.StatusNoContent)
}

// GetGraphParams represents parameters for getting a graph.
type GetGraphParams struct {
	ProjectName *model.ProjectName
	DateRange   *model.DateRange
	Tags        *model.Tags
	Track       bool
}

// NewGetGraphParams creates parameters for graph generation from HTTP request.
func NewGetGraphParams(r *http.Request) (*GetGraphParams, error) {
	projectName, err := model.NewProjectName(r.PathValue("project_name"))
	if err != nil {
		return nil, err
	}

	query := r.URL.Query()

	dateRange, err := model.NewDateRange(query.Get("from"), query.Get("to"))
	if err != nil {
		return nil, err
	}

	tags := model.NewTags(query.Get("tags"))
	track := query.Has("track")

	return &GetGraphParams{
		ProjectName: projectName,
		DateRange:   dateRange,
		Tags:        tags,
		Track:       track,
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
		record, err := model.NewRecord(time.Now(), params.ProjectName.String(), 1, params.Tags.Values())
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

	// レコードの取得
	var records []*model.Record
	if !params.Tags.IsEmpty() {
		// タグフィルタありのレコード取得
		records, err = s.store.ListRecordsWithTags(r.Context(), params.ProjectName.String(), params.DateRange.From(), params.DateRange.To(), params.Tags.Values())
	} else {
		// タグフィルタなしのレコード取得
		records, err = s.store.ListRecords(r.Context(), params.ProjectName.String(), params.DateRange.From(), params.DateRange.To())
	}

	if err != nil {
		log.Printf("Error retrieving records: %v", err)
		http.Error(w, "Failed to retrieve records", http.StatusInternalServerError)
		return
	}

	// 日付ごとに集計
	dateMap := make(map[string]int)
	for _, record := range records {
		dateString := record.Timestamp.Format("2006-01-02")
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
			Count: count,
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
		ProjectName: params.ProjectName.String(),
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
	ProjectName *model.ProjectName
	DateRange   *model.DateRange
	Tags        *model.Tags
	Pagination  *model.Pagination
}

// NewListRecordsParams creates parameters for record listing from HTTP request.
func NewListRecordsParams(r *http.Request) (*ListRecordsParams, error) {
	projectName, err := model.NewProjectName(r.PathValue("project_name"))
	if err != nil {
		return nil, err
	}

	query := r.URL.Query()

	dateRange, err := model.NewDateRange(query.Get("from"), query.Get("to"))
	if err != nil {
		return nil, err
	}

	tags := model.NewTags(query.Get("tags"))

	pagination, err := model.NewPagination(query.Get("limit"), query.Get("offset"))
	if err != nil {
		return nil, err
	}

	return &ListRecordsParams{
		ProjectName: projectName,
		DateRange:   dateRange,
		Tags:        tags,
		Pagination:  pagination,
	}, nil
}

// handleListRecords はプロジェクトに属するレコードの一覧を取得するハンドラーです。
func (s *Server) handleListRecords(w http.ResponseWriter, r *http.Request) {
	// パラメータを検証
	params, err := NewListRecordsParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// レコードの取得
	var records []*model.Record
	if !params.Tags.IsEmpty() {
		// タグフィルタありのレコード取得
		records, err = s.store.ListRecordsWithTags(r.Context(), params.ProjectName.String(), params.DateRange.From(), params.DateRange.To(), params.Tags.Values())
	} else {
		// タグフィルタなしのレコード取得
		records, err = s.store.ListRecords(r.Context(), params.ProjectName.String(), params.DateRange.From(), params.DateRange.To())
	}

	if err != nil {
		log.Printf("Error retrieving records: %v", err)
		http.Error(w, "Failed to retrieve records", http.StatusInternalServerError)
		return
	}

	// ページネーションの適用
	totalRecords := len(records)
	endIndex := params.Pagination.Offset() + params.Pagination.Limit()
	if endIndex > totalRecords {
		endIndex = totalRecords
	}

	// 指定された範囲のレコードのみを抽出
	var pagedRecords []*model.Record
	if params.Pagination.Offset() < totalRecords {
		pagedRecords = records[params.Pagination.Offset():endIndex]
	} else {
		pagedRecords = []*model.Record{} // offsetが範囲外の場合は空配列
	}

	// レスポンスの返却
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pagedRecords); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// GetProjectParams represents parameters for getting project info.
type GetProjectParams struct {
	ProjectName *model.ProjectName
}

// NewGetProjectParams creates parameters for project retrieval from HTTP request.
func NewGetProjectParams(r *http.Request) (*GetProjectParams, error) {
	projectName, err := model.NewProjectName(r.PathValue("project_name"))
	if err != nil {
		return nil, err
	}

	return &GetProjectParams{
		ProjectName: projectName,
	}, nil
}

// handleGetProject はプロジェクト取得をハンドリングします。
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	// パラメータを検証
	params, err := NewGetProjectParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// プロジェクトの取得
	projectStore, ok := s.store.(store.ProjectStore)
	if !ok {
		http.Error(w, "Project operations not supported", http.StatusInternalServerError)
		return
	}

	project, err := projectStore.GetProject(r.Context(), params.ProjectName.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "project not found" {
			http.Error(w, fmt.Sprintf("Project '%s' not found", params.ProjectName.String()), http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Error retrieving project: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// レスポンスの設定
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// JSONとしてレスポンスを返す
	if err := json.NewEncoder(w).Encode(project); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleListProjects はプロジェクト一覧取得をハンドリングします。
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projectStore, ok := s.store.(store.ProjectStore)
	if !ok {
		http.Error(w, "Project operations not supported", http.StatusInternalServerError)
		return
	}

	projects, err := projectStore.ListProjects(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("Error retrieving projects: %v", err), http.StatusInternalServerError)
		return
	}

	// レスポンスの設定
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// JSONとしてレスポンスを返す
	if err := json.NewEncoder(w).Encode(projects); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleCreateProject はプロジェクト作成をハンドリングします。
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	projectStore, ok := s.store.(store.ProjectStore)
	if !ok {
		http.Error(w, "Project operations not supported", http.StatusInternalServerError)
		return
	}

	// リクエストボディの読み取り
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// JSONのパース
	var projectData struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &projectData); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	// プロジェクトの作成
	project, err := model.NewProject(projectData.Name, projectData.Description)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid project data: %v", err), http.StatusBadRequest)
		return
	}

	// データベースに保存
	if err := projectStore.CreateProject(r.Context(), project); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			http.Error(w, fmt.Sprintf("Project '%s' already exists", project.Name), http.StatusConflict)
		} else {
			http.Error(w, fmt.Sprintf("Failed to create project: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// レスポンスの設定
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	// 作成されたプロジェクトをJSONとして返す
	if err := json.NewEncoder(w).Encode(project); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleUpdateProject はプロジェクト更新をハンドリングします。
func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	projectStore, ok := s.store.(store.ProjectStore)
	if !ok {
		http.Error(w, "Project operations not supported", http.StatusInternalServerError)
		return
	}

	// URLからプロジェクト名を取得
	projectName := r.PathValue("project_name")
	if projectName == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	// 既存プロジェクトの取得
	existingProject, err := projectStore.GetProject(r.Context(), projectName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "project not found" {
			http.Error(w, fmt.Sprintf("Project '%s' not found", projectName), http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Error retrieving project: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// リクエストボディの読み取り
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// JSONのパース
	var updateData struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &updateData); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	// プロジェクトの更新
	existingProject.Description = updateData.Description
	existingProject.UpdatedAt = time.Now()

	// データベースに保存
	if err := projectStore.UpdateProject(r.Context(), existingProject); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update project: %v", err), http.StatusInternalServerError)
		return
	}

	// レスポンスの設定
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// 更新されたプロジェクトをJSONとして返す
	if err := json.NewEncoder(w).Encode(existingProject); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// DeleteProjectParams represents parameters for deleting a project.
type DeleteProjectParams struct {
	ProjectName *model.ProjectName
}

// NewDeleteProjectParams creates parameters for project deletion from HTTP request.
func NewDeleteProjectParams(r *http.Request) (*DeleteProjectParams, error) {
	projectName, err := model.NewProjectName(r.PathValue("project_name"))
	if err != nil {
		return nil, err
	}

	return &DeleteProjectParams{
		ProjectName: projectName,
	}, nil
}

// handleDeleteProject はプロジェクト削除をハンドリングします。
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	// パラメータを検証
	params, err := NewDeleteProjectParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// プロジェクト削除の実行
	err = s.store.DeleteProject(r.Context(), params.ProjectName.String())
	if err != nil {
		log.Printf("Error deleting project: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		errorResp := map[string]interface{}{
			"error": fmt.Sprintf("Failed to delete project: %v", err),
			"code":  500,
		}
		if err := json.NewEncoder(w).Encode(errorResp); err != nil {
			log.Printf("Error encoding error response: %v", err)
		}
		return
	}

	// 成功した場合は204 No Contentを返す
	w.WriteHeader(http.StatusNoContent)
}

// BulkDeleteRecordsParams represents parameters for bulk deleting records.
type BulkDeleteRecordsParams struct {
	Project string
	Until   *model.Timestamp
}

// NewBulkDeleteRecordsParams creates parameters for bulk record deletion from HTTP request.
func NewBulkDeleteRecordsParams(query url.Values) (*BulkDeleteRecordsParams, error) {
	project := query.Get("project")

	untilStr := query.Get("until")
	if untilStr == "" {
		return nil, fmt.Errorf("until parameter is required")
	}

	until, err := model.NewTimestamp(untilStr)
	if err != nil {
		return nil, err
	}

	return &BulkDeleteRecordsParams{
		Project: project,
		Until:   until,
	}, nil
}

// handleBulkDeleteRecords は条件に一致するレコードをまとめて削除するハンドラーです。
func (s *Server) handleBulkDeleteRecords(w http.ResponseWriter, r *http.Request) {
	// パラメータを検証
	params, err := NewBulkDeleteRecordsParams(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// レコードの一括削除を実行
	count, err := s.store.DeleteRecordsUntil(r.Context(), params.Project, params.Until.Time())
	if err != nil {
		log.Printf("Error deleting records until specified date: %v", err)
		http.Error(w, "Failed to delete records", http.StatusInternalServerError)
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

// parseInt は文字列を整数に変換し、エラーハンドリングを行います。
func parseInt(s string) (int, error) {
	var value int
	var err error
	if _, err = fmt.Sscanf(s, "%d", &value); err != nil {
		return 0, err
	}
	return value, nil
}


// Run はサーバーを指定されたアドレスで起動します。
func (s *Server) Run(addr string) error {
	log.Printf("Server starting on %s", addr)
	return http.ListenAndServe(addr, s)
}
