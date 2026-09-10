package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zxh326/kite/pkg/common"
	"github.com/zxh326/kite/pkg/model"
	"github.com/zxh326/kite/pkg/rbac"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "kite-proxy-session-test")
	if err != nil {
		panic(err)
	}
	common.DBType = "sqlite"
	common.DBDSN = filepath.Join(dir, "test.db")
	model.InitDB()
	// RequireProxyAuth resolves roles through the in-memory RBAC config,
	// which initializeApp() populates in production.
	rbac.InitRBAC()
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// proxyTestUser creates a user in the DB and returns it with roles attached
// the same way the auth middleware would.
func proxyTestUser(t *testing.T, username string, allowProxy bool) model.User {
	t.Helper()
	user := model.User{Username: username, Provider: "password", Enabled: true}
	if err := model.DB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	user.Roles = []common.Role{{Name: "proxy-test", AllowProxy: allowProxy, Clusters: []string{"*"}}}
	return user
}

func newJSONContext(t *testing.T, method, target string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, target, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, w
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// pkcePair generates a verifier and its S256 challenge like the desktop client does.
func pkcePair(t *testing.T) (verifier, challenge string) {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorizeDevice runs AuthorizeProxy for user and returns the one-time code.
func authorizeDevice(t *testing.T, user model.User, verifier, redirectURI string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(verifier))
	c, w := newJSONContext(t, http.MethodPost, "/api/auth/proxy/authorize", gin.H{
		"redirectURI": redirectURI,
		"challenge":   base64.RawURLEncoding.EncodeToString(sum[:]),
		"state":       "0123456789abcdef0123456789abcdef",
		"deviceName":  "test-device",
	})
	c.Set("user", user)
	AuthorizeProxy(c)
	var resp struct {
		RedirectURI string `json:"redirectURI"`
	}
	decodeJSON(t, w, &resp)
	parsed, err := url.Parse(resp.RedirectURI)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	code := parsed.Query().Get("code")
	if code == "" || parsed.Query().Get("state") != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("redirect missing code/state: %s", resp.RedirectURI)
	}
	return code
}

func exchangeToken(t *testing.T, body gin.H) (int, map[string]any) {
	t.Helper()
	c, w := newJSONContext(t, http.MethodPost, "/api/auth/proxy/token", body)
	ProxyToken(c)
	result := map[string]any{}
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &result)
	}
	return w.Code, result
}

const testRedirectURI = "http://127.0.0.1:45671/callback"

// proxyTestSession performs the full authorize + code exchange for user and
// returns the parsed token response.
func proxyTestSession(t *testing.T, username string) (model.User, map[string]any) {
	t.Helper()
	user := proxyTestUser(t, username, true)
	verifier, _ := pkcePair(t)
	code := authorizeDevice(t, user, verifier, testRedirectURI)
	status, tokens := exchangeToken(t, gin.H{
		"grant_type":    "authorization_code",
		"code":          code,
		"code_verifier": verifier,
		"redirect_uri":  testRedirectURI,
	})
	if status != 200 {
		t.Fatalf("token exchange failed: %d %v", status, tokens)
	}
	return user, tokens
}

func requireProxyAuthStatus(t *testing.T, token string) int {
	t.Helper()
	h := &AuthHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/proxy/kubeconfig", nil)
	c.Request.Header.Set("Authorization", "Bearer "+token)
	h.RequireProxyAuth()(c)
	return w.Code
}

func TestValidProxyRedirect(t *testing.T) {
	for _, v := range []string{
		"http://127.0.0.1:8080/callback",
		"http://127.0.0.1:1/callback",
		"http://127.0.0.1:65535/callback",
	} {
		if !validProxyRedirect(v) {
			t.Errorf("validProxyRedirect(%q) = false, want true", v)
		}
	}
	for _, v := range []string{
		"",
		"https://127.0.0.1:8080/callback",
		"http://localhost:8080/callback",
		"http://127.0.0.1:8080/",
		"http://127.0.0.1:8080/callback?x=1",
		"http://127.0.0.1:8080/callback#frag",
		"http://user:pass@127.0.0.1:8080/callback",
		"http://127.0.0.1/callback",
		"http://127.0.0.1:70000/callback",
		"http://127.0.0.1:8080/other",
		"http://0.0.0.0:8080/callback",
		"file:///etc/passwd",
	} {
		if validProxyRedirect(v) {
			t.Errorf("validProxyRedirect(%q) = true, want false", v)
		}
	}
}

func TestRequireProxyBrowserOrigin(t *testing.T) {
	newCtx := func(method, origin string, withUser bool, user model.User) *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(method, "http://kite.test/api/auth/proxy/authorize", nil)
		if origin != "" {
			c.Request.Header.Set("Origin", origin)
		}
		if withUser {
			c.Set("user", user)
		}
		return c
	}
	runs := func(c *gin.Context) bool { RequireProxyBrowser(c); return c.IsAborted() }

	user := model.User{Provider: "password", Enabled: true}

	if runs(newCtx(http.MethodGet, "", true, user)) {
		t.Error("GET without Origin should pass")
	}
	if runs(newCtx(http.MethodPost, "http://kite.test", true, user)) {
		t.Error("same-origin POST should pass")
	}
	for _, origin := range []string{"https://kite.test", "http://evil.test", "", "http://user@kite.test"} {
		if !runs(newCtx(http.MethodPost, origin, true, user)) {
			t.Errorf("POST with Origin %q should be rejected", origin)
		}
	}
	if !runs(newCtx(http.MethodPost, "http://kite.test", true, model.User{Provider: "api_key", Enabled: true})) {
		t.Error("api_key user must not authorize devices")
	}
	if !runs(newCtx(http.MethodGet, "", false, model.User{})) {
		t.Error("request without user must be rejected")
	}

	anonymous := common.AnonymousUserEnabled
	common.AnonymousUserEnabled = true
	if !runs(newCtx(http.MethodPost, "http://kite.test", true, user)) {
		t.Error("anonymous mode must forbid authorization")
	}
	common.AnonymousUserEnabled = anonymous
}

func TestAuthorizeProxyRejectsUserWithoutProxyRole(t *testing.T) {
	user := proxyTestUser(t, "no-proxy-auth-test", false)
	verifier, _ := pkcePair(t)
	sum := sha256.Sum256([]byte(verifier))
	c, w := newJSONContext(t, http.MethodPost, "/api/auth/proxy/authorize", gin.H{
		"redirectURI": testRedirectURI,
		"challenge":   base64.RawURLEncoding.EncodeToString(sum[:]),
		"state":       "0123456789abcdef0123456789abcdef",
		"deviceName":  "test-device",
	})
	c.Set("user", user)
	AuthorizeProxy(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("authorize without allowProxy should fail, got %d", w.Code)
	}
}

func TestAuthorizeProxyRejectsNonLoopbackRedirect(t *testing.T) {
	user := proxyTestUser(t, "loopback-auth-test", true)
	verifier, _ := pkcePair(t)
	sum := sha256.Sum256([]byte(verifier))
	c, w := newJSONContext(t, http.MethodPost, "/api/auth/proxy/authorize", gin.H{
		"redirectURI": "https://evil.test/callback",
		"challenge":   base64.RawURLEncoding.EncodeToString(sum[:]),
		"state":       "0123456789abcdef0123456789abcdef",
	})
	c.Set("user", user)
	AuthorizeProxy(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-loopback redirect should be rejected, got %d", w.Code)
	}
}

func TestProxyTokenAuthorizationCodeSingleUse(t *testing.T) {
	user := proxyTestUser(t, "code-auth-test", true)
	verifier, _ := pkcePair(t)
	code := authorizeDevice(t, user, verifier, testRedirectURI)

	codeBody := gin.H{
		"grant_type":    "authorization_code",
		"code":          code,
		"code_verifier": verifier,
		"redirect_uri":  testRedirectURI,
	}
	status, tokens := exchangeToken(t, codeBody)
	if status != 200 {
		t.Fatalf("first exchange failed: %d %v", status, tokens)
	}
	if tokens["access_token"] == "" || tokens["refresh_token"] == "" {
		t.Fatalf("token response missing tokens: %v", tokens)
	}
	if _, ok := tokens["username"].(string); !ok {
		t.Fatalf("token response missing username: %v", tokens)
	}

	// The exact same code must not work a second time.
	status, _ = exchangeToken(t, codeBody)
	if status != http.StatusUnauthorized {
		t.Fatalf("replaying authorization code should fail, got %d", status)
	}
}

func TestProxyTokenPKCEVerifierMismatch(t *testing.T) {
	user := proxyTestUser(t, "pkce-auth-test", true)
	code := authorizeDevice(t, user, "43-char-verifier-that-the-client-never-used!!", testRedirectURI)

	verifier := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))
	status, _ := exchangeToken(t, gin.H{
		"grant_type":    "authorization_code",
		"code":          code,
		"code_verifier": verifier,
		"redirect_uri":  testRedirectURI,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong PKCE verifier should fail, got %d", status)
	}
}

func TestProxyTokenRefreshRotation(t *testing.T) {
	user, tokens := proxyTestSession(t, "refresh-auth-test")
	firstRefresh := tokens["refresh_token"].(string)

	status, rotated := exchangeToken(t, gin.H{"grant_type": "refresh_token", "refresh_token": firstRefresh})
	if status != 200 {
		t.Fatalf("refresh failed: %d %v", status, rotated)
	}
	if rotated["refresh_token"] == firstRefresh {
		t.Fatal("refresh token must rotate on every use")
	}

	// Replaying the old refresh token must be rejected.
	status, _ = exchangeToken(t, gin.H{"grant_type": "refresh_token", "refresh_token": firstRefresh})
	if status != http.StatusUnauthorized {
		t.Fatalf("replayed refresh token should fail, got %d", status)
	}

	// The rotated token keeps working.
	status, _ = exchangeToken(t, gin.H{"grant_type": "refresh_token", "refresh_token": rotated["refresh_token"]})
	if status != 200 {
		t.Fatalf("rotated refresh token should work, got %d", status)
	}
	_ = user
}

func TestProxyTokenDisabledUser(t *testing.T) {
	user, tokens := proxyTestSession(t, "disabled-auth-test")
	if err := model.DB.Model(&model.User{}).Where("id = ?", user.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	status, _ := exchangeToken(t, gin.H{"grant_type": "refresh_token", "refresh_token": tokens["refresh_token"]})
	if status != http.StatusUnauthorized {
		t.Fatalf("refresh for disabled user should fail, got %d", status)
	}
	if code := requireProxyAuthStatus(t, tokens["access_token"].(string)); code != http.StatusUnauthorized {
		t.Fatalf("access token of disabled user should be rejected, got %d", code)
	}
}

func TestRequireProxyAuthAndRevoke(t *testing.T) {
	user, tokens := proxyTestSession(t, "revoke-auth-test")
	accessToken := tokens["access_token"].(string)

	h := &AuthHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/proxy/kubeconfig", nil)
	c.Request.Header.Set("Authorization", "Bearer "+accessToken)
	h.RequireProxyAuth()(c)
	if w.Code != 200 {
		t.Fatalf("valid access token should pass, got %d: %s", w.Code, w.Body.String())
	}
	if got := c.MustGet("user").(model.User); got.ID != user.ID {
		t.Fatalf("authenticated as user %d, want %d", got.ID, user.ID)
	}
	sessionID := c.GetUint(ProxySessionKey)
	if sessionID == 0 {
		t.Fatal("proxy session id missing from context")
	}

	// Garbage tokens in the device-token namespace must be rejected.
	if code := requireProxyAuthStatus(t, "kp_not-a-real-token"); code != http.StatusUnauthorized {
		t.Fatalf("garbage device token should be rejected, got %d", code)
	}

	// Cross-user revocation must not touch someone else's session.
	other := proxyTestUser(t, "revoke-other-test", true)
	crossCtx, _ := newJSONContext(t, http.MethodDelete, "/api/auth/proxy/sessions/:"+strconv.FormatUint(uint64(sessionID), 10), nil)
	crossCtx.Set("user", other)
	crossCtx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(sessionID), 10)}}
	RevokeProxySession(crossCtx)
	if code := requireProxyAuthStatus(t, accessToken); code != 200 {
		t.Fatalf("cross-user revoke must not kill the session, got %d", code)
	}

	// Owner revocation kills the session immediately.
	ownerCtx, _ := newJSONContext(t, http.MethodDelete, "/api/auth/proxy/sessions/x", nil)
	ownerCtx.Set("user", user)
	ownerCtx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(sessionID), 10)}}
	RevokeProxySession(ownerCtx)
	if code := requireProxyAuthStatus(t, accessToken); code != http.StatusUnauthorized {
		t.Fatalf("revoked session must be rejected, got %d", code)
	}
}

func TestCurrentProxySessionSelfRevoke(t *testing.T) {
	user, tokens := proxyTestSession(t, "self-revoke-test")
	accessToken := tokens["access_token"].(string)

	h := &AuthHandler{}
	build := func(method string) (*gin.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(method, "/api/v1/proxy/session", nil)
		c.Request.Header.Set("Authorization", "Bearer "+accessToken)
		h.RequireProxyAuth()(c)
		return c, w
	}

	c, w := build(http.MethodGet)
	if c.IsAborted() {
		t.Fatalf("session probe failed: %d", w.Code)
	}
	CurrentProxySession(c)
	var info struct {
		Username string `json:"username"`
	}
	decodeJSON(t, w, &info)
	if info.Username != user.Username {
		t.Fatalf("session reports username %q, want %q", info.Username, user.Username)
	}

	c, w = build(http.MethodDelete)
	if c.IsAborted() {
		t.Fatalf("session delete auth failed: %d", w.Code)
	}
	CurrentProxySession(c)
	// gin only records the status; flush it to the recorder like the engine would.
	c.Writer.WriteHeaderNow()
	if w.Code != 204 {
		t.Fatalf("self revoke should return 204, got %d", w.Code)
	}
	if code := requireProxyAuthStatus(t, accessToken); code != http.StatusUnauthorized {
		t.Fatalf("access token after self revoke should be rejected, got %d", code)
	}
}

func TestProxyTokenRejectsUnknownGrant(t *testing.T) {
	status, _ := exchangeToken(t, gin.H{"grant_type": "password", "username": "x", "password": "y"})
	if status != http.StatusUnauthorized {
		t.Fatalf("unknown grant type should fail, got %d", status)
	}
}

func TestTokenHash(t *testing.T) {
	if tokenHash("a") != tokenHash("a") {
		t.Fatal("hash must be deterministic")
	}
	if len(tokenHash("a")) != 64 {
		t.Fatalf("tokenHash length = %d, want 64", len(tokenHash("a")))
	}
	if tokenHash("input-1") == tokenHash("input-2") {
		t.Fatal("different inputs must hash differently")
	}
}

func TestProxySessionWaiterRegistry(t *testing.T) {
	ch1 := make(chan struct{}, 1)
	ch2 := make(chan struct{}, 1)
	addProxySessionWaiter(999, ch1)
	addProxySessionWaiter(999, ch2)
	removeProxySessionWaiter(999, ch1)
	notifyProxySessionWaiters(999)
	select {
	case <-ch1:
		t.Fatal("removed waiter must not be notified")
	default:
	}
	select {
	case <-ch2:
	default:
		t.Fatal("registered waiter must be notified")
	}
	// notify on an empty id must not panic
	notifyProxySessionWaiters(12345)
}

func longPollContext(t *testing.T, accessToken string, wait string) (*gin.Context, *httptest.ResponseRecorder, chan int) {
	t.Helper()
	h := &AuthHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/proxy/session?wait="+wait, nil)
	c.Request.Header.Set("Authorization", "Bearer "+accessToken)
	h.RequireProxyAuth()(c)
	if c.IsAborted() {
		t.Fatalf("auth failed: %d", w.Code)
	}
	done := make(chan int, 1)
	go func() {
		CurrentProxySession(c)
		done <- w.Code
	}()
	return c, w, done
}

func TestCurrentProxySessionLongPollRevocation(t *testing.T) {
	user, tokens := proxyTestSession(t, "longpoll-revoke-test")
	_, _, done := longPollContext(t, tokens["access_token"].(string), "30")

	select {
	case <-done:
		t.Fatal("long-poll returned before revocation")
	case <-time.After(300 * time.Millisecond):
	}

	// Revoke through the real devices-page path: DB update + waiter wake-up.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/auth/proxy/sessions/:id", nil)
	c.Set("user", user)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(tokens["session_id"].(float64)), 10)}}
	RevokeProxySession(c)
	c.Writer.WriteHeaderNow()
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke failed: %d", w.Code)
	}

	select {
	case code := <-done:
		if code != http.StatusUnauthorized {
			t.Fatalf("revoked long-poll must return 401, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revocation did not wake the long-poll")
	}
}

func TestCurrentProxySessionLongPollTimeout(t *testing.T) {
	_, tokens := proxyTestSession(t, "longpoll-timeout-test")
	_, _, done := longPollContext(t, tokens["access_token"].(string), "1")

	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("long-poll without revocation must return 200, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("long-poll did not return on wait timeout")
	}
}
