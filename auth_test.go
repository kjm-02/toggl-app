package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

func testTokenKeys(t *testing.T) (jwk.Key, jwk.Set) {
	t.Helper()
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	key, err := jwk.FromRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]interface{}{
		jwk.KeyIDKey: "test-key", jwk.AlgorithmKey: jwa.RS256,
	} {
		if err := key.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	public, err := jwk.PublicKeyOf(key)
	if err != nil {
		t.Fatal(err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(public); err != nil {
		t.Fatal(err)
	}
	return key, set
}

func testAccessToken(t *testing.T, key jwk.Key, issuer, audience string, changes map[string]interface{}) string {
	t.Helper()
	token := jwt.New()
	claims := map[string]interface{}{
		jwt.IssuerKey: issuer, jwt.AudienceKey: []string{audience},
		jwt.SubjectKey: "auth0|user-123", jwt.ExpirationKey: time.Now().Add(time.Hour),
	}
	for name, value := range changes {
		claims[name] = value
	}
	for name, value := range claims {
		if value == nil {
			continue
		}
		if err := token.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, key))
	if err != nil {
		t.Fatal(err)
	}
	return string(signed)
}

func testAuthServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	originalTransport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	return server
}

func TestParseAccessToken(t *testing.T) {
	key, set := testTokenKeys(t)
	otherKey, _ := testTokenKeys(t)
	const issuer = "https://tenant.example/"
	const audience = "https://api.example/"
	tests := []struct {
		name    string
		changes map[string]interface{}
		wantID  string
	}{
		{name: "valid", wantID: "user-123"},
		{name: "multiple audiences", changes: map[string]interface{}{jwt.AudienceKey: []string{"other", audience}}, wantID: "user-123"},
		{name: "ID containing separator", changes: map[string]interface{}{jwt.SubjectKey: "provider|user|123"}, wantID: "user|123"},
		{name: "wrong issuer", changes: map[string]interface{}{jwt.IssuerKey: "https://other.example/"}},
		{name: "missing issuer", changes: map[string]interface{}{jwt.IssuerKey: nil}},
		{name: "wrong audience", changes: map[string]interface{}{jwt.AudienceKey: []string{"other"}}},
		{name: "missing audience", changes: map[string]interface{}{jwt.AudienceKey: nil}},
		{name: "expired", changes: map[string]interface{}{jwt.ExpirationKey: time.Now().Add(-time.Minute)}},
		{name: "missing expiration", changes: map[string]interface{}{jwt.ExpirationKey: nil}},
		{name: "not yet valid", changes: map[string]interface{}{jwt.NotBeforeKey: time.Now().Add(time.Hour)}},
		{name: "missing subject", changes: map[string]interface{}{jwt.SubjectKey: nil}},
		{name: "empty subject", changes: map[string]interface{}{jwt.SubjectKey: ""}},
		{name: "no separator", changes: map[string]interface{}{jwt.SubjectKey: "user-123"}},
		{name: "empty provider", changes: map[string]interface{}{jwt.SubjectKey: "|user-123"}},
		{name: "empty ID", changes: map[string]interface{}{jwt.SubjectKey: "auth0|"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := testAccessToken(t, key, issuer, audience, tt.changes)
			id, err := parseAccessToken(raw, set, issuer, audience)
			if tt.wantID == "" {
				if err == nil {
					t.Fatalf("expected rejection, got ID %q", id)
				}
			} else if err != nil || id != tt.wantID {
				t.Fatalf("got ID %q, error %v; want %q", id, err, tt.wantID)
			}
		})
	}
	for _, raw := range []string{"not-a-jwt", testAccessToken(t, otherKey, issuer, audience, nil)} {
		if _, err := parseAccessToken(raw, set, issuer, audience); err == nil {
			t.Fatal("accepted malformed token or wrong signature")
		}
	}
}

func TestAccessTokenParseForAPIRejectsInvalidHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, header := range []string{"", "Basic abc", "Bearer", "Bearer ", "Bearer a b"} {
		t.Run(header, func(t *testing.T) {
			r := gin.New()
			called := false
			r.GET("/", AccessTokenParseForAPI, func(c *gin.Context) {
				called = true
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", header)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized || called {
				t.Fatalf("status=%d, downstream called=%v", w.Code, called)
			}
		})
	}
}

func TestAccessTokenParseRejectsInvalidSessionToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, value := range []interface{}{nil, 123, "", "   "} {
		r := gin.New()
		r.Use(sessions.Sessions("test-session", cookie.NewStore([]byte("test-secret"))))
		called := false
		r.GET("/", func(c *gin.Context) {
			if value != nil {
				sessions.Default(c).Set("access_token", value)
			}
		}, AccessTokenParse, func(c *gin.Context) {
			called = true
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusUnauthorized || called {
			t.Fatalf("token=%v: status=%d, downstream called=%v", value, w.Code, called)
		}
	}
}

func TestAccessTokenMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key, set := testTokenKeys(t)
	jwks, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	jwksStatus := http.StatusOK
	server := testAuthServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			t.Errorf("unexpected JWKS path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(jwksStatus)
		_, _ = w.Write(jwks)
	}))
	t.Setenv("AUTH0_DOMAIN", strings.TrimPrefix(server.URL, "https://"))
	t.Setenv("AUTH0_AUDIENCE", "https://api.example/")
	raw := testAccessToken(t, key, server.URL+"/", "https://api.example/", nil)

	for _, sessionMode := range []bool{false, true} {
		for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
			jwksStatus = status
			r := gin.New()
			if sessionMode {
				r.Use(sessions.Sessions("test-session", cookie.NewStore([]byte("test-secret"))))
				r.Use(func(c *gin.Context) { sessions.Default(c).Set("access_token", raw) })
				r.Use(AccessTokenParse)
			} else {
				r.Use(AccessTokenParseForAPI)
			}
			called := false
			r.GET("/", func(c *gin.Context) {
				called = true
				id := c.GetString("auth0_id")
				if sessionMode {
					id, _ = sessions.Default(c).Get("auth0_id").(string)
				}
				if id != "user-123" {
					t.Errorf("got ID %q", id)
				}
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "bearer "+raw)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			wantStatus := http.StatusOK
			if status != http.StatusOK {
				wantStatus = http.StatusBadGateway
			}
			if w.Code != wantStatus || called != (status == http.StatusOK) {
				t.Fatalf("session=%v, JWKS=%d: response=%d, downstream=%v", sessionMode, status, w.Code, called)
			}
		}
	}
}

func TestAuthenticatorConfiguration(t *testing.T) {
	_, set := testTokenKeys(t)
	server := testAuthServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(set); err != nil {
			t.Errorf("encode JWKS: %v", err)
		}
	}))
	settings := map[string]string{
		"AUTH0_DOMAIN": strings.TrimPrefix(server.URL, "https://"), "AUTH0_CLIENT_ID": "client",
		"AUTH0_CLIENT_SECRET": "secret", "AUTH0_CALLBACK_URL": "http://localhost:3000/callback",
	}
	for name, value := range settings {
		t.Setenv(name, value)
	}
	for name := range settings {
		t.Run("missing "+name, func(t *testing.T) {
			t.Setenv(name, "")
			if _, err := NewAuthenticator(); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("expected error naming %s, got %v", name, err)
			}
		})
	}
	for _, domain := range []string{"", "https://tenant.example", "tenant.example/path", "user@tenant.example", "tenant.example?query", "tenant.example#fragment", "bad host"} {
		if _, err := auth0Issuer(domain); err == nil {
			t.Errorf("accepted invalid domain %q", domain)
		}
	}

	for _, callback := range []string{"/callback", "ftp://example.com/callback", "https://user@example.com/callback", "https://example.com/callback#fragment"} {
		t.Run(callback, func(t *testing.T) {
			t.Setenv("AUTH0_CALLBACK_URL", callback)
			if _, err := NewAuthenticator(); err == nil {
				t.Fatalf("accepted invalid callback %q", callback)
			}
		})
	}
	for _, audience := range []string{"", "https://api.example/"} {
		t.Setenv("AUTH0_AUDIENCE", audience)
		auth, err := NewAuthenticator()
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(auth.AuthorizationURL("state & value"))
		if err != nil {
			t.Fatal(err)
		}
		wantAudience := audience
		if wantAudience == "" {
			wantAudience = "http://localhost:3000/"
		}
		params := u.Query()
		if u.Scheme != "https" || u.Host != settings["AUTH0_DOMAIN"] || u.Path != "/authorize" ||
			params.Get("audience") != wantAudience || params.Get("state") != "state & value" ||
			params.Get("client_id") != settings["AUTH0_CLIENT_ID"] || params.Get("redirect_uri") != settings["AUTH0_CALLBACK_URL"] {
			t.Fatalf("unexpected authorization URL: %s", u)
		}
	}
}

func TestFetchJWKSCancelsRequest(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := testAuthServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := fetchJWKS(ctx, server.URL)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("JWKS request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("JWKS fetch did not stop after cancellation")
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP request was not cancelled")
	}
}
