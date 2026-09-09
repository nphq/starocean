package db

import (
	"os"
	"testing"
)

// TestMain 为 db 包测试进程分配独立的 Turso 原生库缓存目录，避免 `go test ./...`
// 并行测试二进制之间的解压竞态（详见 IsolateTursoCacheDir）。
func TestMain(m *testing.M) {
	cleanup := IsolateTursoCacheDir()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
