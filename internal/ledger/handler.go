package ledger

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/middleware"
	"github.com/nphq/starocean/internal/shared"
)

type Handler struct {
	db *sql.DB
}

func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

func (h *Handler) withTx(c *gin.Context, fn func(*sql.Tx) error) bool {
	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return false
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		shared.JSONBadRequest(c, err.Error())
		return false
	}
	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return false
	}
	return true
}

func Actor(c *gin.Context) string {
	if s := middleware.GetSession(c); s != nil && s.Username != "" {
		return s.Username
	}
	return "system"
}

func periodQuery(c *gin.Context) (int, int) {
	now := time.Now()
	year, _ := strconv.Atoi(c.DefaultQuery("year", strconv.Itoa(now.Year())))
	month, _ := strconv.Atoi(c.DefaultQuery("month", strconv.Itoa(int(now.Month()))))
	if year < 1990 || year > 2100 {
		year = now.Year()
	}
	if month < 0 || month > 12 {
		month = int(now.Month())
	}
	return year, month
}

func (h *Handler) SettingsGet(c *gin.Context) {
	s, err := loadSettings(c.Request.Context(), h.db)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, s)
}

func (h *Handler) SettingsUpdate(c *gin.Context) {
	var in Settings
	if !shared.BindJSON(c, &in) {
		return
	}
	// 税科目缺省为常用科目，避免客户端未下发时被空白覆盖。
	if in.OutputTaxAccount == "" {
		in.OutputTaxAccount = "22210105"
	}
	if in.InputTaxAccount == "" {
		in.InputTaxAccount = "22210101"
	}
	_, err := h.db.ExecContext(c.Request.Context(), `
		UPDATE gl_settings SET
			cash_account=$1, bank_account=$2, ar_account=$3, ap_account=$4, inventory_account=$5,
			revenue_account=$6, cogs_account=$7, opex_account=$8, payroll_account=$9,
			income_summary=$10, retained_earnings=$11, surplus_account=$12, auto_post=$13, costing_method=$14,
			require_review=$15, output_tax_account=$16, input_tax_account=$17,
			updated_at=NOW()
		WHERE company_id=$18`,
		in.CashAccount, in.BankAccount, in.ARAccount, in.APAccount, in.InventoryAccount,
		in.RevenueAccount, in.COGSAccount, in.OpexAccount, in.PayrollAccount,
		in.IncomeSummary, in.RetainedEarnings, in.SurplusAccount, in.AutoPost, in.CostingMethod,
		in.RequireReview, in.OutputTaxAccount, in.InputTaxAccount, companyID)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) AccountsList(c *gin.Context) {
	items, err := ListAccounts(c.Request.Context(), h.db, c.Query("q"), c.Query("category"), c.Query("leaves") == "1")
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"items": shared.EmptySlice(items)})
}

type accountInput struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	ParentCode string `json:"parent_code"`
	Category   string `json:"category"`
	NormalSide string `json:"normal_side"`
	IsLeaf     *bool  `json:"is_leaf"`
	IsCash     bool   `json:"is_cash"`
	AuxAR      bool   `json:"aux_ar"`
	AuxAP      bool   `json:"aux_ap"`
	Active     *bool  `json:"active"`
	SortOrder  int    `json:"sort_order"`
}

func (h *Handler) AccountUpsert(c *gin.Context) {
	var in accountInput
	if !shared.BindJSON(c, &in) {
		return
	}
	if in.Code == "" {
		in.Code = c.Param("code")
	}
	a := Account{
		Code: in.Code, Name: in.Name, ParentCode: in.ParentCode, Category: in.Category,
		NormalSide: in.NormalSide, IsLeaf: true, IsCash: in.IsCash, AuxAR: in.AuxAR, AuxAP: in.AuxAP,
		Active: true, SortOrder: in.SortOrder,
	}
	if in.IsLeaf != nil {
		a.IsLeaf = *in.IsLeaf
	}
	if in.Active != nil {
		a.Active = *in.Active
	}
	if a.SortOrder == 0 {
		a.SortOrder, _ = strconv.Atoi(a.Code)
	}
	if err := UpsertAccount(c.Request.Context(), h.db, a); err != nil {
		shared.JSONBadRequest(c, err.Error())
		return
	}
	shared.JSONOK(c, gin.H{"ok": true, "code": a.Code})
}

func (h *Handler) PeriodsList(c *gin.Context) {
	year, _ := periodQuery(c)
	items, err := ListPeriods(c.Request.Context(), h.db, year)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"items": shared.EmptySlice(items), "year": year})
}

func (h *Handler) CloseCheck(c *gin.Context) {
	year, month := periodQuery(c)
	if month == 0 {
		month = int(time.Now().Month())
	}
	out, err := CheckClose(c.Request.Context(), h.db, year, month)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, out)
}

func (h *Handler) PeriodClose(c *gin.Context) {
	year, _ := strconv.Atoi(c.Param("year"))
	month, _ := strconv.Atoi(c.Param("month"))
	if !h.withTx(c, func(tx *sql.Tx) error {
		return ClosePeriod(c.Request.Context(), tx, year, month, Actor(c))
	}) {
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) PeriodReopen(c *gin.Context) {
	year, _ := strconv.Atoi(c.Param("year"))
	month, _ := strconv.Atoi(c.Param("month"))
	if !h.withTx(c, func(tx *sql.Tx) error {
		return ReopenPeriod(c.Request.Context(), tx, year, month, Actor(c))
	}) {
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) VouchersList(c *gin.Context) {
	year, month := periodQuery(c)
	page := int(shared.GetPage(c))
	items, total, err := ListVouchers(c.Request.Context(), h.db, year, month, c.DefaultQuery("status", "all"), c.Query("q"), page, 20)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"items": shared.EmptySlice(items), "total": total, "page": page, "year": year, "month": month})
}

func (h *Handler) VoucherGet(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	v, err := GetVoucher(c.Request.Context(), h.db, id)
	if err != nil {
		shared.JSONNotFound(c, err.Error())
		return
	}
	shared.JSONOK(c, v)
}

func (h *Handler) VoucherCreate(c *gin.Context) {
	var in VoucherInput
	if !shared.BindJSON(c, &in) {
		return
	}
	var v Voucher
	if !h.withTx(c, func(tx *sql.Tx) error {
		var err error
		v, err = CreateVoucher(c.Request.Context(), tx, in, Actor(c))
		return err
	}) {
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h *Handler) VoucherUpdate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var in VoucherInput
	if !shared.BindJSON(c, &in) {
		return
	}
	var v Voucher
	if !h.withTx(c, func(tx *sql.Tx) error {
		var err error
		v, err = UpdateVoucher(c.Request.Context(), tx, id, in)
		return err
	}) {
		return
	}
	shared.JSONOK(c, v)
}

func (h *Handler) VoucherPost(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	if !h.withTx(c, func(tx *sql.Tx) error {
		return PostVoucher(c.Request.Context(), tx, id, Actor(c))
	}) {
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) VoucherReview(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	// 审核意见可选：无请求体时忽略解析错误，仍执行审核。
	_ = c.ShouldBindJSON(&in)
	if !h.withTx(c, func(tx *sql.Tx) error {
		return ReviewVoucher(c.Request.Context(), tx, id, Actor(c), in.Note)
	}) {
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) VoucherReject(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if !shared.BindJSON(c, &in) {
		return
	}
	if !h.withTx(c, func(tx *sql.Tx) error {
		return RejectVoucher(c.Request.Context(), tx, id, in.Reason)
	}) {
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) VoucherReverse(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var v Voucher
	if !h.withTx(c, func(tx *sql.Tx) error {
		var err error
		v, err = ReverseVoucher(c.Request.Context(), tx, id, Actor(c))
		return err
	}) {
		return
	}
	shared.JSONOK(c, v)
}

func (h *Handler) VoucherDelete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	if !h.withTx(c, func(tx *sql.Tx) error {
		return DeleteVoucher(c.Request.Context(), tx, id)
	}) {
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) Trial(c *gin.Context) {
	year, month := periodQuery(c)
	if month == 0 {
		month = int(time.Now().Month())
	}
	out, err := TrialBalance(c.Request.Context(), h.db, year, month, c.Query("leaves") == "1")
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, out)
}

func (h *Handler) Book(c *gin.Context) {
	year, month := periodQuery(c)
	code := c.Query("account")
	if code == "" {
		shared.JSONBadRequest(c, "请指定科目")
		return
	}
	out, err := AccountBook(c.Request.Context(), h.db, code, year, month)
	if err != nil {
		shared.JSONBadRequest(c, err.Error())
		return
	}
	shared.JSONOK(c, out)
}

func (h *Handler) Journal(c *gin.Context) {
	year, month := periodQuery(c)
	items, err := GeneralJournal(c.Request.Context(), h.db, year, month, c.Query("account"))
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"items": shared.EmptySlice(items)})
}

func (h *Handler) CashBook(c *gin.Context) {
	year, month := periodQuery(c)
	out, err := CashJournal(c.Request.Context(), h.db, year, month, c.DefaultQuery("account", "1001"))
	if err != nil {
		shared.JSONBadRequest(c, err.Error())
		return
	}
	shared.JSONOK(c, out)
}

func (h *Handler) ReportBalanceSheet(c *gin.Context) {
	year, month := periodQuery(c)
	out, err := BalanceSheet(c.Request.Context(), h.db, year, month)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, out)
}

func (h *Handler) ReportIncome(c *gin.Context) {
	year, month := periodQuery(c)
	out, err := IncomeStatement(c.Request.Context(), h.db, year, month)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, out)
}

func (h *Handler) ReportCashFlow(c *gin.Context) {
	year, month := periodQuery(c)
	out, err := CashFlowStatement(c.Request.Context(), h.db, year, month)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, out)
}

type openingsInput struct {
	Year  int         `json:"year"`
	Lines []LineInput `json:"lines"`
}

func (h *Handler) OpeningsSave(c *gin.Context) {
	var in openingsInput
	if !shared.BindJSON(c, &in) {
		return
	}
	if in.Year == 0 {
		in.Year = time.Now().Year()
	}
	var v Voucher
	if !h.withTx(c, func(tx *sql.Tx) error {
		var err error
		v, err = SaveOpenings(c.Request.Context(), tx, in.Year, in.Lines, Actor(c))
		return err
	}) {
		return
	}
	shared.JSONOK(c, v)
}

func (h *Handler) Cockpit(c *gin.Context) {
	year, month := periodQuery(c)
	if month == 0 {
		month = int(time.Now().Month())
	}
	check, err := CheckClose(c.Request.Context(), h.db, year, month)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	tb, _ := TrialBalance(c.Request.Context(), h.db, year, month, true)
	bs, _ := BalanceSheet(c.Request.Context(), h.db, year, month)
	pl, _ := IncomeStatement(c.Request.Context(), h.db, year, month)
	settings, _ := loadSettings(c.Request.Context(), h.db)
	shared.JSONOK(c, gin.H{
		"check":            check,
		"trial":            tb,
		"balance_sheet_ok": bs.Balanced,
		"profit":           lastAmount(pl),
		"settings":         settings,
	})
}

func lastAmount(s Statement) string {
	if len(s.Lines) == 0 {
		return "0.00"
	}
	return s.Lines[len(s.Lines)-1].Amount
}
