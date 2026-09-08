package middleware

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	RoleAdmin  = "admin"  // 改单：全部读写
	RoleViewer = "viewer" // 只看账：仅 GET/HEAD/OPTIONS
)

// GetRole 取本次请求的用户角色（AuthRequired 已写入 context，未认证返回 ""）。
func GetRole(c *gin.Context) string {
	if v, ok := c.Get("role"); ok {
		if r, ok := v.(string); ok {
			return r
		}
	}
	return ""
}

func AuthRequired(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sess := GetSession(c)
		if sess == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}
		// P0: CSRF 纵深防御。SameSite=Lax 已拦住绝大多数跨站 POST；
		// 这里再校验 Origin/Referer（仅当浏览器携带时）：跨站直接 403。
		if !checkSameOrigin(c) {
			c.JSON(http.StatusForbidden, gin.H{"error": "跨站请求被拒绝"})
			c.Abort()
			return
		}
		// P0: 会话中的用户可能已被删除/禁用，每次鉴权确认仍存在。
		// AuthRequired 此前忽略 db 参数，直接放行已删用户。
		// 最小 RBAC：同时载入 role 供 WriteRequired 使用（未知值按 viewer 处理）。
		var role string
		if err := db.QueryRowContext(c.Request.Context(),
			`SELECT COALESCE(role, 'viewer') FROM users WHERE id = $1`, sess.UserID).Scan(&role); err != nil {
			ClearSession(c)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}
		if role != RoleAdmin {
			role = RoleViewer
		}
		c.Set("role", role)
		c.Next()
	}
}

// WriteRequired 写操作仅 admin 可过；viewer 读可以、写 403。
// 登录/登出/个人信息除外（同组路由内豁免）。
// 自给自足：若上游中间件没写 role（如 HTMLAuth），自己按 session 查库。
func WriteRequired(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		switch c.FullPath() {
		case "/api/login", "/api/logout", "/api/me", "/login", "/logout":
			c.Next()
			return
		}
		role := GetRole(c)
		if role == "" {
			sess := GetSession(c)
			if sess != nil {
				_ = db.QueryRowContext(c.Request.Context(),
					`SELECT COALESCE(role, 'viewer') FROM users WHERE id = $1`, sess.UserID).Scan(&role)
			}
			if role != RoleAdmin {
				role = RoleViewer
			}
			c.Set("role", role)
		}
		if role != RoleAdmin {
			if strings.HasPrefix(c.FullPath(), "/api/") {
				c.JSON(http.StatusForbidden, gin.H{"error": "只读账号无权执行写操作"})
			} else {
				c.String(http.StatusForbidden, "只读账号无权执行写操作")
			}
			c.Abort()
			return
		}
		c.Next()
	}
}

// checkSameOrigin 浏览器跨站写请求会带 Origin/Referer；缺失（如 curl/同源 GET）则放行。
func checkSameOrigin(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	referer := c.GetHeader("Referer")
	if origin == "" && referer == "" {
		return true
	}
	host := c.Request.Host
	target := origin
	if target == "" {
		target = referer
	}
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// P0: 登录失败限流（防密码爆破 + 缓解 bcrypt CPU DoS）：单 IP 1 分钟最多 10 次失败。
var loginAttempts = struct {
	sync.Mutex
	hits map[string][]time.Time
}{hits: make(map[string][]time.Time)}

// pruneLocked 清理过期记录并删除空 IP 条目，防止非活跃 IP 在 map 中无限累积。
func pruneLocked(now time.Time) {
	cutoff := now.Add(-time.Minute)
	for ip, hits := range loginAttempts.hits {
		kept := hits[:0]
		for _, t := range hits {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(loginAttempts.hits, ip)
		} else {
			loginAttempts.hits[ip] = kept
		}
	}
}

func loginLimited(ip string) bool {
	loginAttempts.Lock()
	defer loginAttempts.Unlock()
	now := time.Now()
	pruneLocked(now)
	return len(loginAttempts.hits[ip]) >= 10
}

func recordLoginFailure(ip string) {
	loginAttempts.Lock()
	defer loginAttempts.Unlock()
	loginAttempts.hits[ip] = append(loginAttempts.hits[ip], time.Now())
}

func LoginHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if loginLimited(ip) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "尝试过于频繁，请1分钟后再试"})
			return
		}
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "用户名和密码不能为空"})
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "用户名和密码不能为空"})
			return
		}

		var userID uuid.UUID
		var dbUsername, passwordHash, role string
		err := db.QueryRowContext(c.Request.Context(),
			`SELECT id, username, password_hash, COALESCE(role, 'viewer') FROM users WHERE username = $1`, req.Username).
			Scan(&userID, &dbUsername, &passwordHash, &role)
		if err != nil {
			recordLoginFailure(ip)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
			recordLoginFailure(ip)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
			return
		}

		if role != RoleAdmin {
			role = RoleViewer
		}
		SetSession(c, userID.String(), dbUsername)
		c.JSON(http.StatusOK, gin.H{"user_id": userID.String(), "username": dbUsername, "role": role})
	}
}

func LogoutHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ClearSession(c)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func MeHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sess := GetSession(c)
		if sess == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		role := GetRole(c)
		if role == "" {
			// /api/me 不在鉴权组内，自己查库（查不到按 viewer 处理）。
			_ = db.QueryRowContext(c.Request.Context(),
				`SELECT COALESCE(role, 'viewer') FROM users WHERE id = $1`, sess.UserID).Scan(&role)
			if role != RoleAdmin {
				role = RoleViewer
			}
		}
		c.JSON(http.StatusOK, gin.H{"user_id": sess.UserID, "username": sess.Username, "role": role})
	}
}
