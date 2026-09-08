// Package vmodel 供 templ 模板与 web 处理器共享的视图模型与格式化函数。
// 单独成包以避免 view/pages ↔ internal/web 的 import cycle。
package vmodel

import (
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

// MonthPoint 月度趋势点。
type MonthPoint struct {
	Label     string
	Sales     float64
	Purchases float64
}

// StatusLabel 状态中文化。
func StatusLabel(s string) string {
	switch s {
	case "draft":
		return "草稿"
	case "confirmed":
		return "已确认"
	case "shipped":
		return "已发货"
	case "received":
		return "已收货"
	case "invoiced":
		return "已开票"
	case "paid":
		return "已付款"
	case "cancelled":
		return "已取消"
	case "pending_approval":
		return "待审批"
	case "approved":
		return "已批准"
	case "rejected":
		return "已驳回"
	case "active":
		return "在职"
	case "inactive":
		return "停用"
	case "posted":
		return "已过账"
	case "closed":
		return "已结账"
	case "open":
		return "未结账"
	case "void":
		return "已作废"
	case "completed":
		return "已完成"
	case "picked":
		return "已拣"
	case "shortage":
		return "短缺"
	default:
		return s
	}
}

// StatusClass 状态徽标颜色（中性底 + 语义字色小方 pill）。
func StatusClass(s string) string {
	switch s {
	case "confirmed", "invoiced", "paid", "approved", "active", "posted", "completed", "picked":
		return "bg-emerald-50 text-emerald-700 border border-emerald-200 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800/50"
	case "shipped", "received", "pending_approval", "shortage":
		return "bg-amber-50 text-amber-700 border border-amber-200 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-800/50"
	case "cancelled", "rejected", "void":
		return "bg-rose-50 text-rose-700 border border-rose-200 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-800/50"
	default:
		return "bg-warm-100 text-warm-600 border border-warm-200 dark:bg-warm-700/50 dark:text-warm-300 dark:border-warm-700"
	}
}

// Money 金额 → ¥x.xx。
func Money(v decimal.Decimal) string {
	return "¥" + v.StringFixed(2)
}

// MoneyStr 字符串金额 → ¥x.xx（解析失败原样返回）。
func MoneyStr(s string) string {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return "¥" + s
	}
	return Money(d)
}

// DateOnly 日期（零值/远古值显示破折号）。
func DateOnly(t time.Time) string {
	if t.IsZero() || t.Year() < 2000 {
		return "—"
	}
	return t.Format("2006-01-02")
}

// DateTime 日期时间。
func DateTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02 15:04")
}

// Itoa int → string（模板内便捷）。
func Itoa(n int) string {
	return strconv.Itoa(n)
}

// I32toa int32 → string。
func I32toa(n int32) string {
	return strconv.FormatInt(int64(n), 10)
}

// I64toa int64 → string。
func I64toa(n int64) string {
	return strconv.FormatInt(n, 10)
}
