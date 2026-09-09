package web

import (
	"strconv"
)

// getPage 分页参数解析（1 起，封顶 10000）。
func getPage(q map[string][]string, key string) int32 {
	if vs, ok := q[key]; ok && len(vs) > 0 {
		if p, err := strconv.ParseInt(vs[0], 10, 32); err == nil && p > 0 {
			if p > 10000 {
				return 10000
			}
			return int32(p)
		}
	}
	return 1
}
