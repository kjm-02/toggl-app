package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/auth0/go-auth0/v2/authentication"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

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

	for _, setting := range []struct {
		name  string
		value string
	}{
		{"AUTH0_DOMAIN", domain},
		{"AUTH0_CLIENT_ID", clientID},
		{"AUTH0_CLIENT_SECRET", clientSecret},
		{"AUTH0_CALLBACK_URL", callbackURL},
	} {
		if strings.TrimSpace(setting.value) == "" {
			return nil, fmt.Errorf("%s is required", setting.name)
		}
	}
	if _, err := auth0Issuer(domain); err != nil {
		return nil, err
	}
	callback, err := url.Parse(callbackURL)
	if err != nil || callback.Host == "" || (callback.Scheme != "https" && callback.Scheme != "http") || callback.User != nil || callback.Fragment != "" {
		return nil, fmt.Errorf("AUTH0_CALLBACK_URL must be an absolute HTTP or HTTPS URL without user info or fragment")
	}

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
	u := url.URL{Scheme: "https", Host: a.Domain, Path: "/authorize"}
	params := url.Values{
		"response_type": {"code"},
		"client_id":     {a.ClientID},
		"redirect_uri":  {a.CallbackURL},
		"scope":         {"openid profile email offline_access"},
		"state":         {state},
		"audience":      {auth0Audience()},
	}
	u.RawQuery = params.Encode()
	return u.String()
}

func AccessTokenParse(c *gin.Context) {
	session := sessions.Default(c)
	accessToken, ok := session.Get("access_token").(string)
	if !ok || strings.TrimSpace(accessToken) == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	auth0ID, ok := accessTokenUserID(c, accessToken)
	if !ok {
		return
	}
	session.Set("auth0_id", auth0ID)
}

func AccessTokenParseForAPI(c *gin.Context) {
	fields := strings.Fields(c.GetHeader("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	auth0ID, ok := accessTokenUserID(c, fields[1])
	if !ok {
		return
	}
	c.Set("auth0_id", auth0ID)
}

func auth0Audience() string {
	if audience := os.Getenv("AUTH0_AUDIENCE"); audience != "" {
		return audience
	}
	return "http://localhost:3000/"
}

func auth0Issuer(domain string) (string, error) {
	u := url.URL{Scheme: "https", Host: domain, Path: "/"}
	parsed, err := url.Parse(u.String())
	if err != nil || domain == "" || parsed.Host != domain || parsed.Hostname() == "" || parsed.User != nil || strings.ContainsAny(domain, "/?#@ \t\r\n") {
		return "", fmt.Errorf("AUTH0_DOMAIN must be a hostname, optionally with a port, without a scheme or path")
	}
	return u.String(), nil
}

func accessTokenUserID(c *gin.Context, accessToken string) (string, bool) {
	issuer, err := auth0Issuer(os.Getenv("AUTH0_DOMAIN"))
	if err != nil {
		log.Printf("Invalid authentication configuration: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid authentication configuration"})
		return "", false
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	set, err := fetchJWKS(ctx, issuer+".well-known/jwks.json")
	if err != nil {
		log.Printf("Failed to fetch authentication keys: %v", err)
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": "failed to fetch jwks"})
		return "", false
	}

	auth0ID, err := parseAccessToken(accessToken, set, issuer, auth0Audience())
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return "", false
	}
	return auth0ID, true
}

func fetchJWKS(ctx context.Context, endpoint string) (jwk.Set, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned HTTP %d", resp.StatusCode)
	}
	return jwk.ParseReader(resp.Body)
}

func parseAccessToken(accessToken string, set jwk.Set, issuer, audience string) (string, error) {
	token, err := jwt.Parse(
		[]byte(accessToken),
		jwt.WithKeySet(set),
		jwt.WithValidate(true),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
		jwt.WithRequiredClaim(jwt.ExpirationKey),
		jwt.WithRequiredClaim(jwt.SubjectKey),
	)
	if err != nil {
		return "", fmt.Errorf("validate access token: %w", err)
	}

	// Keep the provider-local ID used by existing database records.
	provider, auth0ID, ok := strings.Cut(token.Subject(), "|")
	if !ok || strings.TrimSpace(provider) == "" || strings.TrimSpace(auth0ID) == "" {
		return "", fmt.Errorf("invalid subject claim")
	}
	return auth0ID, nil
}
