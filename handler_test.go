package main

import (
	"fmt"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

type StubRepo struct{}

func (s StubRepo) GetWorks(auth0ID, date string) ([]Works, int, error) {
	return []Works{}, 100, nil
}

func (s StubRepo) GetSummary(auth0ID, date string) ([]WorkSummary, error) {
	return []WorkSummary{}, nil
}

func (s StubRepo) CreateWork(auth0_id string, req reportRequestBody) error {
	return nil
}
func (s StubRepo) EndWork(auth0_id string, req reportRequestBody) error {
	return nil
}
func (s StubRepo) UpdateWork(auth0_id string, req Works, work_id string) error {
	return nil
}
func (s StubRepo) DeleteWork(auth0_id string, work_id string) error {
	return nil
}

type ErrorStubRepo struct{}

func (e ErrorStubRepo) GetWorks(auth0ID, date string) ([]Works, int, error) {
	return nil, 0, fmt.Errorf("db error")
}

func (e ErrorStubRepo) GetSummary(auth0ID, date string) ([]WorkSummary, error) {
	return nil, nil
}

func (s ErrorStubRepo) CreateWork(auth0_id string, req reportRequestBody) error {
	return nil
}
func (s ErrorStubRepo) EndWork(auth0_id string, req reportRequestBody) error {
	return nil
}
func (s ErrorStubRepo) UpdateWork(auth0_id string, req Works, work_id string) error {
	return nil
}
func (s ErrorStubRepo) DeleteWork(auth0_id string, work_id string) error {
	return nil
}

func TestWorksHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		repo       WorkReader
		query      string
		wantStatus int
	}{
		{
			name:       "正常系",
			repo:       StubRepo{},
			query:      "date=2024-01-01",
			wantStatus: 200,
		},
		{
			name:       "dateなし（リダイレクト）",
			repo:       StubRepo{},
			query:      "",
			wantStatus: 302,
		},
		{
			name:       "DBエラー",
			repo:       ErrorStubRepo{},
			query:      "date=2024-01-01",
			wantStatus: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.SetFuncMap(template.FuncMap{
				"replace": func(s, old, new string) string {
					return strings.ReplaceAll(s, old, new)
				},
			})
			r.LoadHTMLGlob("templates/*")

			// session対応
			store := cookie.NewStore([]byte("secret"))
			r.Use(sessions.Sessions("test-session", store))

			r.GET("/", WorksHandler(tt.repo))

			url := "/"
			if tt.query != "" {
				url += "?" + tt.query
			}

			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("got %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestCreateWorkHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		repo       WorkWriter
		body       string
		wantStatus int
	}{
		{
			name: "正常系",
			repo: StubRepo{},
			body: `{
			"date": "2026-06-12",
      "remarks": "今日の作業",
      "works": {
				"project_name": "a",
				"work_class": "b",
				"task_name": "c",
				"start_time": "2026-06-12 15:07:00",
				"end_time": null,
				"memo": "実装"
			}}`,
			wantStatus: 200,
		},
		{
			name: "正常系 空のjson",
			repo: StubRepo{},
			body: `{
      }`, // 空でも通る
			wantStatus: 200,
		},
		{
			name:       "異常系 dateにint",
			repo:       ErrorStubRepo{},
			body:       `{"date":123}`, // stringのDateにint入れる
			wantStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.SetFuncMap(template.FuncMap{
				"replace": func(s, old, new string) string {
					return strings.ReplaceAll(s, old, new)
				},
			})
			r.LoadHTMLGlob("templates/*")

			// session対応
			store := cookie.NewStore([]byte("secret"))
			r.Use(sessions.Sessions("test-session", store))

			r.POST("/works/start", CreateWorkHandler(tt.repo))

			url := "/works/start"
			req := httptest.NewRequest("POST", url, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("got %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}
