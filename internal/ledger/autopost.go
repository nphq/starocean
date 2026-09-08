package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func OnBusinessEvent(ctx context.Context, db DBTX, event string, id uuid.UUID, preparedBy string) error {
	settings, err := loadSettings(ctx, db)
	if err != nil {
		return err
	}
	if !settings.AutoPost {
		return nil
	}
	// 期间锁账：已结账期间禁止倒开业务单据（调用方回滚整个事务）。
	// 只在 AutoPost 开启时拦——关闭自动过账意味着账套不管，业务照常走。
	if err := assertPeriodOpen(ctx, db); err != nil {
		return err
	}
	switch event {
	case "sales_order.confirmed":
		return postSalesConfirm(ctx, db, settings, id, preparedBy)
	case "sales_order.cancelled", "sales_order.deleted":
		return ReverseBySource(ctx, db, "sales_order", id, preparedBy)
	case "purchase_order.received":
		return postPurchaseReceive(ctx, db, settings, id, preparedBy)
	case "purchase_order.cancelled", "purchase_order.deleted":
		exists, err := hasSourceVoucher(ctx, db, "purchase_order", id)
		if err != nil {
			return err
		}
		if exists {
			if err := revertMovingAverage(ctx, db, id); err != nil {
				return err
			}
		}
		return ReverseBySource(ctx, db, "purchase_order", id, preparedBy)
	default:
		return nil
	}
}

// assertPeriodOpen 当前自然期间必须未结账；无记录视为 open（种子覆盖 2024-2028）。
func assertPeriodOpen(ctx context.Context, db DBTX) error {
	now := time.Now()
	var status string
	err := db.QueryRowContext(ctx, `SELECT status FROM gl_periods WHERE year=$1 AND month=$2 AND company_id='default'`,
		now.Year(), int(now.Month())).Scan(&status)
	if err != nil {
		return nil
	}
	if status == "closed" {
		return fmt.Errorf("会计期间 %d年%d月已结账，禁止倒开单据", now.Year(), int(now.Month()))
	}
	return nil
}

func PostPayment(ctx context.Context, db DBTX, paymentID uuid.UUID, typ, amount, partner, notes, refType string, refID uuid.UUID, preparedBy string) error {
	settings, err := loadSettings(ctx, db)
	if err != nil {
		return err
	}
	if !settings.AutoPost {
		return nil
	}
	if err := assertPeriodOpen(ctx, db); err != nil {
		return err
	}
	exists, err := hasSourceVoucher(ctx, db, "payment", paymentID)
	if err != nil || exists {
		return err
	}
	amt, err := parseMoney(amount)
	if err != nil || amt.LessThanOrEqual(decimal.Zero) {
		return err
	}
	cash := settings.BankAccount
	summary := strings.TrimSpace(partner + " " + notes)
	if summary == "" {
		summary = "收付款"
	}
	var lines []LineInput
	if typ == "收入" || typ == "income" {
		credit := settings.ARAccount
		if refType != "sales_order" {
			credit = settings.RevenueAccount
		}
		lines = []LineInput{
			{AccountCode: cash, Summary: summary, Debit: amt.StringFixed(2), PartnerName: partner},
			{AccountCode: credit, Summary: summary, Credit: amt.StringFixed(2), PartnerType: partnerType(refType), PartnerID: idStr(refID), PartnerName: partner},
		}
	} else {
		debit := settings.APAccount
		if refType != "purchase_order" {
			debit = settings.OpexAccount
		}
		lines = []LineInput{
			{AccountCode: debit, Summary: summary, Debit: amt.StringFixed(2), PartnerType: partnerType(refType), PartnerID: idStr(refID), PartnerName: partner},
			{AccountCode: cash, Summary: summary, Credit: amt.StringFixed(2), PartnerName: partner},
		}
	}
	_, err = createAndPost(ctx, db, VoucherInput{
		Word:        "记",
		VoucherDate: todayDate(),
		Summary:     summary,
		SourceType:  "payment",
		SourceID:    paymentID.String(),
		Lines:       lines,
	}, preparedBy)
	return err
}

func PostReimbursement(ctx context.Context, db DBTX, id uuid.UUID, amount, category, applicant string, preparedBy string) error {
	settings, err := loadSettings(ctx, db)
	if err != nil {
		return err
	}
	if !settings.AutoPost {
		return nil
	}
	if err := assertPeriodOpen(ctx, db); err != nil {
		return err
	}
	exists, err := hasSourceVoucher(ctx, db, "reimbursement", id)
	if err != nil || exists {
		return err
	}
	amt, err := parseMoney(amount)
	if err != nil {
		return err
	}
	expense := settings.OpexAccount
	if category == "差旅" || category == "招待" {
		expense = "6601"
	}
	summary := "报销 " + applicant + " " + category
	_, err = createAndPost(ctx, db, VoucherInput{
		Word:        "付",
		VoucherDate: todayDate(),
		Summary:     summary,
		SourceType:  "reimbursement",
		SourceID:    id.String(),
		Lines: []LineInput{
			{AccountCode: expense, Summary: summary, Debit: amt.StringFixed(2), PartnerName: applicant},
			{AccountCode: settings.CashAccount, Summary: summary, Credit: amt.StringFixed(2), PartnerName: applicant},
		},
	}, preparedBy)
	return err
}

func PostSalary(ctx context.Context, db DBTX, id uuid.UUID, gross, net, withhold decimal.Decimal, employee, month string, preparedBy string) error {
	settings, err := loadSettings(ctx, db)
	if err != nil {
		return err
	}
	if !settings.AutoPost {
		return nil
	}
	exists, err := hasSourceVoucher(ctx, db, "salary", id)
	if err != nil || exists {
		return err
	}
	if gross.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	summary := "计提并发放工资 " + employee + " " + month
	lines := []LineInput{
		{AccountCode: settings.OpexAccount, Summary: summary, Debit: money(gross).StringFixed(2), PartnerName: employee},
	}
	if net.GreaterThan(decimal.Zero) {
		lines = append(lines, LineInput{AccountCode: settings.BankAccount, Summary: summary, Credit: money(net).StringFixed(2), PartnerName: employee})
	}
	if withhold.GreaterThan(decimal.Zero) {
		lines = append(lines, LineInput{AccountCode: settings.PayrollAccount, Summary: "代扣社保公积金个税", Credit: money(withhold).StringFixed(2), PartnerName: employee})
	}
	if len(lines) < 2 {
		return nil
	}
	_, err = createAndPost(ctx, db, VoucherInput{
		Word:        "记",
		VoucherDate: todayDate(),
		Summary:     summary,
		SourceType:  "salary",
		SourceID:    id.String(),
		Lines:       lines,
	}, preparedBy)
	return err
}

func PostStockAdjust(ctx context.Context, db DBTX, adjType string, qty int32, cost, name string, movementID uuid.UUID, preparedBy string) error {
	settings, err := loadSettings(ctx, db)
	if err != nil {
		return err
	}
	if !settings.AutoPost {
		return nil
	}
	unit, err := parseMoney(cost)
	if err != nil {
		return err
	}
	amt := unit.Mul(decimal.NewFromInt(int64(qty)))
	if amt.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	summary := "库存调整 " + name
	var lines []LineInput
	if adjType == "in" {
		lines = []LineInput{
			{AccountCode: settings.InventoryAccount, Summary: summary, Debit: amt.StringFixed(2)},
			{AccountCode: settings.SurplusAccount, Summary: summary, Credit: amt.StringFixed(2)},
		}
	} else {
		lines = []LineInput{
			{AccountCode: settings.SurplusAccount, Summary: summary, Debit: amt.StringFixed(2)},
			{AccountCode: settings.InventoryAccount, Summary: summary, Credit: amt.StringFixed(2)},
		}
	}
	_, err = createAndPost(ctx, db, VoucherInput{
		Word:        "转",
		VoucherDate: todayDate(),
		Summary:     summary,
		SourceType:  "stock_adjust",
		SourceID:    movementID.String(),
		Lines:       lines,
	}, preparedBy)
	return err
}

func postSalesConfirm(ctx context.Context, db DBTX, settings Settings, id uuid.UUID, preparedBy string) error {
	exists, err := hasSourceVoucher(ctx, db, "sales_order", id)
	if err != nil || exists {
		return err
	}
	var orderNo, customerName, total string
	var customerID uuid.UUID
	err = db.QueryRowContext(ctx, `
		SELECT so.order_no, so.customer_id, COALESCE(c.name,''), COALESCE(so.total_amount,0)::text
		FROM sales_orders so
		LEFT JOIN customers c ON c.id = so.customer_id
		WHERE so.id=$1`, id).Scan(&orderNo, &customerID, &customerName, &total)
	if err != nil {
		return fmt.Errorf("读取销售订单: %w", err)
	}
	amt, err := parseMoney(total)
	if err != nil {
		return err
	}
	cogs, err := salesCOGS(ctx, db, id)
	if err != nil {
		return err
	}
	summary := "销售 " + orderNo + " " + customerName
	cid := customerID.String()
	lines := []LineInput{}
	if amt.GreaterThan(decimal.Zero) {
		lines = append(lines,
			LineInput{AccountCode: settings.ARAccount, Summary: summary, Debit: amt.StringFixed(2), PartnerType: "customer", PartnerID: cid, PartnerName: customerName},
			LineInput{AccountCode: settings.RevenueAccount, Summary: summary, Credit: amt.StringFixed(2), PartnerType: "customer", PartnerID: cid, PartnerName: customerName},
		)
	}
	if cogs.GreaterThan(decimal.Zero) {
		lines = append(lines,
			LineInput{AccountCode: settings.COGSAccount, Summary: "结转成本 " + orderNo, Debit: cogs.StringFixed(2)},
			LineInput{AccountCode: settings.InventoryAccount, Summary: "结转成本 " + orderNo, Credit: cogs.StringFixed(2)},
		)
	}
	if len(lines) < 2 {
		return nil
	}
	_, err = createAndPost(ctx, db, VoucherInput{
		Word:        "记",
		VoucherDate: todayDate(),
		Summary:     summary,
		SourceType:  "sales_order",
		SourceID:    id.String(),
		Lines:       lines,
	}, preparedBy)
	return err
}

func salesCOGS(ctx context.Context, db DBTX, orderID uuid.UUID) (decimal.Decimal, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT soi.quantity, COALESCE(p.cost_price,0)
		FROM sales_order_items soi
		JOIN products p ON p.id = soi.product_id
		WHERE soi.order_id=$1`, orderID)
	if err != nil {
		return decimal.Zero, err
	}
	defer rows.Close()
	total := decimal.Zero
	for rows.Next() {
		var qty int32
		var cost decimal.Decimal
		if err := rows.Scan(&qty, &cost); err != nil {
			return decimal.Zero, err
		}
		total = total.Add(cost.Mul(decimal.NewFromInt(int64(qty))))
	}
	return money(total), rows.Err()
}

func postPurchaseReceive(ctx context.Context, db DBTX, settings Settings, id uuid.UUID, preparedBy string) error {
	exists, err := hasSourceVoucher(ctx, db, "purchase_order", id)
	if err != nil || exists {
		return err
	}
	var orderNo, supplierName, total string
	var supplierID uuid.UUID
	err = db.QueryRowContext(ctx, `
		SELECT po.order_no, po.supplier_id, COALESCE(s.name,''), COALESCE(po.total_amount,0)::text
		FROM purchase_orders po
		LEFT JOIN suppliers s ON s.id = po.supplier_id
		WHERE po.id=$1`, id).Scan(&orderNo, &supplierID, &supplierName, &total)
	if err != nil {
		return fmt.Errorf("读取采购订单: %w", err)
	}
	if err := updateMovingAverage(ctx, db, id); err != nil {
		return err
	}
	amt, err := parseMoney(total)
	if err != nil {
		return err
	}
	if amt.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	summary := "采购入库 " + orderNo + " " + supplierName
	sid := supplierID.String()
	_, err = createAndPost(ctx, db, VoucherInput{
		Word:        "记",
		VoucherDate: todayDate(),
		Summary:     summary,
		SourceType:  "purchase_order",
		SourceID:    id.String(),
		Lines: []LineInput{
			{AccountCode: settings.InventoryAccount, Summary: summary, Debit: amt.StringFixed(2), PartnerType: "supplier", PartnerID: sid, PartnerName: supplierName},
			{AccountCode: settings.APAccount, Summary: summary, Credit: amt.StringFixed(2), PartnerType: "supplier", PartnerID: sid, PartnerName: supplierName},
		},
	}, preparedBy)
	return err
}

func updateMovingAverage(ctx context.Context, db DBTX, purchaseID uuid.UUID) error {
	rows, err := db.QueryContext(ctx, `
		SELECT poi.product_id, poi.quantity, poi.unit_price, COALESCE(p.current_stock,0), COALESCE(p.cost_price,0)
		FROM purchase_order_items poi
		JOIN products p ON p.id = poi.product_id
		WHERE poi.order_id=$1`, purchaseID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		id             uuid.UUID
		qty, stock     int32
		price, oldCost decimal.Decimal
	}
	var items []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.qty, &r.price, &r.stock, &r.oldCost); err != nil {
			return err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range items {
		oldQty := decimal.NewFromInt(int64(r.stock - r.qty))
		if oldQty.LessThan(decimal.Zero) {
			oldQty = decimal.Zero
		}
		newQty := decimal.NewFromInt(int64(r.stock))
		if newQty.LessThanOrEqual(decimal.Zero) {
			continue
		}
		newCost := oldQty.Mul(r.oldCost).Add(decimal.NewFromInt(int64(r.qty)).Mul(r.price)).Div(newQty)
		if _, err := db.ExecContext(ctx, `UPDATE products SET cost_price=$2, updated_at=NOW() WHERE id=$1`, r.id, money(newCost)); err != nil {
			return err
		}
	}
	return nil
}

func revertMovingAverage(ctx context.Context, db DBTX, purchaseID uuid.UUID) error {
	rows, err := db.QueryContext(ctx, `
		SELECT poi.product_id, SUM(poi.quantity), SUM(poi.quantity * poi.unit_price),
		       COALESCE(MAX(p.current_stock),0), COALESCE(MAX(p.cost_price),0)
		FROM purchase_order_items poi
		JOIN products p ON p.id = poi.product_id
		WHERE poi.order_id=$1
		GROUP BY poi.product_id`, purchaseID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		id          uuid.UUID
		qty, stock  int32
		total, cost decimal.Decimal
	}
	var items []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.qty, &r.total, &r.stock, &r.cost); err != nil {
			return err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range items {
		s := decimal.NewFromInt(int64(r.stock))
		if s.LessThanOrEqual(decimal.Zero) {
			continue
		}
		q := decimal.NewFromInt(int64(r.qty))
		oldCost := r.cost.Mul(s.Add(q)).Sub(r.total).Div(s)
		if oldCost.LessThan(decimal.Zero) {
			oldCost = decimal.Zero
		}
		if _, err := db.ExecContext(ctx, `UPDATE products SET cost_price=$2, updated_at=NOW() WHERE id=$1`, r.id, money(oldCost)); err != nil {
			return err
		}
	}
	return nil
}

func partnerType(ref string) string {
	switch ref {
	case "sales_order":
		return "customer"
	case "purchase_order":
		return "supplier"
	default:
		return ""
	}
}

func idStr(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func todayDate() string {
	return time.Now().Format("2006-01-02")
}
