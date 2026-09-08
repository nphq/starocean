package shared

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// ParseDecimal 解析金额字符串。P0: 解析失败不再静默归零（曾导致资损风险），
// 而是返回 error，由调用方拒绝请求。
func ParseDecimal(s string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, fmt.Errorf("无效的金额: %q", s)
	}
	return d, nil
}

// MustParseDecimal 兼容历史调用：仅用于求平均/展示等非资金写入路径，
// 解析失败按零值跳过（调用方应逐步迁移到 ParseDecimal）。
func MustParseDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// ParsePrice 校验写入路径的单价：非空、合法 decimal、>=0、最多两位小数。
func ParsePrice(s string) (decimal.Decimal, error) {
	d, err := ParseDecimal(s)
	if err != nil {
		return decimal.Zero, err
	}
	if d.IsNegative() {
		return decimal.Zero, fmt.Errorf("金额不能为负数: %q", s)
	}
	return d, nil
}
