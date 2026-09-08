package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var secretKey string

func SetSecretKey(key string) { secretKey = key }

const cookieName = "starocean_sess" // P0-7 注：历史 Cookie 名保持不变以免强制全员掉线；新部署对外品牌统一为 StarOcean。

type SessionData struct {
	UserID   string `json:"uid"`
	Username string `json:"un"`
	Expires  int64  `json:"exp"`
}

func SessionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(cookieName)
		if err != nil {
			c.Next()
			return
		}
		data := decodeSession(cookie)
		if data == nil || data.Expires < time.Now().Unix() {
			c.Next()
			return
		}
		c.Set("session", data)
		c.Next()
	}
}

func SetSession(c *gin.Context, userID, username string) {
	data := SessionData{
		UserID:   userID,
		Username: username,
		Expires:  time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	encoded := encodeSession(data)
	// P0: SameSite=Lax 默认防跨站 POST 携带 Cookie；Secure 跟随请求协议
	// （直连 http 本地开发为 false，经 https/代理为 true）。
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cookieName, encoded, 86400*7, "/", "", isSecureRequest(c), true)
	c.Set("session", &data)
}

func GetSession(c *gin.Context) *SessionData {
	if s, ok := c.Get("session"); ok {
		if sd, ok := s.(*SessionData); ok {
			return sd
		}
	}
	return nil
}

func ClearSession(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cookieName, "", -1, "/", "", isSecureRequest(c), true)
}

// isSecureRequest 判断是否应打 Secure 标记：TLS 直连或代理头 X-Forwarded-Proto=https。
func isSecureRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	if strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}

func encodeSession(data SessionData) string {
	payload, _ := json.Marshal(data)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig
}

func decodeSession(cookie string) *SessionData {
	parts := strings.SplitN(cookie, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[1]), []byte(expected)) {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil
	}
	var data SessionData
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil
	}
	return &data
}
