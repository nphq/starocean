package models

import "github.com/shopspring/decimal"

func (o SalesOrder) CanTransitionTo(toStatus string) bool {
	switch o.Status {
	case "draft":
		return toStatus == "confirmed" || toStatus == "cancelled"
	case "confirmed":
		return toStatus == "shipped" || toStatus == "cancelled"
	case "shipped":
		return toStatus == "invoiced" || toStatus == "cancelled"
	case "invoiced":
		return false
	case "cancelled":
		return toStatus == "draft"
	default:
		return false
	}
}

func (o SalesOrder) NeedsStockReversal() bool {
	return o.Status == "confirmed" || o.Status == "shipped" || o.Status == "invoiced"
}

func (o PurchaseOrder) CanTransitionTo(toStatus string) bool {
	switch o.Status {
	case "draft":
		return toStatus == "confirmed" || toStatus == "cancelled"
	case "confirmed":
		return toStatus == "received" || toStatus == "cancelled"
	case "received":
		return toStatus == "paid"
	case "paid":
		return false
	case "cancelled":
		return toStatus == "draft"
	default:
		return false
	}
}

func (o PurchaseOrder) NeedsStockReversal() bool {
	return o.Status == "received"
}

func (c Customer) CanExtendCredit(amount decimal.Decimal) bool {
	if c.CreditLimit.LessThanOrEqual(decimal.Zero) {
		return true
	}
	newBalance := c.Balance.Add(amount)
	return newBalance.LessThanOrEqual(c.CreditLimit)
}

func (p Product) AfterStockIn(qty int32) int32 {
	return p.CurrentStock + qty
}

func (p Product) AfterStockOut(qty int32) int32 {
	after := p.CurrentStock - qty
	if after < 0 {
		return 0
	}
	return after
}

func (r Reimbursement) CanTransitionTo(toStatus string) bool {
	switch r.Status {
	case "draft":
		return toStatus == "pending_approval" || toStatus == "cancelled"
	case "pending_approval":
		return toStatus == "approved" || toStatus == "rejected"
	case "approved":
		return toStatus == "paid"
	case "paid", "rejected":
		return false
	default:
		return false
	}
}

func (r Reimbursement) IsTerminal() bool {
	return r.Status == "paid" || r.Status == "rejected"
}
