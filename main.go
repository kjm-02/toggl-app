package main

import (
	"database/sql"
	"html/template"
	"log"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"

	"github.com/gin-gonic/gin"
)

var static *template.Template

func main() {
	godotenv.Load()

	// autheticatorの初期化
	auth, auth_err := NewAuthenticator()
	if auth_err != nil {
		log.Fatalf("Failed to initialize the authenticator: %v", auth_err)
	}

	// DB初期化
	var repo RealRepo
	var err any
	DB_PATH := os.Getenv("DB_PATH")
	repo.DB, err = sql.Open("mysql", "root:"+DB_PATH)
	if err != nil {
		log.Fatal(err)
	}

	if err := repo.DB.Ping(); err != nil {
		log.Fatal(err)
	}

	engine := gin.Default()

	initSessionStore(engine)

	engine.SetFuncMap(template.FuncMap{
		"replace": func(s, old, new string) string {
			return strings.ReplaceAll(s, old, new)
		},
	})

	engine.Static("/static", "./static")
	engine.LoadHTMLGlob("templates/*")

	// ログイン
	//engine.GET("/", HomeHandler)
	engine.GET("/", WorksHandler(repo))
	engine.GET("/login", LoginHandler(auth))
	engine.GET("/callback", CallbackHandler(auth, repo))
	engine.GET("/user", UserHandler(auth))
	engine.GET("/logout", LogoutHandler(auth))
	// MS Graph関係
	//engine.GET("/mslogin", MSLogin)
	//engine.GET("/msgraph-callback", MSGraphCallback)

	// 以下認可必要
	api := engine.Group("/")
	api.Use(repo.SaveUserToDB)
	// ユーザー情報の更新
	api.PATCH("/user/update", UpdateUserHandler(auth))
	// 作業情報
	api.POST("/works/start", CreateWorkHandler(repo))
	api.POST("/works/end", EndWorkHandler(repo))
	api.PATCH("/works/:id", EditWorkHandler(repo))
	api.DELETE("/works/:id", DeleteWorkHandler(repo))

	//api.POST("/msgraph/teams", PostToChat)

	// バックエンドapi
	backend_api := engine.Group("/api/core/")
	backend_api.Use(AccessTokenParseForAPI)
	backend_api.GET("/works", GetWorksAPI(repo))
	backend_api.GET("/works_summary", GetWorksSummaryAPI(repo))
	backend_api.POST("works/start", CreateWorksAPI(repo))
	backend_api.POST("works/end", EndWorksAPI(repo))
	backend_api.PATCH("works/:id", UpdateWorksAPI(repo))
	backend_api.DELETE("works/:id", DeleteWorksAPI(repo))

	engine.Run(":3000")
}
