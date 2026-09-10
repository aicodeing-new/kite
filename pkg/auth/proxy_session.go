package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zxh326/kite/pkg/common"
	"github.com/zxh326/kite/pkg/model"
	"github.com/zxh326/kite/pkg/rbac"
	"gorm.io/gorm"
)

const ProxySessionKey = "proxy-device-session"

var pkceVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var pkceChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func tokenHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func randomProxyToken(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}
func validProxyRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "/callback" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port > 0 && port <= 65535
}

// Browser authorization changes require a same-origin JSON request and a real
// enabled user cookie. Anonymous mode and API keys must not authorize devices.
func RequireProxyBrowser(c *gin.Context) {
	u, ok := c.Get("user")
	if !ok || common.AnonymousUserEnabled {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	user := u.(model.User)
	if !user.Enabled || user.Provider == "api_key" {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if c.Request.Method != http.MethodGet {
		origin, err := url.Parse(c.GetHeader("Origin"))
		expected := c.Request.Host
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		if host, err := url.Parse(common.Host); err == nil && host.Host != "" {
			expected = host.Host
			scheme = host.Scheme
		} else if c.GetHeader("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		if err != nil || origin.Host != expected || origin.Scheme != scheme || origin.User != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "same-origin browser request required"})
			return
		}
	}
	c.Next()
}

func AuthorizeProxy(c *gin.Context) {
	var req struct {
		RedirectURI string `json:"redirectURI"`
		Challenge   string `json:"challenge"`
		State       string `json:"state"`
		DeviceName  string `json:"deviceName"`
	}
	if c.ShouldBindJSON(&req) != nil || !validProxyRedirect(req.RedirectURI) || !pkceChallenge.MatchString(req.Challenge) || len(req.State) < 32 || len(req.State) > 128 || len(req.DeviceName) > 100 {
		c.JSON(400, gin.H{"error": "invalid authorization request"})
		return
	}
	user := c.MustGet("user").(model.User)
	allowed := false
	for _, role := range rbac.GetUserRoles(user) {
		if role.AllowProxy {
			allowed = true
		}
	}
	if !allowed {
		c.JSON(403, gin.H{"error": "Your account has no proxy permission. Ask an administrator to grant allowProxy.", "code": "proxy_forbidden"})
		return
	}
	code := randomProxyToken("")
	grant := model.ProxyAuthorizationCode{UserID: user.ID, CodeHash: tokenHash(code), Challenge: req.Challenge, RedirectURI: req.RedirectURI, DeviceName: req.DeviceName, ExpiresAt: time.Now().Add(2 * time.Minute)}
	if err := model.DB.Create(&grant).Error; err != nil {
		c.JSON(500, gin.H{"error": "cannot create authorization"})
		return
	}
	model.DB.Where("expires_at < ?", time.Now()).Delete(&model.ProxyAuthorizationCode{})
	u, _ := url.Parse(req.RedirectURI)
	q := u.Query()
	q.Set("code", code)
	q.Set("state", req.State)
	u.RawQuery = q.Encode()
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"redirectURI": u.String()})
}

func ProxyToken(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req struct {
		GrantType    string `json:"grant_type"`
		Code         string `json:"code"`
		Verifier     string `json:"code_verifier"`
		RedirectURI  string `json:"redirect_uri"`
		RefreshToken string `json:"refresh_token"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	access, refresh := randomProxyToken("kp_"), randomProxyToken("kr_")
	now := time.Now()
	var session model.ProxySession
	var user model.User
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		switch req.GrantType {
		case "authorization_code":
			if !pkceVerifier.MatchString(req.Verifier) || !validProxyRedirect(req.RedirectURI) {
				return errors.New("invalid grant")
			}
			var grant model.ProxyAuthorizationCode
			if err := tx.Where("code_hash = ? AND expires_at > ?", tokenHash(req.Code), now).First(&grant).Error; err != nil {
				return err
			}
			h := sha256.Sum256([]byte(req.Verifier))
			if grant.RedirectURI != req.RedirectURI || grant.Challenge != base64.RawURLEncoding.EncodeToString(h[:]) {
				return errors.New("invalid grant")
			}
			result := tx.Where("id = ?", grant.ID).Delete(&model.ProxyAuthorizationCode{})
			if result.Error != nil || result.RowsAffected != 1 {
				return errors.New("code already used")
			}
			session = model.ProxySession{UserID: grant.UserID, DeviceName: grant.DeviceName, ExpiresAt: now.Add(30 * 24 * time.Hour)}
		case "refresh_token":
			if err := tx.Where("refresh_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenHash(req.RefreshToken), now).First(&session).Error; err != nil {
				return err
			}
		default:
			return errors.New("unsupported grant")
		}
		if err := tx.First(&user, session.UserID).Error; err != nil || !user.Enabled {
			return errors.New("user disabled")
		}
		values := map[string]interface{}{"access_hash": tokenHash(access), "refresh_hash": tokenHash(refresh), "access_expires_at": now.Add(15 * time.Minute), "last_seen_at": now}
		if session.ID == 0 {
			session.AccessHash = tokenHash(access)
			session.RefreshHash = tokenHash(refresh)
			session.AccessExpiresAt = now.Add(15 * time.Minute)
			session.LastSeenAt = now
			return tx.Create(&session).Error
		}
		result := tx.Model(&model.ProxySession{}).Where("id = ? AND refresh_hash = ? AND revoked_at IS NULL", session.ID, tokenHash(req.RefreshToken)).Updates(values)
		if result.Error != nil || result.RowsAffected != 1 {
			return errors.New("refresh token already used")
		}
		return nil
	})
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid or expired authorization", "code": "unauthorized"})
		return
	}
	c.JSON(200, gin.H{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": 900, "session_id": session.ID, "username": user.Username})
}

// RequireProxyAuth is installed ONLY on proxy endpoints. Existing API-key
// clients retain their old authentication path; browser cookies cannot fetch configs.
func (h *AuthHandler) RequireProxyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !strings.HasPrefix(token, "kp_") {
			h.RequireAuth()(c)
			return
		}
		var session model.ProxySession
		if model.DB.Where("access_hash = ? AND access_expires_at > ? AND expires_at > ? AND revoked_at IS NULL", tokenHash(token), time.Now(), time.Now()).First(&session).Error != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "device session expired or revoked", "code": "unauthorized"})
			return
		}
		user, err := model.GetUserByID(uint64(session.UserID))
		if err != nil || !user.Enabled {
			c.AbortWithStatusJSON(401, gin.H{"error": "user disabled", "code": "unauthorized"})
			return
		}
		user.Roles = rbac.GetUserRoles(*user)
		c.Set("user", *user)
		c.Set(ProxySessionKey, session.ID)
		model.DB.Model(&session).Update("last_seen_at", time.Now())
		c.Next()
	}
}

func ListProxySessions(c *gin.Context) {
	user := c.MustGet("user").(model.User)
	var sessions []model.ProxySession
	if model.DB.Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", user.ID, time.Now()).Order("created_at DESC").Find(&sessions).Error != nil {
		c.JSON(500, gin.H{"error": "cannot load devices"})
		return
	}
	c.JSON(200, gin.H{"sessions": sessions})
}
func RevokeProxySession(c *gin.Context) {
	user := c.MustGet("user").(model.User)
	var id uint
	if parsed, err := strconv.ParseUint(c.Param("id"), 10, 64); err == nil {
		id = uint(parsed)
	}
	result := model.DB.Model(&model.ProxySession{}).Where("id = ? AND user_id = ?", c.Param("id"), user.ID).Update("revoked_at", time.Now())
	if result.Error != nil {
		c.JSON(500, gin.H{"error": "cannot revoke device"})
		return
	}
	if result.RowsAffected > 0 && id != 0 {
		notifyProxySessionWaiters(id)
	}
	c.Status(204)
}

// Long-polling support: clients may hold GET /api/v1/proxy/session?wait=Ns
// open so a revocation wakes them within milliseconds instead of at their
// next polling interval.
var (
	proxyWaitersMu sync.Mutex
	proxyWaiters   = map[uint][]chan struct{}{}
)

func addProxySessionWaiter(id uint, ch chan struct{}) {
	proxyWaitersMu.Lock()
	defer proxyWaitersMu.Unlock()
	proxyWaiters[id] = append(proxyWaiters[id], ch)
}

func removeProxySessionWaiter(id uint, ch chan struct{}) {
	proxyWaitersMu.Lock()
	defer proxyWaitersMu.Unlock()
	list := proxyWaiters[id]
	for i, c := range list {
		if c == ch {
			proxyWaiters[id] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(proxyWaiters[id]) == 0 {
		delete(proxyWaiters, id)
	}
}

func notifyProxySessionWaiters(id uint) {
	proxyWaitersMu.Lock()
	list := proxyWaiters[id]
	delete(proxyWaiters, id)
	proxyWaitersMu.Unlock()
	for _, ch := range list {
		close(ch)
	}
}

// sessionWaitSeconds parses the ?wait= parameter, clamped to 1-60 seconds.
func sessionWaitSeconds(c *gin.Context) int {
	secs, err := strconv.Atoi(c.Query("wait"))
	if err != nil || secs < 1 {
		return 0
	}
	if secs > 60 {
		secs = 60
	}
	return secs
}

// proxySessionStillValid re-checks a session after a long-poll wait and
// writes a 401 when it was revoked or the owning user got disabled meanwhile.
func proxySessionStillValid(c *gin.Context, id uint) bool {
	var session model.ProxySession
	if model.DB.Where("id = ? AND revoked_at IS NULL AND expires_at > ?", id, time.Now()).First(&session).Error != nil {
		c.AbortWithStatusJSON(401, gin.H{"error": "device session expired or revoked", "code": "unauthorized"})
		return false
	}
	user, err := model.GetUserByID(uint64(session.UserID))
	if err != nil || !user.Enabled {
		c.AbortWithStatusJSON(401, gin.H{"error": "user disabled", "code": "unauthorized"})
		return false
	}
	return true
}

func CurrentProxySession(c *gin.Context) {
	if c.Request.Method == http.MethodDelete {
		id, exists := c.Get(ProxySessionKey)
		if !exists {
			c.AbortWithStatus(403)
			return
		}
		if model.DB.Model(&model.ProxySession{}).Where("id = ?", id).Update("revoked_at", time.Now()).Error != nil {
			c.Status(500)
			return
		}
		notifyProxySessionWaiters(id.(uint))
		c.Status(204)
		return
	}
	// Long-poll: hold the request open until revocation or the wait deadline.
	if wait := sessionWaitSeconds(c); wait > 0 {
		id := c.GetUint(ProxySessionKey)
		notify := make(chan struct{}, 1)
		addProxySessionWaiter(id, notify)
		defer removeProxySessionWaiter(id, notify)
		timer := time.NewTimer(time.Duration(wait) * time.Second)
		defer timer.Stop()
		select {
		case <-c.Request.Context().Done():
			return
		case <-notify:
		case <-timer.C:
		}
		if !proxySessionStillValid(c, id) {
			return
		}
	}
	user := c.MustGet("user").(model.User)
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"username": user.Username, "sessionId": c.GetUint(ProxySessionKey)})
}
