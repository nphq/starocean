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
	// 价税分离：逐行按“差额法”拆分净额/税额，恒有 net+tax=amount，保证借贷平衡。
	revenue, outTax, err := splitOrderTax(ctx, db, "sales_order_items", id)
	if err != nil {
		return err
	}
	// 无明细行（如手工/种子订单）时退化为全额不含税，保持旧行为。
	if revenue.IsZero() && outTax.IsZero() && amt.GreaterThan(decimal.Zero) {
		revenue = amt
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
			LineInput{AccountCode: settings.RevenueAccount, Summary: summary, Credit: revenue.StringFixed(2), PartnerType: "customer", PartnerID: cid, PartnerName: customerName},
		)
		if outTax.GreaterThan(decimal.Zero) {
			lines = append(lines,
				LineInput{AccountCode: settings.OutputTaxAccount, Summary: summary + " 销项税", Credit: outTax.StringFixed(2), PartnerType: "customer", PartnerID: cid, PartnerName: customerName},
			)
		}
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

// splitOrderTax 逐行按差额法拆分含税额：net = round(amount/(1+rate/100), 2)，tax = amount - net。
// 返回净额合计、税额合计。rate 全 0 时退化为 net=amount、tax=0。
func splitOrderTax(ctx context.Context, db DBTX, itemTable string, orderID uuid.UUID) (decimal.Decimal, decimal.Decimal, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		`SELECT COALESCE(amount,0), COALESCE(tax_rate,0) FROM %s WHERE order_id=$1`, itemTable), orderID)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	defer rows.Close()
	net, tax := decimal.Zero, decimal.Zero
	for rows.Next() {
		var amt, rate decimal.Decimal
		if err := rows.Scan(&amt, &rate); err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		n, t := splitTax(amt, rate)
		net = net.Add(n)
		tax = tax.Add(t)
	}
	return money(net), money(tax), rows.Err()
}

// splitTax 单行差额法拆分。rate 为百分比（13.00 = 13%）。
func splitTax(amount, rate decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	if amount.IsZero() {
		return decimal.Zero, decimal.Zero
	}
	if rate.IsZero() {
		return money(amount), decimal.Zero
	}
	denom := decimal.NewFromInt(100).Add(rate)
	net := money(amount.Mul(decimal.NewFromInt(100)).Div(denom))
	tax := money(amount.Sub(net))
	return net, tax
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
	invAmt, inTax, err := splitOrderTax(ctx, db, "purchase_order_items", id)
	if err != nil {
		return err
	}
	// 无明细行（如手工/种子订单）时退化为全额不计税，保持旧行为。
	if invAmt.IsZero() && inTax.IsZero() {
		invAmt = amt
	}
	summary := "采购入库 " + orderNo + " " + supplierName
	sid := supplierID.String()
	lines := []LineInput{
		{AccountCode: settings.InventoryAccount, Summary: summary, Debit: invAmt.StringFixed(2), PartnerType: "supplier", PartnerID: sid, PartnerName: supplierName},
		{AccountCode: settings.APAccount, Summary: summary, Credit: amt.StringFixed(2), PartnerType: "supplier", PartnerID: sid, PartnerName: supplierName},
	}
	if inTax.GreaterThan(decimal.Zero) {
		lines = append(lines, LineInput{AccountCode: settings.InputTaxAccount, Summary: summary + " 进项税", Debit: inTax.StringFixed(2), PartnerType: "supplier", PartnerID: sid, PartnerName: supplierName})
	}
	_, err = createAndPost(ctx, db, VoucherInput{
		Word:        "记",
		VoucherDate: todayDate(),
		Summary:     summary,
		SourceType:  "purchase_order",
		SourceID:    id.String(),
		Lines:       lines,
	}, preparedBy)
	return err
}

func updateMovingAverage(ctx context.Context, db DBTX, purchaseID uuid.UUID) error {
	rows, err := db.QueryContext(ctx, `
		SELECT poi.product_id, poi.quantity, COALESCE(poi.amount,0), COALESCE(poi.tax_rate,0),
		       COALESCE(p.current_stock,0), COALESCE(p.cost_price,0)
		FROM purchase_order_items poi
		JOIN products p ON p.id = poi.product_id
		WHERE poi.order_id=$1`, purchaseID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type agg struct {
		qty     int32
		stock   int32
		net     decimal.Decimal
		oldCost decimal.Decimal
	}
	byProd := map[uuid.UUID]*agg{}
	for rows.Next() {
		var pid uuid.UUID
		var qty int32
		var gross, rate, stock, oldCost decimal.Decimal
		if err := rows.Scan(&pid, &qty, &gross, &rate, &stock, &oldCost); err != nil {
			return err
		}
		a := byProd[pid]
		if a == nil {
			a = &agg{stock: int32(stock.IntPart()), oldCost: oldCost}
			byProd[pid] = a
		}
		a.qty += qty
		net, _ := splitTax(gross, rate)
		a.net = a.net.Add(net)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for pid, a := range byProd {
		oldQty := decimal.NewFromInt(int64(a.stock - a.qty))
		if oldQty.LessThan(decimal.Zero) {
			oldQty = decimal.Zero
		}
		newQty := decimal.NewFromInt(int64(a.stock))
		if newQty.LessThanOrEqual(decimal.Zero) {
			continue
		}
		// 用净额(不含可抵扣进项)入成本；与 revertMovingAverage 对称。
		newCost := oldQty.Mul(a.oldCost).Add(a.net).Div(newQty)
		if _, err := db.ExecContext(ctx, `UPDATE products SET cost_price=$2, updated_at=NOW() WHERE id=$1`, pid, money(newCost)); err != nil {
			return err
		}
	}
	return nil
}

func revertMovingAverage(ctx context.Context, db DBTX, purchaseID uuid.UUID) error {
	rows, err := db.QueryContext(ctx, `
		SELECT poi.product_id, poi.quantity, COALESCE(poi.amount,0), COALESCE(poi.tax_rate,0),
		       COALESCE(p.current_stock,0), COALESCE(p.cost_price,0)
		FROM purchase_order_items poi
		JOIN products p ON p.id = poi.product_id
		WHERE poi.order_id=$1`, purchaseID)
	if err != nil {
		return err
	}
	defer rows.Close()
	// 按商品聚合净额（同单多行同商品必须合并计算，且逐行税率拆分不能在 SQL 里 SUM），
	// 与 updateMovingAverage 的写法对称。
	type agg struct {
		qty   int32
		stock int32
		net   decimal.Decimal
		cost  decimal.Decimal
	}
	byProd := map[uuid.UUID]*agg{}
	for rows.Next() {
		var pid uuid.UUID
		var qty int32
		var gross, rate, stock, cost decimal.Decimal
		if err := rows.Scan(&pid, &qty, &gross, &rate, &stock, &cost); err != nil {
			return err
		}
		a := byProd[pid]
		if a == nil {
			a = &agg{stock: int32(stock.IntPart()), cost: cost}
			byProd[pid] = a
		}
		a.qty += qty
		net, _ := splitTax(gross, rate)
		a.net = a.net.Add(net)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for pid, a := range byProd {
		s := decimal.NewFromInt(int64(a.stock))
		if s.LessThanOrEqual(decimal.Zero) {
			continue
		}
		q := decimal.NewFromInt(int64(a.qty))
		// 净额口径，与 updateMovingAverage 对称：oldCost = (cost*(stock+qty) - netAmount)/stock
		oldCost := a.cost.Mul(s.Add(q)).Sub(a.net).Div(s)
		if oldCost.LessThan(decimal.Zero) {
			oldCost = decimal.Zero
		}
		if _, err := db.ExecContext(ctx, `UPDATE products SET cost_price=$2, updated_at=NOW() WHERE id=$1`, pid, money(oldCost)); err != nil {
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
