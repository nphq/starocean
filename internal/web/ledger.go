package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/ledger"
	"github.com/nphq/starocean/view/pages"
)

func periodParam(c *gin.Context) (int, int) {
	now := time.Now()
	year, month := now.Year(), int(now.Month())
	if y, err := strconv.Atoi(c.Query("year")); err == nil && y >= 2000 && y <= 2100 {
		year = y
	}
	if m, err := strconv.Atoi(c.Query("month")); err == nil && m >= 1 && m <= 12 {
		month = m
	}
	return year, month
}

func (h *Handler) LedgerCockpit(c *gin.Context) {
	ctx := c.Request.Context()
	year, _ := periodParam(c)
	periods, _ := ledger.ListPeriods(ctx, h.db, year)
	var years []int
	for y := year - 2; y <= year+1; y++ {
		years = append(years, y)
	}
	h.renderPage(c, "总账结账", pages.LedgerCockpit(year, years, periods, ""))
}

func (h *Handler) LedgerAccounts(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	accounts, err := ledger.ListAccounts(ctx, h.db, q, "", false)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	if isHX(c) {
		renderFrag(c, pages.LedgerAccountInner(accounts, q))
		return
	}
	h.renderPage(c, "会计科目", pages.LedgerAccountList(accounts, q))
}

func (h *Handler) LedgerVouchers(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	status := c.DefaultQuery("status", "all")
	page := int(getPage(c.Request.URL.Query(), "page"))
	items, total, err := ledger.ListVouchers(ctx, h.db, 0, 0, status, q, page, 20)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	if isHX(c) {
		renderFrag(c, pages.LedgerVoucherInner(items, q, status, int32(page), total, 20))
		return
	}
	h.renderPage(c, "会计凭证", pages.LedgerVoucherList(items, q, status, int32(page), total, 20))
}

func (h *Handler) LedgerVoucherNew(c *gin.Context) {
	accounts, _ := ledger.ListAccounts(c.Request.Context(), h.db, "", "", true)
	h.renderPage(c, "新增凭证", pages.LedgerVoucherForm(accounts, ""))
}

func (h *Handler) LedgerVoucherCreate(c *gin.Context) {
	ctx := c.Request.Context()
	accounts, _ := ledger.ListAccounts(ctx, h.db, "", "", true)
	fail := func(msg string) {
		h.renderPage(c, "新增凭证", pages.LedgerVoucherForm(accounts, msg))
	}
	date := strings.TrimSpace(c.PostForm("voucher_date"))
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	var lines []ledger.LineInput
	for i := 1; i <= 6; i++ {
		p := strconv.Itoa(i)
		acct := strings.TrimSpace(c.PostForm("account_" + p))
		debit := strings.TrimSpace(c.PostForm("debit_" + p))
		credit := strings.TrimSpace(c.PostForm("credit_" + p))
		if acct == "" && debit == "" && credit == "" {
			continue
		}
		if acct == "" {
			fail("第 " + p + " 行请选择科目")
			return
		}
		lines = append(lines, ledger.LineInput{
			AccountCode: acct,
			Summary:     strings.TrimSpace(c.PostForm("summary")),
			Debit:       debit,
			Credit:      credit,
		})
	}
	if len(lines) < 2 {
		fail("凭证至少需要两行分录")
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	defer tx.Rollback()
	v, err := ledger.CreateVoucher(ctx, tx, ledger.VoucherInput{
		Word:        "记",
		VoucherDate: date,
		Summary:     strings.TrimSpace(c.PostForm("summary")),
		Lines:       lines,
	}, ledger.Actor(c))
	if err != nil {
		fail(err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "保存失败")
		return
	}
	redirect(c, "/ledger/vouchers/"+v.ID.String())
}

func (h *Handler) LedgerVoucherDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	v, err := ledger.GetVoucher(c.Request.Context(), h.db, id)
	if err != nil {
		c.String(http.StatusNotFound, "凭证不存在")
		return
	}
	settings, _ := ledger.GetSettings(c.Request.Context(), h.db)
	h.renderPage(c, v.VoucherNo, pages.LedgerVoucherDetail(v, settings.RequireReview, ""))
}

func (h *Handler) LedgerVoucherReview(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	if err := ledger.ReviewVoucher(ctx, tx, id, ledger.Actor(c), strings.TrimSpace(c.PostForm("note"))); err != nil {
		v, verr := ledger.GetVoucher(ctx, h.db, id)
		if verr != nil {
			c.String(http.StatusNotFound, "凭证不存在")
			return
		}
		settings, _ := ledger.GetSettings(ctx, h.db)
		h.renderPage(c, v.VoucherNo, pages.LedgerVoucherDetail(v, settings.RequireReview, err.Error()))
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	redirect(c, "/ledger/vouchers/"+id.String())
}

func (h *Handler) LedgerVoucherReject(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	if err := ledger.RejectVoucher(ctx, tx, id, strings.TrimSpace(c.PostForm("reason"))); err != nil {
		v, verr := ledger.GetVoucher(ctx, h.db, id)
		if verr != nil {
			c.String(http.StatusNotFound, "凭证不存在")
			return
		}
		settings, _ := ledger.GetSettings(ctx, h.db)
		h.renderPage(c, v.VoucherNo, pages.LedgerVoucherDetail(v, settings.RequireReview, err.Error()))
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	redirect(c, "/ledger/vouchers/"+id.String())
}

func (h *Handler) LedgerVoucherPost(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	if err := ledger.PostVoucher(ctx, tx, id, ledger.Actor(c)); err != nil {
		v, verr := ledger.GetVoucher(ctx, h.db, id)
		if verr != nil {
			c.String(http.StatusNotFound, "凭证不存在")
			return
		}
		settings, _ := ledger.GetSettings(ctx, h.db)
		h.renderPage(c, v.VoucherNo, pages.LedgerVoucherDetail(v, settings.RequireReview, err.Error()))
		return
	}
	if err := tx.Commit(); err != nil {
		c.String(http.StatusInternalServerError, "操作失败")
		return
	}
	redirect(c, "/ledger/vouchers/"+id.String())
}

func (h *Handler) LedgerBooks(c *gin.Context) {
	ctx := c.Request.Context()
	year, month := periodParam(c)
	trial, err := ledger.TrialBalance(ctx, h.db, year, month, true)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败："+err.Error())
		return
	}
	h.renderPage(c, "账簿", pages.LedgerBooks(year, month, trial))
}

func (h *Handler) LedgerReports(c *gin.Context) {
	ctx := c.Request.Context()
	year, month := periodParam(c)
	kind := c.DefaultQuery("kind", "balance")
	switch kind {
	case "income":
		st, err := ledger.IncomeStatement(ctx, h.db, year, month)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		h.renderPage(c, "利润表", pages.LedgerReport(kind, year, month, st))
	case "cashflow":
		st, err := ledger.CashFlowStatement(ctx, h.db, year, month)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		h.renderPage(c, "现金流量表", pages.LedgerReport(kind, year, month, st))
	default:
		st, err := ledger.BalanceSheet(ctx, h.db, year, month)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		h.renderPage(c, "资产负债表", pages.LedgerReport("balance", year, month, st))
	}
}
