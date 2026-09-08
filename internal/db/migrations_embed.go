package db

import "embed"

// EmbeddedMigrations 内嵌迁移目录，供测试/工具直接使用（与 main.go 的 embed 同源）。
//
//go:embed migrations
var EmbeddedMigrations embed.FS
