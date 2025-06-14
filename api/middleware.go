// Package api はsougenのAPIサーバー実装を提供します。
package api

import (
	"encoding/json"
	"net/http"
)

// authMiddleware はAPIリクエストの認証を行うミドルウェアです。
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ヘッダーからAPIキーを取得
		apiKey := r.Header.Get("X-API-Key")

		// APIキーがサーバー側で設定されていない場合はエラー
		if s.config.APIKey == "" {
			type errorResponse struct {
				Error string `json:"error"`
				Code  int    `json:"code"`
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(errorResponse{
				Error: "API authentication is not configured on server",
				Code:  http.StatusInternalServerError,
			})
			return
		}

		// APIキーが一致するか確認
		if apiKey != s.config.APIKey {
			type errorResponse struct {
				Error string `json:"error"`
				Code  int    `json:"code"`
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(errorResponse{
				Error: "Unauthorized: Invalid API key",
				Code:  http.StatusUnauthorized,
			})
			return
		}

		// 認証成功：次のハンドラーを呼び出し
		next.ServeHTTP(w, r)
	})
}
