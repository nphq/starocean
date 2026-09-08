package finance

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/ledger"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
	"github.com/shopspring/decimal"
)

type Handler struct {
	db *sql.DB
}

func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

type CashflowView struct {
	MonthIncome   string `json:"month_income"`
	MonthExpense  string `json:"month_expense"`
	NetCashflow   string `json:"net_cashflow"`
	IncomeChange  string `json:"income_change"`
	ExpenseChange string `json:"expense_change"`
	NetChange     string `json:"net_change"`
}

type ReceivablePayableView struct {
	TotalReceivable string `json:"total_receivable"`
	TotalPayable    string `json:"total_payable"`
}

type AgingView struct {
	Range     string  `json:"range"`
	Count     int64   `json:"count"`
	Amount    string  `json:"amount"`
	MaxAmount float64 `json:"max_amount"`
}

type paymentInput struct {
	Type          string    `json:"type"`
	Amount        string    `json:"amount"`
	PartnerName   string    `json:"partner_name"`
	Notes         string    `json:"notes"`
	ReferenceType string    `json:"reference_type"`
	ReferenceID   uuid.UUID `json:"reference_id"`
}

func (h *Handler) FinancePage(c *gin.Context) {
	page := shared.GetPage(c)
	ctx := c.Request.Context()

	rows, err := h.db.QueryContext(ctx, `SELECT id, type, amount, COALESCE(partner_name,''), COALESCE(notes,''),
		COALESCE(payment_date,'1970-01-01'), COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM payments ORDER BY created_at DESC LIMIT $1 OFFSET $2`, 20, (page-1)*20)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var payments []models.Payment
	for rows.Next() {
		var p models.Payment
		if err := rows.Scan(&p.ID, &p.Type, &p.Amount, &p.PartnerName, &p.Notes,
			&p.PaymentDate, &p.CreatedAt, &p.CompanyID); err != nil {
			shared.JSONInternal(c, err)
			return
		}
		payments = append(payments, p)
	}

	cashflow, _ := getCashflow(ctx, h.db)
	rp, _ := getReceivablePayable(ctx, h.db)
	aging, _ := getReceivableAging(ctx, h.db)

	calcChange := func(cur, prev decimal.Decimal) string {
		if prev.IsZero() {
			if cur.IsZero() {
				return "0.0"
			}
			return "100.0"
		}
		return cur.Sub(prev).Div(prev).Mul(decimal.NewFromInt(100)).StringFixed(1)
	}

	cur, prev, _ := getMonthOverMonth(ctx, h.db)
	cashflow.IncomeChange = calcChange(cur.income, prev.income)
	cashflow.ExpenseChange = calcChange(cur.expense, prev.expense)
	cashflow.NetChange = calcChange(cur.income.Sub(cur.expense), prev.income.Sub(prev.expense))

	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM payments").Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, gin.H{
		"items":              shared.EmptySlice(payments),
		"total":              count,
		"page":               page,
		"cashflow":           cashflow,
		"receivable_payable": rp,
		"aging":              shared.EmptySlice(aging),
	})
}

func (h *Handler) PaymentCreate(c *gin.Context) {
	var input paymentInput
	if !shared.BindJSON(c, &input) {
		return
	}
	if input.Type == "" || input.Amount == "" {
		shared.JSONBadRequest(c, "类型和金额不能为空")
		return
	}
	amt, err := decimal.NewFromString(input.Amount)
	if err != nil || amt.LessThanOrEqual(decimal.Zero) {
		shared.JSONBadRequest(c, "金额必须为正数")
		return
	}

	ctx := c.Request.Context()
	payload := map[string]interface{}{"type": input.Type, "amount": input.Amount, "reference_type": input.ReferenceType}
	if shared.AbortIfPluginRejected(c, shared.FireBefore(ctx, "payment.creating", payload)) {
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	var refType interface{}
	if input.ReferenceType != "" {
		refType = input.ReferenceType
	}
	var refID interface{}
	if input.ReferenceID != uuid.Nil {
		refID = input.ReferenceID
	}
	var partnerName interface{}
	if input.PartnerName != "" {
		partnerName = input.PartnerName
	}
	var notes interface{}
	if input.Notes != "" {
		notes = input.Notes
	}

	paymentID := uuid.New()
	_, err = tx.ExecContext(ctx, `INSERT INTO payments (id, type, amount, reference_type, reference_id, partner_name, notes, payment_date, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())`,
		paymentID, input.Type, input.Amount, refType, refID, partnerName, notes)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	switch input.ReferenceType {
	case "sales_order":
		res, err := tx.ExecContext(ctx, `UPDATE sales_orders SET paid_amount = paid_amount + $1, updated_at = NOW()
			WHERE id = $2 AND paid_amount + $1 <= total_amount`, input.Amount, input.ReferenceID)
		if err != nil {
			shared.JSONInternal(c, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			shared.JSONBadRequest(c, paymentRejectedMsg(ctx, tx, "sales_orders", input.ReferenceID))
			return
		}
		// 回款冲减欠款（仅收入方向；支出挂销售单属异常场景，不碰 balance）。
		if input.Type == "收入" {
			if _, err := tx.ExecContext(ctx, `UPDATE customers SET balance = balance - $1::numeric, updated_at = NOW()
				WHERE id = (SELECT customer_id FROM sales_orders WHERE id = $2)`, input.Amount, input.ReferenceID); err != nil {
				shared.JSONInternal(c, err)
				return
			}
		}
	case "purchase_order":
		res, err := tx.ExecContext(ctx, `UPDATE purchase_orders SET paid_amount = paid_amount + $1, updated_at = NOW()
			WHERE id = $2 AND paid_amount + $1 <= total_amount`, input.Amount, input.ReferenceID)
		if err != nil {
			shared.JSONInternal(c, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			shared.JSONBadRequest(c, paymentRejectedMsg(ctx, tx, "purchase_orders", input.ReferenceID))
			return
		}
	}

	if err := ledger.PostPayment(ctx, tx, paymentID, input.Type, input.Amount, input.PartnerName, input.Notes, input.ReferenceType, input.ReferenceID, ledger.Actor(c)); err != nil {
		shared.JSONBadRequest(c, err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.FireAfter("payment.created", payload)
	shared.JSONCreated(c, gin.H{"ok": true})
}

// paymentRejectedMsg 区分"订单不存在"与"付款超出来单未收金额"两种情况。
func paymentRejectedMsg(ctx context.Context, tx *sql.Tx, table string, id uuid.UUID) string {
	var exists bool
	_ = tx.QueryRowContext(ctx, fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE id = $1)", table), id).Scan(&exists)
	if !exists {
		return "关联单据不存在"
	}
	return "付款金额超出单据未收金额"
}

func (h *Handler) CashflowAPI(c *gin.Context) {
	ctx := c.Request.Context()

	cashflow, err := getCashflow(ctx, h.db)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	rp, err := getReceivablePayable(ctx, h.db)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"cashflow": cashflow, "receivable_payable": rp})
}

func getCashflow(ctx context.Context, db *sql.DB) (CashflowView, error) {
	var income decimal.Decimal
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(amount AS numeric)), 0) FROM payments WHERE type = '收入' AND payment_date >= DATE_TRUNC('month', CURRENT_DATE)::date`).Scan(&income); err != nil {
		return CashflowView{}, err
	}

	var expense decimal.Decimal
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(total_amount AS numeric)), 0) FROM purchase_orders WHERE order_date >= DATE_TRUNC('month', CURRENT_DATE)::date AND status != 'cancelled'`).Scan(&expense); err != nil {
		return CashflowView{}, err
	}

	return CashflowView{
		MonthIncome:  income.StringFixed(2),
		MonthExpense: expense.StringFixed(2),
		NetCashflow:  income.Sub(expense).StringFixed(2),
	}, nil
}

func getReceivablePayable(ctx context.Context, db *sql.DB) (ReceivablePayableView, error) {
	var receivable decimal.Decimal
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)), 0) FROM sales_orders WHERE status != 'cancelled'`).Scan(&receivable); err != nil {
		return ReceivablePayableView{}, err
	}

	var payable decimal.Decimal
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)), 0) FROM purchase_orders WHERE status != 'cancelled'`).Scan(&payable); err != nil {
		return ReceivablePayableView{}, err
	}

	return ReceivablePayableView{
		TotalReceivable: receivable.StringFixed(2),
		TotalPayable:    payable.StringFixed(2),
	}, nil
}

func (h *Handler) CashflowTrendAPI(c *gin.Context) {
	ctx := c.Request.Context()

	trend, err := getCashflowTrend(ctx, h.db)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, trend)
}

func (h *Handler) ReceivableAgingAPI(c *gin.Context) {
	ctx := c.Request.Context()

	aging, err := getReceivableAging(ctx, h.db)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, aging)
}

type cashflowTrendMonth struct {
	Label   string `json:"label"`
	Type    string `json:"type"`
	Income  string `json:"income"`
	Expense string `json:"expense"`
	Net     string `json:"net"`
}

type cashflowTrendResponse struct {
	Months  []cashflowTrendMonth `json:"months"`
	Summary cashflowTrendSummary `json:"summary"`
}

type cashflowTrendSummary struct {
	TotalReceivable string                 `json:"total_receivable"`
	TotalPayable    string                 `json:"total_payable"`
	Aging           map[string]agingBucket `json:"aging"`
}

type agingBucket struct {
	Receivable string `json:"receivable"`
	Payable    string `json:"payable"`
}

func getCashflowTrend(ctx context.Context, db *sql.DB) (cashflowTrendResponse, error) {
	var months []cashflowTrendMonth

	// SQLite 无 generate_series/LATERAL，使用 Go 侧按月查询实现
	if shared.IsSQLite() {
		var err error
		if months, err = cashflowMonthsSQLite(ctx, db); err != nil {
			return cashflowTrendResponse{}, err
		}
		receivable, _ := getAgingReceivable(ctx, db)
		payable, _ := getAgingPayable(ctx, db)
		return cashflowTrendResponse{
			Months: months,
			Summary: cashflowTrendSummary{
				TotalReceivable: receivable.total.StringFixed(2),
				TotalPayable:    payable.total.StringFixed(2),
				Aging: map[string]agingBucket{
					"0_30":    {Receivable: receivable.ranges["0_30"], Payable: payable.ranges["0_30"]},
					"31_60":   {Receivable: receivable.ranges["31_60"], Payable: payable.ranges["31_60"]},
					"61_90":   {Receivable: receivable.ranges["61_90"], Payable: payable.ranges["61_90"]},
					"90_plus": {Receivable: receivable.ranges["90_plus"], Payable: payable.ranges["90_plus"]},
				},
			},
		}, nil
	}

	actualRows, err := db.QueryContext(ctx, `
		SELECT
			TO_CHAR(d.month, 'YYYY-MM') AS label,
			COALESCE(income.total, 0) AS income,
			COALESCE(expense.total, 0) AS expense
		FROM generate_series(
			DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '5 months',
			DATE_TRUNC('month', CURRENT_DATE),
			'1 month'
		) AS d(month)
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(CAST(amount AS numeric)), 0) AS total
			FROM payments
			WHERE type = '收入'
			  AND payment_date >= d.month
			  AND payment_date < d.month + INTERVAL '1 month'
		) income ON true
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(CAST(total_amount AS numeric)), 0) AS total
			FROM purchase_orders
			WHERE status != 'cancelled'
			  AND order_date >= d.month
			  AND order_date < d.month + INTERVAL '1 month'
		) expense ON true
		ORDER BY d.month
	`)
	if err != nil {
		return cashflowTrendResponse{}, err
	}
	defer actualRows.Close()

	for actualRows.Next() {
		var m cashflowTrendMonth
		var income, expense decimal.Decimal
		if err := actualRows.Scan(&m.Label, &income, &expense); err != nil {
			continue
		}
		m.Type = "actual"
		m.Income = income.StringFixed(2)
		m.Expense = expense.StringFixed(2)
		m.Net = income.Sub(expense).StringFixed(2)
		months = append(months, m)
	}

	hasHistory := false
	if len(months) > 0 {
		for _, m := range months {
			if m.Income != "0.00" || m.Expense != "0.00" {
				hasHistory = true
				break
			}
		}
	}

	var avgIncome, avgExpense decimal.Decimal
	if hasHistory {
		for i := len(months) - 3; i < len(months); i++ {
			if i >= 0 {
				avgIncome = avgIncome.Add(shared.MustParseDecimal(months[i].Income))
				avgExpense = avgExpense.Add(shared.MustParseDecimal(months[i].Expense))
			}
		}
		avgIncome = avgIncome.Div(decimal.NewFromInt(3))
		avgExpense = avgExpense.Div(decimal.NewFromInt(3))
	}

	histWeight := decimal.NewFromFloat(0.6)
	orderWeight := decimal.NewFromFloat(0.4)
	if !hasHistory {
		histWeight = decimal.Zero
		orderWeight = decimal.NewFromInt(1)
	}

	forecastRows, err := db.QueryContext(ctx, `
		SELECT
			TO_CHAR(d.month, 'YYYY-MM') AS label,
			COALESCE(so.expected, 0) AS sales_expected,
			COALESCE(po.expected, 0) AS purchase_expected
		FROM generate_series(
			DATE_TRUNC('month', CURRENT_DATE) + INTERVAL '1 month',
			DATE_TRUNC('month', CURRENT_DATE) + INTERVAL '3 months',
			'1 month'
		) AS d(month)
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)), 0) AS expected
			FROM sales_orders
			WHERE status NOT IN ('cancelled', 'invoiced')
			  AND delivery_date >= d.month
			  AND delivery_date < d.month + INTERVAL '1 month'
		) so ON true
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)), 0) AS expected
			FROM purchase_orders
			WHERE status NOT IN ('cancelled', 'received')
			  AND delivery_date >= d.month
			  AND delivery_date < d.month + INTERVAL '1 month'
		) po ON true
		ORDER BY d.month
	`)
	if err != nil {
		return cashflowTrendResponse{}, err
	}
	defer forecastRows.Close()

	for forecastRows.Next() {
		var m cashflowTrendMonth
		var salesExpected, purchaseExpected decimal.Decimal
		if err := forecastRows.Scan(&m.Label, &salesExpected, &purchaseExpected); err != nil {
			continue
		}
		m.Type = "forecast"

		predictedIncome := avgIncome.Mul(histWeight).Add(salesExpected.Mul(orderWeight))
		predictedExpense := avgExpense.Mul(histWeight).Add(purchaseExpected.Mul(orderWeight))

		m.Income = predictedIncome.StringFixed(2)
		m.Expense = predictedExpense.StringFixed(2)
		m.Net = predictedIncome.Sub(predictedExpense).StringFixed(2)
		months = append(months, m)
	}

	receivable, _ := getAgingReceivable(ctx, db)
	payable, _ := getAgingPayable(ctx, db)

	return cashflowTrendResponse{
		Months: months,
		Summary: cashflowTrendSummary{
			TotalReceivable: receivable.total.StringFixed(2),
			TotalPayable:    payable.total.StringFixed(2),
			Aging: map[string]agingBucket{
				"0_30":    {Receivable: receivable.ranges["0_30"], Payable: payable.ranges["0_30"]},
				"31_60":   {Receivable: receivable.ranges["31_60"], Payable: payable.ranges["31_60"]},
				"61_90":   {Receivable: receivable.ranges["61_90"], Payable: payable.ranges["61_90"]},
				"90_plus": {Receivable: receivable.ranges["90_plus"], Payable: payable.ranges["90_plus"]},
			},
		},
	}, nil
}

type agingResult struct {
	total  decimal.Decimal
	ranges map[string]string
}

// cashflowMonthsSQLite 现金流趋势（SQLite 专用）: 近 6 个月实际 + 下 3 个月预测，
// 与 PostgreSQL 的 generate_series + LATERAL 版本语义一致。
// 使用 GROUP BY 一次性聚合（此前逐月查询 18 次 → 4 次）。
func cashflowMonthsSQLite(ctx context.Context, db *sql.DB) ([]cashflowTrendMonth, error) {
	now := time.Now()
	cur := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// ---- 近 6 个月(含当月) 实际 ----
	actStartStr := cur.AddDate(0, -5, 0).Format("2006-01-02")
	actEndStr := cur.AddDate(0, 1, 0).Format("2006-01-02")

	incomeMap, err := shared.SumByMonthSQLite(ctx, db,
		`SELECT strftime('%Y-%m', payment_date) AS m, COALESCE(SUM(CAST(amount AS NUMERIC)), 0)
		 FROM payments WHERE type = '收入' AND payment_date >= ? AND payment_date < ? GROUP BY m`,
		actStartStr, actEndStr)
	if err != nil {
		return nil, err
	}
	expenseMap, err := shared.SumByMonthSQLite(ctx, db,
		`SELECT strftime('%Y-%m', order_date) AS m, COALESCE(SUM(CAST(total_amount AS NUMERIC)), 0)
		 FROM purchase_orders WHERE status != 'cancelled' AND order_date >= ? AND order_date < ? GROUP BY m`,
		actStartStr, actEndStr)
	if err != nil {
		return nil, err
	}

	months := make([]cashflowTrendMonth, 0, 9)
	for i := 0; i < 6; i++ {
		m := cur.AddDate(0, i-5, 0).Format("2006-01")
		income := incomeMap[m]
		expense := expenseMap[m]
		months = append(months, cashflowTrendMonth{Label: m, Type: "actual",
			Income: income.StringFixed(2), Expense: expense.StringFixed(2),
			Net: income.Sub(expense).StringFixed(2)})
	}

	// 与 PG 版本一致: 最近 3 个月均值作为历史权重
	hasHistory := false
	for _, m := range months {
		if m.Income != "0.00" || m.Expense != "0.00" {
			hasHistory = true
			break
		}
	}
	var avgIncome, avgExpense decimal.Decimal
	if hasHistory {
		for i := len(months) - 3; i < len(months); i++ {
			if i >= 0 {
				avgIncome = avgIncome.Add(shared.MustParseDecimal(months[i].Income))
				avgExpense = avgExpense.Add(shared.MustParseDecimal(months[i].Expense))
			}
		}
		avgIncome = avgIncome.Div(decimal.NewFromInt(3))
		avgExpense = avgExpense.Div(decimal.NewFromInt(3))
	}
	histWeight := decimal.NewFromFloat(0.6)
	orderWeight := decimal.NewFromFloat(0.4)
	if !hasHistory {
		histWeight = decimal.Zero
		orderWeight = decimal.NewFromInt(1)
	}

	// ---- 未来 3 个月预测 ----
	fcStartStr := cur.AddDate(0, 1, 0).Format("2006-01-02")
	fcEndStr := cur.AddDate(0, 4, 0).Format("2006-01-02")
	salesMap, err := shared.SumByMonthSQLite(ctx, db,
		`SELECT strftime('%Y-%m', delivery_date) AS m, COALESCE(SUM(CAST(total_amount AS NUMERIC) - COALESCE(paid_amount, 0)), 0)
		 FROM sales_orders WHERE status NOT IN ('cancelled', 'invoiced') AND delivery_date >= ? AND delivery_date < ? GROUP BY m`,
		fcStartStr, fcEndStr)
	if err != nil {
		return nil, err
	}
	purchaseMap, err := shared.SumByMonthSQLite(ctx, db,
		`SELECT strftime('%Y-%m', delivery_date) AS m, COALESCE(SUM(CAST(total_amount AS NUMERIC) - COALESCE(paid_amount, 0)), 0)
		 FROM purchase_orders WHERE status NOT IN ('cancelled', 'received') AND delivery_date >= ? AND delivery_date < ? GROUP BY m`,
		fcStartStr, fcEndStr)
	if err != nil {
		return nil, err
	}
	for i := 1; i <= 3; i++ {
		m := cur.AddDate(0, i, 0).Format("2006-01")
		salesExpected := salesMap[m]
		purchaseExpected := purchaseMap[m]
		predictedIncome := avgIncome.Mul(histWeight).Add(salesExpected.Mul(orderWeight))
		predictedExpense := avgExpense.Mul(histWeight).Add(purchaseExpected.Mul(orderWeight))
		months = append(months, cashflowTrendMonth{Label: m, Type: "forecast",
			Income: predictedIncome.StringFixed(2), Expense: predictedExpense.StringFixed(2),
			Net: predictedIncome.Sub(predictedExpense).StringFixed(2)})
	}
	return months, nil
}

func getAgingReceivable(ctx context.Context, db *sql.DB) (agingResult, error) {
	result := agingResult{ranges: make(map[string]string)}
	rows, err := db.QueryContext(ctx, `
		SELECT
			CASE
				WHEN CURRENT_DATE - COALESCE(order_date, CURRENT_DATE) BETWEEN 0 AND 30 THEN '0_30'
				WHEN CURRENT_DATE - COALESCE(order_date, CURRENT_DATE) BETWEEN 31 AND 60 THEN '31_60'
				WHEN CURRENT_DATE - COALESCE(order_date, CURRENT_DATE) BETWEEN 61 AND 90 THEN '61_90'
				ELSE '90_plus'
			END AS age_range,
			COALESCE(SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)), 0) AS amount
		FROM sales_orders
		WHERE status NOT IN ('cancelled')
		  AND CAST(total_amount AS numeric) - COALESCE(paid_amount, 0) > 0
		GROUP BY age_range
		ORDER BY MIN(CURRENT_DATE - COALESCE(order_date, CURRENT_DATE))
	`)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		var r string
		var amt decimal.Decimal
		if err := rows.Scan(&r, &amt); err != nil {
			continue
		}
		result.ranges[r] = amt.StringFixed(2)
		result.total = result.total.Add(amt)
	}

	for _, key := range []string{"0_30", "31_60", "61_90", "90_plus"} {
		if _, ok := result.ranges[key]; !ok {
			result.ranges[key] = "0.00"
		}
	}
	return result, nil
}

func getAgingPayable(ctx context.Context, db *sql.DB) (agingResult, error) {
	result := agingResult{ranges: make(map[string]string)}
	rows, err := db.QueryContext(ctx, `
		SELECT
			CASE
				WHEN CURRENT_DATE - COALESCE(order_date, CURRENT_DATE) BETWEEN 0 AND 30 THEN '0_30'
				WHEN CURRENT_DATE - COALESCE(order_date, CURRENT_DATE) BETWEEN 31 AND 60 THEN '31_60'
				WHEN CURRENT_DATE - COALESCE(order_date, CURRENT_DATE) BETWEEN 61 AND 90 THEN '61_90'
				ELSE '90_plus'
			END AS age_range,
			COALESCE(SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)), 0) AS amount
		FROM purchase_orders
		WHERE status NOT IN ('cancelled')
		  AND CAST(total_amount AS numeric) - COALESCE(paid_amount, 0) > 0
		GROUP BY age_range
		ORDER BY MIN(CURRENT_DATE - COALESCE(order_date, CURRENT_DATE))
	`)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		var r string
		var amt decimal.Decimal
		if err := rows.Scan(&r, &amt); err != nil {
			continue
		}
		result.ranges[r] = amt.StringFixed(2)
		result.total = result.total.Add(amt)
	}

	for _, key := range []string{"0_30", "31_60", "61_90", "90_plus"} {
		if _, ok := result.ranges[key]; !ok {
			result.ranges[key] = "0.00"
		}
	}
	return result, nil
}

func getReceivableAging(ctx context.Context, db *sql.DB) ([]AgingView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			CASE
				WHEN CURRENT_DATE - COALESCE(order_date, CURRENT_DATE) BETWEEN 0 AND 30 THEN '0-30天'
				WHEN CURRENT_DATE - COALESCE(order_date, CURRENT_DATE) BETWEEN 31 AND 60 THEN '31-60天'
				WHEN CURRENT_DATE - COALESCE(order_date, CURRENT_DATE) BETWEEN 61 AND 90 THEN '61-90天'
				ELSE '90天以上'
			END AS age_range,
			COUNT(*) AS count,
			COALESCE(SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)), 0) AS amount
		FROM sales_orders
		WHERE status NOT IN ('cancelled')
		  AND CAST(total_amount AS numeric) - COALESCE(paid_amount, 0) > 0
		GROUP BY age_range
		ORDER BY MIN(CURRENT_DATE - COALESCE(order_date, CURRENT_DATE))
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []AgingView
	var maxAmt decimal.Decimal
	for rows.Next() {
		var item AgingView
		var amt decimal.Decimal
		if err := rows.Scan(&item.Range, &item.Count, &amt); err != nil {
			continue
		}
		item.Amount = amt.StringFixed(2)
		if amt.GreaterThan(maxAmt) {
			maxAmt = amt
		}
		items = append(items, item)
	}
	maxVal, _ := maxAmt.Float64()
	for i := range items {
		items[i].MaxAmount = maxVal
	}
	return items, nil
}

func (h *Handler) MonthOverMonthAPI(c *gin.Context) {
	ctx := c.Request.Context()

	current, prev, err := getMonthOverMonth(ctx, h.db)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	calcChange := func(cur, prev decimal.Decimal) string {
		if prev.IsZero() {
			if cur.IsZero() {
				return "0.0"
			}
			return "100.0"
		}
		return cur.Sub(prev).Div(prev).Mul(decimal.NewFromInt(100)).StringFixed(1)
	}

	shared.JSONOK(c, gin.H{
		"month_income":   current.income.StringFixed(2),
		"month_expense":  current.expense.StringFixed(2),
		"net_cashflow":   current.income.Sub(current.expense).StringFixed(2),
		"income_change":  calcChange(current.income, prev.income),
		"expense_change": calcChange(current.expense, prev.expense),
		"net_change":     calcChange(current.income.Sub(current.expense), prev.income.Sub(prev.expense)),
	})
}

type monthCashflow struct {
	income  decimal.Decimal
	expense decimal.Decimal
}

func getMonthOverMonth(ctx context.Context, db *sql.DB) (current, previous monthCashflow, err error) {
	for i, mc := range []*monthCashflow{&current, &previous} {
		offset := i
		var income, expense decimal.Decimal
		if err := db.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT COALESCE(SUM(CAST(amount AS numeric)), 0)
			FROM payments
			WHERE type = '收入'
			  AND payment_date >= DATE_TRUNC('month', CURRENT_DATE)::date - INTERVAL '%d months'
			  AND payment_date < DATE_TRUNC('month', CURRENT_DATE)::date - INTERVAL '%d months'
		`, offset, offset-1)).Scan(&income); err != nil {
			return current, previous, err
		}

		if err := db.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT COALESCE(SUM(CAST(total_amount AS numeric)), 0)
			FROM purchase_orders
			WHERE status != 'cancelled'
			  AND order_date >= DATE_TRUNC('month', CURRENT_DATE)::date - INTERVAL '%d months'
			  AND order_date < DATE_TRUNC('month', CURRENT_DATE)::date - INTERVAL '%d months'
		`, offset, offset-1)).Scan(&expense); err != nil {
			return current, previous, err
		}

		mc.income = income
		mc.expense = expense
	}
	return
}
