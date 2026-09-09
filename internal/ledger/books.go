package ledger

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type BalanceRow struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Category      string `json:"category"`
	NormalSide    string `json:"normal_side"`
	IsLeaf        bool   `json:"is_leaf"`
	Level         int    `json:"level"`
	OpeningDebit  string `json:"opening_debit"`
	OpeningCredit string `json:"opening_credit"`
	PeriodDebit   string `json:"period_debit"`
	PeriodCredit  string `json:"period_credit"`
	EndDebit      string `json:"end_debit"`
	EndCredit     string `json:"end_credit"`
}

type TrialBalanceResult struct {
	Year         int          `json:"year"`
	Month        int          `json:"month"`
	Rows         []BalanceRow `json:"rows"`
	DebitTotal   string       `json:"debit_total"`
	CreditTotal  string       `json:"credit_total"`
	Balanced     bool         `json:"balanced"`
	IncomeNet    string       `json:"income_net"`
	AssetNet     string       `json:"asset_net"`
	LiabilityNet string       `json:"liability_net"`
	EquityNet    string       `json:"equity_net"`
}

func TrialBalance(ctx context.Context, db DBTX, year, month int, leavesOnly bool) (TrialBalanceResult, error) {
	out := TrialBalanceResult{Year: year, Month: month}
	accounts, err := ListAccounts(ctx, db, "", "", false)
	if err != nil {
		return out, err
	}
	type amt struct {
		openDr, openCr, perDr, perCr decimal.Decimal
	}
	byCode := map[string]*amt{}
	rows, err := db.QueryContext(ctx, `
		SELECT account_code, opening_debit, opening_credit, period_debit, period_credit
		FROM gl_account_balances
		WHERE period_year=$1 AND period_month=$2 AND company_id=$3`, year, month, companyID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		var a amt
		if err := rows.Scan(&code, &a.openDr, &a.openCr, &a.perDr, &a.perCr); err != nil {
			return out, err
		}
		cp := a
		byCode[code] = &cp
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	accBy := map[string]Account{}
	children := map[string][]string{}
	for _, a := range accounts {
		accBy[a.Code] = a
		if a.ParentCode != "" {
			children[a.ParentCode] = append(children[a.ParentCode], a.Code)
		}
	}
	var roll func(string) amt
	roll = func(code string) amt {
		sum := amt{}
		if leaf, ok := byCode[code]; ok {
			sum = *leaf
		}
		for _, ch := range children[code] {
			c := roll(ch)
			sum.openDr = sum.openDr.Add(c.openDr)
			sum.openCr = sum.openCr.Add(c.openCr)
			sum.perDr = sum.perDr.Add(c.perDr)
			sum.perCr = sum.perCr.Add(c.perCr)
		}
		cp := sum
		byCode[code] = &cp
		return sum
	}
	for _, a := range accounts {
		if a.ParentCode == "" {
			roll(a.Code)
		}
	}

	levelOf := map[string]int{}
	var lvl func(string) int
	lvl = func(code string) int {
		if v, ok := levelOf[code]; ok {
			return v
		}
		a := accBy[code]
		if a.ParentCode == "" {
			levelOf[code] = 1
			return 1
		}
		levelOf[code] = lvl(a.ParentCode) + 1
		return levelOf[code]
	}

	var debitTotal, creditTotal, incomeNet, assetNet, liabNet, equityNet decimal.Decimal
	for _, a := range accounts {
		am := byCode[a.Code]
		if am == nil {
			am = &amt{}
		}
		endDr, endCr := ending(am.openDr, am.openCr, am.perDr, am.perCr)
		zero := am.openDr.IsZero() && am.openCr.IsZero() && am.perDr.IsZero() && am.perCr.IsZero()
		if zero {
			continue
		}
		if leavesOnly && !a.IsLeaf {
			continue
		}
		row := BalanceRow{
			Code:          a.Code,
			Name:          a.Name,
			Category:      a.Category,
			NormalSide:    a.NormalSide,
			IsLeaf:        a.IsLeaf,
			Level:         lvl(a.Code),
			OpeningDebit:  am.openDr.StringFixed(2),
			OpeningCredit: am.openCr.StringFixed(2),
			PeriodDebit:   am.perDr.StringFixed(2),
			PeriodCredit:  am.perCr.StringFixed(2),
			EndDebit:      endDr.StringFixed(2),
			EndCredit:     endCr.StringFixed(2),
		}
		out.Rows = append(out.Rows, row)
		if a.IsLeaf {
			debitTotal = debitTotal.Add(endDr)
			creditTotal = creditTotal.Add(endCr)
			net := endDr.Sub(endCr)
			switch a.Category {
			case "income":
				incomeNet = incomeNet.Sub(net)
			case "expense":
				incomeNet = incomeNet.Sub(net)
			case "asset":
				assetNet = assetNet.Add(net)
			case "liability":
				liabNet = liabNet.Sub(net)
			case "equity":
				equityNet = equityNet.Sub(net)
			}
		}
	}
	sort.Slice(out.Rows, func(i, j int) bool { return out.Rows[i].Code < out.Rows[j].Code })
	out.DebitTotal = money(debitTotal).StringFixed(2)
	out.CreditTotal = money(creditTotal).StringFixed(2)
	out.Balanced = debitTotal.Equal(creditTotal)
	out.IncomeNet = money(incomeNet).StringFixed(2)
	out.AssetNet = money(assetNet).StringFixed(2)
	out.LiabilityNet = money(liabNet).StringFixed(2)
	out.EquityNet = money(equityNet).StringFixed(2)
	return out, nil
}

type LedgerEntry struct {
	Date        string `json:"date"`
	VoucherID   string `json:"voucher_id"`
	VoucherNo   string `json:"voucher_no"`
	Summary     string `json:"summary"`
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
	Balance     string `json:"balance"`
	Side        string `json:"side"`
	PartnerName string `json:"partner_name,omitempty"`
	Status      string `json:"status"`
}

type AccountLedger struct {
	Account Account       `json:"account"`
	Year    int           `json:"year"`
	Month   int           `json:"month"`
	Opening string        `json:"opening"`
	Entries []LedgerEntry `json:"entries"`
	Closing string        `json:"closing"`
	Debit   string        `json:"period_debit"`
	Credit  string        `json:"period_credit"`
}

func AccountBook(ctx context.Context, db DBTX, code string, year, month int) (AccountLedger, error) {
	acc, err := GetAccount(ctx, db, code)
	if err != nil {
		return AccountLedger{}, err
	}
	out := AccountLedger{Account: acc, Year: year, Month: month}
	var openDr, openCr decimal.Decimal
	_ = db.QueryRowContext(ctx, `
		SELECT opening_debit, opening_credit FROM gl_account_balances
		WHERE period_year=$1 AND period_month=$2 AND account_code=$3`, year, month, code).Scan(&openDr, &openCr)
	running := openDr.Sub(openCr)
	out.Opening = running.StringFixed(2)

	q := `
		SELECT v.voucher_date, v.id, v.voucher_no, COALESCE(NULLIF(l.summary,''), v.summary),
		       l.debit, l.credit, l.partner_name, v.status
		FROM gl_voucher_lines l
		JOIN gl_vouchers v ON v.id = l.voucher_id
		WHERE l.account_code=$1 AND v.status='posted' AND v.period_year=$2`
	args := []interface{}{code, year}
	if month > 0 {
		q += " AND v.period_month=$3"
		args = append(args, month)
	}
	q += " ORDER BY v.voucher_date, v.voucher_no, l.line_no"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	periodDr, periodCr := decimal.Zero, decimal.Zero
	for rows.Next() {
		var e LedgerEntry
		var dr, cr decimal.Decimal
		if err := rows.Scan(&e.Date, &e.VoucherID, &e.VoucherNo, &e.Summary, &dr, &cr, &e.PartnerName, &e.Status); err != nil {
			return out, err
		}
		e.AccountCode = acc.Code
		e.AccountName = acc.Name
		e.Debit = dr.StringFixed(2)
		e.Credit = cr.StringFixed(2)
		running = running.Add(dr).Sub(cr)
		e.Balance = running.Abs().StringFixed(2)
		if running.GreaterThanOrEqual(decimal.Zero) {
			e.Side = "借"
		} else {
			e.Side = "贷"
		}
		periodDr = periodDr.Add(dr)
		periodCr = periodCr.Add(cr)
		out.Entries = append(out.Entries, e)
	}
	out.Debit = periodDr.StringFixed(2)
	out.Credit = periodCr.StringFixed(2)
	out.Closing = running.StringFixed(2)
	return out, rows.Err()
}

func GeneralJournal(ctx context.Context, db DBTX, year, month int, account string) ([]LedgerEntry, error) {
	q := `
		SELECT v.voucher_date, v.id, v.voucher_no, COALESCE(NULLIF(l.summary,''), v.summary),
		       l.account_code, a.name, l.debit, l.credit, l.partner_name, v.status
		FROM gl_voucher_lines l
		JOIN gl_vouchers v ON v.id = l.voucher_id
		JOIN gl_accounts a ON a.code = l.account_code
		WHERE v.status='posted' AND v.period_year=$1`
	args := []interface{}{year}
	n := 2
	if month > 0 {
		q += fmt.Sprintf(" AND v.period_month=$%d", n)
		args = append(args, month)
		n++
	}
	if account != "" {
		q += fmt.Sprintf(" AND l.account_code=$%d", n)
		args = append(args, account)
	}
	q += " ORDER BY v.voucher_date, v.voucher_no, l.line_no"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		var dr, cr decimal.Decimal
		if err := rows.Scan(&e.Date, &e.VoucherID, &e.VoucherNo, &e.Summary, &e.AccountCode, &e.AccountName,
			&dr, &cr, &e.PartnerName, &e.Status); err != nil {
			return nil, err
		}
		e.Debit = dr.StringFixed(2)
		e.Credit = cr.StringFixed(2)
		out = append(out, e)
	}
	return out, rows.Err()
}

func CashJournal(ctx context.Context, db DBTX, year, month int, account string) (AccountLedger, error) {
	if account == "" {
		account = "1001"
	}
	acc, err := GetAccount(ctx, db, account)
	if err != nil {
		return AccountLedger{}, err
	}
	if !acc.IsCash {
		return AccountLedger{}, fmt.Errorf("%s 不是现金/银行科目", account)
	}
	return AccountBook(ctx, db, account, year, month)
}

type ReportLine struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Amount  string `json:"amount"`
	Indent  int    `json:"indent"`
	Bold    bool   `json:"bold"`
	Href    string `json:"href,omitempty"`
	Section string `json:"section,omitempty"`
}

type Statement struct {
	Title      string       `json:"title"`
	Year       int          `json:"year"`
	Month      int          `json:"month"`
	Lines      []ReportLine `json:"lines"`
	Balanced   bool         `json:"balanced"`
	Message    string       `json:"message,omitempty"`
	LeftTotal  string       `json:"left_total,omitempty"`
	RightTotal string       `json:"right_total,omitempty"`
}

func sumCodes(tb TrialBalanceResult, codes ...string) decimal.Decimal {
	want := map[string]struct{}{}
	for _, c := range codes {
		want[c] = struct{}{}
	}
	total := decimal.Zero
	for _, r := range tb.Rows {
		if !r.IsLeaf {
			continue
		}
		if _, ok := want[r.Code]; !ok {
			continue
		}
		dr, _ := parseMoney(r.EndDebit)
		cr, _ := parseMoney(r.EndCredit)
		total = total.Add(dr).Sub(cr)
	}
	return money(total)
}

func sumCategory(tb TrialBalanceResult, cat string) decimal.Decimal {
	total := decimal.Zero
	for _, r := range tb.Rows {
		if !r.IsLeaf || r.Category != cat {
			continue
		}
		dr, _ := parseMoney(r.EndDebit)
		cr, _ := parseMoney(r.EndCredit)
		total = total.Add(dr).Sub(cr)
	}
	return money(total)
}

func fmtAmt(d decimal.Decimal) string { return d.StringFixed(2) }

func line(key, label string, amt decimal.Decimal, indent int, bold bool) ReportLine {
	return ReportLine{Key: key, Label: label, Amount: fmtAmt(amt), Indent: indent, Bold: bold, Href: "/ledger/books/trial"}
}

func BalanceSheet(ctx context.Context, db DBTX, year, month int) (Statement, error) {
	tb, err := TrialBalance(ctx, db, year, month, false)
	if err != nil {
		return Statement{}, err
	}
	cash := sumCodes(tb, "1001", "1002", "1012")
	ar := sumCodes(tb, "1121", "1122", "1123", "1221")
	inv := sumCodes(tb, "1403", "1405")
	current := cash.Add(ar).Add(inv)
	fixed := sumCodes(tb, "1601").Sub(sumCodes(tb, "1602").Abs())
	intang := sumCodes(tb, "1701", "1801", "1901")
	noncurrent := fixed.Add(intang)
	assets := current.Add(noncurrent)

	liab := sumCategory(tb, "liability").Neg()
	if liab.LessThan(decimal.Zero) {
		liab = liab.Neg()
	}
	equity := sumCategory(tb, "equity").Neg()
	if equity.LessThan(decimal.Zero) {
		equity = equity.Neg()
	}
	right := liab.Add(equity)

	s := Statement{
		Title:      "资产负债表",
		Year:       year,
		Month:      month,
		Balanced:   assets.Equal(right),
		LeftTotal:  fmtAmt(assets),
		RightTotal: fmtAmt(right),
	}
	if !s.Balanced {
		s.Message = fmt.Sprintf("资产 %s 不等于 负债+权益 %s。请检查未过账凭证或期初是否平衡。", fmtAmt(assets), fmtAmt(right))
	}
	s.Lines = []ReportLine{
		line("cash", "货币资金", cash, 1, false),
		line("ar", "应收及预付款项", ar, 1, false),
		line("inv", "存货", inv, 1, false),
		line("current", "流动资产合计", current, 0, true),
		line("fixed", "固定资产净额", fixed, 1, false),
		line("intang", "无形资产及其他", intang, 1, false),
		line("noncurrent", "非流动资产合计", noncurrent, 0, true),
		line("assets", "资产总计", assets, 0, true),
		line("stloan", "短期借款", sumCodes(tb, "2001").Neg(), 1, false),
		line("ap", "应付及预收款项", sumCodes(tb, "2201", "2202", "2203", "2241").Neg(), 1, false),
		line("payroll", "应付职工薪酬", sumCodes(tb, "2211").Neg(), 1, false),
		line("tax", "应交税费", sumCodes(tb, "22210101", "22210105", "222102", "222103").Neg(), 1, false),
		line("liab", "负债合计", liab, 0, true),
		line("capital", "实收资本", sumCodes(tb, "4001").Neg(), 1, false),
		line("re", "未分配利润", sumCodes(tb, "410401", "4103", "4101", "4002").Neg(), 1, false),
		line("equity", "所有者权益合计", equity, 0, true),
		line("right", "负债和所有者权益总计", right, 0, true),
	}
	for i := range s.Lines {
		if s.Lines[i].Key == "assets" || s.Lines[i].Key == "right" {
			s.Lines[i].Section = "total"
		}
	}
	return s, nil
}

func IncomeStatement(ctx context.Context, db DBTX, year, month int) (Statement, error) {
	act, err := periodActivity(ctx, db, year, month)
	if err != nil {
		return Statement{}, err
	}
	rev := sumActivity(act, "6001", "6051").Neg()
	cogs := sumActivity(act, "6401", "6402")
	gross := rev.Sub(cogs)
	taxAdd := sumActivity(act, "6403")
	opex := sumActivity(act, "6601", "6602", "6603")
	otherInc := sumActivity(act, "6301").Neg()
	otherExp := sumActivity(act, "6711")
	profit := gross.Sub(taxAdd).Sub(opex).Add(otherInc).Sub(otherExp)
	incomeTax := sumActivity(act, "6801")
	net := profit.Sub(incomeTax)
	s := Statement{Title: "利润表", Year: year, Month: month, Balanced: true}
	s.Lines = []ReportLine{
		line("rev", "一、营业收入", rev, 0, true),
		line("cogs", "减：营业成本", cogs, 1, false),
		line("taxadd", "税金及附加", taxAdd, 1, false),
		line("gross", "二、营业利润（毛利）", gross.Sub(taxAdd), 0, true),
		line("opex", "减：期间费用", opex, 1, false),
		line("selling", "　销售费用", sumActivity(act, "6601"), 2, false),
		line("admin", "　管理费用", sumActivity(act, "6602"), 2, false),
		line("fin", "　财务费用", sumActivity(act, "6603"), 2, false),
		line("otherinc", "加：营业外收入", otherInc, 1, false),
		line("otherexp", "减：营业外支出", otherExp, 1, false),
		line("profit", "三、利润总额", profit, 0, true),
		line("tax", "减：所得税费用", incomeTax, 1, false),
		line("net", "四、净利润", net, 0, true),
	}
	return s, nil
}

type activityAmt struct {
	dr, cr decimal.Decimal
}

func periodActivity(ctx context.Context, db DBTX, year, month int) (map[string]activityAmt, error) {
	q := `
		SELECT l.account_code, COALESCE(SUM(l.debit),0), COALESCE(SUM(l.credit),0)
		FROM gl_voucher_lines l
		JOIN gl_vouchers v ON v.id = l.voucher_id
		WHERE v.status='posted' AND v.period_year=$1
		  AND v.source_type NOT IN ('period_close', 'opening')`
	args := []interface{}{year}
	if month > 0 {
		q += " AND v.period_month=$2"
		args = append(args, month)
	}
	q += " GROUP BY l.account_code"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]activityAmt{}
	for rows.Next() {
		var code string
		var a activityAmt
		if err := rows.Scan(&code, &a.dr, &a.cr); err != nil {
			return nil, err
		}
		out[code] = a
	}
	return out, rows.Err()
}

func sumActivity(act map[string]activityAmt, codes ...string) decimal.Decimal {
	total := decimal.Zero
	for _, c := range codes {
		a := act[c]
		total = total.Add(a.dr).Sub(a.cr)
	}
	return money(total)
}

func CashFlowStatement(ctx context.Context, db DBTX, year, month int) (Statement, error) {
	q := `
		SELECT v.id, l.account_code, a.category, a.is_cash, l.debit, l.credit, v.source_type
		FROM gl_voucher_lines l
		JOIN gl_vouchers v ON v.id = l.voucher_id
		JOIN gl_accounts a ON a.code = l.account_code
		WHERE v.status='posted' AND v.period_year=$1`
	args := []interface{}{year}
	if month > 0 {
		q += " AND v.period_month=$2"
		args = append(args, month)
	}
	type cfLine struct {
		code, cat, source string
		cash              bool
		dr, cr            decimal.Decimal
	}
	byV := map[uuid.UUID][]cfLine{}
	vrows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return Statement{}, err
	}
	defer vrows.Close()
	type agg struct{ opIn, opOut, inv, fin decimal.Decimal }
	var a agg
	for vrows.Next() {
		var id uuid.UUID
		var code, cat, source string
		var isCash bool
		var dr, cr decimal.Decimal
		if err := vrows.Scan(&id, &code, &cat, &isCash, &dr, &cr, &source); err != nil {
			return Statement{}, err
		}
		byV[id] = append(byV[id], cfLine{code, cat, source, isCash, dr, cr})
	}
	if err := vrows.Err(); err != nil {
		return Statement{}, err
	}
	for _, lines := range byV {
		cashNet := decimal.Zero
		var counterpart string
		var counterpartCat string
		for _, ln := range lines {
			if ln.cash {
				cashNet = cashNet.Add(ln.dr).Sub(ln.cr)
			} else if counterpart == "" || ln.dr.Add(ln.cr).GreaterThan(decimal.Zero) {
				counterpart = ln.code
				counterpartCat = ln.cat
			}
		}
		if cashNet.IsZero() {
			continue
		}
		bucket := classifyCash(counterpart, counterpartCat, lines[0].source)
		switch bucket {
		case "op_in":
			if cashNet.GreaterThan(decimal.Zero) {
				a.opIn = a.opIn.Add(cashNet)
			} else {
				a.opOut = a.opOut.Add(cashNet.Neg())
			}
		case "op_out":
			if cashNet.GreaterThan(decimal.Zero) {
				a.opIn = a.opIn.Add(cashNet)
			} else {
				a.opOut = a.opOut.Add(cashNet.Neg())
			}
		case "invest":
			a.inv = a.inv.Add(cashNet)
		case "finance":
			a.fin = a.fin.Add(cashNet)
		default:
			if cashNet.GreaterThan(decimal.Zero) {
				a.opIn = a.opIn.Add(cashNet)
			} else {
				a.opOut = a.opOut.Add(cashNet.Neg())
			}
		}
	}
	op := a.opIn.Sub(a.opOut)
	net := op.Add(a.inv).Add(a.fin)
	s := Statement{Title: "现金流量表", Year: year, Month: month, Balanced: true}
	s.Lines = []ReportLine{
		line("opin", "销售及往来收款", a.opIn, 1, false),
		line("opout", "采购、费用及薪酬支付", a.opOut, 1, false),
		line("op", "经营活动产生的现金流量净额", op, 0, true),
		line("inv", "投资活动产生的现金流量净额", a.inv, 0, true),
		line("fin", "筹资活动产生的现金流量净额", a.fin, 0, true),
		line("net", "现金及现金等价物净增加额", net, 0, true),
	}
	return s, nil
}

func classifyCash(code, cat, source string) string {
	switch {
	case source == "sales_order" || code == "1122" || code == "6001":
		return "op_in"
	case source == "purchase_order" || code == "2202" || code == "1405":
		return "op_out"
	case source == "reimbursement" || source == "salary" || cat == "expense":
		return "op_out"
	case code == "1601" || code == "1701":
		return "invest"
	case code == "4001" || code == "2001" || code == "2501":
		return "finance"
	default:
		return "op"
	}
}
