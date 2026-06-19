package main

import (
	"log"
	"strings"

	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/auth0/go-auth0/v2/authentication"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	//"github.com/golang-jwt/jwt/v4"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// Authenticator wraps the go-auth0 authentication client.
type Authenticator struct {
	*authentication.Authentication
	Domain      string
	ClientID    string
	CallbackURL string
}

// NewAuthenticator creates and configures a new Authenticator.
func NewAuthenticator() (*Authenticator, error) {
	domain := os.Getenv("AUTH0_DOMAIN")
	clientID := os.Getenv("AUTH0_CLIENT_ID")
	clientSecret := os.Getenv("AUTH0_CLIENT_SECRET")
	callbackURL := os.Getenv("AUTH0_CALLBACK_URL")

	authClient, err := authentication.New(
		context.Background(),
		domain,
		authentication.WithClientID(clientID),
		authentication.WithClientSecret(clientSecret),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize authentication client: %w", err)
	}

	return &Authenticator{
		Authentication: authClient,
		Domain:         domain,
		ClientID:       clientID,
		CallbackURL:    callbackURL,
	}, nil
}

// AuthorizationURL builds the /authorize URL to redirect users
// to Auth0's Universal Login page.
func (a *Authenticator) AuthorizationURL(state string) string {
	u, _ := url.Parse("https://" + a.Domain + "/authorize")
	params := url.Values{
		"response_type": {"code"},
		"client_id":     {a.ClientID},
		"redirect_uri":  {a.CallbackURL},
		"scope":         {"openid profile email offline_access"},
		"state":         {state},
		"audience": {
			"http://localhost:3000/",
			//"https://dev-jwxo311negg64d1c.us.auth0.com/api/v2/",
		},
	}
	u.RawQuery = params.Encode()
	return u.String()
}

func AccessTokenParse(c *gin.Context) {
	session := sessions.Default(c)
	access_token := session.Get("access_token").(string)

	// JWKS取得
	Domain := os.Getenv("AUTH0_DOMAIN")
	jwksURL := "https://" + Domain + "/.well-known/jwks.json"
	set, err := jwk.Fetch(context.Background(), jwksURL)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "failed to fetch jwks"})
		return
	}

	// JWT検証
	token, err := jwt.Parse(
		[]byte(access_token),
		jwt.WithKeySet(set),
		jwt.WithValidate(true),
	)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	// sub（ユーザーID）
	sub, ok := token.Get("sub")
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid claims"})
		return
	}
	auth0_id := strings.Split(sub.(string), "|")[1]
	session.Set("auth0_id", auth0_id)
}

func AccessTokenParseForAPI(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")

	if !strings.HasPrefix(authHeader, "Bearer ") {
		c.JSON(401, gin.H{"error": "invalid token"})
		return
	}
	access_token := strings.TrimPrefix(authHeader, "Bearer ")

	// JWKS取得
	Domain := os.Getenv("AUTH0_DOMAIN")
	jwksURL := "https://" + Domain + "/.well-known/jwks.json"
	log.Println(jwksURL)
	set, err := jwk.Fetch(context.Background(), jwksURL)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "failed to fetch jwks"})
		return
	}

	// JWT検証
	token, err := jwt.Parse(
		[]byte(access_token),
		jwt.WithKeySet(set),
		jwt.WithValidate(true),
	)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	// sub（ユーザーID）
	sub, ok := token.Get("sub")
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid claims"})
		return
	}
	auth0_id := strings.Split(sub.(string), "|")[1]
	c.Set("auth0_id", auth0_id)
}
