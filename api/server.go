// Package api はsougenのAPIサーバー実装を提供します。
package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
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

	// Project and Record endpoints
	securedHandler.HandleFunc("GET /api/v0/p/{project_name}", s.handleGetProject)
	securedHandler.HandleFunc("DELETE /api/v0/p/{project_name}", s.handleDeleteProject)
	securedHandler.HandleFunc("POST /api/v0/p/{project_name}/r", s.handleCreateRecord)
	securedHandler.HandleFunc("GET /api/v0/p/{project_name}/r", s.handleListRecords)
	securedHandler.HandleFunc("GET /api/v0/p/{project_name}/r/{record_id}", s.handleGetRecord)
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

// handleCreateRecord はレコード作成エンドポイントのハンドラーです。
func (s *Server) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
	// URLからプロジェクト名を取得
	projectName := r.PathValue("project_name")
	if projectName == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	// テンプレートクエリパラメータを取得
	templateParam := r.URL.Query().Get("template")

	// リクエストボディからデータを読み込み
	var reqBody struct {
    Timestamp string   `json:"timestamp"` // ISO8601形式 "2006-01-02T15:04:05Z", 省略可能
		Value  *int      `json:"value"`     // レコードの値, 省略可能
		Tags   []string  `json:"tags"`      // タグ一覧, 省略可能
	}

	// リクエストボディが存在する場合はデコード
	if r.ContentLength > 0 {
		if templateParam != "" {
			// テンプレートパラメータが指定されている場合、元のボディを変換
			transformedBody, err := s.transformRequestBody(r.Body, templateParam)
			if err != nil {
				http.Error(w, fmt.Sprintf("Template transformation failed: %v", err), http.StatusBadRequest)
				return
			}
			// 変換されたボディをデコード
			if err := json.NewDecoder(strings.NewReader(transformedBody)).Decode(&reqBody); err != nil {
				http.Error(w, "Invalid request body after template transformation", http.StatusBadRequest)
				return
			}
		} else {
			// 通常の処理
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}
		}
	}

	// timestmap の処理: 省略された場合は現在時刻を使用
	var timestamp time.Time
	if reqBody.Timestamp == "" {
		// timestamp が省略された場合は現在時刻を使用
		timestamp = time.Now()
	} else {
		// 文字列からtime.Timeに変換
		var err error
		timestamp, err = time.Parse(time.RFC3339, reqBody.Timestamp)
		if err != nil {
			http.Error(w, "Invalid datetime format. Use ISO8601 format (YYYY-MM-DDThh:mm:ssZ)", http.StatusBadRequest)
			return
		}
	}

	// value の処理: 省略された場合はデフォルト値1を使用
	if reqBody.Value == nil {
		defaultValue := 1
		reqBody.Value = &defaultValue
	}

	// value のチェック: 必ず1以上の整数であることを確認
	if *reqBody.Value < 1 {
		http.Error(w, "Value must be a positive integer greater than 0", http.StatusBadRequest)
		return
	}

	// 新しいレコードの作成
	record, err := model.NewRecord(timestamp, projectName, *reqBody.Value, reqBody.Tags)
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

// handleGetRecord は特定のIDのレコードを取得するハンドラーです。
func (s *Server) handleGetRecord(w http.ResponseWriter, r *http.Request) {
	// URLからプロジェクト名を取得
	projectName := r.PathValue("project_name")
	if projectName == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	// URLからレコードIDを取得
	recordID := r.PathValue("record_id")
	if recordID == "" {
		http.Error(w, "Record ID is required", http.StatusBadRequest)
		return
	}

	// IDが有効なUUIDかチェック
	id, err := uuid.Parse(recordID)
	if err != nil {
		http.Error(w, "Invalid UUID format", http.StatusBadRequest)
		return
	}

	// レコードの取得
	record, err := s.store.GetRecord(r.Context(), id)
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
	if record.Project != projectName {
		http.Error(w, "Record not found in this project", http.StatusNotFound)
		return
	}

	// レスポンスの返却
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(record); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// handleDeleteRecord は特定のIDのレコードを削除するハンドラーです。
func (s *Server) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	// URLからプロジェクト名を取得
	projectName := r.PathValue("project_name")
	if projectName == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	// URLからレコードIDを取得
	recordID := r.PathValue("record_id")
	if recordID == "" {
		http.Error(w, "Record ID is required", http.StatusBadRequest)
		return
	}

	// IDが有効なUUIDかチェック
	id, err := uuid.Parse(recordID)
	if err != nil {
		http.Error(w, "Invalid UUID format", http.StatusBadRequest)
		return
	}

	// 削除前にレコードが存在するかつ指定プロジェクトのものかを確認
	record, err := s.store.GetRecord(r.Context(), id)
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
	if record.Project != projectName {
		http.Error(w, "Record not found in this project", http.StatusNotFound)
		return
	}

	// レコードの削除
	if err := s.store.DeleteRecord(r.Context(), id); err != nil {
		log.Printf("Error deleting record: %v", err)
		http.Error(w, "Failed to delete record", http.StatusInternalServerError)
		return
	}

	// 削除成功のレスポンスを返す
	w.WriteHeader(http.StatusNoContent)
}

// handleGetGraph は指定プロジェクトのヒートマップグラフを生成・返却するハンドラーです。
func (s *Server) handleGetGraph(w http.ResponseWriter, r *http.Request) {
	// URLからプロジェクト名を取得
	projectName := r.PathValue("project_name")
	if projectName == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	// クエリパラメータの解析（from, to）
	query := r.URL.Query()

	// アクセスカウンター機能: trackパラメータがある場合、レコードを自動作成
	if query.Has("track") {
		// 新しいレコードの作成（現在時刻、値は1）
		record, err := model.NewRecord(time.Now(), projectName, 1, nil)
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

	// デフォルトの日付範囲を設定: 1年前から今日まで
	now := time.Now()
	defaultFrom := now.AddDate(-1, 0, 0)
	defaultTo := now

	// クエリパラメータからfrom日時を取得
	fromStr := query.Get("from")
	var fromTime time.Time
	var err error
	if fromStr != "" {
		fromTime, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			http.Error(w, "Invalid from parameter. Use ISO8601 format (YYYY-MM-DDThh:mm:ssZ)", http.StatusBadRequest)
			return
		}
	} else {
		fromTime = defaultFrom
	}

	// クエリパラメータからto日時を取得
	toStr := query.Get("to")
	var toTime time.Time
	if toStr != "" {
		toTime, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			http.Error(w, "Invalid to parameter. Use ISO8601 format (YYYY-MM-DDThh:mm:ssZ)", http.StatusBadRequest)
			return
		}
	} else {
		toTime = defaultTo
	}

	// tagsクエリパラメータの処理
	tagsStr := query.Get("tags")
	var records []*model.Record

	if tagsStr != "" {
		// カンマ区切りでタグを分割
		tags := strings.Split(tagsStr, ",")
		// 空白を削除
		for i, tag := range tags {
			tags[i] = strings.TrimSpace(tag)
		}
		// 空のタグを除去
		var filteredTags []string
		for _, tag := range tags {
			if tag != "" {
				filteredTags = append(filteredTags, tag)
			}
		}

		if len(filteredTags) > 0 {
			// タグフィルタありのレコード取得
			records, err = s.store.ListRecordsWithTags(r.Context(), projectName, fromTime, toTime, filteredTags)
		} else {
			// タグが空の場合は通常のレコード取得
			records, err = s.store.ListRecords(r.Context(), projectName, fromTime, toTime)
		}
	} else {
		// タグフィルタなしのレコード取得
		records, err = s.store.ListRecords(r.Context(), projectName, fromTime, toTime)
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

	// 日付範囲内のすべての日を処理するための日付の作成
	// fromTimeとtoTimeを日付のみに切り詰め
	fromDate := time.Date(fromTime.Year(), fromTime.Month(), fromTime.Day(), 0, 0, 0, 0, fromTime.Location())
	toDate := time.Date(toTime.Year(), toTime.Month(), toTime.Day(), 0, 0, 0, 0, toTime.Location())

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
	// NOTE: Dataは昇順であることを前提としている
	svg := heatmap.GenerateYearlyHeatmapSVG(data, nil)

	// レスポンスの返却
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write([]byte(svg))
}

// handleListRecords はプロジェクトに属するレコードの一覧を取得するハンドラーです。
func (s *Server) handleListRecords(w http.ResponseWriter, r *http.Request) {
	// URLからプロジェクト名を取得
	projectName := r.PathValue("project_name")
	if projectName == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	// クエリパラメータの解析
	query := r.URL.Query()

	// デフォルトの日付範囲を設定: 1年前から今日まで
	now := time.Now()
	defaultFrom := now.AddDate(-1, 0, 0)
	defaultTo := now

	// クエリパラメータからfrom日時を取得
	fromStr := query.Get("from")
	var fromTime time.Time
	var err error
	if fromStr != "" {
		fromTime, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			http.Error(w, "Invalid from parameter. Use ISO8601 format (YYYY-MM-DDThh:mm:ssZ)", http.StatusBadRequest)
			return
		}
	} else {
		fromTime = defaultFrom
	}

	// クエリパラメータからto日時を取得
	toStr := query.Get("to")
	var toTime time.Time
	if toStr != "" {
		toTime, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			http.Error(w, "Invalid to parameter. Use ISO8601 format (YYYY-MM-DDThh:mm:ssZ)", http.StatusBadRequest)
			return
		}
	} else {
		toTime = defaultTo
	}

	// tagsクエリパラメータの処理
	tagsStr := query.Get("tags")
	var records []*model.Record

	if tagsStr != "" {
		// カンマ区切りでタグを分割
		tags := strings.Split(tagsStr, ",")
		// 空白を削除
		for i, tag := range tags {
			tags[i] = strings.TrimSpace(tag)
		}
		// 空のタグを除去
		var filteredTags []string
		for _, tag := range tags {
			if tag != "" {
				filteredTags = append(filteredTags, tag)
			}
		}

		if len(filteredTags) > 0 {
			// タグフィルタありのレコード取得
			records, err = s.store.ListRecordsWithTags(r.Context(), projectName, fromTime, toTime, filteredTags)
		} else {
			// タグが空の場合は通常のレコード取得
			records, err = s.store.ListRecords(r.Context(), projectName, fromTime, toTime)
		}
	} else {
		// タグフィルタなしのレコード取得
		records, err = s.store.ListRecords(r.Context(), projectName, fromTime, toTime)
	}

	if err != nil {
		log.Printf("Error retrieving records: %v", err)
		http.Error(w, "Failed to retrieve records", http.StatusInternalServerError)
		return
	}

	// ページネーションパラメータの取得
	limit := 100 // デフォルト値
	offset := 0  // デフォルト値

	// limitパラメータの解析
	limitStr := query.Get("limit")
	if limitStr != "" {
		parsedLimit, err := parseInt(limitStr)
		if err != nil {
			http.Error(w, "Invalid limit parameter: must be a positive integer", http.StatusBadRequest)
			return
		}
		if parsedLimit <= 0 {
			http.Error(w, "Limit must be greater than 0", http.StatusBadRequest)
			return
		}
		if parsedLimit > 1000 { // 上限を設定
			parsedLimit = 1000
		}
		limit = parsedLimit
	}

	// offsetパラメータの解析
	offsetStr := query.Get("offset")
	if offsetStr != "" {
		parsedOffset, err := parseInt(offsetStr)
		if err != nil {
			http.Error(w, "Invalid offset parameter: must be a non-negative integer", http.StatusBadRequest)
			return
		}
		if parsedOffset < 0 {
			http.Error(w, "Offset must be non-negative", http.StatusBadRequest)
			return
		}
		offset = parsedOffset
	}

	// ページネーションの適用
	totalRecords := len(records)
	endIndex := offset + limit
	if endIndex > totalRecords {
		endIndex = totalRecords
	}

	// 指定された範囲のレコードのみを抽出
	var pagedRecords []*model.Record
	if offset < totalRecords {
		pagedRecords = records[offset:endIndex]
	} else {
		pagedRecords = []*model.Record{} // offsetが範囲外の場合は空配列
	}

	// レスポンスの返却
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pagedRecords); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// handleGetProject はプロジェクト情報取得をハンドリングします。
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	// URLからプロジェクト名を取得
	projectName := r.PathValue("project_name")
	if projectName == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	// プロジェクト情報の取得
	projectInfo, err := s.store.GetProjectInfo(r.Context(), projectName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, fmt.Sprintf("Project '%s' not found", projectName), http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Error retrieving project info: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// レスポンスの設定
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// JSONとしてレスポンスを返す
	if err := json.NewEncoder(w).Encode(projectInfo); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleDeleteProject はプロジェクト削除をハンドリングします。
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	// URLからプロジェクト名を取得
	projectName := r.PathValue("project_name")
	if projectName == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	// プロジェクト削除の実行
	err := s.store.DeleteProject(r.Context(), projectName)
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

// handleBulkDeleteRecords は条件に一致するレコードをまとめて削除するハンドラーです。
func (s *Server) handleBulkDeleteRecords(w http.ResponseWriter, r *http.Request) {
	// クエリパラメータの解析
	query := r.URL.Query()

	// projectパラメータの取得（オプション）
	project := query.Get("project")

	// 必須パラメータ: until (この日時より前のレコードを削除)
	untilStr := query.Get("until")
	if untilStr == "" {
		http.Error(w, "until parameter is required", http.StatusBadRequest)
		return
	}

	// 日時のパース
	untilTime, err := time.Parse(time.RFC3339, untilStr)
	if err != nil {
		http.Error(w, "Invalid until parameter. Use ISO8601 format (YYYY-MM-DDThh:mm:ssZ)", http.StatusBadRequest)
		return
	}

	// レコードの一括削除を実行
	count, err := s.store.DeleteRecordsUntil(r.Context(), project, untilTime)
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

// transformRequestBody はGoテンプレートを使用してリクエストボディを変換します。
func (s *Server) transformRequestBody(body io.Reader, templateStr string) (string, error) {
	// リクエストボディを読み取り
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("failed to read request body: %w", err)
	}

	// 元のJSONをmapとしてパース
	var originalData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &originalData); err != nil {
		return "", fmt.Errorf("failed to parse original JSON: %w", err)
	}

	// Goテンプレートをパース
	tmpl, err := template.New("transform").Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// テンプレートを実行してデータを変換
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, originalData); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// Run はサーバーを指定されたアドレスで起動します。
func (s *Server) Run(addr string) error {
	log.Printf("Server starting on %s", addr)
	return http.ListenAndServe(addr, s)
}
