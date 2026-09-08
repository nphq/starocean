package web

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	mw "github.com/nphq/starocean/internal/middleware"
	"github.com/nphq/starocean/view/pages"
	"golang.org/x/crypto/bcrypt"
)

func (h *Handler) LoginPage(c *gin.Context) {
	if mw.GetSession(c) != nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = pages.Login(pages.LoginView{Next: c.Query("next")}, h.companyName).Render(c.Request.Context(), c.Writer)
}

func (h *Handler) LoginPost(c *gin.Context) {
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	next := c.PostForm("next")
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	fail := func(msg string) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusUnauthorized)
		_ = pages.Login(pages.LoginView{Next: c.PostForm("next"), Error: msg}, h.companyName).Render(c.Request.Context(), c.Writer)
	}
	if username == "" || password == "" {
		fail("用户名和密码不能为空")
		return
	}
	var userID uuid.UUID
	var dbUsername, passwordHash string
	err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT id, username, password_hash FROM users WHERE username = $1`, username).
		Scan(&userID, &dbUsername, &passwordHash)
	if err != nil {
		fail("用户名或密码错误")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		fail("用户名或密码错误")
		return
	}
	mw.SetSession(c, userID.String(), dbUsername)
	c.Redirect(http.StatusSeeOther, next)
}

func (h *Handler) Logout(c *gin.Context) {
	mw.ClearSession(c)
	c.Redirect(http.StatusSeeOther, "/login")
}
