package ledger

import (
	"os"
	"testing"

	"github.com/nphq/starocean/internal/db"
)

// TestMain 为 ledger 包测试进程分配独立的 Turso 原生库缓存目录，避免 `go test ./...`
// 并行测试二进制之间的解压竞态（详见 db.IsolateTursoCacheDir）。
func TestMain(m *testing.M) {
	cleanup := db.IsolateTursoCacheDir()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
