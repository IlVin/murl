// Package migrations содержит SQL-миграции для базы данных,
// упакованные в бинарный файл с помощью механизма embed.
package migrations

import "embed"

// MigrationsDir содержит файловую систему с SQL-скриптами миграций.
// Путь "." означает текущую директорию (папку migrations).
//
//go:embed *.sql
var MigrationsDir embed.FS
