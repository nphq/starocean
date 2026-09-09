package db

import "embed"

// schemaFS 内嵌基线 schema（与 main.go 同源，经 MigrateTurso 执行）。
//
//go:embed schema.sql
var schemaFS embed.FS
