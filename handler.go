package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/auth0/go-auth0/v2/authentication/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	//"github.com/gorilla/sessions"
)

type userRequestBody struct {
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
	Name     string `json:"name"`
}

type Works struct {
	Work_id      string
	Project_name string
	Work_class   string
	Task_name    string
	Start_time   string
	End_time     string
	Diff_minute  string
	Memo         string
}

type WorkSummary struct {
	Project_name string
	Work_class   string
	Task_name    string
	Total_minute string
}

type reportRequestBody struct {
	Date    string
	Remarks string
	Works   Works
}

type user struct {
	Auth0_id string
	Email    string
	Name     string
}

var access_token_for_management_API string
var mgmt_token_expires time.Time

func init() {
	gob.Register(map[string]interface{}{})
}

func initSessionStore(r *gin.Engine) {
	store := cookie.NewStore([]byte(os.Getenv("SESSION_SECRET")))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})

	r.Use(sessions.Sessions("auth-session", store))
}

// LoginHandler redirects the user to Auth0's Universal Login page.
func LoginHandler(auth *Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		state, err := generateRandomState()
		if err != nil {
			c.String(500, "Internal error")
			return
		}

		session := sessions.Default(c)
		session.Set("state", state)

		if err := session.Save(); err != nil {
			c.String(500, "Internal error")
			return
		}

		c.Redirect(307, auth.AuthorizationURL(state))
	}
}

// CallbackHandler handles the callback from Auth0 after authentication.
func CallbackHandler(auth *Authenticator, r RealRepo) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)

		if c.Query("state") != session.Get("state") {
			c.String(400, "Invalid state parameter")
			return
		}

		tokenSet, err := auth.OAuth.LoginWithAuthCode(
			c.Request.Context(),
			oauth.LoginWithAuthCodeRequest{
				Code:        c.Query("code"),
				RedirectURI: auth.CallbackURL,
			},
			oauth.IDTokenValidationOptions{},
		)
		if err != nil {
			c.String(401, "Failed to exchange authorization code for token")
			return
		}

		userInfo, err := auth.UserInfo(c.Request.Context(), tokenSet.AccessToken)
		if err != nil {
			c.String(500, "Failed to get user info")
			return
		}

		session.Set("access_token", tokenSet.AccessToken)
		session.Set("refresh_token", tokenSet.RefreshToken)

		// AccessTokenからsubを保存
		AccessTokenParse(c)
		session.Set("profile", map[string]interface{}{
			"nickname": userInfo.Nickname,
			"name":     userInfo.Name,
			"picture":  userInfo.Picture,
			"email":    userInfo.Email,
		})

		if err := session.Save(); err != nil {
			c.String(500, "Internal error")
			return
		}

		// DBにユーザーを保存
		r.SaveUserToDB(c)

		c.Redirect(307, "/user")
	}
}

// UserHandler displays the authenticated user's profile.
func UserHandler(auth *Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 公式のサンプルコードはセッションからとってくる形だが、ここでは
		// atuh.UserInfoを使って毎回最新情報をとる形にする
		// また、refresh tokenを利用するように修正する
		session := sessions.Default(c)
		access_token := session.Get("access_token").(string)
		userInfo, user_err := auth.UserInfo(c.Request.Context(), access_token)
		if user_err != nil {
			// リフレッシュトークンを利用してアクセストークン再取得（ぶっちゃけ今のSSR型構成だと意味がないが、勉強のためにやる）
			refresh_token := session.Get("refresh_token").(string)
			newTokenSet, refresh_err := auth.OAuth.RefreshToken(c.Request.Context(), oauth.RefreshTokenRequest{
				RefreshToken: refresh_token,
			}, oauth.IDTokenValidationOptions{})
			if refresh_err != nil {
				// リフレッシュトークンが無効なら/logoutにリダイレクト
				session.Clear()
				session.Save()
				c.Redirect(303, "/logout")
				return
			}
			session.Set("access_token", newTokenSet.AccessToken)

			// 再度UserInfo取得
			userInfo, user_err = auth.UserInfo(c.Request.Context(), newTokenSet.AccessToken)
			if user_err != nil {
				c.String(401, "Failed to fetch user info after refresh")
				return
			}
			// AccessTokenからsubを保存
			AccessTokenParse(c)
		}

		if err := session.Save(); err != nil {
			c.String(500, "Internal error")
			return
		}

		session.Set("profile", map[string]interface{}{
			"nickname": userInfo.Nickname,
			"name":     userInfo.Name,
			"picture":  userInfo.Picture,
			"email":    userInfo.Email,
		})

		profile, ok := session.Get("profile").(map[string]interface{})
		if !ok {
			c.Redirect(303, "/")
			return
		}

		c.HTML(200, "user.html", profile)
	}
}

// LogoutHandler clears the session and redirects to Auth0's logout endpoint.
func LogoutHandler(auth *Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)

		session.Clear()
		session.Options(sessions.Options{
			MaxAge: -1,
		})
		session.Save()
		logoutURL, _ := url.Parse("https://" + auth.Domain + "/v2/logout")

		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}

		returnTo := scheme + "://" + c.Request.Host

		params := url.Values{}
		params.Add("returnTo", returnTo)
		params.Add("client_id", auth.ClientID)
		logoutURL.RawQuery = params.Encode()

		c.Redirect(307, logoutURL.String())
	}
}

func generateRandomState() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

var layout = "2006-01-02 15:04"

func stringToTime(str string) (time.Time, error) {
	return time.Parse(layout, str)
}

func WorksHandler(repo WorkReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		report_date := c.Query("date")

		if report_date == "" {
			today := time.Now().Format("2006-01-02")
			c.Redirect(302, "/?date="+today)
			return
		}

		session := sessions.Default(c)

		auth0_id, _ := session.Get("auth0_id").(string)

		report_date = c.Query("date")

		var is_login bool
		if session.Get("profile") != nil {
			is_login = true
		}

		// worksの取得
		works, sum, works_error := repo.GetWorks(auth0_id, report_date)

		if works_error != nil {
			c.JSON(500, gin.H{"error": works_error.Error()})
			return
		}

		// teams出力用にGroupby
		summaries, summay_err := repo.GetSummary(auth0_id, report_date)
		if summay_err != nil {
			c.JSON(500, gin.H{"error": summay_err.Error()})
			return
		}

		c.HTML(200, "home.html", gin.H{
			"Is_login":  is_login,
			"Date":      report_date,
			"Works":     works,
			"Sum":       sum,
			"Summaries": summaries,
		})
	}
}

// 作業の開始
func CreateWorkHandler(repo WorkWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		v := session.Get("auth0_id")
		auth0_id, ok := v.(string)
		if !ok {
			auth0_id = ""
		}

		var req reportRequestBody
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{
				"error":   err.Error(),
				"message": req,
			})
			return
		}

		err := repo.CreateWork(auth0_id, req)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
		}

		c.JSON(200, gin.H{"status": "ok"})
	}
}

// 作業の終了
func EndWorkHandler(repo WorkWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		auth0_id := session.Get("auth0_id").(string)

		var req reportRequestBody
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{
				"error":   err.Error(),
				"message": req,
			})
			return
		}

		repo.EndWork(auth0_id, req)
		c.JSON(200, gin.H{"status": "ok"})
	}
}

// 作業の編集
func EditWorkHandler(repo WorkWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		auth0_id := session.Get("auth0_id").(string)
		work_id := c.Param("id")

		var req Works
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{
				"error":   err.Error(),
				"message": req,
			})
			return
		}

		repo.UpdateWork(auth0_id, req, work_id)
		c.JSON(200, gin.H{"status": "ok"})
	}
}

// 作業の削除
func DeleteWorkHandler(repo WorkWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		auth0_id := session.Get("auth0_id").(string)
		work_id := c.Param("id")

		repo.DeleteWork(auth0_id, work_id)
		c.JSON(200, gin.H{"status": "ok"})
	}
}

func GetTokenForManagementAPI(c *gin.Context) string {
	// まだ有効ならそのまま返す
	if access_token_for_management_API != "" && time.Now().Before(mgmt_token_expires) {
		return access_token_for_management_API
	}
	// 以下取得処理
	domain := os.Getenv("AUTH0_DOMAIN")
	clientID := os.Getenv("AUTH0_CLIENT_ID_FOR_MANAGEMENT_API")
	clientSecret := os.Getenv("AUTH0_CLIENT_SECRET_FOR_MANAGEMENT_API")

	url := "https://" + domain + "/oauth/token"

	payload := strings.NewReader(fmt.Sprintf(`{
			"client_id": "%s",
			"client_secret": "%s",
			"audience": "https://%s/api/v2/",
			"grant_type": "client_credentials"
		}`, clientID, clientSecret, domain))
	req, err := http.NewRequest("POST", url, payload)
	if err != nil {
		c.JSON(500, gin.H{"err": "Cannot get access_token for management API"})
	}

	req.Header.Add("content-type", "application/json")

	res, _ := http.DefaultClient.Do(req)

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	//log.Println(string(body))

	var result map[string]interface{}
	json.Unmarshal(body, &result)
	mgmt_token_expires = time.Now().Add(time.Duration(result["expires_in"].(float64)) * time.Second)

	return result["access_token"].(string)
}

// Auth0 managementAPIを使ってユーザー情報更新
// エラー処理を全くしていないので対応する
func UpdateUserHandler(auth *Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		// Management APIのためのtoken取得、全ユーザーで共通のものを使えばよい
		access_token_for_management_API = GetTokenForManagementAPI(c)
		//session.Set("access_token_for_management_API", access_token_for_management_API)

		// tokenを使ってManagement API叩く
		auth0_id := session.Get("auth0_id").(string)
		updateUserURL, _ := url.Parse("https://" + auth.Domain + "/api/v2/users/auth0|" + auth0_id)

		var reqBody userRequestBody
		if err := c.ShouldBindJSON(&reqBody); err != nil {
			c.JSON(400, gin.H{
				"error":   err.Error(),
				"message": reqBody,
			})
			return
		}

		jsonData, _ := json.Marshal(reqBody)
		req, err := http.NewRequest("PATCH", updateUserURL.String(), bytes.NewBuffer(jsonData))
		if err != nil {
			c.JSON(500, gin.H{"err": "error"})
		}

		req.Header.Set("Authorization", "Bearer "+access_token_for_management_API)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(500, gin.H{"err": "error"})
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		var result map[string]interface{}
		json.Unmarshal(body, &result)
	}
}
